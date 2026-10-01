---
schema: 3
id: TKT-01M29CCC0X7SY4SHJ46FV40GNW
title: Implement the settings endpoints and session management
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:identity-sessions
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-10-01T11:39:22Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-ccc0x
  name: ""
extensions: {}
---

## Description

`GET /settings` and `PATCH /settings` for preferences, plus `GET/DELETE /settings/sessions` and `GET/DELETE /settings/identities/{id}`.

The session list is the user-facing half of server-side sessions: see where you are signed in and sign out one device or all of them. Identity management is where a user links a second provider or removes one.

Removing the last identity from an account has to be refused, or the user locks themselves out of an account that still owns posts.

## Acceptance criteria

- [x] GET /settings/sessions lists the viewer's own live sessions with device, IP, provider, created and last-used times, marking the current one; DELETE /settings/sessions/{id} ends one and DELETE /settings/sessions ends all, through session.Store, server-side
- [x] Sign out everywhere ends every session of the account, this one included, and clears the cookie; ending the current session singly does the same; the old cookie's next request is anonymous
- [x] Another member's session id or identity id is not-found and changes nothing (TestRevokeOwnRefusesAnotherMembersSession, TestUnlinkOtherUsersIdentityIsNotFound)
- [x] DELETE /settings/identities/{id} is conflict unless another identity on a configured provider remains, holds under concurrent removals (TestUnlinkRaceLeavesOneIdentity), ends the sessions signed in through it, and writes identity.unlinked
- [x] PATCH /settings validates and stores timezone, page_size and theme with field errors; GET /me and the next thread page reflect them without a re-login, and page_size is in the response cache key (TestPageSizeIsInTheCacheKey)
- [x] The session and identity routes refuse an agent credential with forbidden; anonymous is unauthenticated and a pre-user forbidden
- [x] Goldens for Settings, SessionList and IdentityList; audit.Actions and docs/04 list identity.unlinked; docs/07 describes the routes

## Implementation plan

### Settled in grooming

- Routes are docs/07's row: `GET /settings`, `PATCH /settings`, `GET /settings/sessions`, `DELETE /settings/sessions`, and `GET /settings/identities` with `DELETE /settings/identities/{id}`. The docs/07 notation `GET/DELETE /settings/identities/{id}` is read as the list and the item delete, as `GET/DELETE /settings/sessions` is. Revoking one session needs an item route, so `DELETE /settings/sessions/{id}` is added; docs/07 gets it in the implementing PR. The criterion "revoke one or all" asks for both routes.
- "Sign out everywhere" ends every session of the account, the current one included, and clears the cookie. Decided by the run, docs/05 "Sessions and CSRF" (2026-10-01, pending maintainer review).
- Removing an identity is refused as `conflict` unless another identity on a provider configured now remains. The check and the delete share one write transaction, and the sessions signed in through the removed identity end in it. Decided by the run, docs/05 "Sessions and CSRF" (2026-10-01, pending maintainer review).
- Theme is stored as `theme` in `prefs_json` (`system`, `light`, `dark`; default `system`), and `/me` carries it. Decided by the run, docs/07 "Preact client" (2026-10-01, pending maintainer review).
- Page size is 10 to 50 with a default of 25 (docs/06, docs/10 Q13). It applies to thread pages only. Thread lists, profiles and the queue keep their fixed 25 (docs/07). `threads.Query.PageSize` and `respcache.Key.PageSize` already exist for it (`internal/service/threads/view.go`, `cached.go`).
- Sessions are ended through `internal/auth/session` (`Store.RevokeUser`, plus the new revoke-by-id). Nothing else in the API deletes a session row for this feature. Identities are written by `internal/service/identity` only, as `Link` (`link.go`) is.
- An agent never uses OIDC and has no session (docs/05 "Agent accounts"). The session and identity routes refuse a request authenticated by an agent credential with `forbidden`, as `POST /auth/{provider}/link` does. `GET` and `PATCH /settings` are open to agents, because `page_size` changes the thread pages an agent reads.
- Adding an audit action means adding it to both `audit.Actions` and docs/04 "audit_log" (`TestActionsDocumented`). This ticket adds one action, `identity.unlinked`.

