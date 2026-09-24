package db

import (
	"database/sql"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// try runs fn in a write transaction and returns its error, for tests that
// expect a refusal.
func try(t *testing.T, d *DB, fn func(q *dbq.Queries, tx *sql.Tx) error) error {
	t.Helper()
	return d.Write(t.Context(), fn)
}

func newForum(t *testing.T, d *DB, slug string, parent int64) (int64, error) {
	t.Helper()
	var id int64
	err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateForum(t.Context(), dbq.CreateForumParams{
			ParentID: sql.NullInt64{Int64: parent, Valid: parent != 0},
			Slug:     slug, Name: slug, Visibility: domain.VisibilityMembers, CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	return id, err
}

func groupID(t *testing.T, d *DB, slug string) int64 {
	t.Helper()
	var id int64
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		g, err := q.GetGroupBySlug(t.Context(), slug)
		id = g.ID
		return err
	})
	if err != nil {
		t.Fatalf("group %s: %v", slug, err)
	}
	return id
}

// TestSystemGroupsSeededAndUndeletable is AC1: the migration seeds exactly the
// four system groups, and none of them can be deleted, renamed out of its
// slug, or stripped of its flag; nor can another group become one.
func TestSystemGroupsSeededAndUndeletable(t *testing.T) {
	d, _ := openTest(t)
	var system []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		groups, err := q.ListGroups(t.Context())
		for _, g := range groups {
			if g.IsSystem == 1 {
				system = append(system, g.Slug)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(system, domain.SystemGroups) {
		t.Fatalf("system groups = %v, want %v", system, domain.SystemGroups)
	}

	for _, slug := range domain.SystemGroups {
		id := groupID(t, d, slug)
		for what, stmt := range map[string]string{
			"delete":      "DELETE FROM groups WHERE id = ?",
			"rename slug": "UPDATE groups SET slug = slug || '-x' WHERE id = ?",
			"clear flag":  "UPDATE groups SET is_system = 0 WHERE id = ?",
			"change id":   "UPDATE groups SET id = id + 100 WHERE id = ?",
		} {
			err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), stmt, id)
				return err
			})
			if err == nil {
				t.Errorf("%s of system group %s succeeded", what, slug)
			}
		}
		// A system group can still be renamed for display.
		err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), "UPDATE groups SET name = name || '!' WHERE id = ?", id)
			return err
		})
		if err != nil {
			t.Errorf("renaming the display name of %s: %v", slug, err)
		}
	}

	// An ordinary group is created, cannot be promoted, and can be deleted.
	var gid int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		gid, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Editors", Slug: "editors", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "UPDATE groups SET is_system = 1 WHERE id = ?", gid)
		return err
	}); err == nil {
		t.Error("an ordinary group was promoted to a system group")
	}
	if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(),
			"INSERT INTO groups (name, slug, is_system, created_at, updated_at) VALUES ('X', 'x', 1, 1, 1)")
		return err
	}); err == nil {
		t.Error("a fifth system group was inserted")
	}
	if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "DELETE FROM groups WHERE id = ?", gid)
		return err
	}); err != nil {
		t.Errorf("deleting an ordinary group: %v", err)
	}
}

