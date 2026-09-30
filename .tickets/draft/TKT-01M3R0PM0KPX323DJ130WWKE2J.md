---
schema: 3
id: TKT-01M3R0PM0KPX323DJ130WWKE2J
title: Show slow mode in the composer and add the moderator's slow mode dialog
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CRJ50CEYVG2VHYEXYVDKB
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:01:08Z
updated_at: 2026-09-30T02:01:08Z
created_by:
  id: agent:claude-code/phase2-run-crj50
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-crj50
  name: ""
extensions: {}
---

## Description

Show slow mode in the thread page's composer, and give moderators a "Slow mode…" action in the thread's moderator actions (web/client). The server side is TKT-01M29CRJ50 (Implement per-thread slow mode). It shipped without UI because the client lane held the committed dist (the docs/13 lane rule). Its plan's step 6 described this work, and the criteria below are the client half of that ticket's third criterion and the client test its plan named.

The contract is in docs/07 "Slow mode" and the `POST /threads/{id}/posts` row. `web/client/src/api/dto.ts` already carries the types.
- `viewer.slow_mode_seconds` on the thread page is the thread's effective interval, 0 for off. Every viewer gets it.
- `viewer.next_post_at` is the Unix ms when this viewer may next post. It is null while they may post, for a moderator, who is exempt, and for an anonymous viewer.
- A reply sent too soon answers `rate-limited` with `Retry-After` in whole seconds, rounded up.
- `PUT /api/v1/admin/threads/{id}/slow-mode` with `{seconds}`, 0 to 86400, answers 200 `SlowMode` `{seconds}`, `no-store`. Setting the value the thread already has is `conflict`, one out of range is `validation-failed`, an archived thread is `conflict`, and a viewer without moderate in the forum is `forbidden`.

### Composer

- When `slow_mode_seconds > 0`, the composer shows "Slow mode: one post every N seconds", with N written as minutes or hours when it divides evenly.
- While `next_post_at` is in the future, Send is disabled with a countdown to it. It re-enables when the countdown ends, without a refetch.
- A `rate-limited` answer (another tab posted, or the clock is skewed) shows the wait from `Retry-After` and keeps the draft.

### Moderator action

The thread's moderator actions (TKT-01M29CKRGS's dialog pattern) gain "Slow mode…". It opens a dialog with a seconds field, or a choice of Off, 10 s, 30 s, 1 min, 5 min and a custom value, prefilled from `slow_mode_seconds`. It sends one PUT and then refetches the page, so the viewer block shows the new value. `conflict` and `validation-failed` are shown beside the field.

Tests: the notice text, the disabled Send and the countdown under fake timers, the `Retry-After` path, and the dialog's request and error display. The thread-view bundle stays within the 60000 B gzip budget, and the regenerated dist is committed.

## Acceptance criteria

- [ ] With slow_mode_seconds > 0 the composer shows 'Slow mode: one post every N seconds', and with 0 it shows nothing (Composer.test.tsx)
- [ ] While viewer.next_post_at is in the future Send is disabled with a countdown that re-enables it at next_post_at without a refetch, under fake timers (Composer.test.tsx)
- [ ] A rate-limited answer shows the wait from Retry-After and keeps the draft (Composer.test.tsx)
- [ ] A moderator's 'Slow mode…' action sends one PUT /api/v1/admin/threads/{id}/slow-mode {seconds}, refetches the page, and shows conflict and validation-failed beside the field; a member does not see it
- [ ] The thread-view bundle stays within the 60000 B gzip budget and the dist from go generate ./internal/web/spa is committed
