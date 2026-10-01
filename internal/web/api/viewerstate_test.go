package api

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the viewer-state endpoints (TKT-01M29CA9CE): the read position
// and reply drafts. The seeded thread (thread_test.go) has four posts, and
// carol has read to 2.

func readPath(id threadid.ID) string  { return "/api/v1/threads/" + id.String() + "/read" }
func draftPath(id threadid.ID) string { return "/api/v1/drafts/" + id.String() }

// ok decodes a 200 strictly into T and checks it is not cached.
func ok[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding %T: %v\n%s", v, err, rec.Body)
	}
	return v
}

// position is the stored read position of who in id, or -1 with no row.
func (w *world) position(who string, id threadid.ID) int64 {
	w.t.Helper()
	var rp dbq.ReadPosition
	err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rp, err = q.GetReadPosition(w.t.Context(), dbq.GetReadPositionParams{UserID: w.users[who], ThreadID: int64(id)})
		return err
	})
	if err == sql.ErrNoRows {
		return -1
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return rp.LastReadOrdinal
}

// longThread is a thread of n posts by alice.
func (w *world) longThread(seq uint64, n int) threadid.ID {
	w.t.Helper()
	id, _ := w.thread(seq, "A long one")
	for i := range n {
		w.post(id, "alice", fmt.Sprintf("post %d", i+1), 0)
	}
	return id
}

// TestReadNeverMovesBack is AC1: beacons for 12, 40, 30 and 39 arriving in
// any order leave the position at 40, and each answer is the position as it
// stands after that beacon, never below one already answered. An ordinal
// past the last post is read as the last post.
func TestReadNeverMovesBack(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(2, 45)
	h := w.chain()
	beacons := []int{12, 40, 30, 39}
	for round := range 5 {
		rand.Shuffle(len(beacons), func(i, j int) { beacons[i], beacons[j] = beacons[j], beacons[i] })
		best := 0
		if round > 0 {
			best = 40
		}
		for _, n := range beacons {
			best = max(best, n)
			got := ok[dto.ReadPosition](t, send(h, "carol", http.MethodPut, readPath(id), fmt.Sprintf(`{"ordinal":%d}`, n)))
			if got.LastReadOrdinal != best {
				t.Fatalf("round %d, order %v: beacon %d answered %d, want %d", round, beacons, n, got.LastReadOrdinal, best)
			}
		}
	}
	if got := w.position("carol", id); got != 40 {
		t.Fatalf("stored position %d, want 40", got)
	}

	// Past the end clamps to the last post, and a later lower beacon cannot
	// pull it back.
	if got := ok[dto.ReadPosition](t, send(h, "carol", http.MethodPut, readPath(id), `{"ordinal":4000}`)); got.LastReadOrdinal != 45 {
		t.Errorf("a beacon past the end answered %d, want 45", got.LastReadOrdinal)
	}
	if got := ok[dto.ReadPosition](t, send(h, "carol", http.MethodPut, readPath(id), `{"ordinal":3}`)); got.LastReadOrdinal != 45 {
		t.Errorf("a late beacon for 3 answered %d, want 45", got.LastReadOrdinal)
	}

	// The thread page reads the same integer.
	if p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())); p.Viewer.LastReadOrdinal != 45 {
		t.Errorf("the thread page says %d", p.Viewer.LastReadOrdinal)
	}
}

// TestConcurrentBeacons is AC1 under -race: beacons from two devices at once,
// many of each, in no order. However they interleave, the position ends at
// the highest, and no answer is lower than the position was before the
// request that produced it could have started.
func TestConcurrentBeacons(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(3, 40)
	h := w.chain()
	var wg sync.WaitGroup
	for device := range 2 {
		for _, n := range []int{12, 40, 30, 39, 5, 22, 31, 38} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec := send(h, "carol", http.MethodPut, readPath(id), fmt.Sprintf(`{"ordinal":%d}`, n))
				if rec.Code != http.StatusOK {
					t.Errorf("device %d, beacon %d: %d %s", device, n, rec.Code, rec.Body)
					return
				}
				var got dto.ReadPosition
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Error(err)
					return
				}
				if got.LastReadOrdinal < n {
					t.Errorf("device %d, beacon %d answered %d: below its own ordinal", device, n, got.LastReadOrdinal)
				}
			}()
		}
	}
	wg.Wait()
	if got := w.position("carol", id); got != 40 {
		t.Fatalf("after concurrent beacons the position is %d, want 40", got)
	}
}

