---
schema: 3
id: TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
title: Implement pre-user sessions restricted to the application flow
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:approval-mode
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-24T11:59:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

In approval mode an authenticated visitor with no user gets a session of kind `pre_user` carrying an identity_id and no user_id. It reaches the application form, the status page, and logout. Nothing else.

The allowlist has to be positive rather than a set of exclusions. A new endpoint added next year must be closed to a pre-user session by default, because the failure mode is a pending applicant posting in a forum.

## Acceptance criteria

- [ ] Endpoint access for pre-user sessions is a positive allowlist, and an endpoint not on it is refused
- [ ] A test enumerates the routes and asserts that only the three documented ones accept a pre-user session
- [ ] A pre-user session becomes a member session only through approval, never by upgrade in place
- [ ] The client renders the status page for this session rather than a broken shell

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T11:59:38Z

From TKT-01M29CA99R (Implement GET /me): GET /me, and the shell's bootstrap block, return the anonymous shape (user null, zero counts) for a pre-user session, because dto.Me makes a nil user the only signed-out signal and no pre-user shape is documented. The viewer does carry the distinction (domain.Viewer.PreUser and IdentityID, set by internal/auth/session). If the approval screens need /me to say 'signed in as an applicant', decide the field in docs/07 and dto.Me; CA99R's AC1 stays unticked on exactly that clause until then.
