package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// registry is a migrated database with registration open, and the service
// over it with the real session store.
func registry(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(t.Context(), d, settings.RegistrationMode.Name, json.RawMessage(`"open"`), 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	return &Service{DB: d, Sessions: &session.Store{DB: d}, Perm: perm.New(d)}, d
}

func claims(subject string) Claims {
	return Claims{Provider: "fake", Issuer: "https://idp.example", Subject: subject,
		Email: subject + "@example.org", EmailVerified: true, Name: "Someone " + subject}
}

// count is the rows in each identity table.
func count(t *testing.T, d *db.DB) (users, identities, sessions int) {
	t.Helper()
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM users),
			(SELECT count(*) FROM identities), (SELECT count(*) FROM sessions)`).Scan(&users, &identities, &sessions)
	}); err != nil {
		t.Fatal(err)
	}
	return
}

func TestRegisterCreatesAMemberSignedIn(t *testing.T) {
	s, d := registry(t)
	res, err := s.Register(t.Context(), claims("sub-1"), "  Alice ", session.Meta{Provider: "fake"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != SignedIn || res.Token == "" {
		t.Fatalf("Register = %+v", res)
	}
	sess, err := (&session.Store{DB: d}).Lookup(t.Context(), res.Token)
	if err != nil || sess.User == nil {
		t.Fatalf("the token names no user session: %v", err)
	}
	if sess.User.Handle != "alice" || sess.User.DisplayName != "Someone sub-1" || sess.User.Status != "active" ||
		sess.User.Role != domain.RoleMember {
		t.Errorf("the user is %+v", sess.User)
	}
	var ident dbq.Identity
	d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		ident, err = q.GetIdentityBySubject(t.Context(), dbq.GetIdentityBySubjectParams{ProviderKey: "fake", Subject: "sub-1"})
		return err
	})
	if err != nil || ident.Issuer != "https://idp.example" || ident.Email.String != "sub-1@example.org" ||
		ident.EmailVerified != 1 || !ident.LastLoginAt.Valid {
		t.Errorf("the identity is %+v, %v", ident, err)
	}
}

// A display name falls back to the handle when the IdP asserts none.
func TestRegisterWithNoName(t *testing.T) {
	s, d := registry(t)
	c := claims("sub-1")
	c.Name = "  "
	if _, err := s.Register(t.Context(), c, "alice", session.Meta{}, ""); err != nil {
		t.Fatal(err)
	}
	var name string
	d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT display_name FROM users").Scan(&name)
	})
	if name != "alice" {
		t.Errorf("display name %q, want the handle", name)
	}
}

// AC1 and AC3: an invalid, reserved or taken handle is refused before
// anything is written, and a taken one whatever its case.
func TestRegisterRefuses(t *testing.T) {
	cases := []struct {
		handle string
		want   error
	}{
		{"al", domain.ErrInvalidHandle},
		{"al ice", domain.ErrInvalidHandle},
		{"admin", ErrHandleReserved},
		{"Admin", ErrHandleReserved},
		{"deleted-user-7", ErrHandleReserved},
		{"taken", ErrHandleTaken},
		{"TAKEN", ErrHandleTaken},
	}
	for _, c := range cases {
		t.Run(c.handle, func(t *testing.T) {
			s, d := registry(t)
			if _, err := s.Register(t.Context(), claims("first"), "taken", session.Meta{}, ""); err != nil {
				t.Fatal(err)
			}
			_, err := s.Register(t.Context(), claims("second"), c.handle, session.Meta{}, "")
			if !errors.Is(err, c.want) {
				t.Fatalf("Register(%q) = %v, want %v", c.handle, err, c.want)
			}
			if u, i, n := count(t, d); u != 1 || i != 1 || n != 1 {
				t.Errorf("after a refusal: %d users, %d identities, %d sessions; want the first registration's alone", u, i, n)
			}
		})
	}
}

// An identity registers once. The second submission, from a second tab or a
// double click, is told so and writes nothing.
func TestRegisterTwice(t *testing.T) {
	s, d := registry(t)
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice2", session.Meta{}, ""); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("a second registration = %v, want ErrAlreadyRegistered", err)
	}
	if u, i, n := count(t, d); u != 1 || i != 1 || n != 1 {
		t.Errorf("%d users, %d identities, %d sessions; want 1 each", u, i, n)
	}
}

// Registration is only for mode = "open", and says so if an admin closes it
// while someone is choosing a handle.
func TestRegisterWhenClosed(t *testing.T) {
	s, d := registry(t)
	if err := settings.Set(t.Context(), d, settings.RegistrationMode.Name, json.RawMessage(`"approval"`), 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if open, err := s.RegistrationOpen(t.Context()); err != nil || open {
		t.Fatalf("RegistrationOpen = %v, %v; want false", open, err)
	}
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, ""); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register = %v, want ErrRegistrationClosed", err)
	}
	if u, i, n := count(t, d); u+i+n != 0 {
		t.Errorf("a closed registration wrote %d users, %d identities, %d sessions", u, i, n)
	}
}

// AC2: people registering one handle at the same moment, in different case,
// make one user; everyone else is told the handle is taken. Run under -race
// against a real SQLite file.
func TestConcurrentRegistrationsMakeOneUser(t *testing.T) {
	s, d := registry(t)
	handles := []string{"alice", "Alice", "ALICE", "aLiCe", "alicE", "ALice", "alICE", "AlIcE"}
	errs := make([]error, len(handles))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, h := range handles {
		wg.Go(func() {
			<-start
			_, errs[i] = s.Register(t.Context(), claims(fmt.Sprintf("sub-%d", i)), h, session.Meta{}, "")
		})
	}
	close(start)
	wg.Wait()
	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrHandleTaken):
			t.Errorf("registration %d (%q) = %v, want ErrHandleTaken", i, handles[i], err)
		}
	}
	if won != 1 {
		t.Errorf("%d registrations succeeded, want exactly 1", won)
	}
	if u, i, n := count(t, d); u != 1 || i != 1 || n != 1 {
		t.Errorf("%d users, %d identities, %d sessions; want 1 each", u, i, n)
	}
}

// AC4: when the session cannot be written, neither are the user and the
// identity. A trigger makes the session insert fail after both have been
// inserted in the same transaction.
func TestRegistrationIsAtomic(t *testing.T) {
	s, d := registry(t)
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER no_sessions BEFORE INSERT ON sessions
			BEGIN SELECT RAISE(ABORT, 'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, ""); err == nil {
		t.Fatal("Register succeeded with no way to write a session")
	}
	if u, i, n := count(t, d); u+i+n != 0 {
		t.Errorf("a failed registration left %d users, %d identities, %d sessions", u, i, n)
	}
}

// The same, failing between the user and the identity.
func TestRegistrationIsAtomicAtTheIdentity(t *testing.T) {
	s, d := registry(t)
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER no_identities BEFORE INSERT ON identities
			BEGIN SELECT RAISE(ABORT, 'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, ""); err == nil {
		t.Fatal("Register succeeded with no way to write an identity")
	}
	if u, i, n := count(t, d); u+i+n != 0 {
		t.Errorf("a failed registration left %d users, %d identities, %d sessions", u, i, n)
	}
}

// A config-named admin who registers is an admin from that moment, not from
// their second sign-in (TKT-01M29C800G): Register applies the bootstrap to
// the user it creates. Nobody else is promoted.
func TestRegisterAppliesTheBootstrap(t *testing.T) {
	s, d := registry(t)
	s.Bootstrap = config.Bootstrap{AdminSubjects: []string{"fake:boss"}}
	s.Perm = perm.New(d)
	boss, err := s.Register(t.Context(), claims("boss"), "boss", session.Meta{}, "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Register(t.Context(), claims("other"), "other", session.Meta{}, "")
	if err != nil {
		t.Fatal(err)
	}
	store := &session.Store{DB: d}
	for tok, want := range map[string]string{boss.Token: domain.RoleAdmin, other.Token: domain.RoleMember} {
		sess, err := store.Lookup(t.Context(), tok)
		if err != nil || sess.User == nil || sess.User.Role != want {
			t.Errorf("%+v, %v: want role %s", sess.User, err, want)
		}
	}
	var n int
	d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE action = ?`, perm.ActionPromoted).Scan(&n)
	})
	if n != 1 {
		t.Errorf("%d promotions recorded, want 1", n)
	}
}

