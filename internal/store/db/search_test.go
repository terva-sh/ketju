package db

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

const searchMarker = "-- SEARCH INDEX: derived from thread logs; rebuild with ketju reindex --search"

// matchCount runs a MATCH on whatever runs it and returns the hits.
func matchCount(ctx context.Context, tx *sql.Tx, term string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM search WHERE search MATCH ?`, term).Scan(&n)
	return n, err
}

func put(t *testing.T, d *DB, docs ...SearchDoc) {
	t.Helper()
	err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, doc := range docs {
			if err := PutSearchDoc(t.Context(), tx, doc); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hits(t *testing.T, d *DB, term string) int {
	t.Helper()
	var n int
	err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		n, err = matchCount(t.Context(), tx, term)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func checkIndex(t *testing.T, d *DB) {
	t.Helper()
	err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return CheckSearchIndex(t.Context(), tx)
	})
	if err != nil {
		t.Fatalf("FTS5 integrity-check: %v", err)
	}
}

// TestEveryConnectionHasFTS5 is criterion 1: the write pool, every
// connection of the read pool, and a pool from OpenReadOnly each run MATCH.
// Without fts5.Register in openPool each would fail with "no such module:
// fts5" or "no such table".
func TestEveryConnectionHasFTS5(t *testing.T) {
	d, path := openTest(t)
	put(t, d, SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, SourcePlain: "hello world"})

	err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		n, err := matchCount(t.Context(), tx, "hello")
		if err == nil && n != 1 {
			err = fmt.Errorf("write pool: %d hits, want 1", n)
		}
		return err
	})
	if err != nil {
		t.Fatalf("write pool: %v", err)
	}

	// Hold every read connection at once, so each of them runs the MATCH
	// rather than one connection reused four times.
	var ready, wg sync.WaitGroup
	ready.Add(defaultReadConns)
	errs := make(chan error, defaultReadConns)
	for range defaultReadConns {
		wg.Go(func() {
			errs <- d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				n, err := matchCount(t.Context(), tx, "world")
				ready.Done()
				ready.Wait()
				if err == nil && n != 1 {
					err = fmt.Errorf("read pool: %d hits, want 1", n)
				}
				return err
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("read pool: %v", err)
		}
	}

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	err = ro.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		n, err := matchCount(t.Context(), tx, "hello")
		if err == nil && n != 1 {
			err = fmt.Errorf("%d hits, want 1", n)
		}
		return err
	})
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
}

// TestSearchTablesAreMarked is criterion 7's half in this package. The
// SEARCH INDEX marker is read back from sqlite_schema, and the marked set
// must equal SearchTables in both directions. No search table carries the
// PROJECTION marker, and every other table whose name begins with "search"
// is the FTS5 table or one of its shadow tables.
func TestSearchTablesAreMarked(t *testing.T) {
	d, _ := openTest(t)
	var marked, projections, others []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT name, sql FROM sqlite_schema WHERE type = 'table' ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, ddl string
			if err := rows.Scan(&name, &ddl); err != nil {
				return err
			}
			switch {
			case strings.Contains(ddl, searchMarker):
				marked = append(marked, name)
				if strings.Contains(ddl, projectionMarker) {
					projections = append(projections, name)
				}
			case strings.HasPrefix(name, "search"):
				others = append(others, name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := slices.Sorted(slices.Values(SearchTables)); !slices.Equal(marked, want) {
		t.Errorf("tables carrying the SEARCH INDEX marker are %v; SearchTables is %v", marked, want)
	}
	if len(projections) > 0 {
		t.Errorf("%v carry the PROJECTION marker too; the search index is not a projection", projections)
	}
	for _, s := range SearchTables {
		if slices.Contains(ProjectionTables, s) {
			t.Errorf("%s is in ProjectionTables as well as SearchTables", s)
		}
	}
	// FTS5 with external content keeps no content table of its own.
	if want := []string{"search", "search_config", "search_data", "search_docsize", "search_idx"}; !slices.Equal(others, want) {
		t.Errorf("the other search tables are %v; want the FTS5 table and its shadows %v", others, want)
	}
}

// TestClearProjectionsKeepsSearch is criterion 7's other half: reindex
// --projections empties the projections and leaves every search row.
func TestClearProjectionsKeepsSearch(t *testing.T) {
	d, _ := openTest(t)
	put(t, d, SearchDoc{Thread: 1, Ordinal: 0, Title: "a title"}, SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, SourcePlain: "kept"})
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if err := q.SetSearchState(t.Context(), dbq.SetSearchStateParams{ThreadID: 1, LogLength: 100, UpdatedAt: 1}); err != nil {
			return err
		}
		if err := q.SetSearchVersion(t.Context(), 7); err != nil {
			return err
		}
		return ClearProjections(t.Context(), q)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := hits(t, d, "kept"); n != 1 {
		t.Errorf("%d hits for a post's word after ClearProjections, want 1", n)
	}
	if n := hits(t, d, "title"); n != 1 {
		t.Errorf("%d hits for the title after ClearProjections, want 1", n)
	}
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if n, err := q.GetSearchState(t.Context(), 1); err != nil || n != 100 {
			return fmt.Errorf("search_state = %d, %v; want 100", n, err)
		}
		if v, err := q.GetSearchVersion(t.Context()); err != nil || v != 7 {
			return fmt.Errorf("search_meta = %d, %v; want 7", v, err)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	checkIndex(t, d)
}

// TestClearSearchEmptiesTheIndex: ClearSearch empties every search table and
// the FTS5 index, and integrity-check passes afterwards, including on an
// index whose segments were damaged first, which a row-by-row delete could
// not clear.
func TestClearSearchEmptiesTheIndex(t *testing.T) {
	for _, damaged := range []bool{false, true} {
		t.Run(fmt.Sprintf("damaged=%v", damaged), func(t *testing.T) {
			d, _ := openTest(t)
			var docs []SearchDoc
			for i := range 50 {
				docs = append(docs, SearchDoc{Thread: int64(i%5 + 1), Ordinal: int64(i + 1), Revision: 1,
					SourcePlain: fmt.Sprintf("word%d common text", i)})
			}
			put(t, d, docs...)
			err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
				if err := q.SetSearchState(t.Context(), dbq.SetSearchStateParams{ThreadID: 1, LogLength: 9, UpdatedAt: 1}); err != nil {
					return err
				}
				return q.SetSearchVersion(t.Context(), 3)
			})
			if err != nil {
				t.Fatal(err)
			}
			if damaged {
				// Overwrite every segment block (id > 10 leaves the
				// structure record and averages alone) with garbage.
				err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
					_, err := tx.ExecContext(t.Context(), `UPDATE search_data SET block = x'deadbeefdeadbeef' WHERE id > 10`)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error { return CheckSearchIndex(t.Context(), tx) })
				if err == nil {
					t.Fatal("integrity-check passes on the damaged index; the damage did not take")
				}
			}
			err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error { return ClearSearch(t.Context(), tx) })
			if err != nil {
				t.Fatalf("ClearSearch: %v", err)
			}
			checkIndex(t, d)
			err = d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				for _, table := range append(slices.Clone(SearchTables), "search") {
					var n int
					if err := tx.QueryRowContext(t.Context(), `SELECT count(*) FROM "`+table+`"`).Scan(&n); err != nil {
						return err
					}
					if n != 0 {
						t.Errorf("%s has %d rows after ClearSearch", table, n)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if n := hits(t, d, "common"); n != 0 {
				t.Errorf("%d hits after ClearSearch", n)
			}
			// The cleared index takes new rows.
			put(t, d, SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, SourcePlain: "fresh"})
			if n := hits(t, d, "fresh"); n != 1 {
				t.Errorf("%d hits for a row put after ClearSearch, want 1", n)
			}
			checkIndex(t, d)
		})
	}
}

// TestPutAndDropKeepTheIndexInStep: random puts, replacements, drops and
// whole-thread drops, after which integrity-check passes, and every word
// matches exactly the rows that hold it.
func TestPutAndDropKeepTheIndexInStep(t *testing.T) {
	d, _ := openTest(t)
	rng := rand.New(rand.NewPCG(1, 2))
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"}
	type key struct{ thread, ordinal int64 }
	want := map[key]string{}
	text := func() string {
		n := 1 + rng.IntN(3)
		var w []string
		for range n {
			w = append(w, words[rng.IntN(len(words))])
		}
		return strings.Join(w, " ")
	}
	for range 400 {
		k := key{int64(1 + rng.IntN(4)), int64(rng.IntN(6))}
		err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			switch r := rng.IntN(10); {
			case r < 6:
				s := text()
				want[k] = s
				doc := SearchDoc{Thread: k.thread, Ordinal: k.ordinal, Revision: int64(rng.IntN(3)), SourcePlain: s}
				if k.ordinal == 0 {
					doc.Title, doc.SourcePlain = s, ""
				}
				return PutSearchDoc(t.Context(), tx, doc)
			case r < 9:
				delete(want, k)
				return DropSearchDoc(t.Context(), tx, k.thread, k.ordinal)
			default:
				for w := range want {
					if w.thread == k.thread {
						delete(want, w)
					}
				}
				return DropSearchThread(t.Context(), tx, k.thread)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	checkIndex(t, d)
	for _, w := range words {
		n := 0
		for _, s := range want {
			if slices.Contains(strings.Fields(s), w) {
				n++
			}
		}
		if got := hits(t, d, w); got != n {
			t.Errorf("%q: %d hits, want %d", w, got, n)
		}
	}
	var rows int
	err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM search_docs`).Scan(&rows)
	})
	if err != nil || rows != len(want) {
		t.Errorf("search_docs has %d rows (%v), want %d", rows, err, len(want))
	}
}

