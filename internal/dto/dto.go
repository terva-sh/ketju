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
// POST /render's response is RenderResult (TKT-01M29C2WWG). The endpoint
// itself is TKT-01M29CA9D4's.
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

import "encoding/json"

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
	// Width and Height are an image's, in pixels, and null for anything
	// that is not an image (docs/07 "Attachments").
	Width  *int `json:"width"`
	Height *int `json:"height"`
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
//
// Kind and Value name the lens as it was resolved: the string kind alone when
// Value is empty, and kind:value otherwise, opens the same lens again
// (docs/07 "What the thread endpoint does"). Shown is the lens's posts and
// Hidden the thread's other posts. Gaps are where the lens skipped posts,
// for every gap that touches this page.
type Lens struct {
	Kind   string    `json:"kind"`
	Value  string    `json:"value"`
	Shown  int       `json:"shown"`
	Hidden int       `json:"hidden"`
	Gaps   []LensGap `json:"gaps"`
}

// LensGap is a run of posts a lens skipped: Count posts after ordinal After,
// where After is 0 for a run at the start of the thread. The client draws it
// as a "… N posts …" divider (docs/06 "Lenses").
type LensGap struct {
	After int `json:"after"`
	Count int `json:"count"`
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
	Watch    string `json:"watch"`
	CanReply bool   `json:"can_reply"`
	// CanUpload is whether this viewer may upload a file to attach here:
	// upload in the thread's forum, which is not read-only to them. False
	// for an anonymous viewer (TKT-01M29CXK0K).
	CanUpload   bool `json:"can_upload"`
	CanModerate bool `json:"can_moderate"`
	// SlowModeSeconds is the thread's slow mode, the minimum seconds between
	// one member's posts: the thread's own value, or the site default when it
	// has none. 0 is off. Moderators are exempt, but it is served to them
	// too, so they can see what the thread asks of everyone else.
	SlowModeSeconds int `json:"slow_mode_seconds"`
	// NextPostAt is when slow mode next lets this viewer post: their newest
	// post in the thread plus SlowModeSeconds. It is null when they may post
	// now, when they are exempt, and for an anonymous viewer. It is per
	// viewer, which is why slow mode is in this block and not the header.
	NextPostAt *Millis `json:"next_post_at"`
	// RenameUntil is when this viewer's rename window for the thread closes:
	// set only for the thread's author, without moderate, while the author's
	// rename checks pass (not archived, the forum not read-only, not locked,
	// reply held, and threads.rename_window_seconds not yet passed). It is
	// null otherwise, and so always null for a moderator, who may rename at
	// any time (TKT-01M3QXV399).
	RenameUntil *Millis `json:"rename_until"`
	// AuthorWindows is how long this viewer may edit, and delete or restore,
	// their own posts, measured from each post's creation (docs/07 "Edit,
	// delete and restore"). It is null for a viewer who may moderate the
	// thread's forum, whom neither window binds, and for a viewer who is not
	// a member, who has no posts to act on (TKT-01M3PTT97X).
	AuthorWindows *AuthorWindows `json:"author_windows"`
	// ForumReadOnly is true when the thread's forum is archived and the
	// viewer is not an admin: the viewer may not post here, nor edit, delete
	// or restore their own posts, a moderator's own included. A moderator's
	// action on somebody else's post stays open (docs/07 "Edit, delete and
	// restore", docs/08 "Decided 2026-09-29"). It is what the server's
	// refusal reads, so the client offers nothing on the viewer's own post
	// that the server would refuse (TKT-01M3QHKD21).
	ForumReadOnly bool `json:"forum_read_only"`
}

// AuthorWindows is the two author windows in whole seconds, the ones the
// server enforces: a setting longer than a time.Duration holds is sent as
// the longest Duration, 9223372036 seconds, never as the raw setting
// (TKT-01M3R33SR5). A window is open for its first N seconds, so 0 never
// opens.
type AuthorWindows struct {
	EditSeconds   int `json:"edit_seconds"`
	DeleteSeconds int `json:"delete_seconds"`
}

