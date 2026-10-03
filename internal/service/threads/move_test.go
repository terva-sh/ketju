package threads

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"

	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for moving posts between threads (TKT-01M29CXJZZ). A move world is a
// queue world (flags_test.go, queue_test.go): the "general" forum with the
// flag world's thread, a second public forum "other" with a thread of one
// post, and a restricted forum "staff" only the admin can see. fmod
// moderates general through a group; omod moderates other through a group;
// bmod moderates both through one.

// AppendRun lets the flag world's writer move posts. fail is asked once per
// record, so a test can refuse a run by what it holds.
func (f *faultWriter) AppendRun(th *threadcache.Thread, run []threadlog.Append) ([]threadlog.Result, error) {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail != nil {
		for _, a := range run {
			if err := fail(a); err != nil {
				return nil, err
			}
		}
	}
	return f.inner.AppendRun(th, run)
}

// crashWriter writes the first keep records of every run and then fails, as
// a process that died part-way through writing it would leave the log.
// keep < 0 writes every run whole.
type crashWriter struct {
	inner posts.Writer
	mu    sync.Mutex
	keep  int
}

func (c *crashWriter) Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error) {
	return c.inner.Append(th, a)
}

func (c *crashWriter) AppendRun(th *threadcache.Thread, run []threadlog.Append) ([]threadlog.Result, error) {
	c.mu.Lock()
	keep := c.keep
	c.mu.Unlock()
	if keep < 0 || keep >= len(run) {
		return c.inner.AppendRun(th, run)
	}
	if keep > 0 {
		if _, err := c.inner.AppendRun(th, run[:keep]); err != nil {
			return nil, err
		}
	}
	return nil, errInjected
}

func (c *crashWriter) set(keep int) {
	c.mu.Lock()
	c.keep = keep
	c.mu.Unlock()
}

type moveWorld struct {
	*queueWorld
}

func newMoveWorld(t *testing.T) *moveWorld {
	t.Helper()
	w := &moveWorld{queueWorld: newQueueWorld(t)}
	for _, h := range []string{"omod", "bmod"} {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: h, DisplayName: h,
				Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			w.users[h] = id
			return err
		})
	}
	for _, g := range []struct {
		slug   string
		member string
		forums []int64
	}{{"other-mods", "omod", []int64{w.other}}, {"both-mods", "bmod", []int64{w.forum, w.other}}} {
		var group int64
		w.write(func(q *dbq.Queries) error {
			var err error
			group, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: g.slug, Slug: g.slug, CreatedAt: 1, UpdatedAt: 1})
			return err
		})
		if err := w.svc.Perm.AddMember(t.Context(), audit.By{}, group, w.users[g.member]); err != nil {
			t.Fatal(err)
		}
		for _, f := range g.forums {
			if err := w.svc.Perm.Grant(t.Context(), audit.By{}, f, group, domain.PermModerate); err != nil {
				t.Fatal(err)
			}
		}
	}
	return w
}

// postIn appends a post by author to thread id, replying to replyTo, and
// returns its ordinal.
func (w *moveWorld) postIn(id threadid.ID, author, src string, replyTo uint32) uint32 {
	w.t.Helper()
	w.appendTo(id, threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users[author]),
		Body: &threadlog.PostBody{Source: src, ReplyTo: replyTo}})
	return uint32(w.lenOf(id))
}

func (w *moveWorld) lenOf(id threadid.ID) int {
	w.t.Helper()
	var n int
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		n = th.Index.Len()
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *moveWorld) logOf(id threadid.ID) int64 {
	w.t.Helper()
	var n int64
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		n = th.Actor.LogLength()
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *moveWorld) entryOf(id threadid.ID, o uint32) threadlog.IndexEntry {
	w.t.Helper()
	var e threadlog.IndexEntry
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		e, err = th.Index.Entry(o)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return e
}

