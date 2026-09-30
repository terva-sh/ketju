---
schema: 3
id: TKT-01M3QH3X58PBVSYYJC49XSR65M
title: Answer GET /me for an agent authenticated by its credential
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references:
  - ref: pr:189
    path: null
claim: null
archive: null
created_at: 2026-09-29T21:28:46Z
updated_at: 2026-09-30T02:11:27Z
created_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qh3x58
  name: ""
extensions: {}
---

## Description

`GET /api/v1/me` answers an agent authenticated by its bearer credential as anonymous: `user` is null and there are no unread counts. `users.Service.Me` builds the viewer's fields from `session.From(ctx)`, which a bearer request never has, because the credential is not a session (TKT-01M29P9RSW, Implement agent accounts, provisioning, and the agent credential).

Nothing is exposed and nothing is refused that should be allowed: every other route authorizes from `domain.Viewer`, which the bearer middleware sets. But an agent that calls `/me` to check who it is signed in as reads "nobody". docs/07 says an agent uses the same API as the web client.

A likely fix is for `Me` to read the user row by `v.UserID` when the request has no session but has a credential (`agentcred.From`). That adds one statement on the agent path only, so the session path's two-statement budget is unchanged. Settle whether an agent's `me` should also carry a hint that it is authenticated by a credential.

## Acceptance criteria

- [x] GET /api/v1/me with a live bearer credential answers the agent's handle, display name, role, kind "agent", settings and unread counts (TestMeShapes, golden me.agent.json)
- [x] TestMeBudget's session cases keep their statement lists, and an agent's /me runs MeCounts and at most ForumAccessRows after the middleware's credential statements
- [x] A revoked credential or a suspended or deactivated agent gets 401 from /me, not the anonymous shape, and TestOneDoorIntoSessions passes with its caller lists unchanged (TestMeRefusesADeadCredential)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476.

What exists there: `users.Service.Me` (`internal/service/users/me.go`) builds the viewer's fields from `session.From(ctx)` and returns `anonymousMe()` when there is no session with a user. It is the only reader of `session.From` outside `internal/auth`. `agentcred.Middleware` (`internal/auth/agentcred/middleware.go`) sets `domain.Viewer` from `Credential.Viewer()` and `agentcred.With(ctx, c)`, and never a session. `LookupAgentCredential` (`internal/store/db/queries/agents.sql`) selects `c.id, c.user_id, c.last_used_at, u.role` and the bucket overrides. `MeCounts` reads `mark_all_read_at` from `users` itself, by `user_id`.

### Settled in grooming

- **An agent uses the same API as the web client, and no other** (docs/07 "Agents on the API"). `/me` must answer an agent as the member it is.
- **The `/me` budget**, settled when `/me` was groomed and pinned by `TestMeBudget` (`internal/web/api/me_test.go`): the handler runs at most two statements, the counts and perm's batch, with the middleware's lookup outside the budget. The session path's statements stay exactly `LookupSession`, `MeCounts`, `ForumAccessRows` cold and `LookupSession`, `MeCounts` warm (this ticket's second criterion).
- **The credential is not a session.** This change issues no session and adds no caller to the chokepoint. `TestOneDoorIntoSessions` passes with `doorCallers` and `insertCallers` unchanged, and `TestAgentCredentialMintsNoSession` still holds (TKT-01M29P9RSW).
- **Only a live credential of an active agent reaches the handler.** The middleware answers anything else 401 (`TestAgentCredentialRequiresActiveAgent`). `Me` needs no status check of its own for this path, but keeps `v.Member()` as the guard, as the session path does.
- **An agent is in the `member` response-cache class** (`TestAgentViewerSharesMemberClass`). `/me` is `no-store` and not in the response cache, so no cache key changes.

Engineering choices made here, each with the alternative that lost:
- **The credential lookup carries the user's fields, as the session lookup does.** `LookupAgentCredential` also selects `u.handle, u.display_name, u.avatar_blob, u.kind, u.timezone, u.locale, u.prefs_json`, and `agentcred.Credential` gains `User agentcred.Account` with those fields. `Me` then reads them from the context and runs the same two statements for an agent as for a member. Lost: the ticket's suggestion, a `GetUser` by `v.UserID` in `Me` on the agent path. That is a third handler statement, which breaks the settled budget for agents, and it reads the row a second time in the same request. The cost here is seven more columns on a row the middleware already reads.
- **`Me` builds from one small internal type**, `viewerAccount{Handle, DisplayName, AvatarBlob, Role, Kind, Timezone, Locale, PrefsJSON}`, filled from `session.User` or from `agentcred.Account`. Importing `session` into `agentcred` to reuse `session.User` lost: it couples the credential package to the session package for one struct.
- **No "authenticated by credential" hint in `/me`.** The ticket asks to settle it. `user.kind` is already `agent`, which is what an agent checking its identity needs, and nothing in the client or docs/07 would read a hint. Adding one is a DTO field, a golden and a client type for no reader. If a reader appears, the field can be added then.

### Revised at claim (2026-09-30, against main at c24635c)

The grooming plan stands. Reading the code at claim changed two facts it
relied on, and neither changes a decision:

- `agentcred.Store.Lookup` runs three statements, not one:
  `LookupAgentCredential` and two `GetSetting` reads for the site's bucket
  defaults. All three are the middleware's, outside the handler's budget, so
  the budget line subtracts all of them, as it subtracts `LookupSession`.
