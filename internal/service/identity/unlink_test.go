package identity

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// These are TKT-01M29CCC0X's: removing a linked sign-in (docs/05
// "Sessions and CSRF", decided by the run 2026-10-01).

// unlinking is a registry with both test providers configured, alice
// registered through "fake" and holding that session, and a second
// identity of hers linked at "other".
type unlinking struct {
	s           *Service
	d           *db.DB
	alice       int64
	aliceTok    string
	fake, other int64 // alice's identities
}

func newUnlinking(t *testing.T) *unlinking {
	t.Helper()
	s, d := registry(t)
	s.Providers = []ProviderName{{Key: "fake", Name: "Fake"}, {Key: "other", Name: "Other"}}
	u := &unlinking{s: s, d: d}
	u.alice, u.aliceTok = member(t, s, "alice")
	if why, err := s.Link(t.Context(), u.alice, u.aliceTok, otherClaims("alice-other"), ""); err != nil || why != LinkRefusalNone {
		t.Fatalf("Link = %v, %v", why, err)
	}
	f, _ := identityRow(t, d, "fake", "alice-sub")
	o, _ := identityRow(t, d, "other", "alice-other")
	u.fake, u.other = f.ID, o.ID
	return u
}

// viewer is userID as an active member.
func viewer(userID int64) domain.Viewer {
	return domain.Viewer{UserID: userID, Role: domain.RoleMember, Active: true}
}

