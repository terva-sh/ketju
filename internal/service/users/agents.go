package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Agent accounts (TKT-01M29P9RSW, docs/05 "Agent accounts"). An agent is a
// member with users.kind = 'agent', provisioned by an admin, never
// self-registered: it has no browser and no identity, so nothing can sign
// it in, and it authenticates with the credential issued here
// (internal/auth/agentcred). It is created as a member; a role is changed
// afterwards through SetRole, so a role never changes without its audit
// row. Everything here is an admin's.

// The audit_log actions provisioning writes, against the agent.
const (
	// ActionUserCreated is an account an admin created, with its "kind".
	ActionUserCreated = "user.created"
	// ActionAgentLimitsChanged is an agent's bucket overrides changing,
	// with the new "burst" and "refill_seconds", null for the default.
	ActionAgentLimitsChanged = "agent.limits_changed"
)

// DisplayNameMaxLen bounds an agent's display name, in characters.
const DisplayNameMaxLen = 100

// NewAgent is what an admin fills in to provision an agent.
type NewAgent struct {
	Handle      string
	DisplayName string
	// IssueCredential issues the agent's credential in the same
	// transaction, so creating an agent and giving it a token is one step.
	IssueCredential bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func invalid(format string, args ...any) error {
	return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("users: "+format, args...))
}

// CreateAgent is POST /admin/users with kind agent: an admin provisions an
// agent account, active and a member, and with in.IssueCredential its
// credential. The users row, the agents row, the credential and their audit
// rows are one transaction. The handle passes registration's rules and must
// be free, including of a reservation (identity.HandleFree); a taken one is
// conflict. No identity is written, so no sign-in can reach the account.
func (s *Service) CreateAgent(ctx context.Context, v domain.Viewer, ip string, in NewAgent) (dto.CreatedAgent, error) {
	var out dto.CreatedAgent
	if err := member.RequireAdmin(v, "create an agent"); err != nil {
		return out, err
	}
	h, err := identity.CheckHandle(in.Handle)
	if err != nil {
		return out, problem.Coded(problem.CodeValidationFailed, err)
	}
	// The rule a member's own display name follows too (CheckDisplayName).
	display, code := CheckDisplayName(in.DisplayName)
	switch {
	case code == forms.CodeTooLong:
		return out, invalid("the display name is over %d characters", DisplayNameMaxLen)
	case code != "":
		return out, invalid("the display name is not valid UTF-8 or has a control character")
	case display == "":
		display = h
	}
	now := s.now()
	by := audit.By{Actor: v.UserID, IP: ip}
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := identity.HandleFree(ctx, q, h, now.UnixMilli()); err != nil {
			return err
		}
		id, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: h, DisplayName: display,
			Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindAgent,
			CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()})
		if db.IsUniqueViolation(err) {
			return identity.ErrHandleTaken
		}
		if err != nil {
			return err
		}
		if err := q.CreateAgent(ctx, dbq.CreateAgentParams{UserID: id,
			CreatedBy: sql.NullInt64{Int64: v.UserID, Valid: v.UserID != 0}, CreatedAt: now.UnixMilli()}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, by, ActionUserCreated, audit.User(id),
			map[string]any{"kind": domain.KindAgent, "handle": h}); err != nil {
			return err
		}
		if in.IssueCredential {
			issued, err := agentcred.Issue(ctx, q, now, by, id)
			if err != nil {
				return err
			}
			out.Credential = &dto.IssuedCredential{Token: issued.Token, CreatedAt: dto.Millis(issued.CreatedAt.UnixMilli())}
		}
		return s.adminDetail(ctx, q, h, true, &out.User)
	})
	if errors.Is(err, identity.ErrHandleTaken) {
		return dto.CreatedAgent{}, problem.Coded(problem.CodeConflict, err)
	}
	if err != nil {
		return dto.CreatedAgent{}, err
	}
	return out, nil
}

// agentID is the id of the agent account handle names: not-found for a
// handle nobody has, and conflict for a human's.
func agentID(ctx context.Context, q *dbq.Queries, handle string) (int64, error) {
	u, err := q.GetUserByHandle(ctx, handle)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, problem.Coded(problem.CodeNotFound, fmt.Errorf("users: no account has the handle %q", handle))
	}
	if err != nil {
		return 0, err
	}
	if u.Kind != domain.KindAgent {
		return 0, problem.Coded(problem.CodeConflict, agentcred.ErrNotAgent)
	}
	return u.ID, nil
}

