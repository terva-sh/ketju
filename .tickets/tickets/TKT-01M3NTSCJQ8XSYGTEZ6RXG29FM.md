---
schema: 3
id: TKT-01M3NTSCJQ8XSYGTEZ6RXG29FM
title: Drive Phase 2 to its exit gate autonomously
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-orchestrator
  branch: run/phase2-promote
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-438e26c1
  commit: 5fe97ee57b566857e001d7195a9b4a6d1adbb476
  session: null
  claimed_at: 2026-09-29T07:18:23Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-29T15:28:19Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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

## Notes

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T07:18:23Z

Run started 2026-09-29 by the orchestrator (agent:claude-code/phase2-run-orchestrator) under docs/13, which merged in PR #141. The 36 groomed Phase 2 tickets with no open decision were promoted to ready. Six more are still being planned on groom/phase2-plans-late: TKT-01M3NRP3W9, 3NRX2NN, 3NS16VH, 3NS1931, 3NTSCMN, and the spike 3NS147C, which needs a maintainer decision. CKRFS, CKRF9, CKRES, CKRGS, CRJ5J and CKRH9 were linked to depend on the admin scaffold TKT-01M3NRYCP5, because they write audit rows through its internal/audit helper. There are two lanes, server and client. Only one open PR at a time may change the committed client dist.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T15:28:04Z

Merges so far, written by the orchestrator in one batch:
- TKT-01M29CKRDQ (Add the applications and forms schema): PR #144 at 2897ac0. Migration 0011 adds forms, form_versions, applications, application_notes, identity blocks and notifications.application_id, and seeds the registration form. Review 1373 had two mediums, both fixed, and run 3efcb1fc was clean.
- TKT-01M3NRYCP5 (Add the admin API gate, audit helper and admin screen scaffold): PR #145 at 889870b. It adds internal/audit, the only writer of audit_log (TestOneWayIntoAuditLog enforces it); the admin, staff and moderates gates with adminRoute; perm.ModeratesAny; and the admin layout. Review 1374 had two mediums, both fixed, and run 881 was clean. **AC4 is unticked:** the reader bundle grew 116 B gzip, to 38287 of 60000, for the viewer export the admin chunk needs. This goes to the maintainer to accept or reword.
- TKT-01M29CKRHS (Implement author edit and delete windows with grace edits): PR #146 at ccc05ad. It adds the windows, grace recorded in the log, and the edit-window-closed and delete-window-closed codes. authorize() takes (a, e, action, now, w). The orchestrator approved the small client change. Review 1378 had one medium, fixed, and run 821923a1 was clean. Draft TKT-01M3PTT97X was filed for the client controls.
- Grooming PR #143 merged at 4f38f14 with the late plans and the title-redaction decision. Two round-5 mediums were carried into TKT-01M3NS1931.
Main was green after each merge.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T15:28:19Z

Promoted to ready after #143 merged: TKT-01M3NRP3W9, 3NRX2NN, 3NS16VH, 3NS1931 and 3NTSCMN. TKT-01M3P0CYR3 (admin rename control) and TKT-01M3PTT97X (client edit and delete windows) were filed during the run and have no groomed plan, so under docs/13 they stay draft until one is written.
