package account

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

type world struct {
	t   *testing.T
	db  *db.DB
	svc *Service
	now time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, now: time.UnixMilli(1_790_000_000_000)}
	w.svc = &Service{DB: d, Perm: perm.New(d), Now: func() time.Time { return w.now }}
	return w
}

// user makes an active account of role, and returns it as the viewer its
// session would be.
func (w *world) user(handle, role string) domain.Viewer {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: domain.StatusActive, Role: role, Kind: domain.KindHuman, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return domain.Viewer{UserID: id, Role: role, Active: true, SessionKey: "s-" + handle}
}

func (w *world) status(handle string) string {
	w.t.Helper()
	var s string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(w.t.Context(), handle)
		s = u.Status
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return s
}

func code(err error) problem.Code {
	if err == nil {
		return ""
	}
	return problem.FromError(err)
}

// TestSuspendAuthority is AC7: moderators suspend members only, admins
// anyone except themselves, and nobody the last active admin. A refusal
// changes nothing, and unsuspending follows the same rule.
func TestSuspendAuthority(t *testing.T) {
	for _, c := range []struct {
		name, actor, target string
		want                problem.Code // "" is allowed
	}{
		{"moderator on member", "mod", "mem", ""},
		{"moderator on moderator", "mod", "mod2", problem.CodeForbidden},
		{"moderator on admin", "mod", "adm2", problem.CodeForbidden},
		{"moderator on self", "mod", "mod", problem.CodeForbidden},
		{"admin on member", "adm", "mem", ""},
		{"admin on moderator", "adm", "mod", ""},
		{"admin on another admin", "adm", "adm2", ""},
		{"admin on self", "adm", "adm", problem.CodeForbidden},
		{"member on member", "mem", "mem2", problem.CodeForbidden},
		{"nobody's handle", "adm", "ghost", problem.CodeNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			vs := map[string]domain.Viewer{}
			for h, role := range map[string]string{"mem": domain.RoleMember, "mem2": domain.RoleMember,
				"mod": domain.RoleModerator, "mod2": domain.RoleModerator, "adm": domain.RoleAdmin, "adm2": domain.RoleAdmin} {
				vs[h] = w.user(h, role)
			}
			err := w.svc.Suspend(t.Context(), vs[c.actor], "192.0.2.1", c.target, "spam", nil)
			if got := code(err); (c.want == "" && err != nil) || (c.want != "" && got != c.want) {
				t.Fatalf("suspend: %v (code %q), want %q", err, got, c.want)
			}
			if c.want != "" {
				if c.target != "ghost" && w.status(c.target) != domain.StatusActive {
					t.Errorf("a refused suspension left %s %s", c.target, w.status(c.target))
				}
				return
			}
			if s := w.status(c.target); s != domain.StatusSuspended {
				t.Fatalf("%s is %s", c.target, s)
			}
			if err := w.svc.Unsuspend(t.Context(), vs[c.actor], "", c.target); err != nil {
				t.Fatalf("unsuspend: %v", err)
			}
			if s := w.status(c.target); s != domain.StatusActive {
				t.Errorf("after unsuspending, %s is %s", c.target, s)
			}
		})
	}

	t.Run("the last active admin", func(t *testing.T) {
		// Once adm has suspended the only other admin, adm is the last
		// active one: nobody below may act on them, and they may not leave.
		w := newWorld(t)
		adm := w.user("adm", domain.RoleAdmin)
		w.user("other", domain.RoleAdmin)
		if err := w.svc.Suspend(t.Context(), adm, "", "other", "spam", nil); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.Deactivate(t.Context(), adm, ""); code(err) != problem.CodeConflict {
			t.Errorf("the last active admin deactivated: %v", err)
		}
		if s := w.status("adm"); s != domain.StatusActive {
			t.Errorf("adm is %s", s)
		}
		if err := w.svc.Anonymize(t.Context(), w.user("adm3", domain.RoleAdmin), "", "adm"); err != nil {
			t.Fatalf("with a second active admin, anonymizing the first: %v", err)
		}
	})

	t.Run("a promotion between the lookup and the change", func(t *testing.T) {
		// The authority rule is asked about the row in the transaction that
		// changes it, so a member made moderator a moment ago is out of a
		// moderator's reach.
		w := newWorld(t)
		mod := w.user("mod", domain.RoleModerator)
		w.user("mem", domain.RoleMember)
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `UPDATE users SET role = 'moderator' WHERE handle = 'mem'`)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.Suspend(t.Context(), mod, "", "mem", "spam", nil); code(err) != problem.CodeForbidden {
			t.Errorf("a moderator suspended a moderator: %v", err)
		}
	})
}

// TestSuspendChecksWhatItIsSent: the reason and the end, before anything
// is looked up.
func TestSuspendChecksWhatItIsSent(t *testing.T) {
	w := newWorld(t)
	mod := w.user("mod", domain.RoleModerator)
	w.user("mem", domain.RoleMember)
	past, future := w.now.Add(-time.Minute), w.now.Add(time.Hour)
	for _, c := range []struct {
		name   string
		reason string
		until  *time.Time
	}{
		{"no reason", "", nil},
		{"only spaces", "   ", nil},
		{"too long", strings.Repeat("é", ReasonMaxLen+1), nil},
		{"a control character", "spam\x00", nil},
		{"a newline", "spam\nmore", nil},
		{"not UTF-8", "spam\xff", nil},
		{"an end in the past", "spam", &past},
		{"an end now", "spam", &w.now},
	} {
		if err := w.svc.Suspend(t.Context(), mod, "", "mem", c.reason, c.until); code(err) != problem.CodeValidationFailed {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if s := w.status("mem"); s != domain.StatusActive {
		t.Fatalf("a refused suspension left mem %s", s)
	}
	if err := w.svc.Suspend(t.Context(), mod, "", "MEM", "  "+strings.Repeat("é", ReasonMaxLen)+" ", &future); err != nil {
		t.Fatalf("the longest reason, with an end, by the handle in capitals: %v", err)
	}
}

// Only a member deactivates, and only their own account; only an admin
// anonymizes, and never themselves.
func TestDeactivateAndAnonymizeAuthority(t *testing.T) {
	w := newWorld(t)
	mem := w.user("mem", domain.RoleMember)
	mod := w.user("mod", domain.RoleModerator)
	adm := w.user("adm", domain.RoleAdmin)
	w.user("adm2", domain.RoleAdmin)
	if err := w.svc.Deactivate(t.Context(), domain.Anonymous(), ""); code(err) != problem.CodeUnauthenticated {
		t.Errorf("anonymous deactivated: %v", err)
	}
	if err := w.svc.Anonymize(t.Context(), mod, "", "mem"); code(err) != problem.CodeForbidden {
		t.Errorf("a moderator anonymized: %v", err)
	}
	if err := w.svc.Anonymize(t.Context(), adm, "", "adm"); code(err) != problem.CodeForbidden {
		t.Errorf("an admin anonymized themselves: %v", err)
	}
	if err := w.svc.Deactivate(t.Context(), mem, ""); err != nil {
		t.Fatal(err)
	}
	if s := w.status("mem"); s != domain.StatusDeactivated {
		t.Errorf("mem is %s", s)
	}
	if err := w.svc.Anonymize(t.Context(), adm, "", "mem"); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Anonymize(t.Context(), adm, "", "deleted-user-1"); code(err) != problem.CodeConflict {
		t.Errorf("anonymized twice: %v", err)
	}
}