// IssueCredential is POST /admin/users/{handle}/credential: a new
// credential for the agent, replacing its live one, which stops working at
// once. An agent that is not active is refused (conflict) and gets no
// credential, so an agent unsuspended has none until one is issued afresh.
func (s *Service) IssueCredential(ctx context.Context, v domain.Viewer, ip, handle string) (dto.IssuedCredential, error) {
	var out dto.IssuedCredential
	if err := member.RequireAdmin(v, "issue an agent credential"); err != nil {
		return out, err
	}
	now := s.now()
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		id, err := agentID(ctx, q, handle)
		if err != nil {
			return err
		}
		issued, err := agentcred.Issue(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, id)
		if err != nil {
			return err
		}
		out = dto.IssuedCredential{Token: issued.Token, CreatedAt: dto.Millis(issued.CreatedAt.UnixMilli())}
		return nil
	})
	return out, err
}

// RevokeCredential is DELETE /admin/users/{handle}/credential: the agent's
// live credential stops working at once, and the account is untouched. An
// agent with no live credential is not-found.
func (s *Service) RevokeCredential(ctx context.Context, v domain.Viewer, ip, handle string) error {
	if err := member.RequireAdmin(v, "revoke an agent credential"); err != nil {
		return err
	}
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		id, err := agentID(ctx, q, handle)
		if err != nil {
			return err
		}
		ok, err := perm.RevokeAgentCredentialIn(ctx, q, s.now(), audit.By{Actor: v.UserID, IP: ip}, id, "admin")
		if err == nil && !ok {
			err = problem.Coded(problem.CodeNotFound, fmt.Errorf("users: %q has no live credential", handle))
		}
		return err
	})
}

// SetAgentLimits is PUT /admin/users/{handle}/agent-limits: the agent's own
// burst and refill, each at least 1, or nil for the site default. It
// answers with the account as it now stands, read in the same transaction.
func (s *Service) SetAgentLimits(ctx context.Context, v domain.Viewer, ip, handle string, burst, refill *int) (dto.AdminUserDetail, error) {
	var out dto.AdminUserDetail
	if err := member.RequireAdmin(v, "change an agent's limits"); err != nil {
		return out, err
	}
	if burst != nil && *burst < 1 {
		return out, invalid("the burst is %d; it must be at least 1", *burst)
	}
	if refill != nil && (*refill < 1 || *refill > settings.AgentRefillMaxSeconds) {
		return out, invalid("the refill is %d seconds; it must be from 1 to %d", *refill, settings.AgentRefillMaxSeconds)
	}
	null := func(p *int) sql.NullInt64 {
		if p == nil {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: int64(*p), Valid: true}
	}
	now := s.now()
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		id, err := agentID(ctx, q, handle)
		if err != nil {
			return err
		}
		n, err := q.SetAgentLimits(ctx, dbq.SetAgentLimitsParams{Burst: null(burst), RefillSeconds: null(refill), UserID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			// An agent account with no agents row was not provisioned here
			// (a fixture, or a row written by hand). It gets one now.
			if err := q.CreateAgent(ctx, dbq.CreateAgentParams{UserID: id, CreatedAt: now.UnixMilli()}); err != nil {
				return err
			}
			if _, err := q.SetAgentLimits(ctx, dbq.SetAgentLimitsParams{Burst: null(burst), RefillSeconds: null(refill),
				UserID: id}); err != nil {
				return err
			}
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionAgentLimitsChanged,
			audit.User(id), map[string]any{"burst": burst, "refill_seconds": refill}); err != nil {
			return err
		}
		return s.adminDetail(ctx, q, handle, true, &out)
	})
	return out, err
}

// agentInfo is u's AgentInfo for the admin detail, or nil for a human.
func agentInfo(ctx context.Context, q *dbq.Queries, u dbq.User) (*dto.AgentInfo, error) {
	if u.Kind != domain.KindAgent {
		return nil, nil
	}
	burst, err := settings.AgentWriteBurst.In(ctx, q)
	if err != nil {
		return nil, err
	}
	refill, err := settings.AgentWriteRefillSeconds.In(ctx, q)
	if err != nil {
		return nil, err
	}
	info := &dto.AgentInfo{DefaultBurst: burst, DefaultRefillSeconds: refill}
	switch a, err := q.GetAgent(ctx, u.ID); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if a.BucketBurst.Valid {
			burst := int(a.BucketBurst.Int64)
			info.Burst = &burst
		}
		if a.BucketRefillSeconds.Valid {
			refill := int(a.BucketRefillSeconds.Int64)
			info.RefillSeconds = &refill
		}
	}
	switch c, err := q.LiveAgentCredential(ctx, u.ID); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		info.Credential = &dto.AgentCredential{CreatedAt: dto.Millis(c.CreatedAt)}
		if c.LastUsedAt.Valid {
			used := dto.Millis(c.LastUsedAt.Int64)
			info.Credential.LastUsedAt = &used
		}
	}
	return info, nil
}
