---
schema: 3
id: TKT-01M29P9RSW21SCES335WERTBSF
title: Implement agent accounts, provisioning, and the agent credential
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
  - TKT-01M29CXK1X0EF4CPPN11KVGC2Z
  - TKT-01M29CKRFSDCGKH605X8Y4RT3C
blocks_on: none
references:
  - ref: design:identity
    path: docs/05-identity-and-accounts.md
  - ref: design:api
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-12T02:14:17Z
updated_at: 2026-09-29T07:18:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The implementation of the positions settled in TKT-01M29GGG3W. The design is written into docs/04, docs/05, docs/07, and docs/08; this builds it.

An agent is a first-class member: `users.kind = 'agent'`, its own handle, its own standing, its own credential. An admin provisions it and it never self-registers, because it has no browser and no IdP subject. Roles apply unchanged, so an agent can be a member, a moderator, or an admin.

Lands in Phase 2 because provisioning an account and issuing its credential are one admin screen, and the users and permissions screens are already Phase 2. Splitting them across phases means building that screen twice.

The credential belongs to the agent rather than to the human who created it, so revoking it stops one agent and nothing else. It is a hashed bearer token with no idle expiry, because an agent may be quiet for weeks and then work.

Note the one subtlety worth getting right: the machine-authored frame flag bit is set by the server from `users.kind` at append time, and is never accepted from a client. It records what was true when the post was written, which is why it is in the log and not only derived from the account. An account whose kind changes later does not rewrite its history.

## Acceptance criteria

- [ ] users.kind exists with values human and agent, defaulting to human, and is orthogonal to role
- [ ] An admin can create an agent account and issue its credential in one flow, and revoke the credential without touching the account
- [ ] The credential authenticates as Authorization: Bearer, is stored hashed, has no idle expiry, and creates no session row
- [ ] Suspending or deactivating an agent revokes its credential, and the audit log records the actor either way
- [ ] The server sets the machine-authored frame flag bit from users.kind at append time, and rejects or ignores any client-supplied value
- [ ] The index entry carries the mirroring machine-authored bit so a lens can filter without decoding bodies
- [ ] An agent sets its read position only through PUT /threads/{id}/read, with no beacon and no mark-on-render path
- [ ] author.kind and the per-post machine_authored flag appear in the thread response, and a golden JSON test pins both
- [ ] An agent can hold the moderator and admin roles through the audited role change, and TestNoKindChecksInPermissions finds no reference to domain.KindAgent outside the frame-flag stamp, the credential, provisioning and the limiter's agent policy
- [ ] Bearer-authenticated writes, except read positions and drafts, are charged to a per-agent token bucket (site defaults agents.write_burst 20 and agents.write_refill_seconds 30, overridable per agent), and exceeding it is 429 with Retry-After and appends nothing (TestAgentBucket)
- [ ] A bearer request writes no sessions row and sets no cookie, and the chokepoint's caller lists are unchanged (TestAgentCredentialMintsNoSession, TestOneDoorIntoSessions)
- [ ] Issuing a credential to an agent whose status is not active answers conflict and writes no credential, so an unsuspended agent has no live credential until one is issued afresh (TestIssueRefusedUnlessActive)

## Implementation plan

### Settled in grooming

