package dto

// The moderator queue (TKT-01M29CRJ4E, docs/07 "Moderation and admin",
// docs/08 "Flags"): open flags, one item per flagged post, and pending
// applications, in one list by newest time first. Every response here is
// for a viewer who moderates somewhere; a flag appears only to a viewer who
// may moderate its forum, and an application only to staff.

// The kinds of queue item.
const (
	QueueKindFlag        = "flag"
	QueueKindApplication = "application"
)

// Queue is GET /admin/queue: one page of items.
type Queue struct {
	Items []QueueItem `json:"items"`
	// NextCursor is empty at the end of the list.
	NextCursor string `json:"next_cursor"`
}

// QueueItem is one thing for a moderator to decide. Exactly one of Flag and
// Application is set, as Kind says.
type QueueItem struct {
	Kind string `json:"kind"`
	// CreatedAt orders the list: a flagged post's newest open flag, or when
	// the application was submitted.
	CreatedAt   Millis               `json:"created_at"`
	Flag        *QueueFlag           `json:"flag"`
	Application *AdminApplicationRow `json:"application"`
}

// QueueFlag is a flagged post and every flag open on it.
type QueueFlag struct {
	Thread  QueueThread `json:"thread"`
	Ordinal int         `json:"ordinal"`
	// Post is the post as a moderator reads it, which shows a hidden post's
	// words beside its removal. Null when the thread cannot be read just
	// now, for example while it is quarantined.
	Post *Post `json:"post"`
	// Flags are the open flags, newest first. Their reporters are shown to
	// moderators only (docs/08 "Flags").
	Flags []QueueReport `json:"flags"`
}

// QueueThread names the thread a flagged post is in, for its link.
type QueueThread struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// QueueReport is one open flag.
type QueueReport struct {
	ID int64 `json:"id"`
	// Reason is spam, abuse, off-topic or other.
	Reason    string `json:"reason"`
	Note      string `json:"note"`
	Reporter  Author `json:"reporter"`
	CreatedAt Millis `json:"created_at"`
}

// The outcomes a queue action reports for the post, in its answer and in
// its audit row: what this request did, not what it asked.
const (
	QueuePostHidden        = "hidden"
	QueuePostRestored      = "restored"
	QueuePostAlreadyHidden = "already hidden"
	QueuePostUnchanged     = "unchanged"
)

// QueueResolution is the answer to POST /admin/queue/posts/{id}/{ordinal}.
type QueueResolution struct {
	// Action is what was asked: resolve, dismiss or remove.
	Action string `json:"action"`
	// Post is one of the QueuePost outcomes.
	Post string `json:"post"`
	// Flags are the ids of the flags the action closed.
	Flags []int64 `json:"flags"`
}
