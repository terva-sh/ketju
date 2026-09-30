package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for a hard-redacted post as the API serves it (TKT-01M29CXJZ9,
// docs/03 "Hard redaction"). The log is rewritten as ketju redact does, with
// the thread closed, and the world restarts on it: a fresh thread cache,
// render cache and response cache, and the projection caught up.

const redactedSecret = "text a court ordered removed"

// redactOffline rewrites thread id's log with the posts blanked and their
// markers appended, then restarts the world's stores on it, and returns the
// new log length.
func (w *world) redactOffline(id threadid.ID, ordinals ...uint32) int64 {
	w.t.Helper()
	return w.rewriteOffline(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer, o threadlog.RedactOptions) error {
		_, err := threadlog.RedactPosts(src, size, dst, ordinals, o)
		return err
	})
}

func (w *world) rewriteOffline(id threadid.ID, rw func(src *bytes.Reader, size int64, dst *bytes.Buffer, o threadlog.RedactOptions) error) int64 {
	w.t.Helper()
	if err := w.cache.Close(); err != nil {
		w.t.Fatal(err)
	}
	path := id.LogPath(w.root)
	old, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rw(bytes.NewReader(old), int64(len(old)), &out,
		threadlog.RedactOptions{TimeMS: w.clock, Operator: uint64(w.users["bob"]), Reason: "takedown", Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o640); err != nil {
		w.t.Fatal(err)
	}
	c, err := threadcache.New(w.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { c.Close() })
	w.cache, w.svc.Threads = c, c
	w.svc.Render, w.svc.Responses = render.NewCache(1<<20), respcache.New(1<<20)
	if _, err := w.proj.ProjectThread(w.t.Context(), id, int64(out.Len())); err != nil {
		w.t.Fatal(err)
	}
	return int64(out.Len())
}

// redactedThread is seed, then alice's #5 with an edit, and carol's #6
// quoting it, with #5 then redacted.
func (w *world) redactedThread() threadid.ID {
	w.t.Helper()
	id, _ := w.seed()
	w.post(id, "alice", redactedSecret, 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 5,
		Body: &threadlog.EditBody{Source: redactedSecret + ", edited", Reason: "a reason that names it"}})
	w.post(id, "carol", "> [quote=#5]\n> "+redactedSecret+"\n\nquoting it\n", 0)
	w.redactOffline(id, 5)
	return id
}

// TestRedactedPostIsATombstoneToEveryone is AC1: the post keeps its ordinal
// and permalink and is a tombstone to an anonymous reader, a member, its
// author and a moderator, from the page, the single post and the author's
// post history. A quote of it is a tombstone.
//
// Each of them reads it as redacted, and a visible post (#6), a post its
// author deleted (#3) and a post a moderator hid (#7) as not redacted
// (TKT-01M3R1WPNS).
func TestRedactedPostIsATombstoneToEveryone(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
	w.post(id, "alice", "hidden, not redacted", 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 7,
		Body: &threadlog.DeleteBody{Reason: "off topic", ByModerator: true}})
	w.publicEditHistory(true)
	base := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "carol", "alice", "bob"} {
		for _, path := range []string{base, base + "?ordinal=5", postPathN(id, 5), postPathN(id, 5) + "/history",
			"/api/v1/users/alice/posts", postPathN(id, 6)} {
			rec := w.get(w.viewer(who), path)
			members := strings.HasSuffix(path, "/history") || strings.HasPrefix(path, "/api/v1/users/")
			if rec.Code != http.StatusOK && !(who == "" && members && rec.Code == http.StatusUnauthorized) {
				t.Errorf("%q GET %s: %d %s", who, path, rec.Code, rec.Body)
				continue
			}
			if strings.Contains(rec.Body.String(), "court ordered") || strings.Contains(rec.Body.String(), "names it") {
				t.Errorf("%q GET %s serves the redacted text: %s", who, path, rec.Body)
			}
		}
		p := page(t, w.get(w.viewer(who), base))
		var found bool
		seen := map[int]bool{}
		for _, post := range p.Posts {
			seen[post.Ordinal] = true
			if post.Redacted != (post.Ordinal == 5) {
				t.Errorf("%q's page: #%d has redacted %v", who, post.Ordinal, post.Redacted)
			}
			if post.Ordinal != 5 {
				continue
			}
			found = true
			removedIs(t, fmt.Sprintf("%q's page", who), post, 5, dto.RemovalModerator, "")
			if len(post.Attachments) != 0 {
				t.Errorf("%q reads #5 with attachments %v", who, post.Attachments)
			}
		}
		if !found {
			t.Errorf("%q: #5 is not on the page; its ordinal must stay", who)
		}
		for _, n := range []int{3, 6, 7} {
			if !seen[n] {
				t.Errorf("%q: #%d is not on the page", who, n)
			}
		}
		single := decodePost(t, w.get(w.viewer(who), postPathN(id, 5)))
		removedIs(t, fmt.Sprintf("%q's single post", who), single, 5, dto.RemovalModerator, "")
		if !single.Redacted {
			t.Errorf("%q's single post: #5 is not redacted", who)
		}
		for _, n := range []int{3, 6, 7} {
			if p := decodePost(t, w.get(w.viewer(who), postPathN(id, n))); p.Redacted {
				t.Errorf("%q's single post: #%d is redacted", who, n)
			}
		}
	}
}

