# 02 — Architecture

## Shape of the deployment

```
┌─────────────────────────────── neoforum (one static binary) ───────────────────────────────┐
│                                                                                            │
│  net/http server ──► router ──► middleware (session, rate limit, CSRF, logging)            │
│        │                                                                                   │
│        ├── /api/*         JSON API handlers (the only business-logic entry point)          │
│        ├── /auth/*        OIDC start/callback, logout                                      │
│        ├── /blobs/*       attachment serving                                               │
│        ├── /events        SSE live updates                                                 │
│        ├── /assets/*      embedded Preact build (embed.FS)                                 │
│        └── /*             SPA shell (later: server-rendered HTML for non-JS clients)       │
│                                                                                            │
│  service layer ───────────────────────────────────────────────────────────────────────┐    │
│   threads · posts · forums · users · identity · applications · moderation · search    │    │
│  ─────────────────────────────────────────────────────────────────────────────────────┘    │
│        │                          │                          │                             │
│  thread store (03)          embedded DB (04)          renderer (goldmark + sanitizer)      │
│  logs + indexes             users, perms, projections  markdown → HTML, cached             │
│        │                                                                                   │
│  event bus (in-process) ──► projection writer, search indexer, notifier, SSE fan-out       │
└────────────────────────────────────────────────────────────────────────────────────────────┘
                     data/ directory (threads/, blobs/, db/, config.toml)
```

A reverse proxy (Caddy, nginx, Traefik) in front for TLS is the expected
deployment; the binary can also terminate TLS itself with a configured
certificate or ACME (later phase) for the "just run it" case.

## Go module layout

```
cmd/neoforum/            main: subcommands serve, fsck, reindex, dump, admin, config
internal/
  config/                TOML loading, validation, secrets-from-file
  store/threadlog/       log + index format, thread actor, open-thread cache (03)
  store/db/              embedded DB access, migrations, queries
  store/blob/            content-addressed attachments
  domain/                pure types: Thread, Post, User, Forum, Permission …
  service/               use-cases; the only package that touches both stores
    threads/ posts/ forums/ users/ identity/ applications/ moderation/ search/ notify/
  render/                markdown pipeline, sanitizer, mention/quote extensions
  auth/oidc/             relying-party flows, provider registry
  auth/session/          cookie + server-side sessions
  events/                in-process bus, subscribers, projection catch-up
  web/api/               HTTP handlers for /api, request/response DTOs
  web/sse/               live update endpoint
  web/spa/               embed.FS for the built client, index.html templating
  web/html/              (later) server-rendered pages sharing DTOs with /api
web/client/              Preact + Vite project; build output copied into web/spa
docs/                    these documents
```

Dependency direction: `web → service → store/domain`. Handlers never touch
stores directly; the future non-JS handlers reuse `service` and the same
DTOs as `/api`, which is what makes the second client cheap.

## Request flows

### Read a thread page
1. Session middleware resolves the viewer (or anonymous).
2. Handler calls `threads.View(viewer, threadID, lens, cursor)`.
3. Service checks `permissions` (cached per session for 60 s), opens the
   thread via the thread store, applies the lens to the in-memory index,
   fetches/renders the selected posts through the post cache, attaches viewer
   data (`read_positions`, own-post markers) and returns a DTO.
4. Handler sets `ETag` from `(thread_id, log_length, renderer_version)` and
   `Vary: Cookie`; honors `If-None-Match`.

### Create a post
1. Rate limit and permission check; thread not locked; slow mode.
2. Renderer validates the markdown (also extracts mentions, quotes, reply_to).
3. Thread actor appends the record and fsyncs; returns ordinal and new
   log length.
4. Event bus publishes; projection writer updates `threads`, `post_meta`,
   `mentions`; search indexer indexes; notifier creates `notifications`
   rows; SSE fan-out sends `{thread_id, ordinal, log_length}` to subscribed
   clients, who then fetch the new post(s) via the normal read path (so SSE
   payloads carry no content and no per-viewer logic).
5. Response returns the rendered post so the client can insert it immediately.

### Live updates
`GET /events?threads=…&forums=…` is a long-lived SSE stream. Messages are
tiny invalidation hints keyed on log length; the client reconciles by
requesting `?after_log_length=X`. This keeps SSE stateless with respect to
permissions: the follow-up fetch is where authorization happens.

## Configuration

`config.toml` (bootstrap, providers, paths, listen address, fsync policy,
cache sizes) plus the `settings` table for things admins change at runtime.
Environment variable overrides `NEOFORUM_*` for container deployments.
`neoforum config check` validates and prints the effective config with
secrets redacted.

## Observability

- `slog` structured logging, JSON in production, text in dev.
- `/metrics` (Prometheus text format, behind an admin token or bind
  address): request latency, cache hit ratios, thread store fsync latency,
  projection lag (log length − projected length, summed).
- `/healthz` checks the DB opens and the data directory is writable.

## Background work

All in-process goroutines, no external queue:

| Job | Trigger | Notes |
|-----|---------|-------|
| Projection writer | event bus | ordered per thread, idempotent |
| Search indexer | event bus | batched |
| Notification generator | event bus | |
| Session sweeper | hourly | delete expired |
| Blob GC | daily | refcount 0 and older than 7 days |
| Index flush | on idle / shutdown | write index headers |
| Discovery refresh | on JWKS verify failure / 24 h | per provider |

Graceful shutdown: stop accepting, drain in-flight requests (10 s), flush
thread actors (write index headers), close DB.

## Testing strategy (for the implementation phase)

- Thread log: property tests (random record sequences → rebuild index →
  equals incremental index), crash tests (truncate log at every byte offset
  of the tail frame → recovery yields a valid prefix), fuzz the frame parser.
- Service layer: table-driven tests against a temp data dir and an in-memory
  DB; the OIDC provider is a tiny in-process fake issuer.
- API: golden JSON tests per endpoint; these goldens double as the contract
  for the Preact client and the future HTML client.
- Client: component tests for the composer and the lenses; Playwright smoke
  test against a dev server with the fake issuer, run at both a 390 px
  phone viewport and a 1440 px desktop viewport so neither layout
  regresses silently.
