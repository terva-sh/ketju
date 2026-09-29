package formsvc

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

var (
	admin     = domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true}
	moderator = domain.Viewer{UserID: 2, Role: domain.RoleModerator, Active: true}
)

const schemaV2 = `{"title":"Apply","fields":[{"key":"who","type":"textarea","label":"Who?","required":true},
	{"key":"note","type":"markdown_notice","markdown":"Be **kind**."}]}`

func newService(t *testing.T) *Service {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	exec(t, d, `INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (1, 'root', 'Site Admin', 'active', 'admin', 'human', '{}', 1, 1),
		       (2, 'mod', 'Mod', 'active', 'moderator', 'human', '{}', 1, 1)`)
	return &Service{DB: d, Now: func() time.Time { return time.UnixMilli(1_757_600_000_000) }}
}

func exec(t *testing.T, d *db.DB, query string, args ...any) {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), query, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, d *db.DB, query string) int {
	t.Helper()
	var n int
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), query).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func stored(t *testing.T, d *db.DB, version int) string {
	t.Helper()
	var s string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT schema_json FROM form_versions WHERE form_id = 1 AND version = ?`,
			version).Scan(&s)
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTheSeededFormLoads: the registration form is active at version 1 and
// renders.
func TestTheSeededFormLoads(t *testing.T) {
	s := newService(t)
	v, err := s.Active(t.Context(), Registration)
	if err != nil {
		t.Fatal(err)
	}
	if v.Number != 1 || v.Key != Registration || len(v.Schema.Fields) != 4 {
		t.Errorf("got version %d of %q with %d fields", v.Number, v.Key, len(v.Schema.Fields))
	}
	f, err := ToDTO(v)
	if err != nil {
		t.Fatal(err)
	}
	if f.IntroHTML != "<p>Tell us a bit about yourself…</p>\n" || f.Fields[1].Max != 254 || !f.Fields[1].Locked {
		t.Errorf("rendered %+v", f)
	}
}

// TestCorruptStoredSchemaFailsLoudly is AC4's service half: a version whose
// stored schema has an unknown type fails to load, as ErrCorruptSchema
// naming the form and version. It is not the request's fault, so it maps to
// internal, never to validation-failed, and no form without the field is
// returned.
func TestCorruptStoredSchemaFailsLoudly(t *testing.T) {
	s := newService(t)
	exec(t, s.DB, `INSERT INTO form_versions (form_id, version, schema_json, created_by, created_at)
		VALUES (1, 2, '{"title":"T","fields":[{"key":"born","type":"date","label":"Born"}]}', 1, 5)`)
	exec(t, s.DB, `UPDATE forms SET active_version = 2 WHERE id = 1`)

	v, err := s.Active(t.Context(), Registration)
	if !errors.Is(err, ErrCorruptSchema) {
		t.Fatalf("got %+v, %v; want ErrCorruptSchema", v, err)
	}
	if errors.Is(err, forms.ErrInvalidSchema) || problem.FieldErrorsOf(err) != nil {
		t.Errorf("%v wraps the parse refusal, which would map it to validation-failed", err)
	}
	if got := problem.FromError(err); got != problem.CodeInternal {
		t.Errorf("maps to %s, want internal", got)
	}
	for _, want := range []string{`"registration"`, "version 2", "unknown_type"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %s", err, want)
		}
	}
	if len(v.Schema.Fields) != 0 {
		t.Errorf("a form came back with the error: %+v", v)
	}
	if _, err := s.Admin(t.Context(), admin, Registration, 0); !errors.Is(err, ErrCorruptSchema) {
		t.Errorf("Admin: %v, want ErrCorruptSchema", err)
	}
	if _, err := s.Version(t.Context(), 1, 1); err != nil {
		t.Errorf("version 1 is fine and did not load: %v", err)
	}
}

// TestCreateVersionKeepsOldVersions is AC5's append half: version 1 is
// byte-identical after version 2 is saved, and active moves to 2.
func TestCreateVersionKeepsOldVersions(t *testing.T) {
	s := newService(t)
	before := stored(t, s.DB, 1)
	n, err := s.Create(t.Context(), admin, Registration, 1, []byte(schemaV2), "203.0.113.9")
	if err != nil || n != 2 {
		t.Fatalf("Create = %d, %v", n, err)
	}
	if after := stored(t, s.DB, 1); after != before {
		t.Errorf("version 1 changed:\n%s\n%s", before, after)
	}
	v, err := s.Active(t.Context(), Registration)
	if err != nil || v.Number != 2 || v.CreatedBy != 1 {
		t.Fatalf("active is %+v, %v", v, err)
	}
	// Stored canonical: the whitespace the request carried is gone.
	if strings.Contains(stored(t, s.DB, 2), "\n") {
		t.Errorf("version 2 was not stored canonical: %s", stored(t, s.DB, 2))
	}

	fa, err := s.Admin(t.Context(), admin, Registration, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fa.ActiveVersion != 2 || len(fa.Versions) != 2 || fa.Versions[0].Version != 2 ||
		fa.Versions[0].CreatedBy == nil || fa.Versions[0].CreatedBy.Handle != "root" || fa.Versions[1].CreatedBy != nil {
		t.Errorf("admin view %+v", fa)
	}
	// The notice went through internal/render. Remote images becoming
	// links is render's rule, which TKT-01M3N24RM builds there.
	if html := fa.Form.Fields[1].HTML; html != "<p>Be <strong>kind</strong>.</p>\n" {
		t.Errorf("the notice rendered as %q", html)
	}
	old, err := s.Admin(t.Context(), admin, Registration, 1)
	if err != nil || old.Form.Version != 1 || string(old.Schema) != before {
		t.Errorf("version 1 through Admin: %+v, %v", old, err)
	}

	var action, target, details, ip string
	var actor int64
	if err := s.DB.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT action, actor_id, target_type || ':' || target_id, details_json, ip
			FROM audit_log`).Scan(&action, &actor, &target, &details, &ip)
	}); err != nil {
		t.Fatal(err)
	}
	if action != "form.version_created" || actor != 1 || target != "form:registration" || ip != "203.0.113.9" {
		t.Errorf("audit row %s %d %s %s", action, actor, target, ip)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(details), &d); err != nil || d["key"] != "registration" || d["version"] != float64(2) {
		t.Errorf("audit details %s", details)
	}
}

