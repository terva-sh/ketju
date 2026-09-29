// Package applications is an approval-mode applicant's side of an
// application (docs/05 "mode = approval" steps 2 to 4): reading the form,
// submitting it, reading its status, and withdrawing it. Deciding an
// application is TKT-01M29CP2MG's.
//
// Every route here is reached only from a pre-user session: an applicant
// signed in with an identity that has no user. The rules live here rather
// than in the handlers, so the HTML client can ask the same questions and
// get the same answers (docs/07 "Principles").
//
// An applicant is refused, in this order, when their identity already has a
// user, when it is blocked, when registration.mode is not approval, when
// they have a pending application, and when their last application was
// submitted less than CooldownAfter ago. Submit checks every one of them
// again inside the transaction that writes, so an approval, a block or a
// change of mode between reading the form and submitting it is not missed.
//
// Nothing here issues a session, and nothing here writes an audit row: an
// applicant's own acts are not moderation or administration (docs/04
// "audit_log").
package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// CooldownAfter is how long after one submission the next may be made:
// one application per identity per rolling 24 hours, after a withdrawal or a
// denial alike. It is docs/05 step 4's anti-spam rule, settled in grooming as
// counting submissions (TKT-01M29CP2KZ).
const CooldownAfter = 24 * time.Hour

// The statuses an ApplicationStatus reports. The last four are the
// applications.status column's; none is an identity with no application.
const (
	StatusNone      = "none"
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusWithdrawn = "withdrawn"
)

// The refusals. Each is coded where it is raised, so the handlers map
// nothing themselves.
var (
	// ErrNoApplicant is a request with no session, or one whose identity
	// is gone. It is unauthenticated: signing in is the way forward.
	ErrNoApplicant = errors.New("applications: nobody is signed in as an applicant")
	// ErrNotApplicant is a viewer who is not a pre-user: a member has
	// nothing to apply for. It is forbidden.
	ErrNotApplicant = errors.New("applications: only an applicant's session may apply")
	// ErrAlreadyMember is an identity that has a user. It is conflict, and
	// the status says approved and where to sign in.
	ErrAlreadyMember = errors.New("applications: the identity already has an account")
	// ErrBlocked is a blocked identity. It is forbidden, and says no more
	// (maintainer decision 2026-09-29).
	ErrBlocked = errors.New("applications: the identity is blocked")
	// ErrNotAccepting is registration.mode other than approval. It is
	// forbidden.
	ErrNotAccepting = errors.New("applications: registration is not taking applications")
	// ErrPending is an identity with an application under review. It is
	// conflict: the client shows the status page.
	ErrPending = errors.New("applications: an application is already pending")
	// ErrTooSoon is a submission within CooldownAfter of the last one. It
	// is rate-limited, with the wait as Retry-After.
	ErrTooSoon = errors.New("applications: the last application was submitted less than 24 hours ago")
	// ErrFormChanged is a submission answering a version of the form that
	// is no longer active. It is conflict, and the client reloads the form:
	// accepting the answers against the new version would store answers to
	// questions the applicant never saw.
	ErrFormChanged = errors.New("applications: the form changed since it was loaded")
	// ErrNotPending is a withdrawal with no pending application. It is
	// conflict.
	ErrNotPending = errors.New("applications: there is no pending application to withdraw")
)

// Service is the applications service.
type Service struct {
	DB *db.DB
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// applicant is what the rules read about the viewer's identity.
type applicant struct {
	identity dbq.ApplicantIdentityRow
	// latest is the newest application, when there is one.
	latest    dbq.LatestApplicationForIdentityRow
	hasLatest bool
	mode      string
}

// Applicant refuses a viewer who is not an applicant, as every method here
// does first: anonymous is unauthenticated, anyone else but a pre-user
// forbidden. A handler calls it before it reads a request body, so who may
// ask never depends on what was asked (review 1425).
func Applicant(v domain.Viewer) error {
	_, err := identityOf(v)
	return err
}

// identityOf is the identity a viewer applies as, or the refusal.
func identityOf(v domain.Viewer) (int64, error) {
	switch {
	case v.PreUser && v.IdentityID != 0:
		return v.IdentityID, nil
	case v.UserID != 0:
		return 0, problem.Coded(problem.CodeForbidden, ErrNotApplicant)
	}
	return 0, problem.Coded(problem.CodeUnauthenticated, ErrNoApplicant)
}

// read loads what the rules need, in the caller's transaction.
func read(ctx context.Context, q *dbq.Queries, identityID int64) (applicant, error) {
	var a applicant
	var err error
	a.identity, err = q.ApplicantIdentity(ctx, identityID)
	if errors.Is(err, sql.ErrNoRows) {
		// A session whose identity is gone does not resolve, so this is a
		// race with its removal: the same as no session.
		return a, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("%w: identity %d is gone", ErrNoApplicant, identityID))
	}
	if err != nil {
		return a, err
	}
	a.latest, err = q.LatestApplicationForIdentity(ctx, sql.NullInt64{Int64: identityID, Valid: true})
	switch {
	case err == nil:
		a.hasLatest = true
	case !errors.Is(err, sql.ErrNoRows):
		return a, err
	}
	if a.mode, err = settings.RegistrationMode.In(ctx, q); err != nil {
		return a, fmt.Errorf("applications: reading registration.mode: %w", err)
	}
	return a, nil
}

