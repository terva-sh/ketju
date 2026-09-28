# 07 — API and Clients

## Principles

- **One service layer, two presentation layers.** `/api/*` (JSON) and, later,
  `/…` (HTML) are thin handlers over `internal/service`. Neither contains
  authorization or validation logic of its own. This is the concrete
  mechanism that makes a non-JS client cheap later.
- **Shared DTOs.** The structs serialized by the JSON API are the structs
  handed to HTML templates. Golden JSON tests are the contract.
- **URLs are the same in both clients.** The SPA's routes are exactly the
  paths the HTML client will serve. Deep links never break when the client
  changes.
- **Cookies, not tokens, for browsers.** The SPA calls the API with the
  session cookie. Bearer tokens in `Authorization` are a separate path and are
  never issued to browser code. Two things travel it. **Agent credentials**,
  which an admin issues when provisioning an agent account (05), land in
  Phase 2. **Personal API tokens** for a human's own scripts stay a later
  phase. The first is narrower than the second and does not wait for it.

## URL scheme (both clients)

```
/                                   forum index
/f/{forum-slug}                     thread list
/f/{forum-slug}/new                 new thread composer
/t/{id}/{slug}                      thread page 1
/t/{id}/{slug}?page=N&lens=…        thread page N with lens
/t/{id}/{slug}/{ordinal}            permalink (redirects/anchors to the right page)
/t/{id}/{slug}/{ordinal}/history    revision history
/unread  /new  /participated  /watching  /notifications  /search?q=…
/u/{handle}  /u/{handle}/posts
/settings  /settings/sessions  /settings/identities
/apply  /apply/status
/admin  /admin/queue  /admin/applications  /admin/forums  /admin/users  /admin/forms  /admin/settings
/auth/{provider}/start  /auth/{provider}/callback  /auth/logout (POST)
/auth/register (GET, POST)
```

`/auth/*` is the server's in both clients: plain HTML pages and redirects,
not JSON. `/auth/register` is open registration's handle screen (05
"Registration path"). `GET` shows one field, `handle`, prefilled, and needs
the `__Host-ketju_reg` cookie the callback sets; without it, or after its
fifteen minutes, the page says to sign in again (400). `POST` takes the form
field `handle` as `application/x-www-form-urlencoded`, held to this site by
the cross-origin check (see "CSRF posture"). Success sets the session cookie, clears
the registration cookie and answers 303 to the sign-in's `return_to`. An
invalid or reserved handle is 422 and a taken one 409, each with the form
shown again and the reason beside the field. An identity registered in the
meantime is 409, registration closed in the meantime is 403, and a browser
that signed in as a member in the meantime is 409: a member adds an identity
by linking it, never by registering a second account.

`{id}` is the base32 thread id; `{slug}` is advisory — a stale slug
redirects to the current one.

## JSON API

Base path `/api/v1`. JSON in, JSON out. Errors are RFC 9457 problem
details (`application/problem+json`) with a stable `type` code. Lists are
cursor-paged where the underlying order is a log or time (`?cursor=`), and
page-numbered for thread posts (`?page=`), matching the UI.

### Resources (v1)

