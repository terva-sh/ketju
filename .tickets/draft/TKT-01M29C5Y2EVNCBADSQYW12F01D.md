---
schema: 3
id: TKT-01M29C5Y2EVNCBADSQYW12F01D
title: Implement permission evaluation with a per-session cache
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
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

Resolve a viewer's permissions for a forum as the union of their groups' rows, falling back to global rows, cached per session for about 60 seconds as docs/02 specifies.

Every read and every write in the system passes through this, so it is both the hottest path and the one where a bug is a security bug rather than a glitch. An admin is implicitly a member of every group, which is a rule to implement once here rather than to check at each call site.

The 60-second cache means a permission change takes up to a minute to apply. That is a deliberate trade and it needs saying in the admin UI, not just in a comment.

## Acceptance criteria

- [ ] Evaluation is the union of group rows for the forum, falling back to global rows, with table-driven tests for each permission
- [ ] An admin passes every permission check without needing explicit group rows
- [ ] A restricted forum is invisible rather than forbidden to a viewer without read, and a test covers the difference
- [ ] Cached decisions live at most 30 s and any group or permission write invalidates the cache at once; a test shows a revoked permission denied on the next request

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'A bounded lifetime' became a 30 s TTL plus immediate invalidation on group or permission writes. The TTL is a backstop; invalidation is what makes a revocation take effect at once.
