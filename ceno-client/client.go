package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/nicksnyder/go-i18n/i18n"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

const CONFIG_FILE string = "./config/client.json"

// A global configuration instance. Must be instantiated properly in main().
var Configuration Config

// Verifies a URL as valid (enough)
const URL_REGEX = "(https?://)?(www\\.)?\\w+\\.\\w+"

// The header used to communicate from the browser extension to the bundle server
// that a request for http://site.com was rewritten from one for https://site.com.
const REWRITTEN_HEADER = "X-Ceno-Rewritten"

// Result of a bundle lookup from cache server.
type Result struct {
	ErrCode  ErrorCode
	ErrMsg   string
	Complete bool
	Found    bool
	Bundle   string
	// Should add a Created field for the date created
}

/**
 * Set a header on responses that indicates that the response was served by the CENO client.
 * Useful for checking if the CENO Client is running via an HTTP request.
 * @param {ResponseWriter} w - The ResponseWriter used to serve the current request's response.
 */
func WriteProxyHeader(w http.ResponseWriter) http.ResponseWriter {
	w.Header().Add("X-Ceno-Proxy", "yxorP-oneC-X")
	return w
}

/**
 * Serve a page to inform the user that the bundle for the site they requested is being prepared.
 * The second return value is true when the response is HTML and false when text/plain.
 * @param {string} url - The URL that was originally requested
 */
func pleaseWait(url string) ([]byte, bool) {
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	content, err := ioutil.ReadFile(Configuration.PleaseWaitPage)
	if err != nil {
		T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
		return []byte(T("please_wait_txt")), false
	} else {
		content = bytes.Replace(content, []byte("{{.Paragraph1}}"), []byte(T("please_wait_p1_html")), 1)
		content = bytes.Replace(content, []byte("{{.Paragraph2}}"), []byte(T("please_wait_p2_html")), 1)
		content = bytes.Replace(content, []byte("{{.Redirect}}"), []byte(url), 1)
		return content, true
	}
}

/**
 * Report that an error occurred trying to decode the response from the LCS.
 * @param {string} reportURL - The URL to POST to to report the error.
 * @param {string} errMsg - The message to write to the LCS
 */
func reportDecodeError(reportURL, errMsg string) (bool, error) {
	mapping := map[string]interface{}{
		"error": errMsg,
	}
	marshalled, _ := json.Marshal(mapping)
	reader := bytes.NewReader(marshalled)
	req, err := http.NewRequest("POST", reportURL, reader)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	response, err := client.Do(req)
	return response.StatusCode == 200, err
}

/**
 * Request that the LCS start a lookup process for a particular URL.
 * @param {string} lookupURL - The URL to try to find in the distributed cache
 */
func lookup(lookupURL string) Result {
	response, err := http.Get(BundleLookupURL(Configuration, lookupURL))
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	if err != nil || response.StatusCode != 200 {
		fmt.Println(T("error_cli", map[string]interface{}{
			"Message": err.Error(),
		}))
		return Result{ERR_NO_CONNECT_LCS, err.Error(), false, false, ""}
	}
	decoder := json.NewDecoder(response.Body)
	var result Result
	if err := decoder.Decode(&result); err != nil {
		fmt.Println(T("decode_error_cli", map[string]interface{}{
			"Message": err.Error(),
		}))
		reachedLCS, err2 := reportDecodeError(DecodeErrReportURL(Configuration), err.Error())
		if reachedLCS {
			return Result{ERR_MALFORMED_LCS_RESPONSE, err2.Error(), false, false, ""}
		} else {
			errMsg := T("no_reach_lcs_cli")
			return Result{ERR_NO_CONNECT_LCS, errMsg, false, false, ""}
		}
	}
	return result
}

/**
 * Request that the RS issue a request to create a new bundle.
 * @param {string} lookupURL - The URL of the site to create a bundle for
 * @param {bool} wasRewritten - True of the requested URL was rewritten from HTTPS to HTTP
 */
