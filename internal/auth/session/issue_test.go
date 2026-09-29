package session

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// identityFor gives a user of this status an identity to sign in with, and
// returns the identity's id.
func (w *world) identityFor(handle, status string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: status, Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		id, err = q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: u, Valid: true},
			ProviderKey: "github", Subject: "sub-" + handle, Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// sessions counts the rows in the table, whatever their kind.
func (w *world) sessions() int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), "SELECT count(*) FROM sessions").Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

var meta = Meta{IP: "192.0.2.7", UserAgent: "test-agent", Provider: "github"}

// AC2: only an active user gets a session. Every other status is refused
// with the status named, and nothing is written.
func TestIssueUserRefusesAnyStatusButActive(t *testing.T) {
	for _, status := range []string{"pending", "suspended", "deactivated"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			ident := w.identityFor("u-"+status, status)
			tok, _, err := w.store.IssueUser(t.Context(), ident, meta)
			if !errors.Is(err, ErrNotActive) {
				t.Fatalf("IssueUser for a %s user: err = %v, want ErrNotActive", status, err)
			}
			var na *NotActiveError
			if !errors.As(err, &na) || na.Status != status {
				t.Fatalf("the refusal names status %+v, want %q", na, status)
			}
			if tok != "" {
				t.Errorf("a refusal returned a token")
			}
			if n := w.sessions(); n != 0 {
				t.Errorf("a refusal wrote %d session rows", n)
			}
		})
	}
}

func TestIssueUserSignsInAnActiveUser(t *testing.T) {
	w := newWorld(t)
	ident := w.identityFor("bea", "active")
	tok, sess, err := w.store.IssueUser(t.Context(), ident, meta)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Kind != KindUser || sess.IdentityID != ident || sess.Provider != "github" {
		t.Errorf("issued %+v", sess)
	}
	got, err := w.store.Lookup(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindUser || got.User == nil || got.User.Handle != "bea" || got.IdentityID != ident || got.Provider != "github" {
		t.Fatalf("looked up %+v", got)
	}
	if !got.ExpiresAt.Equal(sess.ExpiresAt) || !got.CreatedAt.Equal(sess.CreatedAt) {
		t.Errorf("issued times %v/%v, stored %v/%v", sess.CreatedAt, sess.ExpiresAt, got.CreatedAt, got.ExpiresAt)
	}
	if v := got.Viewer(); !v.Member() || v.PreUser {
		t.Errorf("an active user's viewer is %+v", v)
	}
	infos, err := w.store.ListForUser(t.Context(), got.User.ID)
	if err != nil || len(infos) != 1 || infos[0].IP != meta.IP || infos[0].UserAgent != meta.UserAgent {
		t.Errorf("the session list shows %+v (%v)", infos, err)
	}
}

// An applicant's identity has no user, and IssueUser will not make it a
// member session; an identity that does not exist is not one either.
func TestIssueUserRefusesAnIdentityWithoutAUser(t *testing.T) {
	w := newWorld(t)
	if _, _, err := w.store.IssueUser(t.Context(), w.identity, meta); !errors.Is(err, ErrNoUser) {
		t.Errorf("an applicant's identity: err = %v, want ErrNoUser", err)
	}
	if _, _, err := w.store.IssueUser(t.Context(), 9999, meta); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("a missing identity: err = %v, want ErrNoIdentity", err)
	}
	if n := w.sessions(); n != 0 {
		t.Errorf("refusals wrote %d session rows", n)
	}
}

// AC4: a pre-user session is its own call, which writes only a pre-user row
// and never serves an identity that has a user, active or not.
func TestIssuePreUserIsNeverAMember(t *testing.T) {
	w := newWorld(t)
	tok, sess, err := w.store.IssuePreUser(t.Context(), w.identity, meta)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Kind != KindPreUser || sess.IdentityID != w.identity {
		t.Errorf("issued %+v", sess)
	}
	got, err := w.store.Lookup(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindPreUser || got.User != nil || got.Provider != "github" {
		t.Fatalf("looked up %+v", got)
	}
	if v := got.Viewer(); v.Member() || v.Active || v.UserID != 0 || !v.PreUser || v.IdentityID != w.identity {
		t.Errorf("a pre-user's viewer is %+v", v)
	}

	for _, status := range []string{"active", "suspended"} {
		ident := w.identityFor("has-"+status, status)
		if _, _, err := w.store.IssuePreUser(t.Context(), ident, meta); !errors.Is(err, ErrHasUser) {
			t.Errorf("IssuePreUser for a %s user's identity: err = %v, want ErrHasUser", status, err)
		}
	}
	if _, _, err := w.store.IssuePreUser(t.Context(), 9999, meta); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("a missing identity: err = %v, want ErrNoIdentity", err)
	}
	if n := w.sessions(); n != 1 {
		t.Errorf("%d session rows, want the one pre-user session", n)
	}
}