func (w *moveWorld) chainOf(id threadid.ID, o uint32) []threadlog.ChainRecord {
	w.t.Helper()
	var c []threadlog.ChainRecord
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		m, err := th.Reader.Chains([]uint32{o}, th.Actor.LogLength())
		c = m[o]
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *moveWorld) merge(who string, source threadid.ID, target threadid.ID, ords ...uint32) (MoveResult, error) {
	w.clock += 1000
	return w.svc.movePosts(w.t.Context(), w.as(who), MoveRequest{Source: source, Ordinals: ords, Target: target})
}

func (w *moveWorld) split(who string, source threadid.ID, forum, title string, ords ...uint32) (MoveResult, error) {
	w.clock += 1000
	return w.svc.movePosts(w.t.Context(), w.as(who), MoveRequest{Source: source, Ordinals: ords,
		Split: &NewThread{Forum: forum, Title: title}})
}

// lengths is both threads' log lengths, to prove a refusal wrote nothing.
func (w *moveWorld) lengths(ids ...threadid.ID) []int64 {
	var out []int64
	for _, id := range ids {
		out = append(out, w.logOf(id))
	}
	return out
}

// TestMergeCopiesChains: a merge copies each moved post's Post and Edits
// into the target, after its posts, keeping the original's author, time,
// flags and revisions; reply_to is renumbered within the moved set and
// dropped outside it; each copy names its origin and carries the copied
// flag. The source keeps every ordinal, each moved one flagged moved and
// served as deleted with no body (criteria 1 and 2).
func TestMergeCopiesChains(t *testing.T) {
	w := newMoveWorld(t)
	w.postIn(w.otherID, "author", "second in other", 0)
	p2 := w.postIn(w.id, "author", "first words", 0)
	w.appendTo(w.id, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: p2, Author: uint64(w.users["author"]),
		Body: &threadlog.EditBody{Source: "first words, edited", Reason: "typo"}})
	p3 := w.postIn(w.id, "rep1", "a reply to the first", p2)
	p4 := w.postIn(w.id, "rep2", "a reply to the opener", 1)
	before := w.lenOf(w.id)

	res, err := w.merge("mod", w.id, w.otherID, p4, p2, p3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != w.otherID || !slices.Equal(res.Moved, []uint32{p2, p3, p4}) || !slices.Equal(res.Copies, []uint32{3, 4, 5}) {
		t.Fatalf("the merge answered %+v", res)
	}
	if n := w.lenOf(w.id); n != before {
		t.Errorf("the source holds %d posts after the move, %d before: ordinals must not change", n, before)
	}
	for i, o := range res.Moved {
		src, cp := w.entryOf(w.id, o), w.entryOf(w.otherID, res.Copies[i])
		if !src.Flags.Has(threadlog.EntryMoved) {
			t.Errorf("source post %d has flags %#x, want moved", o, src.Flags)
		}
		if !cp.Flags.Has(threadlog.EntryCopied) || !cp.Flags.Visible() {
			t.Errorf("copy %d has flags %#x, want copied and shown", res.Copies[i], cp.Flags)
		}
		if cp.Author != src.Author || cp.CreatedMS != src.CreatedMS || cp.Revision != src.Revision {
			t.Errorf("copy %d is %+v; its original %d is %+v", res.Copies[i], cp, o, src)
		}
		chain := w.chainOf(w.otherID, res.Copies[i])
		if len(chain) == 0 || chain[0].Post == nil || chain[0].Post.Origin == nil ||
			*chain[0].Post.Origin != (threadlog.PostOrigin{ThreadID: uint64(w.id), Ordinal: o}) {
			t.Errorf("copy %d's chain is %+v: want a Post naming %s/%d", res.Copies[i], chain, w.id, o)
		}
		orig := w.chainOf(w.id, o)
		if len(chain) != len(orig) {
			t.Fatalf("copy %d carries %d records, its original %d", res.Copies[i], len(chain), len(orig))
		}
		for k := range chain {
			a, b := chain[k].Header, orig[k].Header
			if a.Actor != b.Actor || a.TimestampMS != b.TimestampMS || a.Revision != b.Revision || a.Flags != b.Flags {
				t.Errorf("copy %d record %d header %+v, original %+v", res.Copies[i], k, a, b)
			}
		}
	}
	if e := w.entryOf(w.otherID, 4); e.ReplyTo != 3 {
		t.Errorf("the copy of a reply to a moved post replies to %d, want 3, the copy of what it answered", e.ReplyTo)
	}
	if e := w.entryOf(w.otherID, 5); e.ReplyTo != 0 {
		t.Errorf("the copy of a reply to a post left behind replies to %d, want none", e.ReplyTo)
	}

	// The copy serves the source's current text and whole history.
	h, err := w.svc.History(t.Context(), w.as("rep1"), w.otherID, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Body.Revisions) != 2 || h.Body.Revisions[0].Source != "first words" ||
		h.Body.Revisions[1].Source != "first words, edited" || h.Body.Revisions[1].Reason != "typo" {
		t.Errorf("the copy's history is %+v", h.Body.Revisions)
	}

	// The source still answers every permalink, the moved posts as deleted
	// with no removal and no body, moderators included.
	for _, who := range []string{"rep1", "mod"} {
		page, err := w.svc.View(t.Context(), w.as(who), w.id, Query{PageSize: 50}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Body.Posts) != before {
			t.Fatalf("%s's source page has %d posts, want %d", who, len(page.Body.Posts), before)
		}
		for _, p := range page.Body.Posts {
			moved := slices.Contains(res.Moved, uint32(p.Ordinal))
			if moved && (!p.Deleted || p.HTML != "" || p.Removal != nil) {
				t.Errorf("%s sees moved post %d as %+v", who, p.Ordinal, p)
			}
		}
	}
}

