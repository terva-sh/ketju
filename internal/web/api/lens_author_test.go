package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the author lens (TKT-01M29CTY58, docs/07 "the lens parameter").
// Its rows in the access matrix are in TestLensPostsMatchCanonical.

// authorLens asserts p is an author lens page echoing value, with the posts
// want, and every other post of the thread hidden.
func authorLens(t *testing.T, name string, p dto.ThreadPage, value string, want []int) {
	t.Helper()
	if p.Lens == nil || p.Lens.Kind != "author" || p.Lens.Value != value || p.Lens.Shown != len(want) ||
		p.Lens.Hidden != p.Thread.PostCount-len(want) {
		t.Errorf("%s: lens %+v, want author %q showing %d of %d", name, p.Lens, value, len(want), p.Thread.PostCount)
	}
	if got := ordinalsOf(p.Posts); !slices.Equal(got, want) {
		t.Errorf("%s: posts %v, want %v", name, got, want)
	}
}

// withoutValue is a lens page's body with lens.value blanked, for comparing
// two answers that differ only in the handle they echo.
func withoutValue(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	p := page(t, rec)
	if p.Lens != nil {
		p.Lens.Value = ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sameShape is statements with each IN list written as one parameter, so a
// lookup asked for more ids reads as the same statement.
func sameShape(ran []string) []string {
	out := make([]string, len(ran))
	for i, s := range ran {
		out[i] = inList.ReplaceAllString(s, "IN (?)")
	}
	return out
}

var inList = regexp.MustCompile(`IN \(\?(,\?)*\)`)

// TestAuthorLensFromIndex is AC1: an author lens page runs the canonical
// page's statements and one more, the handle's lookup, on a thread of 10
// posts and one of 2000, for an anonymous viewer and a member. The selection
// is read from the in-memory index's author ids.
func TestAuthorLensFromIndex(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	counts := map[string][]int{}
	for seq, n := range map[uint64]int{1: 10, 2: 2000} {
		id := w.quickThread(seq, n)
		w.post(id, "carol", "a reply by somebody else", 0)
		canonical, lensed := threadPath(id)+"?page=1", threadPath(id)+"?lens=author:alice&page=1"
		for _, who := range []string{"", "carol"} {
			v := w.viewer(who)
			w.get(v, canonical)
			w.get(v, lensed)
			var c, l *httptest.ResponseRecorder
			ranC := st.during(func() { c = w.get(v, canonical) })
			ranL := st.during(func() { l = w.get(v, lensed) })
			if c.Code != http.StatusOK || l.Code != http.StatusOK {
				t.Fatalf("%d posts, %q: %d and %d", n, who, c.Code, l.Code)
			}
			var lookups int
			rest := slices.DeleteFunc(slices.Clone(ranL), func(s string) bool {
				found := strings.Contains(s, "name: GetUserByHandle ")
				if found {
					lookups++
				}
				return found
			})
			// The canonical page also holds carol's post, so its users
			// lookup is asked for two ids where the lens page's is asked
			// for one: the same statement.
			if lookups != 1 || !slices.Equal(sameShape(ranC), sameShape(rest)) {
				t.Errorf("%d posts, %q: the lens page ran the canonical page's %d statements plus %d handle lookups, want 1\ncanonical %q\nlens      %q",
					n, who, len(ranC), lookups, ranC, ranL)
			}
			if p := page(t, l); p.Lens == nil || p.Lens.Shown != n {
				t.Fatalf("%d posts: lens %+v", n, p.Lens)
			}
			counts[who] = append(counts[who], len(ranL))
		}
	}
	for who, c := range counts {
		if len(c) != 2 || c[0] != c[1] {
			t.Errorf("%q: the lens page ran %v statements on the two threads; the length changed the count", who, c)
		}
	}
}

// TestAuthorLensKeepsVisiblePosts is AC2's filter: on accessThread each
// author's lens keeps the posts shown as themselves, and none deleted,
// hidden, hidden pending review, hidden by a suspension, moved or redacted,
// for a member and a moderator alike.
func TestAuthorLensKeepsVisiblePosts(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	for handle, want := range map[string][]int{
		"alice":  {1},  // #5 a moderator hid, #8 redacted
		"scribe": {2},  // an agent's post
		"bob":    {},   // #3, deleted by bob himself
		"carol":  {4},  // #6 hidden pending review, #9 moved
		"dave":   {},   // #7, hidden by his suspension
		"erin":   {10}, // a reply quoting a hidden post
	} {
		for _, who := range []string{"erin", "bob"} {
			authorLens(t, who+" reading "+handle, page(t, w.get(w.viewer(who), threadPath(id)+"?lens=author:"+handle)), handle, want)
		}
	}
}

// TestAuthorLensCaseInsensitive: the handle matches in any case, and the
// answer echoes it as the lens grammar reads it, lowercased.
func TestAuthorLensCaseInsensitive(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	for _, h := range []string{"carol", "Carol", "CAROL", "cArOl"} {
		authorLens(t, h, page(t, w.get(w.viewer("erin"), threadPath(id)+"?lens=author:"+h)), "carol", []int{4})
	}
}

// TestAuthorLensValueNotCached: the response cache holds only the posts, so
// two spellings of one handle, and two handles nobody holds, share an entry
// while each answer still echoes its own handle.
func TestAuthorLensValueNotCached(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	v := w.viewer("erin")
	for _, pair := range [][2]string{{"Carol", "carol"}, {"nobody", "noone"}} {
		page(t, w.get(v, threadPath(id)+"?lens=author:"+pair[0]))
		before := w.svc.Responses.Stats()
		p := page(t, w.get(v, threadPath(id)+"?lens=author:"+pair[1]))
		if after := w.svc.Responses.Stats(); after.Hits != before.Hits+1 {
			t.Errorf("%s after %s was not a cache hit: %+v then %+v", pair[1], pair[0], before, after)
		}
		if p.Lens == nil || p.Lens.Value != pair[1] {
			t.Errorf("%s after %s: lens %+v, want its own value", pair[1], pair[0], p.Lens)
		}
	}
}

// TestAuthorLensUnknownHandleAnswersAsNoPosts is AC2's refusal to be a
// probe: a handle nobody holds, a member with no posts here, a suspended
// member whose every post here a suspension hid, a pending applicant and a
// handle a rename released all answer the same body apart from the handle
// they echo, and the same status, to every viewer class.
func TestAuthorLensUnknownHandleAnswersAsNoPosts(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	w.addUsers("active", "hank")
	w.addUsers("pending", "pia")
	w.renameTo("carol", "carolyn")
	for _, who := range []string{"", "erin", "bob"} {
		v := w.viewer(who)
		want := w.get(v, threadPath(id)+"?lens=author:nobody")
		if want.Code != http.StatusOK {
			t.Fatalf("%q: an unknown handle answers %d", who, want.Code)
		}
		authorLens(t, who+" nobody", page(t, want), "nobody", []int{})
		for _, h := range []string{"hank", "dave", "pia", "carol"} {
			got := w.get(v, threadPath(id)+"?lens=author:"+h)
			if got.Code != want.Code || withoutValue(t, got) != withoutValue(t, want) {
				t.Errorf("%q: author:%s differs from a handle nobody holds\n got %s\nwant %s", who, h, got.Body, want.Body)
			}
		}
		// The member who renamed is found by the handle they hold now.
		authorLens(t, who+" carolyn", page(t, w.get(v, threadPath(id)+"?lens=author:carolyn")), "carolyn", []int{4})
	}
}

// TestAuthorLensEmptyAfterModeration is AC4: an author whose every post here
// a suspension hid gets shown 0 and one empty page, 200, never an error.
func TestAuthorLensEmptyAfterModeration(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	for _, who := range []string{"", "erin", "bob"} {
		rec := w.get(w.viewer(who), threadPath(id)+"?lens=author:dave")
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: %d %s", who, rec.Code, rec.Body)
		}
		p := page(t, rec)
		authorLens(t, who, p, "dave", []int{})
		if p.Page != (dto.Pagination{Number: 1, Size: 25, TotalPages: 1}) || !slices.Equal(p.Lens.Gaps, gaps(0, 10)) {
			t.Errorf("%q: page %+v gaps %v, want one empty page", who, p.Page, p.Lens.Gaps)
		}
	}
}

// TestAuthorLensMissingStarter: on a thread whose starter the database
// lacks, the header draws them as deleted-user-<id>, and that handle's lens
// selects their posts, so "Only <handle>" works. The same form naming a live
// account's id selects nothing, so it never reaches that account's posts.
// An anonymized starter's placeholder is held by their users row, and
// selects their posts through it.
func TestAuthorLensMissingStarter(t *testing.T) {
	w := newWorld(t)
	const lost = 778
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | 9)
	th, err := w.cache.Create(id, uint64(w.forum), w.clock)
	if err != nil {
		t.Fatal(err)
	}
	w.cache.Release(th)
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: lost,
		Body: &threadlog.ThreadCreatedBody{Title: "Lost", Slug: "lost", ForumID: uint64(w.forum)}})
	w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: lost, Body: &threadlog.PostBody{Source: "first"}})
	w.post(id, "alice", "second", 0)
	w.append(id, threadlog.Append{Type: threadlog.TypePost, Author: lost, Body: &threadlog.PostBody{Source: "third"}})

	carol := w.viewer("carol")
	header := page(t, w.get(carol, threadPath(id))).Thread.Author.Handle
	if header != domain.DeletedMemberHandle(lost) {
		t.Fatalf("the header draws the missing starter as %q", header)
	}
	authorLens(t, "the missing starter", page(t, w.get(carol, threadPath(id)+"?lens=author:"+header)), header, []int{1, 3})

	alias := domain.DeletedMemberHandle(w.users["alice"])
	authorLens(t, "a live account's id", page(t, w.get(carol, threadPath(id)+"?lens=author:"+alias)), alias, []int{})
	for _, h := range []string{"deleted-user-0778", "deleted-user-0", "deleted-user-"} {
		if p := page(t, w.get(carol, threadPath(id)+"?lens=author:"+h)); p.Lens != nil && p.Lens.Shown != 0 {
			t.Errorf("%s selected %v", h, ordinalsOf(p.Posts))
		}
	}

	w.anonymize(w.anonymizer(), "alice")
	authorLens(t, "an anonymized author", page(t, w.get(carol, threadPath(id)+"?lens=author:"+alias)), alias, []int{2})
}

