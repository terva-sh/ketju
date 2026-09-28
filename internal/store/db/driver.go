package db

import (
	"database/sql"
	"errors"
	"net/url"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

// This file is the only one in the module that may import a SQLite driver.
// TestOnlyDriverGoImportsADriver enforces it.
//
// Everything else speaks database/sql, and the DSN keys below are shared with
// modernc.org/sqlite, which is what keeps modernc a drop-in fallback (docs/09).
// TKT-01M29BSPE8 measured both drivers and lists what a switch would cost in
// spikes/sqlite-driver/README.md; the parts of that list which apply here are
// the per-connection callback, the busy check, and the persist-WAL control.

// openPool opens a database/sql pool on path with the given DSN query, which
// must start with "?". Every connection the pool opens gets FCNTL_PERSIST_WAL,
// and SQLite's statement trace when trace is not nil.
func openPool(path, query string, trace func(sql string)) (*sql.DB, error) {
	return driver.Open(fileURI(path)+query, func(c *sqlite3.Conn) error {
		// Keep the -wal file when the last connection closes. SQLite would
		// otherwise delete it and create a fresh one on the next open, and
		// ncruces skips the directory fsync that a fresh file needs
		// (TKT-01M392DP19). With the file kept, the one directory fsync in
		// Open covers the life of the database.
		if _, err := c.FileControl("main", sqlite3.FCNTL_PERSIST_WAL, true); err != nil {
			return err
		}
		if trace == nil {
			return nil
		}
		return c.Trace(sqlite3.TRACE_STMT, func(_ sqlite3.TraceEvent, _, sql any) error {
			if s, ok := sql.(string); ok {
				trace(s)
			}
			return nil
		})
	})
}

// fileURI turns a filesystem path into a SQLite file: URI. The path is
// percent-encoded, so a '?', '#' or '%' in it stays part of the filename
// instead of starting the query string or a fragment.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path, OmitHost: true}).String()
}

// isBusy reports whether err is SQLITE_BUSY from the driver, whatever its
// extended code. It is the only place that knows the driver's error type.
func isBusy(err error) bool {
	return errors.Is(err, sqlite3.BUSY)
}

// IsUniqueViolation reports whether err is SQLite refusing a write that would
// break a UNIQUE constraint or index. Registration maps it to "that handle is
// taken" when two people submit the same handle at once (TKT-01M29C8005).
func IsUniqueViolation(err error) bool {
	return errors.Is(err, sqlite3.CONSTRAINT_UNIQUE)
}