// A bootstrap that fails ends the session the registration issued, and the
// registration fails, as a sign-in does. The account stands: signing in
// again retries the promotion.
func TestRegisterWhoseBootstrapFails(t *testing.T) {
	s, d := registry(t)
	s.Bootstrap = config.Bootstrap{AdminSubjects: []string{"fake:boss"}}
	s.Perm = nil
	if _, err := s.Register(t.Context(), claims("boss"), "boss", session.Meta{}, ""); err == nil {
		t.Fatal("Register succeeded without the promotion the configuration asks for")
	}
	if u, i, n := count(t, d); u != 1 || i != 1 || n != 0 {
		t.Errorf("%d users, %d identities, %d sessions; want the account and no session", u, i, n)
	}
}

// Registering ends the session the browser held, as a sign-in does.
func TestRegisterEndsTheSessionItReplaces(t *testing.T) {
	s, d := registry(t)
	old, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(t.Context(), claims("sub-2"), "bob", session.Meta{}, old.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := (&session.Store{DB: d}).Lookup(t.Context(), old.Token); err == nil {
		t.Error("the session the registration replaced still works")
	}
}

// closing is a session store on which an admin closes registration between
// Register's start and its transaction: the interleaving review 1145 names.
type closing struct {
	*session.Store
	t *testing.T
}

func (c closing) IssueUserWith(ctx context.Context, meta session.Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error) {
	if err := settings.Set(ctx, c.DB, settings.RegistrationMode.Name, json.RawMessage(`"approval"`), 0, time.Now()); err != nil {
		c.t.Fatal(err)
	}
	return c.Store.IssueUserWith(ctx, meta, create)
}

// Registration closed after Register began, but before its transaction, is
// still closed: the mode is read in the transaction that writes the account.
func TestRegistrationClosedJustBeforeTheWrite(t *testing.T) {
	s, d := registry(t)
	s.Sessions = closing{Store: &session.Store{DB: d}, t: t}
	if _, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{}, ""); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("Register = %v, want ErrRegistrationClosed", err)
	}
	if u, i, n := count(t, d); u+i+n != 0 {
		t.Errorf("a closed registration wrote %d users, %d identities, %d sessions", u, i, n)
	}
}

// TKT-01M3KB7FXC: a member who registers after ketju reindex raised the
// member id floor gets an id above it, never the id of an old post's author
// the database lacks.
func TestRegisterTakesAnIDAboveTheFloor(t *testing.T) {
	s, d := registry(t)
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, _, err := db.RaiseMemberIDFloor(t.Context(), tx, 50)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Register(t.Context(), claims("sub-1"), "alice", session.Meta{Provider: "fake"}, "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := (&session.Store{DB: d}).Lookup(t.Context(), res.Token)
	if err != nil || sess.User == nil {
		t.Fatalf("the token names no user session: %v", err)
	}
	if sess.User.ID != 51 {
		t.Errorf("the new member's id is %d, want 51, above the floor", sess.User.ID)
	}
}
