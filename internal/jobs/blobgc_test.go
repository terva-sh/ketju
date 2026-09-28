package jobs

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/blob"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// t0 is the fixtures' epoch, in Unix milliseconds.
var t0 = time.UnixMilli(1_757_500_000_000)

type gcWorld struct {
	t    *testing.T
	root string
	db   *db.DB
	now  time.Time
	gc   *BlobGC
	next uint64
}

func newGCWorld(t *testing.T) *gcWorld {
	t.Helper()
	root := t.TempDir()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(root, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, threadid.ThreadsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	w := &gcWorld{t: t, root: root, db: d, now: t0}
	w.gc = &BlobGC{DB: d, Root: root, Now: func() time.Time { return w.now }}
	return w
}

// upload stores a blob as the upload path will: its file, and a row at zero
// references since at.
func (w *gcWorld) upload(name string, at time.Time) string {
	w.t.Helper()
	sum := sha256.Sum256([]byte(name))
	h := hex.EncodeToString(sum[:])
	p, err := blob.Path(w.root, h)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(name), 0o640); err != nil {
		w.t.Fatal(err)
	}
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		return q.CreateBlob(w.t.Context(), dbq.CreateBlobParams{Hash: h, Size: int64(len(name)),
			Mime: "text/plain", UploadedAt: at.UnixMilli()})
	})
	return h
}

func (w *gcWorld) write(fn func(*dbq.Queries, *sql.Tx) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), fn); err != nil {
		w.t.Fatal(err)
	}
}

func (w *gcWorld) addRef(h string) int64 {
	w.t.Helper()
	var n int64
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.AddBlobRef(w.t.Context(), h)
		return err
	})
	return n
}

func (w *gcWorld) dropRef(h string) {
	w.t.Helper()
	w.write(func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.DropBlobRef(w.t.Context(), dbq.DropBlobRefParams{Hash: h, At: w.now.UnixMilli()})
		return err
	})
}

// state reports whether a blob's row and its file exist.
func (w *gcWorld) state(h string) (row, file bool) {
	w.t.Helper()
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), "SELECT count(*) = 1 FROM blobs WHERE hash = ?", h).Scan(&row)
	}); err != nil {
		w.t.Fatal(err)
	}
	p, _ := blob.Path(w.root, h)
	_, err := os.Stat(p)
	return row, err == nil
}

func (w *gcWorld) kept(h string) bool {
	w.t.Helper()
	row, file := w.state(h)
	if row != file {
		w.t.Fatalf("blob %s…: row=%v file=%v; the two must go together", h[:8], row, file)
	}
	return row
}

func (w *gcWorld) run() (Result, error) {
	w.t.Helper()
	return w.gc.Run(w.t.Context())
}

func (w *gcWorld) mustRun() Result {
	w.t.Helper()
	res, err := w.run()
	if err != nil {
		w.t.Fatal(err)
	}
	return res
}

type record struct {
	typ  threadlog.RecordType
	enc  threadlog.BodyEncoding
	body any
}

// thread writes a log by hand, at the path its id names unless dir says
// otherwise, and returns the log's path.
func (w *gcWorld) thread(dir string, recs ...record) string {
	w.t.Helper()
	w.next++
	id := threadid.ID(uint64(t0.UnixMilli())<<16 | w.next)
	buf := threadlog.NewFileHeader(uint64(id), t0.UnixMilli(), 1).AppendTo(nil)
	var ord uint32
	for _, r := range recs {
		enc := r.enc
		if enc == 0 {
			enc = threadlog.EncodingCBOR
		}
		raw, err := threadlog.EncodeBody(enc, r.body)
		if err != nil {
			w.t.Fatal(err)
		}
		h := threadlog.FrameHeader{Type: r.typ, Encoding: enc, Revision: 1, TimestampMS: t0.UnixMilli(), Actor: 1}
		switch r.typ {
		case threadlog.TypePost:
			ord++
			h.Ordinal = ord
		case threadlog.TypeEdit, threadlog.TypeDelete, threadlog.TypeRestore:
			h.Ordinal = ord
		}
		if buf, err = threadlog.AppendFrame(buf, h, raw); err != nil {
			w.t.Fatal(err)
		}
	}
	if dir == "" {
		dir = id.Dir(w.root)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		w.t.Fatal(err)
	}
	p := filepath.Join(dir, id.Hex()+threadid.LogExt)
	if err := os.WriteFile(p, buf, 0o640); err != nil {
		w.t.Fatal(err)
	}
	return p
}

