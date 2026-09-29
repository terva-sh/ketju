// Package agentcred is an agent's credential (docs/05 "Agent accounts",
// docs/07 "Agents on the API", TKT-01M29P9RSW): a bearer token an admin
// issues to an agent account, sent as "Authorization: Bearer <token>".
//
// It is not a session. It lives in agent_credentials, not sessions: there is
// no cookie, no idle or hard expiry, and no row in sessions, so it never
// passes through the session chokepoint (internal/auth/session). Revoking it
// is the only way it ends. It belongs to the agent, not to the admin who
// issued it, so revoking it stops that agent and nothing else.
//
// The token is ketju_agent_ followed by 32 random bytes in unpadded
// base64url. The prefix lets a secret scanner recognise a leaked one. Only
// its SHA-256 is stored, as a session id's is, and the token itself is shown
// once, in the response that issues it.
package agentcred

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/sqltypes"
)

// TokenPrefix begins every agent token.
const TokenPrefix = "ketju_agent_"

// tokenBytes is the token's random part: 256 bits, so its hash needs no salt
// and no slow KDF, as a session id's does not.
const tokenBytes = 32

// TouchInterval is how often last_used_at moves for a credential in use:
// once per five minutes at most, as a session's (session.TouchInterval).
const TouchInterval = 5 * time.Minute

// ActionIssued is a credential being issued, against the agent. Its details
// say whether it replaced a live one.
const ActionIssued = "agent.credential_issued"

var (
	// ErrNoCredential means a token names no live credential: it was never
	// issued, it was revoked, or its agent is not active. The cases are
	// deliberately indistinguishable.
	ErrNoCredential = errors.New("agentcred: no live credential")
	// ErrNotAgent is a credential asked for an account that is not an
	// agent. A human signs in through a provider and has no credential.
	ErrNotAgent = errors.New("agentcred: the account is not an agent")
	// ErrAgentNotActive is a credential asked for an agent whose status is
	// not active. Suspension and deactivation revoke the credential, so one
	// issued during a suspension would come alive at unsuspension with
	// nobody having issued it then (Terva review on PR #140).
	ErrAgentNotActive = errors.New("agentcred: the agent is not active")
)

// Rate is an agent's token bucket: Burst writes at once, refilling one
// every Refill.
type Rate struct {
	Burst  int
	Refill time.Duration
}

// Credential is a live credential as a request resolved it.
type Credential struct {
	ID     int64
	UserID int64
	// Role is the agent's role. The agent is active, or there would be no
	// credential.
	Role string
	// Rate is the agent's bucket: its overrides, or the site defaults.
	Rate       Rate
	LastUsedAt time.Time
	hash       sqltypes.SessionHash
}

// Viewer is the credential as the rest of the server sees it: an active
// member with the agent's role. SessionKey is a hash of the stored hash,
// so a per-viewer cache key is neither the token nor the row.
func (c Credential) Viewer() domain.Viewer {
	sum := sha256.Sum256(c.hash[:])
	return domain.Viewer{UserID: c.UserID, Role: c.Role, Active: true,
		SessionKey: "agent-" + hex.EncodeToString(sum[:12])}
}

// Issued is a credential just issued. Token is the only copy of the
// plaintext there will ever be.
type Issued struct {
	Token     string
	CreatedAt time.Time
}

