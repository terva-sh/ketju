---
schema: 3
id: TKT-01M3NRP3W9XSXYMSPNYS7JGYF4
title: Implement handle changes with cooldown, reservation, and redirect
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29CKRE9F0WR2JH8QEMZ7G5B
blocks_on: none
references:
  - ref: design:identity-handle-change
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-29T05:02:34Z
updated_at: 2026-09-29T05:38:53Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

docs/05 "Changing a handle" records the policy the maintainer decided on 2026-09-28 (docs/10 item 9, TKT-01M29BSPEN). No ticket builds it yet. This one does.

- A member may change their own handle once every 30 days. The new handle passes registration's checks (`identity.CheckHandle`) and must be free in any case.
- The old handle is reserved for 90 days so nobody can take it to impersonate the member. During that time `/u/{old}` and `GET /api/v1/users/{old}` answer 302 to the new handle with `Cache-Control: no-store`. The redirect is temporary on purpose, because a cached 301 would keep sending visitors to the former owner after the handle is free again. After 90 days the old handle answers `not-found` and is free.
- Mentions, posts and quotes name members by user id, so history does not change. Posts show the current handle.
- An admin may rename any member without the cooldown. That is how grandfathered handles ending in `-` get fixed (TKT-01M3ANX1NF). The old handle is reserved and redirected just the same.
- Every change writes an audit row with the old and the new handle.
- An anonymized account (`deleted-user-<id>`) cannot be renamed.

Filed during Phase 2 grooming (TKT-01M3M9RR26) from the plan for TKT-01M29CKRFS "Implement the account lifecycle: deactivate, suspend, anonymize".

### Notes for the plan

- Registration (`identity.Register`), the auto_approve handle skip (TKT-01M29CKRE9), and rename must share one "is this handle free" check. TKT-01M29CKRE9 extracts it as `identity.handleFree(ctx, q, h)`. This ticket adds the reservation lookup there, so a reserved handle counts as taken on all three paths.
- A reservation table (the old handle `COLLATE NOCASE`, the user it redirects to, and an expiry) needs a migration, plus a `users` column or an audit query for the 30-day cooldown. An anonymized account's old handle is also reserved for 90 days, with no redirect (maintainer decision 2026-09-29, recorded on TKT-01M29CKRFS). The table therefore needs a row with no redirect target, and a reservation must not name the anonymized user. Audit rows from renames keep the old and new handle after anonymization, and docs/05 says so (maintainer decision 2026-09-29).
- A rename changes the author rows, so the response cache's authors digest and the render cache's resolved-mention check both see it (docs/07 "The response cache"). Name tests that prove a cached page is not served with the old handle.
- The member route and the admin route are new API surface. Record them in docs/07 in the same PR.

## Acceptance criteria

- [ ] A member can change their own handle at most once per 30 days; a second change inside the window is refused with the date it opens, and an admin rename is not limited
- [ ] A new handle must pass identity.CheckHandle and be free in any case, and a handle reserved by an earlier rename counts as taken for rename, registration and the auto_approve handle skip
- [ ] For 90 days /u/{old} and GET /api/v1/users/{old} answer 302 to the new handle with Cache-Control: no-store; afterwards the old handle is not-found and can be registered
- [ ] Every rename writes an audit_log row with the old and new handle in the transaction that renames
- [ ] An anonymized account cannot be renamed, by its owner or by an admin
- [ ] A cached thread page, profile or mention is not served with the old handle after a rename (response and render cache tests)
- [ ] Anonymizing an account reserves its old handle for 90 days with no redirect, and the reservation row names no user

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:53Z

Maintainer decisions 2026-09-29, from TKT-01M29CKRFS: an anonymized account's old handle is reserved for 90 days with no redirect, and audit rows keep old handles after anonymization, which docs/05 will document. The description and a new criterion reflect the first.
