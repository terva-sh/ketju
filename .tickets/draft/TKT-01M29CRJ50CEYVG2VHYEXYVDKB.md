---
schema: 3
id: TKT-01M29CRJ50CEYVG2VHYEXYVDKB
title: Implement per-thread slow mode
type: task
status: draft
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
updated_at: 2026-09-29T05:07:28Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

A minimum number of seconds between posts per user in a thread, set by a moderator. Stored in the database rather than the log, because it is a live moderation control rather than part of the thread's history.

Slow mode is the tool for a thread that is heating up, and it works by making people wait rather than by removing anything. Moderators are exempt.

## Acceptance criteria

- [ ] A reply inside the thread's interval answers rate-limited with Retry-After equal to the whole seconds left, rounded up, and appends nothing
- [ ] Moderators are exempt and agent accounts are not
- [ ] The interval is per thread in thread_slow_mode, overrides slow_mode.default_seconds (an explicit 0 included), survives a restart and ketju reindex, and the composer shows it from viewer.slow_mode_seconds
- [ ] Setting 0 lets the next reply through at once
- [ ] Setting slow mode is forbidden without moderate, and each change writes one audit_log row with from and to
- [ ] Setting slow mode leaves the page's posts under the same response-cache key while the viewer block reflects it (TestSlowModeIsInTheViewerBlockNotTheCache)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Slow mode lives in the database, not in the log** (docs/08 "Primitives": "`settings` per thread in DB, not log"). It is a live control rather than part of the thread's history, so no record type is involved. The frozen format is untouched, and slow mode is not recoverable from a log. That is intended.
- **Off by default; moderators are exempt; agents are not** (docs/08 "Primitives" and "Rate limits": "Slow mode applies to agents as it does to anyone").
- **The check exists.** `slowMode` in `internal/service/threads/create.go` refuses a reply less than N seconds after the author's newest post in the thread, read from the index. It answers `rate-limited` with `Retry-After` rounded up to whole seconds. Today N is the site-wide `slow_mode.default_seconds` (TKT-01M29P9RS0). The settings comment in `internal/settings/settings.go` names this ticket for the per-thread value.
- **A per-thread value overrides the site default**, including an explicit 0, which turns a thread off under a non-zero default. With no row, the site default applies.
- **"Naming the remaining wait"** is the `Retry-After` header. Problem bodies never interpolate (docs/07 "Error catalogue").
- **Response cache.** Slow mode is per thread, and the wait is per viewer, so both go in the thread page's viewer block. `View` builds that block on every request and never caches it (docs/07 "The response cache"). The cached posts do not change.

### Approach

1. **Migration** (the next free number): `thread_slow_mode (thread_id INTEGER PRIMARY KEY, seconds INTEGER NOT NULL CHECK (seconds >= 0 AND seconds <= 86400), set_by INTEGER REFERENCES users (id), set_at INTEGER NOT NULL)`. It is authoritative, not a projection: no `PROJECTION` marker, and not in `db.ProjectionTables`, so `ketju reindex` keeps it. `TestProjectionTablesAreMarked` holds that. Queries in `queries/moderation.sql`: `GetThreadSlowMode` and `PutThreadSlowMode` (upsert). Regenerate dbq. The upper bound of a day is an engineering guard against a typo, not a policy.
2. **Service** in `internal/service/threads/moderate.go`: `SetSlowMode(ctx, v, id, seconds int)`. It requires `canModerate` (`forbidden` otherwise) and a thread that is not archived (`conflict`). Seconds outside 0 to 86400 is `validation-failed`. In one `db.Write` it upserts the row and appends an audit row, `thread.slow_mode`, `target_type` `thread`, details `{from, to}`, where `from` is the previous row's value or null. Setting the value the thread already has is `conflict`. There is no log append, so TKT-01M29CKRGS's helper is not used. The pattern is `settings.SetBy`'s.
3. **Reply**: `effectiveSlowMode(ctx, id)` reads the row, or `settings.SlowModeDefaultSeconds` when there is none. It replaces the bare setting read in `Reply`. It is read on every reply, so turning slow mode off applies to the next request. Moderators stay exempt through `a.canModerate`, as now.
4. **Viewer block**: `dto.ThreadViewer` gains `slow_mode_seconds int` (0 when off) and `next_post_at *Millis`, the viewer's newest post in the thread plus the interval, or null when they may post now or are exempt. Both are computed in `View` beside `can_reply`. Update the dto golden.
5. **Route**: `PUT /api/v1/admin/threads/{id}/slow-mode` with `{seconds}`, answering 200 `{seconds}` with `no-store`, charged to the write limiter.
6. **Client**: `Composer.tsx` shows "Slow mode: one post every N seconds" when `slow_mode_seconds > 0`. While `next_post_at` is in the future it disables Send with a countdown, and it still handles a `rate-limited` answer by showing `Retry-After`. The thread's moderator actions (TKT-01M29CKRGS's dialog pattern) gain "Slow mode…" with a seconds field. Regenerate `dist/`.
7. **Docs**: docs/04 gets the table under the authoritative tables. docs/07 gets the route, and the `POST /threads/{id}/posts` row names the per-thread value. docs/08 needs no change.

### Tests

- `TestSlowModePerThread`: with a fixed clock and the thread at 30 s, a member's second reply 10 s after the first is `rate-limited` with `Retry-After: 20`. At 30 s it succeeds. An agent account is refused the same way, and a moderator is not.
- `TestSlowModeOverridesDefault`: site default 60 and thread row 0 lets the member reply at once. No row falls back to 60.
- `TestSlowModeOffIsImmediate`: set 30, reply, set 0, and reply again at once succeeds.
- `TestSlowModeSurvivesRestartAndReindex`: the row is still there after reopening the database and after `ketju reindex`.
- `TestSetSlowModeRefusals`: a member is `forbidden`, an archived thread is `conflict`, 86401 is `validation-failed`, and an unchanged value is `conflict`. The audit row names the actor with from and to.
- **Response cache invariant**, `TestSlowModeIsInTheViewerBlockNotTheCache`: with the cache on, a page is cached. Setting slow mode leaves the posts' cache key unchanged (a hit), while the same response's `viewer.slow_mode_seconds` and `next_post_at` reflect the new value.
- Client: the composer notice and the disabled Send with a countdown, under fake timers.

### Out of scope

- Per-agent token buckets: TKT-01M29P9RSW.
- The site-default setting's admin screen: TKT-01M29CXK2H.