// SlowMode answers PUT /admin/threads/{id}/slow-mode: the thread's slow mode
// as it now stands, in seconds, 0 for off (TKT-01M29CRJ50).
type SlowMode struct {
	Seconds int `json:"seconds"`
}

// Post.Removal values: who took a post down (docs/08 "Tombstones").
const (
	// RemovalAuthor is a post its author deleted.
	RemovalAuthor = "author"
	// RemovalModerator is a post a moderator hid.
	RemovalModerator = "moderator"
	// RemovalReview is a post auto-hidden pending review (docs/08 "Flags",
	// TKT-01M29CP2PJ): its open flags reached flags.auto_hide_threshold.
	RemovalReview = "review"
)

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
	// EditedByModerator is true when the post shows as edited and its
	// current revision was written by someone other than its author, which
	// only a moderator may do (TKT-01M29CRJ5J). It names nobody: the post's
	// history does (docs/08 "Post history").
	EditedByModerator bool `json:"edited_by_moderator"`

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

	// Deleted marks a post that is not shown as itself: removed or moved. It
	// stays true for every removal, as it was before Removal existed. An
	// anon or member response carries no HTML for one; a moderator's does,
	// and so does the author's for their own post a moderator removed
	// (docs/07 "Viewer class", TKT-01M29CKRJ9).
	Deleted bool `json:"deleted"`
	// Removal says who removed the post: RemovalAuthor, RemovalModerator or
	// RemovalReview. Nil for a visible post and for a moved one.
	Removal *string `json:"removal"`
	// Redacted marks a post a hard redaction cleared (docs/03 "Hard
	// redaction", TKT-01M3R1WPNS). Such a post is also Deleted, with no HTML
	// for any reader, moderators included, and it can never be restored or
	// edited. Removal still says who took it down. It is the same for every
	// viewer class, since it comes from the index alone.
	Redacted bool `json:"redacted"`
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
	Deleted bool `json:"deleted"`
	// Redacted marks a hard-redacted post, as Post.Redacted does, for every
	// reader. A moderator is still served its revisions, each with no source
	// and no reason, so this is what tells them from edits to nothing.
	Redacted  bool           `json:"redacted"`
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
	// Grace marks an author's edit made within the grace window: stored as
	// a revision, listed here, but not marking the post edited (docs/08
	// "Primitives", TKT-01M29CKRHS).
	Grace bool `json:"grace"`
}

// PostSource is the response to GET /threads/{id}/posts/{ordinal}/source:
// the post's current revision as markdown, for its author or a moderator to
// edit (TKT-01M3QJYV4Z). Revision is the one the source is, so a client
// can tell an answer from before a later edit.
type PostSource struct {
	Ordinal  int    `json:"ordinal"`
	Revision int    `json:"revision"`
	Source   string `json:"source"`
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
	// CanUpload is whether this viewer may upload a file to attach to a new
	// thread here, for the new-thread screen. False for an anonymous viewer
	// (TKT-01M29CXK0K).
	CanUpload bool `json:"can_upload"`

	// ThreadCount is the forum's threads with a post, not including
	// children's.
	ThreadCount int `json:"thread_count"`
	// PostCount is the posts in those threads that are neither deleted nor
	// moved, the rule a profile's post count follows (docs/07), not
	// including children's.
	PostCount int `json:"post_count"`
	// Unread is this forum's own count of threads with an unread post, not
	// including children, so a client can show both a branch total and a
	// leaf count without double counting.
	Unread int `json:"unread"`
	// LastPost is the forum's most recently active thread, the index's
	// "last post" column; nil when the forum has no threads.
	LastPost *ForumLastPost `json:"last_post"`

	// Children is at most one level deep (docs/04), and is an empty array
	// rather than null for a leaf.
	Children []Forum `json:"children"`
}

// ForumLastPost is a forum's latest activity: which thread, who posted, and
// when.
type ForumLastPost struct {
	Thread ThreadLink `json:"thread"`
	Author Author     `json:"author"`
	At     Millis     `json:"at"`
}

// ThreadLink names a thread from inside another response, with what a link to
// it needs.
type ThreadLink struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
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