// TestRedactedHistoryHasNoContent is AC1's history: a moderator is served
// the revisions with no source and no reason; the author and a member the
// tombstone a deleted post's history is. Every one of them reads it as
// redacted, and a visible post's history as not (TKT-01M3R1WPNS).
func TestRedactedHistoryHasNoContent(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
	w.publicEditHistory(true)

	code, h := w.historyOf("bob", id, 5)
	if code != http.StatusOK || !h.Deleted || !h.Redacted || len(h.Revisions) != 2 {
		t.Fatalf("the moderator's history: %d %+v", code, h)
	}
	for _, r := range h.Revisions {
		if r.Source != "" || r.Reason != "" {
			t.Errorf("revision %d has content: %+v", r.Revision, r)
		}
	}
	for _, who := range []string{"alice", "carol"} {
		if code, h := w.historyOf(who, id, 5); code != http.StatusOK || !h.Deleted || !h.Redacted || len(h.Revisions) != 0 {
			t.Errorf("%s's history: %d %+v, want the redacted tombstone", who, code, h)
		}
	}
	for _, who := range []string{"alice", "carol", "bob"} {
		if code, h := w.historyOf(who, id, 6); code != http.StatusOK || h.Redacted || h.Deleted || len(h.Revisions) != 1 {
			t.Errorf("%s's history of visible #6: %d %+v", who, code, h)
		}
	}
}

