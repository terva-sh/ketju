package applications

// Reviewing applications (docs/05 "mode = approval" step 5, TKT-01M29CP2MG):
// staff list them, read one, leave notes on it, approve it, deny it with or
// without a block on the identity, and lift a block.
//
// Every method refuses anyone who is not an active moderator or admin
// (member.RequireStaff) before it reads anything, as the admin API's staff
// gate does before a handler runs, so a second caller that forgot the gate
// is still refused.
//
// Each decision is one transaction: the change, every row it implies, and
// its one audit row commit together or not at all. Whether the application
// is still pending is decided by the guarded UPDATE, not by the read before
// it, so of two moderators deciding at once one decides and the other is
// answered conflict, and nothing the loser's transaction wrote survives.
//
// Approval issues no session (TKT-01M3Q2V25E). It links the identity to the
// new member, which stops LookupSession resolving the applicant's pre-user
// session, and deletes those sessions in the same transaction. The member
// session comes from the applicant's next sign-in, through
// session.IssueUser's status check, like every member session.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/notify"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// PageSize is how many applications one page of the review list holds.
const PageSize = 25

// NoteMaxLen bounds a decision's note and a moderator's note on an
// application, in characters. application_notes holds the same limit in a
// CHECK.
const NoteMaxLen = 2000

// The audit actions a review writes (docs/04 "audit_log").
const (
	ActionApproved  = "application.approved"
	ActionDenied    = "application.denied"
	ActionUnblocked = "identity.unblocked"
)

// The codes a refused handle carries in a validation-failed body's errors,
// under the field "handle" (docs/07 "Applications"). They are the
// approval's own, not forms.Codes: a handle is no form's field.
const (
	CodeInvalidHandle = "invalid_handle"
	CodeReserved      = "reserved"
	CodeTaken         = "taken"
)

// The refusals a review adds. Each is coded where it is raised.
var (
	// ErrNoApplication is an id that names no application. It is
	// not-found.
	ErrNoApplication = errors.New("applications: no such application")
	// ErrAlreadyDecided is a decision on an application that is no longer
	// pending: another moderator decided it, or the applicant withdrew it.
	// It is conflict.
	ErrAlreadyDecided = errors.New("applications: the application is no longer pending")
	// ErrIdentityGone is an approval, or a denial with a block, whose
	// identity was removed, already has a user, or (for an approval) is
	// blocked. It is conflict, and nothing is written.
	ErrIdentityGone = errors.New("applications: the application's identity cannot take this decision")
	// ErrNotBlocked is an unblock of an identity that is not blocked, is
	// gone, or was blocked by another of its applications. It is conflict.
	ErrNotBlocked = errors.New("applications: the identity is not blocked by this application")
)

// staff refuses a viewer who may not review: anonymous or inactive is
// unauthenticated, and a pre-user or a member without a staff role
// forbidden.
func staff(v domain.Viewer, what string) error {
	return member.RequireStaff(v, what)
}

// Staff is the refusal a handler asks for before it reads a request body,
// so who may ask never depends on what was asked (review 1425).
func Staff(v domain.Viewer) error { return staff(v, "review applications") }

// invalid is a validation-failed refusal naming one field and its code.
func invalid(field, code string) error {
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: []forms.FieldError{{Field: field, Code: code}}}
}

// checkNote trims a note and checks it: at most NoteMaxLen characters, no
// control character but a line break or a tab, and present when required.
func checkNote(note string, required bool) (string, error) {
	note = strings.TrimSpace(note)
	switch {
	case note == "" && required:
		return "", invalid("note", forms.CodeRequired)
	case !utf8.ValidString(note):
		return "", invalid("note", forms.CodeInvalidText)
	case utf8.RuneCountInString(note) > NoteMaxLen:
		return "", invalid("note", forms.CodeTooLong)
	}
	for _, r := range note {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return "", invalid("note", forms.CodeInvalidText)
		}
	}
	return note, nil
}

// checkHandle is the handle an approval gives, as identity.CheckHandle
// returns it, or its field error.
func checkHandle(handle string) (string, error) {
	h, err := identity.CheckHandle(handle)
	switch {
	case errors.Is(err, identity.ErrHandleReserved):
		return "", invalid("handle", CodeReserved)
	case err != nil:
		return "", invalid("handle", CodeInvalidHandle)
	}
	return h, nil
}