| Method & path | Purpose |
|---------------|---------|
| `GET  /me` | viewer: user, role, unread counts, pending notifications count, settings. An approval-mode applicant (a pre-user session) gets `user: null` plus `pre_user: {provider}`; a null user stays the only signed-out signal. Two statements for a member: the session lookup and one count query, plus one batched forum-access read while the permission cache is cold. Neither count reads every thread (docs/04 `forum_thread_counts`) |
| `GET  /forums` | tree of forums with per-forum unread counts → `internal/dto` `ForumTree`. A forum the viewer may not read is absent, not flagged, and so are its children; an anonymous viewer sees public forums only. Roots come in position order with their children, one level deep, under them. `thread_count` is threads with a post; `unread` is the viewer's threads with an unread post, by `/me`'s rule (read positions and `mark_all_read_at`, mutes not counted), and 0 for an anonymous viewer; `last_post` is the most recently active thread, its last poster and the time, or null. Each is the forum's own, not its children's. `no-store` |
| `GET  /forums/{slug}/threads?cursor=&sort=` | thread list rows → `internal/dto` `ThreadList`, answered from projections alone. The first page opens with the pins: global pins from every forum the viewer may read, then this forum's own, each by latest activity, at most 50; a pin past its `pinned_until` is no pin, and pins past the 50 sort with the rest. Then 25 threads with a post, newest `last_post_at` first, or newest `created_at` first for `sort=created`; the pins are not counted. `next_cursor` is opaque, bound to its forum and sort, and empty at the end; paging is keyset, so a thread bumped between pages moves up rather than appearing twice. Every page leaves out exactly the forum's threads the first page showed as pins, which the cursor carries, so a pin that ends between pages is not shown again. A cursor is a position, not a credential: one that is malformed or was made for another forum or sort, or any other `sort`, is `bad-request`. `unread` is the viewer's unread posts by the per-post rule (read position and `mark_all_read_at`) and `last_read_ordinal` their position, both 0 for an anonymous viewer. Access answers as the thread page does: an unknown or hidden forum is `not-found`, a signed-in-only one `unauthenticated` to an anonymous viewer, `forbidden` to a member without read. `no-store` |
| `POST /forums/{slug}/threads` | create thread `{title, source, tags}` → 201 `{thread, post}`, `internal/dto` `CreatedThread`: the thread page's header and a `CreatedPost` for post 1, each as a later read serves it. A member session is required (anonymous `unauthenticated`, pre-user `forbidden`); a forum hidden from the viewer is `not-found`, one without `create_thread` is `forbidden`, an archived one is `conflict`. The title is trimmed and must be 1–200 characters with no control characters; the slug is derived from it (an ASCII fold, at most 60 characters). `tags` must be absent or empty until tag rules exist. The source is checked as a reply's is. The thread is projected before the response, so it can be opened at once |
| `GET  /threads/{id}?page=&ordinal=&lens=&after_log_length=` | thread header + page of rendered posts + lens meta + viewer read position |
| `POST /threads/{id}/posts` | `{source, reply_to?, attachments?}` → 201 `{post, ordinal, revision, log_length}`, `internal/dto` `CreatedPost`: the post as `GET /threads/{id}/posts/{ordinal}` serves it, written after the record is durable. A member session is required; without `reply` it is `forbidden`, and not-found or unauthenticated as for any read of the thread. A locked thread is `conflict` except to a moderator; an archived one is `conflict` to everyone. Slow mode (`slow_mode.default_seconds`, off by default; moderators exempt) is `rate-limited` with `Retry-After` set to the seconds left. An empty source, a `reply_to` naming no post, and any attachment (nothing uploads yet) are `validation-failed`; an over-long source is `payload-too-large`. Every refusal comes before anything is appended. Charged to the per-address write limiter |
| `GET  /threads/{id}/posts/{ordinal}` | single post (popover, permalink) |
| `GET  /threads/{id}/posts/{ordinal}/replies` | direct replies (inline expander) |
| `GET  /threads/{id}/posts/{ordinal}/history` | revisions, each with actor, time, reason and source; the client diffs them |
| `PATCH /threads/{id}/posts/{ordinal}` | edit `{source, reason?}` |
| `DELETE /threads/{id}/posts/{ordinal}` | soft delete; `POST …/restore` to undo |
| `PUT  /threads/{id}/read` | `{ordinal}` advance read position; `DELETE` marks unread. Both answer 200 `internal/dto` `ReadPosition`; see "Read position" below |
| `POST /threads/{id}/watch` `{level}` | watch/mute |
| `POST /threads/{id}/flags` | `{ordinal, reason, note}` |
| `POST /render` | `{source, thread_id?, reply_to?}` → `{html, mentions: [{handle}], quotes: [{ordinal, deleted}], reply_to}` (composer preview; the create path records the same facts from the same render, `internal/dto` `RenderResult`). A member session is required (anonymous `unauthenticated`, pre-user `forbidden`). With `thread_id` the viewer must be able to read the thread, quotes resolve against it and `reply_to` is checked (`validation-failed` when no such post; a deleted post is accepted); without it quotes render optimistically and `reply_to` is `bad-request`. Charged to the per-address write limiter; the body limit is the post limit |
| `GET  /drafts/{thread_id}` `PUT` `DELETE` | composer drafts, one per user per thread → `internal/dto` `Draft`; see "Drafts" below |
| `GET  /unread` `/new` `/participated` `/watching` | cross-forum thread lists |
| `GET  /search?q=&forum=&author=&after=&before=` | FTS5 results with snippets |
| `GET  /notifications` `POST /notifications/read` | |
| `POST /blobs` (multipart) `GET /blobs/{hash}` | attachments; GET is outside `/api` at `/blobs/…` with immutable caching |
| `GET  /users/{handle}` `/users/{handle}/posts` | profiles. `/users/{handle}` answers `{user: author, bio_html, joined_at, post_count, status}`, `internal/dto` `Profile`: `bio_html` is the bio rendered as a post is (a quote in it links nowhere, having no thread), and `status` is null except to moderators and admins. `/users/{handle}/posts?cursor=` answers `{posts: [{thread, forum, post}], next_cursor}`, `internal/dto` `UserPosts`: each post rendered as its thread page renders it for the viewer, with a link to its thread and forum, newest first, 25 to a page. Paging is keyset on (created_at, thread, ordinal) descending, so a post written while a reader pages does not appear twice, and a page is short only at the end of the history; the cursor is opaque, names no account, and one that is malformed is `bad-request`. A moved post is left out. Whether a post is deleted is read from its thread's index rather than from `post_meta`, which trails the log, and the count reads it the same way, so the history and the count agree while the projection catches up. The handle matches case-insensitively and an unknown one is `not-found`. An anonymized account shows its `deleted-user-<id>` handle and no display name, avatar or bio, in the profile and on every post in its history; until the `users.anonymized` flag of 05 exists, the reserved handle prefix is what marks one. Both answers are `no-store`. A member session is required for both, as for `GET /users?prefix=` (anonymous `unauthenticated`, pre-user `forbidden`). The post count counts the user's non-deleted posts in forums this viewer can read, so it varies by viewer and says nothing about forums the viewer cannot read. The history spans forums and leaves out posts in forums the viewer cannot read. A deleted post is left out for other members; the author sees their own deleted posts, marked deleted and without a body, as their thread page shows them, and moderators and admins see them marked deleted with the body, as the thread page's moderator view does. A post hidden by a moderator is a deleted post here, as it is on the thread page (08). A deleted post never counts, even for a viewer whose history shows it, so the count equals the posts a member's history lists and is less than an author's, moderator's or admin's history by the deleted posts it shows. A pending account is `not-found` except to moderators and admins; a suspended or deactivated account's profile is served, and its status is shown only to moderators and admins (05, "Account lifecycle"). Decided by the maintainer on 2026-09-28 (TKT-01M3KJJNDC) |
| `GET  /users?prefix=&limit=` | `{users: [author]}`: mention autocomplete, `internal/dto` `UserSuggestions`. Up to `limit` (default 8, at most 20) active members whose handle starts with `prefix`, case-insensitively, in handle order; a prefix no handle can start with is an empty list. Every handle offered is one the renderer resolves as a mention, so a handle ending in `-`, which the mention syntax reads as punctuation, is not offered. A member session is required (anonymous `unauthenticated`, pre-user `forbidden`) |
| `GET  /settings` `PATCH /settings` | prefs; `GET/DELETE /settings/sessions` ; `GET/DELETE /settings/identities/{id}` |
| `GET  /apply/form` `POST /apply` `GET /apply/status` `POST /apply/withdraw` | approval mode (pre-user session) |
| `GET  /events?threads=&forums=` | SSE (outside `/api/v1` versioning: `/events`) |

