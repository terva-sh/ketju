package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for per-thread slow mode (TKT-01M29CRJ50).

func slowModePath(id threadid.ID) string { return modPath(id, "slow-mode") }

// setSlowMode sets thread id's slow mode as bob, through h, and wants it
// answered.
func (w *world) setSlowMode(h http.Handler, id threadid.ID, seconds int) {
	w.t.Helper()
	got := ok[dto.SlowMode](w.t, send(h, "bob", http.MethodPut, slowModePath(id), fmt.Sprintf(`{"seconds":%d}`, seconds)))
	if got.Seconds != seconds {
		w.t.Fatalf("setting slow mode %d answered %+v", seconds, got)
	}
}

// slowModeRow is thread id's thread_slow_mode row as "seconds by at", or
// "none".
func (w *world) slowModeRow(id threadid.ID) string {
	w.t.Helper()
	var out string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.GetThreadSlowMode(w.t.Context(), int64(id))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			out = "none"
			return nil
		case err != nil:
			return err
		}
		out = fmt.Sprintf("%d by %d at %d", row.Seconds, row.SetBy.Int64, row.SetAt)
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// replyAs posts as who through h.
func replyAs(h http.Handler, who string, id threadid.ID, source string) *httptest.ResponseRecorder {
	return send(h, who, http.MethodPost, postsPath(id), `{"source":"`+source+`"}`)
}

// wantWait wants who's reply refused rate-limited with Retry-After retry,
// and the log left as it was.
func (w *world) wantWait(h http.Handler, who string, id threadid.ID, retry string) {
	w.t.Helper()
	before := w.logState(id)
	rec := replyAs(h, who, id, "too soon")
	if problemCode(w.t, rec) != "rate-limited" || rec.Header().Get("Retry-After") != retry {
		w.t.Errorf("%s: %d Retry-After %q %s, want rate-limited and %s", who, rec.Code,
			rec.Header().Get("Retry-After"), rec.Body, retry)
	}
	if after := w.logState(id); after != before {
		w.t.Errorf("%s: a refused reply changed the log: %+v then %+v", who, before, after)
	}
}

// TestSlowModePerThread is the first two criteria: in a thread at 30 s, a
// member's reply 10 s after their last is rate-limited with Retry-After 20
// and appends nothing, and at 30 s it lands. An agent account waits the
// same way; a moderator does not wait at all. The viewer block says the
// interval to everyone, and when the member may post next to the member
// alone.
func TestSlowModePerThread(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	other, _ := w.thread(2, "Another thread")
	w.post(other, "alice", "first", 0)
	h := w.chain()
	w.setSlowMode(h, id, 30)

	for _, who := range []string{"carol", "scribe"} {
		created[dto.CreatedPost](t, replyAs(h, who, id, "one"))
		posted := w.clock
		w.clock += 10_000
		w.wantWait(h, who, id, "20")
		p := page(t, w.get(w.viewer(who), "/api/v1/threads/"+id.String()))
		if p.Viewer.SlowModeSeconds != 30 || p.Viewer.NextPostAt == nil || int64(*p.Viewer.NextPostAt) != posted+30_000 {
			t.Errorf("%s's viewer block while waiting is %+v, want 30 s and next_post_at %d", who, p.Viewer, posted+30_000)
		}
		w.clock += 19_999
		w.wantWait(h, who, id, "1")
		w.clock++
		created[dto.CreatedPost](t, replyAs(h, who, id, "two"))
		// Slow mode is this thread's: the other thread has none.
		created[dto.CreatedPost](t, replyAs(h, who, other, "elsewhere"))
		created[dto.CreatedPost](t, replyAs(h, who, other, "elsewhere again"))
	}

	for range 3 {
		created[dto.CreatedPost](t, replyAs(h, "bob", id, "moderating"))
	}
	// The moderator is exempt, anonymous cannot post, and alice has not
	// posted for minutes: none of them has anything to wait for.
	for _, who := range []string{"bob", "", "alice"} {
		p := page(t, w.get(w.viewer(who), "/api/v1/threads/"+id.String()))
		if p.Viewer.SlowModeSeconds != 30 || p.Viewer.NextPostAt != nil {
			t.Errorf("%q's viewer block is %+v, want 30 s and no next_post_at", who, p.Viewer)
		}
	}
}

