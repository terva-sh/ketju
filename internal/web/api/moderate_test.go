package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for thread lock, unlock, pin and unpin (TKT-01M29CKRGS).

func modPath(id threadid.ID, action string) string {
	return "/api/v1/admin/threads/" + id.String() + "/" + action
}

// audited is one audit_log row as these tests read it.
type audited struct {
	actor   sql.NullInt64
	action  string
	typ, id string
	details map[string]any
}

func (w *world) auditLog() []audited {
	w.t.Helper()
	var out []audited
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT actor_id, action, target_type, target_id, details_json
			FROM audit_log ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a audited
			var dj string
			if err := rows.Scan(&a.actor, &a.action, &a.typ, &a.id, &dj); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(dj), &a.details); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// header decodes a 200 moderation answer strictly.
func header(t *testing.T, rec *httptest.ResponseRecorder) dto.ThreadHeader {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	dec := json.NewDecoder(strings.NewReader(rec.Body.String()))
	dec.DisallowUnknownFields()
	var h dto.ThreadHeader
	if err := dec.Decode(&h); err != nil {
		t.Fatalf("decoding strictly into dto.ThreadHeader: %v\n%s", err, rec.Body)
	}
	return h
}

// TestModerationAppendsOneRecordAndOneAuditRow is AC4: lock, unlock, pin and
// unpin each append one record, answer the header as the thread page now
// serves it, and leave one audit_log row naming the actor, the thread, the
// action and the reason, or the scope and until for a pin.
func TestModerationAppendsOneRecordAndOneAuditRow(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	bob := w.viewer("bob")
	until := w.clock + 3_600_000
	for _, step := range []struct {
		action, body string
		want         string
		details      map[string]any
		check        func(dto.ThreadHeader) bool
	}{
		{"lock", `{"reason":"cooling off"}`, projection.ActionThreadLocked, map[string]any{"reason": "cooling off"},
			func(h dto.ThreadHeader) bool { return h.Locked }},
		{"unlock", ``, projection.ActionThreadUnlocked, map[string]any{"reason": ""},
			func(h dto.ThreadHeader) bool { return !h.Locked }},
		{"unpin", `{"scope":"forum"}`, projection.ActionThreadUnpinned, map[string]any{"scope": "forum", "until": nil},
			func(h dto.ThreadHeader) bool { return h.Pinned == nil }},
		{"pin", fmt.Sprintf(`{"scope":"global","until":%d}`, until), projection.ActionThreadPinned,
			map[string]any{"scope": "global", "until": float64(until)},
			func(h dto.ThreadHeader) bool {
				return h.Pinned != nil && h.Pinned.Scope == "global" && h.Pinned.Until != nil && int64(*h.Pinned.Until) == until
			}},
	} {
		before, rows := w.logState(id), len(w.auditLog())
		h := header(t, w.sendAs(bob, http.MethodPost, modPath(id, step.action), step.body))
		after := w.logState(id)
		if !step.check(h) {
			t.Errorf("%s answered %+v", step.action, h)
		}
		if after.events != before.events+1 || after.posts != before.posts || after.size <= before.size {
			t.Errorf("%s: the log went from %+v to %+v, want one record", step.action, before, after)
		}
		if h.LogLength != after.size || h.PostCount != after.posts {
			t.Errorf("%s: the header says length %d and %d posts; the log is %d with %d",
				step.action, h.LogLength, h.PostCount, after.size, after.posts)
		}
		log := w.auditLog()
		if len(log) != rows+1 {
			t.Fatalf("%s wrote %d audit rows, want 1", step.action, len(log)-rows)
		}
		got := log[len(log)-1]
		step.details["thread"] = id.String()
		step.details["log_offset"] = float64(before.size)
		if got.actor != (sql.NullInt64{Int64: w.users["bob"], Valid: true}) || got.action != step.want ||
			got.typ != "thread" || got.id != id.String() || fmt.Sprint(got.details) != fmt.Sprint(step.details) {
			t.Errorf("%s: audit row %+v, want %s by bob with %v", step.action, got, step.want, step.details)
		}
		// The page serves the same header.
		if p := page(t, w.get(bob, "/api/v1/threads/"+id.String())); !reflect.DeepEqual(p.Thread, h) {
			t.Errorf("%s: the page's header %+v differs from the answer's %+v", step.action, p.Thread, h)
		}
	}
}