### Error catalogue

Every error response is `application/problem+json` carrying `type`, `title`
and `status`. `type` is the stable code; clients switch on it. Titles are
fixed text and never interpolate anything from the request.

| `type` | Status | Meaning |
|--------|--------|---------|
| `bad-request` | 400 | Unparseable JSON, a non-numeric parameter, a thread id that is not base32 |
| `unauthenticated` | 401 | No viewer. The client offers a sign-in |
| `forbidden` | 403 | There is a viewer and they may not do this. The client says so, and does not offer a sign-in |
| `not-found` | 404 | No such endpoint *or* no such object — deliberately one code, so the API does not disclose which thread ids exist |
| `method-not-allowed` | 405 | Right route, wrong verb |
| `conflict` | 409 | Lost a race, or met the object's state: posting to a closed thread |
| `gone` | 410 | Existed and was deleted. Distinct from `not-found` so a permalink can say what happened |
| `payload-too-large` | 413 | Over a body limit: 64 KB for a post source, the attachment cap for an upload |
| `unsupported-media-type` | 415 | A mutating request that is not JSON |
| `validation-failed` | 422 | Understood and refused: an empty post, a title over the limit |
| `rate-limited` | 429 | Token bucket. Carries `Retry-After` in whole seconds |
| `internal` | 500 | A bug, or an error nothing has classified |
| `not-implemented` | 501 | An endpoint this build does not serve — distinct from `not-found`, which means no such endpoint exists |
| `unavailable` | 503 | Draining or a dependency is down. Retryable, unlike `internal` |

These strings are API surface: renaming one or moving it to another status is
a breaking change. The catalogue lives in `internal/problem`, and
`internal/problem/testdata/catalogue.json` is a golden file, so any change to
a code, status or title shows up in a diff.

No error body ever carries the underlying error text. A database constraint
name describes the schema to whoever provoked it and means nothing to a
member, so the error goes to the log and the client gets the fixed title.

### Moderation and admin (`/api/v1/admin/*`, role-gated)

Queue (flags + applications), application approve/deny, thread
lock/pin/move/rename/tag, split/merge, user suspend/anonymize, thread
archive, forum CRUD and ordering, group and permission editing, form
versions, settings, audit log query, `reindex` and `fsck` triggers with
progress via SSE.

Each route is gated on the role docs/08's "Primitives" table names for its
action, which is the one place authority is stated. Archiving a thread is
admin-only there; locking, pinning, moving, renaming, retagging, splitting
and merging are moderator actions.

### Thread response shape (abridged)