// TestMarkUnread is AC2: DELETE is the only way the position goes down. It
// lowers to the ordinal given, or to 0 with none; it never raises; a beacon
// below it after that does not lower it further, and one above moves it on.
func TestMarkUnread(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(2, 10)
	h := w.chain()
	ok[dto.ReadPosition](t, send(h, "carol", http.MethodPut, readPath(id), `{"ordinal":8}`))

	steps := []struct {
		name, method, body string
		want               int
	}{
		{"mark unread after 5", http.MethodDelete, `{"ordinal":5}`, 5},
		{"a late beacon for 3", http.MethodPut, `{"ordinal":3}`, 5},
		{"mark unread after 9, past the position", http.MethodDelete, `{"ordinal":9}`, 5},
		{"read on to 6", http.MethodPut, `{"ordinal":6}`, 6},
		{"mark the whole thread unread", http.MethodDelete, ``, 0},
		{"mark unread again", http.MethodDelete, `{}`, 0},
		{"read on to 2", http.MethodPut, `{"ordinal":2}`, 2},
	}
	for _, s := range steps {
		got := ok[dto.ReadPosition](t, send(h, "carol", s.method, readPath(id), s.body))
		if got.LastReadOrdinal != s.want {
			t.Fatalf("%s: answered %d, want %d", s.name, got.LastReadOrdinal, s.want)
		}
		if stored := w.position("carol", id); stored != int64(s.want) {
			t.Fatalf("%s: stored %d, want %d", s.name, stored, s.want)
		}
	}

	// A DELETE with no body and no content type, as a plain fetch sends it.
	r := httptest.NewRequest(http.MethodDelete, readPath(id), nil)
	r.Header.Set("X-Test-Viewer", "carol")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := ok[dto.ReadPosition](t, rec); got.LastReadOrdinal != 0 {
		t.Errorf("a bare DELETE answered %d, want 0", got.LastReadOrdinal)
	}

	// Mark-unread on a thread never opened writes a row at 0: it cannot
	// mark anything read.
	if got := ok[dto.ReadPosition](t, send(h, "alice", http.MethodDelete, readPath(id), `{"ordinal":7}`)); got.LastReadOrdinal != 0 {
		t.Errorf("mark-unread with no position answered %d, want 0", got.LastReadOrdinal)
	}
	if got := w.position("alice", id); got != 0 {
		t.Errorf("mark-unread with no position stored %d, want 0", got)
	}
}

// sendAs makes a JSON request straight to the API handler as v, which lets a
// test use a session the chain's X-Test-Viewer names cannot: the same user
// on a second device.
func (w *world) sendAs(v domain.Viewer, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(domain.WithViewer(r.Context(), v))
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, r)
	return rec
}

