// The API's response shapes, mirroring internal/dto (docs/07 "Where the
// shapes live"). The Go structs and their goldens are the contract; these are
// written by hand to match, and dto-contract.ts fails the type check when a
// golden and a type here disagree in either direction. What each field means
// is documented on the Go side and not repeated here.

// Milliseconds since the Unix epoch.
export type Millis = number;

export interface Author {
  handle: string;
  display_name: string;
  avatar: string;
  role: string;
  kind: string;
}

export interface Attachment {
  hash: string;
  size: number;
  mime: string;
  original_name: string;
  // An image's dimensions in pixels; null for anything that is not an image.
  width: number | null;
  height: number | null;
}

export interface ForumRef {
  slug: string;
  name: string;
}

export interface Pinned {
  scope: string;
  until: Millis | null;
}

export interface Pagination {
  number: number;
  size: number;
  total_pages: number;
}

export interface Lens {
  kind: string;
  value: string;
  shown: number;
  hidden: number;
  gaps: LensGap[];
}

export interface LensGap {
  after: number;
  count: number;
}

export interface ThreadHeader {
  id: string;
  slug: string;
  title: string;
  forum: ForumRef;
  // author is the thread's starter.
  author: Author;
  post_count: number;
  log_length: number;
  locked: boolean;
  // archived is true once the thread is archived: read-only to everyone,
  // admins included, for good (docs/08 "Locking semantics",
  // TKT-01M3YSQQ33).
  archived: boolean;
  pinned: Pinned | null;
  tags: string[];
  created_at: Millis;
  // split_from is set on a thread a split opened, null on any other. Its
  // thread is null for a viewer who may not read the thread the posts came
  // from (docs/07 "Moving posts").
  split_from: SplitFrom | null;
}

export interface SplitFrom {
  thread: ThreadLink | null;
}

// MovedPosts is POST /admin/threads/{id}/split's answer: both threads'
// headers as their pages now serve them.
export interface MovedPosts {
  source: ThreadHeader;
  thread: ThreadHeader;
}

// MergedPosts is POST /admin/threads/{id}/merge's answer: both threads'
// headers as their pages now serve them.
export interface MergedPosts {
  source: ThreadHeader;
  target: ThreadHeader;
}

export interface ThreadViewer {
  last_read_ordinal: number;
  // last_read_at is when this viewer's place was last set, null with none.
  last_read_at: Millis | null;
  // watch is the thread's own level, '' when it follows its forum;
  // forum_watch is the forum's, which applies then.
  watch: string;
  forum_watch: string;
  can_reply: boolean;
  // can_upload: this viewer may upload a file to attach here.
  can_upload: boolean;
  can_moderate: boolean;
  // slow_mode_seconds is the thread's effective slow mode, 0 for off.
  slow_mode_seconds: number;
  // next_post_at is when slow mode next lets this viewer post, or null.
  next_post_at: Millis | null;
  // rename_until is when this viewer's rename window closes: set only for
  // the thread's author without moderate while the server would let them
  // rename it, and null otherwise, a moderator's included.
  rename_until: Millis | null;
  // author_windows is how long this viewer may edit, and delete or restore,
  // their own posts, from each post's creation, in the seconds the server
  // enforces. null for a viewer who may moderate the forum, whom neither
  // binds, and for one who is not a member.
  author_windows: AuthorWindows | null;
  // forum_read_only: the forum is archived and this viewer is not an admin,
  // so their own posts take no edit, delete or restore.
  forum_read_only: boolean;
}

export interface AuthorWindows {
  edit_seconds: number;
  delete_seconds: number;
}

// SlowMode is PUT /admin/threads/{id}/slow-mode's answer.
export interface SlowMode {
  seconds: number;
}

