package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// applyWorld is the server as serve builds it, with the real session
// middleware, the real per-address limiter and CSRF check, and one clock for
// the sessions, the limiters and the applications service.
type applyWorld struct {
	t   *testing.T
	db  *db.DB
	now time.Time
	h   http.Handler
	// member is a member's session cookie.
	member string
	// addr counts the addresses requests come from, so each can have its
	// own.
	addr int
}

func newApplyWorld(t *testing.T) *applyWorld {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &applyWorld{t: t, db: d, now: time.UnixMilli(1_757_600_000_000)}
	clock := func() time.Time { return w.now }
	p := perm.New(d)
	deps := Deps{Users: &users.Service{DB: d, Perm: p}, Threads: &threads.Service{DB: d, Perm: p},
		Forums: &forums.Service{DB: d, Perm: p}, Forms: &formsvc.Service{DB: d},
		Applications: &applications.Service{DB: d, Now: clock}, Perm: p,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: clock}
	w.h = web.New(web.Groups{API: New(deps)}, web.Options{Session: session.Middleware(&session.Store{DB: d, Now: clock}, nil),
		RateLimit: web.RateLimit(clock), SiteURL: "https://forum.example", Logger: deps.Logger})
	var member int64
	w.write(func(q *dbq.Queries) error {
		member, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "Alice",
			Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.member = sessiontest.SignIn(t, d, member, w.now)
	return w
}

func (w *applyWorld) write(fn func(q *dbq.Queries) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		w.t.Fatal(err)
	}
}

// applicant writes an identity with no user and a pre-user session for it,
// returning the identity and the cookie.
func (w *applyWorld) applicant(subject string) (int64, string) {
	w.t.Helper()
	var id int64
	w.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{ProviderKey: "github", Subject: subject,
			Email: sql.NullString{String: subject + "@example.org", Valid: true}, Issuer: "https://github.com",
			CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return id, sessiontest.PreUser(w.t, w.db, id, w.now)
}

// do sends a request with the cookie token, from a fresh address each time.
func (w *applyWorld) do(method, path, body, token string) *httptest.ResponseRecorder {
	w.t.Helper()
	var r *http.Request
	if body == "" && method == http.MethodGet {
		r = httptest.NewRequest(method, "https://forum.example"+path, nil)
	} else {
		r = httptest.NewRequest(method, "https://forum.example"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	w.addr++
	r.RemoteAddr = fmt.Sprintf("198.51.100.%d:4000", w.addr%250+1)
	if token != "" {
		r.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

const validAnswers = `{"version":1,"answers":{"name":"Carol","who":"A friend","rules":true}}`

func (w *applyWorld) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func applyProblem(rec *httptest.ResponseRecorder) problem.Code {
	var p problem.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return p.Type
}

// AC6: anonymous is 401 and an active member 403 on each route, and a
// pre-user reaches each one. Every response is no-store.
func TestApplyRoutesNeedAnApplicant(t *testing.T) {
	w := newApplyWorld(t)
	_, pre := w.applicant("carol")
	routes := []struct {
		method, path, body string
		// pre is what a pre-user gets, in this order.
		pre int
	}{
		{http.MethodGet, "/api/v1/apply/form", "", http.StatusOK},
		{http.MethodGet, "/api/v1/apply/status", "", http.StatusOK},
		{http.MethodPost, "/api/v1/apply", validAnswers, http.StatusCreated},
		{http.MethodPost, "/api/v1/apply/withdraw", "{}", http.StatusOK},
	}
	for _, rt := range routes {
		for _, c := range []struct {
			who   string
			token string
			code  int
		}{{"anonymous", "", http.StatusUnauthorized}, {"a member", w.member, http.StatusForbidden}, {"a pre-user", pre, rt.pre}} {
			rec := w.do(rt.method, rt.path, rt.body, c.token)
			if rec.Code != c.code {
				t.Errorf("%s %s as %s: %d %s, want %d", rt.method, rt.path, c.who, rec.Code, rec.Body, c.code)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("%s %s as %s: Cache-Control %q, want no-store", rt.method, rt.path, c.who, cc)
			}
		}
	}
	if n := w.count(`SELECT count(*) FROM applications WHERE status = 'withdrawn'`); n != 1 {
		t.Errorf("%d withdrawn applications, want the pre-user's one", n)
	}
}

// Who may ask is judged before what was asked (review 1425): an anonymous
// or member request to either write is 401 or 403 whatever its body,
// malformed, incomplete or valid, and never 400.
func TestApplyRefusesTheViewerBeforeTheBody(t *testing.T) {
	w := newApplyWorld(t)
	for _, path := range []string{"/api/v1/apply", "/api/v1/apply/withdraw"} {
		for _, body := range []string{`{`, `{"answers":{}}`, `{"version":1}`, `{"version":0,"answers":{}}`, `[1]`, validAnswers} {
			if rec := w.do(http.MethodPost, path, body, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("POST %s %s anonymously: %d, want 401", path, body, rec.Code)
			}
			if rec := w.do(http.MethodPost, path, body, w.member); rec.Code != http.StatusForbidden {
				t.Errorf("POST %s %s as a member: %d, want 403", path, body, rec.Code)
			}
		}
	}
}

// A withdrawal with an empty chunked body, which has no Content-Length, is
// accepted as an empty body is (review 1425); a body other than {} is not.
func TestWithdrawTakesAnEmptyChunkedBody(t *testing.T) {
	w := newApplyWorld(t)
	_, pre := w.applicant("carol")
	if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, pre); rec.Code != http.StatusCreated {
		t.Fatalf("POST /apply: %d %s", rec.Code, rec.Body)
	}
	send := func(body io.Reader) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://forum.example/api/v1/apply/withdraw", body)
		r.ContentLength = -1 // chunked: the length is unknown
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(sessiontest.Cookie(pre))
		rec := httptest.NewRecorder()
		w.h.ServeHTTP(rec, r)
		return rec
	}
	if rec := send(strings.NewReader(`{"x":1}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("a withdrawal with a body other than {}: %d, want 400", rec.Code)
	}
	rec := send(strings.NewReader(""))
	var st dto.ApplicationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != http.StatusOK || st.Status != "withdrawn" {
		t.Fatalf("a withdrawal with an empty chunked body: %d %s", rec.Code, rec.Body)
	}
}

// The bodies: the form with its prefill, 201 with the status, a refusal
// that is 409 with a no-store problem, and an empty withdraw body accepted.
func TestApplyBodies(t *testing.T) {
	w := newApplyWorld(t)
	_, pre := w.applicant("carol")
	rec := w.do(http.MethodGet, "/api/v1/apply/form", "", pre)
	var form dto.ApplicationForm
	if err := json.Unmarshal(rec.Body.Bytes(), &form); err != nil || form.Form.Version != 1 ||
		form.Prefill["email"] != "carol@example.org" {
		t.Fatalf("GET /apply/form: %d %s", rec.Code, rec.Body)
	}
	rec = w.do(http.MethodPost, "/api/v1/apply", validAnswers, pre)
	var st dto.ApplicationStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != http.StatusCreated ||
		st.Status != "pending" || !st.CanWithdraw {
		t.Fatalf("POST /apply: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/v1/apply/form"} {
		if rec := w.do(http.MethodGet, path, "", pre); rec.Code != http.StatusConflict || applyProblem(rec) != problem.CodeConflict {
			t.Errorf("GET %s while pending: %d %s, want 409", path, rec.Code, rec.Body)
		}
	}
	if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, pre); rec.Code != http.StatusConflict {
		t.Errorf("POST /apply while pending: %d %s, want 409", rec.Code, rec.Body)
	}
	// An empty body withdraws as {} does.
	r := httptest.NewRequest(http.MethodPost, "https://forum.example/api/v1/apply/withdraw", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(sessiontest.Cookie(pre))
	out := httptest.NewRecorder()
	w.h.ServeHTTP(out, r)
	if err := json.Unmarshal(out.Body.Bytes(), &st); err != nil || out.Code != http.StatusOK || st.Status != "withdrawn" {
		t.Fatalf("POST /apply/withdraw with no body: %d %s", out.Code, out.Body)
	}
	// Retry-After on the 24-hour rule.
	w.now = w.now.Add(time.Hour)
	rec = w.do(http.MethodPost, "/api/v1/apply", validAnswers, pre)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != strconv.Itoa(23*3600) {
		t.Errorf("POST /apply at +1 h: %d, Retry-After %q, want 429 and %d", rec.Code, rec.Header().Get("Retry-After"), 23*3600)
	}
	for _, body := range []string{`{"answers":{}}`, `{"version":1}`, `{"version":0,"answers":{}}`, `{"version":1,"answers":{},"x":1}`} {
		if rec := w.do(http.MethodPost, "/api/v1/apply", body, pre); rec.Code != http.StatusBadRequest {
			t.Errorf("POST /apply %s: %d, want 400", body, rec.Code)
		}
	}
}

// A missing required field is a 422 whose errors name the field's key.
func TestMissingRequiredFieldIsFieldScoped(t *testing.T) {
	w := newApplyWorld(t)
	id, pre := w.applicant("carol")
	rec := w.do(http.MethodPost, "/api/v1/apply", `{"version":1,"answers":{"name":"Carol","rules":true}}`, pre)
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusUnprocessableEntity ||
		p.Type != problem.CodeValidationFailed || len(p.Errors) != 1 || p.Errors[0].Field != "who" || p.Errors[0].Code != "required" {
		t.Fatalf("POST /apply without who: %d %s", rec.Code, rec.Body)
	}
	if n := w.count(`SELECT count(*) FROM applications WHERE identity_id = ?`, id); n != 0 {
		t.Errorf("a refused submission wrote %d applications", n)
	}
}

// AC5: the application writes are limited per identity after session
// resolution. Six attempts in a minute from one identity, each from a fresh
// address so the per-address stage never sees two, and the sixth is 429 with
// Retry-After. Withdrawing draws on the same bucket. A second identity is
// unaffected, and a member, who has no per-session limit, is never keyed.
func TestApplyIsLimitedPerIdentity(t *testing.T) {
	w := newApplyWorld(t)
	_, carol := w.applicant("carol")
	_, dave := w.applicant("dave")
	bad := `{"version":1,"answers":{}}` // 422, which counts as an attempt
	for i := range 5 {
		if rec := w.do(http.MethodPost, "/api/v1/apply", bad, carol); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	for _, path := range []string{"/api/v1/apply", "/api/v1/apply/withdraw"} {
		rec := w.do(http.MethodPost, path, bad, carol)
		if rec.Code != http.StatusTooManyRequests || applyProblem(rec) != problem.CodeRateLimited ||
			rec.Header().Get("Retry-After") == "" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("POST %s over the limit: %d, Retry-After %q", path, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
	// Reads are not limited.
	if rec := w.do(http.MethodGet, "/api/v1/apply/status", "", carol); rec.Code != http.StatusOK {
		t.Errorf("GET /apply/status over the write limit: %d", rec.Code)
	}
	if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, dave); rec.Code != http.StatusCreated {
		t.Errorf("a second identity: %d %s", rec.Code, rec.Body)
	}
	// A minute later one token is back.
	w.now = w.now.Add(time.Minute)
	if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, carol); rec.Code != http.StatusCreated {
		t.Errorf("a minute later: %d %s", rec.Code, rec.Body)
	}
	for i := range 8 {
		if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, w.member); rec.Code != http.StatusForbidden {
			t.Errorf("a member's attempt %d: %d, want the service's 403, never the limiter's 429", i+1, rec.Code)
		}
	}
}

// AC8 at the transport: a pre-user session held past its identity's
// approval by 25 hours no longer resolves (TKT-01M29CKRG9's LookupSession),
// so every application route answers it as anonymous, 401, and nothing is
// written. The same cookie reached the routes before the approval.
func TestAHeldPreUserSessionEndsAtApproval(t *testing.T) {
	w := newApplyWorld(t)
	id, pre := w.applicant("carol")
	if rec := w.do(http.MethodPost, "/api/v1/apply", validAnswers, pre); rec.Code != http.StatusCreated {
		t.Fatalf("POST /apply before the approval: %d %s", rec.Code, rec.Body)
	}
	// The approval TKT-01M29CP2MG writes: a user, the identity linked, and
	// the application approved. It leaves the pre-user session row alone,
	// which is the case this is about.
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		uid, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "carol", DisplayName: "Carol",
			Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(t.Context(), `UPDATE identities SET user_id = ? WHERE id = ?`, uid, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `UPDATE applications SET status = 'approved', user_id = ?, decided_at = ?
			WHERE identity_id = ?`, uid, w.now.UnixMilli(), id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(25 * time.Hour)
	for _, rt := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/apply/form", ""},
		{http.MethodPost, "/api/v1/apply", validAnswers},
		{http.MethodGet, "/api/v1/apply/status", ""},
		{http.MethodPost, "/api/v1/apply/withdraw", "{}"},
	} {
		if rec := w.do(rt.method, rt.path, rt.body, pre); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with the held session: %d %s, want 401", rt.method, rt.path, rec.Code, rec.Body)
		}
	}
	if n := w.count(`SELECT count(*) FROM applications WHERE identity_id = ?`, id); n != 1 {
		t.Errorf("%d applications, want the approved one alone", n)
	}
}
