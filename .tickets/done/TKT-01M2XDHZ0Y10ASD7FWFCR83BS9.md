---
schema: 3
id: TKT-01M2XDHZ0Y10ASD7FWFCR83BS9
title: Install targeted Terva PR reviews
type: chore
status: done
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
claim: null
archive: null
created_at: 2026-09-19T18:06:17Z
updated_at: 2026-09-24T05:02:36Z
created_by:
  id: agent:codex/org-review-rollout
  name: ""
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

User-approved rollout to ketju. Install explicit manual/comment reviews with CPA gpt-5.6-sol low and advisory dispositions. Keep existing build/release gates and secrets intact. Merge requires separate user authorization.

## Acceptance criteria

- [x] Pinned workflow and agent guidance are validated
- [x] Live review evidence is recorded on the installation PR
- [x] Installation is merged and comment requests are available

## Implementation plan

Copy the verified git-ticket installation with full reviewer/helper pins and runtime checksum. Add local request and advisory-disposition instructions. Retain the existing workflow directory when CI uses .github. Validate YAML, shell snippets, links and ticket records; inspect one explicit live installation review where enabled. Reject review-on-every-push because it repeats low-value feedback. Keep required-check settings unchanged.

## Notes

**agent:codex/org-review-rollout** at 2026-09-19T18:08:44Z

Configuration validation: actionlint 1.7.7 passed (custom docker runner accepted; external shellcheck disabled). git diff --check and git ticket check --strict passed. Workflow derives from the deployed git-ticket PR #208 installation; no consumer code is checked out. Live validation and activation remain pending.

**agent:codex/org-review-rollout** at 2026-09-19T18:11:25Z

Installation PR: https://git.local.sothr.com/terva-sh/ketju/pulls/41. Reviewed head d04a8dc9c7d49a4de7464140afafdcca82f5ec51; base d5d553dc3ccf5c188b3d0e4e96ac551953aa512d. Request install-pr-41-initial; trusted candidate workflow at the reviewed head, pinned reviewer c8730fee5650346b17c61babd5da8fdd9982c5d3. Run https://git.local.sothr.com/terva-sh/ketju/actions/runs/87; publication https://git.local.sothr.com/terva-sh/ketju/pulls/41#issuecomment-8527. Inference completed and publication is visible. Accepted review finding-1 (low): always publication requires a workflow change, not just a request ID. Corrected guide wording in this commit. This commit changes only the publication-policy clarification and ticket evidence after the reviewed head. The new head is not claimed as model-reviewed; no new inference is warranted for this minor documentation edit.

**agent:claude/phase1-groom** at 2026-09-24T05:02:36Z

Closing out on behalf of the rollout, as agreed with the user on 2026-09-23 while grooming Phase 1. The installation PR #41 merged into main as ea9c926. Comment-triggered review requests have worked since then: PRs #45 and #46 (TKT-01M35Y4TCG, reading the review settings from organization variables) were reviewed through /terva comments. That satisfies the third criterion. The codex claim was left open after the merge and never released.

## Summary

Targeted Terva reviews are installed. PR #41 merged as ea9c926 and adds the manual and comment-triggered review workflow (.forgejo/workflows/terva-review.yml) and the request and disposition guide (docs/pr-reviews.md). Follow-up work moved the model settings into organization variables (TKT-01M35Y4TCG, PRs #45 and #46). Required-check settings were not changed.
