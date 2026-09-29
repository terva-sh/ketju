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
}

export interface ThreadHeader {
  id: string;
  slug: string;
  title: string;
  forum: ForumRef;
  post_count: number;
  log_length: number;
  locked: boolean;
  pinned: Pinned | null;
  tags: string[];
  created_at: Millis;
}

export interface ThreadViewer {
  last_read_ordinal: number;
  watch: string;
  can_reply: boolean;
  can_moderate: boolean;
}

export interface Post {
  ordinal: number;
  revision: number;
  author: Author;
  created_at: Millis;
  edited_at: Millis | null;
  reply_to: number | null;
  reply_to_suppressed: boolean;
  reply_count: number;
  html: string;
  attachments: Attachment[];
  deleted: boolean;
  moved_to: string | null;
  machine_authored: boolean;
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
}

export interface PostHistory {
  ordinal: number;
  deleted: boolean;
  revisions: PostRevision[];
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
  unread: number;
  last_read_ordinal: number;
}

export interface ThreadList {
  forum: ForumRef | null;
  threads: ThreadRow[];
  next_cursor: string;
}

export interface Forum {
  slug: string;
  name: string;
  description: string;
  visibility: string;
  archived: boolean;
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
}

export interface UnreadCounts {
  threads: number;
  participated: number;
  watching: number;
}

export interface Settings {
  timezone: string;
  locale: string;
  page_size: number;
}

export interface Me {
  user: Author | null;
  // Set only for an approval-mode applicant, whose user is still null.
  pre_user: { provider: string } | null;
  unread: UnreadCounts;
  notifications: number;
  settings: Settings;
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

// ReadPosition is PUT and DELETE /threads/{id}/read: the viewer's position as
// stored after the request, which a PUT never lowers and a DELETE never
// raises, so it can differ from the ordinal sent (internal/dto ReadPosition).
export interface ReadPosition {
  last_read_ordinal: number;
}

// Draft is GET and PUT /drafts/{thread_id}: the viewer's one reply draft for
// the thread. GET answers 204 with no body when there is none
// (internal/dto Draft).
export interface Draft {
  thread_id: string;
  source: string;
  reply_to: number | null;
  updated_at: Millis;
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
