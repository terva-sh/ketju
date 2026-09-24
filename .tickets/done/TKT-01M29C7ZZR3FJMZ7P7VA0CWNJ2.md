---
schema: 3
id: TKT-01M29C7ZZR3FJMZ7P7VA0CWNJ2
title: Route every session creation through one status-checking function
type: chore
status: done
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
  - ref: ticket:TKT-01M3AFHGJDQTMQ67CB8832F4AQ
    path: null
  - ref: pr:71
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-24T20:17:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Exactly one function in the codebase creates a session row, and it checks that the user's status is active before it does.

This exists as its own ticket because docs/09 names the bug it prevents, which Gitea shipped: an approval-gated instance where one code path created a session without checking approval, and a pending applicant walked straight in. The defense is structural rather than careful review. If there is one door, the check is on the door.

Approval mode makes this sharper, because a pre-user session is a legitimate session for a user that does not exist yet. The function must distinguish "no user, by design" from "user exists but is not active".

## Acceptance criteria

- [x] Exactly one function constructs a session row, enforced by a test or a lint that fails on a second one
- [x] The function refuses to create a user session for a status other than active
- [ ] The OIDC callback path is tested explicitly against a pending, a suspended, and a deactivated user
- [x] Creating a pre-user session is a distinct call that cannot accidentally produce a member session

## Implementation plan

Revised at claim (2026-09-24), after reading internal/auth/session as it stands.

### Approach
1. `internal/auth/session/issue.go` exports two constructors and nothing else that creates a session.
   - `(*Store).IssueUser(ctx, identityID, Meta) (token, Session, error)` signs in whoever the identity belongs to.
     - It refuses a missing identity with `ErrNoIdentity`.
     - It refuses an identity with no user (an applicant) with `ErrNoUser`.
     - It refuses any status but active with `*NotActiveError{Status}`, which `errors.Is` `ErrNotActive`.
   - `(*Store).IssuePreUser(ctx, identityID, Meta)` has no user parameter and writes only `kind='pre_user'`. It refuses an identity that has a user with `ErrHasUser`: "no user, by design" is the only case it serves.
   - Both take an identity rather than a user or a status. A sign-in is always by an identity (docs/05 "Login flow"), so a caller cannot name one user and be let in as another, and cannot hand in a status it read earlier.
2. `insert` now takes a `decide func(*dbq.Queries) (newSession, error)`. It runs inside the same `DB.Write` transaction as `CreateSession`, so the check and the row are atomic against a concurrent suspension. The new query `IdentityHolder` reads the identity's user_id and that user's status in one statement. A refusal comes back unwrapped and writes nothing.
3. `chokepoint_test.go` walks the module's non-test Go and SQL, with testdata, spikes and the generated dbq package skipped. It fails on:
   - any caller of `CreateSession` other than `session.insert` and `sessiontest.insert`;
   - a Go string literal inserting into sessions, or updating any sessions column but last_used_at;
   - an SQL statement inserting into sessions other than the query named CreateSession in queries/;
   - an UPDATE of sessions setting any column but last_used_at, since setting kind or user_id would turn a pre-user session into a member one;
   - a non-test file importing sessiontest.
4. `sessiontest` stays as the handler tests' way to sign someone in, including a suspended user whose session predates the suspension. It is allowed only because nothing but a test may import it, and the scan enforces that.

### Tests
- **AC1:** `TestOneDoorIntoSessions` checks the module is clean and the callers equal exactly the two allowed. `TestTheScanFindsASecondDoor` checks the scan is live against testdata/chokepoint, which holds a door of every kind, and that a test file doing the same is exempt.
- **AC2:** `TestIssueUserRefusesAnyStatusButActive` covers pending, suspended and deactivated: `ErrNotActive`, the status named, no token, no row. `TestIssueUserSignsInAnActiveUser` covers the success path end to end through Lookup and ListForUser.
- **AC4:** `TestIssuePreUserIsNeverAMember` checks that the viewer is not a member and that an identity with an active or suspended user is refused.
- **AC3:** the OIDC callback does not exist yet (TKT-01M29C7ZZ9). See the notes.

### Design
docs/09 (Gitea approval-bypass row); docs/05 "Login flow" and "mode = approval" step 1; docs/04 "sessions".

### Interactions
Consumed by TKT-01M29C7ZZ9, TKT-01M29C8005, TKT-01M29CKRG9 and TKT-01M29CP2MG, all of which already depend on this ticket.

### Settled in grooming (2026-09-24)

- Phase 1 has no pending users. The chokepoint refuses any status other than active, and IssuePreUser serves approval mode later.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:51:44Z

Built and verified (2026-09-24).

- `go test ./internal/auth/... ./internal/store/... ./internal/web/...` passes.
- 20 mutants over issue.go, insert and the scanner are all killed. A 21st, setting UserID on the pre-user row, is equivalent: it is always 0 there, because the check just refused a valid one.
  - Two scanner survivors on the first pass each got a fixture: an insert in a migration borrowing the name CreateSession, and an insert before a query file's first `-- name:` marker.

**AC3 is left unticked.** The OIDC callback it names is built by TKT-01M29C7ZZ9, which depends on this ticket, so there is no callback to test yet.
- C7ZZ9's own AC4 covers suspended and deactivated users.
- The pending case is filed as draft TKT-01M3AFHGJD, "Test the OIDC callback against a pending user", which depends on C7ZZ9.
- A note on C7ZZ9 says how the refusals map.