// Filters are the review list's status values. "all" lists every status.
var Filters = []string{StatusPending, StatusApproved, StatusDenied, StatusWithdrawn, "all"}

// List is GET /admin/applications: one page of applications with status
// (pending when empty), newest first, after cursor.
func (s *Service) List(ctx context.Context, v domain.Viewer, status, cursor string) (dto.AdminApplicationList, error) {
	out := dto.AdminApplicationList{Applications: []dto.AdminApplicationRow{}}
	if err := staff(v, "list applications"); err != nil {
		return out, err
	}
	if status == "" {
		status = StatusPending
	}
	valid := false
	for _, f := range Filters {
		valid = valid || f == status
	}
	if !valid {
		return out, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("applications: %q is not a status", status))
	}
	var filter any = status
	if status == "all" {
		filter = nil
	}
	beforeAt, beforeID, err := parseCursor(cursor)
	if err != nil {
		return out, err
	}
	var rows []dbq.ListApplicationsRow
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.ListApplications(ctx, dbq.ListApplicationsParams{Status: filter, BeforeAt: beforeAt,
			BeforeID: beforeID, Lim: PageSize + 1})
		return err
	})
	if err != nil {
		return out, err
	}
	if len(rows) > PageSize {
		rows = rows[:PageSize]
		last := rows[len(rows)-1]
		out.NextCursor = base64.RawURLEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.CreatedAt, 10) + ":" + strconv.FormatInt(last.ID, 10)))
	}
	for _, r := range rows {
		out.Applications = append(out.Applications, row(r.ID, r.Status, r.CreatedAt, r.ProviderKey, r.Email, r.EmailVerified))
	}
	return out, nil
}

// parseCursor reads a list cursor, "<created_at>:<id>" in base64url, or the
// start of the list for "".
func parseCursor(cursor string) (int64, int64, error) {
	if cursor == "" {
		return 1<<63 - 1, 1<<63 - 1, nil
	}
	bad := problem.Coded(problem.CodeBadRequest, fmt.Errorf("applications: cursor %q is malformed", cursor))
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, bad
	}
	at, id, ok := strings.Cut(string(b), ":")
	if !ok {
		return 0, 0, bad
	}
	a, err1 := strconv.ParseInt(at, 10, 64)
	i, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, bad
	}
	return a, i, nil
}

func row(id int64, status string, createdAt int64, provider, email sql.NullString, verified sql.NullInt64) dto.AdminApplicationRow {
	r := dto.AdminApplicationRow{ID: id, Status: status, CreatedAt: dto.Millis(createdAt), EmailVerified: verified.Int64 != 0}
	if provider.Valid {
		r.Provider = new(provider.String)
	}
	if email.Valid {
		r.Email = new(email.String)
	}
	return r
}

// Get is GET /admin/applications/{id}: the application against the form
// version it answered, its identity, the identity's other applications,
// the moderators' notes, the suggested handle, and the decision.
func (s *Service) Get(ctx context.Context, v domain.Viewer, id int64) (dto.AdminApplication, error) {
	if err := staff(v, "read an application"); err != nil {
		return dto.AdminApplication{}, err
	}
	var out dto.AdminApplication
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out, err = detail(ctx, q, id)
		return err
	})
	return out, err
}

// getApplication is the application id names, or not-found.
func getApplication(ctx context.Context, q *dbq.Queries, id int64) (dbq.Application, error) {
	a, err := q.GetApplication(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return a, problem.Coded(problem.CodeNotFound, fmt.Errorf("%w: %d", ErrNoApplication, id))
	}
	return a, err
}

