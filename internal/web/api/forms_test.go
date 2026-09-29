package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

const draftSchema = `{"title":"Apply","intro_markdown":"*Hello*","fields":[
	{"key":"who","type":"textarea","label":"Who?","required":true,"max":500},
	{"key":"likes","type":"select","label":"Likes","options":["Games","Books"],"multiple":true}]}`

// logged rebuilds w's handler with a logger writing to the returned buffer.
func (w *adminWorld) logged() *bytes.Buffer {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	d := w.db
	w.h, w.mux = build(Deps{Users: &users.Service{DB: d, Perm: w.perm}, Threads: &threads.Service{DB: d, Perm: w.perm},
		Forums: &forums.Service{DB: d, Perm: w.perm}, Forms: &formsvc.Service{DB: d}, Perm: w.perm, Logger: log})
	store := &session.Store{DB: d, Now: func() time.Time { return w.now }}
	pass := func(h http.Handler) http.Handler { return h }
	w.handler = web.New(web.Groups{API: w.mux}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass, CSRF: pass})
	return &buf
}

// send sends method path with body as viewer.
func (w *adminWorld) send(method, path, viewer, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := w.tokens[viewer]; tok != "" {
		req.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		w.t.Errorf("%s %s as %s: Cache-Control %q, want no-store", method, path, viewer, cc)
	}
	return rec
}

func (w *adminWorld) rows(query string) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func saveBody(base int, schema string) string {
	b, _ := json.Marshal(map[string]any{"base_version": base, "schema": json.RawMessage(schema)})
	return string(b)
}

func previewBody(schema string) string {
	b, _ := json.Marshal(map[string]any{"schema": json.RawMessage(schema)})
	return string(b)
}

// TestFormAdminRoutesAreAdminOnly is AC5's gate: anonymous is 401, a
// pre-user, member and moderator 403, and an admin is served. Every answer
// is no-store (send checks).
func TestFormAdminRoutesAreAdminOnly(t *testing.T) {
	w := newAdminWorld(t)
	routes := []struct {
		method, path, body string
		ok                 int
	}{
		{"GET", "/api/v1/admin/forms/registration", "", http.StatusOK},
		{"GET", "/api/v1/admin/forms/registration/versions/1", "", http.StatusOK},
		{"POST", "/api/v1/admin/forms/registration/preview", previewBody(draftSchema), http.StatusOK},
		{"POST", "/api/v1/admin/forms/registration/versions", saveBody(1, draftSchema), http.StatusCreated},
	}
	for _, r := range routes {
		for viewer, want := range map[string]int{"anonymous": 401, "suspended": 401, "pre-user": 403, "member": 403,
			"scoped": 403, "moderator": 403} {
			if got := w.send(r.method, r.path, viewer, r.body).Code; got != want {
				t.Errorf("%s %s as %s: %d, want %d", r.method, r.path, viewer, got, want)
			}
		}
		if n := w.rows(`SELECT count(*) FROM form_versions`); n != 1 {
			t.Fatalf("a refused request wrote a version")
		}
	}
	for _, r := range routes {
		if rec := w.send(r.method, r.path, "admin", r.body); rec.Code != r.ok {
			t.Errorf("%s %s as admin: %d %s, want %d", r.method, r.path, rec.Code, rec.Body, r.ok)
		}
	}
}

// TestSaveAFormVersion is AC5's write: 201 with the form at version 2,
// version 1 still readable, and the audit row. A second save on the old
// base is 409 and writes nothing.
func TestSaveAFormVersion(t *testing.T) {
	w := newAdminWorld(t)
	rec := w.send("POST", "/api/v1/admin/forms/registration/versions", "admin", saveBody(1, draftSchema))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var fa dto.FormAdmin
	if err := json.Unmarshal(rec.Body.Bytes(), &fa); err != nil {
		t.Fatal(err)
	}
	if fa.ActiveVersion != 2 || fa.Form.Version != 2 || len(fa.Versions) != 2 || fa.Versions[0].CreatedBy.Handle != "admin" ||
		fa.Form.IntroHTML != "<p><em>Hello</em></p>\n" || fa.Form.Fields[0].Max != 500 {
		t.Errorf("answered %s", rec.Body)
	}
	if w.rows(`SELECT count(*) FROM audit_log WHERE action = 'form.version_created' AND actor_id = 4
		AND target_type = 'form' AND target_id = 'registration'`) != 1 {
		t.Error("no audit row")
	}

	rec = w.send("POST", "/api/v1/admin/forms/registration/versions", "admin", saveBody(1, draftSchema))
	if rec.Code != http.StatusConflict {
		t.Errorf("a stale save: %d %s, want 409", rec.Code, rec.Body)
	}
	if n := w.rows(`SELECT count(*) FROM form_versions`); n != 2 {
		t.Errorf("%d versions after the stale save, want 2", n)
	}

	var old dto.FormAdmin
	rec = w.send("GET", "/api/v1/admin/forms/registration/versions/1", "admin", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &old); err != nil || old.Form.Version != 1 || old.ActiveVersion != 2 {
		t.Errorf("version 1: %s", rec.Body)
	}
}

