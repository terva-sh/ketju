package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The search index and ketju redact (docs/03 "Hard redaction", "The search
// index"; TKT-01M3NS122F). The words are unique to the text the tests
// redact, and none appears in a quote.

// newIndexedRedactWorld is a redactWorld with a public forum 1, so
// views.Search serves its threads.
func newIndexedRedactWorld(t *testing.T) *redactWorld {
	t.Helper()
	w := newRedactWorld(t)
	withDB(t, w.data, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: "public", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return w
}

// indexed projects every thread and catches the search index up, as a
// stopped serve leaves them.
func (w *redactWorld) indexed() {
	w.t.Helper()
	w.ready()
	catchUp(w.t, w.data)
}

// reopen opens the thread cache again after indexed closed it, as a serve
// started again would, so the world can be written to.
func (w *redactWorld) reopen() {
	w.t.Helper()
	c, err := threadcache.New(w.data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache = c
}

// hits is how many rows views.Search finds for word.
func (w *redactWorld) hits(word string) int {
	w.t.Helper()
	return len(searchFor(w.t, w.data, word))
}

// searchRows is how many search_docs rows (thread, ordinal) has.
func (w *redactWorld) searchRows(id threadid.ID, ordinal uint32) int {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM search_docs WHERE thread_id = ? AND ordinal = ?`, int64(id), ordinal)
}

// rawSearch names every search table, the FTS5 shadow tables included,
// with a row holding s anywhere in a text or blob column.
func (w *redactWorld) rawSearch(s string) []string {
	w.t.Helper()
	var found []string
	d := openDB(w.t, w.data)
	defer d.Close()
	if err := d.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT name FROM sqlite_schema WHERE type = 'table' AND name LIKE 'search%'`)
		if err != nil {
			return err
		}
		var tables []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			tables = append(tables, n)
		}
		rows.Close()
		for _, table := range tables {
			if table == "search" {
				continue // the FTS5 table itself reads through search_docs
			}
			rs, err := tx.QueryContext(w.t.Context(), fmt.Sprintf(`SELECT * FROM "%s"`, table))
			if err != nil {
				return err
			}
			cols, _ := rs.Columns()
			for rs.Next() {
				v := make([]any, len(cols))
				p := make([]any, len(cols))
				for i := range v {
					p[i] = &v[i]
				}
				if err := rs.Scan(p...); err != nil {
					rs.Close()
					return err
				}
				for _, x := range v {
					var b []byte
					switch x := x.(type) {
					case string:
						b = []byte(x)
					case []byte:
						b = x
					}
					if bytes.Contains(b, []byte(s)) {
						found = append(found, table)
					}
				}
			}
			rs.Close()
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return found
}

// dbBytes is ketju.db and ketju.db-wal as they are on disk now.
func (w *redactWorld) dbBytes() map[string][]byte {
	w.t.Helper()
	out := map[string][]byte{}
	for _, name := range []string{"ketju.db", "ketju.db-wal"} {
		b, err := os.ReadFile(filepath.Join(w.data, "db", name))
		if err != nil && !os.IsNotExist(err) {
			w.t.Fatal(err)
		}
		out[name] = b
	}
	return out
}

// TestRedactClearsSearch is criteria 1 and 2's first half. A post holding a
// word found nowhere else, with a copy of it in another thread as a split
// makes one, and a post in a third thread quoting it: after ketju redact the
// word finds nothing, no search_docs row is left for either redacted
// (thread, ordinal), no search table holds the word, and the quoting post is
// still found by its own words and listed. The title variant leaves row 0
// holding only the replacement.
func TestRedactClearsSearch(t *testing.T) {
	t.Run("a post and its copy", func(t *testing.T) {
		w := newIndexedRedactWorld(t)
		a, b := w.thread("Source thread"), w.thread("Split thread")
		w.post(a, "an ordinary neighbour")
		n := w.post(a, "the quagmirezyx statement")
		cp := w.copyOf(b, postRef{a, n}, "the quagmirezyx statement")
		q := w.post(a, fmt.Sprintf("> [quote=#%d]\n> the statement\n\ngibbonword reply\n", n))
		w.indexed()
		if w.hits("quagmirezyx") != 2 || w.hits("gibbonword") != 1 {
			t.Fatalf("the setup indexed %d and %d", w.hits("quagmirezyx"), w.hits("gibbonword"))
		}

		out := w.mustRedact(postArgs(a, n)...)
		if !strings.Contains(out, fmt.Sprintf("%s/%d revision 1 quotes #%d", a, q, n)) {
			t.Errorf("the quoting post is not listed:\n%s", out)
		}
		if !strings.Contains(out, "search index: 2 row(s) to clear or rewrite") {
			t.Errorf("the plan does not count the two rows:\n%s", out)
		}
		if h := w.hits("quagmirezyx"); h != 0 {
			t.Errorf("the redacted word still finds %d rows", h)
		}
		for _, p := range []postRef{{a, n}, {b, cp}} {
			if r := w.searchRows(p.Thread, p.Ordinal); r != 0 {
				t.Errorf("%s/%d keeps %d search rows", p.Thread, p.Ordinal, r)
			}
		}
		if tables := w.rawSearch("irezyx"); len(tables) != 0 {
			t.Errorf("search tables still hold the word: %v", tables)
		}
		if w.hits("gibbonword") != 1 || w.hits("neighbour") != 1 {
			t.Error("a post the redaction did not name lost its row")
		}
		if err := checkSearchIndex(t, w.data); err != nil {
			t.Error(err)
		}
	})
	t.Run("a title", func(t *testing.T) {
		w := newIndexedRedactWorld(t)
		id := w.thread("The narwhalqux affair")
		w.post(id, "a post under it")
		w.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: uint64(w.users["mod"]),
			Body: &threadlog.ThreadRenamedBody{Title: "Still narwhalqux", Slug: "still-narwhalqux"}})
		w.indexed()
		if w.hits("narwhalqux") != 1 {
			t.Fatalf("the setup indexed the title %d times", w.hits("narwhalqux"))
		}
		w.mustRedact(titleArgs(id)...)
		if h := w.hits("narwhalqux"); h != 0 {
			t.Errorf("the redacted title still finds %d rows", h)
		}
		var title string
		w.query(&title, `SELECT title FROM search_docs WHERE thread_id = ? AND ordinal = 0`, int64(id))
		if title != defaultReplacement {
			t.Errorf("row 0 holds %q, want %q", title, defaultReplacement)
		}
		if tables := w.rawSearch("halqux"); len(tables) != 0 {
			t.Errorf("search tables still hold the title: %v", tables)
		}
	})
}

