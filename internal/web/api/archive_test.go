package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for archiving a thread (TKT-01M29CRJ64): docs/07 "Archiving
// threads", docs/08 "Locking semantics", docs/15 "Invariants, added".
// Archived is read-only for everyone, and v1 has no unarchive.

// archiveWorld is forumModWorld with an admin, dora, and the viewers the
// archive tests act as.
func archiveWorld(t *testing.T) (*world, map[string]domain.Viewer) {
	t.Helper()
	w, gina, _, _, _ := forumModWorld(t)
	return w, map[string]domain.Viewer{
		"alice": w.viewer("alice"), "carol": w.viewer("carol"), "bob": w.viewer("bob"),
		"gina": gina, "dora": w.newAdmin(), "scribe": w.viewer("scribe"),
	}
}

func archivePath(id threadid.ID) string { return modPath(id, "archive") }

// lastRecord is the type and body of the last record in id's log.
func (w *world) lastRecord(id threadid.ID) (threadlog.FrameHeader, any) {
	w.t.Helper()
	var h threadlog.FrameHeader
	var body any
	hs := w.logHeaders(id)
	h = hs[len(hs)-1]
	for _, f := range w.frames(id, h.Ordinal) {
		if f.h == h {
			body = f.body
		}
	}
	return h, body
}

// TestArchiveThread: an admin archives with a reason. The log ends in one
// ThreadArchived holding it, one thread.archived row names the admin, the
// projection row is archived, and the page then offers nobody a reply.
func TestArchiveThread(t *testing.T) {
	w, vs := archiveWorld(t)
	id := w.threadIn(10, w.forum, "Closed history", "alice")
	rows := len(w.auditLog())
	h := header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id), `{"reason":"finished"}`))
	if h.ID != id.String() {
		t.Errorf("the answer is the header of %s", h.ID)
	}
	hd, body := w.lastRecord(id)
	if hd.Type != threadlog.TypeThreadArchived || hd.Actor != uint64(vs["dora"].UserID) {
		t.Errorf("the last record is %v by %d, want dora's ThreadArchived", hd.Type, hd.Actor)
	}
	if b, ok := body.(*threadlog.ThreadArchivedBody); !ok || b.Reason != "finished" {
		t.Errorf("the ThreadArchived body is %#v", body)
	}
	log := w.auditLog()
	if len(log) != rows+1 {
		t.Fatalf("%d audit rows written, want 1", len(log)-rows)
	}
	if got := log[len(log)-1]; got.action != projection.ActionThreadArchived || got.actor.Int64 != vs["dora"].UserID ||
		got.typ != "thread" || got.id != id.String() || got.details["reason"] != "finished" {
		t.Errorf("the audit row is %+v", got)
	}
	if row := w.threadRow(id); row.Archived != 1 {
		t.Errorf("the projection row's archived is %d", row.Archived)
	}
	if !w.cacheArchived(id) {
		t.Error("the cache does not know the thread is archived")
	}

	// No body at all is an archive with no reason.
	id2 := w.threadIn(11, w.forum, "Closed quietly", "alice")
	header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id2), ``))
	if _, body := w.lastRecord(id2); body.(*threadlog.ThreadArchivedBody).Reason != "" {
		t.Errorf("an archive with no body recorded %#v", body)
	}
}

