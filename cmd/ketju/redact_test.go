package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// redactWorld is a data directory for ketju redact: a database with an admin
// (root), a suspended admin (gone), a member (alice) and a moderator (mod),
// threads written through a thread cache and projected, and blobs.
type redactWorld struct {
	t     *testing.T
	data  string
	cache *threadcache.Cache
	clock int64
	ids   []threadid.ID
	users map[string]int64
}

func newRedactWorld(t *testing.T) *redactWorld {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(data, threadid.ThreadsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	d := openDB(t, data)
	w := &redactWorld{t: t, data: data, clock: 1_760_000_000_000, users: map[string]int64{}}
	for _, u := range []struct{ handle, role, status string }{
		{"root", "admin", "active"}, {"gone", "admin", "suspended"}, {"alice", "member", "active"}, {"mod", "moderator", "active"},
	} {
		if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: u.status, Role: u.role, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			w.users[u.handle] = id
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	d.Close()
	c, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	w.cache = c
	t.Cleanup(func() { w.closeCache() })
	return w
}

func (w *redactWorld) closeCache() {
	if w.cache != nil {
		if err := w.cache.Close(); err != nil {
			w.t.Error(err)
		}
		w.cache = nil
	}
}

func (w *redactWorld) append(id threadid.ID, a threadlog.Append) threadlog.Result {
	w.t.Helper()
	w.clock += 1000
	a.TimeMS = w.clock
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	}); err != nil {
		w.t.Fatalf("appending %s: %v", a.Type, err)
	}
	return res
}

func (w *redactWorld) thread(title string) threadid.ID {
	w.t.Helper()
	id := threadid.ID(uint64(w.clock)<<16 | uint64(len(w.ids)+1))
	th, err := w.cache.Create(id, 1, w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["alice"]),
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-")), ForumID: 1}})
	w.ids = append(w.ids, id)
	return id
}

func (w *redactWorld) post(id threadid.ID, source string, attachments ...string) uint32 {
	w.t.Helper()
	return w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
		Body: &threadlog.PostBody{Source: source, Attachments: attachments}}).Ordinal
}

// copyOf writes a post in id copied from from, as a split or merge would.
func (w *redactWorld) copyOf(id threadid.ID, from postRef, source string) uint32 {
	w.t.Helper()
	return w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
		Body: &threadlog.PostBody{Source: source, Origin: &threadlog.PostOrigin{ThreadID: uint64(from.Thread), Ordinal: from.Ordinal}}}).Ordinal
}

func (w *redactWorld) edit(id threadid.ID, ordinal uint32, body any) {
	w.t.Helper()
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: ordinal, Body: body})
}

// blob stores a blob and gives it refs references, and returns its hash.
func (w *redactWorld) blob(content string, refs int) string {
	w.t.Helper()
	sum := sha256.Sum256([]byte(content))
	h := hex.EncodeToString(sum[:])
	path, err := blob.Path(w.data, h)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		w.t.Fatal(err)
	}
	d := openDB(w.t, w.data)
	defer d.Close()
	if err := d.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		if err := q.CreateBlob(w.t.Context(), dbq.CreateBlobParams{Hash: h, Size: int64(len(content)), Mime: "text/plain", UploadedAt: 1}); err != nil {
			return err
		}
		for range refs {
			if _, err := q.AddBlobRef(w.t.Context(), h); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return h
}

// ready closes the cache, as a stopped serve leaves it, and projects every
// thread.
func (w *redactWorld) ready() {
	w.t.Helper()
	w.closeCache()
	d := openDB(w.t, w.data)
	defer d.Close()
	pw, err := projection.New(projection.Options{DB: d, Root: w.data, Logger: slog.New(slog.DiscardHandler),
		Sweep: func(context.Context) error { return nil }})
	if err != nil {
		w.t.Fatal(err)
	}
	for _, id := range w.ids {
		fi, err := os.Stat(id.LogPath(w.data))
		if err != nil {
			w.t.Fatal(err)
		}
		if _, err := pw.ProjectThread(w.t.Context(), id, fi.Size()); err != nil {
			w.t.Fatal(err)
		}
	}
}

// redact runs the command. failAt names a step at which the run stops.
func (w *redactWorld) redact(failAt string, args ...string) (string, error) {
	w.t.Helper()
	var out, errOut bytes.Buffer
	o := redactOpts{now: func() time.Time { w.clock += 1000; return time.UnixMilli(w.clock) }}
	if failAt != "" {
		o.fail = func(step string) error {
			if step == failAt {
				return fmt.Errorf("injected failure at %s", step)
			}
			return nil
		}
	}
	err := redactMain(w.t.Context(), &out, &errOut, append([]string{"--root", w.data}, args...), o)
	return out.String() + errOut.String(), err
}

func (w *redactWorld) mustRedact(args ...string) string {
	w.t.Helper()
	out, err := w.redact("", args...)
	if err != nil {
		w.t.Fatalf("redact %v: %v\n%s", args, err, out)
	}
	return out
}

func postArgs(id threadid.ID, ordinal uint32) []string {
	return []string{"--operator", "root", "--reason", "court order 17", id.String(), fmt.Sprint(ordinal)}
}

func (w *redactWorld) log(id threadid.ID) []byte {
	w.t.Helper()
	b, err := os.ReadFile(id.LogPath(w.data))
	if err != nil {
		w.t.Fatal(err)
	}
	return b
}

func (w *redactWorld) markers(id threadid.ID) []threadlog.RedactedBody {
	w.t.Helper()
	b := w.log(id)
	m, err := threadlog.Markers(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

func (w *redactWorld) journal() bool {
	_, err := os.Stat(journalPath(w.data))
	return err == nil
}

// query runs a one-value query against the database.
func (w *redactWorld) query(dst any, q string, args ...any) {
	w.t.Helper()
	d := openDB(w.t, w.data)
	defer d.Close()
	if err := d.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), q, args...).Scan(dst)
	}); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