// TestRedactClearsRowsCatchUpWouldMiss is criterion 1's "before it deletes
// its journal": with the logs already swapped and the thread's search_state
// current, a row put back for the redacted post afterwards, as a stale index
// would hold it, is still deleted by the journal's clear on resume.
func TestRedactClearsRowsCatchUpWouldMiss(t *testing.T) {
	w := newIndexedRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "the stalewordqq text")
	w.indexed()
	if _, err := w.redact("committed", postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	if w.hits("stalewordqq") != 0 {
		t.Fatal("the per-thread catch-up did not drop the row")
	}
	withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error {
		return db.PutSearchDoc(t.Context(), tx, db.SearchDoc{Thread: int64(id), Ordinal: int64(n), Revision: 1,
			AuthorID: int64(w.users["alice"]), CreatedAt: 1, SourcePlain: "the stalewordqq text"})
	})
	if w.hits("stalewordqq") != 1 {
		t.Fatal("the stale row was not put")
	}
	w.mustRedact("--resume")
	if w.hits("stalewordqq") != 0 || w.searchRows(id, n) != 0 {
		t.Error("the journal's clear left the stale row")
	}
}

// TestRedactThenReindexSearch is criterion 2's second half: after the
// redaction, ketju reindex --search finds nothing for the word, and the log
// alone indexes nothing for the post.
func TestRedactThenReindexSearch(t *testing.T) {
	w := newIndexedRedactWorld(t)
	id := w.thread("A thread")
	w.post(id, "kept words")
	n := w.post(id, "the wombatique text")
	w.indexed()
	w.mustRedact(postArgs(id, n)...)
	sum := reindexSearchJSON(t, w.data, "--search")
	if sum.Search == nil || len(sum.Search.Failed) != 0 {
		t.Fatalf("reindex --search: %+v", sum.Search)
	}
	if w.hits("wombatique") != 0 || w.searchRows(id, n) != 0 {
		t.Error("the rebuilt index holds the redacted post")
	}
	if w.hits("kept") != 1 {
		t.Error("the rebuilt index lost the other post")
	}
}

