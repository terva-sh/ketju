package auth

import (
	"database/sql"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// mapsForumMods is the provider configuration of the tests below: the
// IdP's forum-mods group is the local moderators group.
func mapsForumMods(pc *config.Provider) {
	pc.GroupsClaim = "groups"
	pc.GroupMap = map[string]string{"forum-mods": "moderators"}
}

// mapping gives the sign-in the policy of these providers, which must be
// the ones w signs in through.
func (w *world) mapping(pcs ...config.Provider) *identity.Service {
	svc := w.handler.Login.(*identity.Service)
	svc.Policy = identity.PolicyFrom(pcs)
	return svc
}

// groupsClaim sets what the IdP asserts in the groups claim; nil leaves the
// claim out of the token.
func (w *world) groupsClaim(groups []string) {
	if groups == nil {
		w.idp.Claims = nil
		return
	}
	w.idp.Claims = map[string]any{"groups": groups}
}

// identityAt gives userID a second identity, at provider, for subject.
func (w *world) identityAt(userID int64, provider, subject string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: userID, Valid: true},
			ProviderKey: provider, Subject: subject, Issuer: w.idp.URL, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// signInWith is the whole flow through provider key, carrying cookies.
func (w *world) signInWith(key string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	resp := w.do("GET", "/auth/"+key+"/start")
	if resp.StatusCode != http.StatusFound {
		w.t.Fatalf("start at %s answered %d", key, resp.StatusCode)
	}
	flow := cookie(resp, FlowCookie)
	cb := w.idp.Authorize(w.t, resp.Header.Get("Location"))
	prefix := site + "/auth/" + key + "/callback"
	if !strings.HasPrefix(cb, prefix) {
		w.t.Fatalf("the IdP sent the browser to %s", cb)
	}
	return w.do("GET", strings.TrimPrefix(cb, site), append([]*http.Cookie{flow}, cookies...)...)
}

// groups is userID's memberships, as slug to source.
func (w *world) groups(userID int64) map[string]string {
	w.t.Helper()
	got := map[string]string{}
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT g.slug, gm.source FROM group_members gm
			JOIN groups g ON g.id = gm.group_id WHERE gm.user_id = ?`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var slug, source string
			if err := rows.Scan(&slug, &source); err != nil {
				return err
			}
			got[slug] = source
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return got
}

// membershipAudit is the membership rows audit_log holds about userID, in
// order, each as "action group provider".
func (w *world) membershipAudit(userID int64) []string {
	w.t.Helper()
	var got []string
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT action, details_json ->> 'group',
			details_json ->> 'provider', details_json ->> 'source', ip
			FROM audit_log WHERE target_type = 'user' AND target_id = CAST(? AS TEXT)
			AND action IN (?, ?) ORDER BY id`, userID, perm.ActionMemberAdded, perm.ActionMemberRemoved)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var action, group, provider, source, ip string
			if err := rows.Scan(&action, &group, &provider, &source, &ip); err != nil {
				return err
			}
			if source != domain.MembershipIdP || ip != "192.0.2.9" {
				w.t.Errorf("audit row %s %s: source %q, ip %q", action, group, source, ip)
			}
			got = append(got, action+" "+group+" "+provider)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return got
}