// TestForumNestingDepth is AC2: one level of nesting, refused at write time
// whichever way the second level is attempted.
func TestForumNestingDepth(t *testing.T) {
	d, _ := openTest(t)
	root, err := newForum(t, d, "root", 0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := newForum(t, d, "child", root)
	if err != nil {
		t.Fatalf("a child of a root forum: %v", err)
	}
	other, err := newForum(t, d, "other", 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := newForum(t, d, "grandchild", child); err == nil {
		t.Error("a grandchild forum was inserted")
	}

	setParent := func(id, parent int64) error {
		return try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			return q.SetForumParent(t.Context(), dbq.SetForumParentParams{
				ParentID: sql.NullInt64{Int64: parent, Valid: true}, UpdatedAt: 2, ID: id,
			})
		})
	}
	for _, c := range []struct {
		name       string
		id, parent int64
	}{
		{"moving a root under a child", other, child},
		{"moving a forum with children under another root", root, other},
		{"making a forum its own parent", other, other},
	} {
		if err := setParent(c.id, c.parent); err == nil {
			t.Errorf("%s succeeded", c.name)
		}
	}
	// Explicit ids bypass the rowid assignment, so the self-parent CHECK is
	// what refuses this one.
	if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO forums (id, parent_id, slug, name, created_at, updated_at)
			VALUES (99, 99, 'self', 'self', 1, 1)`)
		return err
	}); err == nil {
		t.Error("a forum was inserted as its own parent")
	}

	// Forum id 0 is reserved: the permissions uniqueness index uses it to
	// stand for a global grant.
	for _, id := range []int{0, -1} {
		if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO forums (id, slug, name, created_at, updated_at)
				VALUES (?, 'zero', 'zero', 1, 1)`, id)
			return err
		}); err == nil {
			t.Errorf("a forum with id %d was inserted", id)
		}
	}

	// Allowed moves still work: a childless root under another root, and back.
	if err := setParent(other, root); err != nil {
		t.Errorf("moving a childless root under a root: %v", err)
	}
	if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.SetForumParent(t.Context(), dbq.SetForumParentParams{UpdatedAt: 3, ID: other})
	}); err != nil {
		t.Errorf("making a child a root again: %v", err)
	}
}

// TestGroupMemberSource is AC3: a membership records whether an admin or an
// IdP claim mapping made it, and re-evaluating the IdP ones leaves the admin
// ones alone.
func TestGroupMemberSource(t *testing.T) {
	d, _ := openTest(t)
	admin := newUser(t, d, "boss")
	user := newUser(t, d, "drew")
	mods := groupID(t, d, domain.GroupModerators)
	var editors, readers int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		if editors, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Editors", Slug: "editors", CreatedAt: 1, UpdatedAt: 1}); err != nil {
			return err
		}
		readers, err = q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Readers", Slug: "readers", CreatedAt: 1, UpdatedAt: 1})
		return err
	})

	memberships := func() map[int64]string {
		got := map[int64]string{}
		err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			rows, err := q.GroupsForUser(t.Context(), user)
			for _, r := range rows {
				got[r.GroupID] = r.Source
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// An admin puts drew in moderators by hand; the IdP maps drew into
	// moderators and editors.
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		if err := q.AddAdminMembership(t.Context(), dbq.AddAdminMembershipParams{
			GroupID: mods, UserID: user, AddedBy: sql.NullInt64{Int64: admin, Valid: true}, AddedAt: 1,
		}); err != nil {
			return err
		}
		for _, g := range []int64{mods, editors} {
			if err := q.AddIdPMembership(t.Context(), dbq.AddIdPMembershipParams{GroupID: g, UserID: user, AddedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	if got := memberships(); got[mods] != domain.MembershipAdmin || got[editors] != domain.MembershipIdP {
		t.Fatalf("after first login: %v, want moderators=admin (the IdP did not overwrite it), editors=idp", got)
	}

	// Next login the claims say readers instead of editors.
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		if err := q.DeleteIdPMemberships(t.Context(), user); err != nil {
			return err
		}
		return q.AddIdPMembership(t.Context(), dbq.AddIdPMembershipParams{GroupID: readers, UserID: user, AddedAt: 2})
	})
	got := memberships()
	want := map[int64]string{mods: domain.MembershipAdmin, readers: domain.MembershipIdP}
	if len(got) != len(want) || got[mods] != want[mods] || got[readers] != want[readers] {
		t.Fatalf("after re-evaluation: %v, want %v", got, want)
	}

	// The IdP put drew in readers; an admin now adds drew to readers by hand.
	// The admin takes it over, so the next login cannot remove it.
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.AddAdminMembership(t.Context(), dbq.AddAdminMembershipParams{
			GroupID: readers, UserID: user, AddedBy: sql.NullInt64{Int64: admin, Valid: true}, AddedAt: 3,
		})
	})
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error { return q.DeleteIdPMemberships(t.Context(), user) })
	if got := memberships(); got[readers] != domain.MembershipAdmin || got[mods] != domain.MembershipAdmin {
		t.Fatalf("after an admin took over readers and the next login: %v, want readers and moderators both admin", got)
	}
	// Adding an existing admin membership again changes nothing and fails nothing.
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.AddAdminMembership(t.Context(), dbq.AddAdminMembershipParams{
			GroupID: mods, UserID: user, AddedBy: sql.NullInt64{Int64: admin, Valid: true}, AddedAt: 9,
		})
	})
	var addedAt int64
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT added_at FROM group_members WHERE group_id = ? AND user_id = ?", mods, user).Scan(&addedAt)
	}); err != nil {
		t.Fatal(err)
	}
	if addedAt != 1 {
		t.Errorf("re-adding an admin membership rewrote added_at to %d, want the original 1", addedAt)
	}

	for _, c := range []struct {
		name string
		stmt string
		args []any
	}{
		{"an unknown source", "INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (?, ?, 'manual', 1)", []any{editors, admin}},
		{"an idp row naming an adder", "INSERT INTO group_members (group_id, user_id, source, added_by, added_at) VALUES (?, ?, 'idp', ?, 1)", []any{editors, admin, admin}},
		{"a row for everyone", "INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (?, ?, 'admin', 1)", []any{groupID(t, d, domain.GroupEveryone), admin}},
		{"a row for members", "INSERT INTO group_members (group_id, user_id, source, added_at) VALUES (?, ?, 'admin', 1)", []any{groupID(t, d, domain.GroupMembers), admin}},
		{"moving a row into everyone", "UPDATE group_members SET group_id = ? WHERE user_id = ? AND group_id = ?", []any{groupID(t, d, domain.GroupEveryone), user, mods}},
		{"moving a row into members", "UPDATE group_members SET group_id = ? WHERE user_id = ? AND group_id = ?", []any{groupID(t, d, domain.GroupMembers), user, mods}},
	} {
		if err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), c.stmt, c.args...)
			return err
		}); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