export interface Post {
  ordinal: number;
  revision: number;
  author: Author;
  created_at: Millis;
  edited_at: Millis | null;
  // The current revision is a moderator's edit; names nobody.
  edited_by_moderator: boolean;
  reply_to: number | null;
  reply_to_suppressed: boolean;
  reply_count: number;
  // The posts that quote this one, ascending; empty on a post served without
  // html. A quoting post is listed only while every reader sees it as itself
  // (docs/06 "Post anatomy").
  quoted_by: number[];
  html: string;
  attachments: Attachment[];
  deleted: boolean;
  // Who removed the post: 'author', 'moderator' or 'review'; null for a
  // visible or moved post (docs/07 "Removed posts").
  removal: string | null;
  // A hard redaction cleared the post: it is deleted, has no html for anyone,
  // and can never be restored or edited (docs/07 "Removed posts").
  redacted: boolean;
  // moved marks a post a split or merge moved to another thread: deleted,
  // with no removal and no html for anyone. moved_to and move_notice say
  // where, only to a viewer who may read that thread (docs/07 "Removed
  // posts").
  moved: boolean;
  moved_to: MovedTo | null;
  // move_notice is on the first post of each move: "N posts were moved".
  move_notice: MoveNotice | null;
  // merge_notice is on the first copy of each merge into this thread: "N
  // posts were merged from <link>", the link being the source and its first
  // moved post, only to a viewer who may read it (docs/07 "Moving posts").
  merge_notice: MoveNotice | null;
  machine_authored: boolean;
}

// MovedTo is the thread a moved post went to and the copy's own ordinal
// there, null while the move is unfinished.
export interface MovedTo {
  thread: ThreadLink;
  ordinal: number | null;
}

// MoveNotice is a move's count, and its target and first copy when the
// viewer may read the target.
export interface MoveNotice {
  count: number;
  thread: ThreadLink | null;
  ordinal: number | null;
}

export interface ThreadPage {
  thread: ThreadHeader;
  page: Pagination;
  lens: Lens | null;
  viewer: ThreadViewer;
  posts: Post[];
}

export interface PostReplies {
  ordinal: number;
  posts: Post[];
}

export interface PostRevision {
  revision: number;
  actor: Author;
  at: Millis;
  reason: string;
  source: string;
  // An author's edit within the grace window: a stored revision that did not
  // mark the post edited.
  grace: boolean;
}

export interface PostHistory {
  ordinal: number;
  deleted: boolean;
  // A hard-redacted post: a moderator's revisions have no source or reason.
  redacted: boolean;
  revisions: PostRevision[];
}

// PostSource is GET /threads/{id}/posts/{ordinal}/source: the post's current
// revision as markdown, for its author or a moderator to edit (internal/dto
// PostSource).
export interface PostSource {
  ordinal: number;
  revision: number;
  source: string;
}

export interface ThreadRow {
  id: string;
  slug: string;
  title: string;
  tags: string[];
  author: Author;
  reply_count: number;
  last_post_author: Author;
  last_post_at: Millis;
  pinned: Pinned | null;
  locked: boolean;
  archived: boolean;
  // forum is the row's forum on a cross-forum list, null on a forum's own
  // (TKT-01M29CTY7K).
  forum: ForumRef | null;
  unread: number;
  last_read_ordinal: number;
}

export interface ThreadList {
  forum: ForumRef | null;
  // watch is the viewer's level on the forum, 'watch', 'mute' or '' for
  // none, and '' for an anonymous viewer and on a cross-forum list. The
  // forum's header shows it on the watch control (TKT-01M3V6Y87D).
  watch: string;
  threads: ThreadRow[];
  next_cursor: string;
}

export interface Forum {
  slug: string;
  name: string;
  description: string;
  visibility: string;
  archived: boolean;
  // can_upload: this viewer may upload a file to attach to a new thread here.
  can_upload: boolean;
  thread_count: number;
  post_count: number;
  unread: number;
  last_post: ForumLastPost | null;
  children: Forum[];
}

export interface ForumLastPost {
  thread: ThreadLink;
  author: Author;
  at: Millis;
}

export interface ThreadLink {
  id: string;
  slug: string;
  title: string;
}

export interface ForumTree {
  forums: Forum[];
  // members: the accounts the mention lookup can offer, for the front page
  // footer. Null for an anonymous viewer or an applicant (TKT-01M3S3ZRYD).
  members: number | null;
}

