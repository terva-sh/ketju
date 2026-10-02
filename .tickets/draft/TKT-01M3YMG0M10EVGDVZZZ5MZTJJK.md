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
updated_at: 2026-10-02T15:42:30Z
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