// TestModerationRefusals is AC5: without moderate in the thread's forum,
// on an archived thread, for an action that would change nothing, and for a
// bad request, each action is refused, appends nothing and writes no audit
// row.
func TestModerationRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	long := strings.Repeat("é", 501)
	cases := []struct {
		name, who, action, body, code string
	}{
		{"anonymous", "", "lock", ``, "unauthenticated"},
		{"a member", "carol", "lock", ``, "forbidden"},
		{"unlocking an open thread", "bob", "unlock", ``, "conflict"},
		{"unpinning a scope it is not pinned in", "bob", "unpin", `{"scope":"global"}`, "conflict"},
		{"the pin it has", "bob", "pin", `{"scope":"forum","until":1760000000000}`, "conflict"},
		{"an unknown scope", "bob", "pin", `{"scope":"sticky"}`, "validation-failed"},
		{"an end in the past", "bob", "pin", fmt.Sprintf(`{"scope":"global","until":%d}`, w.clock), "validation-failed"},
		{"a reason over 500 characters", "bob", "lock", `{"reason":"` + long + `"}`, "validation-failed"},
		{"a pin with no scope", "bob", "pin", `{}`, "bad-request"},
		{"an unpin with no body", "bob", "unpin", ``, "bad-request"},
		{"an unknown field", "bob", "lock", `{"reason":"x","why":"y"}`, "bad-request"},
	}
	check := func(t *testing.T, name string, do func() string, code string) {
		t.Helper()
		before, rows := w.logState(id), len(w.auditLog())
		if got := do(); got != code {
			t.Errorf("%s: %s, want %s", name, got, code)
		}
		if after := w.logState(id); after != before {
			t.Errorf("%s changed the log: %+v then %+v", name, before, after)
		}
		if got := len(w.auditLog()); got != rows {
			t.Errorf("%s wrote %d audit rows", name, got-rows)
		}
	}
	for _, c := range cases {
		check(t, c.name, func() string {
			return problemCode(t, send(w.chain(), c.who, http.MethodPost, modPath(id, c.action), c.body))
		}, c.code)
	}

	// Past the staff gate, the service still asks for moderate in the
	// thread's forum.
	check(t, "a member through the service", func() string {
		_, err := w.svc.Lock(t.Context(), w.viewer("carol"), id, "")
		return string(problem.FromError(err))
	}, "forbidden")

	header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "lock"), ``))
	check(t, "locking a locked thread", func() string {
		return problemCode(t, send(w.chain(), "bob", http.MethodPost, modPath(id, "lock"), ``))
	}, "conflict")

	// An expired pin is no pin: there is nothing to unpin.
	w.clock = 1_760_000_000_001
	check(t, "unpinning an expired pin", func() string {
		return problemCode(t, send(w.chain(), "bob", http.MethodPost, modPath(id, "unpin"), `{"scope":"forum"}`))
	}, "conflict")

	w.cache.MarkArchived(id)
	for _, c := range []struct{ action, body string }{{"unlock", ``}, {"pin", `{"scope":"global"}`}} {
		check(t, c.action+" on an archived thread", func() string {
			return problemCode(t, send(w.chain(), "bob", http.MethodPost, modPath(id, c.action), c.body))
		}, "conflict")
	}
}

// TestLockedThreadRefusesMembersNotModerators is AC2: in a locked thread a
// member's reply and edit are conflict and append nothing, a moderator's
// reply succeeds and is served with author.role moderator, and a member may
// still delete their own post.
func TestLockedThreadRefusesMembersNotModerators(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "lock"), `{"reason":"cooling off"}`))
	for _, c := range []struct{ name, method, path, body string }{
		{"reply", http.MethodPost, postsPath(id), `{"source":"still here"}`},
		{"edit", http.MethodPatch, postPathN(id, 4), `{"source":"changed"}`},
	} {
		before := w.logState(id)
		if rec := send(w.chain(), "carol", c.method, c.path, c.body); problemCode(t, rec) != "conflict" {
			t.Errorf("a member's %s in a locked thread: %d %s", c.name, rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("a refused %s changed the log: %+v then %+v", c.name, before, after)
		}
	}
	res := created[dto.CreatedPost](t, send(w.chain(), "bob", http.MethodPost, postsPath(id), `{"source":"moderator here"}`))
	if res.Post.Author.Role != "moderator" {
		t.Errorf("the moderator's reply is served with role %q", res.Post.Author.Role)
	}
	changed(t, send(w.chain(), "carol", http.MethodDelete, postPathN(id, 4), ``))
	if p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())); p.Viewer.CanReply || !p.Thread.Locked {
		t.Errorf("a member reads the locked thread as %+v, %+v", p.Thread, p.Viewer)
	}
}

// TestPinExpiryNeedsNoJob is AC3: a pin whose until has passed is gone from
// the thread list's pins and from the thread header, under a fixed clock,
// with nothing run in between but the clock.
func TestPinExpiryNeedsNoJob(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(2, "Pinned for an hour")
	w.post(id, "alice", "Read this first.", 0)
	until := w.clock + 3_600_000
	header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "pin"), fmt.Sprintf(`{"scope":"forum","until":%d}`, until)))

	pinned := func() (inList, inHeader bool) {
		t.Helper()
		l := ok[dto.ThreadList](t, w.get(w.viewer("carol"), "/api/v1/forums/general/threads"))
		for _, r := range l.Threads {
			if r.ID == id.String() {
				inList = r.Pinned != nil
			}
		}
		return inList, page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())).Thread.Pinned != nil
	}
	if l, h := pinned(); !l || !h {
		t.Fatalf("before its end the pin shows in the list %v and the header %v", l, h)
	}
	w.clock = until - 1
	if l, h := pinned(); !l || !h {
		t.Fatalf("a millisecond before its end the pin shows in the list %v and the header %v", l, h)
	}
	w.clock = until
	if l, h := pinned(); l || h {
		t.Fatalf("at its end the pin still shows in the list %v and the header %v", l, h)
	}
}

// TestLockAnswerGolden: the lock route's answer, decoded strictly, against a
// committed golden. Regenerate with `go test ./internal/web/api -update`.
func TestLockAnswerGolden(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, "lock"), `{"reason":"cooling off"}`))
	got, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "moderate.lock.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("the lock answer differs from %s:\n got %s\nwant %s", path, got, want)
	}
}
