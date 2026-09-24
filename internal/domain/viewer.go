package domain

import "context"

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
	// PreUser reports a pre-user session: an approval-mode applicant signed
	// in with an identity and no user yet (docs/05). Such a viewer has
	// UserID 0, so Member is false however the rest is set; PreUser is what
	// lets the application screens tell them from a stranger.
	PreUser bool
	// IdentityID is the identity a pre-user session is bound to, 0 otherwise.
	IdentityID int64
}

// Anonymous is the viewer for a request with no session.
func Anonymous() Viewer { return Viewer{} }

// Member reports whether the viewer is a signed-in, active user: the
// implicit "members" group (docs/04).
func (v Viewer) Member() bool { return v.UserID != 0 && v.Active }

type viewerKey struct{}

// WithViewer returns ctx carrying v. The session middleware (TKT-01M29C5Y3E)
// sets it once per request; nothing else should.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

// ViewerFrom returns the viewer ctx carries, or Anonymous when it carries
// none. Defaulting to anonymous rather than failing is the safe direction: a
// request that somehow skipped session resolution gets the least access, not
// an error that a caller might handle by guessing.
func ViewerFrom(ctx context.Context) Viewer {
	if v, ok := ctx.Value(viewerKey{}).(Viewer); ok {
		return v
	}
	return Anonymous()
}