```json
{
  "thread": {"id": "3yymw47g00001", "slug": "why-we-left-slack", "title": "…",
             "forum": {"slug": "general", "name": "General"},
             "post_count": 212, "log_length": 483920, "locked": false, "pinned": null,
             "tags": ["meta"], "created_at": 1757500000000},
  "page": {"number": 3, "size": 25, "total_pages": 9},
  "lens": {"kind": "author", "value": "drew", "shown": 7, "hidden": 205},
  "viewer": {"last_read_ordinal": 60, "watch": "watch", "can_reply": true, "can_moderate": false},
  "posts": [
    {"ordinal": 61, "revision": 2, "author": {"handle": "…", "display_name": "…", "avatar": "…", "role": "member", "kind": "human"},
     "created_at": 1757500100000, "edited_at": 1757500200000,
     "reply_to": 57, "reply_to_suppressed": false, "reply_count": 3,
     "html": "<p>…</p>", "attachments": [], "deleted": false, "moved_to": null,
     "machine_authored": false}
  ]
}
```

`ETag: "3yymw47g00001-483920-r3"`; `Vary: Cookie`. The viewer block is the
only per-user part, and it is cheap; everything above it comes from the
response cache keyed on `(thread, page, lens, log_length, renderer_version,
viewer_class)` where `viewer_class ∈ {anon, member, moderator}` (moderators
see tombstone contents).

#### What the thread endpoint does (TKT-01M29CA9AX)

- **Which page.** `?ordinal=N` is the permalink form: it serves the page
  holding post N and wins over `page`. With neither, a signed-in viewer who
  has read the thread lands on the page holding their first unread post (the
  last page once they have read everything); anyone else gets page 1. An
  ordinal or page past the end is `not-found`.
- **The ETag** is `"<id>-<log_length>-r<renderer>-<digest>"`, where the
  digest hashes the page body as written. The prefix is the thread's version
  and the pipeline, for a person reading a log. The digest is what makes the
  tag correct: a page also depends on the projection, the users and forums
  tables, the viewer's own state, permissions and the query, and hashing the
  body means two different bodies never share a tag without a list of those
  inputs to keep up to date. The cost is that a 304 still assembles the page
  (from the render cache, mostly); what it saves is the transfer.
  `If-None-Match` accepts a list, `W/` tags and `*`. The response is
  `Cache-Control: private, no-cache`: always revalidate, and a 304 when
  nothing moved.
- **Consistency.** The length, post count, the page's entries and reply
  counts come from one snapshot of the index (`threadlog.Actor.Stable`), and
  the posts are read through it. Quote tombstones and mentions are resolved
  when the page is drawn, so a body may reflect a delete a moment newer than
  its `log_length`; the delete's own event then prompts a refetch. A cached
  rendering is reused only while the posts it quotes keep their deleted
  state and the handles it mentions resolve as they did; a failed handle
  lookup fails the page rather than drawing the mention as unresolved.
- **`after_log_length=L`** keeps the posts *on the requested page* whose
  current revision was written at or after `L`: new posts and edits. The
  header and viewer block are complete. A delete or restore writes no new
  revision of the post, so a client that sees `log_length` move without a
  post to account for it refetches the page.
- **`watch`** is `"watch"`, `"mute"`, or `""` for neither.
- **Access.** A thread the viewer may not see is `not-found`, as if it did
  not exist. A readable-when-signed-in thread is `unauthenticated` to an
  anonymous viewer and `forbidden` to a member without read. `lens` is not
  served yet and is `bad-request`, not ignored.
- **Goldens.** `internal/dto/testdata/golden` fixes the shape;
  `internal/web/api/testdata/golden/thread.{anon,member,moderator}.json` are
  what the endpoint actually serves for one seeded thread, regenerated with
  `go test ./internal/web/api -update`.

#### The response cache (TKT-01M29CCBY0)

`internal/service/respcache` holds a page's posts as serialized JSON, bounded
by `storage.response_cache_mb` and evicted least-recently-used first. The key
above is the design's; the implementation settles four internals:

- **Only the posts are cached.** The header and pagination come from the
  projection row, the forum and the tags, which the request reads anyway for
  its access check, so a forum rename is never pinned. The viewer block is
  built for each request and is never stored.
- **The key adds** the page size (per user, docs/06), the projection's log
  length beside the index's (settled in grooming), and a digest of the
  page's authors' rows as this request read them. The digest does the job
  grooming gave a users-generation counter, and also sees a rename, a role
  change or an anonymization written by another process, such as
  `ketju admin promote`.
- **Viewer class** is the one the access check computes: `moderator` when
  the viewer may moderate the thread's forum (an admin may), `member` for an
  active member, `anon` otherwise, a suspended account included. A role
  change is seen on the next request, because the session reads the role
  from the users row every time.
- **What the key cannot name** is kept beside the entry and checked on every
  hit, as the render cache does: the handles the posts' mentions resolved
  and the state of the posts they quote. A user registering a mentioned
  handle is seen at once. A failed check is a miss, and the fresh page
  replaces the entry.

