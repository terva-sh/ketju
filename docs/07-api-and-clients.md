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
/auth/signin?return_to=…           login page
/auth/{provider}/start  /auth/{provider}/callback  /auth/logout (POST)
/auth/register (GET, POST)
/auth/link  /auth/{provider}/link (POST)
```

`/u/{old}` and `/u/{old}/posts`, for a handle a rename released within 90
days, answer a member `302` to `/u/{new}` with `no-store`, query kept, before
the shell is served, as `GET /users/{old}` does (below; 05 "Changing a
handle", `api.ProfileRedirects`). Anyone else gets the shell, as for any
profile, so a stranger learns nothing about who renamed to what.

`/auth/*` is the server's in both clients: plain HTML pages and redirects,
not JSON. `/auth/signin` is the login page, where a signed-out reader's
"Sign in" goes, with the address they were at as `return_to` (05 "Login
flow"). It lists each enabled provider by its `name`, linking to
`/auth/{key}/start` with the same `return_to`. With exactly one enabled
provider it answers 302 straight to that provider's start, and with none it
answers 200 with a page saying sign-in is not available. A reader already
signed in gets 302 to `return_to`. `return_to` is validated as start
validates it, and anything but a path on this site becomes `/`. The client's
link to it is a full page load, since `/auth/*` is outside the client's
routes. `/auth/register` is open registration's handle screen (05
"Registration path"), and an `auto_approve` provider's under either mode.
`GET` shows one field, `handle`, prefilled, and needs
the `__Host-ketju_reg` cookie the callback sets; without it, or after its
fifteen minutes, the page says to sign in again (400). When the callback
tried an `auto_approve` provider's `preferred_username` as the handle and
could not use it, the screen shows one line above the field naming why:
the sign-in had none, it is not a valid handle, it is reserved, or it is
taken. The reason is sealed in the cookie. `POST` takes the form
field `handle` as `application/x-www-form-urlencoded`, held to this site by
the cross-origin check (see "CSRF posture"). Success sets the session cookie, clears
the registration cookie and answers 303 to the sign-in's `return_to`. An
invalid or reserved handle is 422 and a taken one 409, each with the form
shown again and the reason beside the field. An identity registered in the
meantime is 409, registration closed in the meantime is 403, and a browser
that signed in as a member in the meantime is 409: a member adds an identity
by linking it, never by registering a second account.

`/auth/link` is the "link a sign-in" page (05 "Login flow" step 3): for an
active member it lists each enabled provider as a one-button form posting
to `/auth/{key}/link` with the page's `return_to`, and anyone else gets 303
to `/auth/signin`, with `return_to` bringing them back. `POST
/auth/{provider}/link` is held to this site by the cross-origin check, and
refuses anyone but an active member signed in with a session with 403. It
sets the login cookie, marked with the member's user id, and answers 200
with a page that sends the browser on to the provider with an immediate
refresh and a link, not with a redirect: `form-action 'self'` covers where
a form's submission is redirected. The callback of a link answers 303 to
`return_to` once the identity is linked, with the session cookie
untouched. It answers 409 when the identity belongs to another account or
to an application, or when the browser is no longer signed in as the
member who started the link, and 200 "Already linked" for the member's own
identity. A callback that brings an identity nobody has to a browser a
member is signed in on, without a link started, is 409 and writes nothing.

An approval-mode applicant's pre-user session reaches `/apply`,
`/apply/status`, `/auth/signin`, `/auth/{provider}/start`,
`/auth/{provider}/callback` and `POST /auth/logout` among these, and no
other: any other page answers 303 to `/apply/status`, and any other
request 403 (05 "mode = approval"). The client, seeing `pre_user` in
`/me`, replaces every address but `/apply` with `/apply/status` and draws
the status page there, under a bar with the wordmark and "Sign out" and no
forum navigation (TKT-01M29CP2NG).

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
| `GET  /me` | viewer: user, role, unread counts, pending notifications count, settings. The notifications count is the unread rows, with a `new_application` row counted only while its application is pending (docs/04 "notifications"). An approval-mode applicant (a pre-user session) gets `user: null` plus `pre_user: {provider}`; a null user stays the only signed-out signal. `queue` is the moderator queue's item count, or null for a viewer with no queue (TKT-01M29CRJ4E). Two statements for a member: the session lookup and one count query, plus one batched forum-access read of every forum while the permission cache is cold (`perm.ModeratedForums`), which also answers the read checks the counts need. A viewer who moderates somewhere pays one queue count statement more. Neither count reads every thread (docs/04 `forum_thread_counts`) |
| `GET  /forums` | tree of forums with per-forum unread counts → `internal/dto` `ForumTree`. A forum the viewer may not read is absent, not flagged, and so are its children; an anonymous viewer sees public forums only. Roots come in position order with their children, one level deep, under them. `thread_count` is threads with a post; `post_count` is their posts that are neither deleted nor moved, the rule the profile's post count follows, so a deleted post leaves the total and a restored one returns (a moved post counts in the thread it moved to; Phase 1 writes no record that hides or moves a post, and the record that does must leave the total too), read from docs/04 `forum_thread_counts` without reading `threads` (TKT-01M3B6A68B); `unread` is the viewer's threads with an unread post, by `/me`'s rule (read positions and `mark_all_read_at`, mutes not counted), and 0 for an anonymous viewer; `last_post` is the most recently active thread, its last poster and the time, or null. Each is the forum's own, not its children's. `no-store` |
| `GET  /forums/{slug}/threads?cursor=&sort=` | thread list rows → `internal/dto` `ThreadList`, answered from projections alone. The first page opens with the pins: global pins from every forum the viewer may read, then this forum's own, each by latest activity, at most 50; a pin past its `pinned_until` is no pin, and pins past the 50 sort with the rest. Then 25 threads with a post, newest `last_post_at` first, or newest `created_at` first for `sort=created`; the pins are not counted. `next_cursor` is opaque, bound to its forum and sort, and empty at the end; paging is keyset, so a thread bumped between pages moves up rather than appearing twice. Every page leaves out exactly the forum's threads the first page showed as pins, which the cursor carries, so a pin that ends between pages is not shown again. A cursor is a position, not a credential: one that is malformed or was made for another forum or sort, or any other `sort`, is `bad-request`. `unread` is the viewer's unread posts by the per-post rule (read position and `mark_all_read_at`) and `last_read_ordinal` their position, both 0 for an anonymous viewer. Access answers as the thread page does: an unknown or hidden forum is `not-found`, a signed-in-only one `unauthenticated` to an anonymous viewer, `forbidden` to a member without read. `no-store` |
| `POST /forums/{slug}/threads` | create thread `{title, source, tags}` → 201 `{thread, post}`, `internal/dto` `CreatedThread`: the thread page's header and a `CreatedPost` for post 1, each as a later read serves it. A member session is required (anonymous `unauthenticated`, pre-user `forbidden`); a forum hidden from the viewer is `not-found`, an archived one is `conflict` except to an admin (04, checked before `create_thread`, so the refusal says why), and one without `create_thread` is `forbidden`. The title is trimmed and must be 1–200 characters with no control characters; the slug is derived from it (an ASCII fold, at most 60 characters). `tags` must be absent or empty until tag rules exist. The source is checked as a reply's is. The thread is projected before the response, so it can be opened at once |
| `GET  /threads/{id}?page=&ordinal=&lens=&after_log_length=` | thread header + page of rendered posts + lens meta + viewer read position |
| `POST /threads/{id}/posts` | `{source, reply_to?, attachments?}` → 201 `{post, ordinal, revision, log_length}`, `internal/dto` `CreatedPost`: the post as `GET /threads/{id}/posts/{ordinal}` serves it, written after the record is durable. A member session is required; without `reply` it is `forbidden`, and not-found or unauthenticated as for any read of the thread. A locked thread is `conflict` except to a moderator; an archived one is `conflict` to everyone, and a thread in an archived forum is `conflict` to everyone but an admin (04), before `reply` is checked. Slow mode (`slow_mode.default_seconds`, off by default; moderators exempt) is `rate-limited` with `Retry-After` set to the seconds left. An empty source, a `reply_to` naming no post, and any attachment (nothing uploads yet) are `validation-failed`; an over-long source is `payload-too-large`. Every refusal comes before anything is appended. Charged to the per-address write limiter |
| `GET  /threads/{id}/posts/{ordinal}` | single post (popover, permalink) |
| `GET  /threads/{id}/posts/{ordinal}/replies` | direct replies (inline expander) |
| `GET  /threads/{id}/posts/{ordinal}/history` | revisions, each with actor, time, reason and source; the client diffs them |
| `PATCH /threads/{id}/posts/{ordinal}` | edit `{source, reason?}` |
| `DELETE /threads/{id}/posts/{ordinal}` | soft delete; `POST …/restore` to undo |
| `PUT  /threads/{id}/read` | `{ordinal}` advance read position; `DELETE` marks unread. Both answer 200 `internal/dto` `ReadPosition`; see "Read position" below |
| `POST /threads/{id}/watch` `{level}` | watch/mute |
| `POST /threads/{id}/flags` | `{ordinal, reason, note}` report a post (08 "Flags", TKT-01M29CP2PJ). A member session and read access are required. `reason` is `spam`, `abuse`, `off-topic` or `other`, and `other` needs a `note`, at most 1000 characters (`validation-failed`); an ordinal the thread lacks is `not-found`; a post in an archived thread is `conflict`, and so is a post already removed or moved, except for a member whose flag on it is still open, whose repeat gets the receipt like any other. Answers 200 `internal/dto` `FlagReceipt`, `{"received": true}`, the same for a new flag, a member's repeat (which adds no row) and the flag that hides the post, and nothing else: no count, no reporter, no outcome. If the flag reaches `flags.auto_hide_threshold` and the hide cannot be appended, the flag is kept and the answer is `unavailable`; the sweep hides the post within a minute. Charged to the write limiters. `no-store` |
| `POST /render` | `{source, thread_id?, reply_to?}` → `{html, mentions: [{handle}], quotes: [{ordinal, deleted}], reply_to}` (composer preview; the create path records the same facts from the same render, `internal/dto` `RenderResult`). A member session is required (anonymous `unauthenticated`, pre-user `forbidden`). With `thread_id` the viewer must be able to read the thread, quotes resolve against it and `reply_to` is checked (`validation-failed` when no such post; a deleted post is accepted); without it quotes render optimistically and `reply_to` is `bad-request`. Charged to the per-address write limiter; the body limit is the post limit |
| `GET  /drafts/{thread_id}` `PUT` `DELETE` | composer drafts, one per user per thread → `internal/dto` `Draft`; see "Drafts" below |
| `GET  /unread` `/new` `/participated` `/watching` | cross-forum thread lists |
| `GET  /search?q=&forum=&author=&after=&before=` | FTS5 results with snippets |
| `GET  /notifications` `POST /notifications/read` | |
| `POST /blobs` (multipart) `GET /blobs/{hash}` | attachments; GET is outside `/api` at `/blobs/…` with immutable caching |
| `GET  /users/{handle}` `/users/{handle}/posts` | profiles. `/users/{handle}` answers `{user: author, bio_html, joined_at, post_count, status}`, `internal/dto` `Profile`: `bio_html` is the bio rendered as a post is (a quote in it links nowhere, having no thread), and `status` is null except to moderators and admins. `/users/{handle}/posts?cursor=` answers `{posts: [{thread, forum, post}], next_cursor}`, `internal/dto` `UserPosts`: each post rendered as its thread page renders it for the viewer, with a link to its thread and forum, newest first, 25 to a page. Paging is keyset on (created_at, thread, ordinal) descending, so a post written while a reader pages does not appear twice, and a page is short only at the end of the history; the cursor is opaque, names no account, and one that is malformed is `bad-request`. A moved post is left out. Whether a post is deleted is read from its thread's index rather than from `post_meta`, which trails the log, and the count reads it the same way, so the history and the count agree while the projection catches up. The handle matches case-insensitively and an unknown one is `not-found`. An anonymized account (`users.anonymized`, 04) shows its `deleted-user-<id>` handle and no display name, avatar or bio, in the profile and on every post in its history, as every author block does (`member.Author`, TKT-01M29CKRFS); its old handle is `not-found`. A handle a rename released within 90 days answers a member `302` to the same path under the account's handle now, query kept, with `Cache-Control: no-store`, a chain of renames included (05 "Changing a handle", TKT-01M3NRP3W9). The redirect is temporary so that no cache outlives the reservation. A stranger is refused as for any profile, with no `Location`; an old handle of a pending account is `not-found` except to moderators and admins; an anonymized account's old handles redirect nowhere; and once the 90 days end the old handle is `not-found`. Both answers are `no-store`. A member session is required for both, as for `GET /users?prefix=` (anonymous `unauthenticated`, pre-user `forbidden`). The post count counts the user's non-deleted posts in forums this viewer can read, so it varies by viewer and says nothing about forums the viewer cannot read. The history spans forums and leaves out posts in forums the viewer cannot read. A deleted or hidden post is left out for other members. The author sees their own as their thread page shows them: a post they deleted is marked `removal: "author"` without a body, and a post a moderator hid is marked `removal: "moderator"` with its body (08, decided 2026-09-28; TKT-01M29CKRJ9). Moderators and admins see both marked, with the body, as the thread page's moderator view does. A deleted post never counts, even for a viewer whose history shows it, so the count equals the posts a member's history lists and is less than an author's, moderator's or admin's history by the deleted posts it shows. A pending account is `not-found` except to moderators and admins; a suspended or deactivated account's profile is served, and its status is shown only to moderators and admins (05, "Account lifecycle"). Decided by the maintainer on 2026-09-28 (TKT-01M3KJJNDC) |
| `GET  /users?prefix=&limit=` | `{users: [author]}`: mention autocomplete, `internal/dto` `UserSuggestions`. Up to `limit` (default 8, at most 20) active members whose handle starts with `prefix`, case-insensitively, in handle order; a prefix no handle can start with is an empty list. Every handle offered is one the renderer resolves as a mention, so a handle ending in `-`, which the mention syntax reads as punctuation, is not offered. Registration no longer gives one out, but a handle given out before that rule may still end in `-` (04 "users"), so the filter stays. A member session is required (anonymous `unauthenticated`, pre-user `forbidden`) |
| `GET  /settings` `PATCH /settings` | prefs; `GET/DELETE /settings/sessions` ; `GET/DELETE /settings/identities/{id}` |
| `PUT /settings/handle` `{handle}` | the viewer changes their own handle (05 "Changing a handle", TKT-01M3NRP3W9): 200 `internal/dto` `HandleChange` `{handle, previous, next_change_at}`, `no-store`. `next_change_at` is when they may change it again, 30 days on. A second change inside the 30 days is `conflict` with `available_at` (below, "Errors"), the Unix ms it becomes possible; an admin's rename neither waits for this cooldown nor starts it. A refused handle is `validation-failed` with `errors: [{field: "handle", code}]`, the code one of `invalid_handle`, `reserved` and `taken` as for an approval (below, "Applications"), where `taken` includes a handle a rename or an anonymization released within 90 days, the viewer's own old one included, or `same` for the handle they have. The old handle is reserved for 90 days, and `user.renamed` is audited with `{from, to}` in the same transaction. Anonymous is `unauthenticated`, a pre-user `forbidden`, both before the body is read. Charged to the per-address write limiter |
| `POST /settings/deactivate` | the viewer deactivates their own account (05 "Account lifecycle"): 204 `no-store`, every session of the account deleted in the same transaction, this one included, and the session cookie cleared. The last active admin is `conflict`. Anonymous is `unauthenticated`, a pre-user `forbidden`. They come back by signing in and pressing Reactivate (05 "Login flow"). TKT-01M29CKRFS |
| `GET  /apply/form` `POST /apply` `GET /apply/status` `POST /apply/withdraw` | approval mode, a pre-user session only (05 "mode = approval"): anonymous `unauthenticated`, a member `forbidden`. `GET /apply/form` → `internal/dto` `ApplicationForm`: the active `registration` form as `Form`, and `prefill`, the identity's claim for each `from_claim` field that has one. `POST /apply` `{version, answers}` → 201 `ApplicationStatus`; `version` must be the active version or it is `conflict` and nothing is written, and answers that break the form are `validation-failed` with field errors; a locked field stores the claim whatever is sent. `GET /apply/status` → `ApplicationStatus` `{status, provider, submitted_at, decided_at, can_withdraw, can_apply, can_apply_at, sign_in}`: `status` is none, pending, approved, denied or withdrawn, `can_apply_at` is set only when the 24-hour rule alone stands in the way, and `sign_in` only when approved, which a pre-user session sees only until the approval retires it (05 "mode = approval" step 4); it never carries a moderator's note. `POST /apply/withdraw` (empty body or `{}`) → 200 `ApplicationStatus`, `conflict` with nothing pending. The form and the submit refuse, in this order: an identity with a user `conflict`, a blocked one `forbidden` (with no reason given), `registration.mode` other than approval `forbidden`, a pending application `conflict`, and a submission within 24 hours of the last `rate-limited` with `Retry-After`. The two writes are also limited per identity (08 "The transport-level ceiling"). Every answer is `no-store` |
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
| `edit-window-closed` | 403 | The post is the viewer's, and `posts.edit_window_seconds` has passed since it was written (08) |
| `delete-window-closed` | 403 | The post is the viewer's, and `posts.delete_window_seconds` has passed since it was written, so it can no longer be deleted or restored (08) |
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

A `conflict` whose refusal ends at a known time carries `available_at`, the
Unix ms from which the request can succeed: a member's second handle change
inside the 30-day cooldown (`PUT /settings/handle`, TKT-01M3NRP3W9). It is an
RFC 9457 extension member the server computes, and no other body has it.
`rate-limited` with `Retry-After` lost for this, because a client reads
`Retry-After` as "try again soon", and the wait is weeks.

#### Field errors on `validation-failed`

A `validation-failed` body for a form's answers, a form schema (05
"Forms"), or an application decision carries one more member, `errors`: a
list of `{field, code}`, one entry per thing wrong, in order. This is an
RFC 9457 extension member. No other body has it.

- `field` is an answer's key from the stored schema, or a path into a
  submitted schema of property names and indices, such as
  `fields.2.options` or `title`. The whole schema is `""`. A path never
  carries a key the request sent, so the body echoes nothing. An
  application's approval or denial names its own fields, `handle` and
  `note` (below, "Applications"), and a handle change names `handle`.
- `code` is one of the codes below, or for a `handle` one of the three
  listed with the approval route, plus `same` on a handle change. They are
  API surface, like the `type`s above.

<!-- form-codes:begin -->
| Code | Meaning |
|---|---|
| `required` | Missing or blank, and required; a required checkbox left unticked |
| `too_long` | Over the property's or the answer's character limit, or a schema over 64 KiB as sent or as stored |
| `invalid_email` | Not a bare email address |
| `invalid_text` | Holds a control character, or a line break where one line is expected |
| `not_an_option` | A select answer that is not one of its options |
| `duplicate_option` | An option, or a multiple select answer, given twice |
| `wrong_type` | The wrong JSON type: a number for a string, a string for a list |
| `not_allowed` | A property this field type does not take, or a value sent for a `markdown_notice` |
| `invalid_key` | A field key that does not match `^[a-z][a-z0-9_]{0,39}$` |
| `duplicate_key` | A field key already used in the form |
| `unknown_type` | A field type that is not one of the six |
| `unknown_claim` | A `from_claim` that is not `name`, `email` or `preferred_username` |
| `out_of_range` | A `max` below 1 or over the type's cap |
| `too_many` | Over 50 fields or 100 options |
| `unknown_property` | A property no schema or field has |
| `malformed` | Not JSON, or more than one JSON value |
<!-- form-codes:end -->

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

#### Users, groups and permissions (TKT-01M29CXK1X)

The `/admin/users` screen, with groups and permissions as its
`?tab=groups` and `?tab=permissions` tabs. Moderators may list accounts,
read one, and suspend and unsuspend members, because suspension is a
moderator action reached from here; everything else is an admin's. Every answer is `no-store`. Every write is
audited in the transaction that makes it (04 "audit_log") and applies from
the next request.

| Route | Gate | What it does |
|---|---|---|
| `GET /admin/users?prefix=&role=&status=&kind=&cursor=` | staff | `AdminUserList`: 50 accounts per page in handle order, every status included. A filter value that is not a role, status or kind is `validation-failed`; a malformed cursor is `bad-request` |
| `GET /admin/users/{handle}` | staff | `AdminUserDetail`: the account and its stored groups, each marked `admin` or `idp`. `identities` and `sessions` are an admin's only, and `null` to a moderator |
| `PUT /admin/users/{handle}/role` `{role}` | admin | Through `perm.ChangeRole`: `user.promoted`, `user.demoted` or `user.role_changed`. Demoting the last active admin is `conflict`. Answers with the detail |
| `POST /admin/users/{handle}/suspend` `{reason, until?, hide_posts?}` | staff | 204. Through `perm.ChangeStatus`: `user.suspended`, and the account's sessions deleted, in one transaction. The reason is 1 to 500 characters once trimmed, with no control characters, and is shown to the member at sign-in; `until` is Unix ms in the future, or null for no end, and the suspension expiry job lifts it within a minute of it. From `active` or `deactivated`; anything else is `conflict`. A moderator may suspend a member only, and an admin anyone but themselves (`forbidden` otherwise); the last active admin is `conflict` (05 "Account lifecycle", TKT-01M29CKRFS). With `hide_posts`, the account's visible posts are then hidden with the suspension, as `…/posts/hide` does; the suspension commits first, so a hide that fails part-way answers the error with the account suspended, and `…/posts/hide` finishes it (TKT-01M29CP2QK) |
| `POST /admin/users/{handle}/unsuspend` `{restore_posts?}` | staff | 204. `user.unsuspended`, back to `active`, the reason and end cleared. On the same authority as suspending; an account that is not suspended is `conflict`. The body is optional. With `restore_posts`, the posts the suspension it lifted hid are then restored, as `…/posts/restore` restores them. That suspension is the newest written before this `user.unsuspended` row, so a suspension another moderator makes the moment after keeps its hidden posts. Without `restore_posts` the posts stay hidden |
| `POST /admin/users/{handle}/posts/hide` | staff | 204. Hides every visible post of a suspended account with its current suspension: a moderator's `Delete` carrying the suspension's audit id (03), one per post, projected before the answer, then `user.posts_hidden`. Only threads in forums the caller may moderate are touched, so a moderator's hide passes over a forum they cannot see and says nothing about it; archived threads are passed over. Posts already deleted or hidden are passed over, so a second call changes nothing and finishes one that stopped. On the authority that suspends the account; an account that is not suspended is `conflict`; a thread that is quarantined or cannot be opened is passed over and the answer is `unavailable` once the rest are done. The forum a thread is judged by is the one its log says, moves not yet projected included (08 "Suspension", TKT-01M29CP2QK) |
| `POST /admin/users/{handle}/posts/restore` | staff | 204. Restores exactly the posts whose `Delete` in force carries one of the suspensions the account had when the request read it, whatever the account's status, in the forums the caller may moderate, then `user.posts_restored`. A post a moderator hid for its own sake stays hidden. This is how a moderator brings back the posts of a suspension that expired. On the authority that suspends the account |
| `POST /admin/users/{handle}/anonymize` | admin | 204, and it cannot be undone. Through `perm.Anonymize`: the handle becomes `deleted-user-<id>`, the display name, avatar and bio are cleared, the account becomes `deactivated`, and its sessions and identities are deleted, with `user.anonymized` (naming no handle), in one transaction. The thread logs are not touched. An admin cannot anonymize themselves (`forbidden`); the last active admin, and an account already anonymized, are `conflict` |
| `POST /admin/users/{handle}/rename` `{handle}` | admin | `HandleChange`, as `PUT /settings/handle` answers, with the same field errors, reservation, redirect and `user.renamed` row, the admin as its actor. No cooldown, and the member's own is left as it was: `next_change_at` is theirs. An anonymized account is `conflict`, and a handle nobody has is `not-found`. This is how a grandfathered handle ending in `-` is fixed (TKT-01M3ANX1NF). 05 "Changing a handle", TKT-01M3NRP3W9 |
| `GET /admin/groups` | admin | `GroupList`, each group with its member and grant counts |
| `POST /admin/groups` `{slug, name, description}` | admin | 201 `Group`. The slug follows 04's group slug rule; a taken slug is `conflict`. The name is 1 to 80 characters, the description at most 500 |
| `GET /admin/groups/{id}` | admin | `GroupDetail`: the group and its stored members |
| `PATCH /admin/groups/{id}` `{name?, description?}` | admin | The renamed group. A system group may be renamed; its slug never changes |
| `DELETE /admin/groups/{id}` | admin | 204. A system group, or one with a stored member or a permission row, is `conflict` |
| `POST /admin/groups/{id}/members` `{handle}` | admin | Adds the account on the admin's authority. Answers with the `GroupDetail` |
| `DELETE /admin/groups/{id}/members/{handle}` | admin | Removes it, as above. Adding over or removing an `idp` membership is `conflict`: the next sign-in would undo it (05 "Roles and groups") |
| `GET /admin/permissions` | admin | `PermissionGrid`: the groups, the forums in index order, and every row, `forum_id` null for a site-wide one |
| `POST /admin/permissions` `{changes: [{forum_id, group_id, permission, grant}]}` | admin | The batch in one transaction, one audit row per change, or none. A change naming no forum, group or permission, or the same row twice, is `validation-failed`; a grant of a row that exists or a revoke of one that does not is `conflict`, because the grid moved since the batch was staged. At most 500 changes. Answers with the grid |
| `POST /admin/permissions/preview` `{handle, changes}` | admin | `PermissionPreview`: the account's `allow`, `deny` or `hidden` for each permission in every forum, before and with the batch, decided as a check decides it. Writes nothing, and refuses what applying would |

#### Applications (TKT-01M29CP2MG)

The `/admin/applications` screen, where moderators and admins decide
approval-mode applications (05 "mode = approval" step 5). Every route is
staff's: anonymous and inactive viewers are `unauthenticated`, and a
pre-user or a member without a staff role is `forbidden`. Every answer is
`no-store`, and none goes near the response cache. An id that names no
application is `not-found`, and one that is not a positive integer
`bad-request`. Bodies are decoded strictly: an unknown field is
`bad-request`.

| Route | What it does |
|---|---|
| `GET /admin/applications?status=&cursor=` | `AdminApplicationList`: 25 applications per page, newest first. `status` is `pending` (the default), `approved`, `denied`, `withdrawn` or `all`; anything else is `validation-failed`, and a malformed cursor `bad-request` |
| `GET /admin/applications/{id}` | `AdminApplication`: the answers against the form version they answered, whichever version is active; the identity (provider, email, verified flag, blocked), or null once removed; the applications the identity submitted before this one; the moderators' notes; `suggested_handle`; and the decision, null while pending |
| `POST /admin/applications/{id}/approve` `{handle, note?}` | The `AdminApplication`, approved. In one transaction: the member is created (active, `member`, human), the identity linked, the application approved with the member's id, the applicant's pre-user sessions deleted, and `application.approved` audited. It issues no session. A refused handle is `validation-failed` with `errors: [{field: "handle", code}]`, the code one of `invalid_handle` (breaks 04's handle rule), `reserved` (kept for a route or the software) and `taken` (a member holds it in any case, or a release still reserves it). An application no longer pending is `conflict`, as is one whose identity is gone, has a user, or is blocked; nothing is written. There is no role field |
| `POST /admin/applications/{id}/deny` `{note, block}` | The `AdminApplication`, denied. The note is required, 1 to 2000 characters, and the applicant never sees it; a missing, overlong or control-character note is `validation-failed` with `errors: [{field: "note", code}]`, the code `required`, `too_long` or `invalid_text`. With `block` the identity is blocked too, which must land on exactly one identity with no user, or the denial is `conflict` and nothing is written. One `application.denied` row, with `blocked`. An application no longer pending is `conflict` |
| `POST /admin/applications/{id}/notes` `{body}` | 201 `ApplicationNote`: a note for the other moderators, 1 to 2000 characters. Not audited: it decides nothing |
| `POST /admin/applications/{id}/unblock` (empty body or `{}`) | The `AdminApplication`, its identity no longer blocked. The application stays denied, and the applicant may apply again under the 24-hour rule. `identity.unblocked` is audited against the identity. Only the denial that set the current block lifts it: it must be denied, decided when the block was set, and the identity's newest application. Any other application of the identity, an identity that is not blocked, and one that is gone are `conflict` |

Of two decisions on one application at once, one is made and the other is
`conflict`: the decision is an UPDATE guarded on `status = 'pending'`, and
the transaction that finds nothing to update rolls back whatever it wrote.
A withdrawal racing an approval is decided the same way.

#### The moderator queue (TKT-01M29CRJ4E)

Both routes are behind the `moderates` gate, which admits a member granted
`moderate` on one forum through a group as well as staff by role. The
threads service then scopes what each viewer sees and does by per-forum
`moderate` (`perm.ModeratedForums`). Every answer is `no-store`. How the
actions decide is in 08 "Flags".

| Route | What it does |
|---|---|
| `GET /admin/queue?cursor=` | `Queue`: 25 items per page, newest first. A `flag` item is one flagged post in a forum the viewer moderates, in a thread that is not archived: the thread's id, slug and title, the post as a moderator reads it (null when the thread cannot be read just now), and every flag open on it, newest first, with reporter, reason and note. Its time is its newest open flag. An `application` item is a pending application, as the applications list shows it, for staff only; it is decided through the applications routes. A malformed cursor is `bad-request` |
| `POST /admin/queue/posts/{id}/{ordinal}` `{action, note?}` | `QueueResolution`: `action` as asked, `post` as what happened to the post (`hidden`, `restored`, `already hidden` or `unchanged`), and the ids of the flags it closed. `action` is `resolve` ("Keep post"), `dismiss` or `remove`; anything else is `validation-failed`, as is a note over 500 characters. The body is decoded strictly; one with no `action` is `bad-request`. A post with no open flag, because another moderator acted first, is `conflict`, as is `resolve` on a post a moderator hid by hand, an archived thread, and a post whose state changed between the read and the write. `forbidden` without `moderate` in the thread's forum, and a thread hidden from the viewer is `not-found`. One `flag.resolved` or `flag.dismissed` row is audited against the post, with `{flags, action, note, post}` |

`GET /me`'s `queue` is the number of items the queue lists for the viewer
across every page, counted with the same `WHERE` clauses, and null for a
viewer `perm.ModeratesAny` does not admit.

#### Locking and pinning threads (TKT-01M29CKRGS)

Each route is staff's by role, and the threads service then requires
`moderate` in the thread's forum. Each appends one record to the thread log, projects it before answering,
and answers 200 with the thread's `ThreadHeader` as its page now serves it,
`no-store`. Its `audit_log` row is written by the projection (04
"audit_log"). The thread is read as `GET /threads/{id}` reads it: one hidden
from the viewer is `not-found`. Bodies are decoded strictly and bounded at
4 KiB.

| Route | Body | What it does |
|---|---|---|
| `POST /admin/threads/{id}/lock` | `{reason?}`, or none | `ThreadLocked`. Members' replies and edits are then `conflict`; moderators may still post (08 "Locking semantics"). A member may still delete or restore their own post |
| `POST /admin/threads/{id}/unlock` | `{reason?}`, or none | `ThreadUnlocked` |
| `POST /admin/threads/{id}/pin` | `{scope, until?}` | `ThreadPinned`, replacing any pin the thread has. `scope` is `forum` or `global`; `until` is Unix ms, null or absent for no end. A pin whose `until` has passed is no pin, in the thread list and the header alike, and nothing runs to clear it |
| `POST /admin/threads/{id}/unpin` | `{scope}` | `ThreadUnpinned` of the active pin, which must have that scope |

Refusals come in this order, and each writes nothing:
- `unauthenticated`, or `forbidden`, from the gate;
- `bad-request` for a pin or unpin body with no `scope`, or a body that is
  not the route's;
- `forbidden` without `moderate` in the thread's forum;
- `conflict` on an archived thread. An archived forum does not refuse them
  (08, TKT-01M3N24RHD);
- `conflict` for an action that would change nothing: locking a locked
  thread, unlocking an open one, unpinning a scope that is not the active
  pin's (an expired pin included), or pinning with the active pin's scope
  and `until`;
- `validation-failed` for a scope other than `forum` or `global`, an
  `until` not after now, or a reason over 500 characters.

#### Form versions (TKT-01M29CP2KE)

Admins only: anonymous is `unauthenticated`, and a pre-user, member or
moderator is `forbidden`. Every answer is `no-store`. A key or version that
names nothing is `not-found`.

| Route | |
|---|---|
| `GET /admin/forms/{key}` | `internal/dto` `FormAdmin`: the form's key and name, `active_version`, `versions` newest first (each with `created_at` and `created_by`, null for the seeded version), the active version rendered as `form`, and its stored `schema` |
| `GET /admin/forms/{key}/versions/{version}` | `FormAdmin` with that version as `form` and `schema` |
| `POST /admin/forms/{key}/versions` | `{base_version, schema}` → 201 `FormAdmin` at the new version. `base_version` must be the active version, or it is `conflict` and nothing is written: another admin saved since the editor loaded the form. A schema that breaks the rules (05 "Forms") is `validation-failed` with field errors and writes nothing. The version is appended as the next number and made active, and `form.version_created` is audited, in one transaction. Earlier versions are never changed |
| `POST /admin/forms/{key}/preview` | `{schema}` → `internal/dto` `Form`, rendered as an applicant would see it, as `version` 0. Refused as a save would be, and writes nothing |

`Form` is also what the apply screen draws. Each field carries every
property: `max` is the effective limit (the field's, or its type's cap, 0
for a type without text), `options` is empty except on a select, and `html`
is a `markdown_notice`'s rendered body. `intro_html` is the rendered
`intro_markdown`. A stored version that no longer parses is `internal`
(05 "Forms").

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
     "html": "<p>…</p>", "attachments": [], "deleted": false, "removal": null, "moved_to": null,
     "machine_authored": false, "edited_by_moderator": false}
  ]
}
```

`ETag: "3yymw47g00001-483920-r3"`; `Vary: Cookie`. The viewer block is the
only per-user part, and it is cheap; everything above it comes from the
response cache keyed on `(thread, page, lens, log_length, renderer_version,
viewer_class)` where `viewer_class ∈ {anon, member, moderator, author:<id>}`
(moderators see tombstone contents, and so does the author of a post a
moderator removed; see "Removed posts" below).

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
  `internal/web/api/testdata/golden/thread.{anon,member,moderator,author}.json`
  are what the endpoint actually serves for one seeded thread, regenerated with
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
  from the users row every time. One class is read from the page itself:
  when the page's snapshot holds a post a moderator removed and the viewer,
  not a moderator, wrote it, the class is `author:<user id>`, since that
  page carries the post's body for them alone (TKT-01M29CKRJ9). Every other
  page a member reads stays `member` and shared.
- **What the key cannot name** is kept beside the entry and checked on every
  hit, as the render cache does: the handles the posts' mentions resolved
  and the state of the posts they quote. A user registering a mentioned
  handle is seen at once. A failed check is a miss, and the fresh page
  replaces the entry.
- **A mention follows its member through a handle change**
  (TKT-01M3NRP3W9). The typed handle is resolved against the kept
  `handle_releases` rows, not the `mentions` projection, which trails the
  log. It names whom it named when the post's current revision was
  written: the account a reservation then redirected to, or whoever held
  the handle then. When nobody did, it names
  whoever first took the handle after. Either way it is shown by their
  handle now and followed through their later renames. So an old `@alice`
  links the member who renamed from alice, and never whoever takes `alice`
  after the 90 days, and a mention of an anonymized account's old handle is
  text. While a handle is reserved, a
  new `@alice` mentions the member it redirects to. Each lookup is stored
  with what it was asked against, and a hit asks again
  (`internal/service/threads` `mentions.go`).

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
- A removed post is a tombstone from all three to anyone the "Removed
  posts" rule below does not serve it to: no HTML from the first two, and
  `deleted: true` with no revisions from history, since the old revisions
  are the removed content by another route.
- The author of a post a moderator removed may read that post's history
  even when `public_edit_history` is off, for as long as it stays removed.
  That admits them to that post alone: their visible posts and the posts
  they deleted themselves follow the setting as before.
- Each carries the same body-hash ETag and answers `If-None-Match` with 304.
- History walks the whole log (revisions are not chained in the format), so
  it is for a person opening it, not for a page.

#### Removed posts (TKT-01M29CKRJ9)

A post that is not shown as itself carries `deleted: true` and keeps its
ordinal, `reply_to`, `reply_count` and permalink, so numbering, `?ordinal=`
and the replies expander hold (08 "Tombstones"). `removal` says who removed
it: `"author"` for a post its author deleted, `"moderator"` for one a
moderator hid, and `"review"` for one auto-hidden pending review (08
"Flags", TKT-01M29CP2PJ). It is null for a visible post and for a moved
one, whose `moved_to` names the thread it went to. A post carrying both the
deleted and hidden-by-mod bits, which only a log appended without the
service's checks can hold, is `"author"`.

Who is served a removed post's body is one rule, `revealRemoved` in
`internal/service/threads`, used by the page, the single post, replies,
`/users/{handle}/posts` and history alike:
- a moderator of the thread's forum gets every removed post's body;
- the post's author gets it while a moderator's removal holds it, marked
  `removal: "moderator"` (08, decided 2026-09-28), or while it is hidden
  pending review, marked `removal: "review"` (TKT-01M29CP2PJ), and not for
  a post they deleted themselves;
- nobody else gets it, and nobody gets a moved post's body here.

The rule answers only for removed posts. A visible post keeps the path it
had: its body goes to everyone admitted to the thread, and its history to
whoever the setting admits. A quote of any removed post renders as a
tombstone to everyone.

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
- The post's author may act on it, and so may a viewer with `moderate` in
  the thread's forum, on any post (TKT-01M29CRJ5J); anyone else is
  `forbidden`. A moderator's record names the moderator as its actor, and
  the post keeps its author. A moderator's edit is never a grace edit and
  does not need `reply`. A moderator's delete of somebody else's post writes
  `by_moderator` (03) and hides the post (`hidden-by-mod`) rather than
  deleting it, so only a moderator may restore it (08 "Restoring a
  moderator's delete"). A post hidden by a moderator is `forbidden` to its
  author for all three. An author's edit also needs `reply` in the forum; a
  delete or restore of one's own post does not.
- A post whose current revision is an edit by someone other than its author
  is served with `edited_by_moderator: true`, beside its `edited_at`. It
  names nobody; the post's history names the moderator (08 "Post history").
  It is read from the current revision's frame, which the log length already
  keys, so the response cache needs no editor row.
- A moderator's edit, hide or restore of somebody else's post is projected
  before the answer, so its `audit_log` row (`post.edited`, `post.hidden`,
  `post.restored`, 04) exists when the request returns. An author's own
  action writes no row.
- The author windows (08, TKT-01M29CKRHS) are measured from the post's
  creation, revision 1's time. Once `posts.edit_window_seconds` has passed, an
  edit is `edit-window-closed`; once `posts.delete_window_seconds` has
  passed, a delete or a restore is `delete-window-closed`. A window is open
  for its first N seconds and closed from the Nth on, so a window of 0 never
  opens. A viewer who may moderate the forum is bound by neither window on
  their own post or on anyone else's.
- An author's edit within `posts.grace_edit_seconds` of posting, of a post
  with no visible edit, is written as a grace edit (`grace: true` on the
  `Edit`, docs/03). It is a revision, and history lists it, but the post is
  served with `edited_at` null until a visible edit. The decision is taken
  when the edit is written, so changing the setting later restyles no old
  post. A grace edit never follows a visible one: the actor refuses it as
  `conflict` if a visible edit lands first, so a served `edited_at` is always
  the last visible edit's time.
- An archived thread is `conflict` for all three. In an archived forum an
  author's edit, delete or restore of their own post is `conflict` to
  everyone but an admin, a moderator's own post included, since only an
  admin may post there (04). A moderator's action on somebody else's post is
  moderation and is not refused. A locked thread is
  `conflict` for a member's edit only, since a lock refuses posts and edits
  (08).
- The source is checked and rendered as a reply's is, against the thread and
  the post's own `reply_to`, which an edit keeps. The `Edit` records that
  `reply_to` and the mentions the render found.
- An edit or a delete of a post that is deleted, hidden or moved, a
  moderator's restore of one that is neither deleted nor hidden, and an
  author's restore of one that is not deleted, is `conflict` and appends
  nothing. The
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
  with the account (05). No cookie, no session row, nothing for CSRF to act on,
  but writes must still be JSON, as every client's are. A request that carries
  an `Authorization` header is decided by it: the cookie is not read. A header
  that is not a live bearer credential of an active agent is `401
  unauthenticated` with `WWW-Authenticate: Bearer`. That includes a malformed
  header, another scheme, a revoked token, and an agent that is suspended or
  deactivated. It is not read as an anonymous request, so an agent with a bad
  token learns so at once.
- **Read position** uses `PUT /threads/{id}/read`, exactly as the SPA does. What
  differs is that nothing sets it implicitly: there is no beacon and no
  mark-on-render for an agent, so its position is whatever it last declared, and
  an agent that never calls the endpoint has no position at all.
- **Writes** are charged against a token bucket rather than the human rate
  limit. Exceeding it is `429` with `Retry-After`, and nothing is written. The
  read position and drafts are not charged (08).
- **Provisioning** is an admin's, landed in Phase 2 beside the users and
  permissions screens, because creating an account and issuing its credential
  are one screen, and splitting them across phases means building that screen
  twice (TKT-01M29P9RSW). Every answer is `no-store`:
  - `POST /api/v1/admin/users` with `{kind: "agent", handle, display_name,
    issue_credential}` answers `201` with `{user, credential}`. `user` is the
    account as `GET /admin/users/{handle}` shows it, and `credential` is
    `{token, created_at}`, or null when none was asked for. Any other `kind`
    is `validation-failed`, because a person arrives by signing in. The handle
    follows registration's rules, and a taken one is `conflict`.
  - `POST /api/v1/admin/users/{handle}/credential` answers `201` with a new
    `{token, created_at}` and revokes the live one. An agent that is not
    active, or an account that is not an agent, is `conflict`.
  - `DELETE /api/v1/admin/users/{handle}/credential` revokes it and answers
    `204`. The account is untouched.
  - `PUT /api/v1/admin/users/{handle}/agent-limits` with `{burst,
    refill_seconds}`, where null is the site default, answers with the
    account.

  The token is in those two `201` answers and nowhere else, ever. `GET
  /admin/users/{handle}` shows an admin the agent's `agent`: when its live
  credential was issued and last used, and its limits beside the site's.

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
| `POST /api/v1/threads/{id}/flags` | `POST /t/{id}/posts/{ordinal}/flag` | `reason`, `note` | the JSON route is built (TKT-01M29CP2PJ); the form route is Phase 5 |

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

`img-src 'self' data:` also means a post cannot show a remote image. Decided
2026-09-28 (TKT-01M2K1JKS7): that stays, and the renderer turns a markdown
image with a remote source into a plain link to its URL, so nothing renders
broken and merely viewing the post sends nothing to the image's host. A reader
who chooses to follow the link contacts that host, as with any link (TKT-01M3N24RM0, Render
remote markdown images as links rather than broken images). Attachments are how a
post shows an image. Relaxing `img-src` to `https:` lost because it would
loosen the policy for every page and send every reader's address to any host a
poster names. A server-side proxy lost for now because it is an SSRF surface
that needs hardening of its own; it can be revisited later.

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

A bearer-authenticated request carries no cookie, so rule 3 does not apply to
it and nothing is there for a cross-site form to ride. Rule 1 still does. Its
writes are charged to the per-agent token bucket (08).

Still to come: upload MIME sniffing with an allowlist.
