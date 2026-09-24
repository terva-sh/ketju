---
schema: 3
id: TKT-01M29CA99RTPEA1B3HG24B2KNJ
title: Implement GET /me
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:api-resources
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T05:06:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

The viewer envelope: user, role, unread counts, pending notification count, and settings. The client bootstraps from it, and the SPA shell inlines its response in a `<script type="application/json">` block to save a round trip on first load.

That inlining is why the response has to be cheap and free of anything expensive to compute. An unread count that costs a table scan turns every page load into one.

## Acceptance criteria

- [ ] Returns the documented shape for an anonymous viewer, a member, a moderator, and a pre-user session
- [ ] Counts come from projections rather than being computed from logs
- [ ] The handler issues at most 2 SQL queries per request, asserted by a test that counts them, so it can be inlined on every shell render

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough, with a stated query budget' did not state the budget. It is now at most 2 queries, one for the viewer and session and one for the projected counts, asserted by a test that counts queries.
