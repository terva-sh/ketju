package threads

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for flags and the auto-hide (TKT-01M29CP2PJ). Each world has one
// public forum, one thread whose post 1 is the author's, four reporters and a
// moderator, the default threshold of 3, and a writer a test can fail or
// hold.

// faultWriter is posts.Writer with a hook before every append: hold may
// block, and fail may refuse the append, which then writes nothing.
type faultWriter struct {
	inner posts.Writer
	mu    sync.Mutex
	hold  func(a threadlog.Append)
	fail  func(a threadlog.Append) error
}

func (f *faultWriter) Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error) {
	f.mu.Lock()
	hold, fail := f.hold, f.fail
	f.mu.Unlock()
	if hold != nil {
		hold(a)
	}
	if fail != nil {
		if err := fail(a); err != nil {
			return threadlog.Result{}, err
		}
	}
	return f.inner.Append(th, a)
}

func (f *faultWriter) set(hold func(threadlog.Append), fail func(threadlog.Append) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hold, f.fail = hold, fail
}

type flagWorld struct {
	t      *testing.T
	db     *db.DB
	root   string
	cache  *threadcache.Cache
	proj   *projection.Writer
	svc    *Service
	writer *faultWriter
	users  map[string]int64
	forum  int64
	id     threadid.ID
	clock  int64

	projectMu   sync.Mutex
	failProject bool
}

var errInjected = errors.New("injected failure")

func newFlagWorld(t *testing.T) *flagWorld {
	t.Helper()
	w := &flagWorld{t: t, db: openDB(t), users: map[string]int64{}, clock: 1_757_500_000_000}
	root := filepath.Join(t.TempDir(), "threads")
	w.root = root
	var err error
	if w.cache, err = threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.cache.Close() })
	if w.proj, err = projection.New(projection.Options{DB: w.db, Root: root,
		Sweep: func(context.Context) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct{ handle, role string }{
		{"author", domain.RoleMember}, {"rep1", domain.RoleMember}, {"rep2", domain.RoleMember},
		{"rep3", domain.RoleMember}, {"rep4", domain.RoleMember}, {"mod", domain.RoleModerator},
	} {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: "active", Role: u.role, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			w.users[u.handle] = id
			return err
		})
	}
	w.write(func(q *dbq.Queries) error {
		var err error
		w.forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.writer = &faultWriter{}
	w.svc = &Service{DB: w.db, Perm: perm.New(w.db), Threads: w.cache, Render: render.NewCache(1 << 20),
		Writer: w.writer, IDs: threadid.NewGenerator(),
		Project: func(ctx context.Context, id threadid.ID, n int64) error {
			w.projectMu.Lock()
			fail := w.failProject
			w.projectMu.Unlock()
			if fail {
				return errInjected
			}
			_, err := w.proj.ProjectThread(ctx, id, n)
			return err
		},
		Now: func() time.Time { return time.UnixMilli(w.clock) },
	}

	w.id = threadid.ID(uint64(1_757_500_000_000)<<16 | 1)
	th, err := w.cache.Create(w.id, uint64(w.forum), w.clock)
	if err != nil {
		t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["author"]),
		Body: &threadlog.ThreadCreatedBody{Title: "Flagged", Slug: "flagged", ForumID: uint64(w.forum)}})
	w.post("the post being flagged")
	return w
}

func (w *flagWorld) write(fn func(q *dbq.Queries) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		w.t.Fatal(err)
	}
}

// append writes a record straight to the actor, and projects it.
func (w *flagWorld) append(a threadlog.Append) {
	w.t.Helper()
	w.clock += 1000
	a.TimeMS = w.clock
	var res threadlog.Result
	if err := w.cache.With(w.id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), w.id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
}

// post adds a post by the author and returns its ordinal.
func (w *flagWorld) post(src string) uint32 {
	w.t.Helper()
	w.append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["author"]), Body: &threadlog.PostBody{Source: src}})
	return uint32(w.entries())
}

func (w *flagWorld) viewer(handle string) domain.Viewer {
	role := domain.RoleMember
	if handle == "mod" {
		role = domain.RoleModerator
	}
	return domain.Viewer{UserID: w.users[handle], Role: role, Active: true, SessionKey: "s-" + handle}
}

func (w *flagWorld) flag(who string, ordinal uint32) (dto.FlagReceipt, error) {
	w.clock += 1000
	return w.svc.Flag(w.t.Context(), w.viewer(who), w.id, FlagRequest{Ordinal: ordinal, Reason: FlagSpam})
}

