---
schema: 3
id: TKT-01M29CKRF9H573FYQ2MVDMR3B8
title: Map IdP group claims onto local groups
type: task
status: ready
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
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:identity-roles
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T07:18:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

`claims.groups` names the claim and `group_map` translates its values onto local groups, re-evaluated at every login. This is how an organization manages its moderators from its directory instead of in two places.

Mapped memberships are marked with source `idp` so re-evaluation never clobbers a membership an admin added by hand. Losing a claim removes only the mapped memberships.

## Acceptance criteria

- [ ] A source='admin' membership survives a sign-in whose claim does not assert it, and one the claim does assert stays source='admin' (TestAManualMembershipSurvivesSignIn)
- [ ] A claim value missing from group_map, or mapped to a slug no group has, creates no group and no membership, and an unknown target slug is logged at warn
- [ ] Each membership the sync adds or removes writes one audit_log row (group.member_added or group.member_removed, with source idp and the provider) in the sync's transaction; a sync that changes nothing writes none; with an injected audit failure the memberships are unchanged and the sign-in fails with no session left
- [ ] A pending, suspended or deactivated user signing in gets no session and no membership change (TestGroupMapNeverSignsInAnInactiveUser)
- [ ] ketju config check refuses a group_map that targets everyone or members
- [ ] At every sign-in, the user's source='idp' memberships become exactly the groups that the provider's group_map yields from this token: newly asserted groups are added, and groups no longer asserted, a missing claim, or a provider with no group_map remove them
- [ ] A member who gained a group through a mapped provider and then signs in through a provider without group_map loses that idp membership, and the next permission check reflects it (TestAnUnmappedProviderSignInDropsIdPGroups)
- [ ] docs/05 states that idp memberships are re-evaluated only at sign-in, so a revocation at the IdP takes effect at the member's next sign-in through any provider

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a.

### Settled in grooming

- `groups_claim` names the claim and `group_map` translates its values onto local groups, re-evaluated at every sign-in (docs/05 "Provider configuration" and "Roles and groups"). Both keys sit inside a provider's block, so each provider's map applies to sign-ins through that provider. docs/05 "Login flow" step 3 says "create session, apply group_map".
- Re-evaluation deletes the user's `idp` rows and re-adds what the claims say now. An IdP mapping never overwrites a row an admin set (docs/05 "Roles and groups"). The queries already behave this way: `AddIdPMembership` is `ON CONFLICT DO NOTHING`, so a group the user already has from an admin keeps `source = 'admin'` (note on this ticket from TKT-01M29BYYHG, 2026-09-24). `perm.Evaluator.SyncIdPGroups` exists (`internal/perm/mutate.go`) and writes no audit row today.
- Rows for `everyone` and `members` are refused by a trigger (`group_members_not_implicit`, migration 0003), so a claim must never map onto them.
- An unmapped claim value yields nothing, never a default or a new group (`oidc.Provider.LocalGroups`, and AC3).
- Mapping changes groups, never `users.role`. `role = 'moderator'` implies nothing about the `moderators` group, and the reverse holds too (docs/04 "permissions").
- **Every sign-in re-evaluates** (changed after review 1342, finding 1). A sign-in replaces the user's `idp` memberships with the groups that this sign-in's provider maps from this token. A provider with no `group_map` asserts no groups, so a sign-in through it removes every `idp` membership. The earlier draft left memberships alone on such a sign-in. That was wrong: a member whose directory revoked a privileged group could keep it by signing in through a second provider. With two providers that both map groups, each sign-in leaves exactly what that provider's directory asserts now. A member who needs their mapped groups signs in through the provider that maps them. This is the literal reading of docs/05 "Roles and groups" ("re-adds what the claims say now"), and it fails safe: a sign-in can only drop an `idp` grant or restore one that its own directory asserts at that moment. The alternative, recording the provider on each `idp` row (`group_members.idp_provider`), lost because it would keep the second provider's sign-in from revoking the first provider's grant, which is the hole in the review finding.
- **What re-evaluation does not cover.** Memberships are re-evaluated at sign-in, never during a session (docs/05 "re-evaluated on every login"). A grant revoked at the IdP therefore lasts until the member's next sign-in through any provider. That can be up to the 90-day hard expiry of a session they already hold (docs/05 "Sessions and CSRF"). This limit is accepted and written into docs/05 by this ticket. Revoking a grant sooner is an admin action: removing the membership or suspending the account, which deletes the sessions.
- **A missing claim** from a provider that has a map means the claims assert no groups, so every idp membership is removed. That is the fail-safe reading of "what the claims say now".
- **Mapping runs only for an active user who has just been issued a session**, in `settle`, after bootstrap. Its failure fails the sign-in and ends the session just issued, as a bootstrap failure does (review 511). A member must never come out signed in with memberships their directory has revoked.