There is no invalidation and the package exports no way to forget an entry.
A request with `after_log_length` bypasses the cache. `Stats` reports the
hit ratio for the metrics endpoint (TKT-01M29CEE7S).

#### The post endpoints (TKT-01M29CA9C2)

`GET /threads/{id}/posts/{ordinal}` is a bare `Post`, the same assembly the
page uses for one ordinal, so the popover and the page never disagree. Its
`/replies` is `{ordinal, posts}` with the direct replies in ordinal order,
answered from the index when the thread is open and from `post_meta` when it
is not; `post_meta` is believed only once the projection has reached the
length being served, so a reply it has not seen yet is not dropped. Its
`/history` is `{ordinal, deleted, revisions: [{revision, actor, at, reason,
source}]}`.

- Access is the thread page's: the same 404, 401 and 403.
- History is a members' feature: an anonymous viewer is `unauthenticated`,
  a member is `forbidden` when `public_edit_history` is off, and a
  moderator may always read it.
- A deleted post is a tombstone from all three to anyone but a moderator:
  no HTML from the first two, and `deleted: true` with no revisions from
  history, since the old revisions are the deleted content by another route.
- Each carries the same body-hash ETag and answers `If-None-Match` with 304.
- History walks the whole log (revisions are not chained in the format), so
  it is for a person opening it, not for a page.

#### Edit, delete and restore (TKT-01M29CA9BP)

`PATCH /threads/{id}/posts/{ordinal}` takes `{source, reason?}` and appends an
`Edit`; `DELETE` takes an optional `{reason}` body (none at all is fine) and
appends a `Delete`; `POST …/restore` takes the same optional body and appends a
`Restore`. Each answers 200 with `internal/dto` `CreatedPost`, the shape a
reply answers with: the post as `GET /threads/{id}/posts/{ordinal}` now serves
it to this viewer (a tombstone after a delete), its revision, and a log
length, all three from one snapshot of the index taken after the append, so
they describe the same log, one at least as new as the write. `no-store`, and
charged to the write limiter. The optional reason body is read to at most
4 KiB; past that it is `payload-too-large`.

- A member session is required (anonymous `unauthenticated`, pre-user
  `forbidden`), and access to the thread is the thread page's.
- Only the post's author may act for now; anyone else, a moderator included,
  is `forbidden`. The author windows (TKT-01M29CKRHS) and a moderator's reach
  over every post (TKT-01M29CRJ5J) extend one `authorize` function in
  `internal/service/threads`. A post hidden by a moderator is `forbidden` to
  its author. An edit also needs `reply` in the forum; a delete or restore of
  one's own post does not.
- An archived thread is `conflict` for all three. A locked thread is
  `conflict` for a member's edit only, since a lock refuses posts and edits
  (08).
- The source is checked and rendered as a reply's is, against the thread and
  the post's own `reply_to`, which an edit keeps. The `Edit` records that
  `reply_to` and the mentions the render found.
- An edit or a delete of a post that is deleted, hidden or moved, and a
  restore of one that is not deleted, is `conflict` and appends nothing. The
  thread's actor checks this against the post's flags as they stand when the
  record is written, so a delete racing an edit cannot be undone by it.
  Deleting twice is `conflict` rather than a quiet success, so the log keeps
  no records that change nothing.

#### Read position (TKT-01M29CA9CE)

`PUT /threads/{id}/read` takes `{ordinal}`: the viewer has read up to and
including that post. `DELETE /threads/{id}/read` takes an optional
`{ordinal}` body (none at all is fine): every post after that ordinal is
unread, or the whole thread with no body. Both answer 200 with `internal/dto`
`ReadPosition`, `{last_read_ordinal}`, which is the position as stored after
the request and not always the ordinal sent. It is the same integer as the
thread page's `viewer.last_read_ordinal`. `no-store`, and charged to the
write limiter.

- **A `PUT` never moves the position back.** The server keeps the larger of
  the stored position and the one sent, in one statement, so a beacon that
  arrives late, out of order, or from a second device leaves a higher
  position where it is, and the answer says so. A client needs no ordering
  of its own for correctness. It should still not send a position below one
  it has already had answered, because that request does nothing.
- **An ordinal past the thread's last post** is read as the last post. An
  ordinal of 0 or below, or a missing one, is `bad-request`.
- **A `DELETE` is the only way the position goes down, and it never goes
  up.** Marking unread after a post the viewer has not reached changes
  nothing. With no stored position it writes one at 0.
- **Mark-unread then a late beacon.** A beacon sent before a `DELETE` and
  arriving after it moves the position forward again, since the server
  cannot tell it from a new read. A client flushes its pending beacon before
  it sends a `DELETE`, and sends nothing for that thread until the `DELETE`
  has answered.
- **`mark_all_read_at` still applies.** A post written before the viewer's
  mark counts as read whatever the position says (04 `read_positions`), so a
  `DELETE` cannot make such a post unread.
