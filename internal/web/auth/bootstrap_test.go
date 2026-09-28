package auth

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// bootstrap configures the sign-in to promote the identities cfg names.
func (w *world) bootstrap(cfg config.Bootstrap) {
	svc := w.handler.Login.(*identity.Service)
	svc.Bootstrap = cfg
	svc.Perm = perm.New(w.db)
}

// role is the user's role and how many promotions audit_log records for them.
func (w *world) role(handle string) (string, int) {
	w.t.Helper()
	var role string
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT u.role,
			(SELECT count(*) FROM audit_log a WHERE a.action = 'user.promoted'
			 AND a.target_type = 'user' AND a.target_id = CAST(u.id AS TEXT))
			FROM users u WHERE u.handle = ?`, handle).Scan(&role, &n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return role, n
}

// AC1 and AC4: a subject the config names is made admin at its first
// sign-in and recorded once; later sign-ins change nothing, including after
// a demotion; nobody else is promoted.
func TestBootstrapPromotesOnceAtSignIn(t *testing.T) {
	w := newWorld(t)
	alice := w.user("alice", "active", "user-1")
	w.user("bob", "active", "bob-sub")
	w.bootstrap(config.Bootstrap{AdminSubjects: []string{"fake:user-1"}, AdminEmails: []string{"bob@example.org"}})

	if h := signedInAs(t, w, w.signIn("")); h != "alice" {
		t.Fatalf("signed in as %s", h)
	}
	if role, n := w.role("alice"); role != "admin" || n != 1 {
		t.Fatalf("after the first sign-in: role %s, %d promotions; want admin, 1", role, n)
	}
	var details string
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT details_json FROM audit_log WHERE action = 'user.promoted'
			AND actor_id IS NULL`).Scan(&details)
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"via":"bootstrap"`, `"match":"subject"`, `"provider":"fake"`, `"role":"admin"`} {
		if !strings.Contains(details, want) {
			t.Errorf("audit details %s lack %s", details, want)
		}
	}

	w.signIn("")
	if role, n := w.role("alice"); role != "admin" || n != 1 {
		t.Errorf("after a second sign-in: role %s, %d promotions; want admin, 1", role, n)
	}

	// Demoted by hand, alice stays demoted: the config does not undo an
	// admin's decision at her next sign-in.
	if err := perm.New(w.db).SetRole(t.Context(), alice, "member"); err != nil {
		t.Fatal(err)
	}
	if h := signedInAs(t, w, w.signIn("")); h != "alice" {
		t.Fatalf("signed in as %s", h)
	}
	if role, n := w.role("alice"); role != "member" || n != 1 {
		t.Errorf("after a demotion and a sign-in: role %s, %d promotions; want member, 1", role, n)
	}

	// bob's address is listed, but the IdP asserts no email for him.
	w.idp.Subject = "bob-sub"
	if h := signedInAs(t, w, w.signIn("")); h != "bob" {
		t.Fatalf("signed in as %s", h)
	}
	if role, n := w.role("bob"); role != "member" || n != 0 {
		t.Errorf("bob: role %s, %d promotions; want member, 0", role, n)
	}
}

// AC2 end to end: a listed address makes admin only when the provider
// asserts email_verified, and the refusal is logged for the operator.
func TestBootstrapByEmailNeedsVerification(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprint("email_verified=", verified), func(t *testing.T) {
			w := newWorld(t)
			w.user("alice", "active", "user-1")
			w.bootstrap(config.Bootstrap{AdminEmails: []string{"Alice@Example.org"}})
			w.idp.Claims = map[string]any{"email": "alice@example.org", "email_verified": verified}
			if h := signedInAs(t, w, w.signIn("")); h != "alice" {
				t.Fatalf("signed in as %s", h)
			}
			role, n := w.role("alice")
			warned := strings.Contains(w.log.String(), "did not assert email_verified")
			if verified && (role != "admin" || n != 1 || warned) {
				t.Errorf("verified: role %s, %d promotions, warned %v", role, n, warned)
			}
			if !verified && (role != "member" || n != 0 || !warned) {
				t.Errorf("unverified: role %s, %d promotions, warned %v\n%s", role, n, warned, w.log.String())
			}
		})
	}
}

// A suspended account the config names is refused as any other, and not
// made admin on the way.
func TestBootstrapSkipsAnInactiveAccount(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "suspended", "user-1")
	w.bootstrap(config.Bootstrap{AdminSubjects: []string{"fake:user-1"}})
	w.refused(w.signIn(""), http.StatusForbidden, "Account unavailable")
	if role, n := w.role("alice"); role != "member" || n != 0 {
		t.Errorf("role %s, %d promotions; want member, 0", role, n)
	}
}

// A promotion that fails fails the sign-in and leaves no session, so the
// operator sees the failure, and signing in again retries it.
func TestBootstrapFailureFailsTheSignIn(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	w.bootstrap(config.Bootstrap{AdminSubjects: []string{"fake:user-1"}})
	w.handler.Login.(*identity.Service).Perm = nil
	resp := w.signIn("")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("answered %d", resp.StatusCode)
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		t.Error("a failed sign-in set a session cookie")
	}
	if n := w.sessions(); n != 0 {
		t.Errorf("%d sessions left behind", n)
	}
	var last sql.NullInt64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT last_login_at FROM identities WHERE subject = 'user-1'").Scan(&last)
	}); err != nil {
		t.Fatal(err)
	}
	if last.Valid {
		t.Error("the failed sign-in was recorded on the identity")
	}
	if role, n := w.role("alice"); role != "member" || n != 0 {
		t.Errorf("role %s, %d promotions", role, n)
	}
}