### Approach

1. **Provider policy.** Use `identity.Policy` (`internal/service/identity/policy.go`, built from config in `cmd/ketju/serve.go`) with `GroupsClaim` and `GroupMap`. Create it if TKT-01M29CKRE9 has not landed; that ticket defines the same type for `AutoApprove`. Add `(Policy) LocalGroups(provider string, id *oidc.Identity) []string`, which is empty when the provider has no `group_map`, with the slugs from the existing `LocalGroups` logic, moved or called. `settle` gains the provider's identity claims, which it already receives as `id *oidc.Identity`.
2. **Sync with audit** (`internal/perm/mutate.go`). Replace `SyncIdPGroups(ctx, userID, groupIDs)` with `SyncIdPGroups(ctx, userID int64, s IdPSync) (Changes, error)`, where `IdPSync{Provider string; Slugs []string}`. In one `e.write`:
   - Resolve the slugs with `GetGroupBySlug`. An unknown slug is skipped and reported in `Changes.Unknown`. `everyone` and `members` are skipped.
   - Read the current idp rows with a new query `IdPMembershipsForUser :many` (`SELECT group_id FROM group_members WHERE user_id = ? AND source = 'idp'`).
   - Compute `added` and `removed`.
   - For each removed group, `RemoveIdPMembership` (a new query: `DELETE … WHERE group_id = ? AND user_id = ? AND source = 'idp'`) and `AppendAudit{Action: "group.member_removed", TargetType: "user", TargetID: userID, DetailsJson: {"group": slug, "source": "idp", "provider": p}}`.
   - For each added group, `AddIdPMembership`. Only when it inserted a row (change the query to `:execrows`) does it append `group.member_added` with the same details. A row an admin already holds is neither changed nor audited.
   - Keep `DeleteIdPMemberships` for callers that clear everything, or delete it if nothing uses it. Add `IdPMembershipsForUser`, `RemoveIdPMembership` and the `:execrows` form to `decisionWrites` in `internal/perm/layering_test.go` where they write.

   A sign-in that changes nothing writes no audit row, so every sign-in does not add noise. The cache is invalidated after commit, as every `perm` write is.
3. **Calling it.** In `identity.settle`, after `ApplyBootstrap`, call `s.Perm.SyncIdPGroups` with `Policy.LocalGroups` on every sign-in, mapped or not, so that an unmapped provider's empty list removes the `idp` rows. Then log any `Changes.Unknown` at warn with the provider and the slug, and on error `abandon`. Registration (`Register`, and `AutoRegister` from TKT-01M29CKRE9) reaches `settle` too, so a new member gets their mapped groups at once.
4. **Config check** (`internal/config/validate.go` `validateProviders`). A `group_map` value of `everyone` or `members` is a problem: "is implicit; every member is in it already". `ketju serve` checks at startup that every target slug names a group, and logs an unknown one at warn, as the registry logs a disabled provider. It cannot be a config error, because the check needs the database. docs/02 "Configuration" gains the problem.
5. **Explicit claim default.** `oidc.Provider.groupsClaim()` falls back to `"groups"` when `groups_claim` is empty. Config validation already refuses a `group_map` without `groups_claim`, so the fallback only matters for a provider with no map, where nothing reads it. Leave it as it is.
6. **Docs.** docs/05 "Roles and groups" gains three things: the rule that every sign-in re-evaluates, with an unmapped provider asserting no groups; the sign-in-only limit; and the two audit actions. docs/04 "audit_log" lists the actions.

