package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events/search"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// searchWords are the words the reindex --search tests search for: one per
// post, title or revision in searchRedactWorld, whatever became of it.
var searchWords = []string{
	"tulip", "orchid", "lily", "cactus", "fern", "moss", "ivy", "secretword", "garden", "renamed",
	"quokka", "removed", "copied", "thread",
}

// searchRedactWorld is a data directory written as serve writes one, and
// indexed for search as it goes: posts, an edit, an author's delete, a
// moderator's hide, an auto-hide, a hide restored, a copy as a split makes
// one (split itself is reserved in this build), a rename, and then a ketju
// redact of one post and of one title, after which the index catches up as
// serve's would. It returns the world and its three threads.
func searchRedactWorld(t *testing.T) (*redactWorld, []threadid.ID) {
	t.Helper()
	w := newRedactWorld(t)
	withDB(t, w.data, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: "public", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	d := openDB(t, w.data)
	x, err := search.New(search.Options{DB: d, Root: w.data, Threads: w.cache, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := func(id threadid.ID) {
		t.Helper()
		if err := x.IndexThread(t.Context(), id, 0); err != nil {
			t.Fatal(err)
		}
	}
	mod, alice := uint64(w.users["mod"]), uint64(w.users["alice"])
	del := func(id threadid.ID, o uint32, by uint64, body threadlog.DeleteBody) {
		w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: by, Ordinal: o, Body: &body})
		index(id)
	}
	post := func(id threadid.ID, source string) uint32 {
		o := w.post(id, source)
		index(id)
		return o
	}

	garden := w.thread("Garden thread")
	index(garden)
	tulip := post(garden, "tulip and rose")
	orchid := post(garden, "an orchid")
	w.edit(garden, orchid, &threadlog.EditBody{Source: "an orchid and a lily"})
	index(garden)
	del(garden, post(garden, "a cactus"), alice, threadlog.DeleteBody{})
	del(garden, post(garden, "some fern"), mod, threadlog.DeleteBody{ByModerator: true, Reason: "spam"})
	del(garden, post(garden, "some moss"), 0, threadlog.DeleteBody{Auto: true})
	ivy := post(garden, "ivy climbs")
	del(garden, ivy, mod, threadlog.DeleteBody{ByModerator: true})
	w.append(garden, threadlog.Append{Type: threadlog.TypeRestore, Author: mod, Ordinal: ivy, Body: &threadlog.RestoreBody{}})
	index(garden)
	secret := post(garden, "the secretword itself")
	w.append(garden, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: mod,
		Body: &threadlog.ThreadRenamedBody{Title: "Garden renamed", Slug: "garden-renamed"}})
	index(garden)

	copies := w.thread("Copied thread")
	w.copyOf(copies, postRef{Thread: garden, Ordinal: tulip}, "tulip and rose copied")
	index(copies)
	classified := w.thread("Classified quokka")
	post(classified, "a post under a title that goes")

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	w.ready()
	w.mustRedact(postArgs(garden, secret)...)
	w.mustRedact(titleArgs(classified)...)
	catchUp(t, w.data)
	return w, []threadid.ID{garden, copies, classified}
}

