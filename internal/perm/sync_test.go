package perm

import (
	"database/sql"
	"errors"
	"github.com/terva-sh/ketju/internal/audit"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// slug is the slug of group id.
func (f *fixture) slug(id int64) string {
	f.t.Helper()
	var s string
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(f.t.Context(), "SELECT slug FROM groups WHERE id = ?", id).Scan(&s)
	}); err != nil {
		f.t.Fatal(err)
	}
	return s
}

// memberships is userID's memberships, as slug to source.
func (f *fixture) memberships(userID int64) map[string]string {
	f.t.Helper()
	got := map[string]string{}
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT g.slug, gm.source FROM group_members gm
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
		f.t.Fatal(err)
	}
	return got
}

// audited is the IdP mapping's membership audit rows about userID, in
// order, each as "action slug provider via". An admin's membership rows
// (source admin) are TestMembershipWritesAudited's.
func (f *fixture) audited(userID int64) []string {
	f.t.Helper()
	var got []string
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT action, details_json ->> 'group',
			details_json ->> 'provider', details_json ->> 'source', details_json ->> 'via',
			actor_id IS NULL, ip
			FROM audit_log WHERE target_type = 'user' AND target_id = CAST(? AS TEXT)
			AND action LIKE 'group.member_%' AND details_json ->> 'source' = 'idp' ORDER BY id`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var action, group, provider, source, via, ip string
			var noActor bool
			if err := rows.Scan(&action, &group, &provider, &source, &via, &noActor, &ip); err != nil {
				return err
			}
			if source != domain.MembershipIdP || via != ViaGroupMap || !noActor {
				f.t.Errorf("audit row %s %s: source %q, via %q, no actor %v", action, group, source, via, noActor)
			}
			got = append(got, strings.Join([]string{action, group, provider, ip}, " "))
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fixture) sync(userID int64, s IdPSync) Changes {
	f.t.Helper()
	ch, err := f.ev.SyncIdPGroups(f.t.Context(), userID, s)
	if err != nil {
		f.t.Fatal(err)
	}
	return ch
}

// A user with idp rows {a, b} and an admin row {c}, synced with {b, c, d},
// ends with idp {b, d} and c still admin's, and audit_log records exactly
// the removal of a and the addition of d. The same sync again writes
// nothing.
func TestSyncIdPGroupsAddsAndRemoves(t *testing.T) {
	f := newFixture(t)
	a, b, c, d := f.newGroup(), f.newGroup(), f.newGroup(), f.newGroup()
	sa, sb, sc, sd := f.slug(a), f.slug(b), f.slug(c), f.slug(d)
	v := f.viewer(domain.RoleMember)
	f.sync(v.UserID, IdPSync{Provider: "club", IP: "192.0.2.1", Slugs: []string{sa, sb}})
	if err := f.ev.AddMember(t.Context(), audit.By{}, c, v.UserID); err != nil {
		t.Fatal(err)
	}
	before := len(f.audited(v.UserID))

	ch := f.sync(v.UserID, IdPSync{Provider: "club", IP: "192.0.2.2", Slugs: []string{sb, sc, sd}})
	if !slices.Equal(ch.Added, []string{sd}) || !slices.Equal(ch.Removed, []string{sa}) || len(ch.Unknown) != 0 {
		t.Errorf("changes %+v, want added [%s], removed [%s]", ch, sd, sa)
	}
	got := f.memberships(v.UserID)
	want := map[string]string{sb: "idp", sc: "admin", sd: "idp"}
	if len(got) != len(want) || got[sb] != want[sb] || got[sc] != want[sc] || got[sd] != want[sd] {
		t.Errorf("memberships %v, want %v", got, want)
	}
	rows := f.audited(v.UserID)[before:]
	wantRows := []string{
		"group.member_removed " + sa + " club 192.0.2.2",
		"group.member_added " + sd + " club 192.0.2.2",
	}
	if !slices.Equal(rows, wantRows) {
		t.Errorf("audit rows %q, want %q", rows, wantRows)
	}

	n := len(f.audited(v.UserID))
	if ch := f.sync(v.UserID, IdPSync{Provider: "club", Slugs: []string{sd, sc, sb, sb}}); len(ch.Added)+len(ch.Removed) != 0 {
		t.Errorf("a sync that changes nothing reported %+v", ch)
	}
	if m := len(f.audited(v.UserID)); m != n {
		t.Errorf("a sync that changes nothing wrote %d audit rows", m-n)
	}

	// Nothing asserted: every idp row goes, and the admin's stays.
	ch = f.sync(v.UserID, IdPSync{Provider: "other"})
	if !slices.Equal(ch.Removed, []string{sb, sd}) {
		t.Errorf("an empty sync removed %v, want [%s %s]", ch.Removed, sb, sd)
	}
	if got := f.memberships(v.UserID); len(got) != 1 || got[sc] != "admin" {
		t.Errorf("after an empty sync: %v, want only %s from the admin", got, sc)
	}
}

// An audit row that cannot be written fails the whole sync: no membership
// is added or removed, and the cache is kept, since nothing changed.
func TestSyncIdPGroupsIsOneTransaction(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	a, b := f.newGroup(), f.newGroup()
	v := f.viewer(domain.RoleMember)
	f.sync(v.UserID, IdPSync{Provider: "club", Slugs: []string{f.slug(a)}})
	before := f.memberships(v.UserID)
	audits := len(f.audited(v.UserID))

	f.exec(`CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
	f.check(v, forum, domain.PermRead) // fills the cache
	_, err := f.ev.SyncIdPGroups(t.Context(), v.UserID, IdPSync{Provider: "club", Slugs: []string{f.slug(b)}})
	if err == nil || !strings.Contains(err.Error(), "injected audit failure") {
		t.Fatalf("the sync answered %v, want the injected failure", err)
	}
	if got := f.memberships(v.UserID); len(got) != len(before) || got[f.slug(a)] != "idp" {
		t.Errorf("after a failed sync: %v, want %v; the diff was half applied", got, before)
	}
	if n := len(f.audited(v.UserID)); n != audits {
		t.Errorf("a failed sync left %d audit rows", n-audits)
	}
	if cached(f.ev) == 0 {
		t.Error("a failed sync cleared the cache")
	}
}