var created = record{typ: threadlog.TypeThreadCreated, body: &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 1}}

func post(attachments ...string) record {
	return record{typ: threadlog.TypePost, body: &threadlog.PostBody{Source: "see attached", Attachments: attachments}}
}

// attr returns a Result attribute's value.
func attr(r Result, key string) any {
	for i := 0; i+1 < len(r.Attrs); i += 2 {
		if r.Attrs[i] == key {
			return r.Attrs[i+1]
		}
	}
	return nil
}

// TestBlobGCSevenDayWindow is AC2's first half: a blob at zero for six days
// and twenty-three hours is kept, one at zero for seven days goes, file and
// row together, and one uploaded a moment ago is not even a candidate.
func TestBlobGCSevenDayWindow(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	young := w.upload("young", w.now.Add(-(BlobGrace - time.Hour)))
	edge := w.upload("edge", w.now.Add(-(BlobGrace - time.Millisecond)))
	due := w.upload("due", w.now.Add(-BlobGrace))
	fresh := w.upload("fresh", w.now)

	res := w.mustRun()
	if res.Removed != 1 || attr(res, "candidates") != 1 {
		t.Fatalf("removed %d of %v candidates, want 1 of 1", res.Removed, attr(res, "candidates"))
	}
	for name, c := range map[string]struct {
		h    string
		kept bool
	}{"6 d 23 h": {young, true}, "7 d - 1 ms": {edge, true}, "7 d": {due, false}, "just uploaded": {fresh, true}} {
		if got := w.kept(c.h); got != c.kept {
			t.Errorf("%s: kept=%v, want %v", name, got, c.kept)
		}
	}
}

// TestRefcountReturningCancels is AC2's second half: a reference gained
// clears the clock, so a blob referenced again before the seven days are up
// survives however long it then lives, and the seven days start again from
// the moment it next drops to zero.
func TestRefcountReturningCancels(t *testing.T) {
	w := newGCWorld(t)
	h := w.upload("photo", t0)
	w.now = t0.Add(6 * 24 * time.Hour)
	if w.addRef(h) != 1 {
		t.Fatal("AddBlobRef found no row")
	}
	w.now = t0.Add(30 * 24 * time.Hour)
	w.mustRun()
	if !w.kept(h) {
		t.Fatal("a referenced blob was collected")
	}
	// Dropped to zero on day 30: kept until day 37.
	w.dropRef(h)
	w.now = t0.Add(37*24*time.Hour - time.Millisecond)
	w.mustRun()
	if !w.kept(h) {
		t.Fatal("collected before seven days at zero")
	}
	w.now = t0.Add(37 * 24 * time.Hour)
	if res := w.mustRun(); res.Removed != 1 || w.kept(h) {
		t.Fatalf("not collected at seven days at zero (removed %d)", res.Removed)
	}
}

// TestReferenceCommittedMidPassWins: a reference committed after the pass
// read its candidates and scanned the logs, but before it deletes, keeps
// the blob. The delete re-checks the refcount in its own transaction.
func TestReferenceCommittedMidPassWins(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	h := w.upload("late", t0)
	other := w.upload("other", t0)
	w.gc.afterScan = func() {
		if w.addRef(h) != 1 {
			t.Error("AddBlobRef found no row before the delete ran")
		}
	}
	res := w.mustRun()
	if !w.kept(h) {
		t.Fatal("a blob referenced mid-pass was collected")
	}
	if w.kept(other) || res.Removed != 1 || attr(res, "kept_changed") != int64(1) {
		t.Fatalf("removed=%d kept_changed=%v; the unreferenced one should go and the other be counted",
			res.Removed, attr(res, "kept_changed"))
	}
	// And the other side of the contract: once a blob is collected, adding
	// a reference finds no row, so the attach path knows to refuse.
	if n := w.addRef(other); n != 0 {
		t.Fatalf("AddBlobRef on a collected blob touched %d rows", n)
	}
}