// newGroup makes a group with this slug and returns its id.
func (w *world) newGroup(slug string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateGroup(w.t.Context(), dbq.CreateGroupParams{Name: slug, Slug: slug, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) groupID(slug string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(w.t.Context(), slug)
		id = g.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func mustSignIn(t *testing.T, w *world, resp *http.Response, handle string) *http.Cookie {
	t.Helper()
	if h := signedInAs(t, w, resp); h != handle {
		t.Fatalf("signed in as %s, want %s", h, handle)
	}
	return cookie(resp, session.CookieName)
}

// AC6: each sign-in makes the idp memberships what this token maps to. A
// group asserted is added, and one no longer asserted, or a claim left out
// of the token, removes it, each with one audit row.
func TestGroupMapAtSignIn(t *testing.T) {
	w := newWorld(t, mapsForumMods)
	fake := w.idp.Provider("fake")
	mapsForumMods(&fake)
	w.mapping(fake)
	alice := w.user("alice", "active", "user-1")

	w.groupsClaim([]string{"forum-mods", "unmapped"})
	mustSignIn(t, w, w.signIn(""), "alice")
	if got := w.groups(alice); len(got) != 1 || got["moderators"] != "idp" {
		t.Fatalf("after a sign-in asserting forum-mods: %v, want moderators from the idp", got)
	}
	mustSignIn(t, w, w.signIn(""), "alice")
	w.groupsClaim([]string{})
	mustSignIn(t, w, w.signIn(""), "alice")
	if got := w.groups(alice); len(got) != 0 {
		t.Fatalf("after a sign-in asserting no groups: %v, want none", got)
	}
	w.groupsClaim([]string{"forum-mods"})
	mustSignIn(t, w, w.signIn(""), "alice")
	w.groupsClaim(nil)
	mustSignIn(t, w, w.signIn(""), "alice")
	if got := w.groups(alice); len(got) != 0 {
		t.Fatalf("after a sign-in with no groups claim: %v, want none", got)
	}
	want := []string{
		"group.member_added moderators fake",
		"group.member_removed moderators fake",
		"group.member_added moderators fake",
		"group.member_removed moderators fake",
	}
	if got := w.membershipAudit(alice); !slices.Equal(got, want) {
		t.Errorf("audit rows %q, want %q (and none for the sign-in that changed nothing)", got, want)
	}
}

// AC2: a claim value missing from group_map, or mapped to a slug no group
// has, creates no group and no membership, and the unknown slug is logged
// at warn.
func TestGroupMapGrantsNothingUnmapped(t *testing.T) {
	pc := func(pc *config.Provider) {
		pc.GroupsClaim = "groups"
		pc.GroupMap = map[string]string{"forum-mods": "moderators", "ghosts": "no-such-group"}
	}
	w := newWorld(t, pc)
	fake := w.idp.Provider("fake")
	pc(&fake)
	w.mapping(fake)
	alice := w.user("alice", "active", "user-1")
	var before int
	count := func() int {
		var n int
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), "SELECT count(*) FROM groups").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before = count()

	w.groupsClaim([]string{"ghosts", "staff", "admins"})
	mustSignIn(t, w, w.signIn(""), "alice")
	if got := w.groups(alice); len(got) != 0 {
		t.Errorf("memberships %v, want none", got)
	}
	if n := count(); n != before {
		t.Errorf("the sign-in created %d groups", n-before)
	}
	if rows := w.membershipAudit(alice); len(rows) != 0 {
		t.Errorf("audit rows %q", rows)
	}
	var warned bool
	for _, line := range strings.Split(w.log.String(), "\n") {
		warned = warned || strings.Contains(line, "level=WARN") && strings.Contains(line, "group=no-such-group") &&
			strings.Contains(line, "provider=fake")
	}
	if !warned {
		t.Errorf("the unknown target was not logged at warn:\n%s", w.log.String())
	}
}

// AC1: an admin's membership survives a sign-in whose claim does not
// assert it, and one the claim does assert stays the admin's, unaudited.
func TestAManualMembershipSurvivesSignIn(t *testing.T) {
	w := newWorld(t, mapsForumMods)
	fake := w.idp.Provider("fake")
	mapsForumMods(&fake)
	svc := w.mapping(fake)
	alice := w.user("alice", "active", "user-1")
	staff := w.newGroup("staff")
	for _, g := range []int64{staff, w.groupID("moderators")} {
		if err := svc.Perm.AddMember(t.Context(), g, alice, 0); err != nil {
			t.Fatal(err)
		}
	}
	w.groupsClaim([]string{"forum-mods"})
	mustSignIn(t, w, w.signIn(""), "alice")
	w.groupsClaim([]string{})
	mustSignIn(t, w, w.signIn(""), "alice")
	want := map[string]string{"staff": "admin", "moderators": "admin"}
	if got := w.groups(alice); len(got) != 2 || got["staff"] != "admin" || got["moderators"] != "admin" {
		t.Errorf("memberships %v, want %v", got, want)
	}
	if rows := w.membershipAudit(alice); len(rows) != 0 {
		t.Errorf("audit rows %q, want none: the mapping changed nothing", rows)
	}
}

// AC7 (review 1342, finding 1): a member who gained a group through a
// mapped provider and then signs in through one without a group_map loses
// it, and the next permission check says so. With two mapped providers
// that disagree, each sign-in leaves exactly what its own map yields. An
// admin's membership survives both.
func TestAnUnmappedProviderSignInDropsIdPGroups(t *testing.T) {
	setup := func(t *testing.T, otherMap map[string]string) (*world, *identity.Service, int64) {
		w := newWorld(t)
		club, other := w.idp.Provider("club"), w.idp.Provider("other")
		mapsForumMods(&club)
		if otherMap != nil {
			other.GroupsClaim, other.GroupMap = "groups", otherMap
		}
		w.withProviders(club, other)
		svc := w.mapping(club, other)
		var alice int64
		if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			alice, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "alice", DisplayName: "alice",
				Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		w.identityAt(alice, "club", "user-1")
		w.identityAt(alice, "other", "user-1")
		if err := svc.Perm.AddMember(t.Context(), w.newGroup("staff"), alice, 0); err != nil {
			t.Fatal(err)
		}
		return w, svc, alice
	}

	t.Run("unmapped", func(t *testing.T) {
		w, svc, alice := setup(t, nil)
		var forum int64
		if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "f", Name: "F", Position: 1,
				Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := svc.Perm.Grant(t.Context(), forum, w.groupID("moderators"), domain.PermModerate); err != nil {
			t.Fatal(err)
		}
		viewer := domain.Viewer{UserID: alice, Role: domain.RoleMember, Active: true, SessionKey: "alice"}
		moderates := func() perm.Decision {
			d, err := svc.Perm.Check(t.Context(), viewer, forum, domain.PermModerate)
			if err != nil {
				t.Fatal(err)
			}
			return d
		}

		w.groupsClaim([]string{"forum-mods"})
		mustSignIn(t, w, w.signInWith("club"), "alice")
		if got := w.groups(alice); got["moderators"] != "idp" {
			t.Fatalf("after signing in through club: %v", got)
		}
		if d := moderates(); d != perm.Allow {
			t.Fatalf("moderate after club's grant: %v", d)
		}
		// The directory revokes forum-mods; alice signs in through other,
		// whose token still carries the group but which maps nothing.
		mustSignIn(t, w, w.signInWith("other"), "alice")
		if got := w.groups(alice); len(got) != 1 || got["staff"] != "admin" {
			t.Errorf("after signing in through other: %v, want only staff from the admin", got)
		}
		want := []string{"group.member_added moderators club", "group.member_removed moderators other"}
		if got := w.membershipAudit(alice); !slices.Equal(got, want) {
			t.Errorf("audit rows %q, want %q", got, want)
		}
		if d := moderates(); d != perm.Deny {
			t.Errorf("moderate on the next check after other's sign-in: %v, want deny", d)
		}
	})

	t.Run("two maps disagree", func(t *testing.T) {
		w, _, alice := setup(t, map[string]string{"forum-mods": "editors"})
		w.newGroup("editors")
		w.groupsClaim([]string{"forum-mods"})
		for _, step := range []struct{ provider, group string }{
			{"club", "moderators"}, {"other", "editors"}, {"club", "moderators"},
		} {
			mustSignIn(t, w, w.signInWith(step.provider), "alice")
			got := w.groups(alice)
			if len(got) != 2 || got[step.group] != "idp" || got["staff"] != "admin" {
				t.Errorf("after signing in through %s: %v, want %s from the idp and staff from the admin",
					step.provider, got, step.group)
			}
		}
	})
}

// AC3: a sign-in whose audit row cannot be written changes no membership,
// fails, and leaves no session: neither the one it issued nor the one it
// replaced.
func TestAFailedGroupSyncFailsTheSignIn(t *testing.T) {
	w := newWorld(t, mapsForumMods)
	fake := w.idp.Provider("fake")
	mapsForumMods(&fake)
	w.mapping(fake)
	alice := w.user("alice", "active", "user-1")
	w.groupsClaim([]string{"forum-mods"})
	old := mustSignIn(t, w, w.signIn(""), "alice")
	audits := len(w.membershipAudit(alice))

	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
			BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w.groupsClaim([]string{})
	flow, authURL := w.begin("")
	resp := w.do("GET", w.back(authURL), flow, old)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("answered %d, want 500", resp.StatusCode)
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		t.Error("the failed sign-in set a session cookie")
	}
	if n := w.sessions(); n != 0 {
		t.Errorf("%d sessions left: the failed sign-in's or the one it replaced", n)
	}
	if got := w.groups(alice); len(got) != 1 || got["moderators"] != "idp" {
		t.Errorf("memberships %v, want moderators unchanged", got)
	}
	if n := len(w.membershipAudit(alice)); n != audits {
		t.Errorf("%d audit rows written", n-audits)
	}
	if !strings.Contains(w.log.String(), "injected audit failure") {
		t.Errorf("the failure was not logged:\n%s", w.log.String())
	}
}

// AC4: a pending, suspended or deactivated user signing in gets no session
// and no membership change.
func TestGroupMapNeverSignsInAnInactiveUser(t *testing.T) {
	for _, status := range []string{domain.StatusPending, domain.StatusSuspended, domain.StatusDeactivated} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, mapsForumMods)
			fake := w.idp.Provider("fake")
			mapsForumMods(&fake)
			w.mapping(fake)
			alice := w.user("alice", "active", "user-1")
			w.groupsClaim([]string{"forum-mods"})
			old := mustSignIn(t, w, w.signIn(""), "alice")
			if err := w.store.Revoke(t.Context(), old.Value); err != nil {
				t.Fatal(err)
			}
			audits := len(w.membershipAudit(alice))

			w.setStatus(alice, status)
			w.groupsClaim([]string{})
			w.refused(w.signIn(""), http.StatusForbidden, "Account unavailable")
			if n := w.sessions(); n != 0 {
				t.Errorf("%d sessions", n)
			}
			if got := w.groups(alice); len(got) != 1 || got["moderators"] != "idp" {
				t.Errorf("memberships %v, want moderators unchanged", got)
			}
			if n := len(w.membershipAudit(alice)); n != audits {
				t.Errorf("%d audit rows written", n-audits)
			}
		})
	}
}

// A new member's registration maps their groups at once, as their first
// sign-in. The registration cookie carries only the values the map names.
func TestGroupMapAtRegistration(t *testing.T) {
	w := newWorld(t, mapsForumMods)
	fake := w.idp.Provider("fake")
	mapsForumMods(&fake)
	w.mapping(fake)
	w.open()
	many := []string{"forum-mods"}
	for i := range 400 {
		many = append(many, strings.Repeat("x", 30)+string(rune('a'+i%26))+strings.Repeat("y", i%7))
	}
	w.groupsClaim(many)
	reg := w.registering("")
	resp := w.submit("carol", reg)
	mustSignIn(t, w, resp, "carol")
	var carol int64
	if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(t.Context(), "carol")
		carol = u.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := w.groups(carol); len(got) != 1 || got["moderators"] != "idp" {
		t.Errorf("memberships %v, want moderators from the idp", got)
	}
	if got := w.membershipAudit(carol); !slices.Equal(got, []string{"group.member_added moderators fake"}) {
		t.Errorf("audit rows %q", got)
	}
}