// TestThreadHeaderAuthorCostsNoStatement is AC3: the thread page draws its
// header's author, the starter, in the users lookup it runs for the page's
// posts, so a page whose posts the starter did not write runs one users
// lookup, as a page of the starter's own posts does, and the header names
// the starter on both.
func TestThreadHeaderAuthorCostsNoStatement(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	id := w.quickThread(1, 25)
	for range 25 {
		w.post(id, "carol", "a page alice did not write", 0)
	}
	v := w.viewer("carol")
	ran := map[int][]string{}
	for _, n := range []int{1, 2} {
		path := fmt.Sprintf("%s?page=%d", threadPath(id), n)
		w.get(v, path)
		var rec *httptest.ResponseRecorder
		ran[n] = st.during(func() { rec = w.get(v, path) })
		p := page(t, rec)
		if p.Thread.Author.Handle != "alice" {
			t.Errorf("page %d: the header's author is %+v, want alice", n, p.Thread.Author)
		}
		for _, post := range p.Posts {
			if want := map[int]string{1: "alice", 2: "carol"}[n]; post.Author.Handle != want {
				t.Fatalf("page %d holds #%d by %s", n, post.Ordinal, post.Author.Handle)
			}
		}
		var users int
		for _, s := range ran[n] {
			if strings.Contains(s, "FROM users") {
				users++
			}
		}
		if users != 1 {
			t.Errorf("page %d ran %d users lookups, want the one batch\n%q", n, users, ran[n])
		}
	}
	if len(ran[1]) != len(ran[2]) {
		t.Errorf("page 1 ran %d statements and page 2 %d", len(ran[1]), len(ran[2]))
	}
}