### Revised at claim (2026-10-01, agent:claude-code/phase3-ccc0x)

The code as it stands on main at claim changes these points of the approach below. Everything else stands.

- `session.Info.Key` was `key(hash)`, which is the viewer's `SessionKey`, the key perm's cache uses. It is renamed `Info.ID` and derived as the approach says, under its own prefix. Nothing else read `Info.Key` apart from `TestListForUser`. `Info` also gains `IdentityID` and `Provider`, so `ListUserSessions` returns `identity_id` and `oidc_provider_key`.
- `Unlink` reads the token's session through a new `session.HeldThrough(ctx, q, token, userID, now) (identityID, err)`. `HeldBy` becomes a wrapper over it. It deletes the sessions through the identity with a new `session.EndThrough(ctx, q, identityID)`, which runs in the caller's transaction as `HeldBy` does. Session rows are then deleted only by the session package, as the settled item asks.
- `api.Deps.Sessions` is optional. When nil, `build` makes `&session.Store{DB: d.Users.DB, Now: d.Users.Now}`, as it builds the groups, account and identity services. `serve.go` passes its store, and the settings tests pass the store their session middleware uses, so the two clocks agree. `Deps.Providers []identity.ProviderName` comes from `cfg.OIDC.Providers`. When it is empty, every identity is unconfigured and every removal is `conflict`. That is the safe failure.
- The settings write is one static statement. It uses `json_patch(<the nested CASE guard>, ?)` with a patch object holding only the keys the request names, rather than one `json_set` per key, so sqlc needs no dynamic SQL. For the top-level scalar keys used here, a merge patch is `json_set`: other keys are kept. A patch never holds a null, because each value is validated first.
- `PATCH /settings` decodes into `json.RawMessage` fields with unknown keys refused (`bad-request`). It then decodes each key on its own, so `"30"` for `page_size` is the field error `wrong_type` rather than a bad request. A timezone of `""` or `Local` is `not_an_option`, because `time.LoadLocation` would read either as the server's zone.
- Only `GET /threads/{id}` computes a page (`threads.PageFor` has one caller). `GET /threads/{id}/posts/{ordinal}` returns one post and no page, so only the `thread` handler sets `Query.PageSize`, through a new `users.PageSize(ctx, v)`.
- `DELETE /settings/identities/{id}` answers `dto.IdentityRemoved {signed_out}`, which gets a golden like every other answer. The goldens are `user-settings.member`, `sessions.member`, `identities.member` and `identity-removed.member`. They are not named `settings.*`, because `settings.admin` is the site settings.
- `web/client/src/api/dto.ts` and `dto-contract.ts` mirror the new types, because the contract test fails on a golden nothing imports. `Settings.theme` is optional there (`theme?: ThemePref`). `bootstrap.ts`'s anonymous fallback builds a `Settings` without it, and changing `bootstrap.ts` would change the committed dist, which another lane holds. The types emit no JavaScript, so the dist does not change. TKT-01M29CXK4E (Build the user settings screens) makes `theme` required when it wires the screen and the bootstrap.

### Approach

1. **Session store** (`internal/auth/session/session.go`).
   - Add `Info.ID`, a public id for a listed session. It is a hash of the stored hash under its own prefix (`"ketju session id\x00"`), so it is neither the cookie, nor the `SessionKey` that `perm`'s cache is keyed by, nor `Tag`. Add `Session.ID()` with the same derivation, so a handler can tell which listed row is the current one.
   - Add `RevokeOwn(ctx, userID int64, id string) (bool, error)`. In one write transaction it reads the user's rows (`ListUserSessions`), matches `id`, and deletes with `DELETE FROM sessions WHERE id_hash = ? AND user_id = ?`. It returns false when no row of that user matches, so another member's id is not found.
   - Add `RevokeUserAll(ctx, userID)`, which runs `DeleteUserSessions` and `DeleteIdentitySessions` together, as `perm.deleteSessions` does, so a stale pre-user row of one of the user's identities goes too. `RevokeUser` may simply become this.
   - Add `DeviceLabel(ua string) string`: a small table-driven parser that answers "Firefox on Linux", "Safari on iPhone", "Chrome on Android" and so on, or "Unknown device". It makes no network lookup and uses no dependency.
