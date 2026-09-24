// Package dto holds the structs the JSON API serializes and the future HTML
// templates render.
//
// docs/07 states the rule these types exist to make true: one service layer,
// two presentation layers. `internal/service` returns a value from this
// package, `internal/web` marshals it, and `internal/web/html` will render the
// same value through a template. Neither presentation layer defines a shape of
// its own, because the moment one does, the second client stops being a
// rendering problem and becomes a second API.
//
// This package imports nothing of ours on purpose. It sits below the service
// layer and below both handler packages, so there is no direction in which a
// dependency could point back and turn "shared" into "coupled".
//
// # The goldens are the contract
//
// testdata/golden holds one committed JSON file per endpoint per viewer class.
// They are what a client developer reads instead of asking, and they are what
// fails when a field is renamed. Regenerate them with:
//
//	go test ./internal/dto -update
//
// which is the same command internal/render uses, and is deliberately a
// separate step rather than something a normal run does quietly. A golden that
// rewrites itself asserts nothing.
//
// # What is here and what is not
//
// Only shapes a document actually pins. The thread page response is given in
// full in docs/07; the forum tree, the thread list row and /me are described in
// prose there and in docs/06, and the field sets below are that prose and
// nothing more. Endpoints whose shape no document fixes are absent rather than
// guessed, because a golden over a guess locks the guess in and reads to the
// next person like a decision somebody made.
//
// POST /render's response belongs to TKT-01M29C2WWG, which returns structured
// mentions, quotes and reply_to from the render call, and is not duplicated
// here.
//
// # Nulls
//
// A pointer field means the null is meaningful and the client is expected to
// branch on it: no lens applied, never edited, not pinned, not a reply. A
// non-pointer field is always present. docs/07's abridged response shows
// "pinned": null and "moved_to": null, so absence is spelled as an explicit
// null rather than an omitted key, and no field here carries omitempty. A key
// that comes and goes is a shape a typed client has to treat as optional
// forever.
package dto

// Millis is a unix-millisecond timestamp, UTC.
//
// docs/04 makes this the project-wide time representation, chosen partly so
// that sqlc's DATETIME-to-time.Time mapping never enters the picture. It is a
// named type rather than a bare int64 so that a field holding a duration or an
// ordinal cannot be assigned to one holding a time.
type Millis int64

// Author is a post's author as the client draws them.
//
// Role and Kind are both here because docs/06 draws a badge from each and
// docs/04 keeps the two columns orthogonal: an agent can be a moderator, so
// neither field implies the other.
type Author struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
	// Avatar is a blob hash, or empty when the client should draw a fallback.
	Avatar string `json:"avatar"`
	// Role is member, moderator or admin.
	Role string `json:"role"`
	// Kind is human or agent. This is the account as it stands now, which is
	// not the same fact as Post.MachineAuthored; see the comment there.
	Kind string `json:"kind"`
}

// Attachment is one uploaded blob on a post, from the blobs table in docs/04.
//
// Neither refcount nor status appears: the first is bookkeeping and the second
// tells a reader whether a file was quarantined, which is a moderation fact
// and not something an attachment row should volunteer.
type Attachment struct {
	Hash         string `json:"hash"`
	Size         int64  `json:"size"`
	MIME         string `json:"mime"`
	OriginalName string `json:"original_name"`
}

// ForumRef names a forum from inside another response, where the full forum is
// not wanted and a link is.
type ForumRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// Pinned is a thread's pin, from pinned_scope and pinned_until in docs/04.
// The whole struct is nil when the thread is not pinned.
type Pinned struct {
	// Scope is forum or global.
	Scope string `json:"scope"`
	// Until is nil for a pin with no expiry.
	Until *Millis `json:"until"`
}

// Pagination is the page-numbered paging docs/07 uses for thread posts,
// as opposed to the cursor paging it uses where the order is a log or time.
type Pagination struct {
	Number     int `json:"number"`
	Size       int `json:"size"`
	TotalPages int `json:"total_pages"`
}

// Lens is the filter applied to a thread page, nil when none is.
//
// Shown and Hidden are both reported because docs/06 wants the reader told
// what a lens is keeping from them. A lens that quietly removed 205 posts
// would be indistinguishable from a short thread.
type Lens struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Shown  int    `json:"shown"`
	Hidden int    `json:"hidden"`
}