// TestCreateVersionIsOneTransaction: when the audit row cannot be written,
// no version is written and the active version does not move.
func TestCreateVersionIsOneTransaction(t *testing.T) {
	s := newService(t)
	exec(t, s.DB, `CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'no audit'); END`)
	if _, err := s.Create(t.Context(), admin, Registration, 1, []byte(schemaV2), ""); err == nil {
		t.Fatal("Create succeeded without its audit row")
	}
	if n := count(t, s.DB, `SELECT count(*) FROM form_versions`); n != 1 {
		t.Errorf("%d versions, want 1", n)
	}
	if n := count(t, s.DB, `SELECT active_version FROM forms WHERE key = 'registration'`); n != 1 {
		t.Errorf("active version %d, want 1", n)
	}
}

// TestStaleBaseVersionConflicts: a save based on a version that is no
// longer active is conflict and writes nothing.
func TestStaleBaseVersionConflicts(t *testing.T) {
	s := newService(t)
	if _, err := s.Create(t.Context(), admin, Registration, 1, []byte(schemaV2), ""); err != nil {
		t.Fatal(err)
	}
	for _, base := range []int{1, 0, 3} {
		_, err := s.Create(t.Context(), admin, Registration, base, []byte(schemaV2), "")
		if !errors.Is(err, ErrStaleVersion) || problem.FromError(err) != problem.CodeConflict {
			t.Errorf("base %d: %v, want a stale-version conflict", base, err)
		}
	}
	if n := count(t, s.DB, `SELECT count(*) FROM form_versions`); n != 2 {
		t.Errorf("%d versions, want 2", n)
	}
	if n := count(t, s.DB, `SELECT count(*) FROM audit_log`); n != 1 {
		t.Errorf("%d audit rows, want 1", n)
	}
}