func (w *redactWorld) count(q string, args ...any) int {
	w.t.Helper()
	var n int
	w.query(&n, q, args...)
	return n
}

func (w *redactWorld) refcount(h string) int {
	return w.count(`SELECT coalesce((SELECT refcount FROM blobs WHERE hash = ?), -1)`, h)
}

func (w *redactWorld) blobFile(h string) bool {
	path, _ := blob.Path(w.data, h)
	_, err := os.Stat(path)
	return err == nil
}

func (w *redactWorld) auditDetails(action string) []map[string]any {
	w.t.Helper()
	d := openDB(w.t, w.data)
	defer d.Close()
	var out []map[string]any
	if err := d.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT details_json FROM audit_log WHERE action = ? ORDER BY id`, action)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(s), &m); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// entry opens the thread as serve would and returns ordinal's index entry.
func (w *redactWorld) entry(id threadid.ID, ordinal uint32) threadlog.IndexEntry {
	w.t.Helper()
	c, err := threadcache.New(w.data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	defer c.Close()
	var e threadlog.IndexEntry
	if err := c.With(id, func(th *threadcache.Thread) error {
		var err error
		if e, err = th.Index.Entry(ordinal); err != nil {
			return err
		}
		p, err := th.Reader.Hidden(ordinal)
		if err == nil && e.Flags.Has(threadlog.EntryRedacted) && (!p.Tombstone || p.Source != "") {
			w.t.Errorf("a redacted post reads back as %+v", p)
		}
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return e
}

// hashes is every file under the data directory but the lock record and
// the database's WAL, with its SHA-256.
func (w *redactWorld) hashes() map[string]string {
	w.t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(w.data, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if base := filepath.Base(p); base == lock.FileName || strings.HasSuffix(base, "-wal") || strings.HasSuffix(base, "-shm") {
			return nil
		}
		b, err := os.ReadFile(p)
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestCopySetIsTransitive: the copy set is the whole origin component, both
// ways, to a fixed point.
func TestCopySetIsTransitive(t *testing.T) {
	p := func(th, o int) postRef { return postRef{Thread: threadid.ID(th), Ordinal: uint32(o)} }
	a, b, c, d, e, x := p(1, 1), p(2, 1), p(3, 1), p(4, 1), p(5, 1), p(9, 9)
	for _, tc := range []struct {
		name  string
		edges map[postRef]postRef
		want  []postRef
	}{
		{"a chain a <- b <- c", map[postRef]postRef{b: a, c: b}, []postRef{a, b, c}},
		{"two copies of one copy", map[postRef]postRef{b: a, c: b, d: b}, []postRef{a, b, c, d}},
		{"a diamond from merges", map[postRef]postRef{b: a, c: a, d: b, e: c}, []postRef{a, b, c, d, e}},
	} {
		for _, named := range tc.want {
			if got := copySet(tc.edges, named); !slices.Equal(got, tc.want) {
				t.Errorf("%s, naming %v: %v, want %v", tc.name, named, got, tc.want)
			}
		}
		if got := copySet(tc.edges, x); !slices.Equal(got, []postRef{x}) {
			t.Errorf("%s: an unrelated post's set is %v", tc.name, got)
		}
	}
}

// TestRedactBlanksAndMarks is the whole run once: the post's every revision
// is blank, one marker is appended, the index says redacted, the projection
// says deleted, one audit row, no journal.
func TestRedactBlanksAndMarks(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	w.post(id, "keep me")
	n := w.post(id, "REDACT-ME-1")
	w.edit(id, n, &threadlog.EditBody{Source: "REDACT-ME-2", Reason: "REDACT-REASON"})
	w.ready()
	before := len(w.log(id))

	out := w.mustRedact(postArgs(id, n)...)
	b := w.log(id)
	for _, s := range []string{"REDACT-ME-1", "REDACT-ME-2", "REDACT-REASON"} {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("%q survived", s)
		}
	}
	if !bytes.Contains(b, []byte("keep me")) || len(b) <= before {
		t.Errorf("the log lost another post or shrank: %d, was %d", len(b), before)
	}
	if m := w.markers(id); len(m) != 1 || m[0].Ordinal != n || !slices.Equal(m[0].Revisions, []uint32{1, 2}) ||
		m[0].Operator != uint64(w.users["root"]) || m[0].Reason != "court order 17" || m[0].Journal == "" {
		t.Errorf("markers %+v", m)
	}
	if e := w.entry(id, n); !e.Flags.Has(threadlog.EntryRedacted) {
		t.Errorf("entry flags %#x", e.Flags)
	}
	if got := w.count(`SELECT deleted FROM post_meta WHERE thread_id = ? AND ordinal = ?`, int64(id), n); got != 1 {
		t.Errorf("post_meta.deleted = %d", got)
	}
	if got := w.count(`SELECT log_length FROM projection_state WHERE thread_id = ?`, int64(id)); got != len(b) {
		t.Errorf("projected to %d, log %d", got, len(b))
	}
	rows := w.auditDetails("post.redacted")
	if len(rows) != 1 || rows[0]["reason"] != "court order 17" || rows[0]["via"] != "cli" {
		t.Errorf("post.redacted rows %v", rows)
	}
	if w.journal() {
		t.Error("the journal is left")
	}
	if !strings.Contains(out, "complete") || !strings.Contains(out, "snapshots") {
		t.Errorf("output:\n%s", out)
	}
}

// TestRedactRefusesBesideServe is AC2's first half: the command takes the
// data directory's lock, and refuses while serve holds it, writing nothing.
func TestRedactRefusesBesideServe(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "text")
	w.ready()
	l, err := lock.Acquire(w.data, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	before := w.hashes()
	out, err := w.redact("", postArgs(id, n)...)
	if err == nil || !strings.Contains(err.Error(), "stop ketju serve") || exitCodeFor(err) != exitInternal {
		t.Errorf("beside serve: %v (exit %d)\n%s", err, exitCodeFor(err), out)
	}
	if after := w.hashes(); !mapsEqual(before, after) || w.journal() {
		t.Error("a refused run wrote something")
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestServeRefusesWhileAJournalExists is AC2's second half.
func TestServeRefusesWhileAJournalExists(t *testing.T) {
	path, data := writeConfig(t, "")
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath(data), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	var steps []string
	err := serve(t.Context(), path, serveOpts{logOut: io.Discard, step: func(s string) { steps = append(steps, s) }})
	if err == nil || !strings.Contains(err.Error(), "a hard redaction is pending") ||
		!strings.Contains(err.Error(), "ketju redact --root "+data+" --resume") {
		t.Fatalf("serve beside a journal: %v", err)
	}
	if slices.Contains(steps, "database") || !slices.Contains(steps, "release lock") {
		t.Errorf("steps %v: want it stopped before the database, with the lock released", steps)
	}
}

// TestRedactNeedsAnAdminOperatorAndAReason.
func TestRedactNeedsAnAdminOperatorAndAReason(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "text")
	w.ready()
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--operator", "alice", "--reason", "r", id.String(), fmt.Sprint(n)}, exitUsage, "not an active admin"},
		{[]string{"--operator", "mod", "--reason", "r", id.String(), fmt.Sprint(n)}, exitUsage, "not an active admin"},
		{[]string{"--operator", "gone", "--reason", "r", id.String(), fmt.Sprint(n)}, exitUsage, "not an active admin"},
		{[]string{"--operator", "nobody", "--reason", "r", id.String(), fmt.Sprint(n)}, exitNotFound, "no member"},
		{[]string{"--operator", "root", id.String(), fmt.Sprint(n)}, exitUsage, "--reason"},
		{[]string{"--operator", "root", "--reason", "  ", id.String(), fmt.Sprint(n)}, exitUsage, "--reason"},
		{[]string{"--reason", "r", id.String(), fmt.Sprint(n)}, exitUsage, "--operator"},
		{[]string{"--operator", "root", "--reason", "r", id.String(), "99"}, exitNotFound, "no such post"},
		{[]string{"--operator", "root", "--reason", "r", threadid.ID(12345).String(), "1"}, exitNotFound, "no thread"},
	} {
		out, err := w.redact("", c.args...)
		if exitCodeFor(err) != c.code || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: exit %d, %v; want %d and %q\n%s", c.args, exitCodeFor(err), err, c.code, c.want, out)
		}
		if w.journal() {
			t.Fatalf("%v wrote a journal", c.args)
		}
	}
	if m := w.markers(id); len(m) != 0 {
		t.Errorf("a refused run marked the log: %v", m)
	}
}

// TestRedactUsage is AC12: two forms, and every other combination exits 2.
func TestRedactUsage(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "text")
	other := w.post(id, "other")
	w.ready()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--resume", "--operator", "root"}, "--resume takes only"},
		{[]string{"--resume", "--reason", "r"}, "--resume takes only"},
		{[]string{"--resume", "--title", "t"}, "--resume takes only"},
		{[]string{"--resume", id.String(), "1"}, "--resume takes only"},
		{[]string{"--resume"}, "nothing to resume"},
		{[]string{"--operator", "root", "--reason", "r", id.String()}, "name a THREAD"},
		{[]string{"--operator", "root", "--reason", "r"}, "name a THREAD"},
		{[]string{"--operator", "root", "--reason", "r", "not-an-id", "1"}, "not a thread id"},
		{[]string{"--operator", "root", "--reason", "r", id.String(), "0"}, "not a post ordinal"},
		{[]string{"--operator", "root", "--reason", "r", id.String(), "x"}, "not a post ordinal"},
		{[]string{"--bogus"}, "flag provided but not defined"},
	} {
		out, err := w.redact("", c.args...)
		if exitCodeFor(err) != exitUsage || !strings.Contains(err.Error()+out, c.want) {
			t.Errorf("%v: exit %d, %v; want 2 and %q", c.args, exitCodeFor(err), err, c.want)
		}
	}
	out, err := w.redact("", "-h")
	if exitCodeFor(err) != exitUsage {
		t.Errorf("-h: %v", err)
	}
	for _, form := range []string{
		"ketju redact --root DIR --operator HANDLE --reason TEXT [--title TEXT] [--dry-run] THREAD ORDINAL|title",
		"ketju redact --root DIR --resume [--dry-run]",
	} {
		if !strings.Contains(out, form) {
			t.Errorf("the usage text lacks %q:\n%s", form, out)
		}
	}

	// A pending redaction: a different post is refused, naming the pending
	// one and --resume; --resume alone finishes it.
	if _, err := w.redact("journal", postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	out, err = w.redact("", postArgs(id, other)...)
	if exitCodeFor(err) != exitUsage || !strings.Contains(err.Error(), "--resume") || !strings.Contains(err.Error(), fmt.Sprintf("%s/%d", id, n)) {
		t.Errorf("a different post while one is pending: %v\n%s", err, out)
	}
	if out, err := w.redact("", "--resume"); err != nil || w.journal() {
		t.Fatalf("--resume: %v\n%s", err, out)
	}
	if e := w.entry(id, n); !e.Flags.Has(threadlog.EntryRedacted) {
		t.Error("--resume did not finish the redaction")
	}
}

// TestRedactRefusesASecondRedaction: with no journal pending, a redacted
// post is refused.
func TestRedactRefusesASecondRedaction(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "text")
	w.ready()
	w.mustRedact(postArgs(id, n)...)
	before := w.hashes()
	if _, err := w.redact("", postArgs(id, n)...); exitCodeFor(err) != exitUsage || !strings.Contains(err.Error(), "already redacted") {
		t.Errorf("a second redaction: %v", err)
	}
	if !mapsEqual(before, w.hashes()) {
		t.Error("the refused second run changed the data directory")
	}
}

// TestRedactResumeRightAfterTheSwap is AC10: a rerun naming the same post
// right after its log was swapped resumes rather than being refused as
// already redacted.
func TestRedactResumeRightAfterTheSwap(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "text")
	w.ready()
	if _, err := w.redact("renamed:"+id.String(), postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	if len(w.markers(id)) != 1 || !w.journal() {
		t.Fatal("the failure was not right after the swap")
	}
	if out, err := w.redact("", postArgs(id, n)...); err != nil {
		t.Fatalf("the rerun: %v\n%s", err, out)
	}
	if len(w.markers(id)) != 1 || w.journal() || len(w.auditDetails("post.redacted")) != 1 {
		t.Error("the rerun did not finish exactly once")
	}
}

// TestRedactFollowsOriginBothWays is AC4's copies: naming a copy clears its
// source, and naming a source clears every copy, in every log.
func TestRedactFollowsOriginBothWays(t *testing.T) {
	for _, name := range []string{"copy", "source"} {
		t.Run(name, func(t *testing.T) {
			w := newRedactWorld(t)
			a, b, c := w.thread("Source"), w.thread("Split"), w.thread("Merged")
			src := w.post(a, "ORIGIN-SECRET")
			cp := w.copyOf(b, postRef{a, src}, "ORIGIN-SECRET")
			cc := w.copyOf(c, postRef{b, cp}, "ORIGIN-SECRET")
			w.post(c, "unrelated")
			w.ready()
			named := postRef{a, src}
			if name == "copy" {
				named = postRef{c, cc}
			}
			w.mustRedact(postArgs(named.Thread, named.Ordinal)...)
			for _, id := range []threadid.ID{a, b, c} {
				if bytes.Contains(w.log(id), []byte("ORIGIN-SECRET")) {
					t.Errorf("thread %s still holds the text", id)
				}
				if len(w.markers(id)) != 1 {
					t.Errorf("thread %s has markers %v", id, w.markers(id))
				}
			}
			if !bytes.Contains(w.log(c), []byte("unrelated")) {
				t.Error("an unrelated post was cleared")
			}
			rows := w.auditDetails("post.redacted")
			if len(rows) != 1 || len(rows[0]["targets"].([]any)) != 3 {
				t.Errorf("post.redacted rows %v", rows)
			}
		})
	}
}

// TestRedactTwoOrdinalsInOneLog: a copy in the same thread makes two
// targets in one log, which is rewritten once with both markers; a log with
// only one of them is damage.
func TestRedactTwoOrdinalsInOneLog(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "TWICE-SECRET")
	cp := w.copyOf(id, postRef{id, n}, "TWICE-SECRET")
	w.ready()
	before := len(w.log(id))
	if _, err := w.redact("synced:"+id.String(), postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	m := w.markers(id)
	if len(m) != 2 || m[0].Ordinal != n || m[1].Ordinal != cp {
		t.Fatalf("markers %v", m)
	}
	swappedOnce := len(w.log(id))
	if _, err := w.redact("", "--resume"); err != nil {
		t.Fatal(err)
	}
	if len(w.log(id)) != swappedOnce || len(w.markers(id)) != 2 || before >= swappedOnce {
		t.Errorf("the resume rewrote the log again: %d, was %d", len(w.log(id)), swappedOnce)
	}

	// Damage: a pending journal naming both, over a log carrying one marker.
	w2 := newRedactWorld(t)
	id2 := w2.thread("A thread")
	n2 := w2.post(id2, "TWICE-SECRET")
	w2.copyOf(id2, postRef{id2, n2}, "TWICE-SECRET")
	w2.ready()
	if _, err := w2.redact("journal", postArgs(id2, n2)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	old := w2.log(id2)
	var one bytes.Buffer
	if _, err := threadlog.RedactPosts(bytes.NewReader(old), int64(len(old)), &one, []uint32{n2},
		threadlog.RedactOptions{TimeMS: 1, Operator: 1, Reason: "x", Journal: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id2.LogPath(w2.data), one.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := w2.redact("", "--resume"); exitCodeFor(err) != exitCorrupt || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("one of two markers: %v", err)
	}
	if !w2.journal() {
		t.Error("the journal was removed over a damaged log")
	}
}

// TestRedactListsQuotes: a quote of the post in another post is listed and
// left as it is.
func TestRedactListsQuotes(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A thread")
	n := w.post(id, "QUOTED-SECRET")
	q := w.post(id, fmt.Sprintf("> [quote=#%d]\n> QUOTED-SECRET\n\nindeed\n", n))
	w.ready()
	out := w.mustRedact(postArgs(id, n)...)
	if !strings.Contains(out, fmt.Sprintf("%s/%d revision 1 quotes #%d", id, q, n)) {
		t.Errorf("the quote is not listed:\n%s", out)
	}
	if bytes.Count(w.log(id), []byte("QUOTED-SECRET")) != 1 {
		t.Error("the quoting post was changed, or the quoted one was not")
	}
	if rows := w.auditDetails("post.redacted"); len(rows[0]["quotes"].([]any)) != 1 {
		t.Errorf("the audit row's quotes: %v", rows[0]["quotes"])
	}
}

// TestRedactRemovesBlobUploads: deleting a blob's row takes its upload
// rows with it, in the same transaction (TKT-01M29CXK0K), and a blob that
// stays keeps its own.
func TestRedactRemovesBlobUploads(t *testing.T) {
	w := newRedactWorld(t)
	only, shared := w.blob("only mine", 1), w.blob("shared", 2)
	d := openDB(t, w.data)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		for _, h := range []string{only, shared} {
			if err := q.InsertBlobUpload(t.Context(), dbq.InsertBlobUploadParams{Hash: h, UserID: w.users["alice"],
				OriginalName: "f.txt", UploadedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d.Close()
	id := w.thread("A thread")
	n := w.post(id, "with files", only, shared)
	w.post(id, "also shared", shared)
	w.ready()
	w.mustRedact(postArgs(id, n)...)
	if got := w.count(`SELECT count(*) FROM blob_uploads WHERE hash = ?`, only); got != 0 {
		t.Errorf("the deleted blob keeps %d upload rows", got)
	}
	if got := w.count(`SELECT count(*) FROM blob_uploads WHERE hash = ?`, shared); got != 1 {
		t.Errorf("the kept blob has %d upload rows", got)
	}
}

// TestRedactDeletesOnlyUnsharedBlobs and TestRedactAuditsOnlyCommittedDeletes:
// a blob only the post named goes, row, file and all, with a blob.deleted
// row; one another post names stays, one reference fewer; the post.redacted
// row lists both as planned.
func TestRedactDeletesOnlyUnsharedBlobs(t *testing.T) {
	w := newRedactWorld(t)
	only, shared := w.blob("only mine", 1), w.blob("shared", 2)
	id := w.thread("A thread")
	n := w.post(id, "with files", only, shared)
	w.post(id, "also shared", shared)
	w.ready()
	w.mustRedact(postArgs(id, n)...)
	if w.refcount(only) != -1 || w.blobFile(only) {
		t.Errorf("the unshared blob stays: refcount %d, file %v", w.refcount(only), w.blobFile(only))
	}
	if w.refcount(shared) != 1 || !w.blobFile(shared) {
		t.Errorf("the shared blob: refcount %d, file %v", w.refcount(shared), w.blobFile(shared))
	}
	rows := w.auditDetails("post.redacted")
	if len(rows) != 1 || len(rows[0]["blobs_planned"].([]any)) != 2 || len(rows[0]["references_dropped"].([]any)) != 2 {
		t.Errorf("post.redacted %v", rows)
	}
	if del := w.auditDetails("blob.deleted"); len(del) != 1 {
		t.Errorf("blob.deleted rows %v", del)
	}
	if got := w.count(`SELECT count(*) FROM audit_log WHERE action = 'blob.deleted' AND target_type = 'blob' AND target_id = ?`, only); got != 1 {
		t.Errorf("the deleted blob has %d rows", got)
	}
}

// TestRedactKeepsDraftAttachedBlob is TKT-01M3V73DP6's AC4: a redacted
// post's only blob is kept, file and row, while its author's draft still
// attaches it, and redact says why. The reference the post held is dropped
// all the same, and no blob.deleted row is written.
func TestRedactKeepsDraftAttachedBlob(t *testing.T) {
	w := newRedactWorld(t)
	h := w.blob("posted, then drafted again", 1)
	id := w.thread("A thread")
	n := w.post(id, "with a file", h)
	w.ready()
	d := openDB(t, w.data)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.UpsertThreadDraft(t.Context(), dbq.UpsertThreadDraftParams{UserID: w.users["alice"],
			ThreadID: sql.NullInt64{Int64: int64(id), Valid: true}, Source: "trying again",
			Attachments: `["` + h + `"]`, Now: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	d.Close()

	out := w.mustRedact(postArgs(id, n)...)
	if want := "kept blob " + h + ": another post, an avatar or a draft still refers to it"; !strings.Contains(out, want) {
		t.Errorf("redact said:\n%s\nwant a line %q", out, want)
	}
	if w.refcount(h) != 0 || !w.blobFile(h) {
		t.Errorf("the draft's blob: refcount %d, file %v; want 0 and kept", w.refcount(h), w.blobFile(h))
	}
	if del := w.auditDetails("blob.deleted"); len(del) != 0 {
		t.Errorf("blob.deleted rows %v", del)
	}
}

// TestRedactAuditsOnlyCommittedDeletes and TestRedactBlobDeleteFailureKeepsTheFile:
// a failure inside the blob's transaction leaves its row, its file where it
// was, no .gc and no blob.deleted row; the rerun deletes it once.
func TestRedactBlobDeleteFailureKeepsTheFile(t *testing.T) {
	w := newRedactWorld(t)
	h := w.blob("only mine", 1)
	id := w.thread("A thread")
	n := w.post(id, "with a file", h)
	w.ready()
	if _, err := w.redact("blob-tx:"+h, postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	path, _ := blob.Path(w.data, h)
	if _, err := os.Stat(path + ".gc"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a .gc file was left")
	}
	if w.refcount(h) != 0 || !w.blobFile(h) || len(w.auditDetails("blob.deleted")) != 0 {
		t.Errorf("after a failed blob transaction: refcount %d, file %v, rows %v", w.refcount(h), w.blobFile(h), w.auditDetails("blob.deleted"))
	}
	w.mustRedact("--resume")
	if w.refcount(h) != -1 || w.blobFile(h) || len(w.auditDetails("blob.deleted")) != 1 || len(w.auditDetails("post.redacted")) != 1 {
		t.Errorf("after the rerun: refcount %d, file %v, rows %v", w.refcount(h), w.blobFile(h), w.auditDetails("blob.deleted"))
	}
}

// TestRedactAuditsOnlyCommittedDeletes is AC9: of two planned blobs, one
// another post still names; the post.redacted row lists both as planned, a
// failure before the deleting transaction commits leaves no blob.deleted
// row, and the finished run has exactly one.
func TestRedactAuditsOnlyCommittedDeletes(t *testing.T) {
	w := newRedactWorld(t)
	only, shared := w.blob("only mine", 1), w.blob("shared", 2)
	id := w.thread("A thread")
	n := w.post(id, "with files", only, shared)
	w.post(id, "also shared", shared)
	w.ready()
	if _, err := w.redact("blob-tx:"+only, postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	if rows := w.auditDetails("blob.deleted"); len(rows) != 0 {
		t.Errorf("a blob.deleted row before the delete committed: %v", rows)
	}
	rows := w.auditDetails("post.redacted")
	if len(rows) != 1 || len(rows[0]["blobs_planned"].([]any)) != 2 || rows[0]["blobs_deleted"] != nil {
		t.Fatalf("post.redacted %v", rows)
	}
	if op := w.count(`SELECT actor_id FROM audit_log WHERE action = 'post.redacted'`); op != int(w.users["root"]) {
		t.Errorf("the row's actor is %d, want the operator", op)
	}
	w.mustRedact("--resume")
	del := w.count(`SELECT count(*) FROM audit_log WHERE action = 'blob.deleted'`)
	if del != 1 || w.count(`SELECT count(*) FROM audit_log WHERE action = 'blob.deleted' AND target_id = ?`, only) != 1 {
		t.Errorf("%d blob.deleted rows, want one for the unshared blob", del)
	}
}

// TestRedactResumeRemovesAFileWhoseRowIsGone: a crash after the blob's
// transaction commits and before its file goes; the rerun removes the file
// and writes no second row.
func TestRedactResumeRemovesAFileWhoseRowIsGone(t *testing.T) {
	w := newRedactWorld(t)
	h := w.blob("only mine", 1)
	id := w.thread("A thread")
	n := w.post(id, "with a file", h)
	w.ready()
	if _, err := w.redact("blob-committed:"+h, postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	if w.refcount(h) != -1 || !w.blobFile(h) {
		t.Fatalf("not the crash wanted: refcount %d, file %v", w.refcount(h), w.blobFile(h))
	}
	w.mustRedact("--resume")
	if w.blobFile(h) || len(w.auditDetails("blob.deleted")) != 1 {
		t.Errorf("after the rerun: file %v, rows %v", w.blobFile(h), w.auditDetails("blob.deleted"))
	}
}

// TestRedactResumeDropsEachReferenceOnce is AC8: a blob three posts name,
// the redacted one among them, ends at two references and stays, whatever
// step the first run died at.
func TestRedactResumeDropsEachReferenceOnce(t *testing.T) {
	for _, step := range []string{"renamed:", "references", "committed"} {
		t.Run(step, func(t *testing.T) {
			w := newRedactWorld(t)
			h := w.blob("shared", 3)
			id := w.thread("A thread")
			n := w.post(id, "one", h)
			w.post(id, "two", h)
			w.post(id, "three", h)
			w.ready()
			fail := step
			if step == "renamed:" {
				fail += id.String()
			}
			if _, err := w.redact(fail, postArgs(id, n)...); err == nil {
				t.Fatal("the injected failure did not stop the run")
			}
			w.mustRedact("--resume")
			if w.refcount(h) != 2 || !w.blobFile(h) || len(w.auditDetails("post.redacted")) != 1 {
				t.Errorf("refcount %d, file %v, rows %d", w.refcount(h), w.blobFile(h), len(w.auditDetails("post.redacted")))
			}
		})
	}
}

// TestRedactResumeAfterSwapStillDeletesTheBlob is AC8's last clause: after
// every log is swapped the logs no longer list the blob, and the rerun
// still finds it in the journal and deletes it.
func TestRedactResumeAfterSwapStillDeletesTheBlob(t *testing.T) {
	w := newRedactWorld(t)
	h := w.blob("only mine", 1)
	id := w.thread("A thread")
	n := w.post(id, "with a file", h)
	w.ready()
	if _, err := w.redact("projected:"+id.String(), postArgs(id, n)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	if bytes.Contains(w.log(id), []byte(h)) {
		t.Fatal("the log still names the blob after the swap")
	}
	w.mustRedact("--resume")
	if w.refcount(h) != -1 || w.blobFile(h) {
		t.Errorf("refcount %d, file %v", w.refcount(h), w.blobFile(h))
	}
}

// TestAttachmentInventoryReadsEveryRevision: a hash listed by a Post and an
// Edit of it is one reference, dropped once; one only an Edit lists is found.
func TestAttachmentInventoryReadsEveryRevision(t *testing.T) {
	w := newRedactWorld(t)
	h1, h2 := w.blob("in the post", 1), w.blob("in an edit", 1)
	id := w.thread("A thread")
	n := w.post(id, "with a file", h1)
	w.edit(id, n, map[string]any{"source": "edited", "attachments": []string{h1, h2}})
	w.ready()
	w.mustRedact(postArgs(id, n)...)
	for _, h := range []string{h1, h2} {
		if w.refcount(h) != -1 || w.blobFile(h) {
			t.Errorf("%s: refcount %d, file %v", h[:8], w.refcount(h), w.blobFile(h))
		}
	}
	if rows := w.auditDetails("post.redacted"); len(rows[0]["references_dropped"].([]any)) != 2 {
		t.Errorf("references dropped %v", rows[0]["references_dropped"])
	}
}

// TestRedactResumesAfterEachStep is AC7: a run stopped after any step is
// finished by rerunning, and ends with one marker per target, one
// post.redacted row, one blob.deleted row per deleted blob, no journal, and a
// serve that starts and holds the post as a tombstone.
func TestRedactResumesAfterEachStep(t *testing.T) {
	steps := []string{"journal", "renamed:A", "synced:A", "index:A", "projected:A", "renamed:B", "projected:B",
		"references", "committed", "blob-tx:H", "blob-committed:H", "blob:H"}
	for i, step := range steps {
		t.Run(step, func(t *testing.T) {
			w := newRedactWorld(t)
			h := w.blob("only mine", 1)
			a, b := w.thread("Source"), w.thread("Split")
			n := w.post(a, "RESUME-SECRET", h)
			cp := w.copyOf(b, postRef{a, n}, "RESUME-SECRET")
			w.ready()
			fail := strings.NewReplacer("A", a.String(), "B", b.String(), "H", h).Replace(step)
			if _, err := w.redact(fail, postArgs(a, n)...); err == nil {
				t.Fatal("the injected failure did not stop the run")
			}
			if !w.journal() {
				t.Fatal("no journal was left to resume from")
			}
			// Alternate the two ways of resuming.
			args := []string{"--resume"}
			if i%2 == 1 {
				args = postArgs(a, n)
			}
			w.mustRedact(args...)
			for _, c := range []struct {
				id threadid.ID
				o  uint32
			}{{a, n}, {b, cp}} {
				if m := w.markers(c.id); len(m) != 1 || m[0].Ordinal != c.o {
					t.Errorf("%s: markers %v", c.id, m)
				}
				if bytes.Contains(w.log(c.id), []byte("RESUME-SECRET")) {
					t.Errorf("%s still holds the text", c.id)
				}
				if e := w.entry(c.id, c.o); !e.Flags.Has(threadlog.EntryRedacted) {
					t.Errorf("%s/%d is not redacted in the index", c.id, c.o)
				}
			}
			if len(w.auditDetails("post.redacted")) != 1 || len(w.auditDetails("blob.deleted")) != 1 || w.journal() || w.blobFile(h) {
				t.Errorf("rows %d/%d, journal %v, blob file %v", len(w.auditDetails("post.redacted")),
					len(w.auditDetails("blob.deleted")), w.journal(), w.blobFile(h))
			}
			if i == len(steps)-1 {
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte(fmt.Sprintf("listen = %q\ndata_dir = %q\n[storage]\nfsync = \"never\"\n",
					freePort(t), w.data)), 0o644); err != nil {
					t.Fatal(err)
				}
				r := start(t, path, serveOpts{})
				if err := r.shutdown(t); err != nil {
					t.Fatalf("serve over the redacted data directory: %v\n%s", err, r.log)
				}
			}
		})
	}
}

// TestRedactDryRunWritesNothing is AC13: in both forms, with no journal and
// with one pending at each resume point, nothing in the data directory
// changes, and a pending run's report names the work left.
func TestRedactDryRunWritesNothing(t *testing.T) {
	w := newRedactWorld(t)
	h := w.blob("only mine", 1)
	id := w.thread("A thread")
	n := w.post(id, "DRY-SECRET", h)
	w.ready()
	before := w.hashes()
	out, err := w.redact("", append([]string{"--dry-run"}, postArgs(id, n)...)...)
	if err != nil || !strings.Contains(out, "dry run: nothing was written") || !strings.Contains(out, fmt.Sprintf("%s/%d", id, n)) {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !mapsEqual(before, w.hashes()) || w.journal() {
		t.Fatal("the dry run wrote something")
	}
	if _, err := w.redact("", "--resume", "--dry-run"); exitCodeFor(err) != exitUsage {
		t.Errorf("--resume --dry-run with nothing pending: %v", err)
	}

	for _, c := range []struct{ step, log, audit, row string }{
		{"journal", "not yet rewritten", "not yet written", "row present, file present"},
		{"renamed:T", "rewritten", "not yet written", "row present, file present"},
		{"committed", "rewritten", "written", "row present, file present"},
		{"blob-committed:H", "rewritten", "written", "row gone, file present"},
	} {
		w := newRedactWorld(t)
		h := w.blob("only mine", 1)
		id := w.thread("A thread")
		n := w.post(id, "DRY-SECRET", h)
		w.ready()
		step := strings.NewReplacer("T", id.String(), "H", h).Replace(c.step)
		if _, err := w.redact(step, postArgs(id, n)...); err == nil {
			t.Fatalf("%s: the injected failure did not stop the run", c.step)
		}
		pending := w.hashes()
		for _, args := range [][]string{{"--resume", "--dry-run"}, append([]string{"--dry-run"}, postArgs(id, n)...)} {
			out, err := w.redact("", args...)
			if err != nil {
				t.Fatalf("%s %v: %v\n%s", c.step, args, err, out)
			}
			for _, want := range []string{"log " + id.String() + ": " + c.log, "audit row: " + c.audit, "blob " + h + ": " + c.row, "nothing was written"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s %v: the report lacks %q:\n%s", c.step, args, want, out)
				}
			}
			if !mapsEqual(pending, w.hashes()) {
				t.Errorf("%s %v: the dry run changed the data directory", c.step, args)
			}
		}
	}
}

// Title redaction (decided 2026-09-29, TKT-01M3NS147C).

func titleArgs(id threadid.ID, extra ...string) []string {
	return append(append([]string{"--operator", "root", "--reason", "erasure 4"}, extra...), id.String(), "title")
}

func (w *redactWorld) title(id threadid.ID) (title, slug string) {
	w.t.Helper()
	d := openDB(w.t, w.data)
	defer d.Close()
	if err := d.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT title, slug FROM threads WHERE id = ?`, int64(id)).Scan(&title, &slug)
	}); err != nil {
		w.t.Fatal(err)
	}
	return title, slug
}

