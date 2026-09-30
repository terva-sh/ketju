package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for GET /threads/{id}/posts/{ordinal}/source (TKT-01M3QJYV4Z): a
// post's current source, for its author or a moderator to edit.

// sourceThread is seedHidden, where #1 is alice's with one edit, #3 bob's
// own delete, #4 carol's and #5 alice's post bob hid, then:
//
//	#1 edited again, by bob, so its current revision is a moderator's
//	#6 alice's, which she deleted herself
//	#7 carol's, hidden pending review by enough flags
func (w *world) sourceThread() threadid.ID {
	w.t.Helper()
	id, _ := w.seedHidden()
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["bob"]), Ordinal: 1,
		Body: &threadlog.EditBody{Source: modEditedSource, Reason: "tone"}})
	w.post(id, "alice", selfSecret, 0)
	w.del(id, 6, "alice")
	w.post(id, "carol", reviewSecret, 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 7,
		Body: &threadlog.DeleteBody{Auto: true}})
	return id
}

const (
	modEditedSource = "The bill was the **smaller** half of it, _said a moderator_."
	reviewSecret    = "a post the flags took down pending review"
)

// sourceOf decodes a 200 from the source route strictly, and checks it is
// no-store with no ETag.
func sourceOf(t *testing.T, what string, rec *httptest.ResponseRecorder) dto.PostSource {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200: %s", what, rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("%s: Cache-Control %q, want no-store", what, cc)
	}
	if et := rec.Header().Get("ETag"); et != "" {
		t.Errorf("%s: carries ETag %q", what, et)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var s dto.PostSource
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("%s: decoding dto.PostSource: %v\n%s", what, err, rec.Body)
	}
	return s
}

// TestPostSourceReaders is the first criterion: the author and a moderator
// of the forum read the current revision's source, with public_edit_history
// on and off; the answer is no-store with no ETag, and the response cache
// is neither looked in nor filled. The current revision of #1 is a
// moderator's edit, so the author is served the words as they now stand,
// not their own last ones. An archived forum still serves the author: the
// edit is refused by PATCH, not the reading.
func TestPostSourceReaders(t *testing.T) {
	w := newWorld(t)
	id := w.sourceThread()
	h := w.chain()
	src := func(who string, n int) *httptest.ResponseRecorder {
		return send(h, who, http.MethodGet, postPathN(id, n)+"/source", "")
	}
	before := w.svc.Responses.Stats()

	for _, on := range []bool{true, false} {
		w.publicEditHistory(on)
		for _, c := range []struct {
			who  string
			n    int
			want dto.PostSource
		}{
			{"alice", 1, dto.PostSource{Ordinal: 1, Revision: 3, Source: modEditedSource}},
			{"bob", 1, dto.PostSource{Ordinal: 1, Revision: 3, Source: modEditedSource}},
			{"scribe", 2, dto.PostSource{Ordinal: 2, Revision: 1, Source: "Summarising the thread so far."}},
			{"carol", 4, dto.PostSource{Ordinal: 4, Revision: 1, Source: "Agreed with @alice on this."}},
			{"bob", 4, dto.PostSource{Ordinal: 4, Revision: 1, Source: "Agreed with @alice on this."}},
			// A moderator reads every removed post but a moved one, as
			// revealRemoved serves them its body.
			{"bob", 3, dto.PostSource{Ordinal: 3, Revision: 1, Source: "the text a moderator still needs to read"}},
			{"bob", 5, dto.PostSource{Ordinal: 5, Revision: 1, Source: hiddenSecret}},
			{"bob", 6, dto.PostSource{Ordinal: 6, Revision: 1, Source: selfSecret}},
			{"bob", 7, dto.PostSource{Ordinal: 7, Revision: 1, Source: reviewSecret}},
		} {
			what := fmt.Sprintf("%s's source of #%d", c.who, c.n)
			if got := sourceOf(t, what, src(c.who, c.n)); got != c.want {
				t.Errorf("public_edit_history %v, %s: %+v, want %+v", on, what, got, c.want)
			}
		}
	}

	if after := w.svc.Responses.Stats(); after.Hits+after.Misses != before.Hits+before.Misses ||
		after.Inserts != before.Inserts || after.Entries != before.Entries {
		t.Errorf("the response cache moved for the source route: before %+v, after %+v", before, after)
	}

	w.archive()
	if got := sourceOf(t, "alice's source in an archived forum", src("alice", 1)); got.Source != modEditedSource {
		t.Errorf("in an archived forum alice reads %+v", got)
	}
}