// TestInvalidSchemaIsNotSaved is AC6's refusal: 422 with path-scoped
// errors, the catalogue's title, and no version or audit row written.
func TestInvalidSchemaIsNotSaved(t *testing.T) {
	w := newAdminWorld(t)
	bad := `{"title":"T","fields":[{"key":"who","type":"text","label":"L","options":["x"]},{"key":"who","type":"date","label":"L"}]}`
	for _, path := range []string{"/api/v1/admin/forms/registration/versions", "/api/v1/admin/forms/registration/preview"} {
		body := saveBody(1, bad)
		if strings.HasSuffix(path, "preview") {
			body = previewBody(bad)
		}
		rec := w.send("POST", path, "admin", body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var p problem.Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		title, _ := problem.Title(problem.CodeValidationFailed)
		want := []problem.FieldError{{Field: "fields.0.options", Code: "not_allowed"}, {Field: "fields.1.type", Code: "unknown_type"},
			{Field: "fields.1.key", Code: "duplicate_key"}}
		if p.Type != problem.CodeValidationFailed || p.Title != title || !reflect.DeepEqual(p.Errors, want) {
			t.Errorf("%s: %s", path, rec.Body)
		}
	}
	if n := w.rows(`SELECT count(*) FROM form_versions`) + w.rows(`SELECT count(*) FROM audit_log`); n != 1 {
		t.Errorf("%d rows, want only the seeded version", n)
	}
}

// TestPreviewWritesNothing is AC6's preview: the draft is rendered as
// version 0, and nothing is written.
func TestPreviewWritesNothing(t *testing.T) {
	w := newAdminWorld(t)
	rec := w.send("POST", "/api/v1/admin/forms/registration/preview", "admin", previewBody(draftSchema))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var f dto.Form
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Key != "registration" || f.Version != 0 || len(f.Fields) != 2 || !f.Fields[1].Multiple {
		t.Errorf("previewed %s", rec.Body)
	}
	if n := w.rows(`SELECT count(*) FROM form_versions`) + w.rows(`SELECT count(*) FROM audit_log`); n != 1 {
		t.Errorf("%d rows, want only the seeded version", n)
	}
	if n := w.rows(`SELECT active_version FROM forms WHERE key = 'registration'`); n != 1 {
		t.Errorf("active version %d", n)
	}
}

// TestFormRequestsThatAreNotUnderstood: a body without its members, an
// unknown member, a version that is not a number, and a key naming no form.
func TestFormRequestsThatAreNotUnderstood(t *testing.T) {
	w := newAdminWorld(t)
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/api/v1/admin/forms/registration/versions", `{"schema":{}}`, 400},
		{"POST", "/api/v1/admin/forms/registration/versions", `{"base_version":1}`, 400},
		{"POST", "/api/v1/admin/forms/registration/versions", `{"base_version":1,"schema":{},"x":1}`, 400},
		{"POST", "/api/v1/admin/forms/registration/preview", `{}`, 400},
		{"GET", "/api/v1/admin/forms/registration/versions/0", "", 400},
		{"GET", "/api/v1/admin/forms/registration/versions/two", "", 400},
		{"GET", "/api/v1/admin/forms/registration/versions/9", "", 404},
		{"GET", "/api/v1/admin/forms/reports", "", 404},
		{"POST", "/api/v1/admin/forms/reports/versions", saveBody(1, draftSchema), 404},
		{"POST", "/api/v1/admin/forms/reports/preview", previewBody(draftSchema), 404},
	} {
		if rec := w.send(c.method, c.path, "admin", c.body); rec.Code != c.want {
			t.Errorf("%s %s %s: %d %s, want %d", c.method, c.path, c.body, rec.Code, rec.Body, c.want)
		}
	}
}

// TestCorruptSchemaIs500AndLogged is AC4: a stored version with an unknown
// type answers 500, never a form without the field, and is logged at error
// level naming the form and version.
func TestCorruptSchemaIs500AndLogged(t *testing.T) {
	w := newAdminWorld(t)
	logs := w.logged()
	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO form_versions (form_id, version, schema_json, created_by, created_at)
			VALUES (1, 2, '{"title":"T","fields":[{"key":"born","type":"date","label":"Born"}]}', 4, 5)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), `UPDATE forms SET active_version = 2 WHERE id = 1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rec := w.send("GET", "/api/v1/admin/forms/registration", "admin", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("%d %s, want 500", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "born") || strings.Contains(rec.Body.String(), "date") {
		t.Errorf("the body carries the stored schema: %s", rec.Body)
	}
	for _, want := range []string{"level=ERROR", `form \"registration\" version 2`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the log does not carry %s:\n%s", want, logs)
		}
	}
}
