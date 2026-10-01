---
schema: 3
id: TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
title: Build the SSE endpoint and fan-out
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:realtime
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:live-updates
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T08:36:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
extensions: {}
---

## Description

`GET /events?threads=&forums=` as a long-lived stream carrying `{thread_id, log_length}` invalidation hints and nothing else.

Payloads carry no content and no per-viewer logic, which is what keeps SSE stateless with respect to permissions: the client's follow-up fetch is where authorization happens. A subscriber to a thread they cannot read learns only that it changed, and that is acceptable where a forum listing already leaks as much.

The connection count is the operational risk. Every open tab is a held connection, and the limits need to be explicit rather than discovered when a community grows.

## Acceptance criteria

- [ ] Every event's data is exactly {thread_id, log_length}, and the stream sends only ready and thread events and comments (TestEventPayloadIsThreadAndLogLength)
- [ ] A subscription is admitted only as the thread page would admit the viewer, answering 404, 401 or 403 as the page does, and a pre-user or cross-site request is refused (TestSubscribeAnswersAsThePage, TestPreUserCannotSubscribe, TestSubscribeRefusesCrossSite)
- [ ] An event about an author-deleted, moderator-hidden, hidden-pending-review or redacted post, or about a suspended author's post before and after the bulk hide, carries only the log length the viewer's next thread page serves (TestEventCarriesNoMoreThanThePage)
- [ ] A member who moderates a forum by group grant keeps receiving events through hides and restores there (TestGroupGrantModeratorKeepsStream)
- [ ] limits.sse_connections, limits.sse_per_user and limits.sse_anonymous are config keys with env overrides and docs/07 rows, and a stream over any of them is refused rate-limited with Retry-After (TestStreamLimits, TestSSELimitDefaultsAndEnv)
- [ ] Publishing never blocks on a slow stream; a stream whose queue fills is closed (TestSlowClientIsClosedAndPublishNeverBlocks)
- [ ] A hint reaches a client through httputil.ReverseProxy with default settings within one second of the append (TestHintsFlushThroughReverseProxy)
- [ ] Serve's shutdown ends every open stream inside the drain and returns without cutting requests (TestServeShutdownClosesEventStreams)
- [ ] A stream ends without writing the next hint when its session is revoked or expires, its user is suspended or deactivated, its agent credential is revoked, read is revoked, or the thread moves to a forum the viewer cannot read; an idle stream ends at the next heartbeat (the seven TestStreamCloses* tests and TestAgentStreamClosesWhenCredentialRevoked)
- [ ] A stream opens with ready and the current log length of every thread named; a dropped bus batch resends that snapshot on thread-only streams and closes every stream that names a forum, whose client refetches on the reconnect's ready (TestResumeMissesNothing, TestDroppedBatchResyncs)

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan: docs/02 "Live updates" and "Create a post", docs/03 write path step 5, docs/06 "Live updates", docs/07 endpoint table and "Request limits", docs/14, `internal/events` (`bus.go`, `events.go`), `internal/web/server.go`, `internal/web/preuser.go`, `internal/web/ratelimit.go`, `internal/auth/session`, `internal/auth/agentcred`, `internal/service/threads/access.go`, `internal/store/threadcache/cache.go`, `internal/logging` (`recorder`), `cmd/ketju/serve.go`, and the dogfood deployment handoff with its Traefik route.

### Settled in grooming

- **Route and package.** `GET /events?threads=&forums=`, outside `/api/v1` (docs/07 table). `web.Groups.Events` already exists and is mounted at `/events` in `internal/web/server.go`. It is nil today, so it answers 501. The package is `internal/web/sse`, the name docs/02's package list gives it.
- **Payload, access and limits** are the run decision in docs/02 "Live updates" (2026-10-01, pending maintainer review):
  - an event is `{thread_id, log_length}` only, and the `ordinal` docs/02 step 4 once listed is dropped;
  - a subscription is admitted as the thread page admits the viewer;
  - before every write and at every heartbeat the stream resolves its session or agent credential again, and checks thread access again; it closes when either is lost;
  - three `config.toml` keys set the limits: `limits.sse_connections` (1000), `limits.sse_per_user` (8) and `limits.sse_anonymous` (100). A full limit answers `rate-limited` with `Retry-After`;
  - a stream opens with `event: ready` and then a snapshot of each thread named.
