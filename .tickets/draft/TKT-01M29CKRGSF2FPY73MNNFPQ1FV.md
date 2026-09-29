---
schema: 3
id: TKT-01M29CKRGSF2FPY73MNNFPQ1FV
title: Implement thread lock, unlock, pin, and unpin
type: task
status: draft
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
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T05:06:07Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Lock and unlock with a reason, pin and unpin with a forum or global scope and an optional expiry. Each is a record in the thread log and a row in audit_log.

Locking is not archiving. A locked thread accepts no posts or edits from members, but moderators may still post and their posts are styled as such. Archiving is the stronger, admin-level state.

## Acceptance criteria

- [ ] After ketju reindex a thread's locked, pinned_scope and pinned_until equal what its last lock and pin records say (TestLockAndPinSurviveReindex)
- [ ] In a locked thread a member's reply and edit answer conflict and append nothing, while a moderator's reply succeeds and is served with author.role moderator
- [ ] A pin whose until has passed is absent from the thread list's pins and from the thread header's pinned under a fixed clock, with no background job
- [ ] Lock, unlock, pin and unpin each append one record and write one audit_log row naming the actor, the thread, the action and the reason, and scope and until for pins
- [ ] Each action is forbidden without moderate in the thread's forum and conflict on an archived thread or when it would change nothing, and a refusal appends nothing and writes no audit row
- [ ] A page cached before a lock is not served after it: the next GET is a response-cache miss under the new log length (TestLockMovesTheResponseCacheKey)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **No new record type.** `ThreadLocked` (12), `ThreadUnlocked` (13), `ThreadPinned` (14) and `ThreadUnpinned` (15) are in the docs/03 catalogue and in `internal/store/threadlog/records.go` with bodies `{reason}` and `{scope, until}`. The projection already applies all four (`applyLocked`, `applyPinned`, `applyUnpinned` in `internal/events/projection/apply.go`). This ticket writes them; it does not touch the format.
- **Who.** A moderator: `access.canModerate` for the thread's forum (docs/08 "Primitives", docs/07 "Moderation and admin": locking and pinning are moderator actions; an admin passes as a moderator). Both pin scopes take the same permission, because docs/08 names one role for both.
- **Locking is not archiving** (docs/08 "Locking semantics"). A locked thread refuses member posts and edits; moderators may still post. The refusals already exist in `Reply` (`internal/service/threads/create.go`) and `Edit` (`mutate.go`) against `access.row.Locked`. A member deleting or restoring their own post in a locked thread stays allowed (docs/07 "Edit, delete and restore").
- **Pin expiry needs no job.** A pin whose `pinned_until` has passed is no pin (docs/07 thread list row; `views.activePin` in `internal/store/db/views/threadlist.go` already compares against now). The projection row keeps the expired values until the next pin record.
- **Every action is a thread-log record and an `audit_log` row** (docs/08 opening paragraph; epic TKT-01M29BN36J).
- **Response cache.** Every one of these records is an append, so it moves the log length, and the log length is in `respcache.Key`. No new viewer class is needed (docs/07 "The response cache").

### Approach

1. **Moderation append helper**, new file `internal/service/threads/moderate.go`: `func (s *Service) moderate(ctx, v domain.Viewer, id threadid.ID, rec threadlog.Append, audit auditEntry) (threadlog.Result, error)`. It appends through `s.Writer.Append`, projects synchronously through `s.Project(ctx, id, res.LogLength)` as `CreateThread` does, and then writes one `audit_log` row with `dbq.AppendAudit`: `actor_id` the viewer, `action`, `target_type`, `target_id`, and `details_json` holding the reason, any action fields, and `log_length`. The same helper serves TKT-01M29CKRH9 (rename, move) and TKT-01M29CRJ5J (moderator edits and deletes), which depend on this ticket for it.
   - Order: log first, then the audit row. An audit row written before an append that then fails would record an action that never happened. A missing audit row after a durable append can be recovered from the log, which carries the actor and the time. If the audit write fails, the handler answers `internal` and logs the thread id and log length at error.
   - Synchronous projection because `access` reads `locked` from the projection row. Without it a member's reply sent right after the lock could land.
2. **Service methods** in `moderate.go`: `Lock(ctx, v, id, reason)`, `Unlock(ctx, v, id, reason)`, `Pin(ctx, v, id, scope string, until *time.Time)`, `Unpin(ctx, v, id, scope)`. Each one calls `MayWrite` and then `access`. Refusals, in this order, all before any append:
   - no `canModerate`: `forbidden`;
   - `th.Archived()`: `conflict`;
   - locking a locked thread, unlocking an unlocked one, unpinning a scope that is not the active pin, or pinning with the same scope and until as the active pin: `conflict`, so the log keeps no records that change nothing (the docs/07 rule for Delete);
   - scope other than `forum` or `global`, an `until` not after now, or a reason over 500 characters: `validation-failed`.

   Reasons are optional, as they are for Delete and Restore (docs/07). Requiring one for a lock lost: nothing in docs/08 requires it, and an empty reason is still an audited action. The state checks read the projection row. Two moderators locking at the same instant can both append. Projection is idempotent, so the cost is one redundant record, and that is accepted.
3. **Audit actions**: `thread.locked`, `thread.unlocked`, `thread.pinned`, `thread.unpinned`, all with `target_type` `thread` and `target_id` the base32 id. `details_json` holds `{reason}` or `{scope, until}`, plus `log_length`.
4. **Routes** in a new `internal/web/api/moderate.go`, under the docs/07 admin prefix: `POST /api/v1/admin/threads/{id}/lock` and `/unlock` take `{reason?}`. `POST /api/v1/admin/threads/{id}/pin` takes `{scope, until?}`, where until is unix ms. `POST /api/v1/admin/threads/{id}/unpin` takes `{scope}`. Each answers 200 with `dto.ThreadHeader` as the thread page now serves it, sets `no-store`, and is charged to the write limiter. Bodies are read with `optionalJSON` at 4 KiB, as in `mutate.go`. Register them in `api.New`.
5. **Thread header pin expiry**: `header()` in `internal/service/threads/view.go` shows `Pinned` only while `pinned_until` is null or after `s.now()`, the same rule as `views.activePin`. The header is not cached, so the clock applies on every request.
6. **Client**: `web/client/src/shell/threadActions.ts` gains Lock/Unlock and Pin/Unpin when `viewer.can_moderate`. A small dialog, `components/thread/ModerateDialog.tsx`, takes a reason, or a scope and an optional expiry. The answer's header replaces the cached one. The thread head shows the existing "This thread is locked." and a pinned note. Run `go generate ./internal/web/spa` and commit `dist/`.
7. **Docs**: docs/07 "Moderation and admin" gains the four routes with bodies and refusals. docs/04 notes that the `audit_log` actions above are in use.

### Tests

- `internal/service/threads/moderate_test.go`: lock, unlock, pin and unpin each append exactly one record of the right type, answer the new header, and write one audit row with the actor, action, target and reason. `forbidden` for a member, `conflict` on an archived thread, and `conflict` on each no-op case each leave the log length unchanged and write no audit row.
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
- Whether a moderator may lock or pin in an archived forum follows the open decision on TKT-01M3N24RHD. Until that decision is made, apply only the archived-thread refusal.