// Settings is the viewer's own preferences: in /me, and GET and PATCH
// /settings (TKT-01M29CCC0X).
//
// Deliberately the subset the documents pin. docs/04 gives timezone and locale
// on the users row and docs/06 gives the page size with its 10 to 50 range.
// Theme is system, light or dark, stored in prefs_json and system until the
// member chooses (docs/07 "Preact client", decided by the run 2026-10-01).
// prefs_json may hold more, but nothing else in it is decided, and a golden
// over an undecided field would read as a decision.
type Settings struct {
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	PageSize int    `json:"page_size"`
	Theme    string `json:"theme"`
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
	// Queue is how many items the moderator queue lists for this viewer,
	// the top bar's badge (TKT-01M29CRJ4E). Null for a viewer with no queue:
	// anyone perm.ModeratesAny does not admit.
	Queue    *int     `json:"queue"`
	Settings Settings `json:"settings"`
	// SiteName is the site_name setting, for every viewer, anonymous
	// included: the shell's title and the wordmarks show it, and it changes
	// from the next request when an admin changes it (TKT-01M29CXK2H).
	SiteName string `json:"site_name"`
}

// PreUser is what /me says about an applicant's session.
type PreUser struct {
	// Provider is the configured key of the identity provider they signed in
	// with, so the status page can say which account the application is for.
	Provider string `json:"provider"`
	// Tag is opaque and differs from one pre-user session to the next, so
	// the client can tell two applicants who signed in with one provider
	// apart (TKT-01M3QVWKAG): a page left open for one reloads when the
	// browser is signed in as another. It grants nothing.
	Tag string `json:"tag"`
}

// RenderResult is what one render of a post's source says: the HTML, and the
// facts the post carries (docs/07 POST /render). The composer preview and
// the create path answer with this same shape from the same render call, so
// what a preview shows is what the post will record (TKT-01M29C2WWG).
type RenderResult struct {
	// HTML is sanitized by internal/render and safe to insert as it is.
	HTML string `json:"html"`
	// Mentions is every member the post mentions, in document order. A handle
	// nobody has is not here, and neither is a mention inside a quotation.
	Mentions []MentionRef `json:"mentions"`
	// Quotes is every post this one quotes, in document order.
	Quotes []QuoteRef `json:"quotes"`
	// ReplyTo is the post this one answers, validated against the thread;
	// nil for a post that answers none.
	//
	// The ordinals here are uint32, the renderer's and the log's own type,
	// rather than the int the older responses use: they come straight from
	// a render with no conversion, so no value the renderer accepted can
	// arrive changed, on any word size (review 516).
	ReplyTo *uint32 `json:"reply_to"`
}

// MentionRef is one mention. It carries the handle, spelled as its owner
// spells it, and not the user id, which no response exposes (settled in
// grooming for TKT-01M29C2WWG).
type MentionRef struct {
	Handle string `json:"handle"`
}

// QuoteRef is one quoted post.
type QuoteRef struct {
	Ordinal uint32 `json:"ordinal"`
	// Deleted is true when the quoted post has been deleted, so the render
	// shows its tombstone rather than the quoted words.
	Deleted bool `json:"deleted"`
}

// UserSuggestions is GET /users?prefix= (docs/07): the members the composer
// offers when an author types @ and the start of a handle
// (TKT-01M38XJJKM). Each is an Author, so the list can draw a suggestion
// the way a byline draws its author. Every handle here is one the renderer
// resolves as a mention, which is why a handle the mention syntax cannot
// reach is never offered.
type UserSuggestions struct {
	Users []Author `json:"users"`
}

// CreatedPost is the response to POST /threads/{id}/posts (docs/07): the post
// as a later read serves it, so the client can insert it at once, and where
// the append left the log (docs/03 "Write path", step 6). The post was
// durable before this was written.
//
// PATCH, DELETE and POST …/restore on a post answer with it too
// (TKT-01M29CA9BP): the post as it now reads, a tombstone after a delete, and
// Revision is the post's current revision, which a delete or restore does
// not move.
//
// Ordinal and Revision are uint32 and LogLength int64, the log's own types,
// for the reason RenderResult gives.
type CreatedPost struct {
	Post      Post   `json:"post"`
	Ordinal   uint32 `json:"ordinal"`
	Revision  uint32 `json:"revision"`
	LogLength int64  `json:"log_length"`
}

