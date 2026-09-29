package dto

// The admin users, groups and permissions screens (TKT-01M29CXK1X, docs/07
// "Moderation and admin"). Every response here is staff-only: the list and
// a user's detail to moderators and admins, the rest to admins. A field a
// moderator may not see is null for them rather than absent.

// AdminUser is one account as the users screen lists it. Status is here
// because every viewer of this screen is staff (docs/05 "Account
// lifecycle": status is for moderators and admins only).
type AdminUser struct {
	ID   int64  `json:"id"`
	User Author `json:"user"`
	// Status is active, pending, suspended or deactivated.
	Status    string `json:"status"`
	CreatedAt Millis `json:"created_at"`
	// LastSeenAt is null for an account that has never been seen.
	LastSeenAt *Millis `json:"last_seen_at"`
}

// AdminUserList is GET /admin/users: one page, in handle order.
type AdminUserList struct {
	Users []AdminUser `json:"users"`
	// NextCursor is empty at the end of the list.
	NextCursor string `json:"next_cursor"`
}

// AdminUserDetail is GET /admin/users/{handle}.
type AdminUserDetail struct {
	User AdminUser `json:"user"`
	// Groups are the account's stored memberships. everyone and members
	// are implicit and never listed.
	Groups []Membership `json:"groups"`
	// Identities are the provider accounts that sign this user in: admins
	// only, and null for a moderator.
	Identities []AdminIdentity `json:"identities"`
	// Sessions is how many live member sessions the account has: admins
	// only, and null for a moderator.
	Sessions *int `json:"sessions"`
}

// GroupRef names a group.
type GroupRef struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Membership is one of a user's groups.
type Membership struct {
	Group GroupRef `json:"group"`
	// Source is admin or idp. An idp membership came from the identity
	// provider's group mapping, is re-evaluated at every sign-in, and
	// cannot be changed by hand (docs/05 "Roles and groups").
	Source string `json:"source"`
	// AddedBy is the admin who added it, null for an idp membership or one
	// no account made.
	AddedBy *Author `json:"added_by"`
	AddedAt Millis  `json:"added_at"`
}

// AdminIdentity is one identity that signs a user in.
type AdminIdentity struct {
	// Provider is the configured key of the identity provider.
	Provider string `json:"provider"`
	// Email is what the provider asserted, null when it asserted none.
	Email         *string `json:"email"`
	EmailVerified bool    `json:"email_verified"`
	// LastLoginAt is null for an identity never used to sign in.
	LastLoginAt *Millis `json:"last_login_at"`
}

// Group is one group, as the groups tab and the permission grid list it.
type Group struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// System marks the four seeded groups, which are never deleted and keep
	// their slug. everyone and members have no stored members.
	System bool `json:"system"`
	// Members and Grants count its stored memberships and its permission
	// rows. A group can be deleted only when both are zero.
	Members int `json:"members"`
	Grants  int `json:"grants"`
}

// GroupList is GET /admin/groups, in id order.
type GroupList struct {
	Groups []Group `json:"groups"`
}

// GroupDetail is GET /admin/groups/{id}, and the answer to a change of its
// members.
type GroupDetail struct {
	Group   Group         `json:"group"`
	Members []GroupMember `json:"members"`
}

// GroupMember is one stored member of a group, in handle order.
type GroupMember struct {
	User AdminUser `json:"user"`
	// Source is admin or idp, as Membership's.
	Source  string  `json:"source"`
	AddedBy *Author `json:"added_by"`
	AddedAt Millis  `json:"added_at"`
}

// AdminForum is a forum as the permission grid names it.
type AdminForum struct {
	ID         int64  `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

// PermissionGrid is GET /admin/permissions: the groups, the forums in the
// forum index's order, and every permission row.
type PermissionGrid struct {
	Groups []Group      `json:"groups"`
	Forums []AdminForum `json:"forums"`
	Grants []Grant      `json:"grants"`
}

// Grant is one permission row: Permission given to GroupID in ForumID, or
// site-wide when ForumID is null.
type Grant struct {
	ForumID    *int64 `json:"forum_id"`
	GroupID    int64  `json:"group_id"`
	Permission string `json:"permission"`
}

// PermissionPreview is POST /admin/permissions/preview: what one account
// could do in each forum now, and with the staged changes applied.
type PermissionPreview struct {
	User   AdminUser     `json:"user"`
	Forums []ForumEffect `json:"forums"`
}

// ForumEffect is one forum's decisions for the previewed account.
type ForumEffect struct {
	Forum  AdminForum `json:"forum"`
	Before Decisions  `json:"before"`
	After  Decisions  `json:"after"`
}

// Decisions is one decision per permission: allow, deny (the forum is
// visible and this is refused), or hidden (the forum is invisible).
type Decisions struct {
	Read         string `json:"read"`
	CreateThread string `json:"create_thread"`
	Reply        string `json:"reply"`
	Upload       string `json:"upload"`
	Moderate     string `json:"moderate"`
}