// TestRedactedIsFlaggedEverywhere: a redacted post reads redacted, with no
// body and none of its text, on every route that serves it, to every reader
// that route admits (TKT-01M3R1WPNS): the page, the single post, its
// parent's replies, the author's posts, its history, and the moderator
// queue for a post flagged before its redaction. Its source is refused to
// everyone, moderators included, and serves none of it.
func TestRedactedIsFlaggedEverywhere(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", redactedSecret, 1)
	h := w.chain()
	if rec := send(h, "carol", http.MethodPost, flagsPath(id), `{"ordinal":5,"reason":"spam"}`); rec.Code != http.StatusOK {
		t.Fatalf("carol's flag: %d %s", rec.Code, rec.Body)
	}
	w.redactOffline(id, 5)
	w.publicEditHistory(true)
	h = w.chain()

	leaks := func(where string, rec *httptest.ResponseRecorder) {
		t.Helper()
		if strings.Contains(rec.Body.String(), "court ordered") {
			t.Errorf("%s serves the redacted text: %s", where, rec.Body)
		}
	}
	is := func(where string, p dto.Post) {
		t.Helper()
		if !p.Redacted || !p.Deleted || p.HTML != "" || len(p.Attachments) != 0 {
			t.Errorf("%s: #%d is %+v, want redacted with no body", where, p.Ordinal, p)
		}
	}
	base := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "carol", "alice", "bob"} {
		rec := send(h, who, http.MethodGet, base, "")
		leaks(fmt.Sprintf("%q's page", who), rec)
		for _, p := range page(t, rec).Posts {
			if p.Ordinal == 5 {
				is(fmt.Sprintf("%q's page", who), p)
			}
		}
		rec = send(h, who, http.MethodGet, postPathN(id, 5), "")
		leaks(fmt.Sprintf("%q's single post", who), rec)
		is(fmt.Sprintf("%q's single post", who), decodePost(t, rec))

		rec = send(h, who, http.MethodGet, postPathN(id, 1)+"/replies", "")
		leaks(fmt.Sprintf("%q's replies", who), rec)
		var found bool
		for _, p := range decode[dto.PostReplies](t, rec).Posts {
			if p.Ordinal == 5 {
				found = true
				is(fmt.Sprintf("%q's replies", who), p)
			}
		}
		if !found {
			t.Errorf("%q's replies to #1 lack #5", who)
		}

		rec = send(h, who, http.MethodGet, postPathN(id, 5)+"/history", "")
		leaks(fmt.Sprintf("%q's history", who), rec)
		if who != "" && !decode[dto.PostHistory](t, rec).Redacted {
			t.Errorf("%q's history of #5 is not redacted: %s", who, rec.Body)
		}

		// The source goes to nobody: conflict for its author and a
		// moderator, who might otherwise edit it, forbidden to anyone else.
		rec = send(h, who, http.MethodGet, postPathN(id, 5)+"/source", "")
		leaks(fmt.Sprintf("%q's source", who), rec)
		want := map[string]int{"": http.StatusUnauthorized, "carol": http.StatusForbidden,
			"alice": http.StatusConflict, "bob": http.StatusConflict}[who]
		if rec.Code != want {
			t.Errorf("%q's source of #5: %d, want %d: %s", who, rec.Code, want, rec.Body)
		}
	}

	// The author's posts: #5 is on them for the author and a moderator,
	// as any removed post is, and on no one else's.
	for _, who := range []string{"carol", "alice", "bob"} {
		rec := send(h, who, http.MethodGet, "/api/v1/users/alice/posts", "")
		leaks(fmt.Sprintf("%q's list of alice's posts", who), rec)
		var found bool
		for _, up := range decode[dto.UserPosts](t, rec).Posts {
			if up.Post.Ordinal == 5 {
				found = true
				is(fmt.Sprintf("%q's list of alice's posts", who), up.Post)
			} else if up.Post.Redacted {
				t.Errorf("%q's list of alice's posts: #%d is redacted", who, up.Post.Ordinal)
			}
		}
		if found != (who != "carol") {
			t.Errorf("%q's list of alice's posts has #5: %v", who, found)
		}
	}

	// The moderator queue still lists the flag, and the post under it is
	// the redacted tombstone.
	rec := send(h, "bob", http.MethodGet, "/api/v1/admin/queue", "")
	leaks("the moderator queue", rec)
	var found bool
	for _, it := range decode[dto.Queue](t, rec).Items {
		if it.Flag == nil || it.Flag.Ordinal != 5 {
			continue
		}
		found = true
		if it.Flag.Post == nil {
			t.Fatalf("the queue's flag on #5 has no post: %s", rec.Body)
		}
		is("the moderator queue", *it.Flag.Post)
	}
	if !found {
		t.Errorf("the queue lacks the flag on #5: %s", rec.Body)
	}
}

// TestRedactedPostTakesNoChange is AC1's last clause: an edit, a delete and
// a restore of a redacted post are conflict, from its author and from a
// moderator, and write nothing.
func TestRedactedPostTakesNoChange(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
	h := w.chain()
	for _, who := range []string{"alice", "bob"} {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodPatch, postPathN(id, 5), `{"source":"back again"}`},
			{http.MethodDelete, postPathN(id, 5), `{"reason":"again"}`},
			{http.MethodPost, postPathN(id, 5) + "/restore", `{"reason":"undo"}`},
		} {
			before := w.logState(id)
			if rec := send(h, who, c.method, c.path, c.body); problemCode(t, rec) != "conflict" {
				t.Errorf("%s %s %s: %d %s, want conflict", who, c.method, c.path, rec.Code, rec.Body)
			}
			if after := w.logState(id); after != before {
				t.Errorf("%s %s %s changed the log: %+v then %+v", who, c.method, c.path, before, after)
			}
		}
	}
}