- From this ticket's text: "Depends on the admin users screen rather than duplicating it. Read docs/05 'Agent accounts' and docs/07 'Agents on the API' before starting; both were written for this ticket." That sentence had been caught in the Description by an editing fault, and was moved here in grooming. The dependency on TKT-01M29CXK1X is recorded.
- An agent is a member with `users.kind = 'agent'`. `kind` is orthogonal to `role`, and an admin provisions every agent (docs/04 "users", docs/05 "Agent accounts", docs/10 items 30 to 32, TKT-01M29GGG3W). The column already exists (migrations 0002 and 0010).
- The credential is a bearer token in `Authorization`, stored hashed as session ids are, with no idle expiry. Revocation is the only way it ends. It belongs to the agent, not to the admin who issued it. It is not a session: there is no cookie and no `sessions` row, and CSRF has nothing to act on, but the JSON-only rule still applies (docs/05 "Sessions and CSRF", docs/07 "Agents on the API").
- For an agent, suspension and deactivation revoke its credential rather than its sessions (docs/05 "Account lifecycle"). The transitions are TKT-01M29CKRFS's, a dependency.
- Agent writes are charged to a token bucket, per agent with a site default. Exceeding it is `429` with `Retry-After`, never a queue. Slow mode applies to agents too (docs/08 "Rate limits"). docs/04 "settings" leaves the defaults to this ticket.
- The per-address write ceiling (60 burst, 1 per 0.5 s) applies to agents as well. An agent that trips it gets a 429 that did not come from its own bucket, which is intended (TKT-01M29C800X, the note on this ticket of 2026-09-19).
- The post-session limiter stage is shared with TKT-01M29BKBM3's per-identity limit on application submission. Whichever lands first builds it as a general stage and says so in its summary (the same note).
- The server sets the machine-authored frame bit from `users.kind` at append time (docs/03 "Frame flags"). That is already built: `threads.Service.authorFlags` in `create.go`, used by create, reply, edit, delete and restore. The index mirrors it (`FrameFlags.ToEntryFlags`), and the goldens `thread.member.json` in `internal/dto` and in `internal/web/api` pin `author.kind: "agent"` and `machine_authored: true`. This ticket proves the same for a bearer-authenticated write.
- An agent's read position moves only by `PUT /threads/{id}/read`, with no beacon and no mark on render (docs/07). The server has no mark-on-render path today, and the HTML client's `mark_read` does not exist yet.
- A purpose-built agent surface such as MCP is gated, not scheduled (docs/07). Personal API tokens are a later phase.
- **The session chokepoint.** Only `session.IssueUserWith` issues sessions, and `chokepoint_test.go` fails on a second door. The credential path issues no session, so the chokepoint's caller lists do not change.
- The gate, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The screen is TKT-01M29CXK1X's users screen, in the lazy admin chunk. The built client is committed: run `go generate ./internal/web/spa`.
- **Maintainer decision 2026-09-29:** the agent rate bucket allows a burst of 20 and refills one token every 30 s, as the site defaults `agents.write_burst` and `agents.write_refill_seconds`. It was proposed in grooming with this reasoning: docs/08 describes normal agent traffic as "twenty posts in a minute while working and then silence for hours". The burst covers that minute, and the refill gives 120 an hour, well under what a runaway would do. These are settings, and an operator can change them. A note on this ticket records it.

Engineering choices made here, each with the alternative that lost:
- **A table of its own, `agent_credentials`**, rather than rows in `sessions`. `sessions` has the user and pre-user CHECK, the expiry rules the sweeper applies, and the chokepoint scan. A credential row there would need an exemption from all three.
- **One live credential per agent.** Issuing a new one revokes the old in the same transaction, which is how a leaked token is rotated. Two live tokens for an overlap lost, because an agent is small and operator-controlled (docs/05) and a second token is a second thing to forget to revoke.
- **Token format `ketju_agent_` followed by 32 random bytes in base64url.** The prefix lets a secret scanner recognise a leaked one. It is stored as the SHA-256 hash, the `sqltypes.SessionHash` type, and shown once, in the response that issues it.
- **An `Authorization` header decides the request.** When one is present the cookie is not read. A malformed or unknown bearer, or a revoked one, or one whose agent is not active, is `unauthenticated`, not an anonymous request. An agent with a bad token learns at once, rather than reading as a stranger.
- **Agents are created as `member`.** A role is then changed through `perm.ChangeRole` (TKT-01M29CXK1X), so a role never changes without its audit row, and there is one path for it.
- **What is charged:** every mutating `/api/v1/*` request authenticated by a bearer, except `PUT` and `DELETE /threads/{id}/read` and `/drafts/*`. Reading and marking read is not what the bucket guards against, and charging it would throttle an agent for reading.

### Approach

1. **Migration** (next free number):
   - `agents (user_id INTEGER PRIMARY KEY REFERENCES users (id), bucket_burst INTEGER CHECK (bucket_burst >= 1), bucket_refill_seconds INTEGER CHECK (bucket_refill_seconds >= 1), created_by INTEGER REFERENCES users (id), created_at INTEGER NOT NULL)`. A null override means the site default.
   - `agent_credentials (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users (id), token_hash BLOB NOT NULL UNIQUE, created_by INTEGER REFERENCES users (id), created_at INTEGER NOT NULL, last_used_at INTEGER, revoked_at INTEGER, revoked_by INTEGER REFERENCES users (id))`.
   - `CREATE UNIQUE INDEX agent_credentials_live ON agent_credentials (user_id) WHERE revoked_at IS NULL`.
   Revoked rows are kept as a record.
