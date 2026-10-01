// Package session is the server-side session store (docs/05 "Sessions and
// CSRF"): sessions are rows, the cookie carries a random id, and the table
// holds only that id's hash.
//
// It is hand-rolled rather than a library for the reasons docs/05 gives
// (TKT-01M29BSPEF): exact control over the hash at rest, revocation that is a
// DELETE, and a session with no user yet, which approval mode needs.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/sqltypes"
)

// CookieName is the session cookie. The __Host- prefix makes the browser
// refuse it unless it is Secure, has Path=/ and names no Domain, so no
// subdomain can set or shadow it.
const CookieName = "__Host-ketju_session"

// The expiry rules (docs/05). A session lives IdleExpiry past its last use
// and never past HardExpiry from its creation; last_used_at moves at most
// once per TouchInterval, so an active reader costs a write every five
// minutes rather than one per request.
const (
	IdleExpiry    = 30 * 24 * time.Hour
	HardExpiry    = 90 * 24 * time.Hour
	TouchInterval = 5 * time.Minute
)

// Kind is what a session is for.
type Kind string

const (
	// KindUser is a signed-in user.
	KindUser Kind = "user"
	// KindPreUser is an approval-mode applicant: an identity, no user.
	KindPreUser Kind = "pre_user"
)

// ErrNoSession means the token names no live session: it never existed, it
// expired, or it was revoked. The three are deliberately indistinguishable.
var ErrNoSession = errors.New("session: no live session")

// tokenBytes is the size of the random id: 256 bits, so a hash of it needs
// no salt and no slow KDF. Nobody can enumerate the space to invert it.
const tokenBytes = 32

// Cutoff is the expiry rule as two bounds. A row is live when both its
// expires_at is after ExpiresAt and its last_used_at is after LastUsedAt, in
// Unix milliseconds; anything else is expired.
type Cutoff struct {
	ExpiresAt  int64
	LastUsedAt int64
}

// ExpiredBefore is the expiry rule at now. Lookup binds it, and the sweeper
// (TKT-01M29C5Y2S) deletes by it, so what is expired is decided once.
func ExpiredBefore(now time.Time) Cutoff {
	return Cutoff{ExpiresAt: now.UnixMilli(), LastUsedAt: now.Add(-IdleExpiry).UnixMilli()}
}

// Live reports whether a row with these times is alive at now, by the same
// rule as the SQL.
func (c Cutoff) Live(expiresAt, lastUsedAt int64) bool {
	return expiresAt > c.ExpiresAt && lastUsedAt > c.LastUsedAt
}

// User is the signed-in user as the session lookup reads them: what GET /me
// and the viewer need, from the one query.
type User struct {
	ID            int64
	Handle        string
	DisplayName   string
	AvatarBlob    string
	Status        string
	Role          string
	Kind          string
	Timezone      string
	Locale        string
	PrefsJSON     string
	MarkAllReadAt int64 // 0 when never used
}

// Session is a resolved, live session.
type Session struct {
	hash       sqltypes.SessionHash
	Kind       Kind
	IdentityID int64
	// Provider is the identity provider the session signed in through, when
	// it was an OIDC sign-in.
	Provider   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	// User is nil for a pre-user session.
	User *User
}

// Viewer is the session as the rest of the server sees it. A user whose
// status is not active keeps their session row until something deletes it,
// but is not Active, so they have no member rights on the very next request.
//
// Nor do they carry their role. domain.Viewer says a role is ignored for an
// inactive viewer, and perm honours that, but a viewer that says "admin"
// for a suspended admin is one careless check away from authorising them;
// one that says nothing cannot be misread.
func (s Session) Viewer() domain.Viewer {
	v := domain.Viewer{SessionKey: key(s.hash), IdentityID: s.IdentityID, PreUser: s.Kind == KindPreUser}
	if s.Kind == KindUser && s.User != nil {
		v.UserID = s.User.ID
		v.Active = s.User.Status == "active"
		if v.Active {
			v.Role = s.User.Role
		}
	}
	return v
}

// key is the viewer's SessionKey: a hash of the stored hash, so a cache key
// that ends up in a log or a heap dump is neither the cookie nor the row.
func key(h sqltypes.SessionHash) string {
	sum := sha256.Sum256(h[:])
	return hex.EncodeToString(sum[:12])
}

// Tag names the session to the browser that holds it, so the client can
// tell one applicant's session from another's where /me says nothing else
// that differs (TKT-01M3QVWKAG). It is a hash of the stored hash under its
// own prefix, so it is neither the cookie, the row, nor the SessionKey a
// cache is keyed by, and nothing can be signed in with it.
func (s Session) Tag() string {
	sum := sha256.Sum256(append([]byte("ketju session tag\x00"), s.hash[:]...))
	return hex.EncodeToString(sum[:12])
}