// refusal is why the applicant may not submit an application now, or nil
// when they may. The order is the package comment's.
func (a applicant) refusal(now time.Time) error {
	switch {
	case a.identity.UserID.Valid:
		return problem.Coded(problem.CodeConflict, ErrAlreadyMember)
	case a.identity.BlockedAt.Valid:
		return problem.Coded(problem.CodeForbidden, ErrBlocked)
	case a.mode != "approval":
		return problem.Coded(problem.CodeForbidden, ErrNotAccepting)
	case a.hasLatest && a.latest.Status == StatusPending:
		return problem.Coded(problem.CodeConflict, ErrPending)
	}
	if wait := a.wait(now); wait > 0 {
		return problem.RetryAfter(problem.CodeRateLimited, wait, ErrTooSoon)
	}
	return nil
}

// wait is how long until CooldownAfter has passed since the latest
// submission, or 0.
func (a applicant) wait(now time.Time) time.Duration {
	if !a.hasLatest {
		return 0
	}
	return max(0, time.UnixMilli(a.latest.CreatedAt).Add(CooldownAfter).Sub(now))
}

// claims are the identity's claims a from_claim field may name
// (forms.Claims): email from the identity row, which the last sign-in
// recorded, and name and preferred_username from the claim set that sign-in
// received. A claim that is absent, blank, or not a string is left out.
func (a applicant) claims() map[string]string {
	out := map[string]string{}
	if a.identity.RawClaimsJson.Valid {
		var raw map[string]json.RawMessage
		if json.Unmarshal([]byte(a.identity.RawClaimsJson.String), &raw) == nil {
			for _, k := range []string{"name", "preferred_username"} {
				var v string
				if json.Unmarshal(raw[k], &v) == nil && strings.TrimSpace(v) != "" {
					out[k] = v
				}
			}
		}
	}
	if e := a.identity.Email; e.Valid && strings.TrimSpace(e.String) != "" {
		out["email"] = e.String
	}
	return out
}

// Form is GET /apply/form: the active version of the registration form,
// with the applicant's claims to prefill it, or the refusal.
func (s *Service) Form(ctx context.Context, v domain.Viewer) (dto.ApplicationForm, error) {
	identityID, err := identityOf(v)
	if err != nil {
		return dto.ApplicationForm{}, err
	}
	var a applicant
	var version formsvc.Version
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if a, err = read(ctx, q, identityID); err != nil {
			return err
		}
		if err := a.refusal(s.now()); err != nil {
			return err
		}
		version, err = formsvc.ActiveIn(ctx, q, formsvc.Registration)
		return err
	})
	if err != nil {
		return dto.ApplicationForm{}, err
	}
	form, err := formsvc.ToDTO(version)
	if err != nil {
		return dto.ApplicationForm{}, err
	}
	claims := a.claims()
	prefill := map[string]string{}
	for _, f := range version.Schema.Fields {
		if c, ok := claims[f.FromClaim]; ok && f.FromClaim != "" {
			prefill[f.Key] = c
		}
	}
	return dto.ApplicationForm{Form: form, Prefill: prefill}, nil
}