2. **DTOs** (`internal/dto`).
   - `Settings` gains `Theme string \`json:"theme"\``. Update the `/me` goldens: anonymous, member and agent.
   - New `SessionList {sessions: []SessionInfo}`, where `SessionInfo` is `{id, current, device, user_agent, ip, provider, created_at, last_used_at}`. Only live rows are listed, newest use first.
   - New `IdentityList {identities: []IdentityInfo}`, where `IdentityInfo` is `{id, provider, provider_name, configured, email, created_at, last_login_at, current, removable}`. `provider_name` is the provider's `name` in `config.toml`, or empty once the provider is unconfigured. `removable` is the step 4 rule applied now.
   - Add a golden for each, and mirror each in `web/client/src/api/dto.ts`. The contract test covers them.
3. **Settings service** (`internal/service/users`, beside `Me`).
   - Export the prefs reader as `users.Prefs(prefsJSON) (pageSize int, theme string)`, so the thread handler and `/me` read one rule. Keep `pageSize`'s existing tolerance: missing, malformed or out of range is the default.
   - `UpdateSettings(ctx, v, patch)` validates and writes in one statement.
     - `timezone` must load with `time.LoadLocation`. Import `time/tzdata` in `cmd/ketju`, so the check does not depend on the host's zoneinfo; the binary grows by about 450 KB.
     - `page_size` must be an integer from 10 to 50.
     - `theme` must be one of the three values.
   - Refusals are `validation-failed` with field errors drawn from existing codes only: `not_an_option` for timezone and theme, `out_of_range` for page size, `wrong_type` for a JSON type mismatch.
   - Unknown keys in the body are `bad-request` (`decodeStrict`). Keys already in `prefs_json` that this patch does not name are kept: merge with `json_set` when the stored value is a JSON object. A stored value that is empty, malformed or not an object starts from `'{}'` in the same statement, because the reader already treats it as no preferences and `json_set` would refuse it (Terva review 1704). SQLite does not promise to short-circuit `AND`, and `json_type` raises on malformed JSON, so the guard nests: `CASE WHEN json_valid(prefs_json) THEN CASE WHEN json_type(prefs_json) = 'object' THEN prefs_json ELSE '{}' END ELSE '{}' END` (Terva review 1734). `json_type` is called only in the valid branch.
   - The answer is the stored `dto.Settings`.
   - `locale` is read-only here. Nothing sets it yet; see Out of scope.
4. **Identity service** (`internal/service/identity`, new `unlink.go` beside `link.go`).
   - `ListIdentities(ctx, userID, currentIdentityID)` builds the list from the service's provider list. The new field `Providers []ProviderName{Key, Name}` is built from `cfg.OIDC.Providers` in `cmd/ketju/serve.go`.
   - `Unlink(ctx, userID, token, identityID, ip) (endedCurrent bool, err)` runs one `s.DB.Write`, in this order:
     1. Read the account: active and human, or `ErrCannotLink`'s twin, `ErrCannotUnlink`.
     2. `session.HeldBy(token, userID)`, so a sign-out or revocation landing mid-request refuses the write, as `Link` does (Terva review 1505).
     3. Read the identity. If it is not this user's, return `ErrNoIdentity`, which maps to `not-found`.
     4. Read the user's other identities. Unless at least one is on a configured provider, return `ErrLastIdentity`, which maps to `conflict`.
     5. Delete the sessions with this `identity_id`, using a new query `DeleteSessionsThroughIdentity`, and note whether the token's session was one of them.
     6. Delete the identity, with a new query `DeleteIdentity(id, user_id)`. `applications.identity_id` goes null by its foreign key (docs/04 "applications").
     7. Append `identity.unlinked` against the user, with `{"provider", "identity"}`.
