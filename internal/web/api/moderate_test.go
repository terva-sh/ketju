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

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
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
	// The expiry appends nothing, so the log length and the response cache
	// key stay where they were, and the page's posts are a cache hit. The
	// header is not part of the cached entry: it is drawn on every request
	// from the projection row and the clock, so the hit still drops the pin
	// and equals a page drawn with no cache at all (review 1471, finding 1).
	w.clock = until
	before := w.svc.Responses.Stats()
	if l, h := pinned(); l || h {
		t.Fatalf("at its end the pin still shows in the list %v and the header %v", l, h)
	}
	if s := w.svc.Responses.Stats(); s.Hits != before.Hits+1 || s.Inserts != before.Inserts {
		t.Fatalf("the page after the expiry was not a cache hit: %+v then %+v", before, s)
	}
	if body := w.same(w.viewer("carol"), "/api/v1/threads/"+id.String()); !strings.Contains(body, `"pinned":null`) {
		t.Fatalf("a cached page after the expiry still carries the pin: %s", body)
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

// Tests for moderators by grant (TKT-01M3RQP0TG): thread moderation follows
// the forum grant (docs/08 "Decided 2026-09-30").

// threadAction is one of the seven thread moderation routes, with a body that
// changes the thread from the state the action before it leaves.
type threadAction struct {
	name, method, action, body string
	// audit is the audit_log action the change writes.
	audit string
}

// threadActions is the seven routes in an order each may follow the one
// before on one thread that starts open and unpinned in general: lock,
// unlock, pin and unpin with scope forum, slow mode 30, a retag to garden
// (TKT-01M29CYSCP), and a move to den.
var threadActions = []threadAction{
	{"lock", http.MethodPost, "lock", `{"reason":"cooling off"}`, projection.ActionThreadLocked},
	{"unlock", http.MethodPost, "unlock", ``, projection.ActionThreadUnlocked},
	{"pin", http.MethodPost, "pin", `{"scope":"forum"}`, projection.ActionThreadPinned},
	{"unpin", http.MethodPost, "unpin", `{"scope":"forum"}`, projection.ActionThreadUnpinned},
	{"slow mode", http.MethodPut, "slow-mode", `{"seconds":30}`, threads.ActionSlowMode},
	{"retag", http.MethodPut, "tags", `{"tags":["garden"]}`, projection.ActionThreadRetagged},
	{"move", http.MethodPost, "move", moveTo("den"), projection.ActionThreadMoved},
}

// forumModWorld is a world in which gina moderates general and den through
// the moderators group and holds no role, lounge is a public forum she reads
// and does not moderate, and vault is a restricted forum hidden from her.
func forumModWorld(t *testing.T) (w *world, gina domain.Viewer, lounge, den, vault int64) {
	t.Helper()
	w = newWorld(t)
	lounge = w.forumAt("lounge", domain.VisibilityPublic, false)
	den = w.forumAt("den", domain.VisibilityPublic, false)
	vault = w.forumAt("vault", domain.VisibilityRestricted, false)
	return w, w.groupModerator(w.forum, den), lounge, den, vault
}

// untouched runs do, wants the problem code want from it, and wants the log
// of id, audit_log and the thread's slow mode row left as they were.
func (w *world) untouched(t *testing.T, id threadid.ID, name, want string, do func() string) {
	t.Helper()
	slow := w.slowModeRow(id)
	w.unchanged(t, id, name, want, do)
	if got := w.slowModeRow(id); got != slow {
		t.Errorf("%s changed the slow mode row: %s then %s", name, slow, got)
	}
}

// TestForumModeratorActsInTheirForum is TKT-01M3RQP0TG's second criterion:
// gina, who moderates general and den through a group, locks, unlocks, pins
// and unpins with scope forum, sets slow mode, and moves the thread to den.
// Each answers 200 and writes one record, or one slow mode row, and one
// audit row naming her. The lock moves the page's ETag and its response
// cache key, as a staff lock does (TestLockMovesTheResponseCacheKey).
func TestForumModeratorActsInTheirForum(t *testing.T) {
	w, gina, _, den, _ := forumModWorld(t)
	id := w.threadIn(20, w.forum, "In general", "alice")
	h := w.chain()
	path := threadPath(id)
	tag := w.get(gina, path).Header().Get("ETag")
	w.same(gina, path)
	for _, a := range threadActions {
		before, rows, slow := w.logState(id), len(w.auditLog()), w.slowModeRow(id)
		stats := w.svc.Responses.Stats()
		rec := send(h, "gina", a.method, modPath(id, a.action), a.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("gina's %s: %d %s", a.name, rec.Code, rec.Body)
		}
		after := w.logState(id)
		if a.action == "slow-mode" {
			if after != before {
				t.Errorf("gina's slow mode changed the log: %+v then %+v", before, after)
			}
			if got, want := w.slowModeRow(id), fmt.Sprintf("30 by %d at %d", w.users["gina"], w.clock); got != want {
				t.Errorf("after gina's slow mode the row is %s, want %s", got, want)
			}
		} else {
			if after.events != before.events+1 || after.posts != before.posts || after.size <= before.size {
				t.Errorf("gina's %s: the log went from %+v to %+v, want one record", a.name, before, after)
			}
			if got := w.slowModeRow(id); got != slow {
				t.Errorf("gina's %s changed the slow mode row: %s then %s", a.name, slow, got)
			}
		}
		log := w.auditLog()
		if len(log) != rows+1 {
			t.Fatalf("gina's %s wrote %d audit rows, want 1", a.name, len(log)-rows)
		}
		if got := log[len(log)-1]; got.actor != (sql.NullInt64{Int64: w.users["gina"], Valid: true}) ||
			got.action != a.audit || got.typ != "thread" || got.id != id.String() {
			t.Errorf("gina's %s: audit row %+v, want %s by gina against the thread", a.name, got, a.audit)
		}
		if a.action == "lock" {
			rec := w.get(gina, path)
			if got := rec.Header().Get("ETag"); got == tag || !strings.HasPrefix(got, fmt.Sprintf(`"%s-%d-`, id, after.size)) {
				t.Errorf("after gina's lock her ETag is %s (before %s), not one under the new log length %d", got, tag, after.size)
			}
			if s := w.svc.Responses.Stats(); s.Misses != stats.Misses+1 || s.Hits != stats.Hits {
				t.Errorf("after gina's lock her page was not a fresh draw: %+v then %+v", stats, s)
			}
		}
	}
	if row := w.threadRow(id); row.ForumID != den {
		t.Errorf("after gina's move the thread is in forum %d, want den %d", row.ForumID, den)
	}
}

// TestForumModeratorIsRefusedElsewhere is TKT-01M3RQP0TG's third criterion:
// gina is forbidden all seven actions on a thread in lounge, a forum she reads
// and does not moderate, and gets not-found on a thread in vault, which is
// hidden from her. Once her grant in general is revoked she is forbidden in
// general too: by the service while she still moderates den, and by the
// gate once that grant goes as well. No refusal appends, writes a slow mode
// row, or writes an audit row.
func TestForumModeratorIsRefusedElsewhere(t *testing.T) {
	w, _, lounge, den, vault := forumModWorld(t)
	h := w.chain()
	bob := w.viewer("bob")
	// In lounge, one thread in the state each action needs, so each would
	// pass for a moderator of lounge.
	open := w.threadIn(30, lounge, "Open in the lounge", "alice")
	locked := w.threadIn(31, lounge, "Locked in the lounge", "alice")
	pinned := w.threadIn(32, lounge, "Pinned in the lounge", "alice")
	header(t, w.sendAs(bob, http.MethodPost, modPath(locked, "lock"), ``))
	header(t, w.sendAs(bob, http.MethodPost, modPath(pinned, "pin"), `{"scope":"forum"}`))
	in := func(a threadAction, open, locked, pinned threadid.ID) threadid.ID {
		switch a.action {
		case "unlock":
			return locked
		case "unpin":
			return pinned
		}
		return open
	}
	gina := func(id threadid.ID, a threadAction) func() string {
		return func() string { return problemCode(t, send(h, "gina", a.method, modPath(id, a.action), a.body)) }
	}
	for _, a := range threadActions {
		id := in(a, open, locked, pinned)
		w.untouched(t, id, "gina's "+a.name+" in lounge", "forbidden", gina(id, a))
	}

	hidden := w.threadIn(33, vault, "In the vault", "alice")
	for _, a := range threadActions {
		w.untouched(t, hidden, "gina's "+a.name+" in vault", "not-found", gina(hidden, a))
	}

	// Revoked in general: the gate still admits her, for den, and the
	// service refuses her in general.
	mine := w.threadIn(34, w.forum, "Open in general", "alice")
	mineLocked := w.threadIn(35, w.forum, "Locked in general", "alice")
	minePinned := w.threadIn(36, w.forum, "Pinned in general", "alice")
	header(t, w.sendAs(bob, http.MethodPost, modPath(mineLocked, "lock"), ``))
	header(t, w.sendAs(bob, http.MethodPost, modPath(minePinned, "pin"), `{"scope":"forum"}`))
	var mods int64
	if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupModerators)
		mods = g.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e := perm.New(w.db)
	if err := e.Revoke(t.Context(), audit.By{}, w.forum, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	for _, a := range threadActions {
		id := in(a, mine, mineLocked, minePinned)
		w.untouched(t, id, "gina's "+a.name+" in general after the revoke", "forbidden", gina(id, a))
	}
	// Revoked in den too: the gate refuses her before the service is asked.
	if err := e.Revoke(t.Context(), audit.By{}, den, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	for _, a := range threadActions {
		id := in(a, mine, mineLocked, minePinned)
		w.untouched(t, id, "gina's "+a.name+" with no grant left", "forbidden", gina(id, a))
	}
}

// TestThreadModerationGate is TKT-01M3RQP0TG's fifth criterion: on all seven
// routes anonymous is unauthenticated, a pre-user forbidden, and carol, a
// member with no grant, forbidden by the gate before the body is read, so a
// body that would be bad-request still answers forbidden. Past the gate,
// gina in her forum, bob as a moderator by role and an admin each take all
// seven on a thread of their own.
func TestThreadModerationGate(t *testing.T) {
	w, _, _, _, _ := forumModWorld(t)
	admin := w.newAdmin()
	h := w.chain()
	id := w.threadIn(40, w.forum, "Behind the gate", "alice")
	for _, a := range threadActions {
		for _, c := range []struct{ who, body, want string }{
			{"", a.body, "unauthenticated"},
			{"pre-user", a.body, "forbidden"},
			{"carol", a.body, "forbidden"},
			{"carol", `{"not":"a field this route takes"}`, "forbidden"},
		} {
			w.untouched(t, id, fmt.Sprintf("%s as %q with %s", a.name, c.who, c.body), c.want, func() string {
				return problemCode(t, send(h, c.who, a.method, modPath(id, a.action), c.body))
			})
		}
	}
	for i, c := range []struct {
		name string
		do   func(method, path, body string) *httptest.ResponseRecorder
	}{
		{"gina", func(m, p, b string) *httptest.ResponseRecorder { return send(h, "gina", m, p, b) }},
		{"bob", func(m, p, b string) *httptest.ResponseRecorder { return send(h, "bob", m, p, b) }},
		{"an admin", func(m, p, b string) *httptest.ResponseRecorder { return w.sendAs(admin, m, p, b) }},
	} {
		id := w.threadIn(uint64(41+i), w.forum, fmt.Sprintf("For %s", c.name), "alice")
		for _, a := range threadActions {
			if rec := c.do(a.method, modPath(id, a.action), a.body); rec.Code != http.StatusOK {
				t.Errorf("%s's %s: %d %s", c.name, a.name, rec.Code, rec.Body)
			}
		}
	}
}

// TestGlobalPinStaysStaffs is TKT-01M3RQP0TG's sixth criterion: a global pin
// heads every forum's list, so it is not moderation of the thread's forum.
// gina's pin with scope global, her unpin of bob's global pin, and her pin
// or unpin with scope forum while it is up are each forbidden and write
// nothing; the service refuses the same. Once bob unpins, her forum pin is
// allowed, and an admin pins globally.
func TestGlobalPinStaysStaffs(t *testing.T) {
	w, gina, _, _, _ := forumModWorld(t)
	admin := w.newAdmin()
	h := w.chain()
	id := w.threadIn(50, w.forum, "Pinned for everyone", "alice")
	refused := func(name, action, body string) {
		t.Helper()
		w.untouched(t, id, name, "forbidden", func() string {
			return problemCode(t, send(h, "gina", http.MethodPost, modPath(id, action), body))
		})
	}
	refused("gina's global pin", "pin", `{"scope":"global"}`)
	w.untouched(t, id, "gina's global pin through the service", "forbidden", func() string {
		_, err := w.svc.Pin(t.Context(), gina, id, threads.PinGlobal, nil)
		return string(problem.FromError(err))
	})
	if got := header(t, send(h, "bob", http.MethodPost, modPath(id, "pin"), `{"scope":"global"}`)); got.Pinned == nil || got.Pinned.Scope != "global" {
		t.Fatalf("bob's global pin answered %+v", got)
	}
	refused("gina's unpin of the global pin", "unpin", `{"scope":"global"}`)
	refused("gina's forum pin over the global pin", "pin", `{"scope":"forum"}`)
	refused("gina's forum unpin under the global pin", "unpin", `{"scope":"forum"}`)
	w.untouched(t, id, "gina's unpin of the global pin through the service", "forbidden", func() string {
		_, err := w.svc.Unpin(t.Context(), gina, id, threads.PinGlobal)
		return string(problem.FromError(err))
	})
	if row := w.threadRow(id); row.PinnedScope.String != "global" {
		t.Fatalf("the thread's pin is %q, want bob's global pin still up", row.PinnedScope.String)
	}
	header(t, send(h, "bob", http.MethodPost, modPath(id, "unpin"), `{"scope":"global"}`))
	if got := header(t, send(h, "gina", http.MethodPost, modPath(id, "pin"), `{"scope":"forum"}`)); got.Pinned == nil || got.Pinned.Scope != "forum" {
		t.Errorf("gina's forum pin answered %+v", got)
	}
	if got := header(t, w.sendAs(admin, http.MethodPost, modPath(id, "pin"), `{"scope":"global"}`)); got.Pinned == nil || got.Pinned.Scope != "global" {
		t.Errorf("the admin's global pin answered %+v", got)
	}
}