- A member session is required (anonymous and inactive `unauthenticated`,
  pre-user `forbidden`), and access to the thread is the thread page's: a
  thread the viewer may not see is `not-found`. Reading is all it needs; a
  member without `reply` keeps a position.

#### Drafts (TKT-01M29CA9CE)

`/drafts/{thread_id}` is the composer's autosave for a reply, one per user
per thread (04 `drafts`), kept on the server so it follows the user to
another device.

- `GET` answers 200 `internal/dto` `Draft` `{thread_id, source, reply_to,
  updated_at}`, or 204 with no body when the viewer has no draft for the
  thread. Having no draft is the usual state rather than an error, and
  `not-found` stays reserved for a thread the viewer cannot see.
- `PUT` takes `{source, reply_to?}` and answers 200 with the draft as stored.
  It replaces the viewer's one draft for the thread, whichever device sent
  it, and `updated_at` is the server's time of the save. `source` is required
  but may be empty, because a draft that holds only a reply target is still
  the composer's state; the body limit, which is the post limit, bounds it.
  `reply_to` must name a post the thread has (`validation-failed`
  otherwise); a deleted post is accepted, as a reply accepts it. Saving needs
  `reply` in the forum (`forbidden` otherwise), since a draft is a reply
  being written.
- `DELETE` answers 204 whether or not there was a draft, so a client that
  deletes after posting, or retries, needs no special case.
- A member session and access to the thread are required, as for the read
  position. All three are `no-store`; `PUT` and `DELETE` are charged to the
  write limiter.
- The new-thread draft, which docs/04 keys by forum, has no route yet. That
  is TKT-01M3K9XBJA (Autosave new-thread drafts, and decide whether the
  title is saved).

### Where the shapes live

`internal/dto` holds the structs, and `internal/dto/testdata/golden` holds one
committed JSON file per endpoint per viewer class. Those files are the contract:
they are what a client developer reads instead of asking, and what fails when a
field is renamed.

Regenerate them with one command, and only on purpose:

```sh
go test ./internal/dto -update
```

It is a separate step rather than something an ordinary test run does, because a
golden that rewrites itself asserts nothing. A diff in that directory is the
signal that an API shape changed, so it should appear in a review rather than in
a rebase.

The corpus is held honest by tests that outlive a regeneration, which matters
because the tempting response to a golden failure is to rerun with `-update`:

- every field of every served type is given a value by some fixture, so no field
  is a hole with a passing test over it;
- every nullable field appears both null and set, since the null is the half a
  client gets wrong;
- no two goldens for one endpoint are identical, which would mean the viewer
  class distinction is fake or unexercised;
- no two fields share a JSON tag. `encoding/json` resolves that collision by
  dropping *both* fields and returning no error, so the pair vanishes from the
  API silently.

Only shapes these documents pin have structs. An endpoint whose response no
document fixes is absent rather than guessed, because a golden over a guess
reads to the next person as a decision somebody made.

## Agents on the API

An agent uses this API and no other.

- **Authentication** is `Authorization: Bearer <token>`, the credential issued
  with the account (05). No cookie, no session row, nothing for CSRF to act on.
- **Read position** uses `PUT /threads/{id}/read`, exactly as the SPA does. What
  differs is that nothing sets it implicitly: there is no beacon and no
  mark-on-render for an agent, so its position is whatever it last declared, and
  an agent that never calls the endpoint has no position at all.
- **Writes** are charged against a token bucket rather than the human rate
  limit. Exceeding it is `429` with `Retry-After` (08).
- **Provisioning** is `/api/v1/admin/users` with the agent kind, and the same
  admin flow issues and revokes the token. This lands in Phase 2 beside the
  users and permissions screens, because creating an account and issuing its
  credential are one screen, and splitting them across phases means building
  that screen twice.

`author.kind` and the per-post `machine_authored` flag above are both in the
thread response. They are not the same fact. `kind` is the account as it stands
now and drives the role badge; `machine_authored` comes from the log and records
what was true when the post was written (03). They disagree after an account
changes hands, and when they do, the log is right.

### A purpose-built agent surface is gated, not scheduled

An MCP server, a feed format, or anything else shaped for a model rather than
adapted from the browser client stays unbuilt until both of these are true and
written down:

1. A named agent has done real work through this API and its friction is
   recorded as specific complaints, rather than as a general sense of poor fit.
2. A measurement shows the cost: the round trips or the tokens it takes an agent
   to participate, against what a purpose-built surface would take.

Queue order does not promote this. Either both pieces of evidence exist, or the
work stays where it is.

## Preact client

- **Stack:** Preact 10.x, `@preact/signals`, `preact-iso` router, Vite 8,
  TypeScript. No component library; a small hand-written CSS system with
  custom properties and light/dark themes. Target bundle: < 60 KB gzipped
  for the thread view. CI enforces it, reading 60 KB as 60000 bytes and
  counting everything the reader loads except fonts and the lazy admin
  chunk; the number is committed in `web/client/budget.json`
  (TKT-01M29CGBYH). Hand-written CSS is decided over a utility framework
  (10 Q21, TKT-01M29BW9NW), checked against that budget before adoption: it
  adds no runtime or build dependency, so every byte of it is a byte we wrote,
  and the mockups in `docs/mockups/` are already written that way.