// catchUp runs serve's search catch-up over the data directory.
func catchUp(t *testing.T, data string) {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	x, err := search.New(search.Options{DB: d, Root: data, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := x.Lag(); l.Threads != 0 {
		t.Fatalf("the catch-up left %+v behind", l)
	}
}

// searchAt is the data directory's search index as canonical text, and what
// each of searchWords finds in forum 1, as views.Search serves it.
func searchAt(t *testing.T, data string) (rows, results string) {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	var b strings.Builder
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, word := range searchWords {
			hits, err := views.Search(t.Context(), tx, views.SearchParams{
				Match: views.MatchAll([]string{word}), Forums: []int64{1}, Limit: 100})
			if err != nil {
				return err
			}
			for _, h := range hits {
				fmt.Fprintf(&b, "%s: %+v\n", word, h)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return dbtest.DumpSearch(t, d), b.String()
}

// checkSearchIndex runs FTS5's integrity-check on the data directory's index.
func checkSearchIndex(t *testing.T, data string) error {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	return d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error { return db.CheckSearchIndex(t.Context(), tx) })
}

func reindexSearchJSON(t *testing.T, data string, args ...string) reindexSummary {
	t.Helper()
	out, errOut, err := reindexRun(t, reindexOpts{}, append([]string{"--root", data, "--json"}, args...)...)
	if err != nil {
		t.Fatalf("reindex %v: %v\n%s", args, err, errOut)
	}
	var sum reindexSummary
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return sum
}

// TestReindexSearchEqualsIncremental is criterion 1: the index the
// incremental path built, rebuilt by reindex --search from the index as it
// is, from an emptied index, and from one whose FTS5 segments are damaged,
// has the same rows and gives the same results, and integrity-check passes.
func TestReindexSearchEqualsIncremental(t *testing.T) {
	w, ids := searchRedactWorld(t)
	wantRows, wantResults := searchAt(t, w.data)
	if err := checkSearchIndex(t, w.data); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"tulip", "lily", "ivy", "copied", "removed"} {
		if !strings.Contains(wantResults, s+": ") {
			t.Fatalf("the setup's index does not find %q:\n%s", s, wantResults)
		}
	}
	for _, s := range []string{"cactus", "fern", "moss", "secretword", "quokka"} {
		if strings.Contains(wantResults, s+": ") || strings.Contains(wantRows, s) {
			t.Fatalf("the setup's index holds %q:\n%s\n%s", s, wantRows, wantResults)
		}
	}

	for _, c := range []struct {
		name string
		lose func(t *testing.T)
	}{
		{"as it is", func(t *testing.T) {}},
		{"emptied", func(t *testing.T) {
			withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error { return db.ClearSearch(t.Context(), tx) })
		}},
		{"damaged segments", func(t *testing.T) {
			withDB(t, w.data, func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), `UPDATE search_data SET block = zeroblob(length(block)) WHERE id > 10`)
				return err
			})
			if checkSearchIndex(t, w.data) == nil {
				t.Fatal("the damage did not fail integrity-check")
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.lose(t)
			sum := reindexSearchJSON(t, w.data, "--search")
			if s := sum.Search; s == nil || s.Threads != len(ids) || s.Indexed != len(ids) || len(s.Failed) != 0 {
				t.Fatalf("search report %+v", sum.Search)
			}
			if err := checkSearchIndex(t, w.data); err != nil {
				t.Fatalf("integrity-check after the rebuild: %v", err)
			}
			rows, results := searchAt(t, w.data)
			if rows != wantRows {
				t.Errorf("rebuilt rows differ from the incremental ones:\n%s\n---\n%s", wantRows, rows)
			}
			if results != wantResults {
				t.Errorf("rebuilt results differ from the incremental ones:\n%s\n---\n%s", wantResults, results)
			}
		})
	}
}

// TestReindexSearchInterrupted is criterion 2's second half and the
// requirement carried from Terva review 1731: a run cancelled midway, after
// the index was emptied in its transaction, and one whose commit fails,
// each exit non-zero and leave the earlier index, its rows, results and
// version, as they were, the version being one this build does not write.
func TestReindexSearchInterrupted(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(t *testing.T, data string) error
	}{
		{"cancelled after the clear", func(t *testing.T, data string) error {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			o := reindexOpts{searchProgress: func(p search.RebuildProgress) {
				if p.Done == 1 {
					cancel()
				}
			}}
			var out, errOut strings.Builder
			return reindexMain(ctx, &out, &errOut, []string{"--root", data, "--search"}, o)
		}},
		{"a database failure at the commit", func(t *testing.T, data string) error {
			withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER disk_full BEFORE INSERT ON search_meta
					BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
				return err
			})
			_, _, err := reindexRun(t, reindexOpts{}, "--root", data, "--search")
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				t.Fatalf("want the database's failure, got %v", err)
			}
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := searchRedactWorld(t)
			withDB(t, w.data, func(q *dbq.Queries, _ *sql.Tx) error {
				return q.SetSearchVersion(t.Context(), search.Version-1)
			})
			wantRows, wantResults := searchAt(t, w.data)
			if !strings.Contains(wantRows, fmt.Sprintf("meta 1 %d", search.Version-1)) {
				t.Fatalf("the old version is not recorded:\n%s", wantRows)
			}

			err := c.run(t, w.data)
			if code := exitCodeFor(err); code != exitInternal {
				t.Fatalf("exit %d (%v), want %d", code, err, exitInternal)
			}
			if !strings.Contains(err.Error(), "the search index was not changed") {
				t.Errorf("the error does not say the index was not changed: %v", err)
			}
			rows, results := searchAt(t, w.data)
			if rows != wantRows || results != wantResults {
				t.Errorf("an interrupted rebuild changed the index:\n%s\n%s\n---\n%s\n%s", wantRows, wantResults, rows, results)
			}
			if err := checkSearchIndex(t, w.data); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestReindexSearchOffline is criterion 3: refused while serve holds the
