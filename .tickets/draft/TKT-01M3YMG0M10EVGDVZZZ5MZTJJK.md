---
schema: 3
id: TKT-01M3YMG0M10EVGDVZZZ5MZTJJK
title: Drive Phase 4 to done autonomously
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:process
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:42:30Z
updated_at: 2026-10-02T22:47:44Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

Tracks the autonomous Phase 4 run that docs/15 describes.

On 2026-10-02 the maintainer said "Let's land our merges, rebase and then start burning this down". Asked what to include, they chose all four options:
- Phase 1 cleanup;
- the small unplaced tickets;
- a Phase 4 run;
- the profile and account drafts.

On the same day they answered the thread-tag questions (TKT-01M3M4KVN).

The run's procedure is docs/12, as docs/13 and docs/14 changed it, with docs/15's changes on top.

## Acceptance criteria

- [ ] Every phase-4 ticket other than the epics and this one is done or archived, or blocked on a question docs/15 says the maintainer must answer
- [ ] Every run decision, recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [ ] docs/11 counts are refreshed when the run ends

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:44Z

Run log, 2026-10-02 (orchestrator).

### Grooming merged
- #361 tags-archive (0483f8d6), #358 ops (c0997d3d), #360 client-account (e9a30a2e), #362 correctness (f5ed2d89), #359 moving posts (5b1a6da9), #375 exit check (60653e22).
- Maintainer decisions recorded: #363 no unarchive in v1 (2edadd7e); #365 metrics on a separate address, publishing (Forgejo and GitHub releases, scratch image, no signing), no re-auth for handle change or deactivation, avatars (30834b8e); #371 reply as a new thread: optional `continues` on ThreadCreated, the pre-filled quote is the member's own text (b31d0938).
- #364 was closed unmerged: the orchestrator rebased its pushed branch by mistake, and #365 carried it.

### Implementation merged
- TKT-01M3B12ZZ4 tag rules on create, #368 (6d38a742).
- TKT-01M29CEE7S metrics endpoint, #369 (2c7a2af4).
- TKT-01M3YME9DM API layering fence, #367 (cac3ed9d). Ticked TKT-01M29BKBMC criterion 1.
- TKT-01M3V9DWTN shortcut off switch, #370 (d0bb743c); reader 79859 B. Ticked TKT-01M3S3ZRYD criterion 2.
- TKT-01M3VVZE4G unsigned ordinal bounds and a 386 CI step, #366 (75695ad8).
- TKT-01M29CYSCP thread tags, #372 (06db6b6f); one e2e re-run (phone sign-in timeout).
- TKT-01M3YME9KG method-not-allowed, #374 (24f4f3d6); one e2e re-run (phone Unread badge timeout).
- TKT-01M3VRFRFW index entry checks on open and read, #376 (56636cac).

### Filed by lanes
TKT-01M3YSQPZX, TKT-01M3YSQQ33, TKT-01M3YSQQ5X (done), TKT-01M3YSVEDJ, TKT-01M3YSQ4CR, TKT-01M3YT04ST (no milestone), TKT-01M3YTHAZB, TKT-01M3Z50NKE (flaky search catch-up test, no milestone).