// ThreadHeader is the thread itself, without its posts.
type ThreadHeader struct {
	// ID is the base32 thread id (docs/03).
	ID string `json:"id"`
	// Slug is advisory: a stale one redirects to the current one (docs/07).
	Slug  string   `json:"slug"`
	Title string   `json:"title"`
	Forum ForumRef `json:"forum"`

	PostCount int `json:"post_count"`
	// LogLength is the byte length of the thread log this response reflects.
	// It is part of the ETag and of the response cache key, and it is what a
	// client passes back as after_log_length to ask for what it has not seen.
	LogLength int64    `json:"log_length"`
	Locked    bool     `json:"locked"`
	Pinned    *Pinned  `json:"pinned"`
	Tags      []string `json:"tags"`
	CreatedAt Millis   `json:"created_at"`
}

// ThreadViewer is the only per-viewer part of a thread response.
//
// docs/07 keeps it small and separate for a reason worth preserving: everything
// outside this block comes from a cache keyed on viewer class rather than on
// viewer, so a field that belongs to one person specifically belongs in here or
// it defeats the cache.
type ThreadViewer struct {
	LastReadOrdinal int `json:"last_read_ordinal"`
	// Watch is watch, mute, or empty for neither.
	Watch       string `json:"watch"`
	CanReply    bool   `json:"can_reply"`
	CanModerate bool   `json:"can_moderate"`
}

// Post is one rendered post on a thread page.
type Post struct {
	Ordinal int `json:"ordinal"`
	// Revision counts edits; it appears in the ETag so an edit invalidates a
	// cached page.
	Revision  int    `json:"revision"`
	Author    Author `json:"author"`
	CreatedAt Millis `json:"created_at"`
	// EditedAt is nil for a post that has never been edited, which is not the
	// same as one edited at the moment it was written.
	EditedAt *Millis `json:"edited_at"`

	// ReplyTo is the ordinal this post answers, nil for a top-level post.
	ReplyTo *int `json:"reply_to"`
	// ReplyToSuppressed is true when the reply link was dropped because the
	// parent is the post immediately above, per docs/06. The client needs to
	// tell that apart from a post that replies to nothing.
	ReplyToSuppressed bool `json:"reply_to_suppressed"`
	ReplyCount        int  `json:"reply_count"`

	// HTML is rendered by internal/render, sanitized there rather than here.
	HTML        string       `json:"html"`
	Attachments []Attachment `json:"attachments"`

	// Deleted marks a tombstone. An anon or member response carries no HTML
	// for one; a moderator response does, per docs/07's viewer classes.
	Deleted bool `json:"deleted"`
	// MovedTo is the thread id a split moved this post to, nil otherwise.
	MovedTo *string `json:"moved_to"`

	// MachineAuthored comes from the log's frame flag and records what was
	// true when the post was written (docs/03). It is not Author.Kind, which
	// is the account as it stands now. They disagree after an account changes
	// hands, and docs/07 says the log is right when they do.
	MachineAuthored bool `json:"machine_authored"`
}

// ThreadPage is the response to GET /threads/{id}.
type ThreadPage struct {
	Thread ThreadHeader `json:"thread"`
	Page   Pagination   `json:"page"`
	// Lens is nil when no lens is applied.
	Lens   *Lens        `json:"lens"`
	Viewer ThreadViewer `json:"viewer"`
	Posts  []Post       `json:"posts"`
}

// PostReplies is the response to GET /threads/{id}/posts/{ordinal}/replies:
// the post's direct replies in ordinal order, for the inline expander
// (docs/06 "Post anatomy"). GET /threads/{id}/posts/{ordinal} answers with a
// bare Post.
type PostReplies struct {
	Ordinal int    `json:"ordinal"`
	Posts   []Post `json:"posts"`
}

// PostHistory is the response to GET /threads/{id}/posts/{ordinal}/history.
//
// Each revision carries its source rather than a diff, and the client diffs
// two of them (settled in grooming for TKT-01M29CA9C2; docs/08 "Post history"
// asks for a word-level diff "computed on demand", which the client is where
// the demand is).
type PostHistory struct {
	Ordinal int `json:"ordinal"`
	// Deleted is a tombstone: a deleted post's history is withheld from
	// everyone but a moderator, and Revisions is then empty, since the old
	// revisions are the deleted content by another route.
	Deleted   bool           `json:"deleted"`
	Revisions []PostRevision `json:"revisions"`
}