// TestDraftFollowsTheUser is AC3: a draft saved from one session is the
// draft another session of the same user reads, a second save replaces it
// in the one row with a later updated_at and the next revision, and another
// user sees none of it.
func TestDraftFollowsTheUser(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	laptop, phone := w.viewer("carol"), w.viewer("carol")
	laptop.SessionKey, phone.SessionKey = "s-carol-laptop", "s-carol-phone"

	if rec := w.sendAs(phone, http.MethodGet, draftPath(id), ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("no draft yet: %d %q, want 204 and no body", rec.Code, rec.Body)
	}

	saved := ok[dto.Draft](t, w.sendAs(laptop, http.MethodPut, draftPath(id), `{"source":"Half a thought","reply_to":1}`))
	want := dto.Draft{ThreadID: id.String(), Source: "Half a thought", ReplyTo: ptrU32(1), Attachments: []dto.Attachment{}, UpdatedAt: dto.Millis(w.clock), Revision: 1}
	if renderJSON(t, saved) != renderJSON(t, want) {
		t.Fatalf("saved %s, want %s", renderJSON(t, saved), renderJSON(t, want))
	}
	if got := ok[dto.Draft](t, w.sendAs(phone, http.MethodGet, draftPath(id), "")); renderJSON(t, got) != renderJSON(t, saved) {
		t.Fatalf("the other device reads %s, want %s", renderJSON(t, got), renderJSON(t, saved))
	}

	w.clock += 5_000
	again := ok[dto.Draft](t, w.sendAs(phone, http.MethodPut, draftPath(id), `{"source":"Half a thought, finished"}`))
	if again.ReplyTo != nil || again.Source != "Half a thought, finished" || again.UpdatedAt <= saved.UpdatedAt || again.Revision != 2 {
		t.Fatalf("second save %s after %s", renderJSON(t, again), renderJSON(t, saved))
	}
	if got := ok[dto.Draft](t, w.sendAs(laptop, http.MethodGet, draftPath(id), "")); renderJSON(t, got) != renderJSON(t, again) {
		t.Fatalf("the first device reads %s, want %s", renderJSON(t, got), renderJSON(t, again))
	}
	var rows int
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM drafts WHERE user_id = ?`, w.users["carol"]).Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("%d draft rows for carol, want 1", rows)
	}

	// Somebody else's draft is not yours.
	if rec := send(w.chain(), "alice", http.MethodGet, draftPath(id), ""); rec.Code != http.StatusNoContent {
		t.Errorf("alice reads carol's draft: %d %s", rec.Code, rec.Body)
	}

	// An empty source is still a draft: the reply chip alone.
	if got := ok[dto.Draft](t, send(w.chain(), "carol", http.MethodPut, draftPath(id), `{"source":"","reply_to":4}`)); got.Source != "" || got.ReplyTo == nil || *got.ReplyTo != 4 {
		t.Errorf("an empty draft with a reply target: %s", renderJSON(t, got))
	}

	// Delete, then delete again: both 204, and the draft is gone.
	for range 2 {
		if rec := send(w.chain(), "carol", http.MethodDelete, draftPath(id), ""); rec.Code != http.StatusNoContent {
			t.Fatalf("delete: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := w.sendAs(phone, http.MethodGet, draftPath(id), ""); rec.Code != http.StatusNoContent {
		t.Errorf("after delete: %d %s", rec.Code, rec.Body)
	}
}

// TestViewerStateRefusals: who may keep viewer state, and what a request
// can get wrong. Every refusal writes nothing.
func TestViewerStateRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	type req struct{ method, path, body string }
	routes := []req{
		{http.MethodPut, readPath(id), `{"ordinal":1}`},
		{http.MethodDelete, readPath(id), ``},
		{http.MethodGet, draftPath(id), ``},
		{http.MethodPut, draftPath(id), `{"source":"hi"}`},
		{http.MethodDelete, draftPath(id), ``},
	}
	for who, want := range map[string]string{"": "unauthenticated", "pre-user": "forbidden", "inactive": "unauthenticated"} {
		for _, r := range routes {
			if rec := send(h, who, r.method, r.path, r.body); problemCode(t, rec) != want {
				t.Errorf("%q %s %s: %d %s, want %s", who, r.method, r.path, rec.Code, rec.Body, want)
			}
		}
	}

	for _, c := range []struct {
		name string
		r    req
		want string
	}{
		{"read with no ordinal", req{http.MethodPut, readPath(id), `{}`}, "bad-request"},
		{"read at ordinal 0", req{http.MethodPut, readPath(id), `{"ordinal":0}`}, "bad-request"},
		{"read at a negative ordinal", req{http.MethodPut, readPath(id), `{"ordinal":-1}`}, "bad-request"},
		{"read with an unknown field", req{http.MethodPut, readPath(id), `{"ordinal":1,"at":2}`}, "bad-request"},
		{"unread with an unknown field", req{http.MethodDelete, readPath(id), `{"to":1}`}, "bad-request"},
		{"a thread id that is not base32", req{http.MethodPut, "/api/v1/threads/!!/read", `{"ordinal":1}`}, "bad-request"},
		{"a thread that does not exist", req{http.MethodPut, readPath(threadid.ID(99)), `{"ordinal":1}`}, "not-found"},
		{"a draft with no source", req{http.MethodPut, draftPath(id), `{"reply_to":1}`}, "bad-request"},
		{"a draft replying to no post", req{http.MethodPut, draftPath(id), `{"source":"hi","reply_to":5}`}, "validation-failed"},
		{"a draft replying to post 0", req{http.MethodPut, draftPath(id), `{"source":"hi","reply_to":0}`}, "validation-failed"},
		{"a draft for no thread", req{http.MethodGet, draftPath(threadid.ID(99)), ``}, "not-found"},
	} {
		if rec := send(h, "alice", c.r.method, c.r.path, c.r.body); problemCode(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if got := w.position("alice", id); got != -1 {
		t.Errorf("refused requests left alice a read position of %d", got)
	}

	// A member who may read but not reply keeps a read position, and may
	// read and discard a draft, but may not save one.
	w.write(func(q *dbq.Queries) error {
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
			ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
	})
	h = newHandlerFor(w).chain()
	ok[dto.ReadPosition](t, send(h, "alice", http.MethodPut, readPath(id), `{"ordinal":1}`))
	if rec := send(h, "alice", http.MethodPut, draftPath(id), `{"source":"hi"}`); problemCode(t, rec) != "forbidden" {
		t.Errorf("a draft without reply: %d %s", rec.Code, rec.Body)
	}
	if rec := send(h, "alice", http.MethodGet, draftPath(id), ``); rec.Code != http.StatusNoContent {
		t.Errorf("reading drafts without reply: %d %s", rec.Code, rec.Body)
	}

	// A thread in a forum hidden from the viewer is not-found everywhere.
	w.hideForum()
	w.revokeForumRead()
	h = newHandlerFor(w).chain()
	for _, r := range routes {
		if rec := send(h, "carol", r.method, r.path, r.body); problemCode(t, rec) != "not-found" {
			t.Errorf("hidden: %s %s: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
	}
	if got := w.position("carol", id); got != 2 {
		t.Errorf("a refused beacon moved carol from 2 to %d", got)
	}
}

// formPostRow matches a row of docs/07's form-post table: the JSON route in
// the first cell, in backticks.
var formPostRow = regexp.MustCompile("^\\| `((?:GET|PUT|POST|DELETE|PATCH) /api/v1/[^`]+)` \\|")

// TestEveryViewerStateRouteHasAFormPost is AC4: each viewer-state route this
// package serves has a row in docs/07's "Form posts for viewer state" table,
// saying what the HTML client sends instead. The routes are checked to be
// registered, so the list here cannot name a route that does not exist.
func TestEveryViewerStateRouteHasAFormPost(t *testing.T) {
	served := []string{
		"PUT /api/v1/threads/{id}/read",
		"DELETE /api/v1/threads/{id}/read",
		"GET /api/v1/drafts/{thread_id}",
		"PUT /api/v1/drafts/{thread_id}",
		"DELETE /api/v1/drafts/{thread_id}",
		"GET /api/v1/forums/{slug}/draft",
		"PUT /api/v1/forums/{slug}/draft",
		"DELETE /api/v1/forums/{slug}/draft",
		"POST /api/v1/threads/{id}/watch",
		"POST /api/v1/forums/{slug}/watch",
	}
	w := newWorld(t)
	id, _ := w.seed()
	for _, route := range served {
		method, path, _ := strings.Cut(route, " ")
		path = strings.NewReplacer("{id}", id.String(), "{thread_id}", id.String(), "{slug}", "general").Replace(path)
		// An anonymous request reaches the handler and is refused by it;
		// the catch-all for an unregistered route says not-found.
		if rec := send(w.chain(), "", method, path, `{}`); problemCode(t, rec) != "unauthenticated" {
			t.Errorf("%s is not registered: %d %s", route, rec.Code, rec.Body)
		}
	}

	f, err := os.Open("../../../docs/07-api-and-clients.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	documented := map[string]bool{}
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#"):
			in = strings.TrimSpace(strings.TrimLeft(line, "#")) == "Form posts for viewer state"
		case in:
			if m := formPostRow.FindStringSubmatch(line); m != nil {
				documented[m[1]] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(documented) == 0 {
		t.Fatal(`docs/07 has no "Form posts for viewer state" table, or it has no rows`)
	}
	for _, route := range served {
		if !documented[route] {
			t.Errorf("docs/07's form-post table has no row for %s", route)
		}
	}
}
