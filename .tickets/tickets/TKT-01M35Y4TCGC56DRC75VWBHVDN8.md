---
schema: 3
id: TKT-01M35Y4TCGC56DRC75VWBHVDN8
title: Read Terva review settings from organization variables
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude/terva-migration-b
  branch: t3code/terva-review-org-variables
  worktree: /tmp/terva-migration/ketju
  commit: 93df292b6ec51d9a6cb81f197325688ed94e5233
  session: null
  claimed_at: 2026-09-23T01:30:14Z
  expires_at: null
archive: null
created_at: 2026-09-23T01:30:08Z
updated_at: 2026-09-23T01:30:14Z
created_by:
  id: agent:claude/terva-migration-b
  name: ""
updated_by:
  id: agent:claude/terva-migration-b
  name: ""
extensions: {}
---

## Description

terva-action-code-review merged PR 41 as 7090fc19699fda481d9138f0dcf73f80ee8cab06, which removed the action's defaults for provider, model and thinking: a run missing one now fails with missing_provider, missing_model or missing_thinking (missing_provider_url for openai-compatible without a base URL). terva-sh decides them once, as the organization Actions variables TERVA_REVIEW_PROVIDER, TERVA_REVIEW_BASE_URL, TERVA_REVIEW_MODEL and TERVA_REVIEW_THINKING.

Move the reviewer pin in .forgejo/workflows/terva-review.yml to 7090fc1, install Terva 0.138.2 (the version that pin was built and tested against) by URL and SHA-256, and replace the hardcoded openai-compatible / CPA API / gpt-5.6-sol / low with the four organization variables. Update docs/pr-reviews.md, which named the old pin, Terva 0.137.0 and gpt-5.6-sol at low as the review configuration.

## Acceptance criteria

- [ ] Workflow pins 7090fc1, installs Terva 0.138.2 by SHA-256, and reads provider, base URL, model and thinking from organization variables
- [ ] docs/pr-reviews.md names the new pin, Terva 0.138.2 and the organization variables
- [ ] The PR's own Terva review runs with gpt-6-sol at medium thinking and every finding is dispositioned

## Implementation plan

Follow the action's docs/installation.md ("Organization settings", "Adopting a new pin") at 7090fc1 and its examples/forgejo-review.yml. Change only the checkout ref, the Terva URL and checksum, and the four settings in the workflow; leave triggers, concurrency, allowlist, profile, publication policy and fail severity as they are. Rewrite the "Trusted configuration" paragraph of docs/pr-reviews.md to name the variables and link the action's guide for their current values rather than copying them, so the doc does not drift when the organization changes model. Open the PR, dispatch the review from the PR branch so the new workflow runs, confirm it reports gpt-6-sol at medium, and disposition every finding here.

Alternatives: keeping the hardcoded values at the new pin would work but would override the organization's choice and drift from every other consumer; copying the organization's current values into docs/pr-reviews.md would go stale on the next variable change.
