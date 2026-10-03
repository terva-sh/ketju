package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for reply as a new thread through POST /api/v1/forums/{slug}/threads
// with a from (TKT-01M29CYSC0). The rules are tested in
// internal/service/threads (continue_test.go); these hold the body, the
// answer, and what a redaction of the source does, which needs a log
// rewritten offline (redact_test.go).

func continueBody(from threadid.ID, ordinal int, source string) string {
	b, _ := json.Marshal(map[string]any{"title": "Picked up", "source": source,
		"from": map[string]any{"thread": from.String(), "ordinal": ordinal}})
	return string(b)
}

var generalThreads = threadsPath("general")

// TestReplyAsNewThreadRoute: the from field is read strictly, each of its
// fields required; an applicant and an anonymous viewer are refused as for
// any new thread; a member's thread answers 201 with continued_from, and the
// source post then lists it under continued_in.
func TestReplyAsNewThreadRoute(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	id, _ := w.seed()
	for _, c := range []struct{ who, body, want string }{
		{"", continueBody(id, 1, "x"), "unauthenticated"},
		{"pre-user", continueBody(id, 1, "x"), "forbidden"},
		{"carol", `{"title":"T","source":"x","from":{"thread":"` + id.String() + `"}}`, "bad-request"},
		{"carol", `{"title":"T","source":"x","from":{"ordinal":1}}`, "bad-request"},
		{"carol", `{"title":"T","source":"x","from":{"thread":"` + id.String() + `","ordinal":"1"}}`, "bad-request"},
		{"carol", `{"title":"T","source":"x","from":{"thread":"` + id.String() + `","ordinal":1,"post":2}}`, "bad-request"},
		{"carol", `{"title":"T","source":"x","from":{"thread":"` + id.String() + `","ordinal":-1}}`, "bad-request"},
		{"carol", continueBody(id, 3, "x"), "not-found"}, // deleted by its author in the seed
		{"carol", continueBody(id, 99, "x"), "not-found"},
		{"carol", `{"title":"T","source":"x","from":{"thread":"nope","ordinal":1}}`, "validation-failed"},
	} {
		w.unchanged(t, id, fmt.Sprintf("a new thread as %q with %.70s", c.who, c.body), c.want, func() string {
			return problemCode(t, send(h, c.who, http.MethodPost, generalThreads, c.body))
		})
	}
	res := created[dto.CreatedThread](t, send(h, "carol", http.MethodPost, generalThreads, continueBody(id, 1, "On its own.")))
	if cf := res.Thread.ContinuedFrom; cf == nil || cf.Thread.ID != id.String() || cf.Ordinal != 1 {
		t.Errorf("the new thread's continued_from is %+v", cf)
	}
	var page dto.ThreadPage
	if err := json.Unmarshal(w.get(w.viewer("carol"), threadPath(id)).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if got := page.Posts[0].ContinuedIn; len(got) != 1 || got[0].ID != res.Thread.ID {
		t.Errorf("the source post's continued_in is %+v", got)
	}
	if !page.Viewer.CanCreateThread {
		t.Error("carol is not offered reply as a new thread")
	}
}

// TestReplyAsNewThreadRefusesRedactedSource and TestRedactDoesNotFollowContinues
// together: a redacted post is not one to continue, and a thread already
// continuing it keeps its own text, the quote included, because the quote
// is the member's text and ketju redact follows origin, never continues
// (docs/03 "Hard redaction").
func TestReplyAsNewThreadRefusesRedactedSource(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	id, _ := w.seed()
	w.post(id, "alice", redactedSecret, 0)
	quote := "@alice wrote:\n\n> " + redactedSecret + "\n\nAnd onwards."
	res := created[dto.CreatedThread](t, send(h, "carol", http.MethodPost, generalThreads, continueBody(id, 5, quote)))
	next, _ := threadid.Parse(res.Thread.ID)
	before := w.logState(next)
	w.redactOffline(id, 5)
	h = w.chain()
	w.unchanged(t, id, "continuing a redacted post", "not-found", func() string {
		return problemCode(t, send(h, "carol", http.MethodPost, generalThreads, continueBody(id, 5, "x")))
	})
	if after := w.logState(next); after.size != before.size || after.posts != before.posts {
		t.Errorf("redacting the source rewrote the thread continuing it: %+v, was %+v", after, before)
	}
	body := w.get(w.viewer("carol"), threadPath(next)).Body.String()
	if !strings.Contains(body, redactedSecret) {
		t.Errorf("the continuing thread lost its quote: %s", body)
	}
}

// TestRedactDoesNotFollowContinues is the redaction half on its own terms:
// ketju redact of the source post leaves the continuing thread's log as it
// was, byte for byte.
func TestRedactDoesNotFollowContinues(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	id, _ := w.seed()
	res := created[dto.CreatedThread](t, send(h, "carol", http.MethodPost, generalThreads, continueBody(id, 1, "> quoted\n\nmine")))
	next, _ := threadid.Parse(res.Thread.ID)
	before := w.logState(next)
	w.redactOffline(id, 1)
	if after := w.logState(next); after.size != before.size {
		t.Errorf("the continuing thread's log is %d bytes after the redaction, %d before", after.size, before.size)
	}
}