// cacheArchived is what the thread cache says of id.
func (w *world) cacheArchived(id threadid.ID) bool {
	w.t.Helper()
	var out bool
	if err := w.cache.With(id, func(th *threadcache.Thread) error { out = th.Archived(); return nil }); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestArchiveRefusals: each refusal, at the gate or at the service, writes
// nothing to the log or to audit_log.
func TestArchiveRefusals(t *testing.T) {
	w, vs := archiveWorld(t)
	id := w.threadIn(12, w.forum, "Refusals", "alice")
	h := w.chain()
	for _, c := range []struct{ name, who, body, want string }{
		{"anonymous", "", ``, "unauthenticated"},
		{"a pre-user", "pre-user", ``, "forbidden"},
		{"a member", "carol", ``, "forbidden"},
		{"the thread's author", "alice", ``, "forbidden"},
		{"a moderator by role", "bob", ``, "forbidden"},
		{"a moderator through a group", "gina", ``, "forbidden"},
		{"an agent", "scribe", ``, "forbidden"},
	} {
		w.unchanged(t, id, c.name, c.want, func() string {
			return problemCode(t, send(h, c.who, http.MethodPost, archivePath(id), c.body))
		})
	}
	// The service refuses everyone but an admin itself, whatever the gate
	// admitted.
	for _, who := range []string{"alice", "carol", "bob", "gina", "scribe"} {
		w.unchanged(t, id, who+" through the service", "forbidden", func() string {
			_, err := w.svc.Archive(t.Context(), vs[who], id, "")
			return string(problem.FromError(err))
		})
	}
	dora := vs["dora"]
	for _, c := range []struct{ name, path, body, want string }{
		{"a body that is not JSON", archivePath(id), `reason`, "bad-request"},
		{"an unknown field", archivePath(id), `{"reason":"x","why":1}`, "bad-request"},
		{"an unknown thread", archivePath(threadid.ID(uint64(1_757_500_000_000)<<16 | 999)), ``, "not-found"},
		{"a malformed id", "/api/v1/admin/threads/nope/archive", ``, "bad-request"},
		{"a reason over 500 characters", archivePath(id), `{"reason":"` + strings.Repeat("é", 501) + `"}`, "validation-failed"},
	} {
		w.unchanged(t, id, c.name, c.want, func() string { return problemCode(t, w.sendAs(dora, http.MethodPost, c.path, c.body)) })
	}
	header(t, w.sendAs(dora, http.MethodPost, archivePath(id), `{"reason":"once"}`))
	w.unchanged(t, id, "archiving an archived thread", "conflict", func() string {
		return problemCode(t, w.sendAs(dora, http.MethodPost, archivePath(id), ``))
	})
}

// threadAct is one write a viewer might try on a thread, with what the
// thread needs beforehand for it to make sense.
type threadAct struct {
	name, method string
	path         func(id threadid.ID, p archivePosts) string
	body         string
	prepare      string // "lock", "pin" or "" for none
}

// archivePosts is the ordinals prepareArchive wrote: each viewer's own post,
// and their own post they deleted; carol's post bob hid; and bob's post
// carol flagged.
type archivePosts struct {
	own, gone map[string]int
	hidden    int
	flagged   int
}

var threadActs = []threadAct{
	{"reply", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return postsPath(id) }, `{"source":"still here"}`, ""},
	{"own edit", http.MethodPatch, nil, `{"source":"changed"}`, ""},
	{"own delete", http.MethodDelete, nil, ``, ""},
	{"own restore", http.MethodPost, nil, ``, ""},
	{"edit of alice's post", http.MethodPatch, func(id threadid.ID, _ archivePosts) string { return postPathN(id, 1) }, `{"source":"moderated","reason":"tone"}`, ""},
	{"hide of alice's post", http.MethodDelete, func(id threadid.ID, _ archivePosts) string { return postPathN(id, 1) }, `{"reason":"tone"}`, ""},
	{"restore of the hidden post", http.MethodPost, func(id threadid.ID, p archivePosts) string { return postPathN(id, p.hidden) + "/restore" }, ``, ""},
	{"flag", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return flagsPath(id) }, `{"ordinal":1,"reason":"spam"}`, ""},
	{"queue resolve", http.MethodPost, func(id threadid.ID, p archivePosts) string { return queuePostPath(id, p.flagged) }, `{"action":"dismiss"}`, ""},
	{"lock", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return modPath(id, "lock") }, ``, ""},
	{"unlock", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return modPath(id, "unlock") }, ``, "lock"},
	{"pin", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return modPath(id, "pin") }, `{"scope":"forum"}`, ""},
	{"unpin", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return modPath(id, "unpin") }, `{"scope":"forum"}`, "pin"},
	{"move", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return modPath(id, "move") }, moveTo("den"), ""},
	{"rename", http.MethodPatch, func(id threadid.ID, _ archivePosts) string { return threadPath(id) }, rename("A new title"), ""},
	{"slow mode", http.MethodPut, func(id threadid.ID, _ archivePosts) string { return modPath(id, "slow-mode") }, `{"seconds":30}`, ""},
	{"retag", http.MethodPut, func(id threadid.ID, _ archivePosts) string { return modPath(id, "tags") }, retag("history"), ""},
	{"archive", http.MethodPost, func(id threadid.ID, _ archivePosts) string { return archivePath(id) }, ``, ""},
}

