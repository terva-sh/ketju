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
/unread  /new  /participated  /watching  /search?q=…
/u/{handle}  /u/{handle}/posts
/settings  /settings/sessions  /settings/identities
/apply  /apply/status
/admin  /admin/queue  /admin/applications  /admin/forums  /admin/users  /admin/forms  /admin/settings
/auth/{provider}/start  /auth/{provider}/callback  /auth/logout
```

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
| `GET  /me` | viewer: user, role, unread counts, pending notifications count, settings |
| `GET  /forums` | tree of forums with per-forum unread counts |
| `GET  /forums/{slug}/threads?cursor=&sort=` | thread list rows |
| `POST /forums/{slug}/threads` | create thread `{title, source, tags}` → thread + first post |
| `GET  /threads/{id}?page=&ordinal=&lens=&after_log_length=` | thread header + page of rendered posts + lens meta + viewer read position |
| `POST /threads/{id}/posts` | `{source, reply_to?, attachments?}` → rendered post, ordinal, log_length |
| `GET  /threads/{id}/posts/{ordinal}` | single post (popover, permalink) |
| `GET  /threads/{id}/posts/{ordinal}/replies` | direct replies (inline expander) |
| `GET  /threads/{id}/posts/{ordinal}/history` | revisions with diffs |
| `PATCH /threads/{id}/posts/{ordinal}` | edit `{source, reason?}` |
| `DELETE /threads/{id}/posts/{ordinal}` | soft delete; `POST …/restore` to undo |
| `PUT  /threads/{id}/read` | `{ordinal}` advance read position; `DELETE` marks unread |
| `POST /threads/{id}/watch` `{level}` | watch/mute |
| `POST /threads/{id}/flags` | `{ordinal, reason, note}` |
| `POST /render` | `{source}` → `{html, mentions, reply_to, quotes}` (composer preview) |
| `GET  /drafts/{thread_id}` `PUT` `DELETE` | composer drafts |
| `GET  /unread` `/new` `/participated` `/watching` | cross-forum thread lists |
| `GET  /search?q=&forum=&author=&after=&before=` | FTS5 results with snippets |
| `GET  /notifications` `POST /notifications/read` | |
| `POST /blobs` (multipart) `GET /blobs/{hash}` | attachments; GET is outside `/api` at `/blobs/…` with immutable caching |
| `GET  /users/{handle}` `/users/{handle}/posts` | profiles |
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
lock/pin/move/rename/tag/archive, split/merge, user suspend/anonymize,
forum CRUD and ordering, group and permission editing, form versions,
settings, audit log query, `reindex` and `fsck` triggers with progress via
SSE.

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
  for the thread view. Hand-written CSS is decided over a utility framework
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