func (w *flagWorld) mustFlag(who string, ordinal uint32) {
	w.t.Helper()
	if r, err := w.flag(who, ordinal); err != nil || !r.Received {
		w.t.Fatalf("%s's flag on %d: %+v, %v", who, ordinal, r, err)
	}
}

// hide has three reporters flag post ordinal, which hides it.
func (w *flagWorld) hide(ordinal uint32) {
	w.t.Helper()
	for _, r := range []string{"rep1", "rep2", "rep3"} {
		w.mustFlag(r, ordinal)
	}
	if e := w.entry(ordinal); !e.Flags.Has(threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden) {
		w.t.Fatalf("after three flags, post %d has flags %#x: want it hidden pending review", ordinal, e.Flags)
	}
}

func (w *flagWorld) entry(ordinal uint32) threadlog.IndexEntry {
	w.t.Helper()
	var e threadlog.IndexEntry
	if err := w.cache.With(w.id, func(th *threadcache.Thread) error {
		var err error
		e, err = th.Index.Entry(ordinal)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return e
}

func (w *flagWorld) entries() int {
	w.t.Helper()
	var n int
	if err := w.cache.With(w.id, func(th *threadcache.Thread) error {
		n = th.Index.Len()
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *flagWorld) logLength() int64 {
	w.t.Helper()
	var n int64
	if err := w.cache.With(w.id, func(th *threadcache.Thread) error {
		n = th.Actor.LogLength()
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// flagStatuses is post ordinal's flags as reporter handle to status.
func (w *flagWorld) flagStatuses(ordinal uint32) map[string]string {
	w.t.Helper()
	byID := map[int64]string{}
	for h, id := range w.users {
		byID[id] = h
	}
	out := map[string]string{}
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT reporter_id, status, coalesce(resolved_by, 0) FROM flags
			WHERE thread_id = ? AND ordinal = ? ORDER BY id`, int64(w.id), int64(ordinal))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rep, by int64
			var status string
			if err := rows.Scan(&rep, &status, &by); err != nil {
				return err
			}
			if by != 0 {
				status += " by " + byID[by]
			}
			if prev, ok := out[byID[rep]]; ok {
				status = prev + ", " + status
			}
			out[byID[rep]] = status
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (w *flagWorld) openFlags(ordinal uint32) int64 {
	w.t.Helper()
	var n int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.OpenFlagCount(w.t.Context(), dbq.OpenFlagCountParams{ThreadID: int64(w.id), Ordinal: int64(ordinal)})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// audit is every audit row about a post, as action and actor (0 for none).
func (w *flagWorld) audit() []string {
	w.t.Helper()
	var out []string
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT action, coalesce(actor_id, 0), details_json FROM audit_log
			WHERE target_type = 'post' ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var action, details string
			var actor int64
			if err := rows.Scan(&action, &actor, &details); err != nil {
				return err
			}
			var d map[string]any
			if err := json.Unmarshal([]byte(details), &d); err != nil {
				return err
			}
			who := "system"
			for h, id := range w.users {
				if id == actor {
					who = h
				}
			}
			if actor == 0 && d["via"] != "thread-log" {
				who = "nobody"
			}
			out = append(out, action+" by "+who)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (w *flagWorld) setThreshold(n int) {
	w.t.Helper()
	raw, _ := json.Marshal(n)
	if err := settings.Set(w.t.Context(), w.db, settings.FlagAutoHideThreshold.Name, raw, w.users["mod"], time.UnixMilli(w.clock)); err != nil {
		w.t.Fatal(err)
	}
}

func (w *flagWorld) restore(who string, ordinal uint32) (dto.CreatedPost, error) {
	w.clock += 1000
	return w.svc.Restore(w.t.Context(), w.viewer(who), w.id, ordinal, "looked fine")
}

func codeOf(err error) problem.Code {
	if err == nil {
		return ""
	}
	return problem.FromError(err)
}

// TestAutoHideAtThreshold is AC1: with the threshold at 3, the third
// reporter's flag appends one Delete with auto and frame actor 0, the entry
// gets hidden-by-mod and auto-hidden, and one post.auto_hidden row is
// written, by the system. Two flags hide nothing. A later flag on the hidden
// post is conflict and appends nothing.
func TestAutoHideAtThreshold(t *testing.T) {
	w := newFlagWorld(t)
	for _, r := range []string{"rep1", "rep2"} {
		w.mustFlag(r, 1)
	}
	if e := w.entry(1); !e.Flags.Visible() {
		t.Fatalf("two flags hid the post: %#x", e.Flags)
	}
	before := w.logLength()
	w.mustFlag("rep3", 1)
	e := w.entry(1)
	if e.Flags&(threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden|threadlog.EntryDeleted) !=
		threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden || e.Author != uint64(w.users["author"]) {
		t.Errorf("after the third flag the entry is %+v: want hidden-by-mod and auto-hidden, still the author's", e)
	}
	recs := w.frames(before)
	if len(recs) != 1 {
		t.Fatalf("the third flag appended %d records, want 1", len(recs))
	}
	b, ok := recs[0].body.(*threadlog.DeleteBody)
	if !ok || recs[0].h.Type != threadlog.TypeDelete || recs[0].h.Actor != 0 || recs[0].h.Ordinal != 1 ||
		!b.Auto || b.ByModerator || b.Reason != AutoHideReason {
		t.Errorf("the hide is %s by %d on %d with %+v: want an auto Delete by actor 0", recs[0].h.Type, recs[0].h.Actor, recs[0].h.Ordinal, recs[0].body)
	}
	if got, want := w.audit(), []string{"post.auto_hidden by system"}; !reflect.DeepEqual(got, want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}

	before = w.logLength()
	if _, err := w.flag("rep4", 1); codeOf(err) != problem.CodeConflict {
		t.Errorf("a flag on the hidden post: %v, want conflict", err)
	}
	// A reporter's repeat after the hide is a repeat like any other: the
	// same receipt, so it does not say the post went down (review 1506).
	if r, err := w.flag("rep1", 1); err != nil || r != (dto.FlagReceipt{Received: true}) {
		t.Errorf("rep1's repeat after the hide: %+v, %v; want the receipt", r, err)
	}
	if w.logLength() != before || len(w.audit()) != 1 {
		t.Error("a flag on the hidden post changed the log or the audit log")
	}
}

// frames is every record appended at or after offset from.
func (w *flagWorld) frames(from int64) []struct {
	h    threadlog.FrameHeader
	body any
} {
	w.t.Helper()
	f, err := os.ReadFile(w.id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []struct {
		h    threadlog.FrameHeader
		body any
	}
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		if r.Offset >= from {
			v, err := threadlog.DecodeBody(r.Header.Type, r.Header.Encoding, r.Body)
			if err != nil {
				w.t.Fatal(err)
			}
			out = append(out, struct {
				h    threadlog.FrameHeader
				body any
			}{r.Header, v})
		}
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestDuplicateFlagDoesNotCount is AC4: a member's second flag on the same
// post adds no row, so the open count stays 1, and both answers are the
// same receipt.
func TestDuplicateFlagDoesNotCount(t *testing.T) {
	w := newFlagWorld(t)
	first, err := w.flag("rep1", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.svc.Flag(t.Context(), w.viewer("rep1"), w.id, FlagRequest{Ordinal: 1, Reason: FlagOther, Note: "still spam"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != (dto.FlagReceipt{Received: true}) {
		t.Errorf("answers %+v and %+v: want the same receipt", first, second)
	}
	if n := w.openFlags(1); n != 1 {
		t.Errorf("%d open flags after one member flagged twice, want 1", n)
	}
	if got := w.flagStatuses(1); !reflect.DeepEqual(got, map[string]string{"rep1": "open"}) {
		t.Errorf("flags %v: want one open flag by rep1", got)
	}
}

// TestFlagRefusals: a flag needs a member session and a readable thread, a
// reason among the four, a note for other and one no longer than
// MaxFlagNote, a post that exists, and a post that is shown. A refused flag
// adds no row.
func TestFlagRefusals(t *testing.T) {
	w := newFlagWorld(t)
	long := make([]rune, MaxFlagNote+1)
	for i := range long {
		long[i] = 'x'
	}
	for _, c := range []struct {
		name string
		v    domain.Viewer
		req  FlagRequest
		want problem.Code
	}{
		{"anonymous", domain.Anonymous(), FlagRequest{Ordinal: 1, Reason: FlagSpam}, problem.CodeUnauthenticated},
		{"pre-user", domain.Viewer{PreUser: true, IdentityID: 7, Active: true}, FlagRequest{Ordinal: 1, Reason: FlagSpam}, problem.CodeForbidden},
		{"unknown reason", w.viewer("rep1"), FlagRequest{Ordinal: 1, Reason: "rude"}, problem.CodeValidationFailed},
		{"other without a note", w.viewer("rep1"), FlagRequest{Ordinal: 1, Reason: FlagOther, Note: "  "}, problem.CodeValidationFailed},
		{"a long note", w.viewer("rep1"), FlagRequest{Ordinal: 1, Reason: FlagAbuse, Note: string(long)}, problem.CodeValidationFailed},
		{"no such post", w.viewer("rep1"), FlagRequest{Ordinal: 9, Reason: FlagSpam}, problem.CodeNotFound},
		{"ordinal zero", w.viewer("rep1"), FlagRequest{Ordinal: 0, Reason: FlagSpam}, problem.CodeNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := w.svc.Flag(t.Context(), c.v, w.id, c.req); codeOf(err) != c.want {
				t.Errorf("%v, want %s", err, c.want)
			}
		})
	}
	if _, err := w.svc.Flag(t.Context(), w.viewer("rep1"), threadid.ID(99), FlagRequest{Ordinal: 1, Reason: FlagSpam}); codeOf(err) != problem.CodeNotFound {
		t.Errorf("a flag on a missing thread: %v, want not-found", err)
	}
	// A post its author deleted is not shown.
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["author"]), Ordinal: 1, Body: &threadlog.DeleteBody{}})
	if _, err := w.flag("rep1", 1); codeOf(err) != problem.CodeConflict {
		t.Errorf("a flag on a deleted post: %v, want conflict", err)
	}
	if got := w.flagStatuses(1); len(got) != 0 {
		t.Errorf("refused flags left rows %v", got)
	}
}

// TestAutoHideRecoversFromAFailedAppend is AC7: with the hide's append made
// to fail, the threshold-crossing flag answers unavailable and stays
// committed. One sweep then hides the post and a second appends nothing. A
// threshold lowered to 2 is enforced by the next sweep on a post with two
// open flags.
func TestAutoHideRecoversFromAFailedAppend(t *testing.T) {
	w := newFlagWorld(t)
	w.mustFlag("rep1", 1)
	w.mustFlag("rep2", 1)
	failed := 0
	w.writer.set(nil, func(a threadlog.Append) error {
		if a.Type == threadlog.TypeDelete && failed == 0 {
			failed++
			return errInjected
		}
		return nil
	})
	if _, err := w.flag("rep3", 1); codeOf(err) != problem.CodeUnavailable {
		t.Fatalf("the threshold-crossing flag with its hide failing: %v, want unavailable", err)
	}
	if n := w.openFlags(1); n != 3 {
		t.Fatalf("%d open flags after the failed hide, want 3: the flag is committed", n)
	}
	if e := w.entry(1); !e.Flags.Visible() {
		t.Fatalf("the failed hide left the post %#x", e.Flags)
	}

	n, err := w.svc.SweepAutoHide(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("the sweep hid %d, %v: want 1", n, err)
	}
	if e := w.entry(1); !e.Flags.Has(threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden) {
		t.Errorf("after the sweep the post is %#x: want hidden pending review", e.Flags)
	}
	before := w.logLength()
	if n, err := w.svc.SweepAutoHide(t.Context()); err != nil || n != 0 || w.logLength() != before {
		t.Errorf("a second sweep hid %d, %v, and moved the log from %d to %d: want nothing", n, err, before, w.logLength())
	}

	// A lowered threshold.
	second := w.post("another post")
	w.mustFlag("rep1", second)
	w.mustFlag("rep2", second)
	if n, _ := w.svc.SweepAutoHide(t.Context()); n != 0 || !w.entry(second).Flags.Visible() {
		t.Fatalf("at threshold 3 the sweep hid a post with two flags")
	}
	w.setThreshold(2)
	if n, err := w.svc.SweepAutoHide(t.Context()); err != nil || n != 1 {
		t.Errorf("at threshold 2 the sweep hid %d, %v: want 1", n, err)
	}
	if e := w.entry(second); !e.Flags.Has(threadlog.EntryAutoHidden) {
		t.Errorf("the lowered threshold left post %d %#x", second, e.Flags)
	}
}

// TestRestoreClearsAutoHideAndKeepsFlags is AC2 and the author's half of
// AC5: the author may not restore a post hidden pending review; a
// moderator's Restore clears hidden-by-mod and auto-hidden, the open flags
// become dismissed rows naming the moderator, and a flag reported
// afterwards counts from one.
func TestRestoreClearsAutoHideAndKeepsFlags(t *testing.T) {
	w := newFlagWorld(t)
	w.hide(1)
	if _, err := w.restore("author", 1); codeOf(err) != problem.CodeForbidden {
		t.Errorf("the author's restore: %v, want forbidden", err)
	}
	res, err := w.restore("mod", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Post.Deleted || res.Post.Removal != nil {
		t.Errorf("the restore answered %+v: want the post shown", res.Post)
	}
	if e := w.entry(1); !e.Flags.Visible() || e.Flags.Has(threadlog.EntryAutoHidden) {
		t.Errorf("after the restore the entry is %#x: want visible with auto-hidden clear", e.Flags)
	}
	want := map[string]string{"rep1": "dismissed by mod", "rep2": "dismissed by mod", "rep3": "dismissed by mod"}
	if got := w.flagStatuses(1); !reflect.DeepEqual(got, want) {
		t.Errorf("flags after the restore %v, want %v", got, want)
	}
	if got, want := w.audit(), []string{"post.auto_hidden by system", "post.restored by mod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}
	w.mustFlag("rep1", 1)
	if n := w.openFlags(1); n != 1 || !w.entry(1).Flags.Visible() {
		t.Errorf("a flag after the restore: %d open, post %#x; want 1 and shown", n, w.entry(1).Flags)
	}
}

// TestModeratorRestoreDismissesFlagsOfAnyHide: a moderator who restores a
// post a moderator hid by hand, with open flags at the threshold, has those
// flags dismissed too, or the next sweep would hide it again.
func TestModeratorRestoreDismissesFlagsOfAnyHide(t *testing.T) {
	w := newFlagWorld(t)
	w.mustFlag("rep1", 1)
	w.mustFlag("rep2", 1)
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["mod"]), Ordinal: 1,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	w.setThreshold(2)
	if _, err := w.restore("mod", 1); err != nil {
		t.Fatal(err)
	}
	before := w.logLength()
	if n, err := w.svc.SweepAutoHide(t.Context()); err != nil || n != 0 || w.logLength() != before {
		t.Errorf("the sweep after a moderator's restore hid %d, %v", n, err)
	}
	if n := w.openFlags(1); n != 0 {
		t.Errorf("%d flags open after the restore, want 0", n)
	}
}

// TestSweepCannotReHideDuringRestore is AC9 and AC10, under -race:
//   - a restore held inside its lock at the Restore append keeps a
//     concurrent auto-hide of the post waiting; it then finds no open
//     flags and appends nothing, and the post ends shown;
//   - a sweep held inside its lock between its count and its append keeps a
//     concurrent restore waiting, so the restore cannot land between them;
//   - a failed Restore append reopens the flags it dismissed, and the post
//     stays hidden;
//   - a durable Restore whose projection fails answers internal, the flags
//     stay dismissed, the index shows the post, the next sweep appends
//     nothing, and catch-up projects the Restore.
func TestSweepCannotReHideDuringRestore(t *testing.T) {
	t.Run("restore held at its append", func(t *testing.T) {
		w := newFlagWorld(t)
		w.hide(1)
		reached, release := make(chan struct{}), make(chan struct{})
		w.writer.set(func(a threadlog.Append) {
			if a.Type == threadlog.TypeRestore {
				close(reached)
				<-release
			}
		}, nil)
		restored := make(chan error, 1)
		go func() { _, err := w.restore("mod", 1); restored <- err }()
		<-reached
		// The sweep's query already finds nothing, since the flags were
		// dismissed before the append, so the hide it would run for the
		// post, as a new reporter's flag would, is what must wait.
		swept := make(chan bool, 1)
		go func() {
			hid, err := w.svc.EnforceAutoHide(context.Background(), w.id, 1)
			if err != nil {
				t.Error(err)
			}
			swept <- hid
		}()
		select {
		case hid := <-swept:
			t.Fatalf("the auto-hide finished (hid %t) while the restore held the post's lock", hid)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-restored; err != nil {
			t.Fatal(err)
		}
		if hid := <-swept; hid {
			t.Error("the auto-hide hid the post after the restore")
		}
		if n, err := w.svc.SweepAutoHide(t.Context()); err != nil || n != 0 {
			t.Errorf("a sweep after the restore hid %d, %v", n, err)
		}
		if e := w.entry(1); !e.Flags.Visible() {
			t.Errorf("the post ended %#x: want shown", e.Flags)
		}
	})

	t.Run("sweep held between its count and its append", func(t *testing.T) {
		w := newFlagWorld(t)
		// Two flags, a moderator's hide by hand, then the threshold's third:
		// the post is down, so no flag hides it, but the sweep will count 3.
		w.mustFlag("rep1", 1)
		w.mustFlag("rep2", 1)
		w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["mod"]), Ordinal: 1,
			Body: &threadlog.DeleteBody{ByModerator: true}})
		w.write(func(q *dbq.Queries) error {
			_, err := q.CreateFlag(t.Context(), dbq.CreateFlagParams{ThreadID: int64(w.id), Ordinal: 1,
				ReporterID: w.users["rep3"], Reason: FlagSpam, CreatedAt: w.clock})
			return err
		})
		counted, release := make(chan struct{}), make(chan struct{})
		w.svc.afterFlagCount = func(threadid.ID, uint32) {
			close(counted)
			<-release
		}
		swept := make(chan bool, 1)
		go func() {
			hid, err := w.svc.EnforceAutoHide(context.Background(), w.id, 1)
			if err != nil {
				t.Error(err)
			}
			swept <- hid
		}()
		<-counted
		restored := make(chan error, 1)
		go func() { _, err := w.restore("mod", 1); restored <- err }()
		select {
		case err := <-restored:
			t.Fatalf("the restore finished (%v) while the sweep held the post's lock", err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if hid := <-swept; hid {
			t.Error("the sweep hid a post that was already down")
		}
		if err := <-restored; err != nil {
			t.Fatal(err)
		}
		w.svc.afterFlagCount = nil
		if e := w.entry(1); !e.Flags.Visible() {
			t.Errorf("the post ended %#x: want shown", e.Flags)
		}
		if n, _ := w.svc.SweepAutoHide(t.Context()); n != 0 || !w.entry(1).Flags.Visible() {
			t.Error("a sweep after the restore hid the post again")
		}
	})

	t.Run("restore append fails", func(t *testing.T) {
		w := newFlagWorld(t)
		w.hide(1)
		w.writer.set(nil, func(a threadlog.Append) error {
			if a.Type == threadlog.TypeRestore {
				return errInjected
			}
			return nil
		})
		if _, err := w.restore("mod", 1); !errors.Is(err, errInjected) {
			t.Fatalf("the restore with its append failing: %v", err)
		}
		want := map[string]string{"rep1": "open", "rep2": "open", "rep3": "open"}
		if got := w.flagStatuses(1); !reflect.DeepEqual(got, want) {
			t.Errorf("flags after the failed restore %v, want %v", got, want)
		}
		if e := w.entry(1); !e.Flags.Has(threadlog.EntryAutoHidden) {
			t.Errorf("the failed restore left the post %#x", e.Flags)
		}
	})

	t.Run("restore cancelled at its append", func(t *testing.T) {
		// The request goes away during the append (review 1506): the
		// reopen still runs, on a context of its own.
		w := newFlagWorld(t)
		w.hide(1)
		ctx, cancel := context.WithCancel(t.Context())
		w.writer.set(nil, func(a threadlog.Append) error {
			if a.Type == threadlog.TypeRestore {
				cancel()
				return context.Canceled
			}
			return nil
		})
		w.clock += 1000
		if _, err := w.svc.Restore(ctx, w.viewer("mod"), w.id, 1, "looked fine"); !errors.Is(err, context.Canceled) {
			t.Fatalf("the cancelled restore: %v", err)
		}
		if n := w.openFlags(1); n != 3 {
			t.Errorf("%d open flags after the cancelled restore, want 3 reopened", n)
		}
		if e := w.entry(1); !e.Flags.Has(threadlog.EntryAutoHidden) {
			t.Errorf("the cancelled restore left the post %#x", e.Flags)
		}
	})

	t.Run("restore durable, projection fails", func(t *testing.T) {
		w := newFlagWorld(t)
		w.hide(1)
		w.projectMu.Lock()
		w.failProject = true
		w.projectMu.Unlock()
		if _, err := w.restore("mod", 1); codeOf(err) != problem.CodeInternal {
			t.Fatalf("the restore with its projection failing: %v, want internal", err)
		}
		w.projectMu.Lock()
		w.failProject = false
		w.projectMu.Unlock()
		want := map[string]string{"rep1": "dismissed by mod", "rep2": "dismissed by mod", "rep3": "dismissed by mod"}
		if got := w.flagStatuses(1); !reflect.DeepEqual(got, want) {
			t.Errorf("flags after a durable restore %v, want %v", got, want)
		}
		if e := w.entry(1); !e.Flags.Visible() {
			t.Errorf("the index shows the post %#x after a durable restore", e.Flags)
		}
		before := w.logLength()
		if n, err := w.svc.SweepAutoHide(t.Context()); err != nil || n != 0 || w.logLength() != before {
			t.Errorf("the sweep hid %d, %v after a durable restore", n, err)
		}
		if _, err := w.proj.ProjectThread(t.Context(), w.id, w.logLength()); err != nil {
			t.Fatal(err)
		}
		var deleted int64
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT deleted FROM post_meta WHERE thread_id = ? AND ordinal = 1`,
				int64(w.id)).Scan(&deleted)
		}); err != nil {
			t.Fatal(err)
		}
		if deleted != 0 {
			t.Error("catch-up left the restored post deleted in post_meta")
		}
	})
}

// TestConfirmHide: a moderator's confirmation of a post hidden pending
// review keeps it hidden as the moderator's hide, resolves its flags, and
// its author then reads it as removed by a moderator. Confirming a post not
// pending review is conflict, and a member may not confirm.
func TestConfirmHide(t *testing.T) {
	w := newFlagWorld(t)
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, 1, "spam"); codeOf(err) != problem.CodeConflict {
		t.Errorf("confirming a shown post: %v, want conflict", err)
	}
	w.hide(1)
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("rep4"), w.id, 1, "spam"); codeOf(err) != problem.CodeForbidden {
		t.Errorf("a member's confirmation: %v, want forbidden", err)
	}
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, 1, "spam"); err != nil {
		t.Fatal(err)
	}
	if e := w.entry(1); e.Flags&(threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden) != threadlog.EntryHiddenByMod {
		t.Errorf("after the confirmation the entry is %#x: want hidden-by-mod only", e.Flags)
	}
	want := map[string]string{"rep1": "resolved by mod", "rep2": "resolved by mod", "rep3": "resolved by mod"}
	if got := w.flagStatuses(1); !reflect.DeepEqual(got, want) {
		t.Errorf("flags after the confirmation %v, want %v", got, want)
	}
	served, err := w.svc.Post(t.Context(), w.viewer("author"), w.id, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	p := served.Body
	if p.Removal == nil || *p.Removal != dto.RemovalModerator || p.HTML == "" {
		t.Errorf("the author reads the confirmed post as %+v: want it, removed by a moderator", p)
	}
	if got, want := w.audit(), []string{"post.auto_hidden by system", "post.hidden by mod"}; !reflect.DeepEqual(got, want) {
		t.Errorf("audit rows %v, want %v", got, want)
	}
}