// ThreadHint is the data of one `event: thread` on the live-update stream,
// GET /events (docs/02 "Live updates", TKT-01M29CRJ3X): thread ThreadID has
// reached LogLength. It is the whole payload. It carries no content and
// nothing per viewer, and no more than the thread page, which shows every
// reader the log length anyway. The client fetches what changed through
// the ordinary read path.
type ThreadHint struct {
	ThreadID  string `json:"thread_id"`
	LogLength int64  `json:"log_length"`
}

// CreatedThread is the response to POST /forums/{slug}/threads (docs/07): the
// new thread's header and its first post, each as a later read of the thread
// page serves it.
type CreatedThread struct {
	Thread ThreadHeader `json:"thread"`
	Post   CreatedPost  `json:"post"`
}

// ReadPosition is the answer to PUT and DELETE /threads/{id}/read (docs/07,
// TKT-01M29CA9CE): the viewer's read position in the thread as it stands
// after the request, which is not always the ordinal asked for. A PUT never
// moves it back, so a late or out-of-order beacon answers with the higher
// position already stored; a DELETE never moves it forward.
//
// LastReadOrdinal is the thread page's viewer.last_read_ordinal, the same
// integer: every post after it is unread, and 0 means none has been read.
type ReadPosition struct {
	LastReadOrdinal int `json:"last_read_ordinal"`
}

// FlagReceipt is the answer to POST /threads/{id}/flags (docs/07,
// TKT-01M29CP2PJ): that the report was received, and nothing else. It is
// the same for a new flag, for a member's repeat of their own, and for the
// flag that hid the post, so a reporter learns no count, no other reporter
// and no outcome (docs/08 "Flags": "thanks, a moderator will look").
type FlagReceipt struct {
	Received bool `json:"received"`
}

// Draft is a reply the viewer has started and not posted: GET and PUT
// /drafts/{thread_id} (docs/07, TKT-01M29CA9CE). One per user per thread,
// kept on the server so it follows the user to another device.
//
// ThreadID is the thread's base32 id, as in ThreadHeader. ReplyTo is the
// ordinal the reply chip names, or null when the draft replies to the
// thread as a whole. UpdatedAt is when the server last stored it.
//
// Revision is the draft key's count of writes (TKT-01M3K9XBJA): every save
// adds one, and so does deleting a live draft, whose row the server keeps so
// the count survives. It only grows and reads no clock, so a client orders a
// local copy of the draft by it (docs/07 "Drafts"). A 204 carries it as the
// Draft-Revision header.
//
// Attachments are the files the composer holds, never null (TKT-01M3V73DP6),
// each with the name the viewer uploaded it under. Saving a draft adds no
// reference to them; blob GC keeps a blob a draft names instead.
type Draft struct {
	ThreadID    string       `json:"thread_id"`
	Source      string       `json:"source"`
	ReplyTo     *uint32      `json:"reply_to"`
	Attachments []Attachment `json:"attachments"`
	UpdatedAt   Millis       `json:"updated_at"`
	Revision    int64        `json:"revision"`
}

// ForumDraft is a new thread the viewer has started and not posted: GET and
// PUT /forums/{slug}/draft (docs/07 "Drafts", TKT-01M3K9XBJA). One per user
// per forum, kept on the server so it follows the user to another device.
//
// Forum is the forum's slug. Title is the title as typed, untrimmed, and is
// omitted when the draft has none. Attachments and Revision are as for
// Draft, the revision counted per forum.
type ForumDraft struct {
	Forum       string       `json:"forum"`
	Title       *string      `json:"title,omitempty"`
	Source      string       `json:"source"`
	Attachments []Attachment `json:"attachments"`
	UpdatedAt   Millis       `json:"updated_at"`
	Revision    int64        `json:"revision"`
}