export interface UnreadCounts {
  threads: number;
  participated: number;
  watching: number;
  // new is the New badge's count: the rows of /new (TKT-01M29CTY7K).
  new: number;
}

// Settings is /me's settings, and GET and PATCH /settings (TKT-01M29CCC0X).
// theme is system, light or dark, and the server always sends it; so does
// the bootstrap's anonymous fallback. It is typed as a string, as the
// goldens are, and state/theme.ts reads any value but the three as no
// choice. shortcuts is whether the single-key shortcuts are on, true
// until the member turns them off (TKT-01M3V9DWTN).
export interface Settings {
  timezone: string;
  locale: string;
  page_size: number;
  theme: string;
  shortcuts: boolean;
}

export interface Me {
  user: Author | null;
  // Set only for an approval-mode applicant, whose user is still null.
  pre_user: { provider: string; tag: string } | null;
  unread: UnreadCounts;
  notifications: number;
  // The moderator queue's item count, the top bar's badge; null for a viewer
  // with no queue (TKT-01M29CRJ4E).
  queue: number | null;
  settings: Settings;
  // The site_name setting, for every viewer (TKT-01M29CXK2H): the
  // wordmarks show it.
  site_name: string;
}

// RenderResult is POST /render's answer, and what the create path records
// from the same render (internal/dto RenderResult).
export interface RenderResult {
  // Sanitized by the server's renderer; safe to insert as it is.
  html: string;
  // Handles, as their owners spell them, in document order.
  mentions: { handle: string }[];
  quotes: { ordinal: number; deleted: boolean }[];
  // The post this one answers, validated against the thread.
  reply_to: number | null;
}

// CreatedPost is POST /threads/{id}/posts: the post as a later read serves
// it, and where the append left the log (internal/dto CreatedPost).
export interface CreatedPost {
  post: Post;
  ordinal: number;
  revision: number;
  log_length: number;
}

// ThreadHint is the data of an `event: thread` on the live-update stream,
// GET /events: the thread reached log_length, and nothing else
// (internal/dto ThreadHint).
export interface ThreadHint {
  thread_id: string;
  log_length: number;
}

// CreatedThread is POST /forums/{slug}/threads: the new thread's header and
// its first post (internal/dto CreatedThread).
export interface CreatedThread {
  thread: ThreadHeader;
  post: CreatedPost;
}

// UserSuggestions is GET /users?prefix=, the composer's mention autocomplete
// (internal/dto UserSuggestions). Every handle is one the renderer resolves.
export interface UserSuggestions {
  users: Author[];
}

// Profile is GET /users/{handle}: an account as this viewer may see it
// (internal/dto Profile). A member session is required. post_count is the
// account's non-deleted posts in forums this viewer may read, and status is
// null except to moderators and admins, who are shown active, pending,
// suspended or deactivated. An anonymized account has its placeholder
// handle and empty display name, avatar and bio.
export interface Profile {
  user: Author;
  bio_html: string;
  joined_at: Millis;
  post_count: number;
  status: string | null;
}

// HandleChange answers PUT /settings/handle and POST
// /admin/users/{handle}/rename (internal/dto HandleChange). previous is
// reserved for 90 days and redirects members to handle. next_change_at is
// when the member may next change their own handle, null when they may now.
export interface HandleChange {
  handle: string;
  previous: string;
  next_change_at: Millis | null;
}

// HandleState is GET /settings/handle (internal/dto HandleState,
// TKT-01M3V6XTYG): the viewer's handle, and when they may next change it
// themselves, null when they may now. It is the available_at a PUT inside
// the cooldown is refused with.
export interface HandleState {
  handle: string;
  next_change_at: Millis | null;
}

// SessionList is GET /settings/sessions (TKT-01M29CCC0X): the viewer's live
// sessions, most recently used first. id names one to DELETE
// /settings/sessions/{id}; current is the request's own; device is a label
// read from user_agent; provider is the identity provider's key, empty when
// none was recorded.
export interface SessionInfo {
  id: string;
  current: boolean;
  device: string;
  user_agent: string;
  ip: string;
  provider: string;
  created_at: Millis;
  last_used_at: Millis;
}

export interface SessionList {
  sessions: SessionInfo[];
}

