package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for POST /api/v1/admin/threads/{id}/merge (TKT-01M29CYSBA). The
// merge itself is tested in internal/service/threads (merge_test.go); these
// hold the route, its gate, its body and its answer, and that a merge
// notifies nobody.

func mergeBody(into string, ordinals ...int) string {
	if ordinals == nil {
		ordinals = []int{}
	}
	b, _ := json.Marshal(map[string]any{"ordinals": ordinals, "into": into})
	return string(b)
}

// TestMergeRouteGated: the route is behind the moderates gate, so an
// anonymous viewer, an applicant and a member are refused before the body
// is read; past it the body is read strictly and both fields are required;
// gina, who moderates general and den through a group, merges into den, is
// forbidden lounge, which she reads, and is told vault's thread, which she
// does not read, is not found, as is a thread that does not exist; bob and
// an admin merge as moderators. Each refusal leaves the log and the audit
// log as they were.
func TestMergeRouteGated(t *testing.T) {
	w, _, lounge, den, vault := forumModWorld(t)
	admin := w.newAdmin()
	h := w.chain()
	id := w.threadIn(40, w.forum, "Behind the gate", "alice")
	for range 3 {
		w.post(id, "carol", "a reply that belongs elsewhere", 0)
	}
	inDen := w.threadIn(41, den, "In den", "alice")
	inLounge := w.threadIn(42, lounge, "In lounge", "alice")
	inVault := w.threadIn(43, vault, "In vault", "alice")
	inGeneral := w.threadIn(44, w.forum, "In general", "alice")
	nowhere := threadid.ID(uint64(1_757_500_000_000)<<16 | 99).String()
	path := modPath(id, "merge")
	targets := map[threadid.ID]logState{}
	for _, target := range []threadid.ID{inDen, inLounge, inVault} {
		targets[target] = w.logState(target)
	}
	for _, c := range []struct{ who, body, want string }{
		{"", mergeBody(inDen.String(), 2), "unauthenticated"},
		{"pre-user", mergeBody(inDen.String(), 2), "forbidden"},
		{"carol", mergeBody(inDen.String(), 2), "forbidden"},
		{"carol", `{"not":"a field this route takes"}`, "forbidden"},
		{"gina", `{"ordinals":[2]}`, "bad-request"},
		{"gina", `{"into":"` + inDen.String() + `"}`, "bad-request"},
		{"gina", `{"ordinals":[2],"into":"` + inDen.String() + `","title":"x"}`, "bad-request"},
		{"gina", `{"ordinals":["2"],"into":"` + inDen.String() + `"}`, "bad-request"},
		{"gina", `{"ordinals":[2],"into":7}`, "bad-request"},
		{"gina", `{"ordinals":[2],"into":"` + inDen.String() + `"`, "bad-request"},
		{"gina", `{"ordinals":[` + strings.Repeat("2,", 70_000) + `2],"into":"` + inDen.String() + `"}`, "payload-too-large"},
		{"gina", mergeBody(inDen.String()), "validation-failed"},
		{"gina", mergeBody("not a thread", 2), "validation-failed"},
		{"gina", mergeBody(id.String(), 2), "validation-failed"},
		{"gina", mergeBody(inLounge.String(), 2), "forbidden"},
		{"gina", mergeBody(inVault.String(), 2), "not-found"},
		{"gina", mergeBody(nowhere, 2), "not-found"},
		{"gina", mergeBody(inDen.String(), 99), "not-found"},
	} {
		w.unchanged(t, id, fmt.Sprintf("a merge as %q with %.60s", c.who, c.body), c.want, func() string {
			return problemCode(t, send(h, c.who, http.MethodPost, path, c.body))
		})
	}
	for _, target := range []threadid.ID{inDen, inLounge, inVault} {
		if s := w.logState(target); s != targets[target] {
			t.Errorf("a refused merge wrote to the target %s: %+v, was %+v", target, s, targets[target])
		}
	}

	for _, c := range []struct {
		who     string
		target  threadid.ID
		ordinal int
		send    func(body string) *httptest.ResponseRecorder
	}{
		{"gina", inDen, 2, func(b string) *httptest.ResponseRecorder { return send(h, "gina", http.MethodPost, path, b) }},
		{"bob", inGeneral, 3, func(b string) *httptest.ResponseRecorder { return send(h, "bob", http.MethodPost, path, b) }},
		{"an admin", inVault, 4, func(b string) *httptest.ResponseRecorder { return w.sendAs(admin, http.MethodPost, path, b) }},
	} {
		rec := c.send(mergeBody(c.target.String(), c.ordinal))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s's merge: %d %s", c.who, rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s's merge answered Cache-Control %q", c.who, cc)
		}
		var res dto.MergedPosts
		dec := json.NewDecoder(rec.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&res); err != nil {
			t.Fatal(err)
		}
		if res.Source.ID != id.String() || res.Source.PostCount != 4 {
			t.Errorf("%s's merge answered the source %+v", c.who, res.Source)
		}
		if res.Target.ID != c.target.String() || res.Target.PostCount != 2 || res.Target.SplitFrom != nil {
			t.Errorf("%s's merge answered the target %+v", c.who, res.Target)
		}
		if n := len(w.auditLog()); n == 0 || w.auditLog()[n-1].action != "posts.moved" {
			t.Errorf("%s's merge wrote no posts.moved audit row last: %+v", c.who, w.auditLog())
		}
	}
}

