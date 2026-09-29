package dto

// The admin users, groups and permissions screens' cases (TKT-01M29CXK1X).
// They are registered here, into the same corpus and under the same rules as
// fixtures_test.go's, so that file is not edited by every admin section.

func init() {
	corpus["admin-users."+tAdmin] = adminUserList()
	corpus["admin-user."+tAdmin] = adminUserDetail(tAdmin)
	corpus["admin-user."+tModerator] = adminUserDetail(tModerator)
	corpus["admin-groups."+tAdmin] = GroupList{Groups: []Group{moderatorsGroup, bookClub}}
	corpus["admin-group."+tAdmin] = bookClubDetail()
	corpus["permissions."+tAdmin] = permissionGrid()
	corpus["permission-preview."+tAdmin] = permissionPreview()
}

var (
	adminAlice = AdminUser{ID: 12, User: alice, Status: "active", CreatedAt: 1757000000000,
		LastSeenAt: ptr(Millis(1757600000000))}
	adminScribe = AdminUser{ID: 31, User: scribe, Status: "suspended", CreatedAt: 1757100000000}
	adminBob    = AdminUser{ID: 4, User: bob, Status: "active", CreatedAt: 1756900000000,
		LastSeenAt: ptr(Millis(1757650000000))}

	moderatorsGroup = Group{ID: 3, Slug: "moderators", Name: "Moderators",
		Description: "Moderate the forums they are granted.", System: true, Members: 1, Grants: 1}
	bookClub = Group{ID: 7, Slug: "book-club", Name: "Book club", Description: "Reads the book forum.",
		Members: 2, Grants: 2}

	generalForum = AdminForum{ID: 1, Slug: "general", Name: "General", Visibility: "public"}
	booksForum   = AdminForum{ID: 5, Slug: "books", Name: "Books", Visibility: "restricted"}
)

// adminUserList is a page of the users list: an active member seen
// recently, and a suspended agent never seen, with more to come.
func adminUserList() AdminUserList {
	return AdminUserList{Users: []AdminUser{adminAlice, adminScribe}, NextCursor: "c2NyaWJl"}
}

// adminUserDetail is alice as an admin sees her, with her identities and
// sessions, and as a moderator sees her, without either. One membership an
// admin added, one the identity provider's mapping made.
func adminUserDetail(class string) AdminUserDetail {
	d := AdminUserDetail{
		User: adminAlice,
		Groups: []Membership{
			{Group: GroupRef{ID: 7, Slug: "book-club", Name: "Book club"}, Source: "admin", AddedBy: &bob,
				AddedAt: 1757200000000},
			{Group: GroupRef{ID: 3, Slug: "moderators", Name: "Moderators"}, Source: "idp", AddedAt: 1757300000000},
		},
	}
	if class == tAdmin {
		d.Identities = []AdminIdentity{
			{Provider: "github", Email: ptr("alice@example.org"), EmailVerified: true,
				LastLoginAt: ptr(Millis(1757600000000))},
			{Provider: "club", Email: nil, EmailVerified: false, LastLoginAt: nil},
		}
		d.Sessions = ptr(2)
	}
	return d
}

func bookClubDetail() GroupDetail {
	return GroupDetail{Group: bookClub, Members: []GroupMember{
		{User: adminAlice, Source: "admin", AddedBy: &bob, AddedAt: 1757200000000},
		{User: adminScribe, Source: "idp", AddedAt: 1757300000000},
	}}
}

// permissionGrid has a per-forum row and a site-wide one.
func permissionGrid() PermissionGrid {
	return PermissionGrid{
		Groups: []Group{moderatorsGroup, bookClub},
		Forums: []AdminForum{generalForum, booksForum},
		Grants: []Grant{
			{ForumID: nil, GroupID: 7, Permission: "reply"},
			{ForumID: ptr(int64(5)), GroupID: 7, Permission: "read"},
			{ForumID: ptr(int64(1)), GroupID: 3, Permission: "moderate"},
		},
	}
}

// permissionPreview is bob, a moderator, before and after a batch that
// takes the book club's read on the restricted forum away: the forum goes
// from readable to hidden.
func permissionPreview() PermissionPreview {
	return PermissionPreview{User: adminBob, Forums: []ForumEffect{
		{Forum: generalForum,
			Before: Decisions{Read: "allow", CreateThread: "allow", Reply: "allow", Upload: "deny", Moderate: "allow"},
			After:  Decisions{Read: "allow", CreateThread: "allow", Reply: "allow", Upload: "deny", Moderate: "allow"}},
		{Forum: booksForum,
			Before: Decisions{Read: "allow", CreateThread: "deny", Reply: "allow", Upload: "deny", Moderate: "allow"},
			After:  Decisions{Read: "hidden", CreateThread: "hidden", Reply: "hidden", Upload: "hidden", Moderate: "hidden"}},
	}}
}
