---
schema: 3
id: TKT-01M29CRJ50CEYVG2VHYEXYVDKB
title: Implement per-thread slow mode
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-30T02:02:26Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-crj50
  name: ""
extensions: {}
---

## Description

A minimum number of seconds between posts per user in a thread, set by a moderator. Stored in the database rather than the log, because it is a live moderation control rather than part of the thread's history.

Slow mode is the tool for a thread that is heating up, and it works by making people wait rather than by removing anything. Moderators are exempt.

## Acceptance criteria

- [x] A reply inside the thread's interval answers rate-limited with Retry-After equal to the whole seconds left, rounded up, and appends nothing
- [x] Moderators are exempt and agent accounts are not
- [ ] The interval is per thread in thread_slow_mode, overrides slow_mode.default_seconds (an explicit 0 included), survives a restart and ketju reindex, and the composer shows it from viewer.slow_mode_seconds
- [x] Setting 0 lets the next reply through at once
- [x] Setting slow mode is forbidden without moderate, and each change writes one audit_log row with from and to
- [x] Setting slow mode leaves the page's posts under the same response-cache key while the viewer block reflects it (TestSlowModeIsInTheViewerBlockNotTheCache)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Slow mode lives in the database, not in the log** (docs/08 "Primitives": "`settings` per thread in DB, not log"). It is a live control rather than part of the thread's history, so no record type is involved. The frozen format is untouched, and slow mode is not recoverable from a log. That is intended.
- **Slow mode stays in Phase 2.** Maintainer decision 2026-09-29: this ticket keeps the phase-2 milestone, whatever docs/09 says, where slow mode is listed under Phase 4.
- **Slow mode can be set in an archived forum.** It is a moderation write (maintainer decision 2026-09-29 on TKT-01M3N24RHD); only an archived thread refuses it.
- **Off by default; moderators are exempt; agents are not** (docs/08 "Primitives" and "Rate limits": "Slow mode applies to agents as it does to anyone").
- **The check exists.** `slowMode` in `internal/service/threads/create.go` refuses a reply less than N seconds after the author's newest post in the thread, read from the index. It answers `rate-limited` with `Retry-After` rounded up to whole seconds. Today N is the site-wide `slow_mode.default_seconds` (TKT-01M29P9RS0). The settings comment in `internal/settings/settings.go` names this ticket for the per-thread value.
- **A per-thread value overrides the site default**, including an explicit 0, which turns a thread off under a non-zero default. With no row, the site default applies.
- **"Naming the remaining wait"** is the `Retry-After` header. Problem bodies never interpolate (docs/07 "Error catalogue").
- **Response cache.** Slow mode is per thread, and the wait is per viewer, so both go in the thread page's viewer block. `View` builds that block on every request and never caches it (docs/07 "The response cache"). The cached posts do not change.

### Approach

Revised 2026-09-30 by agent:claude-code/phase2-run-crj50 against main at 2fc352f. The settled items above are unchanged. This PR is the server half. The composer notice, the countdown and the moderator's "Slow mode…" dialog go to a draft filed against this ticket.

1. **Migration 0016**, or the next free number when this merges. #186 also takes 0016, and whichever PR merges second renumbers (`migrate.go`). The table is `thread_slow_mode (thread_id INTEGER PRIMARY KEY, seconds INTEGER NOT NULL CHECK (seconds BETWEEN 0 AND 86400), set_by INTEGER REFERENCES users (id), set_at INTEGER NOT NULL)`. It is authoritative. It has no PROJECTION marker, it is not in `db.ProjectionTables`, and it has no foreign key to `threads`, so `ClearThreads` and its triggers cannot reach it. The queries go in `queries/moderation.sql`: `GetThreadSlowMode` and `PutThreadSlowMode` (an upsert). Regenerate dbq.
2. **Service**: a new file, `internal/service/threads/slowmode.go`. It stays out of moderate.go so that it cannot collide with the rename/move lane. `SetSlowMode(ctx, v, ip, id, seconds)` checks in the order moderate() does:
   1. MayWrite.
   2. access: not-found, unauthenticated, or forbidden to read.
   3. `canModerate`, or `forbidden`.
   4. An archived thread is `conflict`, read from the thread cache as moderate() reads it. An archived forum is allowed.
   5. Seconds outside 0..86400 are `validation-failed`.
   6. One `db.Write` reads the row. The value the row already holds is `conflict`. Otherwise it upserts the row and appends one audit row through `audit.Append`: action `thread.slow_mode`, target `audit.Thread(id)`, details `{from, to}`. `from` is null when there was no row. The actor is v and the IP is the request's.

   A write with no row is a change even when it equals the site default, because it pins the thread against a later change of the default. The audit row comes from the service's own transaction, not from the projection writer, because there is no record to project (docs/04 "audit_log": "A service writes it from its own write transaction").
3. **Reply**: `effectiveSlowMode` reads the row. When there is none, it reads `settings.SlowModeDefaultSeconds.In` in the same read. It replaces the bare setting read. `slowMode` itself and the `canModerate` exemption do not change.
4. **Viewer block**: `dto.ThreadViewer` gains two fields.
   - `slow_mode_seconds` is the effective interval, 0 when off, served to every viewer.
   - `next_post_at` (`*Millis`) is the viewer's newest post plus the interval, read from the index inside the page's Stable snapshot. It is null for an anonymous viewer, for a moderator, and when the viewer may post now.

   Update the dto fixtures and goldens. `web/client/src/api/dto.ts` and `dto-contract.ts` carry the types. The built client does not change: the orchestrator approved type-only edits, with dist byte-identical.
