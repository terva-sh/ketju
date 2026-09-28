---
schema: 3
id: TKT-01M3K5CEV00F4RVT937Y1WE615
title: Run Terva reviews from the v0.5.0 reviewer image
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
  actor: agent:claude/t3code-45332409
  branch: ci/review-v0.5.0
  worktree: /home/sothr/.local/state/agent-rollout/terva-review-v0.5.0/ketju
  commit: ba749bb7301a6ec1ec75dc28edcda4343b284eb6
  session: null
  claimed_at: 2026-09-28T04:46:46Z
  expires_at: null
archive: null
created_at: 2026-09-28T04:46:46Z
updated_at: 2026-09-28T04:46:46Z
created_by:
  id: agent:claude/t3code-45332409
  name: ""
updated_by:
  id: agent:claude/t3code-45332409
  name: ""
extensions: {}
---

## Description

Move this repository's Terva review from the terva-action-code-review v0.3.0 image to v0.5.0 (https://git.local.sothr.com/terva-sh/terva-action-code-review/releases/tag/v0.5.0), pinned by digest `sha256:64a7ba59ca8a0932e2b4d4cf8e50a2231f46fa6eaefa046acbef84f2a2886252`.

### What it gains

- **Up to 1 MiB of context,** not a fixed 256 KiB, so a large PR is reviewed rather than stopped at `context_limit`.
- **A change index** in every prompt.
- **Fallback models** from `TERVA_REVIEW_FALLBACKS`, now read by the workflow.
- **Decided findings** reach the reviewer.
- **A carry of every gate** in one dispatch.

Every profile digest and request key moves, so the first review of each open PR after this merges runs fresh. The allowlist, the concurrency group and the other settings stay as they are.

Part of terva-action-code-review TKT-01M3K5B2YX ('Move the consumers to the v0.5.0 image').

## Acceptance criteria

- [ ] The review workflow runs the v0.5.0 image by digest and reads TERVA_REVIEW_FALLBACKS
- [ ] A review of this change from its branch is recorded