5. **Handlers** (`internal/web/api/settings.go`, new; routes added to `routes()`).
   - `Deps` gains `Sessions *session.Store` and the provider names, wired in `cmd/ketju/serve.go` and the API test harness.
   - Every route needs `v.Member()`: anonymous is `unauthenticated`, and a pre-user is `forbidden`, which `TestPreUserRoutesAreExactlyTheAllowlist` already enforces.
   - The session and identity routes answer `forbidden` when `agentcred.From(ctx)` is set.
   - `DELETE /settings/sessions/{id}` and `DELETE /settings/sessions` answer 204 `no-store`. Each sets `session.ClearCookie()` when the current session ended: for the item route when the id is `Session.ID()`, and always for the collection route.
   - `DELETE /settings/identities/{id}` answers 200 `{signed_out: bool}`, `no-store`, and clears the cookie when the current session ended.
   - The writes are charged to the per-address write limiter like every other mutating route (`h.charged`).
6. **Page size reaches the thread page.** The `thread` and `post` handlers in `api.go`, and any permalink resolution that computes a page, set `threads.Query.PageSize` from the viewer's account prefs (`users.Prefs`). The account is read from the session's user or the agent credential, as `accountOf` does in `me.go`. Remove the "until then every page is this size" comment on `DefaultPageSize`.
7. **Documents.**
   - In docs/07, expand the settings row with the shapes, codes and the item route.
   - In docs/04, add `identity.unlinked` to "audit_log", and add `theme` to the `prefs_json` keys it names.
   - Leave docs/05's run-decision lines as they are.

### Tests

Session management (security; `internal/auth/session`, `internal/web/api/settings_test.go`):
- `TestRevokeOwnEndsTheSessionServerSide`: member A has two sessions. Revoking the other one by its listed id deletes its row, and that cookie's next request to `/api/v1/me` is anonymous. The request's own session still works.
- `TestRevokeOwnRefusesAnotherMembersSession`: A sends B's listed id and gets `not-found`. B's session stays live. The response does not say whether the id exists.
- `TestRevokeCurrentSessionClearsTheCookie`: `DELETE /settings/sessions/{own id}` answers 204 with a clearing `Set-Cookie`, and the next request with the old cookie is anonymous.
- `TestSignOutEverywhereEndsEverySessionIncludingThisOne`: A has three sessions and a stale pre-user row on one of A's identities, and B has one session. `DELETE /settings/sessions` deletes all four of A's rows, clears the cookie, and leaves B untouched.
- `TestSessionListIsOwnLiveSessionsOnly`: an expired but unswept row is not listed, B's rows are not listed, exactly one row is `current`, and `device` comes from `DeviceLabel`.
- `TestSessionIDIsNeitherCookieNorCacheKey`: `Info.ID` differs from the token, `SessionKey` and `Tag` for the same session, and is stable across lookups.
- `TestDeviceLabel`: a table of real user-agent strings.
- `TestSettingsRoutesRefuseAgentsAndAnonymous`: an agent credential gets `forbidden` on the session and identity routes and 200 on `GET /settings`; anonymous gets `unauthenticated`. The pre-user case comes from the existing allowlist walk.

Identities (security; `internal/service/identity/unlink_test.go`, using `dbtest.Open`):
- `TestUnlinkRefusesTheLastIdentity`: with a single identity the answer is `ErrLastIdentity`, and no row, session or audit row changes.
- `TestUnlinkRefusesWhenOnlyUnconfiguredProvidersRemain`: the other identity's provider is absent from `Providers`, so the answer is `ErrLastIdentity`.
- `TestUnlinkRaceLeavesOneIdentity`: two goroutines remove the account's two identities at once. Exactly one succeeds, the other gets `ErrLastIdentity`, and one identity remains. Run it under `-race`, repeated.
- `TestUnlinkEndsSessionsSignedInThroughIt`: sessions through identity X end, sessions through Y stay, and `endedCurrent` is true only when the token's session came through X.
- `TestUnlinkOtherUsersIdentityIsNotFound`: nothing changes.
- `TestUnlinkNeedsALiveSession`: the token is revoked before the call, so the unlink is refused and nothing is written, as in `TestLinkNeedsTheMembersSession`.
- `TestUnlinkKeepsTheApplication`: an application row naming the identity keeps its row with a null `identity_id`.
- `TestUnlinkedSubjectSignsInAsNobody`: after removal, a sign-in with that subject while signed out takes the registration path and does not reach the account.
- `TestUnlinkWritesOneAuditRow`: one `identity.unlinked` row with provider and identity. `TestActionsDocumented` and `TestActionsCoverTheWriters` pass with the new action.