- **Page size:** 25 posts by default, 10–50 as a user setting (06). The
  default is decided (10 Q13) and appears in shared URLs, so the API and the
  client both treat it as a constant rather than a tunable.
- **Fonts:** IBM Plex Sans, IBM Plex Mono and Newsreader, as in the
  mockups, self-hosted as subset WOFF2 files embedded in the binary and
  served from the same origin. The CSP allows no third-party font host.
  They are cached apart from the code and do not count toward the 60 KB
  budget, which measures the thread view's JavaScript and CSS. Two
  alternatives lost. A system font stack would drift from every mockup, and
  counting fonts inside the budget would force dropping Newsreader.
  (Decided 2026-09-24 in Phase 1 grooming.)
- **State:** signals for viewer, notifications, and per-thread caches keyed
  by `log_length`; SWR-style fetch wrapper that honors `ETag`.
- **Routing:** the URL scheme above, one route per screen, lazy-loaded
  admin bundle.
- **Embedding:** `//go:embed all:dist` in `internal/web/spa`; `index.html`
  is a Go template so the server injects `<meta>` for the current thread
  (title, description for link unfurls), a CSP nonce, and a bootstrap
  `<script type="application/json">` with `/me` to save a round trip.
  `dist/` is committed, and CI rebuilds it from the lockfile and fails on a
  difference, so `go build` alone ships the client that the source describes.
  The server supplies each request's title, description and viewer through
  `web.Shell.SetPage`. The JSON block is the one script with a body the CSP
  tolerates: it is data and never executed. html/template escapes the viewer
  in it as a JavaScript value, so a display name cannot close the element.
  Assets under `/assets/` have content-hashed names and are served
  `immutable`; the shell is `no-store`.
- **Dev:** Vite dev server with `server.proxy` → Go on `:8080`; Go serves
  the embedded build in prod. `go generate` runs the client build so `go
  build` alone produces a complete binary when the client is unchanged.
- **Responsive:** one bundle, layout by CSS at the breakpoints in 06
  (720 px, 1100 px); the navigation shell swaps between a top bar and a
  bottom tab bar as a component choice, everything else reflows. Viewport
  meta with `viewport-fit=cover` and `env(safe-area-inset-*)` padding on
  the tab bar and reply bar. A web app manifest and icons make it
  installable; no service worker in v1.
- **Accessibility:** keyboard-first on desktop (06), semantic landmarks,
  focus management on page change, `prefers-reduced-motion`, 44 px
  minimum touch targets on the phone layout.
- **Offline-ish:** none in v1. Drafts are server-side.

## Path to a non-JS client

Adding it later means adding `internal/web/html` with handlers per URL
above, `templ` (preferred) or `html/template` views, and form posts for
mutations. Constraints imposed now so that it stays cheap:

1. Every SPA screen corresponds to a service call that returns a DTO; the
   HTML handler renders the same DTO.
2. Every mutation is expressible as a form post: composer, flags, read
   marks (`?mark_read=` on page load for non-JS), watch toggles. Handlers
   return redirects with flash messages; the JSON handlers return 200.
3. Lenses and pages are URL state, never client-only state.
4. Markdown preview is a server round trip already.
5. `CrossOriginProtection` is enabled from day one so form posts are safe
   when they arrive.
6. The SPA shell includes `<noscript>` with a link to the HTML client path
   (once it exists) rather than a blank page.
7. The HTML client is responsive from its first version, using the same
   breakpoints and the same bottom-bar pattern, so a phone with scripts
   off gets the same arrangement.

Content negotiation on the same routes (`Accept` header) was considered and
rejected: mutation semantics differ (redirect vs JSON) and caching gets
murky. Separate handler packages over one service is clearer.

### Form posts for viewer state

An HTML form sends only `GET` and `POST`, as `application/x-www-form-urlencoded`,
so each viewer-state route has a form shape for the HTML client. Each form
post calls the same `internal/service/threads` method as its JSON route, with
the same rules and refusals, and answers with a `303 See Other` back to the
page it came from and a flash message instead of a body. A refusal is the
same problem code rendered as a page. The routes live under the HTML
client's `/t/{id}` paths; `{slug}` is advisory there as everywhere. A test in
`internal/web/api` checks that every viewer-state route it serves has a row
here.

