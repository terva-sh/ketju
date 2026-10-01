---
schema: 3
id: TKT-01M3V73KM1VJ5XTC53MFCEJ0XW
title: Bound how much one member may upload
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:50:48Z
updated_at: 2026-10-01T07:50:48Z
created_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
extensions: {}
---

## Description

`limits.attachment_mb` bounds one upload, and the per-address write limiter bounds how often a client may send one. Nothing bounds the total a member uploads. A member granted `upload` can fill the disk with files they never attach until the collector deletes them seven days later (docs/03 "Blob garbage collection").

`upload` is granted by an admin and is not seeded for members (docs/07 "Attachments", decided by the run 2026-10-01), so the exposure is limited to people an admin chose. A per-member byte budget over a rolling window, or a cap on unattached bytes per member, would close it.

Found while grooming TKT-01M29CXK0K (Build the content-addressed blob store and upload path). No Phase 3 exit criterion needs it, so it has no milestone (docs/14 "Scope").