// TestMoveNeedsModerateInBoth: a move between forums needs moderate in both,
// by role or through a group, and a refusal writes nothing to either log;
// within one forum, moderate there is enough.
func TestMoveNeedsModerateInBoth(t *testing.T) {
	w := newMoveWorld(t)
	sameForum := w.thread(w.forum, 9)
	cases := []struct {
		who    string
		target threadid.ID
		want   problem.Code
	}{
		{"mod", w.otherID, ""},
		{"bmod", w.otherID, ""},
		{"fmod", w.otherID, problem.CodeForbidden},
		{"omod", w.otherID, problem.CodeForbidden},
		{"rep1", w.otherID, problem.CodeForbidden},
		{"fmod", sameForum, ""},
	}
	for _, c := range cases {
		o := w.postIn(w.id, "author", "to move by "+c.who, 0)
		was := w.lengths(w.id, c.target)
		_, err := w.merge(c.who, w.id, c.target, o)
		if codeOf(err) != c.want {
			t.Errorf("%s moving into %s: %v, want %q", c.who, c.target, err, c.want)
		}
		if c.want != "" && !slices.Equal(w.lengths(w.id, c.target), was) {
			t.Errorf("%s's refused move wrote to a log", c.who)
		}
	}
	// A split into a forum the viewer does not moderate is refused the same
	// way, and opens no thread.
	o := w.postIn(w.id, "author", "to split", 0)
	was := w.lengths(w.id)
	if _, err := w.split("fmod", w.id, "other", "Split away", o); codeOf(err) != problem.CodeForbidden {
		t.Errorf("fmod's split into other: %v, want forbidden", err)
	}
	if !slices.Equal(w.lengths(w.id), was) {
		t.Error("a refused split wrote to the source")
	}
}

// TestMoveRefusesArchived: a move into or out of an archived thread is
// refused as conflict, moderators and admins included, and writes nothing.
func TestMoveRefusesArchived(t *testing.T) {
	for _, archived := range []string{"source", "target"} {
		t.Run(archived, func(t *testing.T) {
			w := newMoveWorld(t)
			o := w.postIn(w.id, "author", "stays put", 0)
			id := w.id
			if archived == "target" {
				id = w.otherID
			}
			if _, err := w.svc.Archive(t.Context(), w.as("admin"), id, "done"); err != nil {
				t.Fatal(err)
			}
			for _, who := range []string{"mod", "admin"} {
				was := w.lengths(w.id, w.otherID)
				if _, err := w.merge(who, w.id, w.otherID, o); codeOf(err) != problem.CodeConflict {
					t.Errorf("%s's move with the %s archived: %v, want conflict", who, archived, err)
				}
				if !slices.Equal(w.lengths(w.id, w.otherID), was) {
					t.Errorf("%s's refused move wrote to a log", who)
				}
			}
		})
	}
}