| JSON route | HTML client | Fields | Service call |
|------------|-------------|--------|--------------|
| `PUT /api/v1/threads/{id}/read` | the thread page itself: `GET /t/{id}/{slug}?page=N` marks the highest ordinal it renders (06), and `&mark_read=M` marks `M` instead | `mark_read` (query) | `MarkRead`: forward only, clamped to the last post |
| `DELETE /api/v1/threads/{id}/read` | `POST /t/{id}/unread` | `ordinal` (optional; empty is the whole thread) | `MarkUnread` |
| `GET /api/v1/drafts/{thread_id}` | none: the thread page fills its composer from the draft | | `Draft` |
| `PUT /api/v1/drafts/{thread_id}` | `POST /t/{id}/draft`, the composer's "Save draft" button (`formaction` on the reply form) | `source`, `reply_to` (optional) | `SaveDraft` |
| `DELETE /api/v1/drafts/{thread_id}` | `POST /t/{id}/draft/delete` | none | `DeleteDraft` |
| `POST /api/v1/threads/{id}/watch` | `POST /t/{id}/watch` | `level` | owned by TKT-01M29CRJ29, which builds the route |
| `POST /api/v1/threads/{id}/flags` | `POST /t/{id}/posts/{ordinal}/flag` | `reason`, `note` | owned by TKT-01M29CP2PJ, which builds the route |

The read mark on a page `GET` is the one place a safe method writes. It is
safe to repeat, because the position only moves forward, and it is what
docs/06 asks of a client that cannot send a beacon. A prefetch that loads a
thread page marks it read as well, which is the cost of marking on page load.
`mark_read` never lowers a position; only `POST /t/{id}/unread` does.

## Security headers and limits

Every response carries these, set on the way in so they are already present
when the body limit answers 413, recovery answers 500, or the router answers
404 — the responses nobody wrote on purpose are the ones a per-handler header
would miss.

| Header | Value |
|--------|-------|
| `Content-Security-Policy` | the policy below |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `X-Content-Type-Options` | `nosniff` |

The policy, with a fresh nonce per response:

```
default-src 'self'; base-uri 'self'; object-src 'none';
frame-ancestors 'none'; form-action 'self'; img-src 'self' data:;
script-src 'self' 'nonce-…'; style-src 'self' 'nonce-…'; connect-src 'self'
```

`default-src 'self'` is the no-third-party-origins rule: everything the client
needs is served from this binary. `script-src` and `style-src` carry the nonce
and never `'unsafe-inline'` — that keyword would make the nonce decorative,
because a browser stops checking nonces once the policy says inline script is
fine anyway. `base-uri` stops an injected `<base>` from re-pointing every
relative URL, and `form-action` matters for the non-JS client, which posts
forms. `X-Frame-Options` is deliberately absent: `frame-ancestors` supersedes
it, and a second header saying the same thing is a second place to keep in
sync.

The SPA shell is a Go template rendered per response with the nonce on every
script and style tag, and is sent `no-store` — a cached shell would hand a
stale nonce to the next reader and the policy would block the whole page.

### Request limits

| Limit | Value | Configurable |
|-------|-------|--------------|
| Request body | 64 KB | no — part of the API contract |
| Upload (`POST /api/v1/blobs`) | 10 MB | yes, `limits.attachment_mb` (`KETJU_LIMITS_ATTACHMENT_MB`) |

Both are refused with a `payload-too-large` problem rather than a truncated
read. A body whose `Content-Length` is over the limit is refused before a byte
is read; a chunked body, or one whose `Content-Length` lies, is caught by
`MaxBytesReader` and the resulting error maps to the same code.

The post limit is route-independent and fixed because it is part of what the
API promises a client — an installation that quietly raised it would accept
posts another installation could not take. The attachment cap is configurable
because what counts as a reasonable attachment depends on who is running the
forum, and is bounded above because an upload is buffered before the uploader
is authenticated.

These sit below two other limits at other layers: `render.MaxSource` (256 KiB)
is the renderer's own backstop, and `MaxFrameSize` (1 MiB, docs/03) is the
storage format's bound.

### CSRF posture

No token is threaded through the SPA. Three things hold instead, and none of
them is sufficient alone:

1. **Mutating API routes accept JSON only.** An HTML form can send exactly
   three content types — `application/x-www-form-urlencoded`,
   `multipart/form-data` and `text/plain` — and none is JSON, so a form on
   another origin cannot produce a request the API will look at. Anything else
   is `unsupported-media-type`.
2. **`http.CrossOriginProtection`** rejects non-safe cross-origin browser
   requests via `Sec-Fetch-Site`, or `Origin` against `Host`. This covers the
   two places rule 1 cannot: the multipart upload route, which must accept a
   form content type, and the form posts the non-JS client will make. The
   configured `site_url` is registered as a trusted origin, because behind a
   proxy the server sees an internal `Host` while the browser sends the public
   one.
3. **Session cookies are `SameSite=Lax`**, so the browser does not attach them
   to a cross-site POST at all. That belongs to the session work, and this
   posture assumes it.

Both are on by default: security middleware that has to be opted into is
security middleware that will be forgotten.

Still to come: upload MIME sniffing with an allowlist, and the per-agent token
bucket for bearer-authenticated writes (08).
