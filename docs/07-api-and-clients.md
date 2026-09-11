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
  session cookie. Personal API tokens (later) are a separate `Authorization:
  Bearer` path for scripts and are never issued to browser code.

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
| `GET  /threads/{id}?page=&lens=&after_log_length=` | thread header + page of rendered posts + lens meta + viewer read position |
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

### Moderation and admin (`/api/v1/admin/*`, role-gated)

Queue (flags + applications), application approve/deny, thread
lock/pin/move/rename/tag/archive, split/merge, user suspend/anonymize,
forum CRUD and ordering, group and permission editing, form versions,
settings, audit log query, `reindex` and `fsck` triggers with progress via
SSE.

### Thread response shape (abridged)

```json
{
  "thread": {"id": "h6ute4a1000001", "slug": "why-we-left-slack", "title": "…",
             "forum": {"slug": "general", "name": "General"},
             "post_count": 212, "log_length": 483920, "locked": false, "pinned": null,
             "tags": ["meta"], "created_at": 1757500000000},
  "page": {"number": 3, "size": 25, "total_pages": 9},
  "lens": {"kind": "author", "value": "drew", "shown": 7, "hidden": 205},
  "viewer": {"last_read_ordinal": 60, "watch": "watch", "can_reply": true, "can_moderate": false},
  "posts": [
    {"ordinal": 61, "revision": 2, "author": {"handle": "…", "display_name": "…", "avatar": "…", "role": "member"},
     "created_at": 1757500100000, "edited_at": 1757500200000,
     "reply_to": 57, "reply_to_suppressed": false, "reply_count": 3,
     "html": "<p>…</p>", "attachments": [], "deleted": false, "moved_to": null}
  ]
}
```

`ETag: "h6ute4a1000001-483920-r3"`; `Vary: Cookie`. The viewer block is the
only per-user part, and it is cheap; everything above it comes from the
response cache keyed on `(thread, page, lens, log_length, renderer_version,
viewer_class)` where `viewer_class ∈ {anon, member, moderator}` (moderators
see tombstone contents).

## Preact client

- **Stack:** Preact 10.x, `@preact/signals`, `preact-iso` router, Vite 8,
  TypeScript. No component library; a small hand-written CSS system with
  custom properties and light/dark themes. Target bundle: < 60 KB gzipped
  for the thread view.
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

CSP with nonces (no inline scripts, `img-src 'self' data:`, no third-party
origins), `Referrer-Policy: strict-origin-when-cross-origin`,
`X-Content-Type-Options: nosniff`, `frame-ancestors 'none'`. Request body
limits (64 KB for posts, configurable attachment cap default 10 MB), upload
MIME sniffing with an allowlist, rate limits per session and per IP on
writes.