// TestMergeNotifiesNobody is half of TKT-01M29CYSBA's fourth criterion,
// through the route and the generator together: a merge of a post that
// mentions dave and of dave's reply to alice into a thread carol watches
// notifies none of them, nor bob who merged it: the generator writes no
// row, and nobody lists a notification of the target. The notifications of
// the moved originals stop being listed, as for any moved post
// (TestNotificationsNeverWidenAccess (e)). A post written in the target
// afterwards still reaches carol, so the watch is live.
func TestMergeNotifiesNobody(t *testing.T) {
	w := newNotifyWorld(t)
	src := w.threadIn(w.forum, "Source")
	dst := w.threadIn(w.forum, "Target")
	w.say(dst, "bob", "the target's opener", 0)
	if rec := w.call("carol", http.MethodPost, "/api/v1/threads/"+dst.String()+"/watch", `{"level":"watch"}`); rec.Code != http.StatusOK {
		t.Fatalf("carol watching the target: %d %s", rec.Code, rec.Body)
	}
	w.say(src, "alice", "hello @dave", 0, "dave")
	w.say(src, "dave", "hello alice", 1)
	rows := func() int {
		var n int
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM notifications`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	who := []string{"alice", "bob", "carol", "dave"}
	if len(w.list("dave")) != 1 || len(w.list("alice")) != 1 {
		t.Fatal("dave was not notified of the mention, or alice of the reply, so the test proves nothing")
	}
	was := rows()

	rec := w.call("bob", http.MethodPost, "/api/v1/admin/threads/"+src.String()+"/merge", mergeBody(dst.String(), 1, 2))
	if rec.Code != http.StatusOK {
		t.Fatalf("bob's merge: %d %s", rec.Code, rec.Body)
	}
	for _, id := range []threadid.ID{src, dst} {
		var n int64
		if err := w.cache.With(id, func(th *threadcache.Thread) error {
			n = th.Actor.LogLength()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		w.generate(id, n)
	}
	if n := rows(); n != was {
		t.Errorf("the merge wrote %d notification rows", n-was)
	}
	for _, h := range who {
		for _, n := range w.list(h) {
			if n.Thread != nil && n.Thread.ID == dst.String() {
				t.Errorf("%s is notified of the target: %+v", h, n)
			}
		}
	}

	w.say(dst, "alice", "after the merge", 0)
	if got := len(w.list("carol")); got != 1 {
		t.Errorf("carol lists %d after a post in the thread she watches, want 1", got)
	}
}
