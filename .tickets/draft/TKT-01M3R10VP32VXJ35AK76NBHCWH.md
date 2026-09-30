---
schema: 3
id: TKT-01M3R10VP32VXJ35AK76NBHCWH
title: Take the per-post flag lock in a suspension's bulk hide
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29CRJ4E4AZ079KEHVGJ01DS
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:06:43Z
updated_at: 2026-09-30T02:06:47Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The moderator queue (TKT-01M29CRJ4E, PR #184) made the thread page's single-post Delete and Restore take the striped per-post flag lock, which the auto-hide, the moderator restore and ConfirmHide already share (TKT-01M29CP2PJ). A suspension's bulk hide (`/admin/users/{handle}/suspend` with hide_posts, and `/posts/hide`, TKT-01M29CP2QK) still appends its Deletes without taking that lock. The queue re-reads the post before it writes, which narrows the race to the write itself but does not close it.

The race: a moderator's restore from the queue and a suspension's hide of the same post can interleave, so the post ends in whichever state was appended last, and the flag rows may not match it (a restore dismisses the post's open flags before its append).

### What to decide

Whether the bulk hide takes the per-post lock for each post it hides (cost: one lock per post in a loop that may touch many threads), or whether the restore's append precondition already makes the outcome safe and only a test is missing. Read the flag lock in internal/service/flags and the hide loop in the suspension service before choosing.

Reported by the moderator queue lane on 2026-09-30.