**Decisions:**
- **The constructors take an identity id, not a user and a status as the pre-groom plan had.** The status read had to move inside the insert's transaction anyway, and passing a user id would let a caller name the wrong user. Every docs/05 path that creates a session has an identity in hand.
- **sessiontest is allowlisted rather than removed.** Routing it through IssueUser would take away the handler tests' way to hold a session for a user suspended after sign-in, which me_test.go's "suspended admin" case needs. The import guard confines it to tests.
- **The scan also refuses updates that could promote a session**, not only inserts. Changing kind or user_id on a pre-user row would be a door with no INSERT in it.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T19:59:48Z

Review 499 (head 8c2b917): one high and two medium findings, all accepted and fixed in chokepoint_test.go.

1. **(high) Callers of insert were not guarded.**
   - insert writes whatever its decide function returns, so a third caller in the session package would have been an unchecked door without adding a CreateSession caller.
   - The scan now records every `.insert` selector in internal/auth/session and requires exactly issue.go:IssueUser and issue.go:IssuePreUser.
   - Fixture: testdata/chokepoint/internal/auth/session/backdoor.go.
2. **(medium) Any query named CreateSession was exempt.**
   - The exemption is now the first statement named CreateSession in internal/store/db/queries/identity.sql. It is used up whether or not it passes, and it may hold one insert.
   - Fixture: a copy of identity.sql whose first CreateSession smuggles a second insert, and whose second CreateSession borrows the name. Both are reported.
   - Found while doing this: my first version let a later CreateSession take the exemption when the first one failed. Mutation testing showed it; it is fixed and covered.
3. **(medium) Schema-qualified names evaded both patterns.**
   - The table pattern now accepts a schema prefix and "", backtick or [] quoting on either part, and requires a word boundary after `sessions`, so `sessions_archive` is not the table.
   - Fixtures: `main.sessions`, `"main"."sessions"`, `[main].[sessions]`, and `sessions_archive` as a non-door.

All mutants over issue.go, insert and the scanner are killed (26, with the equivalent one excluded).

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T20:06:57Z

Review 500 (head d9063c5) marked all three of review 499's findings resolved, and raised two new medium findings. Both are accepted and fixed in chokepoint_test.go.

1. **SQL assembled from several Go strings evaded the per-literal check.**
   - The scan now folds a `+` expression built from literals, parentheses, and identifiers declared in the same file, and checks it whole and once.
   - A variable counts as its initial value. This is the conservative choice: a false door is a question in review, while a missed one is the bug.
   - SQL built at run time (fmt.Sprintf, a computed value) is beyond any source scan, and the test's doc now says so: the scan guards against the accidental and the obvious, and review remains the guard against the deliberate.
   - Fixtures: a constant table name, a nested parenthesised concatenation, a variable, and two strings compared rather than joined (not a door).
2. **An aliased UPDATE evaded the promotion guard.**
   - The update pattern now allows `AS alias` or a bare alias between the table and SET.
   - Fixtures: aliased Go and SQL updates of user_id and kind, and an aliased touch of last_used_at, which is not a door.

All mutants over issue.go, insert and the scanner are killed (34 run). Along the way I dropped the redundant string-kind check, since strconv.Unquote refuses non-strings anyway.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T20:17:35Z

Review 501 (head b8cf2f2) marked both of review 500's findings resolved, and raised two new medium findings. Both are accepted and fixed in chokepoint_test.go.

1. **A WHERE nested in the SET clause ended the clause early.**
   - The update regex now finds only `UPDATE <sessions> [AS alias] SET`. `setClauses` reads the assignments at the top level, where parentheses and '', "" and backtick quotes hide their contents. The clause ends at a top-level WHERE, FROM, RETURNING, semicolon or closing parenthesis, matched as whole words.
   - The column-list form `SET (a, b) = (...)` counts every column it names.
   - Fixtures:
     - the reviewer's example, in Go and in SQL;
     - a column list with a quoted comma and a 'where' literal;
     - keywords inside strings and inside longer words ('x where, y', elsewhere, fromage), which do not end the clause;
     - a last_used_at-only update with a nested SELECT … WHERE … IN (1, 2), which is not a door.
2. **A local := was not resolved.**
   - constString now resolves identifiers declared by `:=` as well as by const and var, by their initial value.
   - Fixture: a two-name `t, n := "sessions", 1`, and a local SET fragment.

Mutation: 42 mutants, all killed, excluding one equivalent. Dropping "where" from the clause-ending words changes nothing for valid SQL, because a WHERE clause has no top-level comma; it stays for the reader. Bracket quoting went, because no real sessions column name can contain the characters it would protect.

## Summary

Session creation has one door: IssueUser and IssuePreUser in internal/auth/session/issue.go, both through insert, which decides inside the transaction that writes the row. IssueUser signs in an identity's user only if active (else *NotActiveError, ErrNoUser, ErrNoIdentity); IssuePreUser serves only an identity with no user. chokepoint_test.go fails on a second CreateSession caller, a hand-written insert, a session update that could promote a row, or sessiontest outside a test. AC3 waits on the callback (TKT-01M29C7ZZ9; pending case in draft TKT-01M3AFHGJD).
