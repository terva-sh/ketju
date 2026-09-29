---
schema: 3
id: TKT-01M29CKRF9H573FYQ2MVDMR3B8
title: Map IdP group claims onto local groups
type: task
status: done
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
  - ref: pr:147
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T15:24:47Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckrf9
  name: ""
extensions: {}
---

## Description

`claims.groups` names the claim and `group_map` translates its values onto local groups, re-evaluated at every login. This is how an organization manages its moderators from its directory instead of in two places.

Mapped memberships are marked with source `idp` so re-evaluation never clobbers a membership an admin added by hand. Losing a claim removes only the mapped memberships.

## Acceptance criteria

- [x] A source='admin' membership survives a sign-in whose claim does not assert it, and one the claim does assert stays source='admin' (TestAManualMembershipSurvivesSignIn)
- [x] A claim value missing from group_map, or mapped to a slug no group has, creates no group and no membership, and an unknown target slug is logged at warn
- [x] Each membership the sync adds or removes writes one audit_log row (group.member_added or group.member_removed, with source idp and the provider) in the sync's transaction; a sync that changes nothing writes none; with an injected audit failure the memberships are unchanged and the sign-in fails with no session left
- [x] A pending, suspended or deactivated user signing in gets no session and no membership change (TestGroupMapNeverSignsInAnInactiveUser)
- [x] ketju config check refuses a group_map that targets everyone or members
- [x] At every sign-in, the user's source='idp' memberships become exactly the groups that the provider's group_map yields from this token: newly asserted groups are added, and groups no longer asserted, a missing claim, or a provider with no group_map remove them
- [x] A member who gained a group through a mapped provider and then signs in through a provider without group_map loses that idp membership, and the next permission check reflects it (TestAnUnmappedProviderSignInDropsIdPGroups)
- [x] docs/05 states that idp memberships are re-evaluated only at sign-in, so a revocation at the IdP takes effect at the member's next sign-in through any provider

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a. Revised at claim against main at 889870b.

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

Revised at claim. What changed from the groomed approach, and why: TKT-01M29CKRE9 has not landed, so this ticket creates `identity.Policy`; `internal/audit` now exists, so the audit rows go through `audit.Append`; and registration carries the mapped claim values in its sealed cookie, so a new member gets their groups at once.

1. **Provider policy.** New `internal/service/identity/policy.go`: `type Policy map[string]ProviderPolicy`, `ProviderPolicy{GroupMap map[string]string}`, `PolicyFrom([]config.Provider)`, and `(Policy) LocalGroups(provider string, groups []string) []string`. The mapping logic moves here from `oidc.Provider.LocalGroups`, which is deleted and its test moved beside the new one. The registry is the wrong owner, because a provider disabled by a failed refresh drops out of `Lookup`. TKT-01M29CKRE9 adds `AutoApprove` to `ProviderPolicy`. `cmd/ketju/serve.go` sets `identity.Service.Policy`.
2. **Sync with audit** (`internal/perm/mutate.go`). `SyncIdPGroups(ctx, userID int64, s IdPSync) (Changes, error)`, with `IdPSync{Provider, IP string; Slugs []string}` and `Changes{Added, Removed, Unknown []string}`. In one `e.write`:
   - the user must be `active`, or it returns `ErrNotActive` and writes nothing;
   - slugs are de-duplicated, `everyone` and `members` skipped, and each resolved with `GetGroupBySlug`; an unknown one is reported in `Changes.Unknown`;
   - the current idp rows are read with a new `IdPMembershipsForUser :many` (group id and slug);
   - each removed group goes through a new `RemoveIdPMembership :execrows`, each added one through `AddIdPMembership`, changed to `:execrows`;
   - each row actually inserted or deleted writes one audit row through `audit.Append`: action `group.member_added` or `group.member_removed`, target `audit.User(userID)`, a NULL actor with `via = "group_map"`, the request's IP, and details `{group, group_id, source: "idp", provider}`. A row an admin holds is neither changed nor audited.

   `DeleteIdPMemberships` loses its last caller and is deleted. `decisionWrites` in `internal/perm/layering_test.go` gains `RemoveIdPMembership`.
3. **Calling it.** `identity.settle` takes the request IP and, after `ApplyBootstrap`, calls `s.Perm.SyncIdPGroups` with `s.Policy.LocalGroups(id.Provider, id.Groups)` on every sign-in, mapped or not. It logs each unknown slug at warn with the provider, and on any error, including a nil `Perm`, it `abandon`s. `Claims` gains `Groups` (json `g`). `Service.ClaimsOf(id)` replaces the free `ClaimsOf` and keeps only the claim values the provider's `group_map` names, so the sealed registration cookie is bounded by the operator's map rather than by how many groups the IdP asserts. The `Login` interface in `internal/web/auth` gains `ClaimsOf`.
4. **Config check** (`internal/config/validate.go`). A `group_map` value of `everyone` or `members` is a problem. `ketju serve` checks at startup that every target slug names a group (`Policy.UnknownTargets`) and logs an unknown one at warn. docs/02 "Configuration" gains the problem.
5. **Claim default.** `oidc.Provider.groupsClaim()` stays as it is.
6. **Docs.** docs/05 "Roles and groups": every sign-in re-evaluates, an unmapped provider asserts no groups, the sign-in-only limit, and the two audit actions. docs/04 "audit_log" names the two actions.