// TestOnlyAnAdminWritesOrReads: the service checks the role itself, and a
// refusal writes nothing.
func TestOnlyAnAdminWritesOrReads(t *testing.T) {
	s := newService(t)
	for name, c := range map[string]struct {
		v    domain.Viewer
		want problem.Code
	}{
		"anonymous":        {domain.Anonymous(), problem.CodeUnauthenticated},
		"a pre-user":       {domain.Viewer{PreUser: true, IdentityID: 1}, problem.CodeForbidden},
		"a moderator":      {moderator, problem.CodeForbidden},
		"an inactive user": {domain.Viewer{UserID: 1, Role: domain.RoleAdmin}, problem.CodeUnauthenticated},
	} {
		_, err := s.Create(t.Context(), c.v, Registration, 1, []byte(schemaV2), "")
		if got := problem.FromError(err); got != c.want {
			t.Errorf("%s saving: %s, want %s", name, got, c.want)
		}
		if _, err := s.Admin(t.Context(), c.v, Registration, 0); problem.FromError(err) != c.want {
			t.Errorf("%s reading: %v", name, err)
		}
		if _, err := s.Preview(t.Context(), c.v, Registration, []byte(schemaV2)); problem.FromError(err) != c.want {
			t.Errorf("%s previewing: %v", name, err)
		}
	}
	if n := count(t, s.DB, `SELECT count(*) FROM form_versions`); n != 1 {
		t.Errorf("%d versions, want 1", n)
	}
}

// TestAnInvalidSchemaWritesNothing: AC6's refusal half, at the service.
func TestAnInvalidSchemaWritesNothing(t *testing.T) {
	s := newService(t)
	_, err := s.Create(t.Context(), admin, Registration, 1, []byte(`{"title":"T","fields":[{"key":"a","type":"date"}]}`), "")
	if !errors.Is(err, forms.ErrInvalidSchema) {
		t.Fatalf("got %v", err)
	}
	if n := count(t, s.DB, `SELECT count(*) FROM form_versions`) + count(t, s.DB, `SELECT count(*) FROM audit_log`); n != 1 {
		t.Errorf("%d rows, want only the seeded version", n)
	}
}

// TestEverySavedVersionLoads: a schema whose stored, canonical form would be
// over the load limit is refused at the save, and the active version still
// loads (review 1407).
func TestEverySavedVersionLoads(t *testing.T) {
	s := newService(t)
	sep := "x" + strings.Repeat(" ", 6000)
	big := `{"title":"T","fields":[{"key":"a","type":"markdown_notice","markdown":"` + sep + `"},` +
		`{"key":"b","type":"markdown_notice","markdown":"` + sep + `"}]}`
	if len(big) > forms.MaxSchemaBytes {
		t.Fatalf("the fixture is %d bytes as sent; it must fit and grow only when stored", len(big))
	}
	_, err := s.Create(t.Context(), admin, Registration, 1, []byte(big), "")
	if fe := problem.FieldErrorsOf(err); len(fe) != 1 || fe[0] != (problem.FieldError{Field: "", Code: forms.CodeTooLong}) {
		t.Fatalf("got %v, want the schema refused as too_long", err)
	}
	if n := count(t, s.DB, `SELECT count(*) FROM form_versions`); n != 1 {
		t.Errorf("%d versions, want 1", n)
	}
	if _, err := s.Active(t.Context(), Registration); err != nil {
		t.Errorf("the active version no longer loads: %v", err)
	}
}

// TestUnknownFormAndVersionAreNotFound.
func TestUnknownFormAndVersionAreNotFound(t *testing.T) {
	s := newService(t)
	for name, err := range map[string]error{
		"active":  func() error { _, err := s.Active(t.Context(), "reports"); return err }(),
		"admin":   func() error { _, err := s.Admin(t.Context(), admin, "reports", 0); return err }(),
		"version": func() error { _, err := s.Admin(t.Context(), admin, Registration, 9); return err }(),
		"by id":   func() error { _, err := s.Version(t.Context(), 7, 1); return err }(),
		"create":  func() error { _, err := s.Create(t.Context(), admin, "reports", 1, []byte(schemaV2), ""); return err }(),
		"preview": func() error { _, err := s.Preview(t.Context(), admin, "reports", []byte(schemaV2)); return err }(),
	} {
		if !errors.Is(err, ErrNoForm) || problem.FromError(err) != problem.CodeNotFound {
			t.Errorf("%s: %v, want not-found", name, err)
		}
	}
}