// TestRedactTitleShowsTheReplacement, TestRedactTitleBlanksEveryTitleFrame
// through the command, and TestRedactTitleAuditHasNoTitle.
func TestRedactTitleShowsTheReplacement(t *testing.T) {
	for _, c := range []struct {
		extra      []string
		want, slug string
	}{
		{nil, "Title removed", "title-removed"},
		{[]string{"--title", "  A neutral title "}, "A neutral title", "a-neutral-title"},
	} {
		w := newRedactWorld(t)
		id := w.thread("Old Secret Title")
		w.post(id, "a post")
		w.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: uint64(w.users["mod"]),
			Body: &threadlog.ThreadRenamedBody{Title: "Newer Secret Title", Slug: "newer-secret-title"}})
		w.ready()
		before := len(w.log(id))
		w.mustRedact(titleArgs(id, c.extra...)...)
		b := w.log(id)
		for _, s := range []string{"Old Secret Title", "old-secret-title", "Newer Secret Title", "newer-secret-title"} {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%q survived", s)
			}
		}
		if len(b) <= before {
			t.Errorf("the log did not grow: %d, was %d", len(b), before)
		}
		if title, slug := w.title(id); title != c.want || slug != c.slug {
			t.Errorf("threads row: %q %q, want %q %q", title, slug, c.want, c.slug)
		}
		if got := w.count(`SELECT log_length FROM projection_state WHERE thread_id = ?`, int64(id)); got != len(b) {
			t.Errorf("projected to %d, log %d", got, len(b))
		}
		rows := w.auditDetails("thread.title_redacted")
		if len(rows) != 1 || rows[0]["frames_blanked"] != float64(2) || rows[0]["replacement_by_operator"] != (c.extra != nil) {
			t.Errorf("thread.title_redacted rows %v", rows)
		}
		raw, _ := json.Marshal(rows)
		for _, s := range []string{"Secret", "secret", c.want} {
			if bytes.Contains(raw, []byte(s)) {
				t.Errorf("the title_redacted row holds %q: %s", s, raw)
			}
		}
		if w.journal() {
			t.Error("the journal is left")
		}
	}
}

