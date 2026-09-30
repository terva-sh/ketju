package sitesettings

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
)

var (
	now   = time.UnixMilli(1_757_500_000_000)
	admin = domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true, SessionKey: "s1"}
)

func newService(t *testing.T) *Service {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, handle, display_name, status, role, kind,
			prefs_json, created_at, updated_at) VALUES (1, 'root', 'Site Admin', 'active', 'admin', 'human', '{}', 1, 1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return &Service{DB: d, Now: func() time.Time { return now }}
}

// TestOnlyAnAdmin: each method asks for an admin itself, so a caller that
// forgot the API's gate is still refused, and a refused Set stores nothing.
func TestOnlyAnAdmin(t *testing.T) {
	s := newService(t)
	for name, v := range map[string]domain.Viewer{
		"a moderator": {UserID: 2, Role: domain.RoleModerator, Active: true, SessionKey: "s2"},
		"a member":    {UserID: 3, Role: domain.RoleMember, Active: true, SessionKey: "s3"},
		"anonymous":   {},
	} {
		if _, err := s.Get(t.Context(), v); err == nil {
			t.Errorf("Get as %s: no error", name)
		}
		if _, err := s.Set(t.Context(), v, "192.0.2.1", "site_name", json.RawMessage(`"Mine"`)); err == nil {
			t.Errorf("Set as %s: no error", name)
		}
	}
	l, err := s.Get(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range l.Settings {
		if v.Key == "site_name" && (!v.IsDefault || v.UpdatedAt != nil) {
			t.Errorf("a refused Set stored site_name: %+v", v)
		}
	}
}

// TestSetAnswersWithTheKey: an admin's Set answers with the key as it now
// stands, naming the admin, and maps the registry's refusals onto problem
// codes.
func TestSetAnswersWithTheKey(t *testing.T) {
	s := newService(t)
	got, err := s.Set(t.Context(), admin, "192.0.2.1", "site_name", json.RawMessage(`"The Supper Club"`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != `"The Supper Club"` || got.IsDefault || got.UpdatedBy == nil ||
		got.UpdatedBy.Handle != "root" || got.UpdatedAt == nil || int64(*got.UpdatedAt) != now.UnixMilli() {
		t.Errorf("the answer: %+v", got)
	}
	for _, c := range []struct {
		key, value string
		want       problem.Code
	}{
		{"nope", `1`, problem.CodeNotFound},
		{"site_name", `7`, problem.CodeValidationFailed},
	} {
		_, err := s.Set(t.Context(), admin, "", c.key, json.RawMessage(c.value))
		if got := problem.FromError(err); got != c.want {
			t.Errorf("Set(%s, %s) = %v, code %s, want %s", c.key, c.value, err, got, c.want)
		}
	}
}
