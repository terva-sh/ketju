package sqlitedriver

import (
	"database/sql"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/ncruces/go-sqlite3"
	ncdriver "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	_ "modernc.org/sqlite"
)

var (
	benchDir = flag.String("bench.dir", "",
		"directory on REAL storage for scratch databases; empty means $HOME/.cache/ketju-bench/sqlite-driver. "+
			"Never the system temp dir, which is commonly tmpfs")
	benchAllowVolatile = flag.Bool("bench.allow-volatile-fs", false,
		"run even where fsync is a no-op, to check the benchmarks still RUN. Numbers produced under it mean nothing")
)

// dsnQuery is the whole connection configuration, and it is the same string
// for both drivers. That is the driver-agnostic claim in its smallest form:
// ncruces and modernc both read _txlock and _pragma, so BEGIN turns into
// BEGIN IMMEDIATE and every connection gets WAL and an explicit busy_timeout
// without a line of driver-specific code. synchronous is left at SQLite's
// default (FULL) so the write figures include a real fsync per commit; the
// projection is rebuildable, but it shares a file with authoritative tables.
const dsnQuery = "?_txlock=immediate" +
	"&_pragma=journal_mode(wal)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(on)"

// driverUnderTest names a database/sql driver and how to open it.
type driverUnderTest struct {
	name string
	open func(path string) (*sql.DB, error) // path may carry extra DSN query keys
}

var drivers = []driverUnderTest{
	{"ncruces", func(path string) (*sql.DB, error) {
		// ncdriver.Open rather than sql.Open("sqlite3", ...) only so the
		// FTS5 extension is registered on every connection. That callback is
		// the single ncruces-specific line the write path needs.
		return ncdriver.Open("file:"+path, func(c *sqlite3.Conn) error {
			return fts5.Register(c)
		})
	}},
	{"modernc", func(path string) (*sql.DB, error) {
		return sql.Open("sqlite", "file:"+path)
	}},
}

// scratchRootOnce resolves the scratch directory once and refuses tmpfs,
// where fsync costs nothing and every write figure would flatter both drivers.
var scratchRootOnce = sync.OnceValues(func() (string, error) {
	dir := *benchDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache", "ketju-bench", "sqlite-driver")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return "", err
	}
	const tmpfsMagic = 0x01021994
	if st.Type == tmpfsMagic && !*benchAllowVolatile {
		return "", errors.New(dir + " is tmpfs; pass -bench.dir on real storage, or -bench.allow-volatile-fs")
	}
	return dir, nil
})

// openScratch opens a fresh database file for one run and applies the draft
// schema. extra is appended to the DSN query. The file is removed when the
// test or benchmark ends.
func openScratch(tb testing.TB, d driverUnderTest, extra string) *sql.DB {
	tb.Helper()
	root, err := scratchRootOnce()
	if err != nil {
		tb.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, d.name+"-")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { os.RemoveAll(dir) })

	db, err := d.open(filepath.Join(dir, "ketju.db") + dsnQuery + extra)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	if _, err := db.Exec(draftSchema); err != nil {
		tb.Fatalf("%s: schema: %v", d.name, err)
	}
	return db
}

// draftSchema is the projection half of docs/04, close enough to exercise the
// write path and no closer. TKT-01M29BYYJ8 owns the real schema.
const draftSchema = `
CREATE TABLE IF NOT EXISTS threads (
	id                   INTEGER PRIMARY KEY,
	forum_id             INTEGER NOT NULL,
	slug                 TEXT    NOT NULL,
	title                TEXT    NOT NULL,
	author_id            INTEGER NOT NULL,
	created_at           INTEGER NOT NULL,
	post_count           INTEGER NOT NULL,
	last_post_at         INTEGER NOT NULL,
	last_post_author_id  INTEGER NOT NULL,
	last_post_ordinal    INTEGER NOT NULL,
	locked               INTEGER NOT NULL DEFAULT 0,
	pinned_scope         TEXT,
	pinned_until         INTEGER,
	archived             INTEGER NOT NULL DEFAULT 0,
	log_length_projected INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS thread_tags (
	thread_id INTEGER NOT NULL,
	tag       TEXT    NOT NULL,
	PRIMARY KEY (thread_id, tag)
);
CREATE TABLE IF NOT EXISTS post_meta (
	thread_id        INTEGER NOT NULL,
	ordinal          INTEGER NOT NULL,
	author_id        INTEGER NOT NULL,
	created_at       INTEGER NOT NULL,
	current_revision INTEGER NOT NULL,
	edited_at        INTEGER,
	reply_to         INTEGER,
	deleted          INTEGER NOT NULL DEFAULT 0,
	moved            INTEGER NOT NULL DEFAULT 0,
	log_offset       INTEGER NOT NULL,
	PRIMARY KEY (thread_id, ordinal)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS post_meta_author ON post_meta (author_id, created_at);
CREATE INDEX IF NOT EXISTS post_meta_reply  ON post_meta (thread_id, reply_to);
CREATE TABLE IF NOT EXISTS mentions (
	thread_id         INTEGER NOT NULL,
	ordinal           INTEGER NOT NULL,
	mentioned_user_id INTEGER NOT NULL,
	PRIMARY KEY (thread_id, ordinal, mentioned_user_id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS projection_state (
	thread_id  INTEGER PRIMARY KEY,
	log_length INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`