// TestSlowModeOverridesDefault is the third criterion's override: the
// thread's own value wins over slow_mode.default_seconds, an explicit 0
// included, and a thread with no row takes the default.
func TestSlowModeOverridesDefault(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	if err := settings.Set(t.Context(), w.db, settings.SlowModeDefaultSeconds.Name, json.RawMessage(`60`), 0, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "one"))
	w.wantWait(h, "carol", id, "60")
	if p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())); p.Viewer.SlowModeSeconds != 60 {
		t.Errorf("with no row the viewer block says %d s, want the default 60", p.Viewer.SlowModeSeconds)
	}

	w.setSlowMode(h, id, 0)
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "two"))
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "three"))
	if p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())); p.Viewer.SlowModeSeconds != 0 || p.Viewer.NextPostAt != nil {
		t.Errorf("under an explicit 0 the viewer block is %+v", p.Viewer)
	}

	w.setSlowMode(h, id, 10)
	w.wantWait(h, "carol", id, "10")
}

// TestSlowModeOffIsImmediate is the fourth criterion: setting 0 lets the
// next reply through at once.
func TestSlowModeOffIsImmediate(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	w.setSlowMode(h, id, 30)
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "one"))
	w.wantWait(h, "carol", id, "30")
	w.setSlowMode(h, id, 0)
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "two"))
}