// TestFlagHoldsThePostLock (Terva review 1504 on PR #176): a flag checks the
// post, inserts and hides under the post's flag lock, so it cannot straddle
// a moderator's restore.
//   - A flag arriving while a restore holds the lock waits, then checks the
//     restored post and counts from one, and hides nothing.
//   - A restore arriving while a flag holds the lock waits for the flag to
//     finish, so a flag checked before the restore is never inserted after
//     the restore dismissed the others.
func TestFlagHoldsThePostLock(t *testing.T) {
	t.Run("a flag during a restore", func(t *testing.T) {
		w := newFlagWorld(t)
		w.hide(1)
		reached, release := make(chan struct{}), make(chan struct{})
		w.writer.set(func(a threadlog.Append) {
			if a.Type == threadlog.TypeRestore {
				close(reached)
				<-release
			}
		}, nil)
		restored := make(chan error, 1)
		go func() { _, err := w.restore("mod", 1); restored <- err }()
		<-reached
		flagged := make(chan error, 1)
		go func() {
			_, err := w.svc.Flag(context.Background(), w.viewer("rep4"), w.id, FlagRequest{Ordinal: 1, Reason: FlagSpam})
			flagged <- err
		}()
		select {
		case err := <-flagged:
			t.Fatalf("the flag finished (%v) while the restore held the post's lock", err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-restored; err != nil {
			t.Fatal(err)
		}
		if err := <-flagged; err != nil {
			t.Fatalf("the flag after the restore: %v", err)
		}
		if n := w.openFlags(1); n != 1 || !w.entry(1).Flags.Visible() {
			t.Errorf("after the restore and the late flag: %d open, post %#x; want 1 and shown", n, w.entry(1).Flags)
		}
	})

	t.Run("a restore during a flag", func(t *testing.T) {
		w := newFlagWorld(t)
		w.hide(1)
		held, release := make(chan struct{}), make(chan struct{})
		w.svc.beforeFlagCheck = func(threadid.ID, uint32) {
			close(held)
			<-release
		}
		flagged := make(chan error, 1)
		go func() {
			_, err := w.svc.Flag(context.Background(), w.viewer("rep4"), w.id, FlagRequest{Ordinal: 1, Reason: FlagSpam})
			flagged <- err
		}()
		<-held
		restored := make(chan error, 1)
		go func() { _, err := w.restore("mod", 1); restored <- err }()
		select {
		case err := <-restored:
			t.Fatalf("the restore finished (%v) while a flag held the post's lock", err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-flagged; codeOf(err) != problem.CodeConflict {
			t.Errorf("the flag checked against the hidden post: %v, want conflict", err)
		}
		if err := <-restored; err != nil {
			t.Fatal(err)
		}
		w.svc.beforeFlagCheck = nil
		if n := w.openFlags(1); n != 0 || !w.entry(1).Flags.Visible() {
			t.Errorf("after the flag and the restore: %d open, post %#x; want none and shown", n, w.entry(1).Flags)
		}
	})
}

// TestConfirmHideRecoversAFailedResolve (Terva review 1504 on PR #176): a
// confirmation whose Delete is durable but whose resolve fails leaves the
// post a moderator's hide with its flags open, and asking again resolves
// them without appending a second Delete. With nothing left open it is
// conflict.
func TestConfirmHideRecoversAFailedResolve(t *testing.T) {
	w := newFlagWorld(t)
	w.hide(1)
	exec := func(stmt string) {
		t.Helper()
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), stmt)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TRIGGER test_refuse_resolve BEFORE UPDATE ON flags WHEN NEW.status = 'resolved'
		BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, 1, "spam"); err == nil {
		t.Fatal("the confirmation succeeded with its resolve failing")
	}
	if e := w.entry(1); e.Flags&(threadlog.EntryHiddenByMod|threadlog.EntryAutoHidden) != threadlog.EntryHiddenByMod {
		t.Fatalf("after the half-done confirmation the entry is %#x: want the moderator's hide", e.Flags)
	}
	if n := w.openFlags(1); n != 3 {
		t.Fatalf("%d open flags after the failed resolve, want 3", n)
	}
	exec(`DROP TRIGGER test_refuse_resolve`)
	before := w.logLength()
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, 1, "spam"); err != nil {
		t.Fatalf("asking again: %v", err)
	}
	if w.logLength() != before {
		t.Error("asking again appended to the log")
	}
	want := map[string]string{"rep1": "resolved by mod", "rep2": "resolved by mod", "rep3": "resolved by mod"}
	if got := w.flagStatuses(1); !reflect.DeepEqual(got, want) {
		t.Errorf("flags after asking again %v, want %v", got, want)
	}
	if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, 1, "spam"); codeOf(err) != problem.CodeConflict {
		t.Errorf("a third confirmation: %v, want conflict", err)
	}
}

// TestConfirmHideRefusesAnArchivedThread (review 1506): an archived thread
// is read-only, so a confirmation is conflict and changes no flag, whether
// the post is pending review or already a moderator's hide with flags open.
func TestConfirmHideRefusesAnArchivedThread(t *testing.T) {
	w := newFlagWorld(t)
	second := w.post("another post")
	w.hide(1)
	w.hide(second)
	// #1 becomes a moderator's hide with its flags still open.
	w.append(threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["mod"]), Ordinal: 1,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	w.cache.MarkArchived(w.id)
	for _, ord := range []uint32{1, second} {
		if _, err := w.svc.ConfirmHide(t.Context(), w.viewer("mod"), w.id, ord, "spam"); codeOf(err) != problem.CodeConflict {
			t.Errorf("confirming #%d in an archived thread: %v, want conflict", ord, err)
		}
		if n := w.openFlags(ord); n != 3 {
			t.Errorf("#%d has %d open flags after the refused confirmation, want 3", ord, n)
		}
	}
}
