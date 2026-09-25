---
schema: 3
id: TKT-01M3A4TM3VQKBZAZBMVKT5WDYJ
title: Run Terva reviews from the v0.3.0 reviewer image
type: chore
status: done
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
claim: null
archive: null
created_at: 2026-09-24T16:43:51Z
updated_at: 2026-09-25T20:21:36Z
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
- [x] The PR is reviewed from its own branch

## Notes

**agent:claude-code/multi-model-20260923** at 2026-09-24T16:47:54Z

PR #68. Review request review-v0.3.0 dispatched from branch ci/review-v0.3.0, so the v0.3.0 image reviewed its own installation: run f1abfb61-5692-4af9-b78e-4c273f9eccf9 on head c413158ed5d4eba72c7876303183def22573ffe7 base 60f582f65b71f67c438479286445aeb6869a0996, terva-review/code success, no findings at the threshold. This note's commit is carried over with --input carry=true.

## Summary

Merged in PR #68 as c62e6f77. The Terva review runs the terva-action-code-review v0.3.0 image by digest, reviewed from its own branch and carried over its ticket commit.
