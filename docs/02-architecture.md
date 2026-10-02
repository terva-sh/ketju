# 02 — Architecture

## Shape of the deployment

```
┌────────────────────────────────── ketju (one static binary) ───────────────────────────────┐
│                                                                                            │
│  net/http server ──► router ──► middleware (log, rate limit, session, pre-user gate, CSRF) │
│        │                                                                                   │
│        ├── /api/*         JSON API handlers (the only business-logic entry point)          │
│        ├── /auth/*        OIDC start/callback, logout                                      │
│        ├── /blobs/*       attachment serving                                               │
│        ├── /events        SSE live updates                                                 │
│        ├── /assets/*      embedded Preact build (embed.FS)                                 │
│        ├── /manifest.webmanifest  web app manifest, named by site_name (06)                │
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

Three operator commitments are decided (10 Q22–Q24, TKT-01M29BW9PB, 2026-09-24):

- **TLS.** v1 assumes the reverse proxy. "Just run it" may mean Caddy in
  front, because Caddy is one line of configuration and ACME inside the
  binary is a phase of work that buys a friend group nothing on day one.
- **One process per data directory.** `serve` takes a lock file in `data/`
  and a second `serve` against the same directory refuses to start. The
  thread actors own their logs in memory and the embedded database has one
  writer, so a second process would not scale anything; it would corrupt
  it. Horizontal scaling is out of scope for the communities ketju is for.
- **Backups.** `ketju backup` ships in Phase 4: it pauses the thread
  actors, checkpoints the WAL, and snapshots consistently. Until then the
  procedure is written down rather than built: stop the server, or copy
  `data/threads/` and `data/blobs/` with `rsync` and take the database with
  `VACUUM INTO`.

**Decided by the run (2026-10-02, pending maintainer review)**
(TKT-01M29CXK35):
- **How `ketju backup` reaches a running server.** It connects to a Unix
  socket, `data/ketju.sock`. `serve` creates the socket under the lock with
  mode 0600 and answers only a peer whose uid is its own. The socket takes
  one request: take a snapshot. To take it, `serve`:
  1. holds new appends at `posts.Writer`;
  2. catches the projection up;
  3. runs a passive WAL checkpoint;
  4. runs `VACUUM INTO` a staging file inside the data directory;
  5. lets appends go.

  The pause covers only those steps, is capped, and handles one snapshot at
  a time. The command then copies each log up to the length the snapshot
  projected it to, so the logs and the database in the backup agree exactly.
  With no `serve` running, the command takes the lock itself and needs no
  pause. Alternatives:
  - An admin API route lost. Anyone with an admin session could then make
    the server write to its own disk, and docs/05 gives no rule for that.
  - A signal lost. It carries no reply, and a stale pid in `ketju.lock`
    would send it to some other process.
  - An offline-only command lost. 10 Q23 decided that the command pauses
    the actors of a running server.
  - Skipping the pause lost, because the backup's logs and database would
    then disagree. Logs copied whole run ahead of the projection, which
    `ketju fsck` reports as drift. Logs cut to the projection can lack
    posts that the database's flags and read positions name.

  The socket gives its caller nothing that the data directory's files do
  not already give.
- **What a backup holds.** `db/ketju.db`, `threads/` without the `.idx`
  files, and `blobs/` without thumbnails or `blobs/tmp/`. Each index is
  rebuilt from its log when its thread is first opened, and each thumbnail
  is made again when it is next requested (03). `config.toml` and
  `secrets/` are left out. The operator keeps their own copy of the config,
  and a lost cookie key ends only the sign-ins in progress. Alternatives:
  - Copying the indexes lost. An index copied mid-write could be trusted
    over its log, which is the failure TKT-01M3VHSYFR closed.
  - Copying `secrets/` lost. It would put a key into every backup's
    storage, and the backup gains nothing from it.

**Decided by the run (2026-10-02, pending maintainer review)**
(TKT-01M29CXK3S): **release binaries are built for `linux/amd64` and
`linux/arm64`**, with `CGO_ENABLED=0` and `-trimpath`. Those two cover a VPS
and a 64-bit home server such as a Raspberry Pi 4 or 5. Alternatives:
- macOS and FreeBSD builds lost. No CI runner starts either one, and a
  release should not offer a binary that nobody has run.
- Windows lost. `serve` refuses to start there, because the data-directory
  lock exists only on unix (`internal/lock`).
- 32-bit ARM lost for v1.0. No runner tests a 32-bit build, and
  TKT-01M3VVZE4G is fixing ordinal bounds on 32-bit builds now.

## Go module layout

```
cmd/ketju/               main: subcommands serve, fsck, reindex, dump, admin, config
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
1. Session middleware resolves the viewer (or anonymous). The pre-user gate
   after it refuses an approval-mode applicant everything outside the
   application flow (05 "mode = approval"), so an applicant never reaches a
   thread handler or the response cache.
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

