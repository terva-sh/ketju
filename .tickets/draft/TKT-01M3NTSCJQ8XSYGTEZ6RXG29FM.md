---
schema: 3
id: TKT-01M3NTSCJQ8XSYGTEZ6RXG29FM
title: Drive Phase 2 to its exit gate autonomously
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M3NTSCMN7YYA0M8TMMQX6H6E
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-29T05:39:33Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Drive the `phase-2` milestone to its exit gate under docs/13 (The Autonomous Phase 2 Run), which amends docs/12.

### The grant, in the maintainer's words (2026-09-29)

"Let's pickup what you recommend next and continue our work through phase 2 autonomously merging after reviews are complete and issues resolved or disagreements recorded."

docs/13 "The grant" says what this covers. The run merges on the docs/12 gate. It records a disagreement with a finding, rather than stopping for one, except for security findings, which are fixed or block the ticket. It may promote a groomed `phase-2` draft that has no open decision.

### Wave order

1. Foundations: the admin scaffold (TKT-01M3NRYCP5), the applications schema (TKT-01M29CKRDQ), pre-user sessions (TKT-01M29CKRG9), the author windows (TKT-01M29CKRHS), lock and pin (TKT-01M29CKRGS), and the archived-forum permission (TKT-01M3N24RHD).
2. Identity: providers, linking, group mapping, the lifecycle, and handle changes.
3. Applications: the form schema, submit, review, notifications, and screens.
4. Moderation: moderator edit and delete, tombstones, flags, rename and move, history, slow mode, the queue, and suspension.
5. Admin screens and agent accounts.
6. Follow-ups: redaction, fsck, Playwright, drafts, mark-unread, and remote images.
7. The exit gate.

`git ticket ready` and the dependency links decide the actual order. This list is only the intent.

One note per merged PR goes here, written by the orchestrator in batches.

## Acceptance criteria

- [ ] The Phase 2 exit ticket is done
- [ ] Every recorded disagreement is listed in the summary for the maintainer
- [ ] docs/11 counts are refreshed when the gate closes
