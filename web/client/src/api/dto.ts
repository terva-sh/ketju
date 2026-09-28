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
