package api

import (
	"bytes"
	"fmt"
	"net/http"
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
func TestRedactedPostIsATombstoneToEveryone(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
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
		for _, post := range p.Posts {
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
		removedIs(t, fmt.Sprintf("%q's single post", who), decodePost(t, w.get(w.viewer(who), postPathN(id, 5))), 5, dto.RemovalModerator, "")
	}
}

// TestRedactedHistoryHasNoContent is AC1's history: a moderator is served
// the revisions with no source and no reason; the author and a member the
// tombstone a deleted post's history is.
func TestRedactedHistoryHasNoContent(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
	w.publicEditHistory(true)

	code, h := w.historyOf("bob", id, 5)
	if code != http.StatusOK || !h.Deleted || len(h.Revisions) != 2 {
		t.Fatalf("the moderator's history: %d %+v", code, h)
	}
	for _, r := range h.Revisions {
		if r.Source != "" || r.Reason != "" {
			t.Errorf("revision %d has content: %+v", r.Revision, r)
		}
	}
	for _, who := range []string{"alice", "carol"} {
		if code, h := w.historyOf(who, id, 5); code != http.StatusOK || !h.Deleted || len(h.Revisions) != 0 {
			t.Errorf("%s's history: %d %+v, want the tombstone", who, code, h)
		}
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