// ID names the session in its member's session list, so "sign out this
// device" can say which (TKT-01M29CCC0X). It is a hash of the stored hash
// under its own prefix, so it is neither the cookie, the row, the
// SessionKey a cache is keyed by, nor Tag, and nothing can be signed in
// with it. Info.ID is the same derivation, so a handler can tell which
// listed row is the request's own.
func (s Session) ID() string { return publicID(s.hash) }

func publicID(h sqltypes.SessionHash) string {
	sum := sha256.Sum256(append([]byte("ketju session id\x00"), h[:]...))
	return hex.EncodeToString(sum[:16])
}

// Store reads and writes sessions.
type Store struct {
	DB *db.DB
	// Now is the clock; nil means time.Now. Tests move it.
	Now func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// hashToken turns a cookie value into the stored hash, or reports that it
// cannot be a session id at all. Only a value that decodes to exactly
// tokenBytes can be, so anything else is refused without a query.
func hashToken(token string) (sqltypes.SessionHash, bool) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return sqltypes.SessionHash{}, false
	}
	return sha256.Sum256(raw), true
}

// Lookup resolves a cookie value to its live session, with the user, in one
// query. It returns ErrNoSession for any token that is not a live session.
func (s *Store) Lookup(ctx context.Context, token string) (Session, error) {
	h, ok := hashToken(token)
	if !ok {
		return Session{}, ErrNoSession
	}
	return s.lookup(ctx, h)
}

// Refresh reads sess again, by the hash it already holds and under Lookup's
// expiry rule, and returns it as it stands now: the user's status and role
// as the row has them, or ErrNoSession once the session has been revoked,
// swept, or has expired. It touches nothing and creates nothing.
//
// A request that outlives the moment it resolved its session, as a
// live-update stream does, calls it before acting on the viewer again, so
// a sign-out, a revocation or a suspension ends what the session was
// holding open (docs/02 "Live updates", TKT-01M29CRJ3X). The cookie is not
// needed, and so is never kept.
func (s *Store) Refresh(ctx context.Context, sess Session) (Session, error) {
	return s.lookup(ctx, sess.hash)
}

// lookup resolves a stored hash to its live session.
func (s *Store) lookup(ctx context.Context, h sqltypes.SessionHash) (Session, error) {
	c := ExpiredBefore(s.now())
	var row dbq.LookupSessionRow
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.LookupSession(ctx, dbq.LookupSessionParams{IDHash: h,
			ExpiresAfter: c.ExpiresAt, LastUsedAfter: c.LastUsedAt})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("session: lookup: %w", err)
	}
	out := Session{hash: row.IDHash, Kind: Kind(row.Kind), IdentityID: row.IdentityID.Int64, Provider: row.OidcProviderKey.String,
		CreatedAt: time.UnixMilli(row.CreatedAt), ExpiresAt: time.UnixMilli(row.ExpiresAt),
		LastUsedAt: time.UnixMilli(row.LastUsedAt)}
	if row.UserID.Valid {
		out.User = &User{ID: row.UserID.Int64, Handle: row.Handle.String, DisplayName: row.DisplayName.String,
			AvatarBlob: row.AvatarBlob.String, Status: row.Status.String, Role: row.Role.String,
			Kind: row.UserKind.String, Timezone: row.Timezone.String, Locale: row.Locale.String,
			PrefsJSON: row.PrefsJson.String, MarkAllReadAt: row.MarkAllReadAt.Int64}
	}
	return out, nil
}

// HeldBy reports, through q inside the caller's transaction, whether token
// is a live member session of userID at now, by Lookup's expiry rule. It
// returns ErrNoSession when it is not: no such session, expired, a pre-user
// one, or another user's.
//
// A write that may happen only while the browser is still signed in as that
// member checks it in the transaction that writes, not from the viewer the
// request resolved at its start: a sign-out or revocation landing in
// between is then either before the check, and refuses the write, or after
// the commit. Account linking is the one such write (TKT-01M29CKRES). It
// reads, and creates nothing.
func HeldBy(ctx context.Context, q *dbq.Queries, token string, userID int64, now time.Time) error {
	_, err := HeldThrough(ctx, q, token, userID, now)
	return err
}

// HeldThrough is HeldBy, and also says which identity the session signed
// in through, 0 for none. Removing an identity reads it (TKT-01M29CCC0X):
// the removal ends every session through the identity, and the browser
// that asked is told whether its own was one of them.
func HeldThrough(ctx context.Context, q *dbq.Queries, token string, userID int64, now time.Time) (int64, error) {
	h, ok := hashToken(token)
	if !ok {
		return 0, ErrNoSession
	}
	c := ExpiredBefore(now)
	row, err := q.LookupSession(ctx, dbq.LookupSessionParams{IDHash: h, ExpiresAfter: c.ExpiresAt, LastUsedAfter: c.LastUsedAt})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, ErrNoSession
	case err != nil:
		return 0, fmt.Errorf("session: lookup: %w", err)
	case Kind(row.Kind) != KindUser || !row.UserID.Valid || row.UserID.Int64 != userID:
		return 0, ErrNoSession
	}
	return row.IdentityID.Int64, nil
}

