---
schema: 3
id: TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
title: Route every session creation through one status-checking function
type: chore
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:identity
  - area:test
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:risks
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T05:21:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Exactly one function in the codebase creates a session row, and it checks that the user's status is active before it does.

This exists as its own ticket because docs/09 names the bug it prevents, which Gitea shipped: an approval-gated instance where one code path created a session without checking approval, and a pending applicant walked straight in. The defense is structural rather than careful review. If there is one door, the check is on the door.

Approval mode makes this sharper, because a pre-user session is a legitimate session for a user that does not exist yet. The function must distinguish "no user, by design" from "user exists but is not active".

## Acceptance criteria

- [ ] Exactly one function constructs a session row, enforced by a test or a lint that fails on a second one
- [ ] The function refuses to create a user session for a status other than active
- [ ] The OIDC callback path is tested explicitly against a pending, a suspended, and a deactivated user
- [ ] Creating a pre-user session is a distinct call that cannot accidentally produce a member session

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add two exported constructors to internal/auth/session, and nothing else:
   - `IssueUser(ctx, tx, u UserStatus, meta Meta) (token string, err error)` refuses unless `u.Status == "active"` and returns `ErrNotActive`, wrapping the status.
   - `IssuePreUser(ctx, tx, identityID int64, meta Meta)` takes no user argument at all, so it cannot write `kind='user'`.
   - Both call the unexported `insert` from TKT-01M29C5Y3E. `insert` is the only caller of the generated sqlc `InsertSession`.
2. `UserStatus` is read inside the same transaction as the insert, with `SELECT status FROM users WHERE id=?`, not passed in from a caller's earlier read. This closes the window between a check and the insert.
3. `IssuePreUser` checks that the identity row exists and `user_id IS NULL`. That is the "no user, by design" case. If a user is attached, it refuses rather than issuing a pre-user session for an existing user.
4. Enforcement test, in two halves, because sqlc is decided (docs/04):
   - The query files contain exactly one statement matching `INSERT INTO sessions` (case-insensitive), the generated `InsertSession`.
   - A go/parser walk of every non-test .go file finds `InsertSession` referenced only from `session.insert`, and finds no hand-written `INSERT INTO sessions` string anywhere else.
5. Map `ErrNotActive` in the identity service (TKT-01M29C7ZZ9) to the status page. It never reaches the problem catalogue as a 500.

### Files
internal/auth/session/issue.go, internal/auth/session/issue_test.go, internal/auth/session/chokepoint_test.go (the AST/SQL scan). Consumes `session.insert` and `Meta{IP, UserAgent, ProviderKey}` from TKT-01M29C5Y3E.

### Tests
- AC1: chokepoint_test.go. Show the test is live by adding a fixture file under testdata containing a second insert and asserting the scanner catches it.
- AC2: table-driven over pending, suspended, and deactivated, each returning `ErrNotActive` with no row written; active succeeds.
- AC3: in TKT-01M29C7ZZ9's callback tests with the fake IdP, add a pending, a suspended, and a deactivated user, and assert no session cookie and an unchanged `sessions` count. docs/05 says approval mode creates no pending user row, but docs/04 still allows `status='pending'`, so keep the case.
- AC4: a pre_user row always has `kind='pre_user'` and `user_id IS NULL`. There is also a compile-level guarantee: IssuePreUser has no user parameter. Add a test that the resulting Viewer has `Member()==false`.

### Design
Reread docs/09 (the Gitea approval-bypass risk row), docs/05 "mode = approval" step 1, and docs/04 "sessions".

### Interactions
Consumed by TKT-01M29C7ZZ9, TKT-01M29C8005, TKT-01M29CKRG9, and TKT-01M29CP2MG. Recommend recording depends-on links from TKT-01M29C7ZZ9 and TKT-01M29C8005 to this ticket; today they depend only on C5Y3E.

### Settled in grooming (2026-09-24)

- Phase 1 has no pending users. The chokepoint refuses any status other than active, and IssuePreUser serves approval mode later.
