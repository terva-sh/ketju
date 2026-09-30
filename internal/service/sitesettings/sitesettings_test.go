package sitesettings

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"regexp"
	"sync"
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
	return newTracedService(t, nil)
}

// newTracedService is newService with trace called on every statement
// SQLite runs.
func newTracedService(t *testing.T, trace func(string)) *Service {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db"), Trace: trace})
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

// TestSetAnswersFromTheWritesTransaction is Terva review 1587's finding on
// PR #230: the answer is read before the change commits, in the same
// transaction, so a failure building it rolls the change back rather than
// reporting failure for a change that was made, which a caller would retry.
// The request is cancelled as the answer's read of the key starts, after
// the audit row is written. Set fails, and the key and the audit log are as
// they were.
func TestSetAnswersFromTheWritesTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	audited := false
	name := regexp.MustCompile(`^-- name: (\w+)`)
	s := newTracedService(t, func(stmt string) {
		mu.Lock()
		defer mu.Unlock()
		m := name.FindStringSubmatch(stmt)
		switch {
		case m == nil:
		case m[1] == "AppendAudit":
			audited = true
		case m[1] == "GetSetting" && audited:
			cancel()
		}
	})
	if _, err := s.Set(ctx, admin, "192.0.2.1", "site_name", json.RawMessage(`"Club"`)); err == nil {
		t.Fatal("Set succeeded with its request cancelled while it built the answer")
	}
	mu.Lock()
	if !audited {
		t.Fatal("the audit row was never written, so the cancel came too early to test anything")
	}
	mu.Unlock()
	l, err := s.Get(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range l.Settings {
		if v.Key == "site_name" && (string(v.Value) != `"Ketju"` || v.UpdatedAt != nil) {
			t.Errorf("the failed Set committed: %+v", v)
		}
	}
	var n int
	if err := s.DB.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the failed Set left %d audit rows", n)
	}
}