// EndThrough deletes, through q inside the caller's transaction, every
// session signed in through identityID, of either kind, and returns how many
// it deleted. Removing an identity calls it in the transaction that deletes
// the identity (TKT-01M29CCC0X), so the sessions it opened close with it,
// and the foreign key from sessions to identities holds.
func EndThrough(ctx context.Context, q *dbq.Queries, identityID int64) (int64, error) {
	n, err := q.DeleteSessionsThroughIdentity(ctx, sql.NullInt64{Int64: identityID, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("session: ending the sessions through identity %d: %w", identityID, err)
	}
	return n, nil
}

// touchDue reports whether a session last used at t may be touched at now.
func touchDue(t, now time.Time) bool { return !t.After(now.Add(-TouchInterval)) }

// Touch slides the session's last use to now if it is due, and reports
// whether it wrote. The statement repeats the check, so concurrent requests
// that both saw a due value write once between them, and it rechecks expiry
// at now: a session looked up a moment before it went idle must not be
// revived by the touch that follows.
func (s *Store) Touch(ctx context.Context, sess Session) (bool, error) {
	now := s.now()
	if !touchDue(sess.LastUsedAt, now) {
		return false, nil
	}
	var n int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		c := ExpiredBefore(now)
		n, err = q.TouchSession(ctx, dbq.TouchSessionParams{Now: now.UnixMilli(), IDHash: sess.hash,
			DueBefore: now.Add(-TouchInterval).UnixMilli(), ExpiresAfter: c.ExpiresAt, LastUsedAfter: c.LastUsedAt})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("session: touch: %w", err)
	}
	return n > 0, nil
}

// Revoke ends the session the token names, if any: sign out this device.
func (s *Store) Revoke(ctx context.Context, token string) error {
	h, ok := hashToken(token)
	if !ok {
		return nil
	}
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error { return q.DeleteSession(ctx, h) })
}

// RevokeUser ends every session a user has: sign out everywhere
// (TKT-01M29CCC0X), the session asking included. It takes effect on each
// device's next request, because every request looks its session up. A
// pre-user session still bound to one of the user's identities from before
// they had an account goes too, in the same transaction, as perm's status
// changes delete them: it no longer resolves, and nothing should be left
// that names the account's identities.
func (s *Store) RevokeUser(ctx context.Context, userID int64) error {
	id := sql.NullInt64{Int64: userID, Valid: true}
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := q.DeleteUserSessions(ctx, id); err != nil {
			return err
		}
		return q.DeleteIdentitySessions(ctx, id)
	})
}

// RevokeOwn ends the session of userID's that the session list names id
// (Info.ID): sign out that device (TKT-01M29CCC0X). It reports false, and
// deletes nothing, when no live session of the user's has that id, so an id
// of another member's session, an expired one, and one that never existed
// all answer the same. The match and the delete share one transaction, and
// the delete names the user as well as the row.
func (s *Store) RevokeOwn(ctx context.Context, userID int64, id string) (bool, error) {
	c := ExpiredBefore(s.now())
	user := sql.NullInt64{Int64: userID, Valid: true}
	var ended bool
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListUserSessions(ctx, user)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !c.Live(r.ExpiresAt, r.LastUsedAt) || publicID(r.IDHash) != id {
				continue
			}
			n, err := q.DeleteUserSession(ctx, dbq.DeleteUserSessionParams{IDHash: r.IDHash, UserID: user})
			ended = n > 0
			return err
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("session: revoking a session of user %d: %w", userID, err)
	}
	return ended, nil
}

// RevokePreUser ends every pre-user session bound to identityID. Approving
// an application calls it (TKT-01M29CP2MG), so the applicant's cookie is
// gone as well as refused: LookupSession stops resolving a pre-user session
// the moment its identity has a user, whether or not this runs.
func (s *Store) RevokePreUser(ctx context.Context, identityID int64) error {
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.DeletePreUserSessions(ctx, sql.NullInt64{Int64: identityID, Valid: true})
	})
}

// SweepInterval is how often serve runs Sweep (docs/02 "Background work").
const SweepInterval = time.Hour