// newToken makes a token and its hash.
func newToken() (string, sqltypes.SessionHash, error) {
	var raw [tokenBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", sqltypes.SessionHash{}, err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), sha256.Sum256(raw[:]), nil
}

// hashToken turns a presented token into the stored hash, or reports that
// it cannot be a token at all, without a query.
func hashToken(token string) (sqltypes.SessionHash, bool) {
	rest, ok := strings.CutPrefix(token, TokenPrefix)
	if !ok {
		return sqltypes.SessionHash{}, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(rest)
	if err != nil || len(raw) != tokenBytes {
		return sqltypes.SessionHash{}, false
	}
	return sha256.Sum256(raw), true
}

// Issue gives userID a new credential inside the caller's transaction,
// revoking any live one first, and writes the audit rows beside it. It
// refuses an account that is not an agent (ErrNotAgent) and an agent whose
// status is not active (ErrAgentNotActive), both conflict. The account is
// read in the transaction that inserts, so a suspension cannot land between
// the check and the write.
func Issue(ctx context.Context, q *dbq.Queries, at time.Time, by audit.By, userID int64) (Issued, error) {
	u, err := q.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Issued{}, perm.ErrNoUser
	}
	if err != nil {
		return Issued{}, err
	}
	if u.Kind != domain.KindAgent {
		return Issued{}, problem.Coded(problem.CodeConflict, ErrNotAgent)
	}
	if u.Status != domain.StatusActive || u.Anonymized != 0 {
		return Issued{}, problem.Coded(problem.CodeConflict, fmt.Errorf("%w: it is %s", ErrAgentNotActive, u.Status))
	}
	replaced, err := perm.RevokeAgentCredentialIn(ctx, q, at, by, userID, ActionIssued)
	if err != nil {
		return Issued{}, err
	}
	token, h, err := newToken()
	if err != nil {
		return Issued{}, err
	}
	if err := q.InsertAgentCredential(ctx, dbq.InsertAgentCredentialParams{UserID: userID, TokenHash: h,
		CreatedBy: sql.NullInt64{Int64: by.Actor, Valid: by.Actor != 0}, CreatedAt: at.UnixMilli()}); err != nil {
		return Issued{}, err
	}
	if _, err := audit.Append(ctx, q, at, by, ActionIssued, audit.User(userID),
		map[string]any{"replaced": replaced}); err != nil {
		return Issued{}, err
	}
	return Issued{Token: token, CreatedAt: time.UnixMilli(at.UnixMilli())}, nil
}

// rate is the bucket for a burst and a refill in seconds, each held inside
// what the settings and migration 0013's CHECKs allow. Both already refuse
// anything outside it; this is the last line, because a refill that
// overflowed time.Duration would read as no limit at all (Terva review
// 1477), and a limiter that fails open is not one.
func rate(burst, refill int) Rate {
	burst = max(burst, 1)
	refill = min(max(refill, 1), settings.AgentRefillMaxSeconds)
	return Rate{Burst: burst, Refill: time.Duration(refill) * time.Second}
}

// Store resolves presented tokens.
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

// Lookup resolves a token to its live credential, with the agent's role and
// bucket, in one read transaction: the credential joined to its account and
// overrides, and the two site defaults beside it. It returns ErrNoCredential
// for any token that is not a live credential of an active agent.
func (s *Store) Lookup(ctx context.Context, token string) (Credential, error) {
	h, ok := hashToken(token)
	if !ok {
		return Credential{}, ErrNoCredential
	}
	var out Credential
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.LookupAgentCredential(ctx, h)
		if err != nil {
			return err
		}
		out = Credential{ID: row.ID, UserID: row.UserID, Role: row.Role, hash: h}
		if row.LastUsedAt.Valid {
			out.LastUsedAt = time.UnixMilli(row.LastUsedAt.Int64)
		}
		burst, err := settings.AgentWriteBurst.In(ctx, q)
		if err != nil {
			return err
		}
		refill, err := settings.AgentWriteRefillSeconds.In(ctx, q)
		if err != nil {
			return err
		}
		if row.BucketBurst.Valid {
			burst = int(row.BucketBurst.Int64)
		}
		if row.BucketRefillSeconds.Valid {
			refill = int(row.BucketRefillSeconds.Int64)
		}
		out.Rate = rate(burst, refill)
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNoCredential
	}
	if err != nil {
		return Credential{}, fmt.Errorf("agentcred: lookup: %w", err)
	}
	return out, nil
}

// Touch records that c was used now, if TouchInterval has passed since it
// last was, and reports whether it wrote. It is a freshness record for the
// admin screen, not a gate.
func (s *Store) Touch(ctx context.Context, c Credential) (bool, error) {
	now := s.now()
	if !c.LastUsedAt.IsZero() && c.LastUsedAt.After(now.Add(-TouchInterval)) {
		return false, nil
	}
	var n int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.TouchAgentCredential(ctx, dbq.TouchAgentCredentialParams{Now: now.UnixMilli(), ID: c.ID,
			DueBefore: now.Add(-TouchInterval).UnixMilli()})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("agentcred: touch: %w", err)
	}
	return n > 0, nil
}

type credentialKey struct{}

// With returns ctx carrying c. The middleware sets it once per request.
func With(ctx context.Context, c Credential) context.Context {
	return context.WithValue(ctx, credentialKey{}, c)
}

// From returns the credential a request authenticated with, if it did.
func From(ctx context.Context) (Credential, bool) {
	c, ok := ctx.Value(credentialKey{}).(Credential)
	return c, ok
}

// Key is the limiter key for c's agent: one bucket per agent, whichever
// credential it holds, so rotating a token does not refill the bucket.
func (c Credential) Key() string { return "agent:" + strconv.FormatInt(c.UserID, 10) }
