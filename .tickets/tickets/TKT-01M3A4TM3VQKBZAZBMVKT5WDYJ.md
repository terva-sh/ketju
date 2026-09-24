---
schema: 3
id: TKT-01M3A4TM3VQKBZAZBMVKT5WDYJ
title: Run Terva reviews from the v0.3.0 reviewer image
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/multi-model-20260923
  branch: ci/review-v0.3.0
  worktree: /tmp/mm/audit/clones/ketju
  commit: 60f582f65b71f67c438479286445aeb6869a0996
  session: null
  claimed_at: 2026-09-24T16:43:51Z
  expires_at: null
archive: null
created_at: 2026-09-24T16:43:51Z
updated_at: 2026-09-24T16:43:52Z
created_by:
  id: agent:claude-code/multi-model-20260923
  name: ""
updated_by:
  id: agent:claude-code/multi-model-20260923
  name: ""
extensions: {}
---

## Description

Move .forgejo/workflows/terva-review.yml from the terva-action-code-review composite action at 7090fc1 (Terva 0.138.2 installed in golang:1.27-alpine with an unpinned apk add) to the v0.3.0 reviewer image by digest, sha256:35199d57112ea0048fe713a49c6087094c5bcbbeaa74d53d6dca89d9a8ba9cd7. The job then checks out and installs nothing. v0.3.0 also leaves an error status when a run stops early, keeps the job green for findings (the status is the gate), and can carry a review's verdict over commits that touch only .tickets/ (REVIEW_CARRY_PATHS). The review doc changes to match. Tracked across repositories by terva-action-code-review TKT-01M396TQ0D.

## Acceptance criteria

- [x] The review workflow runs the v0.3.0 image by digest, with carry over .tickets/
- [x] The review doc describes the image, the status as the gate, and carry
- [ ] The PR is reviewed from its own branch