// TestRedactTitleAuditHasNoTitle is AC17's first clause: one
// thread.title_redacted row, whose details hold none of the old titles or
// the replacement.
func TestRedactTitleAuditHasNoTitle(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("Old Secret Title")
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: uint64(w.users["alice"]),
		Body: &threadlog.ThreadRenamedBody{Title: "Newer Secret Title", Slug: "newer-secret-title"}})
	w.ready()
	w.mustRedact(titleArgs(id, "--title", "Plain Replacement")...)
	rows := w.auditDetails("thread.title_redacted")
	if len(rows) != 1 || rows[0]["journal"] == "" || rows[0]["reason"] != "erasure 4" {
		t.Fatalf("thread.title_redacted rows %v", rows)
	}
	raw, _ := json.Marshal(rows)
	for _, s := range []string{"Secret", "secret", "Plain Replacement", "plain-replacement"} {
		if bytes.Contains(raw, []byte(s)) {
			t.Errorf("the row holds %q: %s", s, raw)
		}
	}
	if got := w.count(`SELECT target_id = ? FROM audit_log WHERE action = 'thread.title_redacted'`, id.String()); got != 1 {
		t.Error("the row does not target the thread")
	}
}

// TestRedactTitleResumes and TestASecondTitleRedactionIsNotSkipped: a title
// redaction resumes after any step, and a second one by the same operator
// for the same reason is not mistaken for the first.
func TestRedactTitleResumes(t *testing.T) {
	for _, step := range []string{"journal", "renamed:T", "projected:T"} {
		t.Run(step, func(t *testing.T) {
			w := newRedactWorld(t)
			id := w.thread("Secret Title")
			w.ready()
			if _, err := w.redact(strings.Replace(step, "T", id.String(), 1), titleArgs(id)...); err == nil {
				t.Fatal("the injected failure did not stop the run")
			}
			w.mustRedact("--resume")
			if m := w.markers(id); len(m) != 1 || !m[0].Title {
				t.Errorf("markers %v", m)
			}
			if len(w.auditDetails("thread.title_redacted")) != 1 || w.journal() {
				t.Error("not finished exactly once")
			}
		})
	}
}

