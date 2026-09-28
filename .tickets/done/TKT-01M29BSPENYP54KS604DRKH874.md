---
schema: 3
id: TKT-01M29BSPENYP54KS604DRKH874
title: Decide the handle change policy
type: spike
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:S
  - decision
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-identity
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-28T21:42:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Question 9 in docs/10. Never, once, or freely with redirects.

The lean is allowed with a 30-day cooldown and old handles reserved for 90 days so an abandoned handle cannot be grabbed to impersonate. Mentions are stored by user id, so history stays correct across a change either way, which is what makes this a policy question rather than a data question.

Decide before the user settings screens are built, because "change handle" is either on that screen or it is not.

## Acceptance criteria

- [x] The policy is recorded in docs/05, including the cooldown and reservation periods if any
- [x] The decision names what happens to a URL of the form /u/{old-handle} after a change

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:32:45Z

Maintainer decision, 2026-09-28, in Phase 2 grooming (TKT-01M3M9RR26): "Allowed: 30-day cooldown". Written into docs/05 "Changing a handle" and docs/10 item 9. It also settles the grandfathered handles ending in '-' from TKT-01M3ANX1NF: an admin renames them without the cooldown.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:42:54Z

Supersedes the decision note's '301': the redirect from /u/{old} and GET /users/{old} during the 90-day reservation is a temporary 302 with Cache-Control: no-store. A cached 301 would keep sending visitors to the former owner after the handle is free and belongs to someone else. Changed on PR #133 after Terva review 1255 (high); docs/05 and docs/10 item 9 now say 302.

## Summary

Members may change their handle once per 30 days. The old handle is reserved for 90 days, and /u/{old} and GET /users/{old} answer a 301 to the new handle during that time, then not-found. Admins may rename anyone without the cooldown. Every change is audited, and anonymized accounts cannot be renamed. History is unaffected, because mentions and posts name the member by user id.
