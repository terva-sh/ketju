package session

import (
	"database/sql"
	"errors"
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