// IdentityList is GET /settings/identities (TKT-01M29CCC0X): the sign-ins
// linked to the account, oldest first. provider_name is empty, and
// configured false, once the operator has removed the provider. removable
// says whether DELETE /settings/identities/{id} would succeed now.
export interface IdentityInfo {
  id: number;
  provider: string;
  provider_name: string;
  configured: boolean;
  email: string | null;
  created_at: Millis;
  last_login_at: Millis | null;
  current: boolean;
  removable: boolean;
}

export interface IdentityList {
  identities: IdentityInfo[];
}

// IdentityRemoved answers DELETE /settings/identities/{id}. signed_out is
// true when the request's own session came through the removed identity:
// the cookie is cleared and the client is signed out.
export interface IdentityRemoved {
  signed_out: boolean;
}

// UserPosts is GET /users/{handle}/posts?cursor=: a page of the account's
// posts, newest first, across the forums this viewer may read
// (internal/dto UserPosts). next_cursor is empty at the end.
export interface UserPosts {
  posts: UserPost[];
  next_cursor: string;
}

// UserPost is one post in a history, as its thread page renders it for this
// viewer, and where it is (internal/dto UserPost).
export interface UserPost {
  thread: ThreadLink;
  forum: ForumRef;
  post: Post;
}

// NotificationList is GET /notifications?cursor=: a page of the viewer's
// notifications, the newest written first (internal/dto NotificationList).
// next_cursor is empty at the end. The client screen is TKT-01M29CTY86's.
export interface NotificationList {
  notifications: Notification[];
  next_cursor: string;
}

// Notification names what happened and where, and carries no post text
// (internal/dto Notification). thread, forum and ordinal are null for the
// application kinds; actor is null when there is none.
export interface Notification {
  id: number;
  kind: string;
  thread: ThreadLink | null;
  forum: ForumRef | null;
  ordinal: number | null;
  actor: Author | null;
  at: Millis;
  read: boolean;
}

// NotificationsRead answers POST /notifications/read (internal/dto
// NotificationsRead).
export interface NotificationsRead {
  marked: number;
}

// SearchResults is GET /search?q=&forum=&author=&after=&before=&cursor=: a
// page of hits in the forums this viewer may read, newest first, with no
// total (internal/dto SearchResults). next_cursor is empty on the last page.
// indexing is true while results may be missing.
export interface SearchResults {
  results: SearchResult[];
  next_cursor: string;
  indexing: boolean;
}

// SearchResult is one hit: a post, or the thread's title when ordinal is 0
// (internal/dto SearchResult). thread is the thread as it is now.
export interface SearchResult {
  thread: ThreadLink;
  forum: ForumRef;
  ordinal: number;
  author: Author;
  created_at: number;
  snippet: SnippetPart[];
}

// SnippetPart is a run of a snippet's plain text. It is never markup: the
// client escapes text and marks the parts whose match is true.
export interface SnippetPart {
  text: string;
  match: boolean;
}

// ReadPosition is PUT and DELETE /threads/{id}/read: the viewer's position as
// stored after the request, which a PUT never lowers and a DELETE never
// raises, so it can differ from the ordinal sent (internal/dto ReadPosition).
export interface ReadPosition {
  last_read_ordinal: number;
}

// MarkedRead is PUT /me/read and PUT /forums/{slug}/read, mark all read at
// site and forum level: the moment up to which everything is now read
// (internal/dto MarkedRead, TKT-01M29CTY7K).
export interface MarkedRead {
  at: Millis;
}

// ThreadWatch is POST /threads/{id}/watch: the thread's own level and its
// forum's after the request, as the viewer block names them (internal/dto
// ThreadWatch).
export interface ThreadWatch {
  watch: string;
  forum_watch: string;
}

// ForumWatch is POST /forums/{slug}/watch: the forum's level after the
// request, the thread list's watch (internal/dto ForumWatch).
export interface ForumWatch {
  watch: string;
}

// FlagReceipt is POST /threads/{id}/flags: that the report was received and
// nothing else, the same for every accepted report (internal/dto
// FlagReceipt).
export interface FlagReceipt {
  received: boolean;
}

