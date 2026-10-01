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
/admin  /admin/queue  /admin/applications  /admin/forums  /admin/users  /admin/forms  /admin/settings  /admin/audit
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
| `GET  /me` | viewer: user, role, unread counts, pending notifications count, settings. The notifications count is the unread rows, with a `new_application` row counted only while its application is pending (docs/04 "notifications"). An approval-mode applicant (a pre-user session) gets `user: null` plus `pre_user: {provider, tag}`; a null user stays the only signed-out signal. `tag` is opaque, a hash of the stored session hash under its own prefix, and differs from one pre-user session to the next, so the client can tell two applicants who signed in through one provider apart and reload a page left open for the other (TKT-01M3QVWKAG); it grants nothing. `queue` is the moderator queue's item count, or null for a viewer with no queue (TKT-01M29CRJ4E). `site_name` is the `site_name` setting, for every viewer, anonymous and pre-user included, and the shell's title is the same value (TKT-01M29CXK2H). Three statements for a member: the session lookup, one count query and the `site_name` read, plus one batched forum-access read of every forum while the permission cache is cold (`perm.ModeratedForums`), which also answers the read checks the counts need. A viewer who moderates somewhere pays one queue count statement more. An anonymous viewer or a pre-user pays the `site_name` read alone. The name is read on every request rather than cached, so a change from `ketju admin setting set`, another process, shows on the next page too. Neither count reads every thread (docs/04 `forum_thread_counts`) |
| `GET  /forums` | tree of forums with per-forum unread counts → `internal/dto` `ForumTree`. A forum the viewer may not read is absent, not flagged, and so are its children; an anonymous viewer sees public forums only. Roots come in position order with their children, one level deep, under them. `thread_count` is threads with a post; `post_count` is their posts that are neither deleted nor moved, the rule the profile's post count follows, so a deleted post leaves the total and a restored one returns (a moved post counts in the thread it moved to; Phase 1 writes no record that hides or moves a post, and the record that does must leave the total too), read from docs/04 `forum_thread_counts` without reading `threads` (TKT-01M3B6A68B); `unread` is the viewer's threads with an unread post, by `/me`'s rule (read positions and `mark_all_read_at`, mutes not counted), and 0 for an anonymous viewer; `last_post` is the most recently active thread, its last poster and the time, or null. `can_upload` is whether the viewer may upload a file to attach to a new thread there: `upload` in the forum, not archived unless the viewer is an admin, and false for an anonymous viewer (TKT-01M29CXK0K). Each is the forum's own, not its children's. `no-store` |
| `GET  /forums/{slug}/threads?cursor=&sort=` | thread list rows → `internal/dto` `ThreadList`, answered from projections alone. The first page opens with the pins: global pins from every forum the viewer may read, then this forum's own, each by latest activity, at most 50; a pin past its `pinned_until` is no pin, and pins past the 50 sort with the rest. Then 25 threads with a post, newest `last_post_at` first, or newest `created_at` first for `sort=created`; the pins are not counted. `next_cursor` is opaque, bound to its forum and sort, and empty at the end; paging is keyset, so a thread bumped between pages moves up rather than appearing twice. Every page leaves out exactly the forum's threads the first page showed as pins, which the cursor carries, so a pin that ends between pages is not shown again. A cursor is a position, not a credential: one that is malformed or was made for another forum or sort, or any other `sort`, is `bad-request`. `unread` is the viewer's unread posts by the per-post rule (read position and `mark_all_read_at`) and `last_read_ordinal` their position, both 0 for an anonymous viewer. Access answers as the thread page does: an unknown or hidden forum is `not-found`, a signed-in-only one `unauthenticated` to an anonymous viewer, `forbidden` to a member without read. `no-store` |
| `POST /forums/{slug}/threads` | create thread `{title, source, tags, attachments?}` → 201 `{thread, post}`, `internal/dto` `CreatedThread`: the thread page's header and a `CreatedPost` for post 1, each as a later read serves it. A member session is required (anonymous `unauthenticated`, pre-user `forbidden`); a forum hidden from the viewer is `not-found`, an archived one is `conflict` except to an admin (04, checked before `create_thread`, so the refusal says why), and one without `create_thread` is `forbidden`. The title is trimmed and must be 1–200 characters with no control characters; the slug is derived from it (an ASCII fold, at most 60 characters). `tags` must be absent or empty until tag rules exist. The source and `attachments` are checked as a reply's are. The thread is projected before the response, so it can be opened at once |
| `GET  /threads/{id}?page=&ordinal=&lens=&after_log_length=` | thread header + page of rendered posts + lens meta + viewer read position |
| `PATCH /threads/{id}` | rename `{title}` → 200 `internal/dto` `ThreadHeader`, `no-store`: a `ThreadRenamed` record with the title and the slug derived from it, as on create. The thread's author may rename it for `threads.rename_window_seconds` (04, default 24 h), and a moderator at any time; see "Renaming and moving threads" below. Charged to the per-address write limiter |
| `POST /threads/{id}/posts` | `{source, reply_to?, attachments?}` → 201 `{post, ordinal, revision, log_length}`, `internal/dto` `CreatedPost`: the post as `GET /threads/{id}/posts/{ordinal}` serves it, written after the record is durable. A member session is required; without `reply` it is `forbidden`, and not-found or unauthenticated as for any read of the thread. A locked thread is `conflict` except to a moderator; an archived one is `conflict` to everyone, and a thread in an archived forum is `conflict` to everyone but an admin (04), before `reply` is checked. Slow mode is the thread's own value (`PUT /admin/threads/{id}/slow-mode`), or `slow_mode.default_seconds` when it has none, off by default. Moderators are exempt, and agent accounts are not. A reply too soon after the author's last post in the thread is `rate-limited`, with `Retry-After` set to the seconds left, rounded up. The page's `viewer.next_post_at` says the same in advance. An empty source and a `reply_to` naming no post are `validation-failed`; an over-long source is `payload-too-large`. `attachments` is at most 10 distinct blob hashes the poster uploaded ("Attachments" below), recorded in the order sent with repeats dropped. A refused one is `validation-failed` with `{field: "attachments", code}`: `too_many`, `not_yours` for a blob the poster did not upload, `blocked` for a quarantined one, and `unknown` for a hash with no blob, including one collected between the check and the append. A poster without `upload` in the forum is `forbidden`. Every refusal comes before anything is appended. A reply with attachments is projected before the response, so its files load at once (TKT-01M29CXK0K). Charged to the per-address write limiter |
| `GET  /threads/{id}/posts/{ordinal}` | single post (popover, permalink) |
| `GET  /threads/{id}/posts/{ordinal}/replies` | direct replies (inline expander) |
| `GET  /threads/{id}/posts/{ordinal}/history` | revisions, each with actor, time, reason and source; the client diffs them |
| `GET  /threads/{id}/posts/{ordinal}/source` | the current revision's source, `{ordinal, revision, source}`, `internal/dto` `PostSource`, for its author or a moderator to edit, whatever `public_edit_history` says. `no-store`, no ETag. See "Edit, delete and restore" below (TKT-01M3QJYV4Z) |
| `PATCH /threads/{id}/posts/{ordinal}` | edit `{source, reason?}` |
| `DELETE /threads/{id}/posts/{ordinal}` | soft delete; `POST …/restore` to undo |
| `PUT  /threads/{id}/read` | `{ordinal}` advance read position; `DELETE` marks unread. Both answer 200 `internal/dto` `ReadPosition`; see "Read position" below |
| `POST /threads/{id}/watch` `{level}` | watch/mute |
| `POST /threads/{id}/flags` | `{ordinal, reason, note}` report a post (08 "Flags", TKT-01M29CP2PJ). A member session and read access are required. `reason` is `spam`, `abuse`, `off-topic` or `other`, and `other` needs a `note`, at most 1000 characters (`validation-failed`); an ordinal the thread lacks is `not-found`; the member's own post is `forbidden`, shown or not and in an archived thread too (maintainer decision 2026-09-29, TKT-01M3QQWA5Z); any other post in an archived thread is `conflict`, and so is a post already removed or moved, except for a member whose flag on it is still open, whose repeat gets the receipt like any other. Answers 200 `internal/dto` `FlagReceipt`, `{"received": true}`, the same for a new flag, a member's repeat (which adds no row) and the flag that hides the post, and nothing else: no count, no reporter, no outcome. If the flag reaches `flags.auto_hide_threshold` and the hide cannot be appended, the flag is kept, the answer is the same receipt, and the failure is logged; the sweep hides the post within a minute. So `unavailable` (a quarantined thread) means nothing was kept. Charged to the write limiters. `no-store` |
| `POST /render` | `{source, thread_id?, reply_to?}` → `{html, mentions: [{handle}], quotes: [{ordinal, deleted}], reply_to}` (composer preview; the create path records the same facts from the same render, `internal/dto` `RenderResult`). A member session is required (anonymous `unauthenticated`, pre-user `forbidden`). With `thread_id` the viewer must be able to read the thread, quotes resolve against it and `reply_to` is checked (`validation-failed` when no such post; a deleted post is accepted); without it quotes render optimistically and `reply_to` is `bad-request`. Charged to the per-address write limiter; the body limit is the post limit |
| `GET  /drafts/{thread_id}` `PUT` `DELETE` | composer drafts, one per user per thread → `internal/dto` `Draft`; see "Drafts" below |
| `GET  /forums/{slug}/draft` `PUT` `DELETE` | new-thread drafts with their title, one per user per forum → `internal/dto` `ForumDraft`; see "Drafts" below (TKT-01M3K9XBJA) |
| `GET  /unread` `/new` `/participated` `/watching` | cross-forum thread lists |
| `GET  /search?q=&forum=&author=&after=&before=` | FTS5 results with snippets |
| `GET  /notifications` `POST /notifications/read` | |
| `POST /blobs?thread={id}` or `?forum={slug}` (multipart) | upload one part named `file` → 201 `internal/dto` `Attachment`, `no-store`. The viewer is admitted before the body is read: anonymous `unauthenticated`, pre-user `forbidden`, the forum hidden `not-found`, without `upload` or in an archived forum but to an admin `forbidden`. Neither or both of `thread` and `forum`, another part, or a second part is `bad-request`, and a second part is found before the file is stored, so the refused request leaves nothing behind (Terva review 1736). The type is sniffed, and a refused file is `validation-failed` with `{field: "file", code}`: `type_not_allowed` or `blocked`. Over `limits.attachment_mb` is `payload-too-large`. See "Attachments" below (TKT-01M29CXK0K) |
| `GET /blobs/{hash}` | outside `/api`, at `/blobs/…`: an attachment's bytes, to a viewer who may read a post naming it, and `not-found` to everyone else. `private, no-cache`, revalidated against the hash as `ETag`, with access checked before any 304; `Range` and `HEAD` work. See "Attachments" below (TKT-01M29CXK0K) |
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
| `rename-window-closed` | 403 | The thread is the viewer's, and `threads.rename_window_seconds` has passed since it was started, so only a moderator may rename it (08) |
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
lock/pin/move/tag, split/merge, user suspend/anonymize, thread
archive, forum CRUD and ordering, group and permission editing, form
versions, settings, audit log query, `reindex` and `fsck` triggers with
progress via SSE.

