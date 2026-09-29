---
schema: 3
id: TKT-01M29CKRGSF2FPY73MNNFPQ1FV
title: Implement thread lock, unlock, pin, and unpin
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
  - ref: pr:161
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T21:22:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckrgs
  name: ""
extensions: {}
---

## Description

Lock and unlock with a reason, pin and unpin with a forum or global scope and an optional expiry. Each is a record in the thread log and a row in audit_log.

Locking is not archiving. A locked thread accepts no posts or edits from members, but moderators may still post and their posts are styled as such. Archiving is the stronger, admin-level state.

## Acceptance criteria

- [x] After ketju reindex a thread's locked, pinned_scope and pinned_until equal what its last lock and pin records say (TestLockAndPinSurviveReindex)
- [x] In a locked thread a member's reply and edit answer conflict and append nothing, while a moderator's reply succeeds and is served with author.role moderator
- [x] A pin whose until has passed is absent from the thread list's pins and from the thread header's pinned under a fixed clock, with no background job
- [x] Lock, unlock, pin and unpin each append one record and write one audit_log row naming the actor, the thread, the action and the reason, and scope and until for pins
- [x] Each action is forbidden without moderate in the thread's forum and conflict on an archived thread or when it would change nothing, and a refusal appends nothing and writes no audit row
- [x] A page cached before a lock is not served after it: the next GET is a response-cache miss under the new log length (TestLockMovesTheResponseCacheKey)
- [x] Audit rows for log-recorded moderation are written by the projection writer in the record's projection transaction: a failed audit insert rolls the projection back and the retry writes the row, reindex adds no duplicate, and an author's own action writes none (TestAuditRowsComeFromTheLog)

## Implementation plan

Revised 2026-09-29 against main at 855ca5d, at claim. The groomed plan (against 7af2c5a) stands with the changes listed under "Revised at claim"; the Settled items below are carried verbatim.

### Settled in grooming

- **No new record type.** `ThreadLocked` (12), `ThreadUnlocked` (13), `ThreadPinned` (14) and `ThreadUnpinned` (15) are in the docs/03 catalogue and in `internal/store/threadlog/records.go` with bodies `{reason}` and `{scope, until}`. The projection already applies all four (`applyLocked`, `applyPinned`, `applyUnpinned` in `internal/events/projection/apply.go`). This ticket writes them; it does not touch the format.
- **Who.** A moderator: `access.canModerate` for the thread's forum (docs/08 "Primitives", docs/07 "Moderation and admin": locking and pinning are moderator actions; an admin passes as a moderator). Both pin scopes take the same permission, because docs/08 names one role for both.
- **Locking is not archiving** (docs/08 "Locking semantics"). A locked thread refuses member posts and edits; moderators may still post. The refusals already exist in `Reply` (`internal/service/threads/create.go`) and `Edit` (`mutate.go`) against `access.row.Locked`. A member deleting or restoring their own post in a locked thread stays allowed (docs/07 "Edit, delete and restore").
- **Pin expiry needs no job.** A pin whose `pinned_until` has passed is no pin (docs/07 thread list row; `views.activePin` in `internal/store/db/views/threadlist.go` already compares against now). The projection row keeps the expired values until the next pin record.
- **Every action is a thread-log record and an `audit_log` row** (docs/08 opening paragraph; epic TKT-01M29BN36J).
- **Response cache.** Every one of these records is an append, so it moves the log length, and the log length is in `respcache.Key`. No new viewer class is needed (docs/07 "The response cache").

### Revised at claim