// insert returns a refusal as it is, writes nothing for it, and wraps only
// a failure of the write itself.
func TestInsertReturnsARefusalUnwrapped(t *testing.T) {
	w := newWorld(t)
	no := errors.New("refused")
	_, _, err := w.store.insert(t.Context(), func(*dbq.Queries) (newSession, error) { return newSession{}, no })
	if err != no {
		t.Errorf("err = %v, want the refusal itself", err)
	}
	// A user session with no user breaks the table's CHECK: the write fails.
	_, _, err = w.store.insert(t.Context(), func(*dbq.Queries) (newSession, error) { return newSession{Kind: KindUser}, nil })
	if err == nil || !strings.HasPrefix(err.Error(), "session: create: ") {
		t.Errorf("a failed write: err = %v", err)
	}
	if n := w.sessions(); n != 0 {
		t.Errorf("%d session rows", n)
	}
}

// userFor makes a user of this status and links identity to them, the step
// approving an application takes (TKT-01M29CP2MG), and nothing else: no
// session is revoked, as if the approval code had forgotten to.
func (w *world) userFor(identity int64, handle, status string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: status, Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(w.t.Context(), "UPDATE identities SET user_id = ? WHERE id = ?", id, identity)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// count runs a count(*) query.
func (w *world) count(query string) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), query).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) memberSessions() int {
	return w.count("SELECT count(*) FROM sessions WHERE kind = 'user'")
}
func (w *world) identities() int { return w.count("SELECT count(*) FROM identities") }

// TestAPreUserCannotBecomeAMember is AC3 of TKT-01M29CKRG9. Once an
// applicant's identity belongs to a user, their pre-user cookie is refused
// at lookup, whatever that user's status; and for a pending, suspended or
// deactivated user the next sign-in writes no member session. For an active
// one it does, and the old cookie stays refused: a pre-user session never
// becomes a member session in place.
func TestAPreUserCannotBecomeAMember(t *testing.T) {
	for _, status := range []string{"pending", "suspended", "deactivated", "active"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			pre := w.preUser()
			if _, err := w.store.Lookup(t.Context(), pre); err != nil {
				t.Fatalf("the pre-user session does not resolve before approval: %v", err)
			}
			w.userFor(w.identity, "applicant", status)

			if _, err := w.store.Lookup(t.Context(), pre); !errors.Is(err, ErrNoSession) {
				t.Errorf("the pre-user cookie of an identity that now has a user: err = %v, want ErrNoSession", err)
			}
			rec := get(w.server(nil), pre)
			if rec.Code != http.StatusUnauthorized || !cleared(rec) {
				t.Errorf("a request with it answered %d (cleared: %v), want an anonymous 401 that clears it",
					rec.Code, cleared(rec))
			}

			tok, _, err := w.store.IssueUser(t.Context(), w.identity, meta)
			if status != "active" {
				var na *NotActiveError
				if !errors.As(err, &na) || na.Status != status {
					t.Errorf("signing in again as a %s user: err = %v, want a *NotActiveError for %s", status, err, status)
				}
				if n := w.memberSessions(); n != 0 {
					t.Errorf("%d member sessions, want none", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("signing in again as an active user: %v", err)
			}
			got, err := w.store.Lookup(t.Context(), tok)
			if err != nil || got.Kind != KindUser || got.User == nil || got.User.Handle != "applicant" {
				t.Fatalf("the new session is %+v (%v), want applicant's member session", got, err)
			}
			if n := w.memberSessions(); n != 1 {
				t.Errorf("%d member sessions, want the one just issued", n)
			}
			if _, err := w.store.Lookup(t.Context(), pre); !errors.Is(err, ErrNoSession) {
				t.Errorf("after the member signed in, the old pre-user cookie: err = %v, want ErrNoSession", err)
			}
		})
	}
}