func requestNewBundle(lookupURL string, wasRewritten bool) error {
	// We can ignore the content of the response since it is not used.
	reader := bytes.NewReader([]byte(lookupURL))
	URL := CreateBundleURL(Configuration, lookupURL)
	req, err := http.NewRequest("POST", URL, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	if wasRewritten {
		req.Header.Set(REWRITTEN_HEADER, "true")
	} else {
		req.Header.Set(REWRITTEN_HEADER, "false")
	}
	client := &http.Client{}
	_, err2 := client.Do(req)
	return err2
}

func execPleaseWait(URL string, w http.ResponseWriter, r *http.Request) {
	body, isHTML := pleaseWait(URL)
	if isHTML {
		w.Header().Set("Content-Type", "text/html")
	} else {
		w.Header().Set("Content-Type", "text/plain")
	}
	w.Write(body)
}

/**
 * Strip out the S in HTTPS from URLs provided to the CC via the /lookup path.
 * Returns the written URL and a boolean indicating whether the downgrade was made.
 * @param {string} URL - The decoded (from b64) URL being requested
 */
func stripHttps(URL string) (string, bool) {
	if strings.Index(URL, "https") != 0 {
		return URL, false
	} else {
		return strings.Replace(URL, "https", "http", 1), true
	}
}

/**
 * Handle requests of the form `http://127.0.0.1:3090/lookup?url=<base64-enc-url>`
 * so that we can simplify the problem of certain browsers trying particularly hard
 * to enforce HTTPS.  Rather than trying to deal with infinite redirecting between
 * HTTP and HTTPS, we can make requests directly to the client.
 * @param {ResponseWriter} w - The object used to handle writing responses to the client
 * @param {*Request} r - Information about the request
 */
func directHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	URLS, found := qs["url"]
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	if !found {
		state := ErrorState{
			"responseWriter": w, "request": r, "errMsg": T("querystring_no_url_cli"),
		}
		ErrorHandlers[ERR_MALFORMED_URL](state)
	} else {
		// Decode the URL so we can save effort by just passing the modified request to
		// the proxyHandler function from here.
		decodedBytes, err := base64.StdEncoding.DecodeString(URLS[0])
		if err != nil {
			state := ErrorState{
				"responseWriter": w, "request": r, "errMsg": T("url_b64_cli"),
			}
			ErrorHandlers[ERR_MALFORMED_URL](state)
		} else {
			decodedURL := string(decodedBytes)
			stripped, rewritten := stripHttps(decodedURL)
			if rewritten {
				r.Header.Set(REWRITTEN_HEADER, "true")
			}
			newURL, parseErr := url.Parse(stripped)
			if parseErr != nil {
				state := ErrorState{
					"responseWriter": w, "request": r, "errMsg": T("malformed_url_cli", map[string]interface{}{
						"URL": stripped,
					}),
				}
				ErrorHandlers[ERR_MALFORMED_URL](state)
			} else {
				// Finally we can pass the modified request onto the proxy server.
				r.URL = newURL
				proxyHandler(w, r)
			}
		}
	}
}

/**
 * Check if a provided URL is well-formed.  If not, serve an error page.
 * This call terminates requests when the return value is false (i.e. invalid URL).
 * @param {string} URL - The URL being requested
 * @param {ResponseWriter} w - The object handling writing responses to the client
 * @param {*Request} r - Information about the request
 */
func validateURL(URL string, w http.ResponseWriter, r *http.Request) bool {
	isValid, err := regexp.MatchString(URL_REGEX, URL)
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	if !isValid || err != nil {
		state := ErrorState{
			"responseWriter": w, "request": r, "errMsg": T("malformed_url_cli", map[string]interface{}{
				"URL": URL,
			}),
		}
		ErrorHandlers[ERR_MALFORMED_URL](state)
		return false
	}
	return true
}