// TestPostSourceRefusals is the second criterion. Anonymous is 401, and so
// is an inactive session, which is served as anonymous; a pre-user is 403
// before the endpoint runs. Another member is 403 for every post not
// theirs, visible or removed, so the refusal says nothing of the post's
// state. The author of a post a moderator hid, or one hidden pending review,
// is 403, as the edit would be; the author of a post they deleted is 409.
// An ordinal past the end is 404, and a thread hidden from the viewer is 404
// even for their own post. No refusal carries the source.
func TestPostSourceRefusals(t *testing.T) {
	w := newWorld(t)
	id := w.sourceThread()
	secrets := []string{modEditedSource, hiddenSecret, selfSecret, reviewSecret, "the text a moderator still needs to read"}

	type refusal struct {
		who  string
		n    int
		want problem.Code
	}
	check := func(h http.Handler, cases []refusal) {
		t.Helper()
		for _, c := range cases {
			rec := send(h, c.who, http.MethodGet, postPathN(id, c.n)+"/source", "")
			if got := problemCode(t, rec); got != string(c.want) {
				t.Errorf("%q's source of #%d: %d %s, want %s", c.who, c.n, rec.Code, rec.Body, c.want)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("%q's refused source of #%d: Cache-Control %q", c.who, c.n, cc)
			}
			for _, s := range secrets {
				if strings.Contains(rec.Body.String(), s) {
					t.Errorf("%q's refused source of #%d carries %q", c.who, c.n, s)
				}
			}
		}
	}

	check(w.chain(), []refusal{
		{"", 1, problem.CodeUnauthenticated},
		{"", 4, problem.CodeUnauthenticated},
		{"inactive", 1, problem.CodeUnauthenticated},
		{"pre-user", 1, problem.CodeForbidden},
		{"carol", 1, problem.CodeForbidden},
		{"carol", 3, problem.CodeForbidden},
		{"carol", 5, problem.CodeForbidden},
		{"carol", 6, problem.CodeForbidden},
		{"alice", 7, problem.CodeForbidden},
		{"scribe", 1, problem.CodeForbidden},
		{"alice", 5, problem.CodeForbidden},
		{"carol", 7, problem.CodeForbidden},
		{"alice", 6, problem.CodeConflict},
		{"bob", 3000, problem.CodeNotFound},
		{"carol", 3000, problem.CodeNotFound},
	})

	// A thread in a forum the viewer cannot read is not there at all, their
	// own post included. A fresh handler, so its permission cache reads the
	// forum as it now is.
	w.hideForum()
	check(newHandlerFor(w).chain(), []refusal{
		{"carol", 4, problem.CodeNotFound},
		{"alice", 1, problem.CodeNotFound},
		{"", 1, problem.CodeNotFound},
	})
}

// TestRedactedPostServesNoSource: once a hard redaction has cleared a post
// (TKT-01M29CXJZ9), its source goes to nobody. Its author and a moderator
// are conflict, having nothing to edit, and another member is forbidden, as
// for any post not theirs. The post beside it still serves its author, so
// the refusal is the redaction's. No answer carries the redacted text.
func TestRedactedPostServesNoSource(t *testing.T) {
	w := newWorld(t)
	id := w.redactedThread()
	w.publicEditHistory(true)
	h := w.chain()
	for _, c := range []struct {
		who  string
		want problem.Code
	}{{"alice", problem.CodeConflict}, {"bob", problem.CodeConflict}, {"carol", problem.CodeForbidden}} {
		rec := send(h, c.who, http.MethodGet, postPathN(id, 5)+"/source", "")
		if got := problemCode(t, rec); got != string(c.want) {
			t.Errorf("%q's source of the redacted #5: %d %s, want %s", c.who, rec.Code, rec.Body, c.want)
		}
		if strings.Contains(rec.Body.String(), "court ordered") || strings.Contains(rec.Body.String(), "names it") {
			t.Errorf("%q's source of the redacted #5 carries its text: %s", c.who, rec.Body)
		}
	}
	got := sourceOf(t, "carol's source of #6", send(h, "carol", http.MethodGet, postPathN(id, 6)+"/source", ""))
	if got.Ordinal != 6 || got.Revision != 1 {
		t.Errorf("carol's source of #6: %+v", got)
	}
}