Each route is gated on the role docs/08's "Primitives" table names for its
action, which is the one place authority is stated. Archiving a thread is
admin-only there; locking, pinning, moving, renaming, retagging, splitting
and merging are moderator actions. For lock, unlock, pin, unpin, move and
slow mode, "moderator" means `moderate` in the thread's forum, by role or
through a group, so those routes and the moderator queue are behind the
`moderates` gate and every other route keeps its role gate (08 "Decided
2026-09-30", TKT-01M3RQP0TG). Renaming is the exception to the prefix:
an author may rename their own thread, so it is `PATCH /threads/{id}`
rather than an admin route (TKT-01M29CKRH9).

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
| `GET /admin/users/{handle}` | staff | `AdminUserDetail`: the account and its stored groups, each marked `admin` or `idp`. `identities` and `sessions` are an admin's only, and `null` to a moderator. `suspension` is `{reason, until}` for a suspended account, to moderators and admins alike, with `until` null for no end; it is `null` for any other status (TKT-01M3QMRE47) |
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

#### Forums (TKT-01M29CXK19)

The `/admin/forums` screen: the forum tree, with every forum's name,
description, place and visibility, and archiving. Every route is an
admin's. Holding `moderate` in a forum, by role or through a group, opens
none of them, since admin screens keep their role gates (08 "Decided
2026-09-30"). Every answer is `no-store`, and every write answers with the
whole `ManagedForumList`, read in the write's transaction, because a move
can renumber the siblings. Every write is audited in the transaction that
makes it (04 "audit_log"). A forum id that names nothing is `not-found`,
and an id that is not a positive integer is `bad-request`.

| Route | What it does |
|---|---|
| `GET /admin/forums` | `ManagedForumList`: every forum, archived and restricted ones included, in the forum index's order, each root followed by its children, with its `parent_id` (null for a root), `position`, and thread and post counts |
| `POST /admin/forums` `{slug, name, description, parent_id, visibility}` | 201, with the list and no `Location`, since no route reads one forum. A new forum, last among its siblings, and `forum.created`. The slug is 1 to 60 characters of a-z, 0-9 and `-`, with no `-` first or last, and a taken one is `conflict`. The name is 1 to 100 characters once trimmed, with no control characters, and the description at most 1000. `parent_id` must name a root, or it is `validation-failed`; one that is not a positive integer is `bad-request`. An empty `visibility` is `members` |
| `PATCH /admin/forums/{id}` `{name?, description?}` | `forum.updated`, with each changed field's `from` and `to`. Neither field is `validation-failed`, and a change to what the forum has writes nothing. The slug cannot change: `/f/{slug}` is in every shared link, and nothing redirects an old one |
| `POST /admin/forums/{id}/move` `{parent_id, before?, after?}` | `forum.moved`. Places the forum under `parent_id`, or at the top level when it is null, before or after the named sibling there, or last when neither is sent. `parent_id` is required: a body without it, or with one that is neither a positive id nor null, is `bad-request`, as is a negative `before` or `after`, so an incomplete request never reads as a move to the top level. A child of a child, a parent for a forum that has children, both `before` and `after`, and a sibling that is not under that parent are `validation-failed`. A move to where the forum already is writes nothing |
| `PUT /admin/forums/{id}/visibility` `{visibility}` | Through `perm.SetVisibility`: `forum.visibility_changed`, and the decision cache cleared on commit, so it applies from every viewer's next request. A value other than `public`, `members` or `restricted` is `validation-failed` |
| `PUT /admin/forums/{id}/archived` `{archived}` | Through `perm.SetForumArchived`: `forum.archived` or `forum.unarchived`, and the cache cleared. No thread moves and no thread record is written. Read access is unchanged. Posting is refused to everyone but admins, and moderators keep hiding, locking, pinning and moving threads out (04 "permissions") |

The screen moves a forum with Move up and Move down buttons and a "Place
under" select, rather than by dragging, so it works from the keyboard. It
says a visibility change takes effect within a minute, the permission
cache's lifetime at most. Beside each archive control it says, whatever the
forum's visibility: "Anyone who could read it still can. Only posting stops,
except for admins. Moderators can still hide, lock, pin and move threads
out, and only admins can move threads in."

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
`moderate` on one forum through a group as well as staff by role. The thread
moderation routes below use the same gate (TKT-01M3RQP0TG). The
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

The client reads it as the whole question. At both widths the top bar
shows a queue button whenever `queue` is a number, with the count as its
badge and no badge at zero, and the admin shell offers the Queue section
on the same condition, so a member moderating one forum through a group
gets both. The screen at `/admin/queue` lists the items in the server's
order with More for the next page. A flag item shows the post as a
moderator reads it, its state, and its reports, with an optional note and
Remove post, Keep post and Dismiss flags, each confirmed first. Keep post
is not offered on a post a moderator hid by hand, which the screen says is
the thread page's to restore, and Remove post is not offered on one
already down. A redacted post (`redacted: true`) reads "Removed
permanently" and is offered neither Remove post nor Keep post, only
Dismiss flags, since nobody can show it again (TKT-01M3R1WPNS). A success takes the item out and fetches `/me` again for
the badge; `conflict` says another moderator has already handled it and
fetches the queue again, as any other failure does. An application item
opens the applications screen's approve and deny controls in place. The
author's link goes to the users screen for a moderator or admin by role,
and to the profile for anyone else, whom that screen would refuse.

#### Locking and pinning threads (TKT-01M29CKRGS)

Thread moderation follows the forum grant (08 "Decided 2026-09-30",
TKT-01M3RQP0TG). Each route is behind the `moderates` gate, which admits a
member who holds `moderate` in any forum, by role or through a group. The
threads service then decides the forum: it requires `moderate` in the
thread's forum as the log stands where the record lands. It projects the
thread up to the log's length, admits the viewer from that row, and appends
at that length, so a move landing in between makes the request decide
again. A refusal is answered only if the log is still that length, so a
`conflict` for an action that would change nothing is never given to a
viewer a move has just taken the thread from. A moderator of one forum cannot lock or pin a thread that has just
left it, and of two locks at once the second is `conflict`. A global pin
stays a moderator's or admin's by role, because it heads the thread list of
every forum. Each appends one record to the thread log, projects it before answering,
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
- `forbidden` for a pin or unpin with scope `global`, or a pin or unpin
  while a global pin is active, which would replace it or take it down, to a
  viewer who is not a moderator or admin by role;
- `conflict` for an action that would change nothing: locking a locked
  thread, unlocking an open one, unpinning a scope that is not the active
  pin's (an expired pin included), or pinning with the active pin's scope
  and `until`;
- `validation-failed` for a scope other than `forum` or `global`, an
  `until` not after now, or a reason over 500 characters.

#### Slow mode (TKT-01M29CRJ50)

`PUT /admin/threads/{id}/slow-mode` with `{seconds}` sets the minimum number
of seconds between one member's posts in the thread, 0 to 86400, where 0 is
off. It answers 200 `internal/dto` `SlowMode`, `{seconds}`, with
`no-store`, and it is charged to the per-address write limiter. The gate and
the `moderate` check are the same as for locking and pinning. No record is
appended, so the transaction that writes the value checks that the log is
still the length the viewer was admitted at before it commits, and decides
again otherwise. The value is
kept in `thread_slow_mode` (04) and appends nothing to the log (08
"Primitives"). It overrides `slow_mode.default_seconds`, and an explicit 0
turns slow mode off under a non-zero default. The change and its
`thread.slow_mode` audit row, with `"from"` and `"to"`, are written in one
transaction. `"from"` is null when the thread had no value of its own.

Refusals come in lock's order, and each writes nothing:
- `unauthenticated` or `forbidden` from the gate;
- `bad-request` for a body with no `seconds`, a `seconds` that is not an
  integer, or any other field;
- `forbidden` without `moderate` in the thread's forum;
- `conflict` on an archived thread. An archived forum does not refuse it;
- `validation-failed` for seconds outside 0 to 86400;
- `conflict` for the value the thread already has. A thread with no value
  of its own has none to repeat, so setting the site default there is a
  change: it pins the thread against a later change of the default.

Slow mode reaches the thread page through its viewer block, never its
posts. `slow_mode_seconds` is the effective interval, which every viewer
gets. `next_post_at` is when the viewer's slow mode ends: their newest post
in the thread plus the interval, in Unix ms. It is null while they may post,
for a moderator, who is exempt, and for an anonymous viewer. Setting slow
mode moves no response cache key, because the cached posts do not depend on
it. The page's ETag changes, because it hashes the viewer block.

#### Renaming and moving threads (TKT-01M29CKRH9)

Renaming lives on the thread resource, `PATCH /threads/{id}` with
`{title}`, because an author may rename their own thread; the service
decides who may. Moving is a moderator's, behind the `moderates` gate, as
lock and pin are, and needs `moderate` in both forums:

| Route | Body | What it does |
|---|---|---|
| `PATCH /threads/{id}` | `{title}` | `ThreadRenamed` with the title, trimmed, and the slug derived from it |
| `POST /admin/threads/{id}/move` | `{forum}`, the destination's slug | `ThreadMoved` to that forum |

Both answer 200 with the thread's `ThreadHeader` as its page now serves it,
`no-store`, after projecting the record. Bodies are decoded strictly and
bounded at 4 KiB. Each request is decided on the log its record lands in:
the thread is projected up to the log's length, the viewer admitted from
that row, and the record appended at that length, so a record landing in
between makes the request decide again. Two moves at once cannot both pass
on a forum the first one took the thread out of.

The old slug keeps working after a rename: a thread is found by its id, and
the client replaces a stale slug in the address, keeping the ordinal,
`/history`, the page, the lens and the hash (`canonicalThreadUrl`).

A rename is refused in this order, and each refusal writes nothing:
- `unauthenticated` or `forbidden` for a viewer with no member session,
  then `bad-request` for a body with no `title`;
- `not-found`, `unauthenticated` or `forbidden` as for any read of the
  thread;
- `forbidden` to anyone but the thread's author or a viewer with
  `moderate` in its forum;
- `conflict` on an archived thread;
- for the author without `moderate`: `conflict` in an archived forum (04)
  and in a locked thread, `forbidden` without `reply`, and
  `rename-window-closed` once `threads.rename_window_seconds` has passed
  since the thread was started. A moderator is bound by none of these;
- `validation-failed` for a title refused as on create, and `conflict` for
  the title the thread already has.

The thread page tells the author in advance. Its viewer block carries
`rename_until`, the end of the viewer's rename window in Unix ms: the
thread's start plus `threads.rename_window_seconds`. It is set only for the
thread's author without `moderate`, while every check above that binds the
author passes, and one helper makes those checks for both the rename and the
page, so the offer and the refusal agree. It is null for everyone else, and
so always null for a moderator, who may rename at any time. Like
`next_post_at` it moves no response cache key, and the ETag changes when it
does (TKT-01M3QXV399).

A move is refused in this order, and each refusal writes nothing:
- `unauthenticated`, or `forbidden`, from the gate, then `bad-request` for
  a body with no `forum`;
- `not-found`, `unauthenticated` or `forbidden` as for any read of the
  thread;
- `forbidden` without `moderate` in the thread's forum;
- `conflict` on an archived thread;
- `not-found` for a destination that does not exist or is hidden from the
  mover, and `forbidden` for one they cannot read;
- `forbidden` without `moderate` in the destination;
- `conflict` for an archived destination, except to an admin. Moving a
  thread out of an archived forum is open to its moderators (08,
  TKT-01M3N24RHD);
- `conflict` for the forum the thread is already in.

The projection writes the audit rows (04 "audit_log"): a moderator's rename
of somebody else's thread writes `thread.renamed` with no title, and every
move writes `thread.moved` with the two forum ids. An author's rename of
their own thread writes none. Both records move the log length, and so the
response cache key; the header, forum included, is drawn from the
projection row on every request.

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

#### The audit log (TKT-01M29CP2R4)

The log says who did what, and from which address, so it is an admin's
alone. Moderators see none of it, by role or through a group (08 "Decided
2026-09-29", "Decided 2026-09-30"). Anonymous and inactive viewers are
`unauthenticated`, and a pre-user, member or moderator is `forbidden`. The
route is behind the admin gate, and the service asks again. The answer is
`no-store`. Nothing in the API changes or removes a row (04 "audit_log").

`GET /admin/audit?actor=&system=&action=&target_type=&target_id=&from=&to=&cursor=`
answers `AuditPage`: 50 rows per page, newest first by id. Every parameter
is optional, and the ones given are combined:

| Parameter | Selects |
|---|---|
| `actor` | the rows a member did, by handle in any case. A handle nobody has is an empty page, not an error |
| `system` | `1`: the rows no account did (the command line, a job, the auto-hide, a thread-log record with no actor). It is not a value of `actor`, because a member may hold any handle. With `actor` it is `bad-request` |
| `action` | one action, or every action starting with it when it ends in `.`, such as `user.` |
| `target_type` | one of the target types in 04 "audit_log"; anything else is `bad-request` |
| `target_id` | one thing of `target_type`, in that type's id format. Without `target_type` it is `bad-request` |
| `from`, `to` | Unix ms: `from` the first included, `to` the first not. `0` is a bound like any other, so `to=0` selects nothing. Anything but a non-negative integer is `bad-request` |
| `cursor` | the previous page's `next_cursor`. A cursor the server did not issue is `bad-request` |

Each entry carries the row's `id`, `at`, `action`, `details` (the row's
`details_json` as written, to be shown as text and never as markup) and
`ip` (empty when there was no request). `actor` is the member drawn as a
post's author is, a deleted member's placeholder for an account the
database lacks, or null when no account did it. `target` is the row's
`type` and `id` as written, with the `label` it reads as now and an `href`:

| Type | `label` | `href` |
|---|---|---|
| `user` | the handle; `deleted-user-<id>` for an account the database lacks | `/u/{handle}`, none for an account the database lacks |
| `thread` | the current title | `/t/{id}/{slug}` |
| `post` | the thread's title and `#{ordinal}` | `/t/{id}/{slug}/{ordinal}` |
| `forum` | the name | `/f/{slug}` |
| `group` | the name | `/admin/users?tab=groups` |
| `flag` | `flag {id} on {title} #{ordinal}` | the flagged post's link |
| `setting` | the key | `/admin/settings` |
| `application` | `application {id}` | `/admin/applications?id={id}` |
| `permission` | the id as written | `/admin/users?tab=permissions` |
| `form` | the key | `/admin/forms` |
| `identity` | `identity {id}` | none |
| `blob` | the first 12 hex digits of the hash | none: a deleted blob has no page |

A thing that no longer resolves has its raw id as its `label` and no
`href`. `related` is empty for every row but `thread.moved`, where it holds
the forum the thread left and then the forum it went to, resolved as a
`forum` target is. `actions` is every action written today (04
"audit_log"), for the screen's action filter. `next_cursor` is empty on the
last page. Each kind of thing a page names is read in one statement for
the whole page, and each filter searches an index (migrations 0006 and
0019).

The screen at `/admin/audit` is offered to admins only, and asks the
server nothing for anyone else. Its address carries the filters as the
endpoint's own parameters, so a filtered view is a link that can be pasted
and Back returns to the list before. The form edits a draft that Apply puts
in the address, so typing fetches nothing. The action filter offers
`actions`, and each noun's prefix before them, and it takes free text. The
dates are local, and "To" includes its day. The table shows the time, who
acted ("System" and its `via` when no account did), the action, the target
and any `related` forums as links, the details as keys and values drawn as
text, and the address. Older follows `next_cursor` and appends the page.

#### Site settings (TKT-01M29CXK2H)

The `/admin/settings` screen: every runtime setting in the `settings`
table (04 "settings"), and the parts of `config.toml` it does not hold.
Both routes are an admin's. Settings change what the site does, the
registration mode and the agent rate limits among them, and holding
`moderate` somewhere opens neither route, since admin screens keep their
role gates (08 "Decided 2026-09-30"). The service asks for an admin again
behind the gate. Every answer is `no-store`.

| Route | What it does |
|---|---|
| `GET /admin/settings` | `SiteSettings`. `settings` is every key in the registry (`settings.All()`), sorted by key, each with its `doc`, its `kind` (`text`, `integer` or `boolean`), `allowed`, the registry's words for what it takes, `choices`, a closed list's values or empty, its `default` and current `value` as JSON of its kind, and `is_default`. `config` is every top-level section of `config.toml`, in the file's order, each with the reason it is set there and the keys under it. It carries no values: `ketju config check` shows those to an operator, with secrets redacted |
| `PUT /admin/settings/{key}` `{value}` | The `SiteSetting` as it now stands, read in the write's transaction, so a failure building the answer rolls the change back rather than reporting failure for a change that was made. Through `settings.SetIn`, which writes `setting.changed` with the admin, the request's address and the old and new values in that transaction (04 "audit_log"). A key the registry does not have is `not-found`. A value the key does not take, `null` included, is `validation-failed`, with no field errors: the listing's `allowed` says what the key takes, and one PUT names one key. A body without `value` is `bad-request` |

Who last changed a key follows the row. Set from the screen,
`updated_at` is the time and `updated_by` the admin. Set from the command
line, `updated_by` is null and `updated_at` is the time, because
`ketju admin setting set` writes no account and the row always has a time.
Never set, both are null. Nothing caches a setting: each reader asks the
table, so a change applies from the next request, and `site_name` reaches
`/me` and the shell's title the same way.

The screen at `/admin/settings` is offered to admins only. It draws one
field per key the listing holds, grouped by the part of the key before its
first dot, so a key added to the registry appears with no client change: a
checkbox for a boolean, a select for a key with `choices`, and a text field
otherwise, with a numeric keyboard for an integer. Each field shows the key's
`doc`, `allowed` and default, and saves itself. A refusal repeats
`allowed` beside the field. "Saved." clears on any edit, and an answer shows
it only while the field still holds what was sent. A key away from its
default offers Reset to default. Below the key, "Last changed" names the
admin and the time, says "from the command line" with the time when
`updated_by` is null, and says nothing for a key never set. Below the
fields, "Set in config.toml" lists each section with its reason and its
key names, and says to change them in `config.toml` and restart. The three
wordmarks, in the desktop bar, the phone bar and the applicant's bar, show
`site_name` from `/me`.

### Thread response shape (abridged)

```json
{
  "thread": {"id": "3yymw47g00001", "slug": "why-we-left-slack", "title": "…",
             "forum": {"slug": "general", "name": "General"},
             "post_count": 212, "log_length": 483920, "locked": false, "pinned": null,
             "tags": ["meta"], "created_at": 1757500000000},
  "page": {"number": 3, "size": 25, "total_pages": 9},
  "lens": {"kind": "author", "value": "drew", "shown": 7, "hidden": 205},
  "viewer": {"last_read_ordinal": 60, "watch": "watch", "can_reply": true, "can_upload": true, "can_moderate": false,
             "slow_mode_seconds": 30, "next_post_at": null, "rename_until": null,
             "author_windows": {"edit_seconds": 86400, "delete_seconds": 900},
             "forum_read_only": false},
  "posts": [
    {"ordinal": 61, "revision": 2, "author": {"handle": "…", "display_name": "…", "avatar": "…", "role": "member", "kind": "human"},
     "created_at": 1757500100000, "edited_at": 1757500200000,
     "reply_to": 57, "reply_to_suppressed": false, "reply_count": 3,
     "html": "<p>…</p>", "attachments": [], "deleted": false, "removal": null, "redacted": false,
     "moved_to": null, "machine_authored": false, "edited_by_moderator": false}
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

A hard-redacted post (03 "Hard redaction", TKT-01M29CXJZ9) is served to
nobody, moderators included: its log holds no text. It reads as
`deleted: true, removal: "moderator"` with no body, and its history is
the tombstone to everyone but a moderator, who is served its revisions
with no source and no reason. An edit, delete or restore of it is
`conflict`. `redacted: true` tells it from a moderator's hide, on the
post and on its history, and is served to every reader alike, since it
comes from the index and the tombstone is the same for all of them
(TKT-01M3R1WPNS). `removal` beside it is unchanged: `"moderator"`, or
`"author"` if its author had deleted it before the redaction. A client
reads `redacted` as "removed permanently": nobody can restore or edit it.
`redacted` is false for every other post, removed or not.

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
- The thread page tells the author the windows in advance. Its viewer block
  carries `author_windows`, `{edit_seconds, delete_seconds}`, read through
  the same settings and the same conversion the refusals use, so a setting
  past what a Go `time.Duration` holds is sent as the longest one,
  9223372036 seconds, and the client's window and the server's agree to the
  second. It is null for a viewer who may moderate the forum, whom neither
  window binds, and for a viewer who is not a member, who has no posts to
  act on. Like `rename_until` it moves no response cache key, and the ETag
  changes when it does. The client offers Edit, Delete and Restore on the
  viewer's own post only while the window is open, and shows a refusal's
  words when its clock and the server's disagree (TKT-01M3PTT97X). A viewer
  whose page says `can_moderate`, by role or through a group, is offered
  Edit and Delete on every post that is not removed, moved or redacted, and
  Restore on every removed post served with its body, at any age and without
  `reply` (TKT-01M3QHKD21).
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
  moderation and is not refused. The thread page's viewer block carries
  `forum_read_only`, true exactly when that refusal applies to the viewer's
  own posts: the forum is archived and the viewer is not an admin. Like
  `author_windows` it moves no response cache key, and the ETag changes when
  it does. The client offers nothing on the viewer's own post while it is
  true (TKT-01M3QHKD21). A locked thread is
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

`GET /threads/{id}/posts/{ordinal}/source` answers the post's current
revision as `internal/dto` `PostSource`, `{ordinal, revision, source}`, so an
author can edit a post whatever `public_edit_history` says (TKT-01M3QJYV4Z).
History is not widened: it still follows the rules above.

- A member session is required (anonymous and an inactive session
  `unauthenticated`, pre-user `forbidden`), and access to the thread is the
  thread page's: a thread hidden from the viewer is `not-found`, their own
  post included. An ordinal past the end is `not-found`.
- The readers are whoever may edit the post now or later. A viewer with
  `moderate` in the thread's forum reads any post that is visible or whose
  content "Removed posts" serves them, which is every removed post but a
  moved or a hard-redacted one; those are `conflict`. The post's author
  reads their own visible post. Anyone else is `forbidden`, whatever state
  the post is in, so the refusal says nothing about it.
- A post a moderator hid, or one hidden pending review, is `forbidden` to
  its author, as the edit would be, although the thread page shows them its
  body. A post the author deleted, a moved one, or a redacted one is
  `conflict` to them: there is nothing to edit.
- A hard-redacted post (03 "Hard redaction", TKT-01M29CXJZ9) serves its
  source to nobody, moderators and admins included.
- The windows, a lock and an archive, of the thread or of its forum, are not
  checked: they refuse the edit, and `PATCH` answers that refusal. Reading
  one's own words gives nothing away.
- `no-store` with no ETag, and the response cache is not used, so no
  reader's source is kept for another, and an edit starts from the
  revision the server holds now.

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

#### Drafts (TKT-01M29CA9CE, TKT-01M3K9XBJA)

`/drafts/{thread_id}` is the composer's autosave for a reply, one per user
per thread (04 `drafts`), kept on the server so it follows the user to
another device.

- `GET` answers 200 `internal/dto` `Draft` `{thread_id, source, reply_to,
  updated_at, revision}`, or 204 with no body when the viewer has no draft
  for the thread. Having no draft is the usual state rather than an error,
  and `not-found` stays reserved for a thread the viewer cannot see.
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

`/forums/{slug}/draft` is the new-thread form's autosave, one per user per
forum (04 `drafts`), with the thread's title as well as its body
(TKT-01M3K9XBJA). It follows the reply draft's rules, with these
differences:

- `GET` answers 200 `internal/dto` `ForumDraft` `{forum, title?, source,
  updated_at, revision}`, where `forum` is the slug and `title` is left out
  when the draft has none, or 204.
- `PUT` takes `{title?, source}` and replaces the whole draft, so a `PUT`
  without `title` clears a stored one. `source` is required but may be
  empty. The title follows a thread title's rules but for two: it may be
  empty, and it is stored as typed, not trimmed, because it is unfinished.
  More than 200 characters (`MaxTitle`) or a control character is
  `validation-failed`.
- Saving is refused as starting a thread there would be: an archived forum
  is `conflict` to everyone but an admin, and it needs `create_thread`
  (`forbidden` otherwise). `GET` and `DELETE` need only read access to the
  forum, so a member can still discard a draft in a forum since archived.
- A forum that does not exist or is hidden from the viewer is `not-found`.

Every draft carries a `revision`, a count per draft key that only grows,
and a client orders a local copy of a draft by it rather than by comparing
its clock with `updated_at` (TKT-01M3NS1931):

- A `PUT` adds one, starting from 1. A `DELETE` of a live draft adds one and
  keeps the row as a tombstone, with its text emptied, which a `GET` answers
  as 204. So a draft deleted and started again answers a higher revision
  than any copy of the old one, whatever the server's clock does. A `DELETE`
  with no live draft changes nothing. Tombstones are never swept, because a
  sweep would restart the count.
- The server computes it in the write's own transaction from the stored
  value. No clock enters it.
- A 204 from any draft route, `GET` or `DELETE`, carries the key's revision
  in a `Draft-Revision` header: the tombstone's, or 0 for a key that has
  never been saved.
- An agent's writes to either draft route are not charged to its bucket
  (see "Agents on the API" below).

**Decided by the run (2026-10-01, pending maintainer review):** both kinds
of draft carry the composer's attachments. `PUT` takes `attachments?`, the
same list of hashes a post takes and checked by the same rules (see
"Attachments" below), but it adds no reference. `GET` answers them as
`attachments: [Attachment]`. A tombstone holds none. Blob GC keeps a blob
that a draft's attachments name, as it keeps one a draft's source names.
This follows the maintainer's decision of 2026-09-28 (TKT-01M3K9XBJA) that
a draft restored on another device should come back whole. Leaving
attachments out of drafts lost, because a restored draft would silently
drop files whose bytes the collector then deletes after seven days.

#### Search (TKT-01M29CTY8T, TKT-01M3V70X0F)

**Decided by the run (2026-10-01, pending maintainer review):**
`GET /search?q=&forum=&author=&after=&before=&cursor=` answers
`internal/dto` `SearchResults` with `no-store`, and never from the response
cache. It searches the index docs/04 "search" describes.
- **Who.** Anyone who may read a forum, an anonymous reader included, as
  for thread pages. A pre-user never reaches it (05 "mode = approval").
  Results come only from the forums `perm.ReadableForums` lists for the
  viewer, and that list is a condition of the one statement that matches,
  orders and pages. The order uses only facts of each result's own post,
  so neither a page, its order nor its length depends on a forum the
  viewer cannot read.
- **`q`** is required: 1 to 200 characters once trimmed, and at most 16
  words, or `validation-failed`. Its words must all match, and a
  double-quoted run of words is a phrase. Nothing else is query syntax:
  `OR`, `NOT`, `NEAR`, `*`, `^` and `column:` are searched as words.
- **`forum`** is a slug, and selects that forum's own threads, not its
  children's. Access is answered as the thread list answers it: unknown or
  hidden is `not-found`, signed-in-only is `unauthenticated` to an
  anonymous reader, and no read is `forbidden`.
- **`author`** is a handle in any case. A handle nobody has is an empty
  page, not an error, as the audit log's `actor` is.
- **`after`, `before`** are Unix ms, `after` the first included and
  `before` the first not, as the audit log's `from` and `to` are. Anything
  but a non-negative integer is `bad-request`.
- **Order and pages.** Newest first (`created_at`, then the row), 25 to a
  page. `next_cursor` is opaque and
  empty on the last page, and a cursor the server did not issue is
  `bad-request`. A search reaches at most 1000 results. No total is
  returned.
- **A result** is `{thread: {id, slug, title}, forum: {slug, name},
  ordinal, author, created_at, snippet}`. `ordinal` is 0 when the thread's
  title matched. `title` is the thread's title now. `author` is drawn as a
  post's author is (`member.Author`). `snippet` is a list of `{text,
  match}` segments of plain text, never markup, and the client marks the
  matches.
- **Fresh when served.** Each hit's thread is judged at the log length the
  request reads under it. Where the projection trails that length, the
  thread is projected up to it first, as a suspension's hide does, and its
  forum and title are read again. A hit is served only if that forum is
  one the viewer may read (and the `forum` asked for, if any). A post must
  also be still visible and still at the revision that was indexed, and a
  title must still be the thread's title. So a thread moved into a forum
  the viewer cannot read is left out even before its projection catches up.
  A post deleted, hidden, moved, redacted or edited since it was indexed is
  left out until the indexer catches up, and is never shown with old text.
  A thread that cannot be projected, or is quarantined, has its results left
  out.
- **`indexing`** is true while `serve`'s first pass over threads whose
  index trails the log is still running (03 "Durability and recovery"),
  so the screen can say that results may be missing.

Alternatives: refusing anonymous readers lost, because they can already
read every public post and search shows nothing more. A total count lost:
it costs a second full match per search, and the freshness check can leave
out a counted row. Relevance order (FTS5's bm25) lost, although it is what
a search box usually promises: bm25 scores with index-wide statistics, so
posts in forums the viewer cannot read would shift the order, and so the
pages, of what they can read (Terva review 1728). A relevance order that
is safe would need statistics per forum set, which FTS5 does not keep; it
is left for later, with a sort option. Passing FTS5's own
syntax through lost, because a malformed query would be a server error and
a column filter names internals. Calendar dates lost to the audit log's
precedent: the client knows the reader's time zone and the server does not.

The screen at `/search` (TKT-01M3V70X0F) keeps the endpoint's parameters,
`cursor` included, in its address, so a search is a link and Back returns
to the one before. The form edits a draft that Search puts in the address,
so typing fetches nothing. The query field comes first and has focus when
the screen opens with no query. The filters are the forum (a list of the
forums `/forums` gives), the author's handle, and From and To dates, which
are local, and To includes its day. On the phone layout the filters fold
under a "Filters" disclosure, open when any is set. Each result shows the
thread's title, linking to the post's permalink or to the thread for a
title match, then the forum, the author, the time (`PostTime`) and the
snippet with each match in `<mark>`. "More results" follows `next_cursor`,
as pages do elsewhere (06). With no query the screen says what search
covers. With no results it names the query and, when a filter is set,
offers to clear the filters. While `indexing` is true it says that some
posts may be missing. It is part of the reader bundle, since a lazy chunk
would count toward the budget all the same (see "Preact client").
Alternatives: searching as the reader types lost, because it sends a query
per keystroke and moves results under a finger on a phone, and the audit
screen already set the draft-then-apply pattern. A search field in the top
bar lost to the shell as built, where the top bar's link to `/search` is
the visible control.

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
- **`GET /me`** answers an agent as the member it is, from its credential:
  its handle, display name, role, `kind: "agent"`, settings and counts, as for
  a signed-in member (TKT-01M3QH3X58). Nothing in it says the request used a
  credential, because `user.kind` already names an agent.
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
  custom properties and light/dark themes. Target bundle: < 70 KB gzipped
  for the thread view. CI enforces it, reading 70 KB as 70000 bytes and
  counting everything the reader loads except fonts and the lazy admin
  chunk; the number is committed in `web/client/budget.json`
  (TKT-01M29CGBYH). The maintainer raised it from 60 KB on 2026-09-30.
  Phase 2 added moderation controls, drafts and thread actions to the
  reader, putting it near 55 KB with several reader tickets still to come.
  Lazy-loading small panels did not help, because the budget counts every
  reader chunk and a split adds a shared chunk. Hand-written CSS is decided over a utility framework
  (10 Q21, TKT-01M29BW9NW), checked against that budget before adoption: it
  adds no runtime or build dependency, so every byte of it is a byte we wrote,
  and the mockups in `docs/mockups/` are already written that way.
- **Page size:** 25 posts by default, 10–50 as a user setting (06). The
  default is decided (10 Q13) and appears in shared URLs, so the API and the
  client both treat it as a constant rather than a tunable.
- **Fonts:** IBM Plex Sans, IBM Plex Mono and Newsreader, as in the
  mockups, self-hosted as subset WOFF2 files embedded in the binary and
  served from the same origin. The CSP allows no third-party font host.
  They are cached apart from the code and do not count toward the 70 KB
  budget, which measures the thread view's JavaScript and CSS. Two
  alternatives lost. A system font stack would drift from every mockup, and
  counting fonts inside the budget would force dropping Newsreader.
  (Decided 2026-09-24 in Phase 1 grooming.)
- **Theme:** `system`, `light` or `dark`. **Decided by the run (2026-10-01,
  pending maintainer review; TKT-01M29CCC0X, TKT-01M29CXK4E):** a member's
  choice is stored on the account, as `theme` in `prefs_json`, and `/me`'s
  `settings` carries it, so it follows them to every device. The browser
  keeps a copy in `localStorage` only so the first paint is not the wrong
  theme. On every load the account's value wins over the copy, and
  replaces it. A signed-out reader's choice stays in the browser.
  Alternatives: a per-device theme lost because docs/09 lists theme among
  the account's settings and the settings screen's criterion asks for it
  to persist across devices; letting the device's copy win lost because a
  change made on one device would never reach the others.
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
client's `/t/{id}` and `/f/{slug}` paths. A thread's slug is advisory there
as everywhere, and a forum's slug is its key. A test in
`internal/web/api` checks that every viewer-state route it serves has a row
here.

| JSON route | HTML client | Fields | Service call |
|------------|-------------|--------|--------------|
| `PUT /api/v1/threads/{id}/read` | the thread page itself: `GET /t/{id}/{slug}?page=N` marks the highest ordinal it renders (06), and `&mark_read=M` marks `M` instead | `mark_read` (query) | `MarkRead`: forward only, clamped to the last post |
| `DELETE /api/v1/threads/{id}/read` | `POST /t/{id}/unread` | `ordinal` (optional; empty is the whole thread) | `MarkUnread` |
| `GET /api/v1/drafts/{thread_id}` | none: the thread page fills its composer from the draft | | `Draft` |
| `PUT /api/v1/drafts/{thread_id}` | `POST /t/{id}/draft`, the composer's "Save draft" button (`formaction` on the reply form) | `source`, `reply_to` (optional) | `SaveDraft` |
| `DELETE /api/v1/drafts/{thread_id}` | `POST /t/{id}/draft/delete` | none | `DeleteDraft` |
| `GET /api/v1/forums/{slug}/draft` | none: the new-thread page fills its form from the draft | | `ForumDraft` |
| `PUT /api/v1/forums/{slug}/draft` | `POST /f/{slug}/draft`, the new-thread form's "Save draft" button (`formaction` on the form) | `title`, `source` | `SaveForumDraft` |
| `DELETE /api/v1/forums/{slug}/draft` | `POST /f/{slug}/draft/delete` | none | `DeleteForumDraft` |
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

The renderer (`internal/render/image.go`, renderer version 5) applies these
rules to a markdown image, in this order:

- **An attachment stays an image.** An attachment is a destination of exactly
  `/blobs/<64 lowercase hex>`, the URL `GET /blobs/{hash}` serves, and it
  renders as `<img>`, inside a link or not. Anything that only resembles one,
  such as an uppercase hash, a suffix, a query or `https://host/blobs/<hash>`,
  is not an attachment. Deciding by "has no scheme" lost, because
  `//host/x.png` and `/\host/x.png` have no scheme and a browser loads both
  from another host.
- **A source no link could carry becomes its alt text.** When the sanitizer
  would drop the URL from a link (`data:`, `javascript:`, and every scheme
  but http, https and mailto), the image is its alt text as plain text, and
  nothing when there is none. The renderer asks the sanitizer's policy rather
  than keeping its own list of schemes, so it never writes a link the
  sanitizer strips.
- **An image inside a link becomes text inside that link:** its alt text, or
  its URL when there is none. HTML cannot nest links. Writing the image's
  link after the outer one lost, because it puts a second link where the
  author wrote one.
- **Any other image becomes a link** to its URL, `<a href="URL">`, with the
  alt text as the link text, or the URL when the alt text is empty or only
  whitespace. The markdown title becomes the link's `title`.

The sanitizer is the second layer: an `img` keeps `src` only when it matches
the attachment pattern, so an image that reaches the policy by another path
loses its source and loads nothing.

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
| Upload (`POST /api/v1/blobs`) | 10 MB for the file, and 64 KiB more for the multipart framing around it | yes, `limits.attachment_mb` (`KETJU_LIMITS_ATTACHMENT_MB`) |

Both are refused with a `payload-too-large` problem rather than a truncated
read. A body whose `Content-Length` is over the limit is refused before a byte
is read; a chunked body, or one whose `Content-Length` lies, is caught by
`MaxBytesReader` and the resulting error maps to the same code.

The upload's file part is held to exactly `limits.attachment_mb` as it is
read, and the body to that plus 64 KiB, so a file of the full size fits with
its part headers and a larger one does not (TKT-01M29CXK0K).

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

Upload MIME sniffing against an allowlist is in "Attachments" below (TKT-01M29CXK0K).

### Attachments

**Decided by the run (2026-10-01, pending maintainer review)**
(TKT-01M29CXK0K, TKT-01M29CYS9W). These settle what the design leaves open
about uploads and serving:
- **Upload.** `POST /api/v1/blobs?thread={id}` or `?forum={slug}` takes one
  multipart part named `file`. The query names where the file is going,
  because `upload` is granted per forum (04 "permissions"). It needs a
  member session and `upload` in that forum, and is refused as a post in
  that forum is: `not-found` if the forum is hidden, `unauthenticated` for
  an anonymous viewer, `forbidden` without `upload`. An archived forum
  denies `upload` to all but admins. No migration grants `upload` to the
  members group. An admin grants it, as for any other permission. A grant
  in a migration would change what members of an existing install may do
  without anyone deciding it.
- **Type.** The type is sniffed from the first 512 bytes with
  `http.DetectContentType`, never taken from the part's `Content-Type` or
  the file's name. The allowlist is `image/png`, `image/jpeg`, `image/gif`,
  `application/pdf` and `text/plain`. An image must also pass the standard
  library's `DecodeConfig` for its type. Anything else is
  `validation-failed` with `{field: "file", code: "type_not_allowed"}`.
  Bytes identical to a quarantined blob are `{field: "file", code:
  "blocked"}`. SVG and HTML lost, because both run script on this origin.
  WebP lost, because the standard library can neither check nor thumbnail
  it. Archives lost, because nothing here needs them yet.
- **Answer.** 201 `dto.Attachment` `{hash, size, mime, original_name, width,
  height}` for new bytes and known bytes alike, so an upload does not reveal
  whether someone else sent the same file. `original_name` is the name the
  viewer first uploaded these bytes under, which is the name this upload
  sent unless they uploaded the same bytes before. `width` and `height` are
  null for anything that is not an image.
- **Attach.** `POST /threads/{id}/posts` and `POST /forums/{slug}/threads`
  take `attachments`, at most 10 distinct hashes. Each must be a blob the
  poster uploaded and that is not quarantined, and the poster needs
  `upload` in the forum. An edit takes none, since `Edit` has no
  attachments field (03).
- **Serving.** `GET /blobs/{hash}` and `GET /blobs/{hash}/thumb` serve only
  a viewer who may read a post naming the hash in its `attachments`. "May
  read" uses the thread page's rules for that post's body, so a removed post
  counts only for those its tombstone opens to (a moderator, and the author
  of a post a moderator removed), and a redacted post counts for nobody.
  Every other request gets `not-found`, including the uploader's own
  request for a blob no post names yet, and a request for a hash with no
  blob. A `/blobs/<hash>` URL written into a post's source is not a
  reference. Responses carry the stored type, `Cache-Control: private,
  no-cache`, `Vary: Cookie`, the hash as `ETag`,
  `Content-Security-Policy: default-src 'none'; sandbox`,
  `Cross-Origin-Resource-Policy: same-origin`, and `Content-Disposition:
  inline` for images and `attachment` for everything else. The browser
  keeps the bytes but asks again on every use, and the server checks access
  before it answers 304, so a reader who loses access, or signs out, stops
  seeing the file. This replaces docs/03's "immutable cache headers", which
  predates the docs/14 rule. A revalidation costs a request, not a
  transfer.
- **Names.** A post's attachment shows the name its author first uploaded
  the bytes under (the `blob_uploads` table, 04). A row is written once and
  never changed, so uploading the same bytes again under another name
  renames nothing already posted, and a second uploader of the same bytes
  never learns the first one's file name.

Alternatives: letting an uploader fetch a blob before a post names it lost,
because docs/14 says a blob is served only to a reader of a post that
references it, and widening that is the maintainer's call (docs/14 class 1).
The composer shows its own uploads by name and size instead. Counting a
`/blobs/` URL in a post's source as a reference lost, because then anyone
who learned a hash could publish the bytes by writing the URL into a public
post. Attaching any blob the poster may read lost to the same reasoning: a
re-upload is the honest way to re-share. A public `Cache-Control` lost,
because the answer depends on who asks. `immutable` with a year's
`max-age` lost (Terva review 1696), because a browser would show the file
for a year after its reader lost access to the post. Taking the name from
each upload lost, because a later upload of the same bytes would rename
the attachment on posts already written.