- **Missing actor rows.** `audit_log.actor_id` references `users (id)` and foreign keys are on, while the projection tables have no such reference: a log can name a member the database lacks (a database restored from a backup older than the logs, which reindex's member id floor exists for). `auditFor` writes such an actor as NULL and keeps the log's id in `details_json` as `"log_actor"`, so an audited record can never wedge a thread's projection on a foreign key.
- **Delete `auto` and `by_moderator`** are not keys `DeleteBody` has yet (TKT-01M29CRJ5J and TKT-01M29CP2PJ add them). Until they exist, a `Delete` is audited when its actor is not the post's author, the same rule as `Edit` and `Restore`, which is what a moderator's delete of someone else's post is.
- **Client (step 6)** moves to a draft, filed and linked from here: the client lane holds `internal/web/spa/dist` during this run (docs/13 "Running two lanes"), so this PR changes no client code or dist.
- **No migration.** The `audit_log_target` index already serves `AuditExistsForRecord`; two small reads are added, `PostAuthor` and `UserExists`.
- The header answer is built by one helper, `threadHeader`, from the projection row re-read after the synchronous projection, the forum, the tags and one index snapshot, rather than by drawing a whole page.

### Approach

1. **Audit rows for log-recorded moderation come from the log**, written by the projection writer in the transaction that projects the record (revised for Terva review 1352 on PR #138, finding 1; this replaces the service-side audit write, and the two helpers, of the review 1343 revision).
   - **Why this order.** The log append and a database row cannot share a transaction. Writing the audit row before the append lost: `audit_log` is append-only by trigger (docs/04), so a pending row can never be completed or withdrawn, and an append that then fails leaves a row for an action that never happened. Writing it after the append from the service lost: a failure there leaves a durable action with no row and nothing that ever writes one, which is what review 1352 found. The projection writer already applies every record exactly once in effect. It projects a range in one transaction that advances `projection_state`, and a thread whose projection fails is held and retried (docs/03 "Write path"). An audit row written in that transaction therefore exists if and only if the record's projection committed, and a failure is retried until it does. No other reconciliation job is needed.
   - **Where.** `internal/events/projection/audit.go`: `auditFor(ctx, q, f frame) (action string, target audit.Target, details map[string]any, ok bool)`, called by each `applyFunc` that can carry a moderation action. The row goes through `internal/audit` from TKT-01M3NRYCP5 (Add the admin API gate, audit helper and admin screen scaffold, filed on PR #140), whose `TestOneWayIntoAuditLog` refuses any other path to `dbq.AppendAudit`. `at` and `actor_id` are the frame's timestamp and actor, with actor 0 written as NULL. `ip` is empty, because the log does not carry an address; the request log does.
   - **Which records are audited** is decided from the record and the rows it is projected against, so an author's own action never produces a row and there is no helper choice to get wrong (review 1343, finding 3). Always audited: `ThreadLocked`, `ThreadUnlocked`, `ThreadPinned`, `ThreadUnpinned`, `ThreadMoved`, and a `Delete` with `auto` or `by_moderator`. Audited when the frame actor is not the author: `ThreadRenamed` (against `threads.author_id`), and `Edit` and `Restore` (against `post_meta.author_id`). Never audited: `ThreadCreated`, `Post`, an author's own `Edit`, `Delete` or `Restore`, and `ThreadArchived` until its own ticket decides.
   - **Idempotent across reindex.** `ketju reindex` empties projection tables but not `audit_log`, and it replays every record. Each row's details carry `{thread, log_offset}`. The new query `AuditExistsForRecord` (target, action, `json_extract(details_json, '$.log_offset')`, through the `audit_log_target` index) is checked before the insert, so a replay adds no second row.
   - **The service side** is one helper in `internal/service/threads/moderate.go`: `appendEvent(ctx, id, rec)` appends through `s.Writer.Append` and projects synchronously through `s.Project(ctx, id, res.LogLength)`, as `CreateThread` does. Because the audit row is written by that projection, it exists when the handler answers. If the synchronous projection fails, the handler answers `internal`, and the bus-driven projection retries and writes the row. TKT-01M29CKRH9, TKT-01M29CRJ5J and TKT-01M29CP2PJ use it. Once PR #140 lands, link this ticket to TKT-01M3NRYCP5 with `--depends-on`; it cannot be linked from this branch, where that ticket does not exist yet.
   - Synchronous projection also matters because `access` reads `locked` from the projection row. Without it a member's reply sent right after the lock could land.
   - The four routes below register through TKT-01M3NRYCP5's `adminRoute` with the `staff` gate, then check `moderate` on the thread's forum in the service.
2. **Service methods** in `moderate.go`: `Lock(ctx, v, id, reason)`, `Unlock(ctx, v, id, reason)`, `Pin(ctx, v, id, scope string, until *time.Time)`, `Unpin(ctx, v, id, scope)`. Each one calls `MayWrite` and then `access`. Refusals, in this order, all before any append:
   - no `canModerate`: `forbidden`;
   - `th.Archived()`: `conflict`;
   - locking a locked thread, unlocking an unlocked one, unpinning a scope that is not the active pin, or pinning with the same scope and until as the active pin: `conflict`, so the log keeps no records that change nothing (the docs/07 rule for Delete);
   - scope other than `forum` or `global`, an `until` not after now, or a reason over 500 characters: `validation-failed`.

   Reasons are optional, as they are for Delete and Restore (docs/07). Requiring one for a lock lost: nothing in docs/08 requires it, and an empty reason is still an audited action. The state checks read the projection row. Two moderators locking at the same instant can both append. Projection is idempotent, so the cost is one redundant record, and that is accepted.
3. **Audit actions**: `thread.locked`, `thread.unlocked`, `thread.pinned`, `thread.unpinned`, all with `target_type` `thread` and `target_id` the base32 id. `details_json` holds `{reason}` or `{scope, until}`, plus `thread` and `log_offset`, all taken from the record by `auditFor`.
4. **Routes** in a new `internal/web/api/moderate.go`, under the docs/07 admin prefix: `POST /api/v1/admin/threads/{id}/lock` and `/unlock` take `{reason?}`. `POST /api/v1/admin/threads/{id}/pin` takes `{scope, until?}`, where until is unix ms. `POST /api/v1/admin/threads/{id}/unpin` takes `{scope}`. Each answers 200 with `dto.ThreadHeader` as the thread page now serves it, sets `no-store`, and is charged to the write limiter. Bodies are read with `optionalJSON` at 4 KiB, as in `mutate.go`. Register them in `api.New`.
5. **Thread header pin expiry**: `header()` in `internal/service/threads/view.go` shows `Pinned` only while `pinned_until` is null or after `s.now()`, the same rule as `views.activePin`. The header is not cached, so the clock applies on every request.
6. **Client**: `web/client/src/shell/threadActions.ts` gains Lock/Unlock and Pin/Unpin when `viewer.can_moderate`. A small dialog, `components/thread/ModerateDialog.tsx`, takes a reason, or a scope and an optional expiry. The answer's header replaces the cached one. The thread head shows the existing "This thread is locked." and a pinned note. Run `go generate ./internal/web/spa` and commit `dist/`.
7. **Docs**: docs/07 "Moderation and admin" gains the four routes with bodies and refusals. docs/04 notes that the `audit_log` actions above are in use.

### Tests

- `internal/service/threads/moderate_test.go`: lock, unlock, pin and unpin each append exactly one record of the right type, answer the new header, and write one audit row with the actor, action, target and reason. `forbidden` for a member, `conflict` on an archived thread, and `conflict` on each no-op case each leave the log length unchanged and write no audit row.
- `TestAuditRowsComeFromTheLog` (`internal/events/projection`): projecting a log with lock, pin, a moderator rename and an author rename writes rows for the first three and none for the author's. `ketju reindex` over the same database adds no second row. With the audit insert made to fail once, the range's projection rolls back, `projection_state` does not advance, and the retry writes the row, so no projected moderation record is ever left without one.
- TKT-01M3NRYCP5's `TestOneWayIntoAuditLog` passes over `internal/events/projection` and `internal/service/threads`.
- `TestLockedThreadRefusesMembersNotModerators`: after `Lock`, a member's `Reply` and `Edit` are `conflict`. A moderator's `Reply` succeeds and is served with `author.role` `moderator`. A member's `Delete` of their own post still succeeds.
- `TestPinExpiryNeedsNoJob`: with `Service.Now` fixed, pin with until = now+1h. The thread list shows it pinned and the header shows `pinned`. Advance the clock past until: the list sorts it with the rest and the header's `pinned` is null. No job runs in the test.
- `TestLockAndPinSurviveReindex` (`internal/events/projection`): a log with lock, unlock, lock, pin (global, until), unpin (forum, a no-match), and after `ketju reindex` the row is locked with the global pin and its until.
- **Response cache invariant**, `TestLockMovesTheResponseCacheKey` (`internal/web/api/respcache_test.go`): GET a page twice (a hit), lock the thread, then GET again. The answer is a miss under a new key with the new log length, and its header says locked.
- API: each route's refusal codes, and a golden of the lock answer.
- Client: the actions show only when `can_moderate`, and the dialog sends the body the route takes.

### Out of scope

- Archiving a thread (`ThreadArchived`, admin): no ticket in this group; docs/08 lists it.
- Rename and move: TKT-01M29CKRH9. Retag: TKT-01M29CYSCP (phase 4).
- Live updates of a lock to open pages: the SSE fan-out, TKT-01M29CRJ3X (phase 3).
- The audit log view: TKT-01M29CP2R4.
- Locking and pinning in an archived forum stay open to moderators (maintainer decision 2026-09-29 on TKT-01M3N24RHD). Only the archived-thread refusal applies.

## Notes

**agent:claude-code/phase2-run-ckrgs** at 2026-09-29T21:19:06Z

Implementation notes, 2026-09-29.

- The client half of the groomed plan (step 6) is TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen), filed as a draft that depends on this ticket. This PR changes nothing under web/client or internal/web/spa/dist.
- No migration and no dto change. Two queries were added: AuditExistsForRecord (moderation.sql) and PostAuthor (projection.sql).
- Mutations tried against the tests: dropping the idempotency check in writeAudit fails TestAuditRowsComeFromTheLog and TestLockAndPinSurviveReindex on reindex; auditing an author's own Edit/Delete/Restore, or an author's own rename, fails TestAuditRowsComeFromTheLog; the header showing an expired pin fails TestPinExpiryNeedsNoJob; skipping the synchronous projection fails TestLockedThreadRefusesMembersNotModerators and TestModerationAppendsOneRecordAndOneAuditRow; taking both LogLength and Projected out of respcache.Key fails TestLockMovesTheResponseCacheKey. Taking out LogLength alone does not: Projected moves with the same append, because the service projects before it answers. Either field alone keeps the invariant.
- The header now reads the service clock (pin expiry). TestResponseCacheUnderConcurrency failed under -race on the test world's clock, a plain field that w.post advances while readers call View. The test now freezes the readers' clock. The race was in the test harness, not in the server.
- The actor of an audited record may be missing from users (a database restored from an older backup). audit_log.actor_id references users, so such a row is written with a NULL actor and "log_actor" in its details. Without that, a foreign key failure would hold the thread's projection until someone repaired it by hand. Actor 0 is written with "via": "thread-log". Both are documented in docs/04 "audit_log".
- The routes are gated staff by role, as docs/07 "Moderation and admin" says, and the service then checks moderate in the thread's forum. A member granted moderate only through a group cannot reach them. That is docs/07's rule; this ticket does not change it.

**agent:claude-code/phase2-run-ckrgs** at 2026-09-29T21:21:35Z

Terva review 1471 (head 0a2323e, base fbe4787), finding 1, medium, 'Ensure cached thread pages expire when their pin expires': fixed in the test, not in the code. The response cache holds only a page's posts (threads.pagePosts; respcache.Key). View draws the header, pin included, on every request from the projection row and s.now(), outside the cached entry, so a cache hit cannot serve a stale pin. TestPinExpiryNeedsNoJob now proves it: after the clock passes the pin's end, the next GET is a response-cache hit (Hits +1, Inserts unchanged), its header has pinned null, and it equals a page drawn with no cache (w.same).

**agent:claude-code/phase2-run-ckrgs** at 2026-09-29T21:22:38Z

Terva review of PR #161, head 1a0f7b90bbedb891098a15cf7e500b470db32de6, base fbe4787173af5ec9989c2b3f49e7bfafb11a3c94: clean (run bbf32161-36ac-4708-9502-4ac77d30423b, Actions run #1015, request ready-review-1a0f7b9). No findings. The model marked finding 1 of review 1471 (head 0a2323e) resolved. Commits after this head touch only .tickets/ and are carried.

## Summary

Lock, unlock, pin and unpin are staff routes under /api/v1/admin/threads/{id}/ (internal/web/api/moderate.go). Each is backed by threads.Service Lock, Unlock, Pin and Unpin (internal/service/threads/moderate.go). Each action checks moderate in the thread's forum, then the archived thread, then whether the action would change anything, then the request. It appends one record, projects it synchronously through the shared appendEvent helper, and answers the thread header.

The projection writer now writes the audit_log row for every moderation record it projects, in the same transaction (internal/events/projection/audit.go). The row is keyed by log offset, so a reindex adds none, and it is written through internal/audit. An author's own rename, edit, delete or restore writes no row. The thread header drops a pin whose end has passed. The routes are documented in docs/07 and the audit actions in docs/04. The client screen is TKT-01M3QGH66W.