// TestMoveRefusesWhatIsNotShown: only posts shown as themselves move; an
// unknown post is not-found, and the request's own shape is checked first.
func TestMoveRefusesWhatIsNotShown(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "deleted soon", 0)
	if _, err := w.svc.Delete(t.Context(), w.as("author"), w.id, o, ""); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ords []uint32
		want problem.Code
	}{
		{"deleted", []uint32{o}, problem.CodeConflict},
		{"unknown", []uint32{99}, problem.CodeNotFound},
		{"none", nil, problem.CodeValidationFailed},
		{"twice", []uint32{1, 1}, problem.CodeValidationFailed},
	}
	for _, c := range cases {
		if _, err := w.merge("mod", w.id, w.otherID, c.ords...); codeOf(err) != c.want {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	if _, err := w.merge("mod", w.id, w.id, 1); codeOf(err) != problem.CodeBadRequest {
		t.Errorf("a merge into its own thread: %v, want bad-request", err)
	}
	// A post already moved does not move again.
	if _, err := w.merge("mod", w.id, w.otherID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := w.merge("mod", w.id, w.otherID, 1); codeOf(err) != problem.CodeConflict {
		t.Errorf("a second move of post 1: %v, want conflict", err)
	}
}

// TestSplitOpensThread: a split writes the new thread, by the moderator,
// with no continues, then moves the posts into it; the copies are read
// under the target forum's rules and by no other path. A reader of the
// source forum alone gets no body of the moved post from the source and
// not-found from the target.
func TestSplitOpensThread(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "SECRET-MOVED-TEXT", 0)
	quoting := w.postIn(w.id, "rep2", fmt.Sprintf("> [quote=#%d]\n> SECRET-MOVED-TEXT\n\nI agree", o), 0)
	res, err := w.split("admin", w.id, "staff", "Split off", o)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Copies, []uint32{1}) {
		t.Fatalf("the split answered %+v", res)
	}
	var created *threadlog.ThreadCreatedBody
	if err := w.cache.With(res.Target, func(th *threadcache.Thread) error {
		h, body, err := th.Reader.RecordAt(threadlog.FileHeaderSize, th.Actor.LogLength())
		if err != nil {
			return err
		}
		if h.Actor != uint64(w.users["admin"]) {
			t.Errorf("the new thread's ThreadCreated is by %d, want the admin", h.Actor)
		}
		created, _ = body.(*threadlog.ThreadCreatedBody)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if created == nil || created.Title != "Split off" || created.ForumID != uint64(w.staff) {
		t.Errorf("the new thread was created as %+v", created)
	}

	// The admin reads the copy in its new thread.
	page, err := w.svc.View(t.Context(), w.as("admin"), res.Target, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Body.Posts) != 1 || !strings.Contains(page.Body.Posts[0].HTML, "SECRET-MOVED-TEXT") {
		t.Errorf("the admin's page of the new thread is %+v", page.Body.Posts)
	}

	// rep1 reads general only, and moderators of general do not read staff.
	for _, who := range []string{"rep1", "mod", "fmod"} {
		if _, err := w.svc.View(t.Context(), w.as(who), res.Target, Query{}, ""); codeOf(err) != problem.CodeNotFound {
			t.Errorf("%s reading the new thread: %v, want not-found", who, err)
		}
		p, err := w.svc.Post(t.Context(), w.as(who), w.id, o, "")
		if err != nil {
			t.Fatal(err)
		}
		if !p.Body.Deleted || p.Body.HTML != "" {
			t.Errorf("%s reads the moved post as %+v", who, p.Body)
		}
		// A quote of the moved post is a tombstone: no text, no link.
		q, err := w.svc.Post(t.Context(), w.as(who), w.id, quoting, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(q.Body.HTML, "SECRET-MOVED-TEXT") || !strings.Contains(q.Body.HTML, "I agree") {
			t.Errorf("%s reads a quote of the moved post as:\n%s", who, q.Body.HTML)
		}
		h, err := w.svc.History(t.Context(), w.as(who), w.id, o, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(h.Body.Revisions) != 0 {
			t.Errorf("%s reads the moved post's history: %+v", who, h.Body.Revisions)
		}
		if _, err := w.svc.Source(t.Context(), w.as(who), w.id, o); err == nil {
			t.Errorf("%s read the moved post's source", who)
		}
		r, err := w.svc.Replies(t.Context(), w.as(who), w.id, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range r.Body.Posts {
			if strings.Contains(p.HTML, "SECRET-MOVED-TEXT") {
				t.Errorf("%s reads the moved post among replies", who)
			}
		}
		up, err := w.svc.UserPosts(t.Context(), w.as(who), "author", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range up.Posts {
			if strings.Contains(p.Post.HTML, "SECRET-MOVED-TEXT") {
				t.Errorf("%s reads the moved post or its copy in the author's posts: %+v", who, p)
			}
		}
	}
}

// crashed builds a move world whose writer keeps the first keep records of
// the move's target run, and moves posts 2 and 3 of the general thread,
// post 2 edited, into the other thread. It answers the world and the
// move's error, which must be unavailable.
func crashed(t *testing.T, keep int) (*moveWorld, *crashWriter) {
	t.Helper()
	w := newMoveWorld(t)
	p2 := w.postIn(w.id, "author", "one", 0)
	w.appendTo(w.id, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: p2, Author: uint64(w.users["author"]),
		Body: &threadlog.EditBody{Source: "one, edited"}})
	w.postIn(w.id, "rep1", "two", p2)
	cw := &crashWriter{keep: keep}
	w.svc.Writer = cw
	_, err := w.merge("mod", w.id, w.otherID, 2, 3)
	if codeOf(err) != problem.CodeUnavailable {
		t.Fatalf("a move whose target run failed answered %v, want unavailable", err)
	}
	cw.set(-1)
	return w, cw
}

// checkFinished holds the other thread to the finished move: posts 2 and
// 3 are the copies of the general thread's 2 and 3, each at its original's
// revision, with one PostsMergedIn ahead of them.
func (w *moveWorld) checkFinished() {
	w.t.Helper()
	if n := w.lenOf(w.otherID); n != 3 {
		w.t.Fatalf("the target holds %d posts, want 3", n)
	}
	for _, o := range []uint32{2, 3} {
		src, cp := w.entryOf(w.id, o), w.entryOf(w.otherID, o)
		if !cp.Flags.Has(threadlog.EntryCopied) || cp.Revision != src.Revision {
			w.t.Errorf("copy %d is %+v, its original %+v", o, cp, src)
		}
	}
	merged := 0
	for _, r := range w.scan(w.otherID) {
		if r.Header.Type == threadlog.TypePostsMergedIn {
			merged++
		}
	}
	if merged != 1 {
		w.t.Errorf("the target holds %d PostsMergedIn, want 1", merged)
	}
}

func (w *moveWorld) finish(want int) {
	w.t.Helper()
	n, err := w.svc.FinishMoves(w.t.Context())
	if err != nil || n != want {
		w.t.Fatalf("FinishMoves finished %d moves, %v; want %d", n, err, want)
	}
}

// TestMoveCrashAfterSourceRecordFinishes: the source's PostsSplitOut is
// written and nothing of the target's run; the posts are hidden in the
// source and not yet in the target, and FinishMoves completes the move from
// the logs.
func TestMoveCrashAfterSourceRecordFinishes(t *testing.T) {
	w, _ := crashed(t, 0)
	if n := w.lenOf(w.otherID); n != 1 {
		t.Fatalf("the target holds %d posts before the finish, want 1", n)
	}
	if !w.entryOf(w.id, 2).Flags.Has(threadlog.EntryMoved) {
		t.Fatal("the source post is not moved")
	}
	w.finish(1)
	w.checkFinished()
}

// TestMoveCrashMidRunFinishes: the PostsMergedIn and the first copy with
// its Edit are on disk, the second copy is not.
func TestMoveCrashMidRunFinishes(t *testing.T) {
	w, _ := crashed(t, 3)
	if n := w.lenOf(w.otherID); n != 2 {
		t.Fatalf("the target holds %d posts before the finish, want 2", n)
	}
	w.finish(1)
	w.checkFinished()
}

// TestMoveCrashBetweenPostAndEditsFinishes: the first copy's Post is on
// disk without its Edit; the finisher appends the Edit, and the copy serves
// the source's current revision and its whole history.
func TestMoveCrashBetweenPostAndEditsFinishes(t *testing.T) {
	w, _ := crashed(t, 2)
	if e := w.entryOf(w.otherID, 2); e.Revision != 1 {
		t.Fatalf("the half-copied post is at revision %d before the finish, want 1", e.Revision)
	}
	w.finish(1)
	w.checkFinished()
	h, err := w.svc.History(t.Context(), w.as("rep1"), w.otherID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Body.Revisions) != 2 || h.Body.Revisions[1].Source != "one, edited" {
		t.Errorf("the finished copy's history is %+v", h.Body.Revisions)
	}
}

// TestFinishMovesIdempotent: a finished move is never written twice; a
// second pass writes no byte, and so does a pass over a move that never
// failed.
func TestFinishMovesIdempotent(t *testing.T) {
	w, _ := crashed(t, 2)
	w.finish(1)
	was := w.lengths(w.id, w.otherID)
	w.finish(0)
	if !slices.Equal(w.lengths(w.id, w.otherID), was) {
		t.Error("a second FinishMoves wrote to a log")
	}
	// Finishing a finished move directly writes nothing either.
	if _, err := w.svc.finishMove(t.Context(), w.id, w.splitOffset(w.id)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.lengths(w.id, w.otherID), was) {
		t.Error("finishMove on a finished move wrote to a log")
	}
}

// splitOffset is the offset of the first PostsSplitOut in id's log.
func (w *moveWorld) splitOffset(id threadid.ID) int64 {
	w.t.Helper()
	for _, r := range w.scan(id) {
		if r.Header.Type == threadlog.TypePostsSplitOut {
			return r.Offset
		}
	}
	w.t.Fatalf("%s holds no PostsSplitOut", id)
	return 0
}

// scan is every frame of id's log, read from its file.
func (w *moveWorld) scan(id threadid.ID) []threadlog.ScanRecord {
	w.t.Helper()
	f, err := os.ReadFile(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []threadlog.ScanRecord
	if err := threadlog.ScanFrames(bytes.NewReader(f), int64(len(f)), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			w.t.Fatal(r.Err)
		}
		out = append(out, r)
		return true
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestUnfinishedCopyRefusesEdit: while a move is unfinished an Edit of its
// copy is refused, since it would take a revision a carried Edit needs; a
// moderator's hide is not, since it takes none. Once finished, the copy
// takes edits again and its move stays finished.
func TestUnfinishedCopyRefusesEdit(t *testing.T) {
	w, _ := crashed(t, 2)
	if _, err := w.svc.Edit(t.Context(), w.as("mod"), w.otherID, 2, EditRequest{Source: "too soon"}); codeOf(err) != problem.CodeConflict {
		t.Fatalf("an edit of an unfinished copy: %v, want conflict", err)
	}
	w.finish(1)
	if _, err := w.svc.Edit(t.Context(), w.as("mod"), w.otherID, 2, EditRequest{Source: "after the move"}); err != nil {
		t.Fatal(err)
	}
	w.finish(0)
	if err := w.svc.moveUnfinished(t.Context(), w.otherID); err != nil {
		t.Errorf("an edited finished copy reads as unfinished: %v", err)
	}
}

// TestArchiveWaitsForMove: a thread with an unfinished move into or out of
// it is not archived, so the finisher never finds its target sealed.
func TestArchiveWaitsForMove(t *testing.T) {
	w, _ := crashed(t, 0)
	for _, id := range []threadid.ID{w.id, w.otherID} {
		if _, err := w.svc.Archive(t.Context(), w.as("admin"), id, "closing"); codeOf(err) != problem.CodeConflict {
			t.Errorf("archiving %s mid-move: %v, want conflict", id, err)
		}
	}
	w.finish(1)
	if _, err := w.svc.Archive(t.Context(), w.as("admin"), w.otherID, "closing"); err != nil {
		t.Errorf("archiving the target once the move finished: %v", err)
	}
}

// TestMoveSerializesWithThreadMove: a thread move holds the move lock, so a
// split decided behind it is decided against the forum the thread moved
// to. fmod moderates general only, and the thread leaves general.
func TestMoveSerializesWithThreadMove(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "moving", 0)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	w.writer.set(func(a threadlog.Append) {
		if a.Type == threadlog.TypeThreadMoved {
			once.Do(func() { close(entered) })
			<-release
		}
	}, nil)
	sameForum := w.thread(w.forum, 9)
	moved := make(chan error, 1)
	go func() {
		_, err := w.svc.Move(t.Context(), w.as("mod"), w.id, "other")
		moved <- err
	}()
	<-entered
	splitErr := make(chan error, 1)
	go func() {
		_, err := w.merge("fmod", w.id, sameForum, o)
		splitErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	if err := <-moved; err != nil {
		t.Fatal(err)
	}
	if err := <-splitErr; codeOf(err) != problem.CodeForbidden {
		t.Errorf("fmod's move out of a thread that left their forum: %v, want forbidden", err)
	}
	if w.entryOf(w.id, o).Flags.Has(threadlog.EntryMoved) {
		t.Error("the post moved")
	}
}

// TestSlowModeUsesNewestTime: a copy keeps its original's older time at a
// later ordinal, so slow mode measures from the author's newest post by
// time, not their last ordinal.
func TestSlowModeUsesNewestTime(t *testing.T) {
	w := newMoveWorld(t)
	old := w.postIn(w.id, "author", "old", 0)
	w.clock += 3_600_000
	w.postIn(w.otherID, "author", "new", 0)
	newest := w.clock
	if _, err := w.merge("mod", w.id, w.otherID, old); err != nil {
		t.Fatal(err)
	}
	var wait time.Duration
	if err := w.cache.With(w.otherID, func(th *threadcache.Thread) error {
		var err error
		wait, err = slowModeWait(th.Index, w.users["author"], 60, time.UnixMilli(newest+1000))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if wait != 59*time.Second {
		t.Errorf("slow mode waits %s, want 59s from the newest post", wait)
	}
}

// TestUnfinishedCopyDoesNotMoveOn (Terva review 2007, finding 1): a copy
// whose move is unfinished does not move again, since the actor would then
// refuse the carried Edits it still owes and the first move could never
// finish. Once finished, it moves like any post.
func TestUnfinishedCopyDoesNotMoveOn(t *testing.T) {
	w, _ := crashed(t, 2)
	third := w.thread(w.other, 9)
	was := w.lengths(w.otherID, third)
	if _, err := w.merge("mod", w.otherID, third, 2); codeOf(err) != problem.CodeConflict {
		t.Fatalf("moving an unfinished copy on: %v, want conflict", err)
	}
	if !slices.Equal(w.lengths(w.otherID, third), was) {
		t.Error("a refused move wrote to a log")
	}
	w.finish(1)
	w.checkFinished()
	if _, err := w.merge("mod", w.otherID, third, 2); err != nil {
		t.Fatalf("moving a finished copy on: %v", err)
	}
}

// TestArchiveWaitsForUnprojectedMove (Terva review 2007, finding 2): a move
// whose source record is written but not projected, so post_moves does not
// name it, still keeps its target from being archived until it finishes.
func TestArchiveWaitsForUnprojectedMove(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "moving", 0)
	w.writer.set(func(a threadlog.Append) {
		if a.Type == threadlog.TypePostsSplitOut {
			w.projectMu.Lock()
			w.failProject = true
			w.projectMu.Unlock()
		}
	}, nil)
	if _, err := w.merge("mod", w.id, w.otherID, o); codeOf(err) != problem.CodeUnavailable {
		t.Fatalf("a move whose projection failed: %v, want unavailable", err)
	}
	w.writer.set(nil, nil)
	w.projectMu.Lock()
	w.failProject = false
	w.projectMu.Unlock()
	if !w.entryOf(w.id, o).Flags.Has(threadlog.EntryMoved) {
		t.Fatal("the source record was not written")
	}
	if _, err := w.svc.Archive(t.Context(), w.as("admin"), w.otherID, "closing"); codeOf(err) != problem.CodeConflict {
		t.Fatalf("archiving the target of an unprojected move: %v, want conflict", err)
	}
	w.finish(1)
	if _, err := w.svc.Archive(t.Context(), w.as("admin"), w.otherID, "closing"); err != nil {
		t.Errorf("archiving the target once the move finished: %v", err)
	}
}

// TestMoveNamesOneDestination (Terva review 2018): a request naming both a
// target and a new thread is refused before anything is written, so a split
// into a forum the viewer moderates cannot carry posts into a thread they
// were never checked against; nor is a request naming neither accepted.
func TestMoveNamesOneDestination(t *testing.T) {
	w := newMoveWorld(t)
	o := w.postIn(w.id, "author", "STAYS-HERE", 0)
	was := w.lengths(w.id, w.staffID)
	_, err := w.svc.movePosts(t.Context(), w.as("fmod"), MoveRequest{Source: w.id, Ordinals: []uint32{o},
		Target: w.staffID, Split: &NewThread{Forum: "general", Title: "Looks harmless"}})
	if codeOf(err) != problem.CodeBadRequest {
		t.Errorf("a move naming a target and a new thread: %v, want bad-request", err)
	}
	if _, err := w.svc.movePosts(t.Context(), w.as("mod"), MoveRequest{Source: w.id, Ordinals: []uint32{o}}); codeOf(err) != problem.CodeBadRequest {
		t.Errorf("a move naming no destination: %v, want bad-request", err)
	}
	if !slices.Equal(w.lengths(w.id, w.staffID), was) {
		t.Error("a refused move wrote to a log")
	}
	if w.entryOf(w.id, o).Flags.Has(threadlog.EntryMoved) {
		t.Error("the post moved")
	}
}

// TestCopyDropsAReplyEditedOutside (Terva review 2020): a post that first
// replied to a post moved with it, then was edited to reply to one left
// behind, is copied replying to nothing. An Edit's zero keeps the post's
// reply, so renumbering each record alone would leave the copy replying
// to the copy of the post it no longer answers.
func TestCopyDropsAReplyEditedOutside(t *testing.T) {
	w := newMoveWorld(t)
	p2 := w.postIn(w.id, "author", "first", 0)
	p3 := w.postIn(w.id, "rep1", "a reply to the first", p2)
	w.appendTo(w.id, threadlog.Append{Type: threadlog.TypeEdit, Ordinal: p3, Author: uint64(w.users["rep1"]),
		Body: &threadlog.EditBody{Source: "now a reply to the opener", ReplyTo: 1}})
	if e := w.entryOf(w.id, p3); e.ReplyTo != 1 {
		t.Fatalf("the source post replies to %d, want 1", e.ReplyTo)
	}
	res, err := w.merge("mod", w.id, w.otherID, p2, p3)
	if err != nil {
		t.Fatal(err)
	}
	if e := w.entryOf(w.otherID, res.Copies[1]); e.ReplyTo != 0 {
		t.Errorf("the copy replies to %d, want nothing: its post now answers one left behind", e.ReplyTo)
	}
}