// TestSetSlowModeRefusals is the fifth criterion: without moderate the
// change is refused, as it is on an archived thread, for a value out of
// range or a bad body, and for the value the thread already has; none of
// those writes the row or an audit row. Each change writes one audit_log
// row against the thread, naming the moderator, with from and to. A thread
// in an archived forum still takes it.
func TestSetSlowModeRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	check := func(t *testing.T, name string, do func() string, code string) {
		t.Helper()
		row, rows := w.slowModeRow(id), len(w.auditLog())
		if got := do(); got != code {
			t.Errorf("%s: %s, want %s", name, got, code)
		}
		if got := w.slowModeRow(id); got != row {
			t.Errorf("%s changed the row: %s then %s", name, row, got)
		}
		if got := len(w.auditLog()); got != rows {
			t.Errorf("%s wrote %d audit rows", name, got-rows)
		}
	}
	for _, c := range []struct{ name, who, body, code string }{
		{"anonymous", "", `{"seconds":30}`, "unauthenticated"},
		{"a pre-user", "pre-user", `{"seconds":30}`, "forbidden"},
		{"a member", "carol", `{"seconds":30}`, "forbidden"},
		{"over a day", "bob", `{"seconds":86401}`, "validation-failed"},
		{"negative", "bob", `{"seconds":-1}`, "validation-failed"},
		{"no seconds", "bob", `{}`, "bad-request"},
		{"seconds as a string", "bob", `{"seconds":"30"}`, "bad-request"},
		{"a fraction", "bob", `{"seconds":1.5}`, "bad-request"},
		{"an unknown field", "bob", `{"seconds":30,"reason":"x"}`, "bad-request"},
	} {
		check(t, c.name, func() string {
			return problemCode(t, send(h, c.who, http.MethodPut, slowModePath(id), c.body))
		}, c.code)
	}
	check(t, "no such thread", func() string {
		return problemCode(t, send(h, "bob", http.MethodPut, "/api/v1/admin/threads/0000000000c1s/slow-mode", `{"seconds":30}`))
	}, "not-found")
	// Past the staff gate, the service still asks for moderate in the
	// thread's forum.
	check(t, "a member through the service", func() string {
		_, err := w.svc.SetSlowMode(t.Context(), w.viewer("carol"), "", id, 30)
		return string(problem.FromError(err))
	}, "forbidden")

	for i, step := range []struct {
		seconds int
		from    any
	}{{30, nil}, {0, float64(30)}, {86400, float64(0)}} {
		rows := len(w.auditLog())
		w.setSlowMode(h, id, step.seconds)
		if got, want := w.slowModeRow(id), fmt.Sprintf("%d by %d at %d", step.seconds, w.users["bob"], w.clock); got != want {
			t.Errorf("change %d: the row is %s, want %s", i, got, want)
		}
		log := w.auditLog()
		if len(log) != rows+1 {
			t.Fatalf("change %d wrote %d audit rows, want 1", i, len(log)-rows)
		}
		got := log[len(log)-1]
		want := map[string]any{"from": step.from, "to": float64(step.seconds)}
		if got.actor != (sql.NullInt64{Int64: w.users["bob"], Valid: true}) || got.action != threads.ActionSlowMode ||
			got.typ != "thread" || got.id != id.String() || fmt.Sprint(got.details) != fmt.Sprint(want) {
			t.Errorf("change %d: audit row %+v, want %s by bob against the thread with %v", i, got, threads.ActionSlowMode, want)
		}
		check(t, fmt.Sprintf("setting %d again", step.seconds), func() string {
			return problemCode(t, send(h, "bob", http.MethodPut, slowModePath(id), fmt.Sprintf(`{"seconds":%d}`, step.seconds)))
		}, "conflict")
	}
	var ip string
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT ip FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`,
			threads.ActionSlowMode).Scan(&ip)
	}); err != nil {
		t.Fatal(err)
	}
	if ip == "" {
		t.Error("the audit row has no address")
	}

	w.cache.MarkArchived(id)
	check(t, "an archived thread", func() string {
		return problemCode(t, send(h, "bob", http.MethodPut, slowModePath(id), `{"seconds":30}`))
	}, "conflict")

	t.Run("an archived forum", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.archive()
		w.setSlowMode(w.chain(), id, 30)
		if got := w.slowModeRow(id); !strings.HasPrefix(got, "30 by ") {
			t.Errorf("the row is %s", got)
		}
	})
}

// TestSlowModeIsInTheViewerBlockNotTheCache is the sixth criterion, the
// response cache invariant (docs/13 "Invariants"): slow mode is not in the
// posts, so setting it leaves every class's cached posts under the same key
// and the next page is a hit, while the same response's viewer block, and
// so its ETag, carries the new value.
func TestSlowModeIsInTheViewerBlockNotTheCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	created[dto.CreatedPost](t, replyAs(h, "carol", id, "just now"))
	posted := w.clock
	path := "/api/v1/threads/" + id.String()
	classes := []string{"", "carol", "bob"}

	type seen struct {
		etag, posts string
		viewer      dto.ThreadViewer
		length      int64
	}
	round := func() map[string]seen {
		out := map[string]seen{}
		for _, who := range classes {
			rec := w.get(w.viewer(who), path)
			p := page(t, rec)
			out[who] = seen{rec.Header().Get("ETag"), renderJSON(t, p.Posts), p.Viewer, p.Thread.LogLength}
		}
		return out
	}
	round()
	before := round()
	stats := w.svc.Responses.Stats()
	for who, s := range before {
		if s.viewer.SlowModeSeconds != 0 || s.viewer.NextPostAt != nil {
			t.Fatalf("%q's viewer block before slow mode is %+v", who, s.viewer)
		}
	}

	w.setSlowMode(h, id, 30)
	after := round()
	if s := w.svc.Responses.Stats(); s.Hits != stats.Hits+3 || s.Misses != stats.Misses || s.Inserts != stats.Inserts {
		t.Errorf("setting slow mode moved the posts' key: %+v then %+v, want three hits and nothing else", stats, s)
	}
	for _, who := range classes {
		b, a := before[who], after[who]
		if a.posts != b.posts || a.length != b.length {
			t.Errorf("%q's posts or log length changed with slow mode", who)
		}
		if a.etag == b.etag {
			t.Errorf("%q's ETag %s did not change with the viewer block", who, a.etag)
		}
		if a.viewer.SlowModeSeconds != 30 {
			t.Errorf("%q's viewer block says %d s after setting 30", who, a.viewer.SlowModeSeconds)
		}
		var want *dto.Millis
		if who == "carol" {
			at := dto.Millis(posted + 30_000)
			want = &at
		}
		if (a.viewer.NextPostAt == nil) != (want == nil) || want != nil && *a.viewer.NextPostAt != *want {
			t.Errorf("%q's next_post_at is %v, want %v", who, a.viewer.NextPostAt, want)
		}
	}
}