- **Why the fan-out checks access** although docs/02 called SSE stateless: docs/14 "Invariants, added" says a live event reaches a viewer only if they may read the post when it is served. The thread page shows every reader the thread's log length, so a thread-level check is exactly "no more than the thread page". A hint about a removed, hidden or redacted post carries only the log length the reader's next page fetch shows anyway.
- **The bus** (`internal/events`) never blocks a publisher. It delivers per-thread in log order, and reports drops in `Batch.Dropped`, which the subscriber must repair from durable state. The hub is a `Subscriber` named `sse`, registered in `cmd/ketju/serve.go` beside the projection writer, before `bus.Start`. A dropped batch makes every stream resend its snapshot.
- **The middleware chain already streams.** The logging `recorder` implements `Flush` and `Unwrap`, so `http.ResponseController` flushes and sets write deadlines through it. `http.Server` in `serve.go` sets no `WriteTimeout`. GET requests are neither rate-limited nor CSRF-checked. The pre-user gate refuses `/events`, because `PreUserRoutes` does not list it, and nothing here changes that.
- **The dogfood proxy works as it stands. No deployment change is needed.** The route is `~/.config/traefik/dynamic/ketju.yml`, Traefik 3.7.13.
  - The `ketju` router has no middlewares, so nothing compresses or buffers the response.
  - Neither entry point sets `respondingTimeouts`, so the v3 defaults apply. `writeTimeout` is 0, meaning unbounded. `readTimeout` is 60 s and bounds reading the request, which for a GET ends with its headers. `idleTimeout` is 180 s and applies between requests, not during a response.
  - Traefik's proxy is Go's `httputil.ReverseProxy`, which flushes a `text/event-stream` response at once rather than on its 100 ms flush interval.
  - The 25 s heartbeat stays under every timer above.

  This is to be confirmed with `curl -N` through `https://ketju.brokkr.local.sothr.com/events?threads=…` when the endpoint is first deployed, and recorded on the exit check TKT-01M3V3CFRE (Check Phase 3's exit criterion with named end-to-end tests). A proxy change, if one were ever needed, would be outside the repository and therefore docs/14 class 3. The client's polling fallback (TKT-01M29CTYA0) keeps updates arriving through a proxy that buffers.
- **Shutdown.** `srv.Shutdown` waits for active handlers, and an SSE handler never finishes by itself. The hub is closed from `srv.RegisterOnShutdown`, which runs as `Shutdown` starts, so every stream returns inside the drain.
- **No new session issuer.** `Refresh` reads a session by the hash it already holds and creates nothing, so the chokepoint test's caller lists stay as they are (docs/13 "One session chokepoint").

### Approach

1. **Config** (`internal/config`).
   - Add `Limits.SSEConnections`, `SSEPerUser` and `SSEAnonymous` with defaults 1000, 8 and 100.
   - `validate.go` refuses a value below 1, and refuses values above a sanity ceiling (100000, 1000, 100000), in the style of `maxAttachmentMB`.
   - Add the environment overrides `KETJU_LIMITS_SSE_CONNECTIONS`, `KETJU_LIMITS_SSE_PER_USER` and `KETJU_LIMITS_SSE_ANONYMOUS` in `env.go`.
   - Add the keys to `testdata/full.toml` and to the key table in `source_test.go`.
   - Add three rows to docs/07 "Request limits".
2. **Revalidation without a new door.**
   - `session.Store.Refresh(ctx, Session) (Session, error)` runs `LookupSession` with the session's own hash and `Lookup`'s expiry rule. It does not touch the session, and it returns `ErrNoSession` when the session has gone.
   - `agentcred.Store.Refresh(ctx, Credential)` does the same with the credential's token hash through `LookupAgentCredential`.
   - The handler keeps the resolved `session.Session` or `agentcred.Credential` from the request context. It never keeps the cookie or the bearer token.
3. **Thread service hooks** (`internal/service/threads`, new file `live.go`):
   - `ReadAccess(ctx, v, id) (forumID int64, err error)` is `access` reduced to its verdict. It returns the same problem codes as the thread page.
   - `ForumAccess(ctx, v, slug) (forumID int64, err error)` runs `perm.Check` for `read`. Hidden answers `not-found`, Deny answers `unauthenticated` to an anonymous viewer and `forbidden` to a member. These are the thread list's codes.
   - `LogLength(ctx, id) (int64, error)` reads the acknowledged length from the open actor when `threadcache.Cache.Peek` finds the thread, and otherwise from `threads.log_length_projected`.
4. **The hub** (`internal/web/sse/hub.go`):
   - It keeps a registry: thread id to streams, forum id to streams, and counters for all streams, per user, and anonymous.
   - `Deliver` handles each event without blocking. It looks up the thread's streams. When any forum is subscribed, it also reads the thread's forum from its projection row, one read per event and never one per stream, and looks up that forum's streams. Each stream gets the event on its channel (capacity 64) with a non-blocking send. A full channel marks the stream overflowed and closes it, and the browser reconnects and resynchronises.
   - `Batch.Dropped > 0` sets a resync flag on every stream that names only threads, and closes every stream that names a forum. A forum has no snapshot to resend, so the reconnect's `ready` is that client's signal to refetch its listing (docs/02, after Terva review 1708). An overflowed stream is closed whatever it names.
   - `Close()` refuses new streams and closes every stream's `done` channel.
   - `Stats()` reports open, anonymous, refused and overflowed counts, for the metrics endpoint (TKT-01M29CEE7S).
5. **The handler** (`internal/web/sse/handler.go`, GET and HEAD only):
   1. Parse `threads`, at most 20 base32 ids, and `forums`, at most 10 slugs, comma-separated and deduplicated. At least one is required, and anything else is `bad-request`.
   2. Refuse `Sec-Fetch-Site: cross-site` with `forbidden`.
   3. Admit the subscription: `ReadAccess` for each thread and `ForumAccess` for each forum. The first refusal is answered as the page answers it, so a list holding one hidden thread is `not-found`.
   4. Reserve a slot, or answer `rate-limited` with `Retry-After: 30`.
   5. Write the headers: `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-store` and `X-Accel-Buffering: no`. Then write `retry: 5000`, `event: ready`, and an `event: thread` line from `LogLength` for each thread named. Flush.
   6. Loop on the request context, the hub's `done`, the stream's channel and a 25 s ticker. Before any write, refresh the session or credential and rebuild the viewer from it. For a thread event, check `ReadAccess` for that thread. At a heartbeat or a resync, check every thread and forum named. A thread named in `threads` that fails, or any lost session, closes the stream.
   7. A forum-routed hint for a thread the viewer cannot read is skipped, not closed, because the forum may still be readable.
   8. Each write sets a 10 s write deadline through `http.ResponseController`. An error ends the stream, and the slot is released on return.
   9. The wire format is `event: thread` followed by `data: {"thread_id":"<base32>","log_length":N}`, and the heartbeat is the comment `: hb`.
6. **Wiring** (`cmd/ketju/serve.go`):
   - Build the hub with the thread service, the session and agent credential stores, and the limits.
   - `bus.Subscribe(hub, 0)` beside the projection writer.
   - Set `Groups.Events` to the handler.
   - `srv.RegisterOnShutdown(hub.Close)` before `Serve`.
7. **Docs.**
   - docs/07: the `/events` row says the parameters, the refusals, the event names and the payload.
   - docs/02: the decision above is already written. The implementation PR adds a pointer to `internal/web/sse`.

### Tests

The invariant tests run against `dbtest.Open` and the real thread service, through `sessiontest` sessions. Clock-driven cases use `testing/synctest`.
- `TestEventPayloadIsThreadAndLogLength`: every `data:` line decodes with `DisallowUnknownFields` into `{thread_id, log_length}`. The only event names are `ready` and `thread`, and no `id:` field is sent.
- `TestSubscribeAnswersAsThePage`: an unknown thread and a thread in a hidden forum are 404. A signed-in-only forum is 401 to an anonymous viewer, and 403 to a member without `read`. A list holding one hidden thread is 404. `TestPreUserCannotSubscribe` and `TestSubscribeRefusesCrossSite` cover the rest.
- `TestEventCarriesNoMoreThanThePage`, one subtest per case, each appended while a member's stream is open:
  - an author's delete;
  - a moderator's hide;
  - a post hidden pending review, by flags at the threshold;
  - a hard redaction's `Redacted` record;
  - a suspended author's visible post;
  - that author's posts after the bulk hide.

  In each case the member receives exactly `{thread_id, log_length}`, and the log length equals what that member's next `GET /threads/{id}` serves.
- Each of these appends a post after the change and asserts that the stream ends without writing the hint:
  - `TestStreamClosesWhenSessionRevoked`: `Store.Revoke`, then `RevokeUser`;
  - `TestStreamClosesWhenSessionExpires`: the clock passes `IdleExpiry`;
  - `TestStreamClosesAtHeartbeatAfterSignOut`: no events at all, and the stream ends within 25 s;
  - `TestStreamClosesWhenUserSuspended`;
  - `TestStreamClosesWhenUserDeactivated`;
  - `TestStreamClosesWhenReadRevoked`: a `perm` write removes the group's `read`;
  - `TestStreamClosesWhenThreadMovedToUnreadableForum`;
  - `TestAgentStreamClosesWhenCredentialRevoked`.
- `TestGroupGrantModeratorKeepsStream`: a member who moderates a restricted forum by group grant (docs/08, "Decided 2026-09-30") keeps receiving through hides and restores there.
- `TestForumSubscription`: a hint for each thread in the forum. Once a thread moves out, no more hints for it. A thread in a forum the viewer cannot read sends nothing.
- `TestStreamLimits`: the total, per-user and anonymous limits each answer 429 with `Retry-After`, and a closed stream frees its slot.
- `TestResumeMissesNothing`: a client that held an old length reconnects after three appends, and the snapshot carries the current length.
- `TestDroppedBatchResyncs`: a `Batch` with `Dropped > 0` makes every thread-only stream resend its snapshot, and closes every stream that names a forum.
- `TestSlowClientIsClosedAndPublishNeverBlocks`: a stream that never reads is closed once its channel fills, and `Publish` stays non-blocking throughout.
- `TestHeartbeat`: `: hb` arrives every 25 s under synctest.
- `TestHintsFlushThroughReverseProxy`: with `httputil.ReverseProxy` and its default settings in front of the handler, a hint arrives within one second of the append.
- `TestServeShutdownClosesEventStreams` (`cmd/ketju`): with one stream open, cancelling serve ends the stream. Serve returns without logging "requests still running after the drain".
- `TestRefreshSeesRevocationAndDoesNotTouch` (`internal/auth/session`) and the same for `agentcred`.
- Config: `TestSSELimitDefaultsAndEnv` and the validation cases.
- The docs/13 invariants are unchanged: `chokepoint_test.go` passes with no list edited, nothing reads or writes `respcache`, and no record type or log field is added.

### Out of scope

- The client subscription, the pill and the polling fallback: TKT-01M29CTYA0 (Add the client SSE subscription with the new-posts pill).
- Live forum lists in the client. No Phase 3 screen asks for them, and `forums=` is built here only because the design names it.
- The metrics endpoint's view of `Hub.Stats`: TKT-01M29CEE7S, in Phase 4.
- The non-JS client's refresh link: Phase 5.
- Confirming the stream through the dogfood proxy: recorded on TKT-01M3V3CFRE when it is deployed. Any change to the Traefik configuration is outside the repository (docs/14 class 3).

## Notes

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:35Z

run decision: pending maintainer review. Recorded in docs/02 'Live updates' (2026-10-01). The stream checks access as the thread page does: at subscribe, and again before every write and at every 25 s heartbeat, after resolving the session or agent credential afresh. Losing either closes the stream. The payload is {thread_id, log_length} only. The limits are limits.sse_connections 1000, limits.sse_per_user 8 and limits.sse_anonymous 100 (one pool, because web.ClientIP behind a proxy is the proxy). A stream opens with ready and a snapshot. Alternatives and why they lost are in the doc. The docs/02 'stateless' wording, against the docs/14 invariant, is a contradiction with a safer reading, so this is a run decision and not an open one.
