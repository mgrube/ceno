package main

import (
    "testing"
    "os"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

// Database file to craete/use that will not interfere with the one
// created during the normal operation on the RSS Reader
const TEST_DB_FILE = "testing_db.db"

// A URI for Hacker News' RSS feed
const TEST_FEED_URL = "https://news.ycombinator.com/rss"

/**
 * Test that the database has been intialized properly and that we
 * can, without error, insert some data into the feeds table and then
 * retrieve it successfully.
 */
func TestDBInitialization(t *testing.T) {
    t.Log("Testing database initialization")
    var db *sql.DB
    var err error
    db, err = InitDBConnection(TEST_DB_FILE)
    defer db.Close()
    if err != nil {
        t.Error(err)
    }
    tx, _ := db.Begin()
    stmt, _ := tx.Prepare("insert into feeds(url, type, charset) values(?, ?, ?)")
    _, err = stmt.Exec(TEST_FEED_URL, "RSS", "")
    if err != nil {
        t.Error(err)
    }
    tx.Commit()
    rows, err2 := db.Query("select url, type, charset from feeds")
    if err2 != nil {
        t.Error(err2)
    }
    var foundTestData bool = false
    for rows.Next() {
        var url, _type, charset string
        rows.Scan(&url, &_type, &charset)
        if url == TEST_FEED_URL && (_type == "RSS" || _type == "rss") && charset == "" {
            foundTestData = true
            break
        }
    }
    if !foundTestData {
        t.Log("Could not find the test data that was inserted into the database.")
        t.Fail()
    }
}

/**
 * Test that our abstraction over Go's builtin database operations work
 * well enough for an operation to save new feed data to work.
 */
func TestSaveNewFeed(t *testing.T) {
    t.Log("Testing SaveNewFeed")
    db, err := InitDBConnection(TEST_DB_FILE)
    if err != nil {
        t.Error(err)
    }
    defer db.Close()
    feed := FeedInfo{0, TEST_FEED_URL, "RSS", "test-charset"}
    err = SaveNewFeed(db, feed)
    if err != nil {
        t.Error(err)
    }
    rows, err2 := db.Query("select url, type, charset from feeds")
    if err2 != nil {
        t.Error(err2)
    }
    var foundTestData bool = false
    for rows.Next() {
        var url, _type, charset string
        rows.Scan(&url, &_type, &charset)
        if url == TEST_FEED_URL &&
            (_type == "RSS" || _type == "rss") &&
            charset == "test-charset" {
            foundTestData = true
            break
        }
    }
    if !foundTestData {
        t.Log("Could not find the test data that was inserted into the database")
        t.Fail()
    }
}

/**
 * Test that we can create a handful of feeds and then retrieve them all.
 */
func TestAllFeeds(t *testing.T) {
    testFeeds := []FeedInfo{
        {0, "URL1", "RSS", "chs1"},
        {1, "URL2", "Atom", "chs2"},
        {2, "URL3", "RSS", "chs3"},
    }
    // A parallel array signalling which testFeeds have been retrieved.
    // Note that the values each default to `false` so they don't need to be set manually.
    var testsMatched []bool = make([]bool, len(testFeeds))
    db, err := InitDBConnection(TEST_DB_FILE)
    if err != nil {
        t.Error(err)
    }
    defer db.Close()
    tx, _ := db.Begin()
    // Insert all the test feeds into the database
    for _, feed := range testFeeds {
        stmt, err2 := tx.Prepare("insert into feeds (url, type, charset) values (?, ?, ?)")
        if err2 != nil {
            t.Error(err2)
        }
        defer stmt.Close()
        stmt.Exec(feed.URL, feed.Type, feed.Charset)
    }
    tx.Commit()
    // Retrieve all the test feeds from the database and make sure
    // we got everything we put in
    feeds, err3 := AllFeeds(db)
    if err3 != nil {
        t.Error(err3)
    }
    if len(feeds) < len(testFeeds) {
        t.Log("Did not retrieve as many feeds as were inserted for testing.")
        t.Fail()
    }
    for _, feed := range feeds {
        for i, testCase := range testFeeds {
            if feed.URL == testCase.URL &&
                feed.Type == testCase.Type &&
                feed.Charset == testCase.Charset {
                testsMatched[i] = true
                break
            }
        }
    }
    for i, match := range testsMatched {
        if !match {
            t.Logf("Did not retrieve test feed #%d.", i)
            t.Fail()
        }
    }
}

func TestMain(m *testing.M) {
    // Create the DB ahead of time.
    db, _ := InitDBConnection(TEST_DB_FILE)
    db.Close()
    result := m.Run()
    // Quite effectively deletes the entire SQLite database.
    os.Remove(TEST_DB_FILE)
    os.Exit(result)
}