5. **Route**: `PUT /api/v1/admin/threads/{id}/slow-mode` with `{seconds}`. It sits behind the staff gate, as lock and pin do, and the service then checks moderate in the thread's forum. A missing `seconds` is `bad-request` (strict decode). The answer is 200 `dto.SlowMode{seconds}` with no-store. `adminRoute` charges it to the write limiter.
6. **Docs**:
   - docs/04 gets `thread_slow_mode` under the authoritative tables, and the `thread.slow_mode` action under audit_log.
   - docs/07 gets the route. Its `POST /threads/{id}/posts` row names the per-thread value and the two viewer fields.

### Tests

- `TestSlowModePerThread` (api): the thread is at 30 s. A member's second reply 10 s after the first is `rate-limited` with `Retry-After: 20` and appends nothing. At 30 s it succeeds. The agent (scribe) is refused the same way. The moderator (bob) is not.
- `TestSlowModeOverridesDefault`: with a site default of 60 and a thread row of 0, the member replies at once. With no row, the default of 60 applies.
- `TestSlowModeOffIsImmediate`: set 30, reply, then set 0. An immediate second reply succeeds.
- `TestSlowModeSurvivesRestartAndReindex` (cmd/ketju): the row is still there after the database is reopened and after `ketju reindex`.
- `TestSetSlowModeRefusals`: none of these cases writes a row or an audit row.
  - Anonymous is unauthenticated.
  - A member is forbidden, at the gate and through the service.
  - An archived thread is `conflict`.
  - 86401 and -1 are `validation-failed`.
  - A missing seconds is `bad-request`.
  - An unchanged value is `conflict`.

  A success writes one audit row naming bob, with from and to. A thread in an archived forum accepts the change.
- `TestSlowModeIsInTheViewerBlockNotTheCache`: with the cache on, the page is cached for each class. Setting slow mode leaves the posts under the same key, so the next GET is a hit. The same response's `viewer.slow_mode_seconds` and `next_post_at` show the new value, and its ETag differs.

### Out of scope

- Per-agent token buckets: TKT-01M29P9RSW.
- The site-default setting's admin screen: TKT-01M29CXK2H.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:39:01Z

Maintainer decision, 2026-09-29: slow mode stays in phase-2, whatever docs/09 says (it lists slow mode under Phase 4). Recorded in the plan's Settled section.

**agent:claude-code/phase2-run-crj50** at 2026-09-30T02:01:35Z

Criterion 3 is left unticked. Its server half is done and tested: the interval lives in thread_slow_mode (migration 0016). It overrides slow_mode.default_seconds, an explicit 0 included (TestSlowModeOverridesDefault). It survives a restart and ketju reindex (cmd/ketju TestSlowModeSurvivesRestartAndReindex). viewer.slow_mode_seconds and viewer.next_post_at are served. What remains is "the composer shows it from viewer.slow_mode_seconds", which is client work. The orchestrator scoped this PR to the server half because another lane holds the committed dist. That work is TKT-01M3R0PM0KPX323DJ130WWKE2J (Show slow mode in the composer and add the moderator's slow mode dialog), filed as a draft that depends on this ticket.

Decisions taken in the build:
- **The audit row comes from the service, not the projection writer.** The run prompt said that thread-level moderation goes through the projection writer's audit rows by log offset, as lock and pin do. That path exists for actions the thread log records. The settled item "Slow mode lives in the database, not in the log" means there is no record and no offset, and the original plan said TKT-01M29CKRGS's helpers are not used. So SetSlowMode writes its row through audit.Append, from its own db.Write, in the transaction that writes thread_slow_mode (docs/04 "audit_log": "A service writes it from its own write transaction"). internal/audit stays the only writer, and TestOneWayIntoAuditLog passes.
- **A write over no row is a change.** Setting 0, or the site default, on a thread with no row writes the row, with "from" null. It pins the thread against a later change of the default. Only repeating the row's own value is conflict. docs/07 "Slow mode" says so.
- **The route sits behind the staff gate, as lock and pin do.** A member whose group grants moderate in the forum is exempt from slow mode (canModerate) but cannot set it, the same asymmetry lock has. The moderates gate would admit them, and that is a product change, so this PR does not make it.
- **The archived check reads the thread cache before the write.** It does not hold the thread through the DB write, so an archive that lands in between leaves a slow mode on a thread nobody can post in. That is harmless, and the comment on SetSlowMode records it.
- **web/client/src/api/dto.ts and dto-contract.ts carry the new types.** internal/dto's golden tests require every new field populated and every type golden'd, and the client contract test and tsc hold dto.ts to the goldens. The orchestrator approved type-only edits on condition that dist stays byte-identical. go generate ./internal/web/spa left it byte-identical.
- **The migration is 0016.** #186 (handle changes) also adds 0016, and whichever merges second renumbers (migrate.go).

**agent:claude-code/phase2-run-crj50** at 2026-09-30T02:02:26Z

Supersedes the migration number in the previous note: #186 merged first with 0016_handle_changes, so this ticket's migration is 0017_thread_slow_mode. The branch had not been pushed, so it was rebased onto c24635c before its first push.

## Summary

Server half landed. Migration 0017 adds thread_slow_mode, which is authoritative with no foreign key to threads. PUT /api/v1/admin/threads/{id}/slow-mode {seconds} sits behind the staff gate and is moderate-checked. It writes the row and its thread.slow_mode audit row (from, to) in one transaction through internal/audit. Reply reads the thread's value or the site default. The thread page's viewer block gains slow_mode_seconds and next_post_at, and the cached posts are untouched. Criterion 3's composer half is TKT-01M3R0PM0KPX323DJ130WWKE2J (draft).