func TestASecondTitleRedactionIsNotSkipped(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("First Secret")
	w.ready()
	w.mustRedact(titleArgs(id, "--title", "Second Secret")...)
	if _, err := w.redact("renamed:"+id.String(), titleArgs(id)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	// The failure came after the swap: resume from before it too.
	w2 := newRedactWorld(t)
	id2 := w2.thread("First Secret")
	w2.ready()
	w2.mustRedact(titleArgs(id2, "--title", "Second Secret")...)
	if _, err := w2.redact("journal", titleArgs(id2)...); err == nil {
		t.Fatal("the injected failure did not stop the run")
	}
	for _, x := range []struct {
		w  *redactWorld
		id threadid.ID
	}{{w, id}, {w2, id2}} {
		x.w.mustRedact("--resume")
		m := x.w.markers(x.id)
		if len(m) != 2 || m[0].Journal == m[1].Journal || !m[0].Title || !m[1].Title {
			t.Errorf("markers %v", m)
		}
		if bytes.Contains(x.w.log(x.id), []byte("Second Secret")) {
			t.Error("the first replacement was not blanked by the second redaction")
		}
		if title, _ := x.w.title(x.id); title != "Title removed" {
			t.Errorf("title %q", title)
		}
		if len(x.w.auditDetails("thread.title_redacted")) != 2 {
			t.Error("want a row per title redaction")
		}
	}
}

// TestRedactTitleUsage: --title goes only with the title target, and a bad
// one is refused before anything is written.
func TestRedactTitleUsage(t *testing.T) {
	w := newRedactWorld(t)
	id := w.thread("A title")
	n := w.post(id, "text")
	w.ready()
	before := w.hashes()
	for _, args := range [][]string{
		append(postArgs(id, n)[:4], "--title", "x", id.String(), fmt.Sprint(n)),
		titleArgs(id, "--title", strings.Repeat("x", 201)),
		titleArgs(id, "--title", "a\tb"),
		titleArgs(id, "--title", "  "),
	} {
		if _, err := w.redact("", args...); exitCodeFor(err) != exitUsage {
			t.Errorf("%v: %v", args, err)
		}
	}
	if !mapsEqual(before, w.hashes()) || w.journal() {
		t.Error("a refused title redaction wrote something")
	}
}

func TestNewULID(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	a, err := newULID(at, bytes.NewReader(bytes.Repeat([]byte{0xff}, 10)))
	if err != nil || len(a) != 26 || a[0] != '0' || !strings.HasSuffix(a, "ZZZZZZZZZZZZZZZZ") {
		t.Errorf("ULID %q, %v", a, err)
	}
	b, _ := newULID(at.Add(time.Millisecond), bytes.NewReader(make([]byte, 10)))
	if b[:10] <= a[:10] || !strings.HasSuffix(b, "0000000000000000") {
		t.Errorf("a later ULID's time sorts before: %s, %s", b, a)
	}
	if _, err := newULID(at, bytes.NewReader(nil)); err == nil {
		t.Error("no entropy was accepted")
	}
}