// Draft is GET and PUT /drafts/{thread_id}: the viewer's one reply draft for
// the thread. GET answers 204 with no body when there is none
// (internal/dto Draft).
export interface Draft {
  thread_id: string;
  source: string;
  reply_to: number | null;
  // attachments are the composer's files, never null (TKT-01M3V73DP6).
  attachments: Attachment[];
  updated_at: Millis;
  // revision only grows, whatever either clock does; a 204 carries it as
  // the Draft-Revision header.
  revision: number;
}

// ForumDraft is GET and PUT /forums/{slug}/draft: the viewer's one
// new-thread draft for the forum. title is as typed, and absent when the
// draft has none. GET answers 204 with no body when there is none
// (internal/dto ForumDraft).
export interface ForumDraft {
  forum: string;
  title?: string;
  source: string;
  attachments: Attachment[];
  updated_at: Millis;
  revision: number;
}

// The admin users, groups and permissions screens (internal/dto admin.go,
// TKT-01M29CXK1X). Types only: nothing here reaches the reader's bundle.

export interface AdminUser {
  id: number;
  user: Author;
  status: string;
  created_at: Millis;
  last_seen_at: Millis | null;
}

export interface AdminUserList {
  users: AdminUser[];
  next_cursor: string;
}

export interface GroupRef {
  id: number;
  slug: string;
  name: string;
}

export interface Membership {
  group: GroupRef;
  source: string;
  added_by: Author | null;
  added_at: Millis;
}

export interface AdminIdentity {
  provider: string;
  email: string | null;
  email_verified: boolean;
  last_login_at: Millis | null;
}

export interface AdminUserDetail {
  user: AdminUser;
  groups: Membership[];
  identities: AdminIdentity[] | null;
  sessions: number | null;
  // agent is an agent account's credential and limits: admins only, null
  // for a moderator and for a human.
  agent: AgentInfo | null;
  // suspension is the suspension in force, to moderators and admins alike;
  // null unless the account is suspended (TKT-01M3QMRE47).
  suspension: AdminSuspension | null;
}

// AdminSuspension is a suspension's reason, which the member is shown at
// sign-in, and its end, null for one with no end.
export interface AdminSuspension {
  reason: string;
  until: Millis | null;
}

export interface AgentInfo {
  credential: AgentCredential | null;
  // burst and refill_seconds are null when the agent uses the site default.
  burst: number | null;
  refill_seconds: number | null;
  default_burst: number;
  default_refill_seconds: number;
}

export interface AgentCredential {
  created_at: Millis;
  last_used_at: Millis | null;
}

// IssuedCredential carries the only copy of an agent's token there will be.
export interface IssuedCredential {
  token: string;
  created_at: Millis;
}

export interface CreatedAgent {
  user: AdminUserDetail;
  credential: IssuedCredential | null;
}

export interface Group {
  id: number;
  slug: string;
  name: string;
  description: string;
  system: boolean;
  members: number;
  grants: number;
}

export interface GroupList {
  groups: Group[];
}

export interface GroupMember {
  user: AdminUser;
  source: string;
  added_by: Author | null;
  added_at: Millis;
}

export interface GroupDetail {
  group: Group;
  members: GroupMember[];
}

export interface AdminForum {
  id: number;
  slug: string;
  name: string;
  visibility: string;
}

// ManagedForumList is GET /admin/forums (TKT-01M29CXK19), and the answer
// to every write there: every forum, roots each followed by their children.
export interface ManagedForumList {
  forums: ManagedForum[];
}

// ManagedForum is a forum as the admin forum screen edits it. parent_id is
// null for a root; position only orders siblings.
export interface ManagedForum {
  id: number;
  parent_id: number | null;
  slug: string;
  name: string;
  description: string;
  position: number;
  // public, members or restricted.
  visibility: string;
  archived: boolean;
  threads: number;
  posts: number;
}

export interface Grant {
  forum_id: number | null;
  group_id: number;
  permission: string;
}

export interface PermissionGrid {
  groups: Group[];
  forums: AdminForum[];
  grants: Grant[];
}

