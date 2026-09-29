package api

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for POST /api/v1/threads/{id}/flags and what an auto-hidden post
// looks like to each viewer (TKT-01M29CP2PJ). In the seeded thread #4 is
// carol's, and dave, erin and fay are three more members to report it.

func flagsPath(id threadid.ID) string { return "/api/v1/threads/" + id.String() + "/flags" }

func (w *world) flagWorld() (threadid.ID, http.Handler) {
	w.t.Helper()
	id, _ := w.seed()
	w.addUsers("active", "dave", "erin", "fay")
	return id, w.chain()
}

// TestReporterSeesOnlyTheReceipt is AC3: the answers to a new flag, a
// repeat of it, and the flag that hides the post are byte-identical, the
// receipt and nothing else, never cached. The reporters' pages afterwards
// carry no flag count and no reporter.
func TestReporterSeesOnlyTheReceipt(t *testing.T) {
	w := newWorld(t)
	id, h := w.flagWorld()
	var bodies []string
	for _, c := range []struct{ who, body string }{
		{"dave", `{"ordinal":4,"reason":"spam","note":""}`},
		{"dave", `{"ordinal":4,"reason":"abuse","note":"again"}`}, // a repeat: no row
		{"erin", `{"ordinal":4,"reason":"off-topic"}`},
		{"fay", `{"ordinal":4,"reason":"other","note":"a scam link"}`}, // the third reporter: hides it
	} {
		rec := send(h, c.who, http.MethodPost, flagsPath(id), c.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's flag: status %d: %s", c.who, rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control %q, want no-store", cc)
		}
		bodies = append(bodies, rec.Body.String())
	}
	for i, b := range bodies {
		if b != bodies[0] || strings.TrimSpace(b) != `{"received":true}` {
			t.Errorf("answer %d is %s: want every answer the bare receipt", i, b)
		}
	}
	if e := w.entry(id, 4); !e.Flags.Has(threadlog.EntryAutoHidden) {
		t.Fatalf("three reporters did not hide #4: %#x", e.Flags)
	}
	for _, who := range []string{"dave", "fay", "carol"} {
		body := w.get(w.viewer(who), "/api/v1/threads/"+id.String()).Body.String()
		if strings.Contains(body, "flag") || strings.Contains(body, "dave") || strings.Contains(body, "reporter") {
			t.Errorf("%s's page mentions a flag or a reporter: %s", who, body)
		}
		me := w.get(w.viewer(who), "/api/v1/me").Body.String()
		if strings.Contains(me, "flag") || strings.Contains(me, "report") {
			t.Errorf("%s's /me mentions a flag: %s", who, me)
		}
	}
}