// actorOrder is every viewer the read-only invariant is held against.
var actorOrder = []string{"alice", "carol", "bob", "gina", "dora", "scribe"}

// prepareArchive makes thread seq in general, started by alice, holding a
// post of each viewer's and one each deleted, carol's post hidden by bob, and
// bob's post flagged by carol, then locked or pinned as prepare says, and
// archived by dora when archive is set.
func (w *world) prepareArchive(vs map[string]domain.Viewer, seq uint64, prepare string, archive bool) (threadid.ID, archivePosts) {
	w.t.Helper()
	id := w.threadIn(seq, w.forum, fmt.Sprintf("Thread %d", seq), "alice")
	p := archivePosts{own: map[string]int{"alice": 1}, gone: map[string]int{}}
	n := 1
	add := func(who string) int {
		n++
		a := threadlog.Append{Type: threadlog.TypePost, Author: uint64(vs[who].UserID), Body: &threadlog.PostBody{Source: who + "'s post"}}
		if who == "scribe" {
			a.Flags = threadlog.FrameFlagMachineAuthored
		}
		w.append(id, a)
		return n
	}
	for _, who := range actorOrder {
		if who != "alice" {
			p.own[who] = add(who)
		}
		p.gone[who] = add(who)
		w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(vs[who].UserID), Ordinal: uint32(p.gone[who]),
			Body: &threadlog.DeleteBody{}})
	}
	p.hidden = add("carol")
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(vs["bob"].UserID), Ordinal: uint32(p.hidden),
		Body: &threadlog.DeleteBody{Reason: "tone", ByModerator: true}})
	p.flagged = p.own["bob"]
	if rec := w.sendAs(vs["carol"], http.MethodPost, flagsPath(id), fmt.Sprintf(`{"ordinal":%d,"reason":"spam"}`, p.flagged)); rec.Code != http.StatusOK {
		w.t.Fatalf("carol's flag: %d %s", rec.Code, rec.Body)
	}
	switch prepare {
	case "lock":
		header(w.t, w.sendAs(vs["bob"], http.MethodPost, modPath(id, "lock"), ``))
	case "pin":
		header(w.t, w.sendAs(vs["bob"], http.MethodPost, modPath(id, "pin"), `{"scope":"forum"}`))
	}
	if archive {
		header(w.t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id), `{"reason":"done"}`))
	}
	return id, p
}

// TestArchivedThreadIsReadOnlyToEveryone is docs/15's invariant: each
// viewer, from the thread's author to an admin and an agent, tries every
// write on an archived thread, and on a live twin prepared the same way.
// What the viewer may do on the live twin is conflict on the archived
// thread; what the live twin refuses, the archived thread refuses as well,
// with the same code or conflict. Either way the archived thread's log, its
// audit rows and its slow mode are as they were.
func TestArchivedThreadIsReadOnlyToEveryone(t *testing.T) {
	w, vs := archiveWorld(t)
	seq := uint64(100)
	allowedLive := 0
	for _, act := range threadActs {
		for _, who := range actorOrder {
			path := act.path
			switch act.name {
			case "own edit", "own delete":
				path = func(id threadid.ID, p archivePosts) string { return postPathN(id, p.own[who]) }
			case "own restore":
				path = func(id threadid.ID, p archivePosts) string { return postPathN(id, p.gone[who]) + "/restore" }
			}
			name := fmt.Sprintf("%s's %s", who, act.name)

			live, lp := w.prepareArchive(vs, seq, act.prepare, false)
			arch, ap := w.prepareArchive(vs, seq+1, act.prepare, true)
			seq += 2
			liveRec := w.sendAs(vs[who], act.method, path(live, lp), act.body)
			liveOK := liveRec.Code < 300
			want := "conflict"
			if liveOK {
				allowedLive++
			} else {
				want = problemCode(t, liveRec)
			}
			slow := w.slowModeRow(arch)
			before, rows := w.logState(arch), len(w.auditLog())
			rec := w.sendAs(vs[who], act.method, path(arch, ap), act.body)
			got := ""
			if rec.Code >= 300 {
				got = problemCode(t, rec)
			}
			if got != "conflict" && (liveOK || got != want) {
				t.Errorf("%s: %d %s on the archived thread, want conflict (live: %d %s)", name, rec.Code, got, liveRec.Code, want)
			}
			if after := w.logState(arch); after != before {
				t.Errorf("%s changed the archived thread's log: %+v then %+v", name, before, after)
			}
			if got := len(w.auditLog()); got != rows {
				t.Errorf("%s wrote %d audit rows", name, got-rows)
			}
			if got := w.slowModeRow(arch); got != slow {
				t.Errorf("%s changed the slow mode row: %s then %s", name, slow, got)
			}
		}
	}
	// The table must exercise the invariant: most writes are open to
	// someone on a live thread.
	if allowedLive < 40 {
		t.Errorf("only %d of the writes passed on a live thread; the table is not testing much", allowedLive)
	}
}