// Each is allow, deny or hidden.
export interface Decisions {
  read: string;
  create_thread: string;
  reply: string;
  upload: string;
  moderate: string;
}

export interface ForumEffect {
  forum: AdminForum;
  before: Decisions;
  after: Decisions;
}

export interface PermissionPreview {
  user: AdminUser;
  forums: ForumEffect[];
}

// Form is a form version as a client renders it (internal/dto Form):
// the apply screen and the form editor's preview. version is 0 for an
// unsaved draft. intro_html and a notice's html are rendered by the server.
export interface Form {
  key: string;
  version: number;
  title: string;
  intro_html: string;
  fields: FormField[];
}

// FormField is one field of a Form (internal/dto FormField). Every property
// is present: max is the effective limit, 0 for a type without text;
// options is empty except on a select; html is a markdown_notice's body.
export interface FormField {
  key: string;
  type: string;
  label: string;
  required: boolean;
  max: number;
  options: string[];
  multiple: boolean;
  locked: boolean;
  from_claim: string;
  html: string;
}

// FormAdmin is GET /admin/forms/{key} and its versions, and the answer to a
// save (internal/dto FormAdmin). Admins only. schema is the stored schema,
// which the editor edits and sends back.
export interface FormAdmin {
  key: string;
  title: string;
  active_version: number;
  versions: FormVersionInfo[];
  form: Form;
  schema: FormSchema;
}

// FormVersionInfo is one version in a form's history, newest first.
// created_by is null for the version the migration seeded.
export interface FormVersionInfo {
  version: number;
  created_at: Millis;
  created_by: Author | null;
}

// FormSchema is a form version's schema_json (docs/05 "Forms"). The stored
// form is canonical: a property left at its default is absent.
export interface FormSchema {
  title: string;
  intro_markdown?: string;
  fields: FormSchemaField[];
}

// FormSchemaField is one field of a FormSchema. Which optional properties
// a field may carry depends on its type; the server holds the rules.
export interface FormSchemaField {
  key: string;
  type: string;
  label?: string;
  required?: boolean;
  max?: number;
  options?: string[];
  multiple?: boolean;
  from_claim?: string;
  locked?: boolean;
  markdown?: string;
}

// ApplicationForm is GET /apply/form (internal/dto ApplicationForm): the
// active registration form, and the applicant's claims keyed by the field
// they prefill. A locked field is stored as its prefill whatever is sent.
export interface ApplicationForm {
  form: Form;
  prefill: Record<string, string>;
}

// ApplicationStatus is GET /apply/status, and the answer to POST /apply and
// POST /apply/withdraw (internal/dto ApplicationStatus). status is none,
// pending, approved, denied or withdrawn. can_apply_at is set only when the
// 24-hour rule alone stands in the way, and sign_in only when approved.
export interface ApplicationStatus {
  status: string;
  provider: string;
  submitted_at: Millis | null;
  decided_at: Millis | null;
  can_withdraw: boolean;
  can_apply: boolean;
  can_apply_at: Millis | null;
  sign_in: string | null;
}

// The applications review screen (internal/dto applications.go,
// TKT-01M29CP2MG). Staff only; nothing here reaches an applicant.

// AdminApplicationRow is one application in the review list. provider is
// null once the identity was removed, and email when there is none.
export interface AdminApplicationRow {
  id: number;
  status: string;
  created_at: Millis;
  provider: string | null;
  email: string | null;
  email_verified: boolean;
}

// AdminApplicationList is GET /admin/applications, newest first.
export interface AdminApplicationList {
  applications: AdminApplicationRow[];
  next_cursor: string;
}

// The moderator queue (internal/dto queue.go, TKT-01M29CRJ4E): open flags,
// one item per flagged post, and pending applications, newest first. A flag
// item appears only in a forum the viewer moderates, and an application only
// to staff.

// QueueReport is one open flag on a post. Reporters are shown to moderators
// only.
export interface QueueReport {
  id: number;
  // spam, abuse, off-topic or other.
  reason: string;
  note: string;
  reporter: Author;
  created_at: Millis;
}