// TestPutWithUnchangedTextWritesNoIndexEntry: putting a row again with the
// same text leaves the FTS5 index untouched, and a changed revision alone
// is still recorded.
func TestPutWithUnchangedTextWritesNoIndexEntry(t *testing.T) {
	var stmts []string
	var mu sync.Mutex
	path := filepath.Join(t.TempDir(), "db", "ketju.db")
	d, err := Open(t.Context(), Options{Path: path, Trace: func(s string) {
		mu.Lock()
		stmts = append(stmts, s)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	doc := SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, SourcePlain: "same"}
	put(t, d, doc)
	mu.Lock()
	stmts = nil
	mu.Unlock()
	doc.Revision = 2
	put(t, d, doc)
	mu.Lock()
	seen := slices.Clone(stmts)
	mu.Unlock()
	for _, s := range seen {
		if strings.Contains(s, "INSERT INTO search ") {
			t.Errorf("an unchanged text wrote to the FTS5 index: %s", s)
		}
	}
	err = d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		r, err := q.GetSearchDoc(t.Context(), dbq.GetSearchDocParams{ThreadID: 1, Ordinal: 1})
		if err == nil && r.Revision != 2 {
			err = fmt.Errorf("revision %d, want 2", r.Revision)
		}
		return err
	})
	if err != nil {
		t.Error(err)
	}
}