// TestIssuePreUserWithIsAtomic: the identity create writes and the pre-user
// session commit together or not at all. A failed session insert leaves no
// identity, and an identity that turns out to have a user is ErrHasUser
// and leaves nothing either.
func TestIssuePreUserWithIsAtomic(t *testing.T) {
	newIdentity := func(user int64) func(context.Context, *dbq.Queries) (int64, error) {
		return func(ctx context.Context, q *dbq.Queries) (int64, error) {
			return q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: user, Valid: user != 0},
				ProviderKey: "github", Subject: "new-applicant", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		}
	}

	t.Run("issued", func(t *testing.T) {
		w := newWorld(t)
		before := w.identities()
		tok, sess, err := w.store.IssuePreUserWith(t.Context(), meta, newIdentity(0))
		if err != nil {
			t.Fatal(err)
		}
		got, err := w.store.Lookup(t.Context(), tok)
		if err != nil || got.Kind != KindPreUser || got.User != nil || got.IdentityID != sess.IdentityID {
			t.Fatalf("looked up %+v (%v)", got, err)
		}
		if n := w.identities(); n != before+1 {
			t.Errorf("%d identities, want %d", n, before+1)
		}
	})

	t.Run("the session insert fails", func(t *testing.T) {
		w := newWorld(t)
		before := w.identities()
		if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER no_sessions BEFORE INSERT ON sessions
				BEGIN SELECT RAISE(ABORT, 'no sessions today'); END`)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_, _, err := w.store.IssuePreUserWith(t.Context(), meta, newIdentity(0))
		if err == nil || !strings.Contains(err.Error(), "no sessions today") {
			t.Fatalf("err = %v, want the trigger's refusal", err)
		}
		if n := w.identities(); n != before {
			t.Errorf("%d identities after a failed insert, want %d: the identity outlived its session", n, before)
		}
		if n := w.sessions(); n != 0 {
			t.Errorf("%d sessions", n)
		}
	})

	t.Run("the identity has a user", func(t *testing.T) {
		w := newWorld(t)
		before := w.identities()
		_, _, err := w.store.IssuePreUserWith(t.Context(), meta, newIdentity(w.member))
		if !errors.Is(err, ErrHasUser) {
			t.Fatalf("err = %v, want ErrHasUser", err)
		}
		if n := w.identities(); n != before {
			t.Errorf("%d identities, want %d", n, before)
		}
		if n := w.sessions(); n != 0 {
			t.Errorf("%d sessions", n)
		}
	})

	t.Run("create refuses", func(t *testing.T) {
		w := newWorld(t)
		before := w.identities()
		no := errors.New("registration is closed")
		_, _, err := w.store.IssuePreUserWith(t.Context(), meta, func(ctx context.Context, q *dbq.Queries) (int64, error) {
			if _, err := newIdentity(0)(ctx, q); err != nil {
				return 0, err
			}
			return 0, no
		})
		if err != no {
			t.Fatalf("err = %v, want create's refusal as it is", err)
		}
		if n, s := w.identities(), w.sessions(); n != before || s != 0 {
			t.Errorf("%d identities and %d sessions, want %d and none", n, s, before)
		}
	})
}

// RevokePreUser ends every pre-user session of one identity and nothing
// else: not another applicant's, not a member's.
func TestRevokePreUser(t *testing.T) {
	w := newWorld(t)
	a, b := w.preUser(), w.preUser()
	var other int64
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		other, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github",
			Subject: "applicant-2", Issuer: "https://github.com", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	otherTok, _, err := w.store.IssuePreUser(t.Context(), other, meta)
	if err != nil {
		t.Fatal(err)
	}
	member := w.signIn(w.member)
	if err := w.store.RevokePreUser(t.Context(), w.identity); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{a, b} {
		if _, err := w.store.Lookup(t.Context(), tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("a revoked applicant session: err = %v", err)
		}
	}
	for _, tok := range []string{otherTok, member} {
		if _, err := w.store.Lookup(t.Context(), tok); err != nil {
			t.Errorf("a session RevokePreUser should not touch: %v", err)
		}
	}
	if n := w.sessions(); n != 2 {
		t.Errorf("%d sessions, want 2", n)
	}
}
