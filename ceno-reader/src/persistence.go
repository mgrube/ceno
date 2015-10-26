/**
 * Functionality to persist information about feeds that
 * have been subscribed to and any data we might want to
 * keep about things like the number of items retrieved.
 */

package main

import (
	"database/sql"
	rss "github.com/jteeuwen/go-pkg-rss"
	//"github.com/jteeuwen/go-pkg-xmlx"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"time"
)

const createFeedsTable = `create table if not exists feeds(
    id integer primary key,
    url varchar(255) unique,
    type varchar(8),
    charset varchar(64),
    articles integer,
    lastPublished varchar(64),
    latest varchar(255)
);`

const createItemsTable = `create table if not exists items(
    id integer primary key,
    title varchar(255),
    url varchar(255),
    feed_url varchar(255),
    authors text,
    published varchar(64)
);`

const createErrorsTable = `create table if not exists errors(
	id integer primary key,
	resource_types integer,
	error_types integer,
	message text
);`

var tableInitializers = []string{
	createFeedsTable,
	createItemsTable,
	createErrorsTable,
}

/**
 * Repeatedly test a condition until it passes, allowing a thread to block
 * on the condition.
 * @param {func() bool} condition - A closure that will test the condition
 * @param {time.Duration} testRate - The frequency at which to invoke the condition function
 * @return A channel from which the number of calls to the condition were made when it passes
 */
func WaitUntilPass(condition func() bool, testRate time.Duration) chan int {
	reportAttempts := make(chan int, 1)
	go func() {
		attempts := 0
		passed := false
		for !passed {
			attempts++
			passed = condition()
			<-time.After(testRate)
		}
		reportAttempts <- attempts
	}()
	return reportAttempts
}

/**
 * Create a database connection to SQLite3 and initialize (if not already)
 * the tables used to store information about RSS feeds being followed etc.
 * @param {string} dbFileName - The name of the file to keep the database info in
 */
func InitDBConnection(dbFileName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbFileName)
	if err != nil {
		return nil, err
	}
	for _, initializer := range tableInitializers {
		_, err = db.Exec(initializer)
		if err != nil {
			break
		}
	}
	return db, err
}

/**
 * Persist information about a feed to the storage medium.
 * @param {*sql.DB} db - The database connection to use
 * @param {Feed} feed - Information describing the new feed to save
 */
func SaveFeed(db *sql.DB, feed Feed) error {
	tx, err1 := db.Begin()
	if err1 != nil {
		return err1
	}
	_, err2 := tx.Exec(`
        insert into feeds(url, type, charset, articles, lastPublished, latest)
        values(?,?,?,?,?,?)`,
		feed.Url, feed.Type, feed.Charset, feed.Articles, feed.LastPublished, feed.Latest)
	if err2 != nil {
		return err2
	}
	tx.Commit()
	return nil
}

/**
 * Get a collection of all the feeds subscribed to.
 * @param {*sql.DB} db - The database connection to use
 */
func AllFeeds(db *sql.DB) ([]Feed, error) {
	var feeds []Feed
	tx, err1 := db.Begin()
	if err1 != nil {
		return feeds, err1
	}
	rows, err2 := tx.Query(`select id, url, type, charset, articles, lastPublished, latest
                            from feeds`)
	if err2 != nil {
		return feeds, err2
	}
	for rows.Next() {
		var url, _type, charset, lastPublished, latest string
		var id, articles int
		rows.Scan(&id, &url, &_type, &charset, &articles, &lastPublished, &latest)
		fmt.Printf("Found feed %s with %d articles. Last published %s on %s.\n",
			url, articles, latest, lastPublished)
		feeds = append(feeds, Feed{id, url, _type, charset, articles, lastPublished, latest})
	}
	rows.Close()
	return feeds, nil
}

/**
 * Get the basic information about a persisted feed from its URL.
 * @param {*sql.DB} db - The database connection to use
 * @param {string} url - The URL to search for
 */
func GetFeed(db *sql.DB, url string) (Feed, error) {
	var feed Feed
	tx, err1 := db.Begin()
	if err1 != nil {
		return feed, err1
	}
	stmt, err2 := tx.Prepare(`select id, url, type, charset, articles, lastPublished, latest
                              from feeds where url=?`)
	if err2 != nil {
		return feed, err2
	}
	defer stmt.Close()
	rows, err3 := stmt.Query(url)
	if err3 != nil {
		return feed, err3
	}
	var id, articles int
	var _type, charset, lastPublished, latest string
	rows.Scan(&id, &url, &_type, &charset, &articles, &lastPublished, &latest)
	rows.Close()
	return Feed{id, url, _type, charset, articles, lastPublished, latest}, nil
}

/**
 * Delete a feed by referencing its URL.
 * @param {*sql.DB} db - The database connection to use
 * @param {string} url - The URL of the feed
 */