// detail reads Get's answer through q.
func detail(ctx context.Context, q *dbq.Queries, id int64) (dto.AdminApplication, error) {
	a, err := getApplication(ctx, q, id)
	if err != nil {
		return dto.AdminApplication{}, err
	}
	version, err := formsvc.VersionIn(ctx, q, a.FormID, int(a.FormVersion))
	if err != nil {
		return dto.AdminApplication{}, err
	}
	form, err := formsvc.ToDTO(version)
	if err != nil {
		return dto.AdminApplication{}, err
	}
	out := dto.AdminApplication{ID: a.ID, Status: a.Status, CreatedAt: dto.Millis(a.CreatedAt), Form: form,
		Answers: json.RawMessage(a.AnswersJson), Prior: []dto.AdminApplicationRow{}, Notes: []dto.ApplicationNote{}}

	if a.IdentityID.Valid {
		ident, err := q.ApplicantIdentity(ctx, a.IdentityID.Int64)
		switch {
		case err == nil:
			out.Identity = &dto.ApplicantIdentity{Provider: ident.ProviderKey, EmailVerified: ident.EmailVerified != 0,
				Blocked: ident.BlockedAt.Valid}
			if ident.Email.Valid {
				out.Identity.Email = new(ident.Email.String)
			}
			claims := applicant{identity: ident}.claims()
			out.SuggestedHandle = identity.Prefill(claims["preferred_username"], claims["email"])
		case !errors.Is(err, sql.ErrNoRows):
			return out, err
		}
		prior, err := q.PriorApplicationsForIdentity(ctx, dbq.PriorApplicationsForIdentityParams{IdentityID: a.IdentityID,
			BeforeAt: a.CreatedAt, BeforeID: a.ID})
		if err != nil {
			return out, err
		}
		for _, p := range prior {
			out.Prior = append(out.Prior, row(p.ID, p.Status, p.CreatedAt, p.ProviderKey, p.Email, p.EmailVerified))
		}
	}

	notes, err := q.ApplicationNotes(ctx, a.ID)
	if err != nil {
		return out, err
	}
	var ids []int64
	for _, n := range notes {
		ids = append(ids, n.AuthorID)
	}
	if a.DecidedBy.Valid {
		ids = append(ids, a.DecidedBy.Int64)
	}
	if a.UserID.Valid {
		ids = append(ids, a.UserID.Int64)
	}
	authors, err := member.AuthorsByID(ctx, q, ids)
	if err != nil {
		return out, err
	}
	for _, n := range notes {
		out.Notes = append(out.Notes, dto.ApplicationNote{ID: n.ID, Author: authors[n.AuthorID], Body: n.Body,
			CreatedAt: dto.Millis(n.CreatedAt)})
	}
	if a.Status != StatusPending {
		d := &dto.ApplicationDecision{At: dto.Millis(a.DecidedAt.Int64), Note: a.DecisionNote.String}
		if a.DecidedBy.Valid {
			d.By = new(authors[a.DecidedBy.Int64])
		}
		if a.UserID.Valid {
			d.User = new(authors[a.UserID.Int64])
		}
		out.Decision = d
	}
	return out, nil
}

// pending is the application id names while it is pending, or the refusal.
func pending(ctx context.Context, q *dbq.Queries, id int64) (dbq.Application, error) {
	a, err := getApplication(ctx, q, id)
	if err != nil {
		return a, err
	}
	if a.Status != StatusPending {
		return a, problem.Coded(problem.CodeConflict, fmt.Errorf("%w: %d is %s", ErrAlreadyDecided, id, a.Status))
	}
	return a, nil
}

// decided is the guarded decision's refusal when it wrote no row.
func decided(n int64, err error, id int64) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("%w: %d", ErrAlreadyDecided, id))
	}
	return nil
}