// TestRedactLeavesNoTermsInTheDatabase is criterion 5. The post is indexed
// at revision 1 holding W1, then edited by serve's ordinary path to revision
// 2 holding W2 and not W1, so W1's row was replaced and its pages freed long
// before the redaction. A control shows W1's bytes were in ketju.db then.
// After the command, neither ketju.db nor its WAL holds the last six bytes of
// W1 or W2. FTS5 stores terms prefix-compressed, so the test looks for the
// suffix rather than the whole word.
func TestRedactLeavesNoTermsInTheDatabase(t *testing.T) {
	const w1, w2 = "xylophonevqwerty", "marmaladekzxcvbn"
	suffix := func(s string) []byte { return []byte(s[len(s)-6:]) }
	w := newIndexedRedactWorld(t)
	id := w.thread("A thread")
	w.post(id, "kept words")
	n := w.post(id, "first "+w1)
	w.indexed()
	w.reopen()
	w.edit(id, n, &threadlog.EditBody{Source: "second " + w2})
	w.indexed()
	if w.hits(w1) != 0 || w.hits(w2) != 1 {
		t.Fatalf("after the edit: %d hits for W1, %d for W2", w.hits(w1), w.hits(w2))
	}
	before := w.dbBytes()
	if !bytes.Contains(before["ketju.db"], suffix(w1)) {
		t.Fatal("the control failed: W1 was not in ketju.db before the run, so the test proves nothing")
	}

	w.mustRedact(postArgs(id, n)...)
	after := w.dbBytes()
	for name, b := range after {
		for _, word := range []string{w1, w2} {
			if bytes.Contains(b, suffix(word)) {
				t.Errorf("%s holds the end of %q after the redaction", name, word)
			}
		}
	}
	if w.hits("kept") != 1 {
		t.Error("the other post lost its row")
	}
}

// TestRedactResumesSearchClear is criterion 3's first half: a run stopped at
// each search step, and at each step TestRedactResumesAfterEachStep covers,
// then resumed, ends with the same search rows as a run never stopped.
func TestRedactResumesSearchClear(t *testing.T) {
	world := func(t *testing.T) (*redactWorld, threadid.ID, threadid.ID, uint32, string) {
		w := newIndexedRedactWorld(t)
		h := w.blob("only mine", 1)
		a, b := w.thread("Source"), w.thread("Split")
		w.post(a, "kept words")
		n := w.post(a, "the resumeword text", h)
		w.copyOf(b, postRef{a, n}, "the resumeword text")
		w.indexed()
		return w, a, b, n, h
	}
	clean, _, _, cn, _ := world(t)
	clean.mustRedact(postArgs(clean.ids[0], cn)...)
	want, wantResults := searchAt(t, clean.data)

	steps := []string{"journal", "renamed:A", "synced:A", "index:A", "projected:A", "searched:A",
		"renamed:B", "projected:B", "searched:B", "references", "committed", "blob-tx:H", "blob-committed:H",
		"blob:H", "search-rows", "vacuumed", "search-cleared"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			w, a, b, n, h := world(t)
			fail := strings.NewReplacer("A", a.String(), "B", b.String(), "H", h).Replace(step)
			if _, err := w.redact(fail, postArgs(a, n)...); err == nil {
				t.Fatal("the injected failure did not stop the run")
			}
			if !w.journal() {
				t.Fatal("no journal was left to resume from")
			}
			w.mustRedact("--resume")
			got, results := searchAt(t, w.data)
			if got != want || results != wantResults {
				t.Errorf("resumed rows differ from an uninterrupted run's:\n%s\n---\n%s", want, got)
			}
			if err := checkSearchIndex(t, w.data); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestRedactWithoutSearchTables is criterion 3's second half: a database
// whose migrations stop before the search index is redacted without error,
// and is left without the search tables.
func TestRedactWithoutSearchTables(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "the text")
	w.ready()
	withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range []string{`DROP TABLE search`, `DROP TABLE search_docs`, `DROP TABLE search_state`,
			`DROP TABLE search_meta`, `DELETE FROM schema_migrations WHERE version >= 22`} {
			if _, err := tx.ExecContext(t.Context(), s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		return nil
	})
	w.mustRedact(postArgs(id, n)...)
	var missing []string
	withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		missing, err = db.MissingSearchTables(t.Context(), tx)
		return err
	})
	if len(missing) != 4 {
		t.Errorf("the run created search tables: missing %v", missing)
	}
}