// TestArchiveRacingReplyNeverLandsAfter: twenty members reply while an admin
// archives. No record follows the ThreadArchived in the log, and every reply
// either landed before it or answered conflict.
func TestArchiveRacingReplyNeverLandsAfter(t *testing.T) {
	for round := range 5 {
		w, vs := archiveWorld(t)
		id := w.threadIn(uint64(200+round), w.forum, "Racing", "alice")
		var wg sync.WaitGroup
		codes := make([]string, 20)
		start := make(chan struct{})
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rec := w.sendAs(vs[[]string{"carol", "alice", "scribe"}[i%3]], http.MethodPost, postsPath(id), `{"source":"racing the archive"}`)
				if rec.Code != http.StatusCreated {
					codes[i] = problemCode(t, rec)
				}
			}()
		}
		var archiveRec *httptest.ResponseRecorder
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			archiveRec = w.sendAs(vs["dora"], http.MethodPost, archivePath(id), ``)
		}()
		close(start)
		wg.Wait()
		if archiveRec.Code != http.StatusOK {
			t.Fatalf("round %d: the archive answered %d %s", round, archiveRec.Code, archiveRec.Body)
		}
		hs := w.logHeaders(id)
		if hs[len(hs)-1].Type != threadlog.TypeThreadArchived {
			t.Fatalf("round %d: the log ends in a %v after its ThreadArchived", round, hs[len(hs)-1].Type)
		}
		for i, c := range codes {
			if c != "" && c != "conflict" {
				t.Errorf("round %d: reply %d answered %s", round, i, c)
			}
		}
	}
}

// TestArchiveDropsThreadFromCacheFirst: once archived through the service,
// the thread is evicted before any live one when the cache is over its
// bound, however recently it was used.
func TestArchiveDropsThreadFromCacheFirst(t *testing.T) {
	w, vs := archiveWorld(t)
	w.bounded(2)
	live := w.threadIn(300, w.forum, "Live", "alice")
	gone := w.threadIn(301, w.forum, "Archived", "alice")
	header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(gone), ``))
	// The archived thread is the most recently used, and live the least.
	for _, id := range []threadid.ID{live, gone} {
		if err := w.cache.With(id, func(*threadcache.Thread) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	w.threadIn(302, w.forum, "Third", "alice") // over the bound
	if !w.isOpen(live) {
		t.Error("the live thread was evicted")
	}
	if w.isOpen(gone) {
		t.Error("the archived thread is still open; it should go first")
	}
}

// TestArchivedViewerBlockOffersNothing: on an archived thread nobody is
// offered a reply or an upload, and the author no rename, while watching,
// muting and the read position still work.
func TestArchivedViewerBlockOffersNothing(t *testing.T) {
	w, vs := archiveWorld(t)
	id := w.threadIn(400, w.forum, "Offers nothing", "alice")
	w.post(id, "carol", "a reply", 0)
	if p := page(t, w.get(vs["alice"], threadPath(id))); !p.Viewer.CanReply || p.Viewer.RenameUntil == nil {
		t.Fatalf("before the archive the author is offered %+v", p.Viewer)
	}
	header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id), ``))
	for _, who := range []string{"alice", "carol", "bob", "gina", "dora", "scribe"} {
		p := page(t, w.get(vs[who], threadPath(id)))
		if p.Viewer.CanReply || p.Viewer.CanUpload || p.Viewer.RenameUntil != nil {
			t.Errorf("%s is offered %+v on an archived thread", who, p.Viewer)
		}
	}
	carol := vs["carol"]
	if rec := w.sendAs(carol, http.MethodPost, threadPath(id)+"/watch", `{"level":"watch"}`); rec.Code != http.StatusOK {
		t.Errorf("watching an archived thread: %d %s", rec.Code, rec.Body)
	}
	if rec := w.sendAs(carol, http.MethodPost, threadPath(id)+"/watch", `{"level":"mute"}`); rec.Code != http.StatusOK {
		t.Errorf("muting an archived thread: %d %s", rec.Code, rec.Body)
	}
	if rec := w.sendAs(carol, http.MethodPut, threadPath(id)+"/read", `{"ordinal":2}`); rec.Code != http.StatusOK {
		t.Errorf("setting the read position on an archived thread: %d %s", rec.Code, rec.Body)
	}
	if p := page(t, w.get(carol, threadPath(id))); p.Viewer.LastReadOrdinal != 2 || p.Viewer.Watch != "mute" {
		t.Errorf("carol's place in the archived thread is %+v", p.Viewer)
	}
}