// Approve is POST /admin/applications/{id}/approve: the applicant becomes a
// member with handle, and the application is approved, in one transaction
// with its audit row and the new member's application_decided
// notification. note is optional. ip is the request's address, for
// the audit row.
//
// The member is created active, with role member and kind human, and named
// by the IdP's name claim or else the handle. The order is fixed: the user
// is created and the identity linked before the decision UPDATE, which
// sets the status and user_id in one statement (review 1344). When that
// UPDATE finds the application no longer pending, the whole transaction
// rolls back, the user and the link with it.
//
// It returns the approved application.
func (s *Service) Approve(ctx context.Context, v domain.Viewer, id int64, handle, note, ip string) (dto.AdminApplication, error) {
	if err := staff(v, "approve an application"); err != nil {
		return dto.AdminApplication{}, err
	}
	h, err := checkHandle(handle)
	if err != nil {
		return dto.AdminApplication{}, err
	}
	note, err = checkNote(note, false)
	if err != nil {
		return dto.AdminApplication{}, err
	}
	var out dto.AdminApplication
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		at := now.UnixMilli()
		a, err := pending(ctx, q, id)
		if err != nil {
			return err
		}
		gone := problem.Coded(problem.CodeConflict, fmt.Errorf("%w: application %d", ErrIdentityGone, id))
		if !a.IdentityID.Valid {
			return gone
		}
		ident, err := q.ApplicantIdentity(ctx, a.IdentityID.Int64)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return gone
		case err != nil:
			return err
		case ident.UserID.Valid || ident.BlockedAt.Valid:
			return gone
		}
		// Free of any member and of any reservation, in this transaction,
		// as every path that gives out a handle is (TKT-01M29CKRFS).
		switch err := identity.HandleFree(ctx, q, h, at); {
		case errors.Is(err, identity.ErrHandleTaken):
			return invalid("handle", CodeTaken)
		case err != nil:
			return err
		}
		display := strings.TrimSpace(applicant{identity: ident}.claims()["name"])
		if display == "" {
			display = h
		}
		userID, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: h, DisplayName: display,
			Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: at, UpdatedAt: at})
		if db.IsUniqueViolation(err) {
			return invalid("handle", CodeTaken)
		}
		if err != nil {
			return err
		}
		n, err := q.LinkIdentityToUser(ctx, dbq.LinkIdentityToUserParams{UserID: sql.NullInt64{Int64: userID, Valid: true},
			UpdatedAt: at, ID: ident.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			return gone
		}
		n, err = q.DecideApplication(ctx, dbq.DecideApplicationParams{Status: StatusApproved,
			DecidedBy: sql.NullInt64{Int64: v.UserID, Valid: true}, DecidedAt: sql.NullInt64{Int64: at, Valid: true},
			DecisionNote: sql.NullString{String: note, Valid: note != ""}, UserID: sql.NullInt64{Int64: userID, Valid: true},
			ID: a.ID})
		if err := decided(n, err, id); err != nil {
			return err
		}
		// The applicant's pre-user sessions no longer resolve, because the
		// identity has a user; this removes the rows too (TKT-01M3Q2V25E).
		if err := q.DeletePreUserSessions(ctx, a.IdentityID); err != nil {
			return err
		}
		// The applicant learns the decision by signing in as the new member,
		// where this row waits (TKT-01M3Q2V25E, TKT-01M29CP2N0).
		if err := notify.ApplicationDecided(ctx, q, userID, a.ID, v.UserID, now); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionApproved, audit.Application(a.ID),
			map[string]any{"user_id": userID, "identity_id": ident.ID, "handle": h, "note": note}); err != nil {
			return err
		}
		out, err = detail(ctx, q, a.ID)
		return err
	})
	if err != nil {
		return dto.AdminApplication{}, err
	}
	return out, nil
}