// TestConcurrentReferencesAndCollection runs the collector against a
// stream of references. Whatever the interleaving, each blob ends either
// referenced with its row and file intact, or collected with neither, and
// AddBlobRef's answer says which. Run under -race.
func TestConcurrentReferencesAndCollection(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	var hashes []string
	for i := range 60 {
		hashes = append(hashes, w.upload(fmt.Sprintf("b%d", i), t0))
	}
	ref := make([]int64, len(hashes))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := len(hashes) - 1; i >= 0; i -= 2 {
			// Direct writes, not w.addRef: t.Fatal belongs to the test goroutine.
			_ = w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				n, err := q.AddBlobRef(t.Context(), hashes[i])
				ref[i] = n
				return err
			})
		}
	}()
	if _, err := w.run(); err != nil {
		t.Error(err)
	}
	wg.Wait()
	for i, h := range hashes {
		kept := w.kept(h)
		switch {
		case ref[i] == 1 && !kept:
			t.Errorf("blob %d was referenced and then collected", i)
		case i%2 == 0 && kept && ref[i] == 0:
			// The goroutine references only the odd indexes, so an even one
			// has nothing keeping it.
			t.Errorf("unreferenced blob %d survived", i)
		}
	}
}

// TestLogReferencesKeepABlob: a blob at zero references that a thread log
// names anywhere is kept, whatever the refcount says. That covers a post
// that has been soft-deleted (Restore must bring its attachments back),
// a hash in an edit's source, a JSON body, upper case, and a log sitting in
// the wrong shard.
func TestLogReferencesKeepABlob(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	softDeleted := w.upload("soft-deleted", t0)
	edited := w.upload("edited", t0)
	jsonBody := w.upload("json", t0)
	upper := w.upload("upper", t0)
	strayLog := w.upload("stray", t0)
	loose := w.upload("loose", t0)

	w.thread("", created, post(softDeleted),
		record{typ: threadlog.TypeDelete, body: &threadlog.DeleteBody{Reason: "oops"}})
	w.thread("", created, post(),
		record{typ: threadlog.TypeEdit, body: &threadlog.EditBody{Source: "![](/blobs/" + edited + ")"}})
	w.thread("", created, record{typ: threadlog.TypePost, enc: threadlog.EncodingJSON,
		body: &threadlog.PostBody{Source: "x", Attachments: []string{`{"hash":"` + jsonBody + `"}`}}})
	w.thread("", created, post("sha256:"+strings.ToUpper(upper)))
	w.thread(filepath.Join(w.root, threadid.ThreadsDir, "00", "00"), created, post(strayLog))

	res := w.mustRun()
	for name, h := range map[string]string{"soft-deleted post": softDeleted, "edit source": edited,
		"JSON body": jsonBody, "upper case": upper, "log in the wrong shard": strayLog} {
		if !w.kept(h) {
			t.Errorf("%s: collected a blob a log names", name)
		}
	}
	if w.kept(loose) {
		t.Error("a blob no log names was kept")
	}
	if res.Removed != 1 || attr(res, "kept_referenced_in_log") != 5 {
		t.Fatalf("removed %d, kept_referenced_in_log %v; want 1 and 5", res.Removed, attr(res, "kept_referenced_in_log"))
	}
}

// TestAnUnreadableLogHoldsThePass: if any log cannot be read to the end, the
// pass cannot know what it references, so it deletes nothing at all.
func TestAnUnreadableLogHoldsThePass(t *testing.T) {
	for name, damage := range map[string]func(path string){
		"torn tail": func(p string) {
			b, _ := os.ReadFile(p)
			if err := os.WriteFile(p, b[:len(b)-3], 0o640); err != nil {
				t.Fatal(err)
			}
		},
		"bad checksum": func(p string) {
			b, _ := os.ReadFile(p)
			b[len(b)-10] ^= 0xff
			if err := os.WriteFile(p, b, 0o640); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newGCWorld(t)
			w.now = t0.Add(30 * 24 * time.Hour)
			h := w.upload("unsure", t0)
			damage(w.thread("", created, post()))
			if _, err := w.run(); !errors.Is(err, ErrHeld) {
				t.Fatalf("Run = %v, want ErrHeld", err)
			}
			if !w.kept(h) {
				t.Fatal("a held pass deleted a blob")
			}
		})
	}
}

// TestHoldHoldsThePass: while Hold names a reason, such as a quarantined
// thread whose log may be missing, nothing is deleted.
func TestHoldHoldsThePass(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	h := w.upload("quarantine", t0)
	w.gc.Hold = func() string { return "1 thread is quarantined" }
	if _, err := w.run(); !errors.Is(err, ErrHeld) || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("Run = %v, want ErrHeld naming the reason", err)
	}
	if !w.kept(h) {
		t.Fatal("a held pass deleted a blob")
	}
	w.gc.Hold = func() string { return "" }
	if res := w.mustRun(); res.Removed != 1 {
		t.Fatalf("once released, removed %d, want 1", res.Removed)
	}
}

