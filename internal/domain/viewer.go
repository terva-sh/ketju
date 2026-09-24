package domain

// User roles (docs/04 "users", docs/05 "Roles and groups").
const (
	RoleMember    = "member"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
)

// Viewer is whoever is making a request, as the session middleware resolved
// it (TKT-01M29C5Y3E). The zero value is an anonymous viewer.
type Viewer struct {
	// UserID is 0 for an anonymous viewer.
	UserID int64
	// Role is the user's role. It is ignored for an anonymous or inactive
	// viewer.
	Role string
	// Active reports that the user's status is 'active'. A signed-in viewer
	// who is not active is treated as anonymous: no member rights and no
	// role, whatever their role column says.
	Active bool
	// SessionKey identifies the session, for per-session caches. It is
	// opaque and never the session id itself. Empty for anonymous viewers,
	// who share one cache entry.
	SessionKey string
}

// Anonymous is the viewer for a request with no session.
func Anonymous() Viewer { return Viewer{} }

// Member reports whether the viewer is a signed-in, active user: the
// implicit "members" group (docs/04).
func (v Viewer) Member() bool { return v.UserID != 0 && v.Active }
