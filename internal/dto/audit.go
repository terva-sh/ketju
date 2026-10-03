package dto

import "encoding/json"

// The admin audit view (TKT-01M29CP2R4, docs/07 "The audit log"). Every
// response here is an admin's alone: the log says who did what, from which
// address, and moderators see none of it (docs/08 "Decided 2026-09-29").

// AuditPage is GET /admin/audit: one page of audit_log, newest first.
type AuditPage struct {
	Entries []AuditEntry `json:"entries"`
	// Actions is every action a row is written with today, sorted, for the
	// screen's action filter (audit.Actions). The filter also takes free
	// text, for an action this list lacks.
	Actions []string `json:"actions"`
	// NextCursor is empty at the end of the log.
	NextCursor string `json:"next_cursor"`
}

// AuditEntry is one audit_log row, its actor and target resolved for
// display.
type AuditEntry struct {
	ID int64  `json:"id"`
	At Millis `json:"at"`
	// Actor is the member who acted, drawn as a post's author is, or null
	// when no account did: the command line, a job, the auto-hide. Details
	// then say what did, as "via".
	Actor  *Author     `json:"actor"`
	Action string      `json:"action"`
	Target AuditTarget `json:"target"`
	// Related is the other things the row names, resolved as the target
	// is: a thread.moved row's forum before and after the move, and a
	// posts.moved row's thread the posts left and the one they went to,
	// each in that order. Empty, never null, for every other row.
	Related []AuditTarget `json:"related"`
	// Details is the row's details_json as written, a JSON object. It is
	// data to show as text, never markup.
	Details json.RawMessage `json:"details"`
	// IP is the request's address, or empty when there was no request.
	IP string `json:"ip"`
}

// AuditTarget is what a row names, as the screen links it.
type AuditTarget struct {
	// Type is the row's target_type and ID its target_id, as written
	// (docs/04 "audit_log").
	Type string `json:"type"`
	ID   string `json:"id"`
	// Label is how the thing reads now: a handle, a title, a forum's or
	// group's name, a setting's key. It is the raw id when the thing no
	// longer resolves, except an absent user, which reads as
	// deleted-user-<id>.
	Label string `json:"label"`
	// Href is where the thing is, or empty when it has no page: an
	// identity, a deleted blob, a thing that no longer resolves.
	Href string `json:"href"`
}