// Submit is POST /apply: it stores answers to version of the registration
// form as a pending application, and returns the status that follows.
//
// Every refusal is checked inside the transaction that writes, the user
// check included, and version must be the version active in it. The
// answers are stored as forms.Validate normalizes them, each locked field
// holding the identity's claim whatever was submitted. The
// applications_one_pending index stands behind the pending check.
func (s *Service) Submit(ctx context.Context, v domain.Viewer, version int, answers map[string]json.RawMessage) (dto.ApplicationStatus, error) {
	identityID, err := identityOf(v)
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	var out dto.ApplicationStatus
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		a, err := read(ctx, q, identityID)
		if err != nil {
			return err
		}
		if err := a.refusal(now); err != nil {
			return err
		}
		active, err := formsvc.ActiveIn(ctx, q, formsvc.Registration)
		if err != nil {
			return err
		}
		if active.Number != version {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("%w: the answers are to version %d, and %d is active",
				ErrFormChanged, version, active.Number))
		}
		valid, err := forms.Validate(active.Schema, answers, a.claims())
		if err != nil {
			return err
		}
		stored, err := json.Marshal(valid)
		if err != nil {
			return fmt.Errorf("applications: encoding the answers: %w", err)
		}
		id, err := q.InsertApplication(ctx, dbq.InsertApplicationParams{
			IdentityID: sql.NullInt64{Int64: identityID, Valid: true}, FormID: active.FormID,
			FormVersion: int64(active.Number), AnswersJson: string(stored), CreatedAt: now.UnixMilli()})
		if db.IsUniqueViolation(err) {
			return problem.Coded(problem.CodeConflict, ErrPending)
		}
		if err != nil {
			return err
		}
		a.latest = dbq.LatestApplicationForIdentityRow{ID: id, FormID: active.FormID, FormVersion: int64(active.Number),
			Status: StatusPending, CreatedAt: now.UnixMilli()}
		a.hasLatest = true
		out = a.status(now)
		return nil
	})
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	return out, nil
}

// Status is GET /apply/status: the latest application and what the
// applicant may do next. It refuses only a viewer who is not an applicant.
func (s *Service) Status(ctx context.Context, v domain.Viewer) (dto.ApplicationStatus, error) {
	identityID, err := identityOf(v)
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	var out dto.ApplicationStatus
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		a, err := read(ctx, q, identityID)
		if err != nil {
			return err
		}
		out = a.status(s.now())
		return nil
	})
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	return out, nil
}

// Withdraw is POST /apply/withdraw: the pending application is withdrawn,
// or the answer is ErrNotPending. It writes no audit row and no
// notification: it is the applicant's own act, and a withdrawn application
// is no longer counted as waiting for review.
func (s *Service) Withdraw(ctx context.Context, v domain.Viewer) (dto.ApplicationStatus, error) {
	identityID, err := identityOf(v)
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	var out dto.ApplicationStatus
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		// Read first, so an identity that is gone is ErrNoApplicant, as on
		// every other route, rather than a missing application.
		if _, err := read(ctx, q, identityID); err != nil {
			return err
		}
		n, err := q.WithdrawApplication(ctx, dbq.WithdrawApplicationParams{
			DecidedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true}, IdentityID: sql.NullInt64{Int64: identityID, Valid: true}})
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.Coded(problem.CodeConflict, ErrNotPending)
		}
		a, err := read(ctx, q, identityID)
		if err != nil {
			return err
		}
		out = a.status(now)
		return nil
	})
	if err != nil {
		return dto.ApplicationStatus{}, err
	}
	return out, nil
}

// status is a's ApplicationStatus at now. It says what is possible as well
// as what happened, so no client keeps a copy of the rules: can_apply is
// true exactly when refusal is nil, and can_apply_at is set only when the
// 24-hour rule is the one refusal.
func (a applicant) status(now time.Time) dto.ApplicationStatus {
	out := dto.ApplicationStatus{Status: StatusNone, Provider: a.identity.ProviderKey}
	if a.hasLatest {
		out.Status = a.latest.Status
		out.SubmittedAt = ptr(dto.Millis(a.latest.CreatedAt))
		if a.latest.DecidedAt.Valid {
			out.DecidedAt = ptr(dto.Millis(a.latest.DecidedAt.Int64))
		}
	}
	refused := a.refusal(now)
	out.CanApply = refused == nil
	out.CanWithdraw = out.Status == StatusPending
	if errors.Is(refused, ErrTooSoon) {
		out.CanApplyAt = ptr(dto.Millis(a.latest.CreatedAt + CooldownAfter.Milliseconds()))
	}
	if a.identity.UserID.Valid {
		// The identity is a member's now, whatever its applications say:
		// the way on is signing in again, as a member, through
		// session.IssueUser's status check.
		out.Status = StatusApproved
		out.CanWithdraw = false
		out.SignIn = ptr("/auth/" + url.PathEscape(a.identity.ProviderKey) + "/start?return_to=/")
	}
	return out
}

func ptr[T any](v T) *T { return &v }
