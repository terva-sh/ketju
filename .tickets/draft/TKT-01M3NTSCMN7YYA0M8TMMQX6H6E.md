---
schema: 3
id: TKT-01M3NTSCMN7YYA0M8TMMQX6H6E
title: "Prove Phase 2's exit: approval registration and agent credentials e2e"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
  - TKT-01M29CP2NGWH32C6M718QHB30X
  - TKT-01M29CRJ4E4AZ079KEHVGJ01DS
  - TKT-01M29CP2QKKZSK4PDB40EJHC3P
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-29T05:39:38Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Phase 2's exit criterion (docs/09): "a club can open registration to strangers safely, and an admin can give an agent an account and a credential". This ticket proves it end to end, as TKT-01M29CGBY1 proved Phase 1.

### What the tests must show

- **Approval mode, through the real HTTP stack**, in a Go integration test with a fake OIDC provider:
  - a stranger signs in and gets a pre-user session;
  - that session is refused on every member route, and reaches the apply routes;
  - the stranger submits the form;
  - a moderator approves the application;
  - the applicant signs in again and gets a member session, and can post.
- **A denied applicant** cannot get a member session.
- **A suspended member** loses their session and cannot sign in, and can sign in again after the suspension is lifted.
- **An agent account**: an admin provisions it and issues a credential; the agent posts with its bearer token, and the post is marked machine-authored; after the admin revokes the credential, the agent is refused.
- **Playwright**, at 390 px and 1440 px: the apply screen, the status screen, and the moderator queue approve flow, with axe run on each.

## Acceptance criteria

- [ ] A Go integration test walks a stranger from sign-in through application, approval and first post, and proves a pre-user session is refused on member routes
- [ ] Tests show a denied applicant and a suspended member get no member session
- [ ] A test provisions an agent, posts with its credential, revokes it, and is refused afterwards
- [ ] Playwright covers apply, status and queue approve at 390 and 1440 px with axe