// TestPermissionSetIsClosed is AC4: exactly the five permissions of docs/04,
// enforced by the schema, and the same five as domain.AllPermissions.
func TestPermissionSetIsClosed(t *testing.T) {
	d, _ := openTest(t)
	forum, err := newForum(t, d, "general", 0)
	if err != nil {
		t.Fatal(err)
	}
	mods := groupID(t, d, domain.GroupModerators)
	grant := func(p string, forumID int64) error {
		return try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
				ForumID: sql.NullInt64{Int64: forumID, Valid: forumID != 0}, GroupID: mods, Permission: p, CreatedAt: 1,
			})
		})
	}
	for _, p := range domain.AllPermissions {
		if err := grant(string(p), forum); err != nil {
			t.Errorf("granting %s: %v", p, err)
		}
	}
	for _, p := range []string{"write", "admin", "READ", ""} {
		if err := grant(p, forum); err == nil {
			t.Errorf("the permission %q was accepted", p)
		}
	}
	if err := grant(string(domain.PermRead), forum); err == nil {
		t.Error("a duplicate forum grant was accepted")
	}
	if err := grant(string(domain.PermUpload), 0); err != nil {
		t.Fatalf("a global grant: %v", err)
	}
	if err := grant(string(domain.PermUpload), 0); err == nil {
		t.Error("a duplicate global grant was accepted: NULL forum_ids compared distinct")
	}

	// The CHECK's list, read back from the schema, is domain's list.
	var ddl string
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name = 'permissions'").Scan(&ddl)
	}); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`permission IN \(([^)]*)\)`).FindStringSubmatch(ddl)
	if m == nil {
		t.Fatalf("no permission CHECK in %s", ddl)
	}
	var inSchema []string
	for _, s := range strings.Split(m[1], ",") {
		inSchema = append(inSchema, strings.Trim(strings.TrimSpace(s), "'"))
	}
	var inGo []string
	for _, p := range domain.AllPermissions {
		inGo = append(inGo, string(p))
	}
	if !slices.Equal(inSchema, inGo) {
		t.Fatalf("schema permissions %v, domain.AllPermissions %v", inSchema, inGo)
	}
}

