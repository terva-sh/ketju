package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/blobs/blobstest"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for POST /api/v1/admin/threads/{id}/split (TKT-01M29CYSAM). The
// split itself is tested in internal/service/threads (split_test.go); these
// hold the route: its gate, its body and its answer.

func splitBody(forum, title string, ordinals ...int) string {
	if ordinals == nil {
		ordinals = []int{}
	}
	b, _ := json.Marshal(map[string]any{"ordinals": ordinals, "title": title, "forum": forum})
	return string(b)
}

// TestSplitRouteGated: the route is behind the moderates gate, so an
// anonymous viewer, an applicant and a member are refused before the body
// is read; past it the body is read strictly and every field is required;
// gina, who moderates general and den through a group, splits into den and
// is refused in lounge and vault; bob and an admin split as moderators.
// Each refusal leaves the log and the audit log as they were.
func TestSplitRouteGated(t *testing.T) {
	w, _, _, _, _ := forumModWorld(t)
	admin := w.newAdmin()
	h := w.chain()
	id := w.threadIn(40, w.forum, "Behind the gate", "alice")
	for range 3 {
		w.post(id, "carol", "a reply worth its own thread", 0)
	}
	path := modPath(id, "split")
	for _, c := range []struct{ who, body, want string }{
		{"", splitBody("den", "Away", 2), "unauthenticated"},
		{"pre-user", splitBody("den", "Away", 2), "forbidden"},
		{"carol", splitBody("den", "Away", 2), "forbidden"},
		{"carol", `{"not":"a field this route takes"}`, "forbidden"},
		{"gina", `{"ordinals":[2],"title":"Away"}`, "bad-request"},
		{"gina", `{"ordinals":[2],"forum":"den"}`, "bad-request"},
		{"gina", `{"title":"Away","forum":"den"}`, "bad-request"},
		{"gina", `{"ordinals":[2],"title":"Away","forum":"den","tags":[]}`, "bad-request"},
		{"gina", `{"ordinals":["2"],"title":"Away","forum":"den"}`, "bad-request"},
		{"gina", `{"ordinals":[-2],"title":"Away","forum":"den"}`, "bad-request"},
		{"gina", `{"ordinals":[2],"title":"Away","forum":"den"`, "bad-request"},
		{"gina", `{"ordinals":[` + strings.Repeat("2,", 70_000) + `2],"title":"Away","forum":"den"}`, "payload-too-large"},
		{"gina", splitBody("den", "Away"), "validation-failed"},
		{"gina", splitBody("den", "", 2), "validation-failed"},
		{"gina", splitBody("lounge", "Away", 2), "forbidden"},
		{"gina", splitBody("vault", "Away", 2), "not-found"},
		{"gina", splitBody("den", "Away", 99), "not-found"},
	} {
		w.unchanged(t, id, fmt.Sprintf("a split as %q with %.60s", c.who, c.body), c.want, func() string {
			return problemCode(t, send(h, c.who, http.MethodPost, path, c.body))
		})
	}

	for i, c := range []struct {
		who     string
		forum   string
		ordinal int
		send    func(body string) *httptest.ResponseRecorder
	}{
		{"gina", "den", 2, func(b string) *httptest.ResponseRecorder { return send(h, "gina", http.MethodPost, path, b) }},
		{"bob", "general", 3, func(b string) *httptest.ResponseRecorder { return send(h, "bob", http.MethodPost, path, b) }},
		{"an admin", "vault", 4, func(b string) *httptest.ResponseRecorder { return w.sendAs(admin, http.MethodPost, path, b) }},
	} {
		title := fmt.Sprintf("Split by %s", c.who)
		rec := c.send(splitBody(c.forum, title, c.ordinal))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's split: %d %s", c.who, rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s's split answered Cache-Control %q", c.who, cc)
		}
		var res dto.MovedPosts
		dec := json.NewDecoder(rec.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&res); err != nil {
			t.Fatal(err)
		}
		if res.Source.ID != id.String() || res.Source.PostCount != 4 {
			t.Errorf("%s's split answered the source %+v", c.who, res.Source)
		}
		if res.Thread.Forum.Slug != c.forum || res.Thread.Title != title || res.Thread.PostCount != 1 {
			t.Errorf("%s's split answered the new thread %+v", c.who, res.Thread)
		}
		if sf := res.Thread.SplitFrom; sf == nil || sf.Thread == nil || sf.Thread.ID != id.String() {
			t.Errorf("%s's new thread says it was split from %+v", c.who, sf)
		}
		if n := len(w.auditLog()); n == 0 || w.auditLog()[n-1].action != "posts.moved" {
			t.Errorf("%s's split (%d) wrote no posts.moved audit row last: %+v", c.who, i, w.auditLog())
		}
	}
}

// TestMovedPostBlobFollowsTarget is the blob half of TKT-01M29CXJZZ's sixth
// criterion: a moved post's attachment is served under the target's rules.
// A copy adds no blob reference, and its post_attachments row is what
// admits a reader of the target: one split into a restricted forum is
// served to its reader alone, and one split into a members forum to members,
// while the source's other readers are answered as for a hash nothing has.
func TestMovedPostBlobFollowsTarget(t *testing.T) {
	bw := newBlobWorld(t)
	bw.admin()
	bw.grantUpload(0)
	bw.forumAt("lounge", domain.VisibilityMembers, false)
	bw.forumAt("staff", domain.VisibilityRestricted, false)
	id := bw.threadIn(100, bw.forum, "Attachments to move", "alice")
	toStaff := bw.uploaded("alice", "staff.png", blobstest.PNG(2, 2, 150)).Hash
	toLounge := bw.uploaded("alice", "lounge.png", blobstest.PNG(2, 2, 151)).Hash
	created[dto.CreatedPost](t, bw.replyWith("alice", id, toStaff))
	created[dto.CreatedPost](t, bw.replyWith("alice", id, toLounge))
	checkAccess(t, bw, []accessCase{
		{"before the split", toStaff, []string{"", "carol", "alice", "bob"}, nil},
	}, "")
	refs := []int64{bw.refcount(toStaff), bw.refcount(toLounge)}

	split := func(ordinal uint32, forum string) threadid.ID {
		t.Helper()
		bw.clock += 1000
		res, err := bw.svc.Split(t.Context(), bw.as("dora"), id, []uint32{ordinal}, forum, "Moved to "+forum)
		if err != nil {
			t.Fatal(err)
		}
		target, err := threadid.Parse(res.Thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	staffThread := split(2, "staff")
	split(3, "lounge")

	if got := []int64{bw.refcount(toStaff), bw.refcount(toLounge)}; got[0] != refs[0] || got[1] != refs[1] {
		t.Errorf("the split changed the refcounts: %v, were %v", got, refs)
	}
	page, err := bw.svc.View(t.Context(), bw.as("dora"), staffThread, threads.Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if a := page.Body.Posts[0].Attachments; len(a) != 1 || a[0].Hash != toStaff {
		t.Errorf("the copy in staff carries %+v", a)
	}
	checkAccess(t, bw, []accessCase{
		{"split into a restricted forum", toStaff, []string{"dora"}, []string{"", "carol", "alice", "bob", "inactive:carol"}},
		{"split into a members forum", toLounge, []string{"carol", "alice", "bob", "dora"}, []string{"", "inactive:carol"}},
	}, "")
}
