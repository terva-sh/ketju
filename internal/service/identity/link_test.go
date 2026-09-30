package identity

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// These are TKT-01M29CKRES's: account linking, one user and many
// identities, reached only through an explicit link.

// member registers an active member with handle through the fake provider
// and returns their user id.
func member(t *testing.T, s *Service, handle string) (int64, string) {
	t.Helper()
	res, err := s.Register(t.Context(), claims(handle+"-sub"), handle, session.Meta{Provider: "fake"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return userOf(t, s, res.Token), res.Token
}

// userOf is the user a session token signs in, 0 for none.
func userOf(t *testing.T, s *Service, token string) int64 {
	t.Helper()
	sess, err := s.Sessions.(*session.Store).Lookup(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if sess.User == nil {
		return 0
	}
	return sess.User.ID
}

// otherClaims is an identity at a second provider, "other".
func otherClaims(subject string) Claims {
	c := claims(subject)
	c.Provider, c.Issuer = "other", "https://other.example"
	c.RawClaims = json.RawMessage(`{"sub":"` + subject + `"}`)
	return c
}

func identityRow(t *testing.T, d *db.DB, provider, subject string) (dbq.Identity, bool) {
	t.Helper()
	var ident dbq.Identity
	err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		ident, err = q.GetIdentityBySubject(t.Context(), dbq.GetIdentityBySubjectParams{ProviderKey: provider, Subject: subject})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ident, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return ident, true
}

// linkedRows is every identity.linked audit row, as "actor action type:id
// provider identity ip".
func linkedRows(t *testing.T, d *db.DB) []string {
	t.Helper()
	var got []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT actor_id || ' ' || action || ' ' || target_type || ':' || target_id
			|| ' ' || (details_json ->> 'provider') || ' ' || (details_json ->> 'identity') || ' ' || ip
			FROM audit_log WHERE action = ? ORDER BY id`, ActionLinked)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			got = append(got, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return got
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

// AC2 and AC7: a link attaches the identity to the member, writes one
// identity.linked row, and makes no user and no session. It is not a
// sign-in, so last_login_at and raw_claims_json stay empty until the
// identity is first used to sign in, which sets them (review 1342,
// finding 2).
func TestLinkAttachesAndAudits(t *testing.T) {
	s, d := registry(t)
	alice, aliceTok := member(t, s, "alice")
	u0, i0, n0 := count(t, d)

	why, err := s.Link(t.Context(), alice, aliceTok, otherClaims("other-sub"), "192.0.2.7")
	if err != nil || why != LinkRefusalNone {
		t.Fatalf("Link = %v, %v", why, err)
	}
	u, i, n := count(t, d)
	if u != u0 || n != n0 || i != i0+1 {
		t.Errorf("after the link: %d users, %d identities, %d sessions; before %d, %d, %d", u, i, n, u0, i0, n0)
	}
	ident, ok := identityRow(t, d, "other", "other-sub")
	if !ok {
		t.Fatal("no identity was written")
	}
	if ident.UserID.Int64 != alice || ident.Issuer != "https://other.example" || ident.Email.String != "other-sub@example.org" ||
		ident.EmailVerified != 1 {
		t.Errorf("the identity is %+v", ident)
	}
	if ident.LastLoginAt.Valid || ident.RawClaimsJson.Valid {
		t.Errorf("a link was recorded as a sign-in: last_login_at %v, raw_claims_json %v", ident.LastLoginAt, ident.RawClaimsJson)
	}
	want := []string{itoa(alice) + " identity.linked user:" + itoa(alice) + " other " + itoa(ident.ID) + " 192.0.2.7"}
	if got := linkedRows(t, d); len(got) != 1 || got[0] != want[0] {
		t.Errorf("audit rows %q, want %q", got, want)
	}

	// The first sign-in with it is a sign-in as alice, through IssueUser,
	// and records it.
	oc := otherClaims("other-sub")
	res, err := s.Login(t.Context(), &oidc.Identity{Provider: "other", Issuer: oc.Issuer, Subject: "other-sub",
		Email: oc.Email, EmailVerified: true, RawClaims: oc.RawClaims}, domain.Viewer{}, 0, session.Meta{Provider: "other"}, "")
	if err != nil || res.Outcome != SignedIn || userOf(t, s, res.Token) != alice {
		t.Fatalf("signing in with the linked identity: %+v, %v", res, err)
	}
	ident, _ = identityRow(t, d, "other", "other-sub")
	if !ident.LastLoginAt.Valid || ident.RawClaimsJson.String != `{"sub":"other-sub"}` {
		t.Errorf("the first sign-in was not recorded: %v, %v", ident.LastLoginAt, ident.RawClaimsJson)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// AC2: the identity and its audit row are one transaction. With the audit
// write failing, no identity is written.
func TestLinkIsOneTransaction(t *testing.T) {
	s, d := registry(t)
	alice, aliceTok := member(t, s, "alice")
	exec(t, d, `CREATE TRIGGER no_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	_, i0, _ := count(t, d)
	if _, err := s.Link(t.Context(), alice, aliceTok, otherClaims("other-sub"), ""); err == nil {
		t.Fatal("Link succeeded with no way to write its audit row")
	}
	if _, ok := identityRow(t, d, "other", "other-sub"); ok {
		t.Error("the identity was written without its audit row")
	}
	if _, i, _ := count(t, d); i != i0 {
		t.Errorf("%d identities, want %d", i, i0)
	}
}

// AC4: a member who is not active cannot link, and neither can an agent,
// which never uses OIDC. Nothing is written.
func TestLinkRefusesAnInactiveMember(t *testing.T) {
	for _, c := range []struct{ name, set string }{
		{"pending", "status = 'pending'"},
		{"suspended", "status = 'suspended'"},
		{"deactivated", "status = 'deactivated'"},
		{"agent", "kind = 'agent'"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, d := registry(t)
			alice, aliceTok := member(t, s, "alice")
			exec(t, d, "UPDATE users SET "+c.set+" WHERE id = ?", alice)
			u0, i0, n0 := count(t, d)
			why, err := s.Link(t.Context(), alice, aliceTok, otherClaims("other-sub"), "")
			if !errors.Is(err, ErrCannotLink) || why != LinkRefusalNone {
				t.Errorf("Link = %v, %v; want ErrCannotLink", why, err)
			}
			if u, i, n := count(t, d); u != u0 || i != i0 || n != n0 {
				t.Errorf("the refused link wrote rows: %d, %d, %d; before %d, %d, %d", u, i, n, u0, i0, n0)
			}
			if got := linkedRows(t, d); len(got) != 0 {
				t.Errorf("audit rows %q", got)
			}
		})
	}
	t.Run("no such user", func(t *testing.T) {
		s, _ := registry(t)
		if _, err := s.Link(t.Context(), 999, "", otherClaims("other-sub"), ""); !errors.Is(err, ErrCannotLink) {
			t.Errorf("Link = %v", err)
		}
	})
}

// Link writes only while the browser is still signed in as the member who
// started it, which it checks in the transaction that writes (Terva review
// 1505). A session ended after the callback read its viewer, a session of
// another member, a pre-user's, an expired one, and none at all are each
// LinkNotYou, and nothing is written.
func TestLinkNeedsTheMembersSession(t *testing.T) {
	for _, c := range []struct {
		name  string
		token func(t *testing.T, s *Service, d *db.DB, aliceTok, bobTok string) string
	}{
		{"signed out since", func(t *testing.T, s *Service, _ *db.DB, aliceTok, _ string) string {
			if err := s.Sessions.Revoke(t.Context(), aliceTok); err != nil {
				t.Fatal(err)
			}
			return aliceTok
		}},
		{"every session revoked", func(t *testing.T, s *Service, d *db.DB, aliceTok, _ string) string {
			exec(t, d, "DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE handle = 'alice')")
			return aliceTok
		}},
		{"another member's session", func(_ *testing.T, _ *Service, _ *db.DB, _, bobTok string) string { return bobTok }},
		{"expired", func(t *testing.T, s *Service, _ *db.DB, aliceTok, _ string) string {
			s.Now = func() time.Time { return time.Now().Add(session.HardExpiry + time.Hour) }
			return aliceTok
		}},
		{"no session", func(*testing.T, *Service, *db.DB, string, string) string { return "" }},
		{"not a token", func(*testing.T, *Service, *db.DB, string, string) string { return "nonsense" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, d := registry(t)
			alice, aliceTok := member(t, s, "alice")
			_, bobTok := member(t, s, "bob")
			token := c.token(t, s, d, aliceTok, bobTok)
			_, i0, n0 := count(t, d)
			why, err := s.Link(t.Context(), alice, token, otherClaims("other-sub"), "")
			if err != nil || why != LinkNotYou {
				t.Errorf("Link = %v, %v; want LinkNotYou", why, err)
			}
			if _, ok := identityRow(t, d, "other", "other-sub"); ok {
				t.Error("the identity was linked without the member's session")
			}
			if _, i, n := count(t, d); i != i0 || n != n0 {
				t.Errorf("%d identities and %d sessions, before %d and %d", i, n, i0, n0)
			}
			if got := linkedRows(t, d); len(got) != 0 {
				t.Errorf("audit rows %q", got)
			}
		})
	}
	t.Run("a pre-user session", func(t *testing.T) {
		s, d := registry(t)
		alice, _ := member(t, s, "alice")
		approval(t, d)
		res, err := s.BeginApplication(t.Context(), claims("applicant"), session.Meta{}, "")
		if err != nil {
			t.Fatal(err)
		}
		if why, err := s.Link(t.Context(), alice, res.Token, otherClaims("other-sub"), ""); err != nil || why != LinkNotYou {
			t.Errorf("Link = %v, %v; want LinkNotYou", why, err)
		}
	})
}

// AC1: an identity someone has by the time the link is written is refused
// with what it belongs to, and no identities row changes.
func TestLinkFindsTheIdentityTaken(t *testing.T) {
	s, d := registry(t)
	alice, aliceTok := member(t, s, "alice")
	bob, bobTok := member(t, s, "bob")
	// bob's second identity, an applicant's, and one alice already has.
	if _, err := s.Link(t.Context(), bob, bobTok, otherClaims("bob-other"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(t.Context(), alice, aliceTok, otherClaims("alice-other"), ""); err != nil {
		t.Fatal(err)
	}
	exec(t, d, `INSERT INTO identities (provider_key, subject, issuer, created_at, updated_at)
		VALUES ('other', 'applicant', 'https://other.example', 1, 1)`)
	snapshot := func() string {
		var s string
		d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT group_concat(id || ':' || coalesce(user_id, '-') || ':' || subject, ',')
				FROM identities`).Scan(&s)
		})
		return s
	}
	before, audits := snapshot(), len(linkedRows(t, d))
	for subject, want := range map[string]LinkRefusal{
		"bob-other":   LinkOtherUser,
		"applicant":   LinkApplicant,
		"alice-other": LinkAlready,
	} {
		why, err := s.Link(t.Context(), alice, aliceTok, otherClaims(subject), "")
		if err != nil || why != want {
			t.Errorf("%s: Link = %v, %v; want %v", subject, why, err, want)
		}
	}
	if after := snapshot(); after != before {
		t.Errorf("identities changed:\n  %s\nwas\n  %s", after, before)
	}
	if n := len(linkedRows(t, d)); n != audits {
		t.Errorf("%d identity.linked rows, want %d", n, audits)
	}
}

// Login's answer for each combination of link intent, viewer and identity.
// A link attempt never signs in and never registers, and nothing Login
// answers here writes a row.
func TestLoginDecidesLinks(t *testing.T) {
	s, d := registry(t)
	alice, _ := member(t, s, "alice")
	bob, _ := member(t, s, "bob")
	exec(t, d, `INSERT INTO identities (provider_key, subject, issuer, created_at, updated_at)
		VALUES ('fake', 'applicant', 'https://idp.example', 1, 1)`)
	asAlice := domain.Viewer{UserID: alice, Active: true, Role: domain.RoleMember}
	inactiveAlice := domain.Viewer{UserID: alice, Role: domain.RoleMember}
	asBob := domain.Viewer{UserID: bob, Active: true, Role: domain.RoleMember}
	for _, c := range []struct {
		name     string
		subject  string
		viewer   domain.Viewer
		linkUser int64
		want     Outcome
		why      LinkRefusal
	}{
		{"new, alice linking", "new", asAlice, alice, Link, LinkRefusalNone},
		{"new, signed in, no intent", "new", asAlice, 0, LinkRefused, LinkNoIntent},
		{"new, signed out, no intent", "new", domain.Viewer{}, 0, Register, LinkRefusalNone},
		{"new, alice's link, signed out", "new", domain.Viewer{}, alice, LinkRefused, LinkNotYou},
		{"new, alice's link, bob signed in", "new", asBob, alice, LinkRefused, LinkNotYou},
		{"new, alice's link, alice not active", "new", inactiveAlice, alice, LinkRefused, LinkNotYou},
		{"bob's, alice linking", "bob-sub", asAlice, alice, LinkRefused, LinkOtherUser},
		{"an applicant's, alice linking", "applicant", asAlice, alice, LinkRefused, LinkApplicant},
		{"alice's own, alice linking", "alice-sub", asAlice, alice, LinkRefused, LinkAlready},
		{"bob's, alice's link, signed out", "bob-sub", domain.Viewer{}, alice, LinkRefused, LinkNotYou},
	} {
		t.Run(c.name, func(t *testing.T) {
			u0, i0, n0 := count(t, d)
			res, err := s.Login(t.Context(), &oidc.Identity{Provider: "fake", Subject: c.subject}, c.viewer, c.linkUser,
				session.Meta{Provider: "fake"}, "")
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != c.want || res.Refusal != c.why || res.Token != "" {
				t.Errorf("Login = %+v, want outcome %v refusal %v and no session", res, c.want, c.why)
			}
			if u, i, n := count(t, d); u != u0 || i != i0 || n != n0 {
				t.Errorf("Login wrote rows: %d, %d, %d; before %d, %d, %d", u, i, n, u0, i0, n0)
			}
		})
	}
}