### Tests

- `internal/perm`: `TestSyncIdPGroupsAddsAndRemoves`, `TestSyncIdPGroupsIsOneTransaction` (an injected `BEFORE INSERT ON audit_log` trigger: memberships unchanged, cache not invalidated), `TestSyncIdPGroupsIgnoresUnknownAndImplicit`, `TestSyncIdPGroupsInvalidates`, `TestSyncIdPGroupsRefusesAnInactiveUser`.
- `internal/service/identity`: `TestPolicyLocalGroups` (moved from oidc) and the `ClaimsOf` filter.
- `internal/web/auth`: `TestGroupMapAtSignIn`, `TestAManualMembershipSurvivesSignIn`, `TestAnUnmappedProviderSignInDropsIdPGroups` (two subtests), `TestAFailedGroupSyncFailsTheSignIn` (no session left, the replaced session gone), `TestGroupMapNeverSignsInAnInactiveUser`, `TestGroupMapAtRegistration`.
- `internal/config`: `TestGroupMapRefusesImplicitGroups`.
- The session chokepoint test's caller list does not change: sessions are still issued only by `session.IssueUser` and `IssueUserWith`.

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

**agent:claude-code/phase2-run-ckrf9** at 2026-09-29T15:09:49Z

Built at 14ac84f. Three places where the build differs from the revised plan, and why:

1. `DeleteIdPMemberships` is kept, not deleted. The plan's step 2 said it would be removed once it lost its last caller. Production code no longer calls it, but `internal/store/db/forums_groups_test.go` still uses it to prove the schema keeps admin rows. It stays in `decisionWrites`, so no package outside `perm` and the store can call it.
2. `SyncIdPGroups` refuses a user who is not `active` (`perm.ErrNotActive`), inside its transaction. `session.IssueUser` already refuses an inactive user before `settle` runs, so a sign-in never reaches the sync for one. The check covers a suspension that commits between issuing the session and syncing, and keeps the rule "only an active user is mapped" in the one place that writes.
3. Audit rows carry `via = "group_map"` with a NULL actor, as docs/04 prescribes for work no account did, plus the request IP. Details are `group`, `group_id`, `source: "idp"` and `provider`.

Criteria evidence: AC1 TestAManualMembershipSurvivesSignIn; AC2 TestGroupMapGrantsNothingUnmapped and TestSyncIdPGroupsIgnoresUnknownAndImplicit; AC3 TestSyncIdPGroupsAddsAndRemoves, TestSyncIdPGroupsIsOneTransaction and TestAFailedGroupSyncFailsTheSignIn; AC4 TestGroupMapNeverSignsInAnInactiveUser and TestSyncIdPGroupsRefusesAnInactiveUser; AC5 TestGroupMapRefusesImplicitGroups and the "a group_map onto members" case of TestConfigCheckFailsNamingKey; AC6 TestGroupMapAtSignIn and TestGroupMapAtRegistration; AC7 TestAnUnmappedProviderSignInDropsIdPGroups; AC8 docs/05 "Roles and groups".

Contract changes for later tickets: `identity.Policy` / `ProviderPolicy{GroupMap}` / `PolicyFrom([]config.Provider)` now exist, and `cmd/ketju/serve.go` sets `identity.Service.Policy`. TKT-01M29CKRE9 adds `AutoApprove` to `ProviderPolicy`. `identity.ClaimsOf` is now the method `(*Service).ClaimsOf`, and the `auth.Login` interface has it. `identity.Service` needs `Perm` for every sign-in, not only a bootstrap one.

**agent:claude-code/phase2-run-ckrf9** at 2026-09-29T15:24:47Z

Terva reviews of PR 147, both clean, with no findings at any severity:

- Run 2bb5ec90-b3c3-41da-a964-cc22c1de4630 (request ready-review-b2ce8ba, Actions run #896): head b2ce8ba36606e330cadb42a5ce7fa419b9783059, base 889870b4f853046430ae18cc75db6f1dcd1debf4.
- Run 4c5d0778-2a2a-4e81-b725-dd09b8cc7181 (request ready-review-01b7f3c, Actions run #902): head 01b7f3c813de02ccd5b5e5908af12bf2e3826216, base ccc05adb32252b1e51ea6ebb6131a26edbd3ae92. This head adds one comment correction found in self-review: `identity.Service.Bootstrap` no longer says that Perm may be nil.

Main moved to ccc05ad (TKT-01M29CKRHS) while the PR was open. The only file both sides touched is the generated `internal/store/db/dbq/querier.go`, in separate hunks, and the PR stays mergeable. The commits after 01b7f3c touch only `.tickets/`, so the review result is carried to them.

## Summary

Every sign-in, and registration as the first, now sets the user's idp memberships to exactly what that provider's group_map yields from the token. An unmapped provider or a missing claim removes them all, and admin rows are never touched. perm.SyncIdPGroups does it in one transaction with one audit row per change (group.member_added/removed, via group_map), refuses inactive users, and skips everyone, members and unknown slugs. identity.settle runs it after bootstrap and ends the new session if it fails. identity.Policy is new. config check refuses group_map onto everyone/members. serve warns about unknown targets. docs/05 records the sign-in-only limit. All eight criteria met.
