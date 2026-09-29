---
schema: 3
id: TKT-01M3Q2V25EXKYCTJZ24PM8RP7T
title: Decide what an approved applicant sees once their pre-user session ends
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T17:19:16Z
updated_at: 2026-09-29T18:18:26Z
created_by:
  id: agent:claude-code/phase2-run-cp2kz
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-cp2kz
  name: ""
extensions: {}
---

## Description

TKT-01M29CP2KZ (Implement application submit, status, and withdraw) is blocked on this question. The implementation sits in PR #154, which is open, and every criterion except AC8 is met there.

AC8 and TKT-01M29CP2MG's settled grooming ("Approval issues no session... the applicant's pre-user session stays as it is and keeps showing the status page, which then says approved with a sign-in link") both assume that an applicant's pre-user session still reaches `GET /api/v1/apply/status` after approval. Merged code rules that out in two places:
- `LookupSession` (TKT-01M29CKRG9) stops resolving a pre-user session once its identity has a user, and docs/05 "mode = approval" step 1 says so.
- `session.Middleware` answers any cookie that does not resolve with `ClearCookie()`. So after approval, the applicant's first request of any kind deletes the cookie.

As a result, the approved applicant is anonymous: `/me` answers the anonymous shape and `/api/v1/apply/status` answers 401. Terva reviews 1425 and 1427 on PR #154 raise this as a medium finding. It concerns the session contract, so under docs/13 it is fixed or the ticket blocks. It cannot be fixed without changing how sessions resolve, which is a security and product decision.

The decision needed from the maintainer is which of these holds:
1. **Keep the merged behaviour.** AC8's clause "GET /apply/status answers approved with sign_in" is dropped or reworded. The approved applicant sees the signed-out shell and signs in again as a member. The client may add a line such as "applied? sign in to see your decision". TKT-01M29CP2MG's settled item and TKT-01M29CP2NG's "approved" status state change to match. PR #154 can then merge as it stands.
2. **Let a retired pre-user session answer the status route alone.** This means a lookup that recognizes a live pre-user session whose identity now has a user, and that is used only by `GET /api/v1/apply/status`. The middleware must not clear such a cookie, and approval must not delete the row (`RevokePreUser`). It needs its own security review.

Whichever is chosen goes into docs/05 step 4, and TKT-01M29CP2KZ is then unblocked.

## Notes

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T18:17:18Z

Maintainer decision 2026-09-29, relayed by the Phase 2 run's coordinator: **option 1, keep the merged behaviour.** An approved applicant's pre-user session retires: `LookupSession` stops resolving it once the identity has a user, and the session middleware clears the cookie. The applicant then sees the signed-out shell, with a hint such as "Applied? Sign in to see your decision". Signing in gives them a member session through `session.IssueUser`, and the approval notification is waiting there (TKT-01M29CP2N0).

Option 2 lost. A retired pre-user session that still answers the status route would need a second kind of session lookup, a middleware that keeps a cookie it cannot resolve, and approval that leaves the row in place (`RevokePreUser`). That is three loosenings of TKT-01M29CKRG9's guarantee, and all they buy is a page the member session and the notification already provide.

Carried into:
- TKT-01M29CP2KZ's AC8, reworded: status answers approved only while the pre-user session still resolves, never after approval;
- docs/05 "mode = approval" step 4, and the docs/07 `/apply` row;
- a dated superseding note on TKT-01M29CP2MG, replacing its settled item "Approval issues no session" as it concerns the status page, and its plan;
- a dated superseding note on TKT-01M29CP2NG, replacing its "approved" status state with the signed-out hint.

## Summary

Decided by the maintainer on 2026-09-29: option 1, keep the merged behaviour. An approved applicant's pre-user session retires (LookupSession and the session middleware, TKT-01M29CKRG9). They see the signed-out shell with a hint like 'Applied? Sign in to see your decision', and signing in gives them a member session where the approval notification waits. Carried into TKT-01M29CP2KZ's AC8, docs/05 'mode = approval' step 4 and the docs/07 /apply row, TKT-01M29CP2MG's plan with a note, and a note on TKT-01M29CP2NG.