// Sweep deletes every session that has expired by either rule, and returns
// how many it deleted. It binds the cutoff from ExpiredBefore on the store's
// own clock, as Lookup does, so it deletes exactly what a lookup at the same
// moment would refuse and never a session that is still live
// (TKT-01M29C5Y2S).
//
// A request that resolved its session before the sweep is not affected: the
// session is in its context, and nothing reads the row again. A Touch that
// follows finds no row and writes nothing, which is what it would do for an
// expired row anyway. Pre-login state is a sealed cookie, not a row, so the
// sweep never sees it; a pre_user session is a row and expires by the same
// rules as any other.
func (s *Store) Sweep(ctx context.Context) (int64, error) {
	c := ExpiredBefore(s.now())
	var n int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.SweepSessions(ctx, dbq.SweepSessionsParams{ExpiresAfter: c.ExpiresAt, LastUsedAfter: c.LastUsedAt})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("session: sweep: %w", err)
	}
	return n, nil
}

// Info is one of a user's sessions as the session list shows it. ID names
// the row for "sign out this device" (RevokeOwn), by Session.ID's
// derivation: it is not the cookie value, the row, or the SessionKey.
type Info struct {
	ID   string
	Kind Kind
	// IdentityID is the identity the session signed in through, and
	// Provider that identity's provider key; 0 and "" when none was
	// recorded.
	IdentityID int64
	Provider   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	IP         string
	UserAgent  string
	// Live is false for a session that has expired and not yet been swept.
	Live bool
}

// ListForUser lists a user's sessions, most recently used first, expired
// ones included until the sweeper removes them; Live says which is which.
func (s *Store) ListForUser(ctx context.Context, userID int64) ([]Info, error) {
	var out []Info
	c := ExpiredBefore(s.now())
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListUserSessions(ctx, sql.NullInt64{Int64: userID, Valid: true})
		for _, r := range rows {
			out = append(out, Info{ID: publicID(r.IDHash), Kind: Kind(r.Kind), IdentityID: r.IdentityID.Int64,
				Provider: r.OidcProviderKey.String, CreatedAt: time.UnixMilli(r.CreatedAt),
				ExpiresAt: time.UnixMilli(r.ExpiresAt), LastUsedAt: time.UnixMilli(r.LastUsedAt),
				IP: r.Ip.String, UserAgent: r.UserAgent.String, Live: c.Live(r.ExpiresAt, r.LastUsedAt)})
		}
		return err
	})
	return out, err
}

// newSession is what insert needs to create a row.
type newSession struct {
	Kind       Kind
	UserID     int64
	IdentityID int64
	IP         string
	UserAgent  string
	Provider   string
}

// insert creates a session and returns its token, the only time the
// plaintext exists outside the browser. decide says what the row is, and
// runs in the transaction that writes it, so what it read is still true
// when the row lands; an error from it writes nothing and is returned as it
// is. insert is unexported on purpose, and its only callers are
// IssueUserWith and IssuePreUserWith (issue.go), which decide who may have a
// session.
func (s *Store) insert(ctx context.Context, decide func(*dbq.Queries) (newSession, error)) (string, Session, error) {
	var raw [tokenBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	h := sqltypes.SessionHash(sha256.Sum256(raw[:]))
	now := s.now()
	nullInt := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }
	nullStr := func(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }
	var n newSession
	var refused error
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if n, err = decide(q); err != nil {
			refused = err
			return err
		}
		return q.CreateSession(ctx, dbq.CreateSessionParams{IDHash: h, UserID: nullInt(n.UserID),
			IdentityID: nullInt(n.IdentityID), Kind: string(n.Kind), CreatedAt: now.UnixMilli(),
			ExpiresAt: now.Add(HardExpiry).UnixMilli(), LastUsedAt: now.UnixMilli(), Ip: nullStr(n.IP),
			UserAgent: nullStr(n.UserAgent), OidcProviderKey: nullStr(n.Provider)})
	})
	if refused != nil {
		return "", Session{}, refused
	}
	if err != nil {
		return "", Session{}, fmt.Errorf("session: create: %w", err)
	}
	created := time.UnixMilli(now.UnixMilli())
	return token, Session{hash: h, Kind: n.Kind, IdentityID: n.IdentityID, Provider: n.Provider,
		CreatedAt: created, ExpiresAt: created.Add(HardExpiry), LastUsedAt: created}, nil
}

// Cookie is the session cookie for token, expiring with the session's hard
// expiry. The idle rule is the server's to enforce; the browser only needs to
// forget the cookie once no rule could let it back in.
func Cookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: token, Path: "/", Expires: expires,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

// ClearCookie tells the browser to drop the session cookie: one that names
// no session, or one just signed out.
func ClearCookie() *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

type sessionKey struct{}

// From returns the session a request resolved to, for handlers that need
// more than the viewer: GET /me reads the user fields from here rather than
// querying again.
func From(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}