// Deny is POST /admin/applications/{id}/deny: the application is denied
// with note, which is required and which the applicant never sees, and with
// block the identity is blocked too, so it cannot apply again until staff
// lift the block. The denial, the block and the audit row are one
// transaction.
//
// A block must land on exactly one identity with no user. When the
// identity was removed, or has a user, the denial is refused as conflict
// and rolls back, and the moderator may deny without the block.
func (s *Service) Deny(ctx context.Context, v domain.Viewer, id int64, note string, block bool, ip string) (dto.AdminApplication, error) {
	if err := staff(v, "deny an application"); err != nil {
		return dto.AdminApplication{}, err
	}
	note, err := checkNote(note, true)
	if err != nil {
		return dto.AdminApplication{}, err
	}
	var out dto.AdminApplication
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		at := now.UnixMilli()
		a, err := pending(ctx, q, id)
		if err != nil {
			return err
		}
		n, err := q.DecideApplication(ctx, dbq.DecideApplicationParams{Status: StatusDenied,
			DecidedBy: sql.NullInt64{Int64: v.UserID, Valid: true}, DecidedAt: sql.NullInt64{Int64: at, Valid: true},
			DecisionNote: sql.NullString{String: note, Valid: true}, ID: a.ID})
		if err := decided(n, err, id); err != nil {
			return err
		}
		if block {
			gone := problem.Coded(problem.CodeConflict, fmt.Errorf("%w: application %d has no identity to block", ErrIdentityGone, id))
			if !a.IdentityID.Valid {
				return gone
			}
			n, err := q.BlockIdentity(ctx, dbq.BlockIdentityParams{BlockedAt: sql.NullInt64{Int64: at, Valid: true},
				BlockedBy: sql.NullInt64{Int64: v.UserID, Valid: true}, UpdatedAt: at, ID: a.IdentityID.Int64})
			if err != nil {
				return err
			}
			if n != 1 {
				return gone
			}
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionDenied, audit.Application(a.ID),
			map[string]any{"note": note, "blocked": block}); err != nil {
			return err
		}
		out, err = detail(ctx, q, a.ID)
		return err
	})
	if err != nil {
		return dto.AdminApplication{}, err
	}
	return out, nil
}

// Unblock is POST /admin/applications/{id}/unblock: the block the denial of
// application id set is lifted, and nothing else changes; the application
// stays denied, and the identity may apply again under the 24-hour rule.
//
// Only the denial that set the current block can lift it, which the
// UnblockIdentity statement decides in one go (review 1470): another of the
// identity's applications, older or newer, cannot, so the audit row always
// names the denial whose block it lifted. An identity that is not blocked,
// is gone, or was blocked by another application is conflict.
func (s *Service) Unblock(ctx context.Context, v domain.Viewer, id int64, ip string) (dto.AdminApplication, error) {
	if err := staff(v, "unblock an applicant"); err != nil {
		return dto.AdminApplication{}, err
	}
	var out dto.AdminApplication
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		a, err := getApplication(ctx, q, id)
		if err != nil {
			return err
		}
		refused := problem.Coded(problem.CodeConflict, fmt.Errorf("%w: application %d", ErrNotBlocked, id))
		if !a.IdentityID.Valid {
			return refused
		}
		n, err := q.UnblockIdentity(ctx, dbq.UnblockIdentityParams{UpdatedAt: now.UnixMilli(), ApplicationID: a.ID})
		if err != nil {
			return err
		}
		if n != 1 {
			return refused
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionUnblocked,
			audit.Identity(a.IdentityID.Int64), map[string]any{"application_id": a.ID}); err != nil {
			return err
		}
		out, err = detail(ctx, q, a.ID)
		return err
	})
	if err != nil {
		return dto.AdminApplication{}, err
	}
	return out, nil
}

// AddNote is POST /admin/applications/{id}/notes: a moderator's note for
// the others, of 1 to NoteMaxLen characters. It decides nothing, so it
// writes no audit row, and any status may carry one.
func (s *Service) AddNote(ctx context.Context, v domain.Viewer, id int64, body string) (dto.ApplicationNote, error) {
	if err := staff(v, "note an application"); err != nil {
		return dto.ApplicationNote{}, err
	}
	body, err := checkNote(body, true)
	if err != nil {
		var inv *forms.Invalid
		if errors.As(err, &inv) {
			inv.Fields[0].Field = "body"
		}
		return dto.ApplicationNote{}, err
	}
	var out dto.ApplicationNote
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		at := s.now().UnixMilli()
		if _, err := getApplication(ctx, q, id); err != nil {
			return err
		}
		noteID, err := q.InsertApplicationNote(ctx, dbq.InsertApplicationNoteParams{ApplicationID: id, AuthorID: v.UserID,
			Body: body, CreatedAt: at})
		if err != nil {
			return err
		}
		authors, err := member.AuthorsByID(ctx, q, []int64{v.UserID})
		if err != nil {
			return err
		}
		out = dto.ApplicationNote{ID: noteID, Author: authors[v.UserID], Body: body, CreatedAt: dto.Millis(at)}
		return nil
	})
	if err != nil {
		return dto.ApplicationNote{}, err
	}
	return out, nil
}