// PostRevision is one version of a post: the original, or an edit.
type PostRevision struct {
	Revision int `json:"revision"`
	// Actor is who wrote this revision, which for an edit may be a moderator
	// rather than the author.
	Actor Author `json:"actor"`
	At    Millis `json:"at"`
	// Reason is the edit's stated reason, empty for the original and for an
	// edit that gave none.
	Reason string `json:"reason"`
	// Source is markdown, not HTML: it is what the diff is taken over.
	Source string `json:"source"`
}

// ThreadRow is one row in a thread list, with the columns docs/06 names.
type ThreadRow struct {
	ID    string   `json:"id"`
	Slug  string   `json:"slug"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`

	Author     Author `json:"author"`
	ReplyCount int    `json:"reply_count"`
	// LastPostAuthor is the "last poster" column, which is frequently not the
	// thread's author and is what a reader scans to decide whether to open it.
	LastPostAuthor Author `json:"last_post_author"`
	LastPostAt     Millis `json:"last_post_at"`

	Pinned *Pinned `json:"pinned"`
	Locked bool    `json:"locked"`

	// Unread is the badge count. LastReadOrdinal is what the badge links to:
	// the client jumps to LastReadOrdinal+1. Both are zero for a viewer with
	// no read position, including every anonymous one.
	Unread          int `json:"unread"`
	LastReadOrdinal int `json:"last_read_ordinal"`
}

// ThreadList is the response to the forum thread list and to the cross-forum
// surfaces (/unread, /new, /participated, /watching).
type ThreadList struct {
	// Forum is nil on a cross-forum surface, which is the difference between
	// "this forum's threads" and "threads from anywhere that are unread".
	Forum   *ForumRef   `json:"forum"`
	Threads []ThreadRow `json:"threads"`
	// NextCursor is empty at the end of the list. docs/07 makes these lists
	// cursor-paged because their order is time, and an offset into a list
	// ordered by last_post_at skips rows whenever somebody posts.
	NextCursor string `json:"next_cursor"`
}

// Forum is a node in the forum tree.
//
// There is no position field: docs/04 orders forums by a position column, and
// the client's job is to render the array in the order it arrives. Sending the
// sort key as well would let a client disagree with the server about order.
type Forum struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Visibility is public, members or restricted.
	Visibility string `json:"visibility"`
	Archived   bool   `json:"archived"`

	ThreadCount int `json:"thread_count"`
	// Unread is this forum's own count, not including children, so a client
	// can show both a branch total and a leaf count without double counting.
	Unread int `json:"unread"`

	// Children is at most one level deep (docs/04), and is an empty array
	// rather than null for a leaf.
	Children []Forum `json:"children"`
}

// ForumTree is the response to GET /forums.
type ForumTree struct {
	Forums []Forum `json:"forums"`
}

// UnreadCounts are the badges on the cross-forum surfaces in docs/07.
type UnreadCounts struct {
	Threads      int `json:"threads"`
	Participated int `json:"participated"`
	Watching     int `json:"watching"`
}

// Settings is the viewer's own preferences.
//
// Deliberately the subset the documents pin. docs/04 gives timezone and locale
// on the users row and docs/06 gives the page size with its 10 to 50 range.
// prefs_json holds more, but what is in it is not decided, and a golden over
// an undecided field would read as a decision.
type Settings struct {
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	PageSize int    `json:"page_size"`
}

// Me is the response to GET /me.
//
// It is the first call the client makes and docs/07 has the shell inline it as
// a bootstrap script to save the round trip, so its shape is load-bearing for
// first paint.
type Me struct {
	// User is nil for an anonymous viewer. That is the whole of how a client
	// learns it is signed out; there is no separate boolean to disagree with.
	User *Author `json:"user"`
	// PreUser is set only for an approval-mode applicant: signed in with an
	// identity, with no user yet (docs/05). User stays nil for them, so a
	// null user is still the one signed-out signal and a client that knows
	// nothing of applicants treats them as signed out, which is what they
	// are everywhere but the application flow (settled in grooming for
	// TKT-01M29CA99R).
	PreUser       *PreUser     `json:"pre_user"`
	Unread        UnreadCounts `json:"unread"`
	Notifications int          `json:"notifications"`
	Settings      Settings     `json:"settings"`
}

// PreUser is what /me says about an applicant's session.
type PreUser struct {
	// Provider is the configured key of the identity provider they signed in
	// with, so the status page can say which account the application is for.
	Provider string `json:"provider"`
}