// Profile is GET /users/{handle} (docs/07): an account as the viewer may see
// it. A member session is required.
type Profile struct {
	// User is the author block a post draws. For an anonymized account it
	// is the placeholder handle with no display name or avatar.
	User Author `json:"user"`
	// BioHTML is the bio rendered by internal/render, empty when there is
	// none and for an anonymized account.
	BioHTML  string `json:"bio_html"`
	JoinedAt Millis `json:"joined_at"`
	// PostCount is the account's posts that are neither deleted nor moved,
	// in forums this viewer may read. It varies by viewer, and a deleted
	// post never counts, even for a viewer whose history shows it.
	PostCount int `json:"post_count"`
	// Status is active, pending, suspended or deactivated, and is shown to
	// moderators and admins only: null for everyone else.
	Status *string `json:"status"`
}

// HandleChange is the answer to a handle change (docs/05 "Changing a
// handle"): PUT /settings/handle, a member's own, and POST
// /admin/users/{handle}/rename, an admin's. Previous is reserved for 90 days
// and redirects members to Handle.
type HandleChange struct {
	Handle   string `json:"handle"`
	Previous string `json:"previous"`
	// NextChangeAt is when the member may next change their own handle,
	// 30 days after their last own change; null when they may now. An
	// admin's rename does not move it.
	NextChangeAt *Millis `json:"next_change_at"`
}

// SessionList is GET /settings/sessions (TKT-01M29CCC0X): where the viewer
// is signed in, live sessions only, most recently used first.
type SessionList struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionInfo is one session in the list. ID names it to DELETE
// /settings/sessions/{id}; it is neither the cookie nor anything a session
// can be resumed with. Current marks the session the request came with.
// Device is a label read from UserAgent ("Firefox on Linux"), and UserAgent
// the header as the sign-in sent it. Provider is the key of the identity
// provider the session signed in through, empty when none was recorded.
type SessionInfo struct {
	ID         string `json:"id"`
	Current    bool   `json:"current"`
	Device     string `json:"device"`
	UserAgent  string `json:"user_agent"`
	IP         string `json:"ip"`
	Provider   string `json:"provider"`
	CreatedAt  Millis `json:"created_at"`
	LastUsedAt Millis `json:"last_used_at"`
}

// IdentityList is GET /settings/identities (TKT-01M29CCC0X): the sign-ins
// linked to the viewer's account, oldest first.
type IdentityList struct {
	Identities []IdentityInfo `json:"identities"`
}

// IdentityInfo is one linked sign-in. ProviderName is the provider's name
// in config.toml, empty once the operator has removed the provider, and
// Configured says which. Email is what the provider asserted, null when it
// asserted none; LastLoginAt is null for an identity never used to sign in.
// Current marks the identity the request's session signed in through.
// Removable is whether DELETE /settings/identities/{id} would remove it now:
// another identity on a configured provider remains (docs/05 "Sessions and
// CSRF", decided by the run 2026-10-01).
type IdentityInfo struct {
	ID           int64   `json:"id"`
	Provider     string  `json:"provider"`
	ProviderName string  `json:"provider_name"`
	Configured   bool    `json:"configured"`
	Email        *string `json:"email"`
	CreatedAt    Millis  `json:"created_at"`
	LastLoginAt  *Millis `json:"last_login_at"`
	Current      bool    `json:"current"`
	Removable    bool    `json:"removable"`
}

// IdentityRemoved is the answer to DELETE /settings/identities/{id}
// (TKT-01M29CCC0X). SignedOut is true when the request's own session came
// through the removed identity and so ended with it: the cookie is cleared,
// and the client is signed out.
type IdentityRemoved struct {
	SignedOut bool `json:"signed_out"`
}

// UserPosts is GET /users/{handle}/posts (docs/07): a page of an account's
// posts across the forums the viewer may read, newest first.
type UserPosts struct {
	Posts []UserPost `json:"posts"`
	// NextCursor is empty at the end of the history.
	NextCursor string `json:"next_cursor"`
}

// UserPost is one post in a history: the post as its thread page renders it
// for this viewer, and where it is.
type UserPost struct {
	Thread ThreadLink `json:"thread"`
	Forum  ForumRef   `json:"forum"`
	Post   Post       `json:"post"`
}