// data directory, changing nothing, and run with serve stopped from the data
// directory alone.
func TestReindexSearchOffline(t *testing.T) {
	_, data, ids := searchWorld(t, 3)
	withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error { return db.ClearSearch(t.Context(), tx) })
	before, _ := searchAt(t, data)

	lk, err := lock.Acquire(data, "serve")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = reindexRun(t, reindexOpts{}, "--root", data, "--search")
	if err == nil {
		lk.Release()
		t.Fatal("reindex --search ran while serve held the data directory")
	}
	if code := exitCodeFor(err); code != exitInternal || !strings.Contains(err.Error(), "stop ketju serve") {
		t.Errorf("exit %d: %v", code, err)
	}
	if after, _ := searchAt(t, data); after != before {
		t.Errorf("a refused reindex changed the index:\n%s\n---\n%s", before, after)
	}
	if err := lk.Release(); err != nil {
		t.Fatal(err)
	}

	sum := reindexSearchJSON(t, data, "--search")
	if sum.Search == nil || sum.Search.Indexed != len(ids) || sum.Search.Posts != len(ids) {
		t.Fatalf("search report %+v", sum.Search)
	}
	if n := len(searchFor(t, data, "hello")); n != len(ids) {
		t.Errorf("after the rebuild, hello finds %d posts, want %d", n, len(ids))
	}
}

// TestReindexFlags is criterion 4: the five combinations of docs/03, as the
// plan's table gives them.
func TestReindexFlags(t *testing.T) {
	for _, c := range []struct {
		args              []string
		projections, srch bool
		usage             bool
	}{
		{nil, true, false, false},
		{[]string{"--search"}, false, true, false},
		{[]string{"--search", "--projections"}, true, true, false},
		{[]string{"--search", "--projections=false"}, false, true, false},
		{[]string{"--projections=false"}, false, false, true},
	} {
		t.Run(strings.Join(append([]string{"reindex"}, c.args...), " "), func(t *testing.T) {
			data, _ := populate(t, 2)
			catchUp(t, data)
			// Stale rows in both, so a rebuild of either shows.
			withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error {
				if _, err := tx.ExecContext(t.Context(), `UPDATE threads SET title = 'stale'`); err != nil {
					return err
				}
				return db.PutSearchDoc(t.Context(), tx, db.SearchDoc{Thread: 1, Ordinal: 1, Revision: 1, SourcePlain: "stalesearch"})
			})
			out, errOut, err := reindexRun(t, reindexOpts{}, append([]string{"--root", data, "--json"}, c.args...)...)
			if c.usage {
				if exitCodeFor(err) != exitUsage {
					t.Fatalf("exit %d (%v), want %d", exitCodeFor(err), err, exitUsage)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v\n%s", err, errOut)
			}
			var sum reindexSummary
			if err := json.Unmarshal([]byte(out), &sum); err != nil {
				t.Fatal(err)
			}
			proj, _ := dumpAt(t, data)
			rows, _ := searchAt(t, data)
			if got := !strings.Contains(proj, "stale"); got != c.projections {
				t.Errorf("projections rebuilt = %v, want %v", got, c.projections)
			}
			if got := sum.Projected == 2; got != c.projections {
				t.Errorf("report projected %d threads", sum.Projected)
			}
			if got := !strings.Contains(rows, "stalesearch"); got != c.srch {
				t.Errorf("search rebuilt = %v, want %v", got, c.srch)
			}
			if got := sum.Search != nil; got != c.srch {
				t.Errorf("report search = %+v", sum.Search)
			}
			if c.srch && !c.projections && (sum.Threads != 0 || sum.Members != 0 || sum.MaxMemberID != 0) {
				t.Errorf("a search-only run filled the projection fields: %+v", sum)
			}
			if c.projections && c.srch && strings.Index(errOut, "reindex --search:") < strings.Index(errOut, "reindex: ") {
				t.Errorf("the search index was rebuilt before the projections:\n%s", errOut)
			}
		})
	}
}

// TestReindexProjectionsKeepsSearch is criterion 4's last clause: a
// projections-only run leaves the search rows exactly as they were.
func TestReindexProjectionsKeepsSearch(t *testing.T) {
	data, _ := populate(t, 3)
	catchUp(t, data)
	before, results := searchAt(t, data)
	if !strings.Contains(before, "doc ") {
		t.Fatalf("the setup indexed nothing:\n%s", before)
	}
	sum := reindexSearchJSON(t, data)
	if sum.Projected != 3 || sum.Search != nil {
		t.Fatalf("report %+v", sum)
	}
	if after, r := searchAt(t, data); after != before || r != results {
		t.Errorf("a projections-only reindex changed the search index:\n%s\n---\n%s", before, after)
	}
}

