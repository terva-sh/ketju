---
schema: 3
id: TKT-01M2XDHZ0Y10ASD7FWFCR83BS9
title: Install targeted Terva PR reviews
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - workflow
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/org-review-rollout
  branch: ci/targeted-terva-reviews
  worktree: /tmp/terva-org-rollout-8ghy0wps/ketju
  commit: d5d553dc3ccf5c188b3d0e4e96ac551953aa512d
  session: null
  claimed_at: 2026-09-19T18:06:17Z
  expires_at: null
archive: null
created_at: 2026-09-19T18:06:17Z
updated_at: 2026-09-19T18:08:44Z
created_by:
  id: agent:codex/org-review-rollout
  name: ""
updated_by:
  id: agent:codex/org-review-rollout
  name: ""
extensions: {}
---

## Description

User-approved rollout to ketju. Install explicit manual/comment reviews with CPA gpt-5.6-sol low and advisory dispositions. Keep existing build/release gates and secrets intact. Merge requires separate user authorization.

## Acceptance criteria

- [x] Pinned workflow and agent guidance are validated
- [ ] Live review evidence is recorded on the installation PR
- [ ] Installation is merged and comment requests are available

## Implementation plan

Copy the verified git-ticket installation with full reviewer/helper pins and runtime checksum. Add local request and advisory-disposition instructions. Retain the existing workflow directory when CI uses .github. Validate YAML, shell snippets, links and ticket records; inspect one explicit live installation review where enabled. Reject review-on-every-push because it repeats low-value feedback. Keep required-check settings unchanged.

## Notes

**agent:codex/org-review-rollout** at 2026-09-19T18:08:44Z

Configuration validation: actionlint 1.7.7 passed (custom docker runner accepted; external shellcheck disabled). git diff --check and git ticket check --strict passed. Workflow derives from the deployed git-ticket PR #208 installation; no consumer code is checked out. Live validation and activation remain pending.