2. **Settings** in `internal/settings`: `agents.write_burst` (20, `atLeast(1)`) and `agents.write_refill_seconds` (30, `atLeast(1)`). Add both rows to docs/04's table (`TestKeysDocumented`).
3. **`internal/auth/agentcred`** (new):
   - `Store`. `Issue(ctx, q, userID, by) (token string, err)` is used inside a caller's transaction. It refuses a user whose kind is not `agent`, and a user whose status is not `active` with `ErrAgentNotActive`, which maps to `conflict`. Suspension and deactivation revoke the credential, so a credential issued while the agent is suspended would come alive at unsuspension without anyone issuing it then (Terva review on PR #140, head a9d8a0a). The status is read in the same transaction as the insert, so a suspension cannot land between the check and the write. Otherwise it revokes any live row and inserts. `CreateAgent` creates the agent `active`, so its issue passes. `RevokeIn(ctx, q, userID, by, via)`.
   - `Lookup(ctx, token) (Credential, error)`: one query joining `agent_credentials`, `users` and `agents`, and reading the two settings rows beside them. It returns nothing unless the credential is not revoked, `users.kind = 'agent'` and `users.status = 'active'`. `last_used_at` is touched at most every 5 minutes, as `session.TouchInterval`.
   - `Middleware(store, next func(http.Handler) http.Handler, log)`. When `Authorization` is present it must be `Bearer <token>`, or the answer is `unauthenticated`. After a lookup the context carries `domain.Viewer{UserID, Active: true, Role, SessionKey: <hash of the hash>}` and `agentcred.From(ctx)`, the burst and refill included. Without the header it defers to the session middleware. `serve` wires `Options.Session` to the composition, so the chain's order in `internal/web/server.go` does not change.
   - The logging redactor already drops `authorization` and `token` (`internal/logging/logging.go`). Do not log the credential under any other name.
4. **The post-session limiter stage** in `internal/web/identitylimit.go`: `IdentityLimit(policies ...Policy)`, where a `Policy` returns `(key string, rate Rate, charge bool)` for a request and its context. It sits between session resolution and CSRF, as a new `Options.IdentityLimit` field, and position 6b is documented in `chain`. It reuses `limiter` with a per-key rate. The agent policy charges as described above, keyed `agent:<user id>`. Over the limit the answer is `problem.WriteRetryAfter(rate-limited)`. If TKT-01M29BKBM3's stage has landed, add this policy to it instead.
5. **Lifecycle.** In TKT-01M29CKRFS's suspend, deactivate and anonymize transitions, for `kind = 'agent'`, call `agentcred.RevokeIn` in the same transaction and write `agent.credential_revoked {"via": "<transition action>"}` beside the transition's own row.
6. **`internal/service/users/agents.go`** (admin-gated):
   - `CreateAgent(ctx, v, by, NewAgent{Handle, DisplayName; IssueCredential bool})` runs in one `db.Write`. It inserts the `users` row (`kind = 'agent'`, `status = 'active'`, `role = 'member'`) and the `agents` row, and appends `user.created {"kind": "agent"}`. With `IssueCredential` it also issues, appending `agent.credential_issued`. The handle passes registration's checks (`internal/service/identity/handle.go`: valid, not reserved, free in any case), and a taken one is `conflict`. No `identities` row is written.
   - `IssueCredential`, `RevokeCredential` (`agent.credential_revoked`), and `SetLimits(burst, refill *int)` (`agent.limits_changed`).
7. **Routes** in `internal/web/api/admin_users.go` (admin):
   - `POST /api/v1/admin/users` with `{kind: "agent", handle, display_name, issue_credential}` answers 201 `dto.CreatedAgent{User AdminUserDetail; Credential *IssuedCredential{Token, CreatedAt}}`. A `kind` other than `agent` is `validation-failed`, because humans arrive by signing in.
   - `POST /api/v1/admin/users/{handle}/credential` answers 201 `IssuedCredential`, and `DELETE` it answers 204.
   - `PUT /api/v1/admin/users/{handle}/agent-limits` with `{burst, refill_seconds}`, where null means the default.
   All are `no-store`. `dto.AdminUserDetail` gains `Agent *AgentInfo{Credential *{CreatedAt, LastUsedAt}; Burst, RefillSeconds *int; DefaultBurst, DefaultRefillSeconds int}`. Goldens `created-agent.admin.json` and an agent variant of the user detail.
8. **Client** (admin chunk), in the users screen:
   - A "New agent" form.
   - After creation or issue, a one-time token panel with a copy button and the line "This token is shown once. Store it now."
   - Issue new token (with a confirmation that it replaces the old) and Revoke.
   - Limit fields that show the site default as a placeholder.
9. **Docs**:
   - docs/05 "Agent accounts": the token format, one live credential, and header precedence.
   - docs/07 "Agents on the API": the routes, and 401 on a bad bearer.
   - docs/08 "Rate limits": the defaults and what is charged.
   - docs/04: the two tables and two settings.
   - docs/07 "CSRF posture" drops "the per-agent token bucket" from "Still to come".