**Decided by the run (2026-10-01, maintainer approved):** the stream
checks access as the thread page does, because docs/14 requires that a live
event reach a viewer only if that viewer may read the thread when the event
is sent. The rules (TKT-01M29CRJ3X):
- **Payload.** An event is `{thread_id, log_length}` and nothing else, the
  shape docs/06 and the ticket name. The `ordinal` in step 4 above is dropped,
  because the thread page already shows the log length to every reader and
  shows nothing else of a change. A hint about a removed, hidden or redacted
  post therefore tells a reader no more than their next page fetch would.
- **Access.** A subscription is admitted only if the thread page would admit
  the viewer to every thread named, answering 404, 401 or 403 as the page
  does. A forum is admitted on `read`. Before each write, the stream resolves
  its session again through `session.Store` (or its agent credential through
  `agentcred.Store`) and checks thread access again. The same check runs at
  each 25-second heartbeat. If the session has ended, or the viewer has lost
  access to any thread named, the server closes the stream. The browser's
  reconnect is then judged from scratch.
- **Limits.** Three `config.toml` keys, each answering `rate-limited` with
  `Retry-After` when full: `limits.sse_connections` (all streams, default
  1000), `limits.sse_per_user` (one signed-in user's streams, default 8), and
  `limits.sse_anonymous` (all anonymous streams together, default 100).
  Anonymous streams share one pool rather than a per-address cap, because
  behind a reverse proxy every client has the proxy's address (`web.ClientIP`
  reads `RemoteAddr`).
- **Resuming.** A stream opens with an `event: ready`, then sends the current
  `{thread_id, log_length}` of each thread named. A client that missed events
  while disconnected therefore catches up by comparing log lengths. A forum
  has no such snapshot, so a client of `forums=` refetches its listing on
  every `ready`. When the hub loses events (a dropped bus batch, or a full
  stream queue), it closes every stream that names a forum, and resends the
  snapshot on streams that name only threads. The reconnect's `ready` is
  the forum client's signal to refetch (Terva review 1708).
- **The projection goes first.** The hub handles an event only once the
  thread's projection row has reached the event's log length. Both the
  forum it routes by and the access it checks are read from that row. The
  projection writer is another subscriber on the same bus, so without the
  wait a move could be routed to the forum the thread left, and the move's
  own hint could reach a reader who has just lost access. If the row has
  not caught up within 5 s, the hub closes every stream that names the
  thread or a forum, and each reconnect is judged afresh (Terva review
  1712).