/**
 * Handle errors reported by the LCS.  This function should terminate requests.
 * @param {Result} errInfo - Information (ErrCode and ErrMsg) about the error
 * @param {ResponseWriter} w - The object handling responding to the client
 * @param {*Request} r - Information about the request
 */
func handleLCSErrors(errInfo Result, w http.ResponseWriter, r *http.Request) {
	state := ErrorState{"responseWriter": w, "request": r, "errMsg": errInfo.ErrMsg}
	ErrorHandlers[ERR_FROM_LCS](state)
}

/**
 * Try to request a new bundle be created and serve the please wait page.
 * This function should terminate requests.
 * @param {string} URL - The URL to POST to to request a new bundle
 * @param {bool} rewritten - True if the request was downgraded from HTTPS to HTTP else false
 * @param {ResponseWriter} w - the object handling responding to the client
 * @param {*Request} r - Information about the request
 */
func tryRequestBundle(URL string, rewritten bool, w http.ResponseWriter, r *http.Request) {
	err := requestNewBundle(URL, rewritten)
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	if err != nil {
		fmt.Println(T("bundle_err_cli", map[string]interface{}{
			"Message": err.Error(),
		}))
		handleLCSErrors(Result{ERR_NO_CONNECT_RS, err.Error(), false, false, ""}, w, r)
	} else {
		execPleaseWait(URL, w, r)
	}
}

/**
 * Handle incoming requests for bundles.
 * 1. Initiate bundle lookup process
 * 2. Initiate bundle creation process when no bundle exists anywhere
 * @param {ResponseWriter} w - The object handling responding to the client
 * @param {*Request} r - Information about the request
 */
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	w = WriteProxyHeader(w)
	URL := r.URL.String()
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	wasRewritten := r.Header.Get(REWRITTEN_HEADER) == "true"
	fmt.Println(T("got_request_msg_cli", map[string]interface{}{
		"URL":       URL,
		"Rewritten": wasRewritten,
	}))
	if isValidURL := validateURL(URL, w, r); !isValidURL {
		return
	}
	result := lookup(URL)
	if result.ErrCode > 0 {
		fmt.Println(T("err_from_lcs_cli", map[string]interface{}{
			"Code":    result.ErrCode,
			"Message": result.ErrMsg,
		}))
		// Assuming the reason the response is malformed is because of the formation of the bundle,
		// so we will request that a new bundle be created.
		if result.ErrCode == ERR_MALFORMED_LCS_RESPONSE {
			tryRequestBundle(URL, wasRewritten, w, r)
		} else {
			handleLCSErrors(Result{ErrCode: result.ErrCode, ErrMsg: result.ErrMsg}, w, r)
		}
	} else if result.Complete {
		if result.Found {
			w.Write([]byte(result.Bundle))
		} else {
			tryRequestBundle(URL, wasRewritten, w, r)
		}
	} else {
		execPleaseWait(URL, w, r)
	}
}

func main() {
	// Configure the i18n library to use the preferred language set in the LANGUAGE environement variable
	language := os.Getenv("LANGUAGE")
	i18n.MustLoadTranslationFile(path.Join("translations", language+".all.json"))
	T, _ := i18n.Tfunc(os.Getenv("LANGUAGE"), "en-us")
	// Read an existing configuration file or have the user supply settings
	conf, err := ReadConfigFile(CONFIG_FILE)
	if err != nil {
		fmt.Println(T("no_config_cli", map[string]interface{}{
			"Location": CONFIG_FILE,
		}))
		Configuration = GetConfigFromUser()
	} else {
		Configuration = conf
	}
	// Create an HTTP proxy server
	http.HandleFunc("/lookup", directHandler)
	http.HandleFunc("/", proxyHandler)
	fmt.Println(T("listening_msg_cli", map[string]interface{}{
		"Port": Configuration.PortNumber,
	}))
	if err = http.ListenAndServe(Configuration.PortNumber, nil); err != nil {
		panic(err)
	}
}