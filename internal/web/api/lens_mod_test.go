package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
)

// Tests for the moderator lens (TKT-01M29CTY6E) and for every lens's
// answer naming a lens that opens it again. The moderator lens's rows in the
// access matrix are in TestLensPostsMatchCanonical.

// TestModLensRefusedAtServiceLayer is AC3: the refusal is in the service,
// so it holds for any caller of Service.View, not only the JSON handler.
// Anonymous and suspended viewers are unauthenticated; a member, the author
// of a post a moderator hid and a moderator of another forum are forbidden;
// a moderator by role, by group grant and an admin are served. On a thread
// in a forum hidden from the viewer, the lens is not-found, as the canonical
// page is.
func TestModLensRefusedAtServiceLayer(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	elsewhere := w.forumAt("elsewhere", domain.VisibilityPublic, false)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		want problem.Code
	}{
		{"anonymous", domain.Anonymous(), problem.CodeUnauthenticated},
		{"suspended", w.session("dave"), problem.CodeUnauthenticated},
		{"a suspended moderator", w.suspendedModerator("sam"), problem.CodeUnauthenticated},
		{"member", w.viewer("erin"), problem.CodeForbidden},
		{"author of a post a moderator hid", w.viewer("alice"), problem.CodeForbidden},
		{"moderator of another forum", w.forumModerator("hank", elsewhere), problem.CodeForbidden},
		{"moderator by role", w.viewer("bob"), ""},
		{"moderator by group grant", w.groupModerator(w.forum), ""},
		{"admin", w.newAdmin(), ""},
	} {
		res, err := w.svc.View(t.Context(), c.v, id, threads.Query{Lens: "mod"}, "")
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v, want the lens", c.name, err)
		case c.want == "" && (res.Body.Lens == nil || res.Body.Lens.Kind != "mod"):
			t.Errorf("%s: lens %+v, want mod", c.name, res.Body.Lens)
		case c.want != "" && problem.FromError(err) != c.want:
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		case c.want != "" && len(res.Body.Posts) != 0:
			t.Errorf("%s was refused and still served %v", c.name, ordinalsOf(res.Body.Posts))
		}
	}

	w.hideForum()
	hidden := newHandlerFor(w)
	for _, who := range []string{"", "carol", "bob"} {
		if _, err := hidden.svc.View(t.Context(), w.viewer(who), id, threads.Query{Lens: "mod"}, ""); problem.FromError(err) != problem.CodeNotFound {
			t.Errorf("%q on a hidden forum: %v, want not-found", who, err)
		}
	}
}

// suspendedModerator is a moderator by role whose account is suspended, as
// the session middleware would read it.
func (w *world) suspendedModerator(handle string) domain.Viewer {
	w.t.Helper()
	w.addUsers("suspended", handle)
	w.exec(`UPDATE users SET role = ? WHERE id = ?`, domain.RoleModerator, w.users[handle])
	return w.session(handle)
}

// TestModLensSelectsRemovedPosts is AC4: the lens keeps exactly the removed
// posts, each with its removal and redacted marks. A redacted post and a
// moved post have no body for anyone; a post hidden pending review, one a
// moderator hid and one its author deleted show their bodies to the
// moderator, as the canonical page does.
func TestModLensSelectsRemovedPosts(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	for _, v := range []domain.Viewer{w.viewer("bob"), w.groupModerator(w.forum), w.newAdmin()} {
		p := page(t, w.get(v, threadPath(id)+"?lens=mod"))
		if p.Lens == nil || p.Lens.Kind != "mod" || p.Lens.Value != "" || p.Lens.Shown != 6 || p.Lens.Hidden != 4 {
			t.Fatalf("user %d: lens %+v", v.UserID, p.Lens)
		}
		if got := ordinalsOf(p.Posts); !slices.Equal(got, []int{3, 5, 6, 7, 8, 9}) {
			t.Fatalf("user %d: posts %v, want the removed ones", v.UserID, got)
		}
		for _, post := range p.Posts {
			removal := ""
			if post.Removal != nil {
				removal = *post.Removal
			}
			want := map[int]struct {
				removal  string
				redacted bool
				body     bool
			}{
				3: {dto.RemovalAuthor, false, true},
				5: {dto.RemovalModerator, false, true},
				6: {dto.RemovalReview, false, true},
				7: {dto.RemovalModerator, false, true},
				8: {dto.RemovalModerator, true, false},
				9: {"", false, false},
			}[post.Ordinal]
			if removal != want.removal || post.Redacted != want.redacted || (post.HTML != "") != want.body {
				t.Errorf("user %d, #%d: removal %q redacted %v body %v; want %+v", v.UserID, post.Ordinal, removal,
					post.Redacted, post.HTML != "", want)
			}
			if strings.Contains(post.HTML, redactedSecret) {
				t.Errorf("#%d carries the redacted text", post.Ordinal)
			}
		}
	}
}