Settings and invariants:
- `TestUpdateSettingsValidates`: a table of bad timezone, page size 9 and 51, `"30"` as a string, an unknown theme, and an unknown key. Each gets the field error codes above, and nothing is written.
- `TestUpdateSettingsKeepsOtherPrefs`: a `prefs_json` key the patch does not name survives.
- `TestUpdateSettingsRepairsMalformedPrefs`: with `prefs_json` set to `''`, `'not json'`, `'[1]'` and `'null'`, a `PATCH` succeeds and stores an object holding only the patched key.
- `TestSettingsApplyWithoutReLogin`: after `PATCH`, the same cookie's next `/me` shows the new values, and the next thread page uses the new size.
- `TestPageSizeIsInTheCacheKey` (response-cache invariant): two members with sizes 10 and 50 read the same thread through the response cache, each gets their own page shape, and neither is served the other's entry.
- `TestMeGolden*`: updated for `theme`.
- The session chokepoint test (`chokepoint_test.go`) still passes. This ticket creates no session.

### Out of scope

- The screens: TKT-01M29CXK4E (Build the user settings screens).
- Setting `locale`. No document says which locales exist, and the client formats with `Intl`. Leave it read-only until a ticket decides; none is filed, because no Phase 3 deliverable needs it.
- Re-authentication before unlinking or signing out everywhere. docs/05 asks for none. The maintainer is asked in TKT-01M3V6XSW2 (Decide whether unlink, link and sign-out-everywhere need recent sign-in), which blocks nothing here.
- Ending SSE streams of a revoked session: TKT-01M29CRJ3X (Build the SSE endpoint and fan-out) owns the test that a session that ends stops receiving events. It must notice revocations made through `session.Store`, as this ticket makes them.
- Screen controls for changing a handle and deactivating, whose endpoints already exist: TKT-01M3V6XTYG (Add handle change and self-deactivation to the settings screen).

## Notes

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:13Z

run decision: pending maintainer review. Sign out everywhere ends every session of the account, this one included, and clears the cookie; the per-session sign-out covers everyone but me. Recorded in docs/05 Sessions and CSRF (2026-10-01), with the alternatives.

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:13Z

run decision: pending maintainer review. Removing an identity is conflict unless another identity on a provider configured now remains; check and delete in one write transaction; the sessions signed in through it end, the current one included. Recorded in docs/05 Sessions and CSRF (2026-10-01), with the alternatives.

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:13Z

run decision: pending maintainer review. Theme is stored on the account as prefs_json theme (system, light, dark) and carried in /me settings; the account's value wins over the browser's copy on load. Recorded in docs/07 Preact client, Theme (2026-10-01).

**agent:claude-code/phase3-groom-pages** at 2026-10-01T08:53:36Z

Terva review 1704 (head 37bcff8, base a599b48), finding 'Allow settings updates when existing preferences JSON is malformed': fixed in the plan. A stored prefs_json that is not a valid JSON object is replaced by '{}' before json_set in the same statement, and TestUpdateSettingsRepairsMalformedPrefs covers it.

**agent:claude-code/phase3-groom-pages** at 2026-10-01T10:18:44Z

Terva review 1734 (head 0d015f4, base f3538e6), finding 'Guard json_type before calling it on malformed preferences': fixed in the plan with a nested CASE, so json_type runs only on valid JSON. TestUpdateSettingsRepairsMalformedPrefs's 'not json' case covers it.

**agent:claude-code/phase3-ccc0x** at 2026-10-01T11:38:54Z