// searchWrites matches a statement that writes search_docs or the FTS5
// table: an insert, replace, update or delete naming either.
var searchWrites = regexp.MustCompile(`(?is)\b(INSERT\s+(OR\s+\w+\s+)?INTO|REPLACE\s+INTO|UPDATE(\s+OR\s+\w+)?|DELETE\s+FROM)\s+["` + "`" + `\[]?search(_docs)?\b["` + "`" + `\]]?\s*[(\s;]`)

// searchWriters are the generated methods that write search_docs.
var searchWriters = []string{"InsertSearchDoc", "UpdateSearchDoc", "DeleteSearchDoc", "ClearSearchDocs"}

// searchWritesIn reports, for every non-test Go file under root, the string
// literals that write the index and the calls to a generated writer, outside
// the files allowed to: allowed are relative to root.
func searchWritesIn(t *testing.T, root string, allowed []string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var found []string
	checked := 0
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", "spikes", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		if slices.Contains(allowed, rel) {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(n.Value)
				if err == nil && searchWrites.MatchString(s+" ") {
					found = append(found, fmt.Sprintf("%s: %q", rel, s))
				}
			case *ast.SelectorExpr:
				if slices.Contains(searchWriters, n.Sel.Name) {
					found = append(found, fmt.Sprintf("%s: calls %s", rel, n.Sel.Name))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatalf("no Go files under %s", root)
	}
	return found
}

// TestOnlySearchGoWritesTheIndex: search.go is the one place that writes
// search_docs or the FTS5 table, and the generated queries it calls are
// called from nowhere else. A second writer would skip the FTS5 'delete'
// that keeps the index in step with its rows, and the index would then hold
// text no row has.
func TestOnlySearchGoWritesTheIndex(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	found := searchWritesIn(t, root, []string{
		"internal/store/db/search.go",
		"internal/store/db/dbq/search.sql.go", // the generated statements themselves
		"internal/store/db/dbq/querier.go",    // their interface
	})
	for _, f := range found {
		t.Errorf("%s. Only internal/store/db/search.go writes the search index; use PutSearchDoc, DropSearchDoc or ClearSearch.", f)
	}
}

// TestTheSearchWriteWalkFindsAStrayWriter: the walk is live. A file that
// writes the index in each way it can is found, and the allowed file is not.
func TestTheSearchWriteWalkFindsAStrayWriter(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ok/search.go", "package ok\nconst s = `DELETE FROM search_docs WHERE id = ?`\n")
	write("bad/a.go", "package bad\nconst a = `INSERT INTO search (rowid, title) VALUES (?, ?)`\n"+
		"const b = \"update search_docs set title = ?\"\nconst c = `DELETE FROM search_state`\n")
	write("bad/b.go", "package bad\nfunc f(q interface{ DeleteSearchDoc() }) { q.DeleteSearchDoc() }\n")
	write("bad/b_test.go", "package bad\nconst t = `DELETE FROM search_docs`\n")
	found := searchWritesIn(t, root, []string{"ok/search.go"})
	want := []string{
		`bad/a.go: "INSERT INTO search (rowid, title) VALUES (?, ?)"`,
		`bad/a.go: "update search_docs set title = ?"`,
		`bad/b.go: calls DeleteSearchDoc`,
	}
	if !slices.Equal(found, want) {
		t.Errorf("found %q\nwant  %q", found, want)
	}
}