Alternatives: keeping the fan-out free of permission checks lost, because
docs/14 makes any widening a security finding, and a thread moved to a forum
the reader cannot see would keep reporting activity to them. Filtering per
post lost, because the page shows every reader of a thread its log length
anyway. Holding streams open across a session's end with a final `event:
end` lost, because the client would have to handle a second refusal path
beside the HTTP one. A per-address anonymous cap lost to the proxy problem
above.

The implementation is `internal/web/sse`: the hub there is the bus
subscriber named `sse`, and the handler serves `/events`. docs/07 has the
wire format. `ketju serve` closes the hub as shutdown begins, because a
stream never finishes by itself and the drain waits for every handler.

## Configuration

`config.toml` (bootstrap, providers, paths, listen address, fsync policy,
cache sizes) plus the `settings` table for things admins change at runtime.
The split is the rule: **config.toml holds what must be readable before the
database exists; everything an admin changes at runtime lives in
`settings`.** A key written into the wrong half is refused at startup with
the other half named, rather than quietly accepted. Before the admin screen
exists, an operator changes a setting on a stopped instance with
`ketju admin setting set KEY VALUE`, and reads them with `get KEY` and
`list` (05 "Provider configuration").

Environment variable overrides `KETJU_*` for container deployments, listed
explicitly rather than derived from field names, so that renaming a Go
field cannot silently rename a documented variable. One further rule
applies to every one of them: `KETJU_X_FILE` supplies the value of
`KETJU_X` from a file, which is how a secret reaches a container without
being inherited by every child process and readable from `/proc`. Setting
both forms is an error rather than a precedence rule.

An absent key takes a documented default; an invalid key is an error naming
the key, the value and the reason; an invalid value never becomes a zero
value. Every problem in the file is reported in one pass.

`ketju config check` validates and prints the effective config with
secrets redacted. It loads the file exactly as `serve` does, from
`--config`, else `$KETJU_CONFIG`, else `./config.toml`, and prints every
key with its value and the layer it came from: `file`, `env`, `env-file`,
`secret-file` (a provider's `client_secret_file`) or `default`. A secret is
redacted by its type rather than by its key's name, so a secret added under
a name nobody expected is redacted as well. The check also lists the
`KETJU_*` variables the build reads and the runtime settings, which it
names but never reads from the file. It exits 0 when the config is valid,
1 when it has problems, each named by its key, 2 for usage, and 4 when the
file is missing or unreadable, as the other commands do.

A problem stops `serve` and fails the check. A warning does neither: `serve`
logs it at startup and the check prints it. A configuration is a warning
rather than an error when it is legal, has a use, and costs something the
operator should choose knowingly:

- `storage.fsync = "never"` loses acknowledged posts on a crash, and is
  right for an import or a test run;
- `insecure_skip_issuer_check` exists for one provider family (09) and is a
  hole anywhere else;
- a `client_secret_file` that is group or world writable, because
  bind-mounted and projected secrets land with modes an application does not
  control.

Everything the loader refuses is an error, including a runtime setting
written into `config.toml`, and a provider's `group_map` onto `everyone` or
`members`, whose membership is implicit and never stored. A `group_map`
target that names no group is neither: only the database can tell, so
`serve` logs it at startup and at each sign-in that meets it (05 "Roles and
groups").

## Observability

- `slog` structured logging. `log.format` is `json`, `text`, or `auto`
  (the default), where `auto` writes text to a terminal and JSON to anything
  else — a pipe, a file, a collector. `log.level` is `debug`, `info`, `warn` or
  `error`. Both take `KETJU_LOG_FORMAT` / `KETJU_LOG_LEVEL`.
- **No post source, no session id, no token and no secret ever reaches a log
  line.** Logs leave the machine, and a body that was soft-deleted in the
  thread log is not deleted if a copy is sitting in an aggregator. Three
  mechanisms hold that up: a redacting `slog.Handler` that drops values by
  attribute name, including inside groups and inside `With`; types that carry
  secrets implementing `slog.LogValuer` (the one that survives an innocuous
  key, and what to reach for when adding such a type); and request logging that
  records `r.URL.Path` and never the query string, because the OIDC callback
  arrives as `?code=…&state=…`.
- Request log line: method, path, status, bytes, duration, and the viewer id
  when the request is authenticated. 5xx logs at error, 4xx at warn, so a wall
  of 404s does not train anyone to ignore the error stream.
- `/metrics` (Prometheus text format): request latency, cache hit ratios,
  thread store fsync latency, projection lag (log length − projected length,
  summed). **Decided by the maintainer (2026-10-02, TKT-01M29CEE7S):** it is
  guarded by a separate bind address. A `metrics_listen` key in
  `config.toml` (for example `127.0.0.1:9090`) starts a second listener that
  serves `/metrics` and nothing else, unauthenticated. An empty key, the
  default, turns metrics off. The main listener never serves `/metrics`.
  Alternatives: a bearer token read from a file lost because it adds a new
  kind of credential; supporting both lost for the same reason; an admin
  account's credential lost because the scraper would then hold full admin
  power. As built (TKT-01M29CEE7S): `KETJU_METRICS_LISTEN` overrides the key;
  a `metrics_listen` equal to `listen` is refused at startup; the main
  listener answers `/metrics` with a 404 problem, never the shell; the
  metrics listener answers 404 to every other path and 405 to a method other
  than GET or HEAD, with `Cache-Control: no-store`. It binds before the main
  listener, so an address in use stops the start, and it stops first at
  shutdown, before any store a scrape reads. If it fails while serving, the
  failure is logged and the forum goes on serving. The metrics are collected
  whether or not the key is set, at the cost of an atomic add per request and
  per fsync. "The metrics" below lists them.
- `/healthz` checks the DB opens and the data directory is writable.
  (TKT-01M29CCBZ8.) It is unauthenticated, answers GET and HEAD only, and
  says only this:
  `{"status":"ok"|"degraded"|"unavailable","checks":{"database":"ok"|"failing","data_dir":"ok"|"failing","threads":"ok"|"quarantined"}}`,
  with `Cache-Control: no-store`. No version, path, error text, count, thread
  id or configuration value is in a response; the detail goes to the log,
  once per transition. `unavailable` is 503 and means the database or the
  data directory check failed. `degraded` is 200 and means threads are
  quarantined (03): a 503 would have a supervisor or a load balancer take the
  whole forum down over one bad file, which quarantine exists to prevent, and
  a restart cannot repair it. Projection lag stays on `/metrics`.
  Each request costs one statement on the read pool (it reads `sqlite_schema`,
  since a bare `SELECT 1` passes for a file that can no longer be read) and no
  disk I/O. The data-directory probe creates, writes, fsyncs and removes
  `data/.healthz-probe-<random>` in the background every 10 s; a result older
  than two intervals counts as failing, so a probe hung on a stuck disk does
  not read as healthy. Every probe, the first included, runs in the
  background. Startup waits at most 5 s for the first probe's result, and
  shutdown waits at most 5 s for a probe in flight. A stuck fsync cannot be
  interrupted, and the lock must still be released. The random suffix gives each process its own file, so a probe
  left running past that wait cannot touch the next owner's, and `serve`
  removes leftover probe files under the lock before it starts. The route skips rate limiting, session resolution and CSRF, so
  a poll carrying a session cookie still costs one statement.

### The metrics

`internal/metrics/collect` registers these, and
`TestMetricNamesFollowConventions` fails on a name that is not in this table
or breaks the rules: `ketju_` and lowercase, `_total` on a counter and only
on a counter, and the unit as a suffix (TKT-01M29CEE7S). A hit ratio is
exported as hit and miss counters, because a ratio cannot be summed or
rated over a window; compute it in the query.

| Metric | Type | Unit | Labels | Meaning |
|--------|------|------|--------|---------|
| `ketju_http_request_duration_seconds` | histogram | seconds | `route`, `method`, `code` | Time to answer a request. Live-update streams are not included. |
| `ketju_threadlog_fsync_duration_seconds` | histogram | seconds | `file` | Time an fsync of a thread's `log` or `index` took. |
| `ketju_projection_lag_bytes` | gauge | bytes | — | Log bytes the projection has not applied, summed over threads. |
| `ketju_threads_quarantined` | gauge | — | — | Threads whose log is shorter than their projection. |
| `ketju_cache_hits_total` | counter | — | `cache` | Lookups the `response` or `render` cache answered. |
| `ketju_cache_misses_total` | counter | — | `cache` | Lookups it could not answer. A stale response entry is a miss. |
| `ketju_cache_evictions_total` | counter | — | `cache` | Entries dropped to stay within the bound. |
| `ketju_cache_stale_total` | counter | — | — | Response cache entries found with moved render dependencies. |
| `ketju_thread_cache_opens_total` | counter | — | — | Threads opened from disk. |
| `ketju_thread_cache_evictions_total` | counter | — | — | Threads closed to stay within `storage.open_threads`. |
| `ketju_thread_cache_open_threads` | gauge | — | — | Threads open now. |
| `ketju_events_queued` | gauge | — | `subscriber` | Events waiting in a bus subscriber's queue. |
| `ketju_events_dropped_total` | counter | — | `subscriber` | Events a full queue dropped; catch-up recovers them. |
| `ketju_sse_streams` | gauge | — | `kind` | Live-update streams open now, `member` or `anonymous`. |
| `ketju_sse_refused_total` | counter | — | — | Streams a connection limit refused. |
| `ketju_sse_overflowed_total` | counter | — | — | Streams closed because their queue filled. |
| `ketju_sse_lagged_total` | counter | — | — | Times the projection missed a live event's wait. |
| `ketju_goroutines` | gauge | — | — | Goroutines running now. |
| `ketju_heap_bytes` | gauge | bytes | — | Heap held by live and unswept objects. |

The labels take values from fixed sets, so no client can grow the series
and no label names content:
- `route` is the pattern the router matched, such as
  `/api/v1/threads/{id}`, never the path. A request answered before any
  router matched it, such as a rate-limit refusal, is `unmatched`. A path no
  specific route matches carries its catch-all's pattern, `/` or `/api/v1/`.
- `method` is `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE` or `OPTIONS`,
  and any other method is `other` (Terva review 1943).
- `code` is the status class, `1xx` to `5xx`.
- `subscriber` is a bus subscriber's name: `projection`, `sse`, `search` or
  `notifications`.

No label carries a thread id, a handle, a forum slug, a request path or a
query, which is the rule this section holds log lines to. Per-thread and
per-forum metrics are left out on purpose, because a label of either would
name content.

A scrape takes no lock a request takes. The cache, thread cache, hub and
bus counters are atomics. Projection lag reads every projected length in
one statement on the read pool and every log's size from the `threads/`
directory walk, and a result is reused for 10 s, so a scraper polling fast
cannot keep the walk running. Under `storage.fsync = "never"` there is no
fsync to time, and the fsync histogram stays empty.

## Background work

All in-process goroutines, no external queue:

| Job | Trigger | Notes |
|-----|---------|-------|
| Projection writer | event bus | ordered per thread, idempotent |
| Search indexer | event bus | batched |
| Notification generator | event bus | per-thread progress in `notification_progress`; projects a thread before reading it; catch-up at startup and after a drop, running the projection's sweep first, then every log against its progress (TKT-01M29CRJ2T) |
| Session sweeper | hourly | delete expired |
| Blob GC | daily | refcount 0 for 7 days and named in no thread log (03 "Blob garbage collection") |
| Suspension expiry | every minute | lift suspensions whose end has passed; their hidden posts stay hidden (08 "Suspension") |
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