// TestFlagRouteRefusals: the route's own refusals and the service's, as
// status codes, and none of them writes a row that could count.
func TestFlagRouteRefusals(t *testing.T) {
	w := newWorld(t)
	id, h := w.flagWorld()
	for _, c := range []struct {
		name, who, body string
		want            int
	}{
		{"anonymous", "", `{"ordinal":4,"reason":"spam"}`, http.StatusUnauthorized},
		{"pre-user", "pre-user", `{"ordinal":4,"reason":"spam"}`, http.StatusForbidden},
		{"no ordinal", "dave", `{"reason":"spam"}`, http.StatusBadRequest},
		{"unknown field", "dave", `{"ordinal":4,"reason":"spam","count":1}`, http.StatusBadRequest},
		{"unknown reason", "dave", `{"ordinal":4,"reason":"rude"}`, http.StatusUnprocessableEntity},
		{"other without a note", "dave", `{"ordinal":4,"reason":"other"}`, http.StatusUnprocessableEntity},
		{"no such post", "dave", `{"ordinal":40,"reason":"spam"}`, http.StatusNotFound},
		{"a deleted post", "dave", `{"ordinal":3,"reason":"spam"}`, http.StatusConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec := send(h, c.who, http.MethodPost, flagsPath(id), c.body); rec.Code != c.want {
				t.Errorf("status %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
	if rec := send(h, "dave", http.MethodPost, flagsPath(threadid.ID(12345)), `{"ordinal":1,"reason":"spam"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a flag on a missing thread: status %d, want 404", rec.Code)
	}
	// A fresh handler, so its permission cache reads the forum as it now is.
	w.hideForum()
	if rec := send(newHandlerFor(w).chain(), "dave", http.MethodPost, flagsPath(id), `{"ordinal":4,"reason":"spam"}`); rec.Code != http.StatusNotFound {
		t.Errorf("a flag in a hidden forum: status %d, want 404", rec.Code)
	}
	var n int
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM flags`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("refused flags wrote %d rows", n)
	}
}

// TestAutoHiddenVisibility is AC5 and the server half of AC8: while #4 is
// hidden pending review, its author and a moderator get its html with
// removal "review", and another member and an anonymous viewer get a
// tombstone with removal "review" and no html. The author's restore is
// forbidden.
func TestAutoHiddenVisibility(t *testing.T) {
	w := newWorld(t)
	id, h := w.flagWorld()
	for _, who := range []string{"dave", "erin", "fay"} {
		if rec := send(h, who, http.MethodPost, flagsPath(id), `{"ordinal":4,"reason":"spam"}`); rec.Code != http.StatusOK {
			t.Fatalf("%s's flag: %d %s", who, rec.Code, rec.Body)
		}
	}
	const text = "Agreed with"
	for _, c := range []struct {
		who      string
		sees     bool
		pageKind string
	}{
		{"carol", true, "author"}, {"bob", true, "moderator"}, {"alice", false, "member"}, {"", false, "anon"},
	} {
		t.Run(c.pageKind, func(t *testing.T) {
			p := page(t, w.get(w.viewer(c.who), "/api/v1/threads/"+id.String()))
			var post *dto.Post
			for i := range p.Posts {
				if p.Posts[i].Ordinal == 4 {
					post = &p.Posts[i]
				}
			}
			if post == nil {
				t.Fatal("#4 is not on the page")
			}
			if !post.Deleted || post.Removal == nil || *post.Removal != dto.RemovalReview {
				t.Errorf("#4 is %+v: want deleted with removal review", post)
			}
			if got := strings.Contains(post.HTML, text); got != c.sees {
				t.Errorf("#4's html %q: the post's text shown=%v, want %v", post.HTML, got, c.sees)
			}
			if !c.sees && post.HTML != "" {
				t.Errorf("#4's tombstone carries html %q", post.HTML)
			}
			single := decodePost(t, w.get(w.viewer(c.who), postPathN(id, 4)))
			if strings.Contains(single.HTML, text) != c.sees || single.Removal == nil || *single.Removal != dto.RemovalReview {
				t.Errorf("GET #4 alone: %+v", single)
			}
		})
	}
	if rec := send(h, "carol", http.MethodPost, postPathN(id, 4)+"/restore", ""); rec.Code != http.StatusForbidden {
		t.Errorf("the author's restore: status %d, want 403: %s", rec.Code, rec.Body)
	}
	res := changed(t, send(h, "bob", http.MethodPost, postPathN(id, 4)+"/restore", `{"reason":"not spam"}`))
	if res.Post.Deleted || res.Post.Removal != nil || !strings.Contains(res.Post.HTML, text) {
		t.Errorf("the moderator's restore answered %+v", res.Post)
	}
}

// TestAutoHideMovesTheResponseCacheKey is AC6: a member's page cached
// before the hide is not served after it. The hide is an append, so it
// moves the log length the key holds, and the member's next request is a
// miss that draws #4 as a tombstone with no content, the same page a fresh
// draw gives.
func TestAutoHideMovesTheResponseCacheKey(t *testing.T) {
	w := newWorld(t)
	id, h := w.flagWorld()
	path := "/api/v1/threads/" + id.String()
	const text = "Agreed with"
	if body := w.same(w.viewer("alice"), path); !strings.Contains(body, text) {
		t.Fatal("#4 is not on alice's page before the hide")
	}
	w.get(w.viewer("alice"), path) // a hit
	before := w.svc.Responses.Stats()
	if before.Hits == 0 {
		t.Fatalf("stats %+v: the page was never served from the cache", before)
	}
	for _, who := range []string{"dave", "erin", "fay"} {
		if rec := send(h, who, http.MethodPost, flagsPath(id), `{"ordinal":4,"reason":"spam"}`); rec.Code != http.StatusOK {
			t.Fatalf("%s's flag: %d %s", who, rec.Code, rec.Body)
		}
	}
	p := page(t, w.get(w.viewer("alice"), path))
	after := w.svc.Responses.Stats()
	if after.Inserts <= before.Inserts {
		t.Errorf("stats before %+v, after %+v: the page after the hide was not drawn afresh", before, after)
	}
	for _, post := range p.Posts {
		if post.Ordinal == 4 && (!post.Deleted || post.HTML != "" || post.Removal == nil || *post.Removal != dto.RemovalReview) {
			t.Errorf("after the hide alice reads #4 as %+v: want a tombstone pending review", post)
		}
	}
	if body := w.same(w.viewer("alice"), path); strings.Contains(body, text) {
		t.Error("the cached page after the hide carries #4's text")
	}
}