// Form is a declarative form version as a client renders it (docs/05
// "Forms"): the apply screen, the form editor's preview, and a future HTML
// client draw the same fields from this one shape.
type Form struct {
	// Key names the form, such as registration.
	Key string `json:"key"`
	// Version is the version this is, or 0 for an unsaved draft the
	// editor previewed.
	Version int    `json:"version"`
	Title   string `json:"title"`
	// IntroHTML is the schema's intro_markdown rendered by
	// internal/render, empty when there is none.
	IntroHTML string      `json:"intro_html"`
	Fields    []FormField `json:"fields"`
}

// FormField is one field of a Form. Every property is present for every
// type, at its zero value where the type does not use it.
type FormField struct {
	Key string `json:"key"`
	// Type is text, textarea, email, checkbox, select or markdown_notice.
	Type string `json:"type"`
	// Label may be empty for a markdown_notice.
	Label    string `json:"label"`
	Required bool   `json:"required"`
	// Max is the most characters an answer may have: the field's max, or
	// its type's cap. It is 0 for a type that takes no text.
	Max int `json:"max"`
	// Options and Multiple are a select's. Options is empty otherwise.
	Options  []string `json:"options"`
	Multiple bool     `json:"multiple"`
	// Locked means the server stores the FromClaim claim whatever is
	// submitted, so the client shows the value and does not offer to edit
	// it.
	Locked bool `json:"locked"`
	// FromClaim names the IdP claim that prefills the field, or is empty.
	FromClaim string `json:"from_claim"`
	// HTML is a markdown_notice's body rendered by internal/render. It is
	// empty for every other type.
	HTML string `json:"html"`
}

// FormAdmin is GET /admin/forms/{key} and its versions (docs/07): a form as
// the admin editor edits it. Admins only.
type FormAdmin struct {
	Key string `json:"key"`
	// Title is the form's own name, not its schema's title.
	Title         string            `json:"title"`
	ActiveVersion int               `json:"active_version"`
	Versions      []FormVersionInfo `json:"versions"`
	// Form is the version this answer is about, rendered: the active one,
	// or the one the path names.
	Form Form `json:"form"`
	// Schema is that version's schema_json as stored, which is what the
	// editor edits and what POST /admin/forms/{key}/versions takes back.
	Schema json.RawMessage `json:"schema"`
}

// FormVersionInfo is one version in a form's history, newest first.
type FormVersionInfo struct {
	Version   int    `json:"version"`
	CreatedAt Millis `json:"created_at"`
	// CreatedBy is the admin who saved it, or null for the version the
	// migration seeded.
	CreatedBy *Author `json:"created_by"`
}

// ApplicationForm is GET /apply/form (docs/07): the active version of the
// registration form, and what the applicant's identity prefills.
type ApplicationForm struct {
	Form Form `json:"form"`
	// Prefill is keyed by field key: for each field with a from_claim, the
	// claim the identity carries, when it carries one. A locked field is
	// stored as this value whatever is submitted.
	Prefill map[string]string `json:"prefill"`
}

// ApplicationStatus is GET /apply/status, and the answer to POST /apply and
// POST /apply/withdraw (docs/07): the applicant's latest application, and
// what they may do next. It never carries a moderator's note (maintainer
// decision 2026-09-29).
type ApplicationStatus struct {
	// Status is none, pending, approved, denied or withdrawn. It is
	// approved whenever the identity has a user.
	Status string `json:"status"`
	// Provider is the provider key of the identity applying.
	Provider string `json:"provider"`
	// SubmittedAt is when the latest application was submitted, null for
	// none.
	SubmittedAt *Millis `json:"submitted_at"`
	// DecidedAt is when it was approved, denied or withdrawn, null while
	// it is pending or for none.
	DecidedAt *Millis `json:"decided_at"`
	// CanWithdraw is true while the application is pending.
	CanWithdraw bool `json:"can_withdraw"`
	// CanApply is true when POST /apply would be accepted now.
	CanApply bool `json:"can_apply"`
	// CanApplyAt is when an application may be submitted again, set only
	// when the 24-hour rule is all that stands in the way.
	CanApplyAt *Millis `json:"can_apply_at"`
	// SignIn is where an approved applicant signs in as a member, set only
	// when approved.
	SignIn *string `json:"sign_in"`
}