// A claim never creates a group, and never a row for the implicit groups:
// an unknown slug is reported and skipped, everyone and members are
// skipped.
func TestSyncIdPGroupsIgnoresUnknownAndImplicit(t *testing.T) {
	f := newFixture(t)
	v := f.viewer(domain.RoleMember)
	var groups int
	count := func() int {
		var n int
		if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), "SELECT count(*) FROM groups").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	groups = count()
	ch := f.sync(v.UserID, IdPSync{Provider: "club",
		Slugs: []string{"nope", domain.GroupEveryone, domain.GroupMembers, "nope"}})
	if !slices.Equal(ch.Unknown, []string{"nope"}) || len(ch.Added) != 0 {
		t.Errorf("changes %+v, want only nope unknown", ch)
	}
	if got := f.memberships(v.UserID); len(got) != 0 {
		t.Errorf("memberships %v, want none", got)
	}
	if n := count(); n != groups {
		t.Errorf("the sync created %d groups", n-groups)
	}
	if rows := f.audited(v.UserID); len(rows) != 0 {
		t.Errorf("audit rows %q, want none", rows)
	}
}

// The next check after a sync that adds moderators, whose group moderates
// the forum, sees the grant with no time passing.
func TestSyncIdPGroupsInvalidates(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	mods := f.group("moderators")
	f.grant(forum, mods, domain.PermModerate)
	v := f.viewer(domain.RoleMember)
	if d := f.check(v, forum, domain.PermModerate); d != Deny {
		t.Fatalf("before the sync: %v", d)
	}
	f.sync(v.UserID, IdPSync{Provider: "club", Slugs: []string{"moderators"}})
	if d := f.check(v, forum, domain.PermModerate); d != Allow {
		t.Errorf("after the sync: %v, want allow on the next check", d)
	}
	f.sync(v.UserID, IdPSync{Provider: "other"})
	if d := f.check(v, forum, domain.PermModerate); d != Deny {
		t.Errorf("after an empty sync: %v, want deny on the next check", d)
	}
}

// A user who is not active gets no membership change and no audit row.
func TestSyncIdPGroupsRefusesAnInactiveUser(t *testing.T) {
	for _, status := range []string{domain.StatusPending, domain.StatusSuspended, domain.StatusDeactivated} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			a, b := f.newGroup(), f.newGroup()
			v := f.viewer(domain.RoleMember)
			f.sync(v.UserID, IdPSync{Provider: "club", Slugs: []string{f.slug(a)}})
			f.exec("UPDATE users SET status = ? WHERE id = ?", status, v.UserID)
			audits := len(f.audited(v.UserID))
			_, err := f.ev.SyncIdPGroups(t.Context(), v.UserID, IdPSync{Provider: "club", Slugs: []string{f.slug(b)}})
			if !errors.Is(err, ErrNotActive) {
				t.Fatalf("answered %v, want ErrNotActive", err)
			}
			if got := f.memberships(v.UserID); len(got) != 1 || got[f.slug(a)] != "idp" {
				t.Errorf("memberships %v, want only %s", got, f.slug(a))
			}
			if n := len(f.audited(v.UserID)); n != audits {
				t.Errorf("%d audit rows written", n-audits)
			}
		})
	}
	f := newFixture(t)
	if _, err := f.ev.SyncIdPGroups(t.Context(), 9999, IdPSync{}); !errors.Is(err, ErrNoUser) {
		t.Errorf("a user id naming nobody: %v, want ErrNoUser", err)
	}
}