// TestReindexSearchDamagedThread is criterion 6: a thread damaged before its
// last frame, after its posts were indexed, is reported with exit 1 and left
// with no search rows and no search_state, while the other threads are
// indexed.
func TestReindexSearchDamagedThread(t *testing.T) {
	data, ids := populate(t, 3)
	catchUp(t, data)
	bad := ids[1]
	if rows, _ := searchAt(t, data); !strings.Contains(rows, fmt.Sprintf("doc %d ", int64(bad))) {
		t.Fatalf("the thread to damage had no row before:\n%s", rows)
	}
	b, err := os.ReadFile(bad.LogPath(data))
	if err != nil {
		t.Fatal(err)
	}
	b[threadlog.FileHeaderSize+40] ^= 0xff // inside the first frame
	if err := os.WriteFile(bad.LogPath(data), b, 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := reindexRun(t, reindexOpts{}, "--root", data, "--search")
	if exitCodeFor(err) != exitCorrupt {
		t.Fatalf("exit %d (%v), want %d:\n%s%s", exitCodeFor(err), err, exitCorrupt, out, errOut)
	}
	for _, want := range []string{"search: FAILED   " + bad.String(), "search: indexed 2 of 3 thread(s)",
		"search: 1 thread(s) FAILED and have no search rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	rows, _ := searchAt(t, data)
	if strings.Contains(rows, fmt.Sprintf(" %d ", int64(bad))) {
		t.Errorf("the damaged thread kept rows or a search_state:\n%s", rows)
	}
	for _, id := range []threadid.ID{ids[0], ids[2]} {
		if !strings.Contains(rows, fmt.Sprintf("state %d ", int64(id))) {
			t.Errorf("thread %s was not indexed beside the damaged one:\n%s", id, rows)
		}
	}
	if err := checkSearchIndex(t, data); err != nil {
		t.Error(err)
	}
}

// TestReindexSearchProgress is criterion 2's first half: progress on stderr,
// spaced as the projection reindex spaces it, and the report on stdout, in
// text and in JSON.
func TestReindexSearchProgress(t *testing.T) {
	data, _ := populate(t, 30)
	out, errOut, err := reindexRun(t, reindexOpts{every: 2 * time.Second, now: fakeClock()}, "--root", data, "--search")
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if !strings.HasPrefix(lines[0], "reindex --search: 30 thread(s), ") {
		t.Errorf("no opening line naming the work:\n%s", errOut)
	}
	progress := regexp.MustCompile(`^reindex --search: (\d+)/30 threads, [\d.]+ [KMG]?i?B of [\d.]+ [KMG]?i?B`)
	seen := 0
	for _, l := range lines[1:] {
		if progress.MatchString(l) {
			seen++
		}
	}
	if seen < 10 || seen > 16 {
		t.Errorf("%d progress lines for 30 threads read a second apart, every 2 s:\n%s", seen, errOut)
	}
	if !regexp.MustCompile(`(?m)^search: indexed 30 of 30 thread\(s\), \d+ post\(s\), in `).MatchString(out) ||
		strings.Contains(out, "/30 threads") || strings.Contains(out, "reindexed ") {
		t.Errorf("stdout has no search summary, has progress, or reports projections:\n%s", out)
	}

	sum := reindexSearchJSON(t, data, "--search")
	s := sum.Search
	if s == nil || s.Threads != 30 || s.Indexed != 30 || s.Posts == 0 || s.Torn == nil || s.Failed == nil {
		t.Fatalf("search report %+v", s)
	}
	var raw map[string]json.RawMessage
	jout, _, _ := reindexRun(t, reindexOpts{}, "--root", data, "--json")
	if err := json.Unmarshal([]byte(jout), &raw); err != nil || string(raw["search"]) != "null" {
		t.Errorf("a projections-only report's search is %s, want null (%v)", raw["search"], err)
	}
}

// TestReindexSearchMissingTable is step 4 of the plan: a search table dropped
// by hand, with its migration recorded, exits 4 naming it, and nothing is
// recreated.
func TestReindexSearchMissingTable(t *testing.T) {
	data, _ := populate(t, 1)
	withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `DROP TABLE search_state`)
		return err
	})
	_, _, err := reindexRun(t, reindexOpts{}, "--root", data, "--search")
	if exitCodeFor(err) != exitInternal || !strings.Contains(err.Error(), "search_state") ||
		!strings.Contains(err.Error(), "backup") {
		t.Fatalf("exit %d: %v", exitCodeFor(err), err)
	}
	var missing []string
	withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		missing, err = db.MissingSearchTables(t.Context(), tx)
		return err
	})
	if len(missing) != 1 || missing[0] != "search_state" {
		t.Errorf("missing tables after the run: %v", missing)
	}
	if _, _, err := reindexRun(t, reindexOpts{}, "--root", data); err != nil {
		t.Errorf("a projections-only run over the same database: %v", err)
	}
}