// signInThrough is a session for the user identityID belongs to, signed in
// through it as a callback would.
func (u *unlinking) signInThrough(t *testing.T, identityID int64) string {
	t.Helper()
	tok, _, err := u.s.Sessions.IssueUser(t.Context(), identityID, session.Meta{Provider: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// live reports whether token still resolves.
func (u *unlinking) live(t *testing.T, token string) bool {
	t.Helper()
	_, err := u.s.Sessions.(*session.Store).Lookup(t.Context(), token)
	if err != nil && !errors.Is(err, session.ErrNoSession) {
		t.Fatal(err)
	}
	return err == nil
}

// unlinkedRows is every identity.unlinked audit row, as "actor action
// type:id provider identity ip".
func unlinkedRows(t *testing.T, d *db.DB) []string {
	t.Helper()
	var got []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT actor_id || ' ' || action || ' ' || target_type || ':' || target_id
			|| ' ' || (details_json ->> 'provider') || ' ' || (details_json ->> 'identity') || ' ' || ip
			FROM audit_log WHERE action = ? ORDER BY id`, ActionUnlinked)
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

// refusedWith checks err is want, coded as code, and that nothing was
// written: the counts are as before and no identity.unlinked row exists.
func (u *unlinking) refusedWith(t *testing.T, err, want error, code problem.Code, users0, idents0, sessions0 int) {
	t.Helper()
	if !errors.Is(err, want) || problem.FromError(err) != code {
		t.Errorf("Unlink = %v (%s); want %v as %s", err, problem.FromError(err), want, code)
	}
	if us, is, ss := count(t, u.d); us != users0 || is != idents0 || ss != sessions0 {
		t.Errorf("a refused removal wrote: %d users, %d identities, %d sessions; before %d, %d, %d",
			us, is, ss, users0, idents0, sessions0)
	}
	if got := unlinkedRows(t, u.d); len(got) != 0 {
		t.Errorf("a refused removal was audited: %q", got)
	}
}

// TestUnlinkRefusesTheLastIdentity: with one identity left, removing it is
// conflict, and no row, session or audit row changes.
func TestUnlinkRefusesTheLastIdentity(t *testing.T) {
	s, d := registry(t)
	s.Providers = []ProviderName{{Key: "fake", Name: "Fake"}}
	alice, tok := member(t, s, "alice")
	ident, _ := identityRow(t, d, "fake", "alice-sub")
	u := &unlinking{s: s, d: d}
	u0, i0, n0 := count(t, d)
	_, err := s.Unlink(t.Context(), viewer(alice), tok, ident.ID, "")
	u.refusedWith(t, err, ErrLastIdentity, problem.CodeConflict, u0, i0, n0)
}

// TestUnlinkRefusesWhenOnlyUnconfiguredProvidersRemain: an identity on a
// provider the operator removed cannot sign in, so it does not count. The
// identity on the removed provider may itself be removed.
func TestUnlinkRefusesWhenOnlyUnconfiguredProvidersRemain(t *testing.T) {
	u := newUnlinking(t)
	u.s.Providers = []ProviderName{{Key: "fake", Name: "Fake"}}
	u0, i0, n0 := count(t, u.d)
	_, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.fake, "")
	u.refusedWith(t, err, ErrLastIdentity, problem.CodeConflict, u0, i0, n0)

	// With no provider configured at all, nothing may be removed.
	u.s.Providers = nil
	_, err = u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, "")
	u.refusedWith(t, err, ErrLastIdentity, problem.CodeConflict, u0, i0, n0)

	u.s.Providers = []ProviderName{{Key: "fake", Name: "Fake"}}
	if out, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, ""); err != nil || out {
		t.Fatalf("removing the unconfigured identity = %v, %v", out, err)
	}
	if _, ok := identityRow(t, u.d, "other", "alice-other"); ok {
		t.Error("the identity is still there")
	}
}

// TestUnlinkRaceLeavesOneIdentity: two removals of the account's two
// identities at once. Exactly one succeeds, the other is ErrLastIdentity,
// and one identity remains. The session asking came through neither, so
// the loser is refused for the last identity and not for a session the
// winner ended. Repeated, under -race in CI.
func TestUnlinkRaceLeavesOneIdentity(t *testing.T) {
	for round := range 20 {
		u := newUnlinking(t)
		tok := sessiontest.SignIn(t, u.d, u.alice, time.Now())
		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for i, id := range []int64{u.fake, u.other} {
			wg.Go(func() {
				<-start
				_, errs[i] = u.s.Unlink(t.Context(), viewer(u.alice), tok, id, "")
			})
		}
		close(start)
		wg.Wait()
		ok, last := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLastIdentity):
				last++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		_, idents, _ := count(t, u.d)
		if ok != 1 || last != 1 || idents != 1 {
			t.Fatalf("round %d: %d removed, %d refused, %d identities left; want 1, 1, 1", round, ok, last, idents)
		}
		if got := unlinkedRows(t, u.d); len(got) != 1 {
			t.Fatalf("round %d: %d audit rows", round, len(got))
		}
	}
}

// TestUnlinkEndsSessionsSignedInThroughIt: the sessions through the
// removed identity end and the others stay, and the answer says whether
// the asking session was one of them.
func TestUnlinkEndsSessionsSignedInThroughIt(t *testing.T) {
	u := newUnlinking(t)
	viaFake := u.signInThrough(t, u.fake)
	viaOther := u.signInThrough(t, u.other)
	viaOther2 := u.signInThrough(t, u.other)

	// Asked from a session through other: the fake sessions end, and the
	// asking one does not.
	ended, err := u.s.Unlink(t.Context(), viewer(u.alice), viaOther, u.fake, "")
	if err != nil || ended {
		t.Fatalf("Unlink = %v, %v; want the asking session kept", ended, err)
	}
	for name, c := range map[string]struct {
		tok  string
		want bool
	}{"registered through fake": {u.aliceTok, false}, "signed in through fake": {viaFake, false},
		"asking, through other": {viaOther, true}, "another through other": {viaOther2, true}} {
		if got := u.live(t, c.tok); got != c.want {
			t.Errorf("the session %s is live: %v, want %v", name, got, c.want)
		}
	}

	// Asked from a session through the identity removed: it ends too.
	if why, err := u.s.Link(t.Context(), u.alice, viaOther, claims("alice-third"), ""); err != nil || why != LinkRefusalNone {
		t.Fatalf("Link = %v, %v", why, err)
	}
	ended, err = u.s.Unlink(t.Context(), viewer(u.alice), viaOther, u.other, "")
	if err != nil || !ended {
		t.Fatalf("Unlink = %v, %v; want the asking session ended", ended, err)
	}
	if u.live(t, viaOther) || u.live(t, viaOther2) {
		t.Error("a session through the removed identity is still live")
	}
}

// TestUnlinkOtherUsersIdentityIsNotFound: alice naming bob's identity is
// not-found, and nothing changes: bob keeps the identity and the session.
func TestUnlinkOtherUsersIdentityIsNotFound(t *testing.T) {
	u := newUnlinking(t)
	_, bobTok := member(t, u.s, "bob")
	bobs, _ := identityRow(t, u.d, "fake", "bob-sub")
	u0, i0, n0 := count(t, u.d)
	for _, id := range []int64{bobs.ID, 9999} {
		_, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, id, "")
		u.refusedWith(t, err, ErrNoIdentity, problem.CodeNotFound, u0, i0, n0)
	}
	if !u.live(t, bobTok) {
		t.Error("bob's session ended")
	}
	// Nor may alice act through bob's session.
	_, err := u.s.Unlink(t.Context(), viewer(u.alice), bobTok, u.other, "")
	u.refusedWith(t, err, ErrNotSignedIn, problem.CodeUnauthenticated, u0, i0, n0)
}

// TestUnlinkNeedsALiveSession: a token revoked before the call refuses the
// removal and writes nothing, as for Link (TestLinkNeedsTheMembersSession).
func TestUnlinkNeedsALiveSession(t *testing.T) {
	u := newUnlinking(t)
	if err := u.s.Sessions.Revoke(t.Context(), u.aliceTok); err != nil {
		t.Fatal(err)
	}
	u0, i0, n0 := count(t, u.d)
	for _, tok := range []string{u.aliceTok, "", "not-a-token"} {
		_, err := u.s.Unlink(t.Context(), viewer(u.alice), tok, u.other, "")
		u.refusedWith(t, err, ErrNotSignedIn, problem.CodeUnauthenticated, u0, i0, n0)
	}
}

// TestUnlinkRefusesAnAccountThatCannot: an agent or an account that is not
// active is forbidden, whatever the viewer says.
func TestUnlinkRefusesAnAccountThatCannot(t *testing.T) {
	for _, set := range []string{"kind = 'agent'", "status = 'suspended'"} {
		u := newUnlinking(t)
		exec(t, u.d, "UPDATE users SET "+set+" WHERE id = ?", u.alice)
		u0, i0, n0 := count(t, u.d)
		_, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, "")
		u.refusedWith(t, err, ErrCannotUnlink, problem.CodeForbidden, u0, i0, n0)
	}
}

// TestUnlinkKeepsTheApplication: an application naming the identity keeps
// its row, with identity_id null.
func TestUnlinkKeepsTheApplication(t *testing.T) {
	u := newUnlinking(t)
	exec(t, u.d, `INSERT INTO applications (identity_id, form_id, form_version, answers_json, status, user_id,
		decided_at, created_at) VALUES (?, 1, 1, '{}', 'approved', ?, 1, 1)`, u.other, u.alice)
	if _, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, ""); err != nil {
		t.Fatal(err)
	}
	var n int
	var identity sql.NullInt64
	if err := u.d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*), max(identity_id) FROM applications WHERE user_id = ?`,
			u.alice).Scan(&n, &identity)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 || identity.Valid {
		t.Errorf("%d applications, identity_id %v; want one with null", n, identity)
	}
}

// TestUnlinkedSubjectSignsInAsNobody: once removed, the subject is
// nobody's. A sign-in with it while signed out takes the registration
// path and does not reach alice's account.
func TestUnlinkedSubjectSignsInAsNobody(t *testing.T) {
	u := newUnlinking(t)
	if _, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, ""); err != nil {
		t.Fatal(err)
	}
	oc := otherClaims("alice-other")
	res, err := u.s.Login(t.Context(), &oidc.Identity{Provider: "other", Issuer: oc.Issuer, Subject: "alice-other",
		Email: oc.Email, EmailVerified: true, RawClaims: oc.RawClaims}, domain.Viewer{}, 0, session.Meta{Provider: "other"}, "")
	if err != nil || res.Outcome != Register || res.Token != "" {
		t.Fatalf("signing in with the removed subject: %+v, %v; want the registration path", res, err)
	}
}

// TestUnlinkWritesOneAuditRow: one identity.unlinked row, against alice,
// by alice, with the provider, the identity and the address.
func TestUnlinkWritesOneAuditRow(t *testing.T) {
	u := newUnlinking(t)
	if _, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, "192.0.2.9"); err != nil {
		t.Fatal(err)
	}
	want := itoa(u.alice) + " identity.unlinked user:" + itoa(u.alice) + " other " + itoa(u.other) + " 192.0.2.9"
	if got := unlinkedRows(t, u.d); len(got) != 1 || got[0] != want {
		t.Errorf("audit rows %q, want %q", got, want)
	}
}