// TestDefaultPermissionsSeeded: a fresh install grants members read,
// create_thread and reply everywhere, and everyone read, and nothing else
// (docs/04, decided 2026-09-24).
func TestDefaultPermissionsSeeded(t *testing.T) {
	d, _ := openTest(t)
	var got []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `
			SELECT g.slug || ':' || p.permission FROM permissions p JOIN groups g ON g.id = p.group_id
			WHERE p.forum_id IS NULL ORDER BY 1`)
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
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"everyone:read", "members:create_thread", "members:read", "members:reply"}
	if !slices.Equal(got, want) {
		t.Fatalf("seeded global grants %v, want %v", got, want)
	}
}

// TestPermissionRowsForAForum: the query permission evaluation will use
// returns the forum's own grants and the global ones, for the given groups
// only, and nothing from other forums.
func TestPermissionRowsForAForum(t *testing.T) {
	d, _ := openTest(t)
	general, _ := newForum(t, d, "general", 0)
	other, _ := newForum(t, d, "other", 0)
	mods := groupID(t, d, domain.GroupModerators)
	members := groupID(t, d, domain.GroupMembers)
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		for _, g := range []dbq.GrantPermissionParams{
			{ForumID: sql.NullInt64{Int64: general, Valid: true}, GroupID: mods, Permission: "moderate", CreatedAt: 1},
			{ForumID: sql.NullInt64{Int64: other, Valid: true}, GroupID: mods, Permission: "upload", CreatedAt: 1},
		} {
			if err := q.GrantPermission(t.Context(), g); err != nil {
				return err
			}
		}
		return nil
	})

	var got []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := q.PermissionRows(t.Context(), dbq.PermissionRowsParams{ForumID: general, GroupIds: []int64{members, mods}})
		for _, r := range rows {
			scope := "global"
			if r.ForumID.Valid {
				scope = "forum"
			}
			got = append(got, scope+":"+r.Permission)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"forum:moderate", "global:create_thread", "global:read", "global:reply"}
	if !slices.Equal(got, want) {
		t.Fatalf("PermissionRows = %v, want %v (no everyone grant, nothing from 'other')", got, want)
	}

	// RevokePermission removes a global row as well as a forum one.
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.RevokePermission(t.Context(), dbq.RevokePermissionParams{GroupID: members, Permission: "reply"})
	})
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := q.PermissionRows(t.Context(), dbq.PermissionRowsParams{ForumID: general, GroupIds: []int64{members}})
		for _, r := range rows {
			if r.Permission == "reply" {
				t.Error("the global reply grant survived RevokePermission")
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestListForumsOrder: roots come in position order, not id order, and each
// root is followed by its own children in their position order.
func TestListForumsOrder(t *testing.T) {
	d, _ := openTest(t)
	mk := func(slug string, parent, position int64) int64 {
		var id int64
		write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			var err error
			id, err = q.CreateForum(t.Context(), dbq.CreateForumParams{
				ParentID: sql.NullInt64{Int64: parent, Valid: parent != 0},
				Slug:     slug, Name: slug, Position: position, Visibility: domain.VisibilityMembers, CreatedAt: 1, UpdatedAt: 1,
			})
			return err
		})
		return id
	}
	// Created in an order that disagrees with position at every level.
	late := mk("late", 0, 2)
	early := mk("early", 0, 1)
	mk("late-b", late, 2)
	mk("late-a", late, 1)
	mk("early-a", early, 1)

	var got []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		forums, err := q.ListForums(t.Context())
		for _, f := range forums {
			got = append(got, f.Slug)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"early", "early-a", "late", "late-a", "late-b"}
	if !slices.Equal(got, want) {
		t.Fatalf("ListForums = %v, want %v", got, want)
	}
}