// TestModLensNeverServedFromAMemberCacheEntry is AC4's cache half: a mod
// page is drawn for the moderator class, so a member asking after a
// moderator is refused rather than served the cached page, and a second
// moderator is served the first one's entry.
func TestModLensNeverServedFromAMemberCacheEntry(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	path := threadPath(id) + "?lens=mod"
	first := w.get(w.viewer("bob"), path)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), hiddenSecret) {
		t.Fatalf("the moderator's mod page: %d %s", first.Code, first.Body)
	}
	for who, code := range map[string]int{"erin": http.StatusForbidden, "alice": http.StatusForbidden, "": http.StatusUnauthorized} {
		rec := w.get(w.viewer(who), path)
		if rec.Code != code || strings.Contains(rec.Body.String(), hiddenSecret) {
			t.Errorf("%q after a moderator: %d %s", who, rec.Code, rec.Body)
		}
	}
	before := w.svc.Responses.Stats()
	if rec := w.get(w.groupModerator(w.forum), path); rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("a second moderator: %d", rec.Code)
	}
	if after := w.svc.Responses.Stats(); after.Hits != before.Hits+1 {
		t.Errorf("a second moderator was not served the moderator entry: %+v then %+v", before, after)
	}
}

// TestLensValueRoundTrips is AC6: the lens an answer names, kind alone when
// value is empty and kind:value otherwise (docs/07), serves the same posts
// to the same viewer when opened again. mentions:x and mod:x are not lenses,
// and serve the canonical page.
func TestLensValueRoundTrips(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	for _, c := range []struct{ who, raw string }{
		{"bob", "mod"}, {"gina", "mod"}, {"erin", "mentions"}, {"alice", "mentions"}, {"", "mentions"},
		{"carol", "since:2025-09-10"}, {"", "since:2025-09-10T12:00:00Z"}, {"carol", "since:0"},
		{"carol", "unread"}, {"", "unread"}, {"carol", "author:Alice"}, {"erin", "chain:10"},
	} {
		v := w.viewer(c.who)
		if c.who == "gina" {
			v = w.groupModerator(w.forum)
		}
		first := page(t, w.get(v, threadPath(id)+"?lens="+c.raw))
		if first.Lens == nil {
			t.Fatalf("%q %s: no lens", c.who, c.raw)
		}
		named := first.Lens.Kind
		if first.Lens.Value != "" {
			named += ":" + first.Lens.Value
		}
		again := page(t, w.get(v, threadPath(id)+"?lens="+named))
		a, _ := json.Marshal(first.Posts)
		b, _ := json.Marshal(again.Posts)
		if again.Lens == nil || again.Lens.Kind != first.Lens.Kind || again.Lens.Value != first.Lens.Value || string(a) != string(b) {
			t.Errorf("%q %s names %s, which serves %+v %v, not %+v %v", c.who, c.raw, named, again.Lens,
				ordinalsOf(again.Posts), first.Lens, ordinalsOf(first.Posts))
		}
	}
	canonical := w.get(w.viewer("bob"), threadPath(id)).Body.String()
	for _, raw := range []string{"mentions:x", "mod:x", "mod:", "mentions:"} {
		if got := w.get(w.viewer("bob"), fmt.Sprintf("%s?lens=%s", threadPath(id), raw)).Body.String(); got != canonical {
			t.Errorf("%s: not the canonical page", raw)
		}
	}
}
