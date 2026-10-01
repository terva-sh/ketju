---
schema: 3
id: TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
title: Build the SSE endpoint and fan-out
type: task
status: done
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
  - ref: pr:281
    path: null
  - ref: pr:287
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T11:19:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-crj3x
  name: ""
extensions: {}
---

## Description

`GET /events?threads=&forums=` as a long-lived stream carrying `{thread_id, log_length}` invalidation hints and nothing else.

Payloads carry no content and no per-viewer logic, which is what keeps SSE stateless with respect to permissions: the client's follow-up fetch is where authorization happens. A subscriber to a thread they cannot read learns only that it changed, and that is acceptable where a forum listing already leaks as much.

The connection count is the operational risk. Every open tab is a held connection, and the limits need to be explicit rather than discovered when a community grows.

## Acceptance criteria

- [x] Every event's data is exactly {thread_id, log_length}, and the stream sends only ready and thread events and comments (TestEventPayloadIsThreadAndLogLength)
- [x] A subscription is admitted only as the thread page would admit the viewer, answering 404, 401 or 403 as the page does, and a pre-user or cross-site request is refused (TestSubscribeAnswersAsThePage, TestPreUserCannotSubscribe, TestSubscribeRefusesCrossSite)
- [x] An event about an author-deleted, moderator-hidden, hidden-pending-review or redacted post, or about a suspended author's post before and after the bulk hide, carries only the log length the viewer's next thread page serves (TestEventCarriesNoMoreThanThePage)
- [x] A member who moderates a forum by group grant keeps receiving events through hides and restores there (TestGroupGrantModeratorKeepsStream)
- [x] limits.sse_connections, limits.sse_per_user and limits.sse_anonymous are config keys with env overrides and docs/07 rows, and a stream over any of them is refused rate-limited with Retry-After (TestStreamLimits, TestSSELimitDefaultsAndEnv)
- [x] Publishing never blocks on a slow stream; a stream whose queue fills is closed (TestSlowClientIsClosedAndPublishNeverBlocks)
- [x] A hint reaches a client through httputil.ReverseProxy with default settings within one second of the append (TestHintsFlushThroughReverseProxy)
- [x] Serve's shutdown ends every open stream inside the drain and returns without cutting requests (TestServeShutdownClosesEventStreams)
- [x] A stream ends without writing the next hint when its session is revoked or expires, its user is suspended or deactivated, its agent credential is revoked, read is revoked, or the thread moves to a forum the viewer cannot read; an idle stream ends at the next heartbeat (the seven TestStreamCloses* tests and TestAgentStreamClosesWhenCredentialRevoked)
- [x] A stream opens with ready and the current log length of every thread named; a dropped bus batch resends that snapshot on thread-only streams and closes every stream that names a forum, whose client refetches on the reconnect's ready (TestResumeMissesNothing, TestDroppedBatchResyncs)
- [x] The hub handles an event only after the thread's projection row has reached it, so a move is routed to the forum the thread joined and its hint never reaches a reader who lost access by it; a row still behind after 5 s closes the affected streams (TestMoveIsRoutedAfterProjection, TestProjectionLagClosesStreams)

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
   - **The projection goes first** (docs/02, after Terva review 1712). `Deliver` first waits until `threads.log_length_projected` for each thread in the batch has reached that thread's newest event, polling every 20 ms for up to 5 s. The bus lets a subscriber block, and the cost is its own queue. Routing and every access check below then read a row that already reflects the event, so a move is routed to the forum the thread joined. A viewer who loses access through the move never receives its hint. A thread whose row is still behind after 5 s, being held or quarantined, gets no hint: every stream naming it or any forum is closed, and the reconnect is judged afresh.
   - `Deliver` never blocks a stream. It looks up the thread's streams. When any forum is subscribed, it also reads the thread's forum from its projection row, one read per event and never one per stream, and looks up that forum's streams. Each stream gets the event on its channel (capacity 64) with a non-blocking send. A full channel marks the stream overflowed and closes it, and the browser reconnects and resynchronises.
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
- `TestMoveIsRoutedAfterProjection`: the hub receives the move's event before the projection writer has applied it. A subscriber of the new forum gets the hint, a subscriber of the old forum does not, and a thread subscriber who cannot read the new forum has their stream closed without the move's hint.
- `TestProjectionLagClosesStreams`: a thread held by the projection writer for more than 5 s gets no hint, and the streams naming it or a forum are closed.
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

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T09:59:27Z