// TestRedactedPostLeavesTheQueue: the flags on a redacted post can be
// closed from the moderator queue, whatever state the post was in when it
// was redacted, and closing them writes nothing to the log
// (TKT-01M3R1WPNS). The queue takes a redacted post as one already down, so
// no action tries the Restore or Delete the log refuses. A post hidden
// pending review, then redacted, would otherwise be restored by a dismiss,
// be refused, and stay in the queue for good.
func TestRedactedPostLeavesTheQueue(t *testing.T) {
	for _, c := range []struct {
		name      string
		reporters []string
		action    string
	}{
		{"shown, then dismissed", []string{"dave"}, "dismiss"},
		{"shown, then kept", []string{"dave"}, "resolve"},
		{"shown, then removed", []string{"dave"}, "remove"},
		{"hidden pending review, then dismissed", []string{"dave", "erin", "fay"}, "dismiss"},
		{"hidden pending review, then kept", []string{"dave", "erin", "fay"}, "resolve"},
		{"hidden pending review, then removed", []string{"dave", "erin", "fay"}, "remove"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			id, h := w.flagWorld()
			for _, who := range c.reporters {
				if rec := send(h, who, http.MethodPost, flagsPath(id), `{"ordinal":4,"reason":"spam"}`); rec.Code != http.StatusOK {
					t.Fatalf("%s's flag: %d %s", who, rec.Code, rec.Body)
				}
			}
			if hidden := w.entry(id, 4).Flags.Has(threadlog.EntryAutoHidden); hidden != (len(c.reporters) == 3) {
				t.Fatalf("#4 auto-hidden %v with %d reporters", hidden, len(c.reporters))
			}
			w.redactOffline(id, 4)
			h = w.chain()

			before := w.logState(id)
			rec := send(h, "bob", http.MethodPost, queuePostPath(id, 4), `{"action":"`+c.action+`"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("bob's %s: %d %s", c.action, rec.Code, rec.Body)
			}
			if res := decode[dto.QueueResolution](t, rec); res.Post != dto.QueuePostAlreadyHidden || len(res.Flags) != len(c.reporters) {
				t.Errorf("bob's %s answered %s", c.action, rec.Body)
			}
			if after := w.logState(id); after != before {
				t.Errorf("bob's %s changed the log: %+v then %+v", c.action, before, after)
			}
			if rec := send(h, "bob", http.MethodGet, "/api/v1/admin/queue", ""); len(decode[dto.Queue](t, rec).Items) != 0 {
				t.Errorf("the queue after bob's %s: %s", c.action, rec.Body)
			}
		})
	}
}

// TestOldSlugRedirectsAfterTitleRedaction and the API half of
// TestRedactTitleShowsTheReplacement: after a title redaction the thread is
// served by its id with the replacement's title and slug, in the page header
// and the forum's thread list, so a stale slug is replaced by the client as
// after any rename, and no old title is served.
func TestOldSlugRedirectsAfterTitleRedaction(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.rewriteOffline(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer, o threadlog.RedactOptions) error {
		_, err := threadlog.RedactTitle(src, size, dst, threadlog.ThreadRenamedBody{Title: "Title removed", Slug: "title-removed"}, o)
		return err
	})
	rec := w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())
	p := page(t, rec)
	if p.Thread.Title != "Title removed" || p.Thread.Slug != "title-removed" {
		t.Errorf("the page header is %q %q", p.Thread.Title, p.Thread.Slug)
	}
	list := w.get(w.viewer("carol"), "/api/v1/forums/general/threads")
	for _, body := range []string{rec.Body.String(), list.Body.String()} {
		if strings.Contains(body, "Slack") || strings.Contains(body, "slack") {
			t.Errorf("an old title is served: %s", body)
		}
	}
	if !strings.Contains(list.Body.String(), `"title-removed"`) {
		t.Errorf("the thread list lacks the replacement: %s", list.Body)
	}
}

// TestRedactedThreadPageIsNewer: the log only grows, so the page's
// log_length and its ETag after the redaction exceed every earlier value,
// and a client holding the old page takes the new one.
func TestRedactedThreadPageIsNewer(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", redactedSecret, 0)
	path := "/api/v1/threads/" + id.String()
	before := w.get(w.viewer("carol"), path)
	was := page(t, before).Thread.LogLength

	n := w.redactOffline(id, 5)
	after := w.get(w.viewer("carol"), path)
	now := page(t, after).Thread.LogLength
	if now != n || now <= was {
		t.Errorf("log_length %d after, %d before, log %d", now, was, n)
	}
	if tag := after.Header().Get("ETag"); tag == before.Header().Get("ETag") || !strings.Contains(tag, fmt.Sprintf("-%d-", now)) {
		t.Errorf("ETag %s after, %s before", tag, before.Header().Get("ETag"))
	}
	if rec := w.get(w.viewer("carol"), path, "If-None-Match", before.Header().Get("ETag")); rec.Code != http.StatusOK {
		t.Errorf("the old ETag revalidates: %d", rec.Code)
	}
}