// TestRedactRefusesPartialSearchTables: a database missing only some of the
// search tables, as dropping one by hand leaves it, is refused before any
// log is touched, since the rest may hold the text.
func TestRedactRefusesPartialSearchTables(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "the text")
	w.ready()
	withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `DROP TABLE search_state`)
		return err
	})
	before := w.hashes()
	_, err := w.redact("", postArgs(id, n)...)
	if exitCodeFor(err) != exitInternal || !strings.Contains(fmt.Sprint(err), "search_state") {
		t.Fatalf("exit %d: %v", exitCodeFor(err), err)
	}
	if !mapsEqual(before, w.hashes()) || w.journal() {
		t.Error("a refused run changed the data directory")
	}
}

// TestRestoredBackupDropsRedactedText is criterion 4: a backup taken before
// the redaction, restored after it, loses the redacted rows when the indexer
// catches up as serve's does.
func TestRestoredBackupDropsRedactedText(t *testing.T) {
	w := newIndexedRedactWorld(t)
	id := w.thread("A thread")
	w.post(id, "kept words")
	n := w.post(id, "the backupword text")
	w.indexed()
	// A copy of a cleanly closed database is a whole backup; see
	// TestREADMERecoveryProcedure for why it is not VACUUM INTO.
	backup, err := os.ReadFile(filepath.Join(w.data, "db", "ketju.db"))
	if err != nil {
		t.Fatal(err)
	}
	w.mustRedact(postArgs(id, n)...)
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(filepath.Join(w.data, "db", "ketju.db"+suffix)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(w.data, "db", "ketju.db"), backup, 0o600); err != nil {
		t.Fatal(err)
	}
	if w.hits("backupword") != 1 {
		t.Fatal("the restored backup does not hold the row")
	}
	catchUp(t, w.data)
	if w.hits("backupword") != 0 || w.searchRows(id, n) != 0 {
		t.Error("catch-up over the restored backup kept the redacted row")
	}
	if w.hits("kept") != 1 {
		t.Error("catch-up lost the other post")
	}
}

// TestRedactKeepsTheJournalWhenTheCheckpointIsBlocked: a reader on another
// handle holding the WAL when the run checkpoints it, so ketju.db would keep
// its pages from before the VACUUM, fails the run with the journal kept,
// and a resume once the reader is gone finishes it.
func TestRedactKeepsTheJournalWhenTheCheckpointIsBlocked(t *testing.T) {
	w := newIndexedRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "the blockedword text")
	w.indexed()

	release := make(chan struct{})
	held := make(chan error, 1)
	var reader *db.DB
	o := redactOpts{now: func() time.Time { w.clock += 1000; return time.UnixMilli(w.clock) },
		fail: func(step string) error {
			if step != "vacuumed" {
				return nil
			}
			var err error
			if reader, err = db.Open(t.Context(), db.Options{Path: filepath.Join(w.data, "db", "ketju.db")}); err != nil {
				return err
			}
			started := make(chan struct{})
			go func() {
				held <- reader.Read(context.Background(), func(_ *dbq.Queries, tx *sql.Tx) error {
					var c int
					if err := tx.QueryRow(`SELECT count(*) FROM search_docs`).Scan(&c); err != nil {
						return err
					}
					close(started)
					<-release
					return nil
				})
			}()
			<-started
			return nil
		}}
	var out bytes.Buffer
	err := redactMain(t.Context(), &out, &out, append([]string{"--root", w.data}, postArgs(id, n)...), o)
	close(release)
	if reader == nil {
		t.Fatalf("the run never reached the vacuumed step: %v\n%s", err, out.String())
	}
	if herr := <-held; herr != nil {
		t.Fatal(herr)
	}
	reader.Close()
	if err == nil || !errors.Is(err, db.ErrBusy) || !w.journal() {
		t.Fatalf("a blocked checkpoint: err %v, journal kept %v\n%s", err, w.journal(), out.String())
	}
	w.mustRedact("--resume")
	if w.journal() || w.hits("blockedword") != 0 {
		t.Error("the resume did not finish the run")
	}
	for name, b := range w.dbBytes() {
		if bytes.Contains(b, []byte("kedword")) {
			t.Errorf("%s still holds the word", name)
		}
	}
}