### Tests

- `internal/perm` `TestSyncIdPGroupsAddsAndRemoves`. A user with idp rows {a, b} and an admin row {c}, synced with {b, c, d}, ends with idp {b, d}, `c` still `source = 'admin'`, and audit rows exactly `member_removed a` and `member_added d`. A second identical sync writes no audit row.
- `TestSyncIdPGroupsIsOneTransaction`. An injected `BEFORE INSERT ON audit_log` trigger makes the sync fail, and the memberships are exactly as before (no half-applied diff). The perm cache is not invalidated on the failed write (`TestFailedWriteKeepsCache` pattern).
- `TestSyncIdPGroupsIgnoresUnknownAndImplicit`. The slugs `nope`, `everyone` and `members` create no group and no row. `nope` comes back in `Changes.Unknown`, and the other two are skipped.
- `TestSyncIdPGroupsInvalidates`. After a sync that adds `moderators`, whose group grants `moderate` on forum F, the next `Check` for that viewer sees `moderate` without waiting out the TTL.
- `internal/web/auth` `TestGroupMapAtSignIn`. With the fake IdP asserting `groups: ["forum-mods"]` and `group_map = {"forum-mods" = "moderators"}`, sign-in adds the membership and the audit row. The next sign-in asserting `[]`, and then one with the claim absent, each removes it and writes one `member_removed`.
- `TestAManualMembershipSurvivesSignIn` (AC2).
- `TestAnUnmappedProviderSignInDropsIdPGroups` (review 1342, finding 1). A member with identities on `club` (mapped) and `other` (unmapped) gains `moderators` through `club`. `club` then stops asserting `forum-mods`, and the member signs in through `other`. The `moderators` membership is gone, one `group.member_removed` row names `other`, and the next `perm` check has no `moderate`. A second subtest has two mapped providers that disagree: after each sign-in, the `idp` rows equal that provider's mapping alone. A `source='admin'` row survives both subtests.
- `TestAFailedGroupSyncFailsTheSignIn`. With the audit trigger injected, the callback answers 500, no session row is left for the user, and the replaced session is already gone, following `TestBootstrapFailureFailsTheSignIn`.
- `TestGroupMapNeverSignsInAnInactiveUser`. For each of `pending`, `suspended` and `deactivated`, sign-in is "Account unavailable", `SyncIdPGroups` is not called (no audit rows), and memberships are unchanged.
- `internal/config` `TestGroupMapRefusesImplicitGroups`.

### Out of scope

- Showing idp memberships as read-only in the admin screen: TKT-01M29CXK1X.
- Mapping claims onto `users.role`: not designed, and not planned.
- A per-provider source on `group_members`: not planned (see "Every sign-in re-evaluates" above).
- Re-evaluating memberships during a session: not designed. The limit is stated under "What re-evaluation does not cover".

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:43:34Z

From TKT-01M29BYYHG, 2026-09-24: the login re-evaluation is two queries in one db.Write: `DeleteIdPMemberships(userID)`, then `AddIdPMembership` for each mapped group. AddIdPMembership is ON CONFLICT DO NOTHING, so a group the user is already in because an admin put them there keeps source 'admin'. Rows for everyone and members are refused by a trigger, so do not map claims onto them.

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:56Z

Review 1342 finding 1 (high) is fixed in the plan. It supersedes the settled item 'Which sign-ins re-evaluate'. Every sign-in now replaces the user's idp memberships with what that provider's group_map yields, and a provider with no map yields none. A sign-in through an unmapped provider therefore drops grants that a mapped directory revoked, instead of keeping them. The plan now states the remaining limit, that memberships are re-evaluated only at sign-in, and docs/05 will record it. TestAnUnmappedProviderSignInDropsIdPGroups and the rewritten criteria cover it.