Built as the plan and its "Revised at claim" section say. These choices were left open by the plan, and none changes what a member can do:

- `PATCH /settings` treats `null` for any key as `wrong_type`, because `json.Unmarshal` would otherwise accept it as the zero value. A timezone of `""`, `Local` or over 64 bytes is `not_an_option`.
- `DELETE /settings/identities/{id}` with an id that is not a positive integer is `not-found`, the same answer as an id nobody has. It is not `bad-request`.
- `RevokeOwn` matches live sessions only. An expired, unswept session's id is `not-found`, the same as an id the list never showed.
- `Unlink` refuses a session that ended mid-request with `ErrNotSignedIn` (`unauthenticated`). It refuses an agent or inactive account with `ErrCannotUnlink` (`forbidden`).

Client source. The orchestrator asked this lane not to change client source or `internal/web/spa/dist`. The contract test fails on any golden that `dto-contract.ts` does not import, and `me.*.json` gains `theme`. So the PR edits only `web/client/src/api/dto.ts` (types) and `dto-contract.ts` (a type check that is never bundled). `go generate ./internal/web/spa` leaves the dist byte-identical, and the reader bundle stays at 60465 B (delta 0). `Settings.theme` is optional in TypeScript for now. `bootstrap.ts`'s anonymous fallback builds a `Settings` without it, and changing that file would change the dist. TKT-01M29CXK4E (Build the user settings screens) should make it required when it wires the bootstrap.

Not done: TKT-01M3V6XSW2 (recent sign-in before these actions) is untouched, as the orchestrator directed. No migration was needed.

## Summary

The settings, session and identity routes are built under `/api/v1/settings` as docs/07 now describes them. All seven criteria are met and tested, and nothing is left open.

- **Settings.** `GET` and `PATCH /settings` read and write `timezone`, `page_size` and `theme`, with field errors from the existing codes. The write is one `UpdateUserSettings` statement that merges the named keys into `prefs_json`, behind the nested-CASE guard. `users.Prefs` is the one reader of `page_size` and `theme`. `users.PageSize` sets `threads.Query.PageSize` in the thread handler, so `/me` and the next thread page change with no new sign-in, and `respcache.Key.PageSize` keeps two sizes apart (`TestPageSizeIsInTheCacheKey`, proven to fail without it).
- **Sessions.** `GET /settings/sessions` lists live sessions with the current one marked, a device label, the IP and the provider. Each is named by `Session.ID`, a hash under its own prefix. `Info.Key` exposed the SessionKey perm's cache uses, and is gone. `DELETE /settings/sessions/{id}` goes through `session.Store.RevokeOwn`, and `DELETE /settings/sessions` through `RevokeUser`, which also deletes pre-user rows bound to the account's identities. Ending the request's own session clears the cookie.
- **Identities.** `GET /settings/identities` and `DELETE /settings/identities/{id}` go through `identity.Service.ListIdentities` and `Unlink`. One write transaction checks the account and the session, finds the identity among the member's own, applies the configured-provider rule, ends the sessions through it (`session.EndThrough`), deletes it, and writes `identity.unlinked`. `TestUnlinkRaceLeavesOneIdentity` fails if the rule is read outside that transaction.
- **Access.** Anonymous is `unauthenticated` and a pre-user `forbidden` on every route. An agent's credential is `forbidden` on the session and identity routes and allowed on `/settings`.
- **Contracts.** `dto.Settings.theme`, `SessionList`, `IdentityList` and `IdentityRemoved` have goldens and are mirrored in `dto.ts` as types only. The dist is unchanged. `audit.Actions` and docs/04 list `identity.unlinked`, docs/04 names the `prefs_json` keys, and `cmd/ketju` embeds `time/tzdata`.

For the next ticket: TKT-01M29CXK4E (Build the user settings screens) should make `Settings.theme` required in `dto.ts` and give `bootstrap.ts`'s fallback a theme. TKT-01M29CRJ3X (Build the SSE endpoint and fan-out) must notice sessions that `RevokeOwn`, `RevokeUser` and `EndThrough` end.