// QueueFlag is a flagged post and every flag open on it. post is as a
// moderator reads it, and null when the thread cannot be read just now.
export interface QueueFlag {
  thread: { id: string; slug: string; title: string };
  ordinal: number;
  post: Post | null;
  flags: QueueReport[];
}

// QueueItem is one thing to decide: flag is set for kind "flag", and
// application for kind "application".
export interface QueueItem {
  // flag or application.
  kind: string;
  created_at: Millis;
  flag: QueueFlag | null;
  application: AdminApplicationRow | null;
}

// Queue is GET /admin/queue: one page. next_cursor is empty at the end.
export interface Queue {
  items: QueueItem[];
  next_cursor: string;
}

// QueueResolution is POST /admin/queue/posts/{id}/{ordinal}'s answer: the
// action asked (resolve, which is "Keep post", dismiss or remove), what
// happened to the post (hidden, restored, already hidden or unchanged), and
// the flags it closed.
export interface QueueResolution {
  action: string;
  post: string;
  flags: number[];
}

// AuditPage is GET /admin/audit, an admin's alone: one page of the audit
// log, newest first. actions is every action a row is written with today,
// for the action filter. next_cursor is empty at the end.
export interface AuditPage {
  entries: AuditEntry[];
  actions: string[];
  next_cursor: string;
}

// AuditEntry is one row. actor is null when no account did it, and details
// then say what did, as "via". related is a thread.moved row's forums, the
// one it left first, and empty for every other row. details is data to
// show as text, never markup; ip is empty when there was no request.
export interface AuditEntry {
  id: number;
  at: Millis;
  actor: Author | null;
  action: string;
  target: AuditTarget;
  related: AuditTarget[];
  details: Record<string, unknown>;
  ip: string;
}

// AuditTarget is what a row names: type and id as written, the label it
// reads as now, and href, empty when it has no page.
export interface AuditTarget {
  type: string;
  id: string;
  label: string;
  href: string;
}

// SiteSettings is GET /admin/settings, an admin's alone (TKT-01M29CXK2H):
// every runtime setting in the registry, sorted by key, and every
// config.toml section, named with its reason and no values.
export interface SiteSettings {
  settings: SiteSetting[];
  config: ConfigSection[];
}

// SiteSetting is one key, and the answer to PUT /admin/settings/{key}.
// default and value are of the key's kind. allowed says in words what the
// key takes, and choices is a closed list's values, empty for any other key.
// Set from the screen, updated_at and updated_by are both set; from the
// command line, updated_by is null; never set, both are null.
export interface SiteSetting {
  key: string;
  doc: string;
  // "text", "integer" or "boolean".
  kind: string;
  allowed: string;
  choices: string[];
  default: SettingValue;
  value: SettingValue;
  is_default: boolean;
  updated_at: Millis | null;
  updated_by: Author | null;
}

export type SettingValue = string | number | boolean;

// ConfigSection is one top-level part of config.toml: why it lives in the
// file rather than on the settings screen, and the keys under it.
export interface ConfigSection {
  name: string;
  reason: string;
  keys: string[];
}

// AdminApplication is GET /admin/applications/{id}, and the answer to
// approve, deny and unblock. form is the version the application answered,
// and prior the identity's applications submitted before it. identity is
// null once removed, and decision while pending.
export interface AdminApplication {
  id: number;
  status: string;
  created_at: Millis;
  form: Form;
  answers: Record<string, unknown>;
  identity: ApplicantIdentity | null;
  prior: AdminApplicationRow[];
  notes: ApplicationNote[];
  suggested_handle: string;
  decision: ApplicationDecision | null;
}

export interface ApplicantIdentity {
  provider: string;
  email: string | null;
  email_verified: boolean;
  blocked: boolean;
}

// ApplicationNote is one moderator's note, and the answer to POST
// /admin/applications/{id}/notes.
export interface ApplicationNote {
  id: number;
  author: Author;
  body: string;
  created_at: Millis;
}

// ApplicationDecision: by is null for a withdrawal, user unless approved.
export interface ApplicationDecision {
  by: Author | null;
  at: Millis;
  note: string;
  user: Author | null;
}