// TestNoThreadsDirectoryHoldsThePass: a data directory with no threads/ is
// not one whose logs reference nothing.
func TestNoThreadsDirectoryHoldsThePass(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	h := w.upload("orphan", t0)
	if err := os.Remove(filepath.Join(w.root, threadid.ThreadsDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.run(); !errors.Is(err, ErrHeld) || !w.kept(h) {
		t.Fatalf("Run = %v, kept=%v; want held and kept", err, w.kept(h))
	}
}

// TestAvatarAndDraftGuards: a blob a user's avatar or a draft names is kept,
// though nothing writes either reference yet.
func TestAvatarAndDraftGuards(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	avatar := w.upload("avatar", t0)
	drafted := w.upload("drafted", t0)
	w.write(func(q *dbq.Queries, tx *sql.Tx) error {
		uid, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE users SET avatar_blob = ? WHERE id = ?", avatar, uid); err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO drafts (user_id, thread_id, source, created_at, updated_at)
			VALUES (?, 1, ?, 1, 1)`, uid, "half-written ![](/blobs/"+drafted+")")
		return err
	})
	res := w.mustRun()
	if !w.kept(avatar) || !w.kept(drafted) || res.Removed != 0 || attr(res, "kept_changed") != int64(2) {
		t.Fatalf("avatar kept=%v, draft kept=%v, removed %d", w.kept(avatar), w.kept(drafted), res.Removed)
	}
}

// TestAFileThatWillNotGoKeepsItsRow: when the file cannot be removed, the
// row delete rolls back, so the next pass finds the blob again rather than
// leaving a file no row accounts for.
func TestAFileThatWillNotGoKeepsItsRow(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	h := w.upload("stuck", t0)
	p, _ := blob.Path(w.root, h)
	shard := filepath.Dir(p)
	if err := os.Chmod(shard, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(shard, 0o750) })

	res, err := w.run()
	if err == nil || res.Removed != 0 || attr(res, "failed") != int64(1) {
		t.Fatalf("Run = %v, removed %d, failed %v; want the failure reported", err, res.Removed, attr(res, "failed"))
	}
	if !w.kept(h) {
		t.Fatal("the row went though the file stayed")
	}
	if err := os.Chmod(shard, 0o750); err != nil {
		t.Fatal(err)
	}
	if res := w.mustRun(); res.Removed != 1 || w.kept(h) {
		t.Fatal("the next pass did not collect it")
	}
}

// TestAMissingFileStillClearsTheRow: a file already gone, as a pass whose
// commit failed after the removal leaves it, is not an error.
func TestAMissingFileStillClearsTheRow(t *testing.T) {
	w := newGCWorld(t)
	w.now = t0.Add(30 * 24 * time.Hour)
	h := w.upload("gone", t0)
	p, _ := blob.Path(w.root, h)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if res := w.mustRun(); res.Removed != 1 {
		t.Fatalf("removed %d, want 1", res.Removed)
	}
	if row, _ := w.state(h); row {
		t.Fatal("the row is still there")
	}
}

// TestNoCandidatesReadsNoLogs: with nothing to collect, which is every pass
// until uploads exist, the logs are not read at all, so a damaged one does
// not turn an idle pass into a failure.
func TestNoCandidatesReadsNoLogs(t *testing.T) {
	w := newGCWorld(t)
	p := w.thread("", created, post())
	if err := os.WriteFile(p, []byte("not a log"), 0o640); err != nil {
		t.Fatal(err)
	}
	res := w.mustRun()
	if res.Removed != 0 || attr(res, "candidates") != 0 {
		t.Fatalf("an empty pass = %+v", res)
	}
}

func TestHexMentions(t *testing.T) {
	h := strings.Repeat("ab", 32)
	want := map[string]bool{h: true}
	for _, c := range []struct {
		body  string
		found bool
	}{
		{h, true},
		{"x" + h + "y", true},
		{"0" + h + "0", true}, // inside a longer run of hex
		{strings.ToUpper(h), true},
		{h[:63], false},
		{h[:32] + "-" + h[32:], false},
		{"", false},
	} {
		found := map[string]bool{}
		hexMentions([]byte(c.body), want, found)
		if found[h] != c.found {
			t.Errorf("hexMentions(%q) found=%v, want %v", c.body, found[h], c.found)
		}
	}
}