func DeleteFeed(db *sql.DB, url string) error {
	tx, err1 := db.Begin()
	if err1 != nil {
		return err1
	}
	stmt, err2 := tx.Prepare("delete from feeds where url=?")
	if err2 != nil {
		return err2
	}
	defer stmt.Close()
	_, err3 := stmt.Exec(url)
	if err3 != nil {
		return err3
	}
	tx.Commit()
	return nil
}

/**
 * Store information about an item in a channel from a particular feed
 * @param {*sql.DB} db - the database connection to use
 * @param {string} feedUrl - The URL of the RSS/Atom feed
 * @param {*rss.Item} item - The item to store the content of
 */
func SaveItem(db *sql.DB, feedUrl string, item *rss.Item) error {
	tx, err1 := db.Begin()
	if err1 != nil {
		return err1
	}
	url := item.Links[0].Href
	authors := item.Author.Name
	if item.Contributors != nil {
		for _, contrib := range item.Contributors {
			authors += " " + contrib
		}
	}
	// Insert the item itself
	_, err2 := tx.Exec(`
        insert into items(title, url, feed_url, authors, published)
        values(?, ?, ?, ?, ?)`,
		item.Title, url, feedUrl, authors, item.PubDate)
	if err2 != nil {
		return err2
	}
	_, err3 := tx.Exec(`
        update feeds
        set articles=articles+1, lastPublished=?, latest=?
        where url=?`,
		item.PubDate, item.Title, feedUrl)
	if err3 != nil {
		return err3
	}
	tx.Commit()
	return nil
}

/**
 * Get the items stored for a particular feed in reference to its URL.
 * @param {*sql.DB} db - the database connection to use
 * @param {string} url - The URL of the feed to get items from
 */
func GetItems(db *sql.DB, feedUrl string) ([]Item, error) {
	var items []Item
	tx, err1 := db.Begin()
	if err1 != nil {
		return items, err1
	}
	stmt, err2 := tx.Prepare(`select id, title, url, authors, published
                              from items where feed_url=?`)
	if err2 != nil {
		return items, err2
	}
	defer stmt.Close()
	rows, err3 := stmt.Query(feedUrl)
	if err3 != nil {
		return items, err3
	}
	for rows.Next() {
		var id int
		var title, authors, published, url string
		rows.Scan(&id, &title, &url, &authors, &published)
		items = append(items, Item{id, title, url, feedUrl, authors, published})
	}
	rows.Close()
	return items, nil
}

/**
 * Delete a particular item.
 * @param {*sql.DB} db - The database connection to use
 * @param {int} id - The identifier of the item to delete
 */
func DeleteItem(db *sql.DB, id int) error {
	tx, err1 := db.Begin()
	if err1 != nil {
		return err1
	}
	stmt, err2 := tx.Prepare("delete from items where id=?")
	if err2 != nil {
		return err2
	}
	defer stmt.Close()
	_, err3 := stmt.Exec(id)
	if err3 != nil {
		return err3
	}
	tx.Commit()
	return nil
}

/**
 * Save a report of the incidence of an error having occurred.
 * Note that converting between encoded resource types and error classifications
 * occurs strictly in the CENO Reader codebase. We aren't going to bother doing it in SQL.
 * @param {*sql.DB} db - The database connection to use
 * @param {Errorreport} report - Information about the error that occurred
 */
func SaveError(db *sql.DB, report ErrorReport) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, execErr := tx.Exec(`
		insert into errors (resource_types, error_types, message) values(?, ?, ?)`,
		report.ResourceTypes, report.ErrorTypes, report.ErrorMessage)
	if execErr != nil {
		return execErr
	}
	tx.Commit()
	return nil
}

/**
 * Get an array of ErrorReports corresponding to the kinds specified by the
 * argument to this function.  Once error reports are retrieved from the database,
 * they are deleted since we want to avoid reporting errors twice.
 * @param {*sql.DB} db - The database connection to use
 * @param {ErrorReport} kinds - Information about the errors to fetch
 */
func GetErrors(db *sql.DB, kinds ErrorReport) ([]ErrorReport, error) {
	reports := make([]ErrorReport, 0)
	ids := make([]int, 0)
	tx, err := db.Begin()
	if err != nil {
		return reports, err
	}
	// Get the relevant rows from the database.
	// Note that error types and resource types are specified in the database as integers.
	// That means we do the usual binary operations to find them.
	rows, queryError := tx.Query(`
		select id, resource_types, error_types, message
		from errors
		where resource_types & ? != 0 and error_types & ? != 0`,
		kinds.ResourceTypes, kinds.ErrorTypes)
	if queryError != nil {
		return reports, queryError
	}
	for rows.Next() {
		var id, resourceTypes, errorTypes int
		var message string
		rows.Scan(&id, &resourceTypes, &errorTypes, &message)
		reports = append(reports, ErrorReport{
			id, Resource(resourceTypes), ErrorClass(errorTypes), message,
		})
		ids = append(ids, id)
	}
	rows.Close()
	// Now we delete all the retrieved errors so we don't duplicate them in a later report.
	_, execError := tx.Exec(`delete from errors where id in ?`, ids)
	if execError != nil {
		return reports, execError
	}
	tx.Commit()
	return reports, nil
}