Built to the plan and its Settled in grooming, against origin/main at 6dfd01d. The plan stands as written; the points below are where the code settled something the plan left open, or differs in a detail. Each is an internal default (docs/12 "Gaps the design leaves open"), and docs/07's /events row records the wire-visible ones.

### What landed

- `internal/web/sse`: `hub.go` (the bus subscriber `sse`) and `handler.go` (`GET /events`). serve subscribes the hub after the projection writer and before `bus.Start`, mounts the handler as `web.Groups.Events`, and closes the hub from `srv.RegisterOnShutdown`.
- `session.Store.Refresh` and `agentcred.Store.Refresh` read the row again by the hash already held. Neither touches the row or creates anything, and `chokepoint_test.go` passes unedited.
- `threads.Service.ReadAccess`, `ForumAccess` and `LogLength`, plus `threads.Projections` (`internal/service/threads/live.go`).
- Config: `limits.sse_connections` (1000), `limits.sse_per_user` (8) and `limits.sse_anonymous` (100), with `KETJU_LIMITS_SSE_*` overrides and ceilings of 100000, 1000 and 100000. They appear in `ketju config check` (golden updated) and in docs/07 "Request limits".
- `dto.ThreadHint` `{thread_id, log_length}`, with the golden `thread-hint.anon.json` and the TS type in `dto.ts` and `dto-contract.ts`. `go generate ./internal/web/spa` leaves the dist byte-identical (checked with `git diff --exit-code`). The reader bundle is unchanged at 60465 B.

### Settled while building

1. **`ready` carries `data: {}`.** An EventSource drops an event that has no data line, so a bare `event: ready` would never reach the client's ready handler. The alternative was a payload on ready; it lost, because the payload is meant to be `{thread_id, log_length}` and nothing else.
2. **Hints are coalesced.** The hub routes each thread once per bus batch, at that thread's newest length. Each stream also skips a hint that is no longer than what it has already written, so a hint arriving after a snapshot that already covers it is not repeated. The client needs only the newest length.
3. **A stream opened anonymously stays anonymous.** A stream opened by a member is checked at every write and heartbeat. It ends when the refreshed session or credential is gone, or when it no longer resolves to the same active member. That second check covers a status change that leaves the session row in place. The alternative was to re-check access as anonymous and keep the stream open. It lost, because the stream was opened, and counted against the limits, as that member.
4. **Fail closed on a forum-routed hint.** A forum-routed hint for a thread the viewer cannot read is skipped only on a refusal code (not-found, forbidden, unauthenticated or unavailable). Any other error ends the stream.
5. **Unknown query parameters are `bad-request`**, as are a parameter given twice and an empty list element.
6. **`threads.Projections` is a separate type** from `threads.Service`, because the hub subscribes to the bus before serve has built the service.

### Test notes

- **Pre-user.** The pre-user gate answers `GET /events` with its 303 to the status page, as it answers any page; the plan's Settled section says nothing here changes the gate. `TestPreUserCannotSubscribe` asserts the 303, that no stream opens, and that the handler itself answers 403 should the gate ever let a pre-user through.
- **Hard redaction.** A `Redacted` record is written only by `ketju redact`, with the server stopped; the actor refuses one from anywhere else. The hard-redaction case of `TestEventCarriesNoMoreThanThePage` therefore redacts offline and restarts the stores. It then checks that the reconnect's snapshot carries the log length the next page serves.
- **Shortened timers.** `TestProjectionLagClosesStreams` and `TestStreamClosesAtHeartbeatAfterSignOut` set short timers through options and assert the 5 s and 25 s defaults. `TestHeartbeat` runs at 25 s under `testing/synctest`.
- **Close tests.** The seven `TestStreamCloses*` tests are top-level tests.

### Mutations tried, each caught

- dropping the access check before a hint;
- not refreshing the session before a hint;
- dropping the active-member check;
- routing without waiting for the projection;
- skipping the heartbeat's check;
- not closing forum streams on a dropped batch;
- not refusing cross-site requests;
- dropping the per-user limit;
- not closing the hub at serve's shutdown.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T10:18:54Z

Terva review 1733 (run da23e51a-09de-4ca2-b8b9-71afcba057cf, Actions run 1855), head 7facedc, base f3538e6. It raised four findings, one high and three medium. Each bears on what reaches a viewer and when, so each is fixed, none argued:

1. **High: recheck forum membership before writing a queued hint.** Fixed. A hint that reached a stream through a forum is now written only if `ReadAccess` places the thread in a forum the stream names at the moment of writing. The forum ids are kept from admission. Test: `TestForumHintChecksTheThreadsForumNow`.
2. **Medium: check every named forum before writing a hint.** Fixed. Every hint now runs `checkAll` first: the session or credential resolved again, plus every named thread and forum. Test: `TestHintChecksEveryNamedForum`.
3. **Medium: revalidate access while writing a multi-thread snapshot.** Fixed. The snapshot runs `checkAll` before each thread's hint, including a snapshot sent on resync. Test: `TestSnapshotRechecksBeforeEachThread`.
4. **Medium: stop processing queued hints once the hub ends a stream.** Fixed in two places. The loop's `next` checks for an end after every select, so an end wins over a ready hint. `stream.end` also sets an ended flag first, and `Hub.send` queues nothing for an ended stream. Test: `TestEndedStreamWritesNoQueuedHint`.

Mutations of each fix were caught. One was missed at first: a check before the select hid the check after it. That check was dropped, and the one after the select now covers both cases and is the one the test exercises.

The cost: each hint now re-checks the whole subscription, at most 20 threads and 10 forums, where the plan checked only the hint's thread. A stream normally names one thread.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T10:45:13Z

Terva review 1742 (run d37df3cb-421f-4a08-9023-1035dd9d0f9f, Actions run 1891), head 8354bae, base 3890b38. Of review 1733's findings, the review marks 1 to 3 resolved and finding 4 still present in part. It also raised two new mediums. Both are about a stream outliving its end, so both are fixed:

1. **Medium: stop an in-progress hint when the hub ends its stream.** This is the remaining part of review 1733's finding 4. Fixed: `write` checks that the stream has not ended, immediately before writing and after setting the deadline. That covers a hint already past its access checks. Test: `TestHubEndDuringDeliverWritesNothing`.
2. **Medium: make shutdown interrupt a blocked stream write.** Fixed. A watcher goroutine in `run` sets the write deadline to now when the client, the hub's close, or the hub's end of the stream stops it, so a write blocked on a client that has stopped reading fails at once. The watcher is waited for before `run` returns, so it never touches the connection after the handler. A stopped stream leaves the deadline in the past, so the response's own ending cannot block the drain either. Test: `TestShutdownInterruptsABlockedWrite`, in which the stream returns at once rather than after the 10 s write deadline.

A mutation of each fix was caught. Review 1733's earlier "first review" CI run (1854) was cancelled by the next push and was not a failure of the code. CI run 1867 on 8354bae was green.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T10:51:33Z

Terva review 1744 (run 54556edf-7c79-4512-9b65-02333e913f95, Actions run 1898), head bbf4e91, base 3890b38. The review marks review 1742's finding 1 resolved and finding 2 still present. It raised two mediums. Both are fixed:

1. **Medium: do not clear a shutdown deadline before flushing.** Fixed. I read the old order as already safe: the clear ran only after `Flush` returned, and `run`'s defer set a past deadline again. The fix is cheap and makes the rule explicit, so it is made. Every change to the write deadline now goes through one mutex. The watcher's `halt` marks the stream stopped and sets a past deadline under that mutex, and `setDeadline` refuses once the stream has stopped. From the stop on, the deadline only ever moves to the past. Test: `TestStoppedStreamKeepsAPastDeadline`. A mutation that restores the unconditional clear is caught.
2. **Medium: recheck access before writing the opening ready event.** Fixed. `ServeHTTP` runs `checkAll` after the stream is registered and before anything is written. A loss is answered as admission answers it: the page's code, or unauthenticated for an ended session. Test: `TestReadyFollowsAFreshCheck`. A mutation that drops the check is caught.

This was review round 3 of 5.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T10:56:27Z

Terva review 1745 (run 22b6f331-1215-4493-8cab-504b49d90dc1, Actions run 1906), head 61ccccb, base 3890b38. The review marks both of review 1744's findings resolved. It raised one medium and one low. Both are fixed:

1. **Medium: do not report every refresh failure as an ended session.** Fixed. `current` marks a stream `errEnded` only for `session.ErrNoSession`, `agentcred.ErrNoCredential`, or a different or inactive member. Any other refresh error, such as a database failure, is returned as it is. The stream still ends, failing closed, and the check before ready answers it 500 rather than 401. Test: `TestRefreshErrorIsNotAnEndedSession`. A mutation is caught.
2. **Low: enforce subscription limits before deduplicating entries.** Fixed. `parse` bounds the raw comma-separated count against `MaxThreads` and `MaxForums` before it drops duplicates. Test: `TestParseBoundsTheListAsSent`. A mutation is caught.

This was review round 4 of 5. The full `go test -race ./...` passes on this tree.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T11:05:23Z

Carried to #287. Main moved under #281 when the blob store (#284, TKT-01M29CXK0K) landed, and the two PRs conflicted in `cmd/ketju/serve.go` and docs/07. Per the run brief, the final tree of #281 is carried onto main ccb234d as one commit, bc34f19, on `run/crj3x-sse-fanout-2`, and #281 is closed with a comment. Both sides of each conflict are kept: `Blobs` and `Events` are both mounted, and docs/07's request limits keep #284's upload row and the three stream rows.

Terva review 1746 (round 5 on #281: head c5eef67, base ccb234d, run bc2937e2-8935-456c-a64b-d47d517eac77): the status was success, with one low finding, "Reject malformed query parameters instead of silently ignoring them" (handler.go:105). `URL.Query()` drops a pair it cannot decode, so `forums=%ZZ` was read as absent. It is fixed in #287: `parse` uses `url.ParseQuery(r.URL.RawQuery)` and answers `bad-request` on its error. `TestParseRefusesAMalformedQuery` covers a bad escape in a value, a bad escape in a key and a `;` separator, and checks that a well-formed escape still decodes. Reverting the check is caught. The docs/07 /events row says so.

**agent:claude-code/phase3-run-crj3x** at 2026-10-01T11:19:54Z

Terva review 1747 on #287 (round 1 on #287: head b3aba54, base ccb234d, run 57dce6d8-b03a-4516-b7ea-c7f0f79af213, Actions run 1920): status success, with one low finding, "Do not count interrupted projection waits as lag" (hub.go:338). `await` returned the threads still behind both when the wait ran out and when the bus context or the hub's shutdown cut it short, and `Deliver` counted every one in `Stats.Lagged`. So a shutdown could report lag that never happened. Fixed: `await` also reports whether the wait ran its full length, and only then is `Lagged` counted. Either way the streams that could hear of the thread still end, because their hint cannot be routed. Ending them fails closed, which is the security posture. `TestInterruptedWaitIsNotLag` covers a cancelled context, a closed hub and a wait that ran out. Three mutations were tried and each was caught: counting lag unconditionally, never reporting a timeout, and not ending the streams.

## Summary

`GET /events` is built in `internal/web/sse` and wired into `ketju serve`.

**Stream contents.** The stream opens with `event: ready`, carrying `data: {}`, then sends a snapshot of each thread it names. After that it sends `event: thread` with `{thread_id, log_length}` (`dto.ThreadHint`) and nothing else, plus a `: hb` heartbeat every 25 s.

**Access checks.** A subscription is admitted as the thread page and the thread list admit the viewer. Before every write and at every heartbeat, the stream resolves its session or agent credential again (`session.Store.Refresh`, `agentcred.Store.Refresh`, neither of which touches the row) and checks thread access again. It closes on any loss.

**The hub.** The hub is the bus subscriber `sse`. It routes an event only once the thread's projection row has reached it, so a move goes to the forum the thread joined. A thread still behind after 5 s closes the streams that could hear of it. A dropped batch resends the snapshot on streams that name only threads, and closes streams that name a forum. A stream whose 64-hint queue fills is closed, and publishing never blocks.

**Limits.** Three config keys set the limits, with env overrides, docs/07 rows and `ketju config check` output: `limits.sse_connections` (1000), `limits.sse_per_user` (8) and `limits.sse_anonymous` (100). A stream over any of them is refused `rate-limited` with `Retry-After: 30`.

**Shutdown.** Serve closes the hub as shutdown begins, so the drain does not wait for open streams.

Every acceptance criterion has a named test, and the mutations listed in the notes were each caught. The client dist is byte-identical. The reader bundle is unchanged at 60465 B.

The client subscription is TKT-01M29CTYA0 (Add the client SSE subscription with the new-posts pill). Confirming the stream through the dogfood proxy belongs to TKT-01M3V3CFRE (Check Phase 3's exit criterion with named end-to-end tests).