### Tests

- **The chokepoint.** `TestOneDoorIntoSessions` passes with `doorCallers` and `insertCallers` unchanged. `TestAgentCredentialMintsNoSession`: a bearer request that writes a post leaves the `sessions` count unchanged and sets no cookie.
- `TestAgentCredentialRequiresActiveAgent`: for a pending, suspended or deactivated agent, a revoked credential, and a credential row a test points at a human, the answer is 401 and nothing is written.
- `TestAgentHasNoIdentity`: provisioning writes no `identities` row, so `IssueUser` has nothing to sign the agent in with.
- `TestCredentialStoredHashed`: the token's bytes appear in no table and in no log line from a request that carried it.
- `TestRevokeStopsOnlyThatAgent`: two agents, one revoked. The other still authenticates, and the revoked agent's `users` row is unchanged.
- `TestIssueRefusedUnlessActive`: issuing to a suspended, deactivated or pending agent answers `conflict`, and no `agent_credentials` row is written. After unsuspension the agent has no live credential until one is issued.
- `TestIssueReplacesTheLiveCredential`: the old token is 401 at once.
- `TestSuspendRevokesCredential`: through TKT-01M29CKRFS's transition. The credential is revoked in the same transaction, and a test trigger aborting the audit insert leaves it live. Both audit rows name the actor.
- `TestAgentCreationAuditedInOneTransaction`: the `users` row, the `agents` row, the credential and the audit rows are written all together or not at all.
- `TestAgentRolesUnchanged`: an agent promoted to moderator through `perm.ChangeRole` can lock a thread, with a `user.role_changed` row.
- `TestNoKindChecksInPermissions`: a source scan fails on any reference to `domain.KindAgent` outside `threads.authorFlags`, `internal/auth/agentcred`, `internal/service/users/agents.go` and the limiter's agent policy.
- `TestBearerWriteIsMachineAuthored`: a bearer reply's frame has `FrameFlagMachineAuthored`, its index entry has `EntryMachineAuthored`, and the thread response shows `machine_authored: true` and `author.kind: "agent"`.
- `TestClientCannotSetMachineAuthored`: a human's reply body with `"machine_authored": true` is `bad-request` (the decoder disallows unknown fields), and nothing is appended.
- `TestAgentBucket` (fake clock): 20 posts pass and the 21st is 429 with `Retry-After`, with nothing appended. A refill admits one more. A per-agent override is honoured. A changed site default applies on the next request. `PUT /threads/{id}/read` and drafts are not charged. A cookie-authenticated member is never charged.
- `TestAgentReadPositionOnlyExplicit`: a `GET` of the thread by an agent leaves `read_positions` unchanged, and `PUT` sets it.
- `TestAgentViewerSharesMemberClass`: an agent member's thread page uses the `member` response-cache class. No new viewer class is introduced, so the key needs nothing new.
- Goldens and client tests. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- Personal API tokens for humans (a later phase, docs/07).
- An MCP server or feed (gated, docs/07).
- Changing an account's `kind`.
- Agent avatars, since uploads are not built.
- The lifecycle transitions themselves (TKT-01M29CKRFS).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Maintainer decision 2026-09-29: the agent rate bucket allows a burst of 20 and refills one token every 30 s (agents.write_burst and agents.write_refill_seconds defaults). The plan's Settled section records it.

**agent:claude-code/phase2-groom** at 2026-09-29T06:56:02Z

Terva review on PR #140, head a9d8a0a, medium (security): accepted and fixed. agentcred.Issue refuses an agent whose status is not active with conflict, checked in the insert's transaction, so a credential cannot be issued during a suspension and come alive at unsuspension (TestIssueRefusedUnlessActive).

## Comments

**agent:claude-code/web** at 2026-09-19T23:21:33Z

The post-session limiter stage this ticket's per-agent token bucket needs is now
shared with TKT-01M29BKBM3 "Approval-mode registration and the forms system",
which acquired a per-identity limit on application submission when
TKT-01M29C800X settled where each half of docs/05's rate-limit line belongs.

Whichever ticket lands first should build the stage as a general post-session
limiter rather than a single-purpose one, and say in its summary that it did.

Worth knowing while building this: the per-address write ceiling from C800X
applies to agent traffic too, at 60 burst and one per half second. An agent that
trips it gets a 429 that did not come from its own bucket. That is intended --
the ceiling is a crash barrier for every caller -- but it will look like a bug to
whoever debugs the first agent that hits it.
