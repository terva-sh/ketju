package domain

// Permission is one of the five things a group can be granted, globally or on
// one forum (docs/04, "permissions").
type Permission string

const (
	PermRead         Permission = "read"
	PermCreateThread Permission = "create_thread"
	PermReply        Permission = "reply"
	PermUpload       Permission = "upload"
	PermModerate     Permission = "moderate"
)

// AllPermissions is the closed set. The permissions table's CHECK constraint
// lists the same five, and a test in internal/store/db holds the two together.
var AllPermissions = []Permission{PermRead, PermCreateThread, PermReply, PermUpload, PermModerate}

// The system groups, seeded by migration with these slugs and never deleted.
// Membership of GroupEveryone (every viewer, signed in or not) and of
// GroupMembers (every active user) is implicit and never stored as rows.
const (
	GroupEveryone   = "everyone"
	GroupMembers    = "members"
	GroupModerators = "moderators"
	GroupAdmins     = "admins"
)

// SystemGroups lists the four in their seeded order.
var SystemGroups = []string{GroupEveryone, GroupMembers, GroupModerators, GroupAdmins}

// Forum visibility values (docs/04, "forums").
const (
	VisibilityPublic     = "public"
	VisibilityMembers    = "members"
	VisibilityRestricted = "restricted"
)

// Where a group membership came from (docs/05, "Roles and groups"). An IdP
// membership is re-evaluated at every login; an admin one is never touched by
// that re-evaluation.
const (
	MembershipAdmin = "admin"
	MembershipIdP   = "idp"
)