// TestArchiveMovesTheResponseCacheKey: the page cached before the archive is
// not served after it, for any viewer class.
func TestArchiveMovesTheResponseCacheKey(t *testing.T) {
	w, vs := archiveWorld(t)
	id, _ := w.seed()
	path := threadPath(id)
	classes := []string{"", "carol", "bob"}
	for _, who := range classes {
		w.same(w.viewer(who), path)
	}
	h := header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id), ``))
	before := w.svc.Responses.Stats()
	for _, who := range classes {
		rec := w.get(w.viewer(who), path)
		p := page(t, rec)
		if p.Thread.LogLength != h.LogLength || p.Viewer.CanReply {
			t.Errorf("after the archive %q got the header %+v and viewer %+v", who, p.Thread, p.Viewer)
		}
		if tag := rec.Header().Get("ETag"); !strings.HasPrefix(tag, fmt.Sprintf(`"%s-%d-`, id, h.LogLength)) {
			t.Errorf("after the archive %q got ETag %s, not one under the new log length %d", who, tag, h.LogLength)
		}
	}
	if s := w.svc.Responses.Stats(); s.Misses != before.Misses+3 || s.Hits != before.Hits {
		t.Errorf("after the archive the three classes' pages were not fresh draws: %+v then %+v", before, s)
	}
}

// TestArchivedThreadStaysReadable: a reader still reads the archived
// thread, a post, its history, and finds it in the forum's list and in
// /participated.
func TestArchivedThreadStaysReadable(t *testing.T) {
	w, vs := archiveWorld(t)
	id := w.threadIn(500, w.forum, "Still readable", "alice")
	w.post(id, "carol", "carol was here", 0)
	header(t, w.sendAs(vs["dora"], http.MethodPost, archivePath(id), ``))
	carol := vs["carol"]
	p := page(t, w.get(carol, threadPath(id)))
	if len(p.Posts) != 2 {
		t.Errorf("the archived page has %d posts", len(p.Posts))
	}
	for _, path := range []string{postPathN(id, 2), postPathN(id, 2) + "/history"} {
		if rec := w.get(carol, path); rec.Code != http.StatusOK {
			t.Errorf("GET %s on an archived thread: %d %s", path, rec.Code, rec.Body)
		}
	}
	found := false
	for _, r := range decode[dto.ThreadList](t, w.get(carol, threadsPath("general"))).Threads {
		found = found || r.ID == id.String()
	}
	if !found {
		t.Error("the archived thread is not in the forum's list")
	}
	if ids := w.crossIDs("carol", "participated"); !containsString(ids, id.String()) {
		t.Errorf("the archived thread is not in carol's /participated: %v", ids)
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// bounded replaces w's thread cache with one holding at most n threads.
func (w *world) bounded(n int) {
	w.t.Helper()
	c, err := threadcache.New(w.root, threadcache.Options{Bound: n, Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { c.Close() })
	w.cache, w.svc.Threads = c, c
	w.svc.Render, w.svc.Responses = render.NewCache(1<<20), respcache.New(1<<20)
}

// isOpen reports whether w's cache holds id open, without opening it.
func (w *world) isOpen(id threadid.ID) bool {
	th, ok := w.cache.Peek(id)
	w.cache.Release(th)
	return ok
}
