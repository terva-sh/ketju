package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// decode reads a 200 strictly into T: a field T does not have fails.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding strictly: %v\n%s", err, rec.Body)
	}
	return v
}

// TestSinglePostMatchesThePage is AC1: a post on another page, fetched on its
// own as a popover does, is exactly what its page renders.
func TestSinglePostMatchesThePage(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 60 {
		w.post(id, "alice", fmt.Sprintf("post **%d**", i+1), uint32(max(0, i-3)))
	}
	path := "/api/v1/threads/" + id.String()
	p3 := page(t, w.get(domain.Anonymous(), path+"?page=3"))
	for _, want := range []dto.Post{p3.Posts[0], p3.Posts[5]} {
		got := decode[dto.Post](t, w.get(domain.Anonymous(), fmt.Sprintf("%s/posts/%d", path, want.Ordinal)))
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if !bytes.Equal(a, b) {
			t.Errorf("post %d on its own:\n%s\non page 3:\n%s", want.Ordinal, a, b)
		}
	}
	for _, c := range []struct {
		path string
		want int
	}{
		{path + "/posts/61", http.StatusNotFound},
		{path + "/posts/0", http.StatusBadRequest},
		{path + "/posts/x", http.StatusBadRequest},
		{path + "/posts/-1", http.StatusBadRequest},
		{path + "/posts/61/replies", http.StatusNotFound},
		{path + "/posts/61/history", http.StatusUnauthorized},
	} {
		if rec := w.get(domain.Anonymous(), c.path); rec.Code != c.want {
			t.Errorf("%s: %d, want %d", c.path, rec.Code, c.want)
		}
	}
	// A conditional request is answered as the page is.
	rec := w.get(domain.Anonymous(), path+"/posts/53")
	if again := w.get(domain.Anonymous(), path+"/posts/53", "If-None-Match", rec.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Errorf("the single post's own ETag got %d", again.Code)
	}
}

// TestRepliesInOrder: the expander's replies are the direct replies, in
// ordinal order, rendered as the page renders them.
func TestRepliesInOrder(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	got := decode[dto.PostReplies](t, w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()+"/posts/1/replies"))
	if got.Ordinal != 1 || len(got.Posts) != 2 || got.Posts[0].Ordinal != 2 || got.Posts[1].Ordinal != 4 {
		t.Fatalf("replies to 1: %+v", got)
	}
	if got := decode[dto.PostReplies](t, w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()+"/posts/3/replies")); len(got.Posts) != 0 || got.Posts == nil {
		t.Fatalf("a post with no replies: %+v, want an empty list", got)
	}
}

// TestHistory is AC3: three edits by two actors give four revisions, each
// with its actor, time, reason and source; the public_edit_history setting
// gates members, never moderators, and anonymous viewers are refused.
func TestHistory(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Edited")
	w.post(id, "alice", "first", 0)
	edit := func(who, src, reason string) {
		w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users[who]), Ordinal: 1,
			Body: &threadlog.EditBody{Source: src, Reason: reason}})
	}
	edit("alice", "second", "")
	edit("bob", "third", "removed a phone number")
	edit("alice", "fourth", "typo")

	path := "/api/v1/threads/" + id.String() + "/posts/1/history"
	h := decode[dto.PostHistory](t, w.get(w.viewer("carol"), path))
	if h.Ordinal != 1 || h.Deleted || len(h.Revisions) != 4 {
		t.Fatalf("history: %+v", h)
	}
	for i, want := range []struct{ handle, source, reason string }{
		{"alice", "first", ""}, {"alice", "second", ""}, {"bob", "third", "removed a phone number"}, {"alice", "fourth", "typo"},
	} {
		r := h.Revisions[i]
		if r.Revision != i+1 || r.Actor.Handle != want.handle || r.Source != want.source || r.Reason != want.reason {
			t.Errorf("revision %d: %+v, want %+v", i+1, r, want)
		}
		if i > 0 && r.At <= h.Revisions[i-1].At {
			t.Errorf("revision %d at %d, not after %d", i+1, r.At, h.Revisions[i-1].At)
		}
	}
	if rec := w.get(domain.Anonymous(), path); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous, with the setting on: %d", rec.Code)
	}

	if err := settings.Set(t.Context(), w.db, "public_edit_history", json.RawMessage("false"), 0, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	for handle, want := range map[string]int{"carol": http.StatusForbidden, "bob": http.StatusOK, "": http.StatusUnauthorized} {
		if rec := w.get(w.viewer(handle), path); rec.Code != want {
			t.Errorf("%q with the setting off: %d, want %d", handle, rec.Code, want)
		}
	}
}

// TestTombstoneFromEveryEndpoint is AC4: a deleted post is a tombstone from
// the single-post, replies and history endpoints to a member, and its content
// only to a moderator.
func TestTombstoneFromEveryEndpoint(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", "a reply that gets removed", 1)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 5,
		Body: &threadlog.DeleteBody{}})
	base := "/api/v1/threads/" + id.String()

	for handle, content := range map[string]bool{"carol": false, "bob": true} {
		v := w.viewer(handle)
		single := decode[dto.Post](t, w.get(v, base+"/posts/3"))
		if !single.Deleted || (single.HTML != "") == !content {
			t.Errorf("%s, single post 3: %+v", handle, single)
		}
		replies := decode[dto.PostReplies](t, w.get(v, base+"/posts/1/replies"))
		last := replies.Posts[len(replies.Posts)-1]
		if last.Ordinal != 5 || !last.Deleted || (last.HTML != "") == !content {
			t.Errorf("%s, replies to 1: the removed reply is %+v", handle, last)
		}
		h := decode[dto.PostHistory](t, w.get(v, base+"/posts/3/history"))
		if !h.Deleted || (len(h.Revisions) > 0) != content {
			t.Errorf("%s, history of 3: %+v", handle, h)
		}
		// One revision: the delete is not a revision of the post.
		if content && (len(h.Revisions) != 1 || h.Revisions[0].Source != "the text a moderator still needs to read") {
			t.Errorf("the moderator's history of 3: %+v", h.Revisions)
		}
	}
}
