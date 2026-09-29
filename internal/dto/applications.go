package dto

import "encoding/json"

// The applications review screen (TKT-01M29CP2MG, docs/05 "mode = approval"
// step 5). Staff only: moderators and admins. The applicant's identity,
// email included, is shown to both, because deciding needs it (docs/05).
// Nothing here is ever served to a pre-user session: a decision's note is
// written for colleagues, and the applicant sees the status only
// (ApplicationStatus).

// AdminApplicationRow is one application as the review list shows it.
type AdminApplicationRow struct {
	ID int64 `json:"id"`
	// Status is pending, approved, denied or withdrawn.
	Status    string `json:"status"`
	CreatedAt Millis `json:"created_at"`
	// Provider, Email and EmailVerified are the identity's. Provider is
	// null once the identity was removed, and Email when the provider
	// asserted none or the identity was removed.
	Provider      *string `json:"provider"`
	Email         *string `json:"email"`
	EmailVerified bool    `json:"email_verified"`
}

// AdminApplicationList is GET /admin/applications: one page, newest first.
type AdminApplicationList struct {
	Applications []AdminApplicationRow `json:"applications"`
	// NextCursor is empty at the end of the list.
	NextCursor string `json:"next_cursor"`
}

// AdminApplication is GET /admin/applications/{id}, and the answer to a
// decision or an unblock.
type AdminApplication struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	CreatedAt Millis `json:"created_at"`
	// Form is the version of the form the application answered, rendered,
	// whichever version is active now.
	Form Form `json:"form"`
	// Answers are as stored, keyed by the form's field keys.
	Answers json.RawMessage `json:"answers"`
	// Identity is null once the identity was removed.
	Identity *ApplicantIdentity `json:"identity"`
	// Prior are the identity's other applications, newest first.
	Prior []AdminApplicationRow `json:"prior"`
	// Notes are the moderators' notes, oldest first.
	Notes []ApplicationNote `json:"notes"`
	// SuggestedHandle is the handle approval offers, from the identity's
	// claims, or empty when they make none. It is a suggestion: approval
	// checks whatever handle it is given.
	SuggestedHandle string `json:"suggested_handle"`
	// Decision is null while the application is pending.
	Decision *ApplicationDecision `json:"decision"`
}

// ApplicantIdentity is the identity an application came from.
type ApplicantIdentity struct {
	Provider      string  `json:"provider"`
	Email         *string `json:"email"`
	EmailVerified bool    `json:"email_verified"`
	// Blocked is true while the identity is refused new applications.
	Blocked bool `json:"blocked"`
}

// ApplicationNote is one moderator's note on an application.
type ApplicationNote struct {
	ID        int64  `json:"id"`
	Author    Author `json:"author"`
	Body      string `json:"body"`
	CreatedAt Millis `json:"created_at"`
}

// ApplicationDecision is how an application left pending.
type ApplicationDecision struct {
	// By is the deciding moderator, null for a withdrawal, which is the
	// applicant's own.
	By *Author `json:"by"`
	At Millis  `json:"at"`
	// Note is the decision's note, empty when none was given.
	Note string `json:"note"`
	// User is the member an approval created, null otherwise.
	User *Author `json:"user"`
}
