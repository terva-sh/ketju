package sqlitedriver

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/ncruces/go-sqlite3"
	ncdriver "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

// ftsSchema is the search table docs/04 describes, over the columns it names.
// Only title and source_plain are indexed; the rest ride along unindexed so a
// hit can be turned back into a post.
func ftsSchema(tokenize string) string {
	return `CREATE VIRTUAL TABLE search USING fts5(
		thread_id UNINDEXED, ordinal UNINDEXED, revision UNINDEXED,
		title, source_plain, tokenize = '` + tokenize + `')`
}

const (
	stemmedDoc   = "We were running the migration twice before anyone noticed."
	unstemmedDoc = "The garden has a small pond."
)

func insertSearchRows(t *testing.T, db *sql.DB) {
	t.Helper()
	for i, body := range []string{stemmedDoc, unstemmedDoc} {
		if _, err := db.Exec(`INSERT INTO search (thread_id, ordinal, revision, title, source_plain)
			VALUES (?, ?, 1, 'Weekly notes', ?)`, 7, i+1, body); err != nil {
			t.Fatal(err)
		}
	}
}

// matchOrdinals returns the ordinals a MATCH query hits, in rank order.
func matchOrdinals(t *testing.T, db *sql.DB, query string) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT ordinal FROM search WHERE search MATCH ? ORDER BY rank`, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var o int64
		if err := rows.Scan(&o); err != nil {
			t.Fatal(err)
		}
		got = append(got, o)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestFTS5WithGoTokenizer is acceptance criterion 2 for ncruces: FTS5 is
// present, MATCH and snippet() work, and a tokenizer written in Go is
// registered through ext/fts5 and actually consulted.
func TestFTS5WithGoTokenizer(t *testing.T) {
	// FTS correctness does not depend on real storage, so t.TempDir is fine.
	path := filepath.Join(t.TempDir(), "fts.db")

	db, err := ncdriver.Open("file:"+path+dsnQuery, func(c *sqlite3.Conn) error {
		return fts5.RegisterCustom(c, func(api *fts5.API) error {
			return api.CreateTokenizer("ketju_stem", func(args []string) (fts5.Tokenizer, error) {
				return stemTokenizer{}, nil
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(ftsSchema("ketju_stem")); err != nil {
		t.Fatal(err)
	}
	insertSearchRows(t, db)

	// "run" matches "running" only because the Go tokenizer stemmed both.
	if got := matchOrdinals(t, db, "run"); len(got) != 1 || got[0] != 1 {
		t.Fatalf(`MATCH "run" with ketju_stem = %v, want [1]`, got)
	}

	var snip string
	if err := db.QueryRow(`SELECT snippet(search, 4, '[', ']', '…', 6)
		FROM search WHERE search MATCH 'run'`).Scan(&snip); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snip, "[running]") {
		t.Fatalf("snippet = %q, want the original word highlighted", snip)
	}

	// Control: the same query against the default tokenizer finds nothing,
	// which is what shows the Go tokenizer, not FTS5 itself, made the match.
	if _, err := db.Exec(`DROP TABLE search; ` + ftsSchema("unicode61")); err != nil {
		t.Fatal(err)
	}
	insertSearchRows(t, db)
	if got := matchOrdinals(t, db, "run"); len(got) != 0 {
		t.Fatalf(`MATCH "run" with unicode61 = %v, want none`, got)
	}
}

// TestFTS5OnModernc is the fallback's side of criterion 2: modernc ships
// FTS5 and its built-in tokenizers, porter among them, so English stemming
// survives a switch. There is no Go tokenizer API to test, which is recorded
// in the README as fallback cost.
func TestFTS5OnModernc(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "fts.db")+dsnQuery)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(ftsSchema("porter unicode61")); err != nil {
		t.Fatal(err)
	}
	insertSearchRows(t, db)
	if got := matchOrdinals(t, db, "run"); len(got) != 1 || got[0] != 1 {
		t.Fatalf(`MATCH "run" with porter = %v, want [1]`, got)
	}

	// An unregistered tokenizer name is a hard error, not a silent fallback
	// to unicode61, so a schema written for ncruces fails loudly on modernc.
	_, err = db.Exec(`CREATE VIRTUAL TABLE other USING fts5(body, tokenize = 'ketju_stem')`)
	if err == nil {
		t.Fatal("creating an fts5 table with an unregistered tokenizer succeeded on modernc")
	}
}

// stemTokenizer is a deliberately crude English stemmer: lower-case, split on
// anything that is not a letter or digit, strip a trailing "ing" or "s", and
// undouble a final consonant. It exists to prove a Go tokenizer is reachable,
// not to be one ketju ships; a real one would wrap a Snowball stemmer per
// locale, which is the reason docs/01 gives for preferring ncruces.
type stemTokenizer struct{}

func (stemTokenizer) Tokenize(flags fts5.TokenizeFlag, text, locale string, emit fts5.TokenCallback) error {
	start := -1
	for i, r := range text + " " {
		word := unicode.IsLetter(r) || unicode.IsDigit(r)
		switch {
		case word && start < 0:
			start = i
		case !word && start >= 0:
			if err := emit(0, stem(strings.ToLower(text[start:i])), start, i); err != nil {
				return err
			}
			start = -1
		}
	}
	return nil
}

func stem(w string) string {
	switch {
	case len(w) > 5 && strings.HasSuffix(w, "ing"):
		w = w[:len(w)-3]
		if n := len(w); n >= 2 && w[n-1] == w[n-2] && !strings.ContainsRune("aeiou", rune(w[n-1])) {
			w = w[:n-1]
		}
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		w = w[:len(w)-1]
	}
	return w
}

// TestSharedDSNIsHonoured checks the claim dsnQuery's comment makes: the same
// query string gives both drivers WAL, the busy_timeout, and BEGIN IMMEDIATE.
func TestSharedDSNIsHonoured(t *testing.T) {
	for _, d := range drivers {
		t.Run(d.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dsn.db")
			db, err := d.open(path + dsnQuery)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			var mode string
			var timeout int
			if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
				t.Fatal(err)
			}
			if mode != "wal" || timeout != 5000 {
				t.Fatalf("journal_mode=%s busy_timeout=%d, want wal and 5000", mode, timeout)
			}

			// An open transaction that has not written yet must already hold
			// the write lock. With a deferred BEGIN it would not, and a second
			// writer would get in.
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			other, err := d.open(path + dsnQuery)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			conn, err := other.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(t.Context(), `PRAGMA busy_timeout = 0`); err != nil {
				t.Fatal(err)
			}
			_, err = conn.ExecContext(t.Context(), `BEGIN IMMEDIATE`)
			if err == nil {
				t.Fatal("second writer began while the first transaction was open: BEGIN was not IMMEDIATE")
			}
			if !isBusy(err) {
				t.Fatalf("second writer got %v, want SQLITE_BUSY", err)
			}
		})
	}
}

// isBusy recognises SQLITE_BUSY from either driver by its message, which is
// the only thing the two error types have in common.
func isBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}