// TestUnlinkIsOneTransaction: with the audit write failing, neither the
// identity nor its sessions go.
func TestUnlinkIsOneTransaction(t *testing.T) {
	u := newUnlinking(t)
	via := u.signInThrough(t, u.other)
	exec(t, u.d, `CREATE TRIGGER no_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	u0, i0, n0 := count(t, u.d)
	if _, err := u.s.Unlink(t.Context(), viewer(u.alice), u.aliceTok, u.other, ""); err == nil {
		t.Fatal("Unlink succeeded with no way to write its audit row")
	}
	if us, is, ss := count(t, u.d); us != u0 || is != i0 || ss != n0 || !u.live(t, via) {
		t.Errorf("the failed removal wrote: %d, %d, %d; before %d, %d, %d", us, is, ss, u0, i0, n0)
	}
}

// TestListIdentities: the account's own identities, oldest first, with
// the provider's configured name, the current one marked, and removable
// as Unlink would decide it.
func TestListIdentities(t *testing.T) {
	u := newUnlinking(t)
	member(t, u.s, "bob")
	u.s.Providers = []ProviderName{{Key: "other", Name: "Other IdP"}}
	list, err := u.s.ListIdentities(t.Context(), viewer(u.alice), u.fake)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Identities) != 2 {
		t.Fatalf("listed %+v; want alice's two", list.Identities)
	}
	f, o := list.Identities[0], list.Identities[1]
	if f.ID != u.fake || f.Provider != "fake" || f.ProviderName != "" || f.Configured || !f.Current || !f.Removable ||
		f.Email == nil || *f.Email != "alice-sub@example.org" {
		t.Errorf("the fake identity is %+v", f)
	}
	if o.ID != u.other || o.ProviderName != "Other IdP" || !o.Configured || o.Current || o.Removable || o.LastLoginAt != nil {
		t.Errorf("the other identity is %+v: the only configured one is not removable", o)
	}
	if _, err := u.s.ListIdentities(t.Context(), domain.Anonymous(), 0); problem.FromError(err) != problem.CodeUnauthenticated {
		t.Errorf("anonymous: %v", err)
	}
}