- On a cold permission cache, `/me` runs perm's batch before the counts, and
  the batch is named `ForumAccessRowsAll` (the queue's scope, TKT-01M29CRJ4E).
  The expected lists below use the real names and order.

### Approach

1. **Query** `internal/store/db/queries/agents.sql`, `LookupAgentCredential`: add `u.handle, u.display_name, u.avatar_blob, u.kind, u.timezone, u.locale, u.prefs_json` to the select list. The WHERE clause is unchanged. Regenerate `dbq` with `go generate ./internal/store/db`.
2. **`internal/auth/agentcred/agentcred.go`**: `type Account struct { Handle, DisplayName, AvatarBlob, Kind, Timezone, Locale, PrefsJSON string }`, and `Credential.User Account`, filled in `Store.Lookup`. `Credential.Viewer()` is unchanged. Nothing logs a `Credential`; none of these fields is secret.
3. **`internal/service/users/me.go`**:
   - `viewerAccount` and two constructors, `fromSession(*session.User)` and `fromCredential(agentcred.Credential)`.
   - `Me` keeps the pre-user branch first. Then it takes the account from the session when there is one with a user, or else from `agentcred.From(ctx)` when that credential's `UserID` is the viewer's. With neither, or with `!v.Member()`, it answers `anonymousMe()`. The rest of the function, the queue count included, is unchanged and reads the fields from `viewerAccount`.
   - The function comment says the agent path and its budget.
4. **Golden** `internal/dto/testdata/golden/me.agent.json`, from a `tAgent` class in `internal/dto/fixtures_test.go` built on the existing `scribe` author, and the matching line in `web/client/src/api/dto-contract.ts` (contract.test.ts fails on a golden the contract does not name). `dto.ts` does not change, and `go generate ./internal/web/spa` must leave `internal/web/spa/dist` byte-identical.
5. **Docs**: docs/07 "Agents on the API" gains one line: `GET /me` answers an agent as the member it is, from its credential.

### Tests

- `internal/web/api/me_test.go`: the `/me` world is served through `agentcred.Middleware` in front of `session.Middleware`, as serve composes them. A request without `Authorization` passes straight to the session middleware, so every session case is unchanged.
  - `TestMeShapes` gains "agent by credential": the agent's handle, display name, `role`, `kind: "agent"`, its settings, and its unread and notification counts from seeded read positions.
  - `TestMeBudget` gains two agent cases. First request: `LookupAgentCredential GetSetting GetSetting TouchAgentCredential ForumAccessRowsAll MeCounts`. Second: `LookupAgentCredential GetSetting GetSetting MeCounts`. The session cases and their lists are unchanged. The handler's count subtracts the middleware's statements (`LookupSession`, `LookupAgentCredential`, `GetSetting`, `TouchAgentCredential`), so it is 2 cold and 1 warm for both kinds of viewer.
  - `TestMeRefusesADeadCredential`: a revoked credential, and an agent suspended or deactivated, get 401 with `WWW-Authenticate: Bearer`, not the anonymous shape.
  - A bearer `/me` leaves the `sessions` count as it was and sets no cookie.
- `internal/auth/agentcred/agentcred_test.go`, `TestLookupCarriesTheAccount`: `Lookup` returns the handle, display name, avatar, kind and settings stored on the users row.
- `TestOneDoorIntoSessions`, `TestTheIssuersAreKnown` and `TestAgentCredentialMintsNoSession` pass unchanged; `doorCallers`, `insertCallers` and `issuerCallers` are not edited.
- The DTO golden test covers `me.agent.json`; the client's contract test covers its `dto-contract.ts` line.

### Out of scope

- Anything the client shows an agent. Agents do not use the SPA.
- The credential's own fields in `/me` (issue time, last use, limits). They are on the admin detail (TKT-01M29P9RSW), and an agent reading its own bucket is not asked for.
- Admin controls for agents: TKT-01M3QH3GS2.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:21Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. Settled as an engineering choice: the credential lookup carries the user's fields, as the session lookup does, so an agent's /me stays within the two-statement handler budget. The ticket's suggested GetUser in Me would have been a third statement. The ticket asked whether /me should hint that the request was authenticated by a credential: no, because user.kind is already agent and nothing reads such a hint. The plan records both alternatives.

## Summary

GET /api/v1/me now answers an agent authenticated by its bearer credential as the member it is: handle, display name, avatar, role, kind agent, settings, unread and notification counts, and the moderator queue count as for anyone else. LookupAgentCredential selects the user fields /me needs, as LookupSession does; agentcred.Credential carries them as User Account; users.Service.Me takes the viewer's account from the session, or else from the credential, each only when it names the viewer's own user. The handler runs the same statements after the middleware as for a member (ForumAccessRowsAll cold, MeCounts), pinned in TestMeBudget; the middleware's credential statements are LookupAgentCredential, two GetSetting reads for the bucket defaults, and TouchAgentCredential when due. No session is issued and the chokepoint lists (doorCallers, insertCallers, issuerCallers) are unchanged. A dead credential stays a 401 from the middleware (TestMeRefusesADeadCredential). Golden me.agent.json and its dto-contract.ts line are new; dto.ts and the committed dist are unchanged. docs/07 Agents on the API names the /me behaviour. No authenticated-by-credential hint was added, as settled in grooming.
