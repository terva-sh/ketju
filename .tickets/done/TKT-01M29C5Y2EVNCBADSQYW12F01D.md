---
schema: 3
id: TKT-01M29C5Y2EVNCBADSQYW12F01D
title: Implement permission evaluation with a per-session cache
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:17:26Z
updated_at: 2026-09-24T08:07:11Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Resolve a viewer's permissions for a forum as the union of their groups' rows, falling back to global rows, cached per session for about 60 seconds as docs/02 specifies.

Every read and every write in the system passes through this, so it is both the hottest path and the one where a bug is a security bug rather than a glitch. An admin is implicitly a member of every group, which is a rule to implement once here rather than to check at each call site.

The 60-second cache means a permission change takes up to a minute to apply. That is a deliberate trade and it needs saying in the admin UI, not just in a comment.

## Acceptance criteria

- [x] Evaluation is the union of group rows for the forum, falling back to global rows, with table-driven tests for each permission
- [x] An admin passes every permission check without needing explicit group rows
- [x] A restricted forum is invisible rather than forbidden to a viewer without read, and a test covers the difference
- [x] Cached decisions live at most 60 s (docs/02) and any group or permission write invalidates the cache at once; a test shows a revoked permission denied on the next request

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands.

### Approach
1. `internal/domain/viewer.go`: `Viewer{UserID, Role, Active, SessionKey}`, `Anonymous()`, `Member()`, and the role constants. No other ticket had created it; TKT-01M29C5Y3E fills it from the session.
2. `internal/perm`: `Decision` (Deny, Allow, Hidden), `TTL = 60 s`, `Evaluator` with `Check`, `Can`, `ReadableForums`, `Invalidate`. The plan's alias `perm.Permission` is dropped; `domain.Permission` is used directly, as one name is better than two.
3. Evaluation (perm.go), in order:
   - an inactive viewer becomes `Anonymous()`;
   - an active admin gets Allow without touching the database;
   - an unknown forum is Hidden, and that answer is never cached, because forum creation does not invalidate;
   - the groups are {everyone}, plus {members} and the stored `group_members` rows for an active member. The ids of everyone and members are looked up by slug, and a failed lookup is retried rather than remembered;
   - visibility gates: members drops everyone; restricted ignores global rows;
   - fallback is per group, and the result is the union over groups;
   - role=moderator adds moderate;
   - restricted without read is Hidden.
4. Cache: keyed by {session, user, role, active}, so a role or status change is a new key. The entry holds per-forum access, an expiry of fill+TTL, and the fill generation.
   - `Invalidate` bumps an atomic generation, then clears the map.
   - A result evaluated across an invalidation is answered but not stored.
   - Each fill sweeps expired entries.
5. Mutations (mutate.go): Grant, Revoke, AddMember, RemoveMember, SyncIdPGroups, SetVisibility and SetRole. Each writes in one transaction, then invalidates after the commit, and does not invalidate on failure.
   - New queries: RemoveMembership, SetForumVisibility, SetUserRole.
   - Forum parent is not an input to evaluation, so SetForumParent is not routed through perm.
6. layering_test.go fails if any non-test Go file outside internal/perm and internal/store/db calls one of the eight decision-changing dbq methods.
7. docs/04 "permissions" records the per-group reading, the moderator role, inactive-as-anonymous, Hidden versus Deny, and that archived is undecided.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'A bounded lifetime' became a 30 s TTL plus immediate invalidation on group or permission writes. The TTL is a backstop; invalidation is what makes a revocation take effect at once.

**agent:claude/phase1-groom** at 2026-09-24T05:16:10Z

This corrects the grooming note above. The TTL is 60 s, not 30 s. docs/02 (Read a thread page, step 3) and this ticket's description already said 60 s, and the 30 s in the rewritten criterion came from grooming, not from any decision. The criterion now matches the docs. The part of the earlier rewrite that still stands is the immediate invalidation on writes.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:43:34Z

From TKT-01M29BYYHG (Add the forums, groups, and permissions schema), 2026-09-24: `dbq.PermissionRows(ctx, {ForumID, GroupIds})` returns the forum's own grants and the global rows (forum_id NULL) for the given group ids, forum rows first. The viewer's groups are the stored ones (`GroupsForUser`) plus the implicit ones, which have no rows: everyone (id 1, every viewer) and members (id 2, every active user). Find them by slug through domain.GroupEveryone and domain.GroupMembers. The everyone read grant applies only where forums.visibility = 'public' (docs/04), which evaluation must apply because the row itself does not say it. Admins pass without rows (docs/05). The vocabulary is domain.Permission and domain.AllPermissions.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:06:56Z

Implementation notes for the next reader.

- **Archived forums are unspecified.** Nothing in docs/04, docs/05 or docs/06 says what `forums.archived` does to permissions: read-only, hidden, or unchanged. Evaluation ignores it, and docs/04 now says so. It is a product decision, so it was left undecided rather than invented here. Filed as a draft.
- **Missing forums are not cached.** Check on a forum id that does not exist answers Hidden and is not cached. Otherwise someone probing id N+1 would keep a new forum hidden from themselves for up to 60 s, because CreateForum does not invalidate. Test: TestMissingForumIsNotCached.
- **Generation order.** access reads the generation before the clock, which is what lets TestInvalidateDuringEvaluationIsNotCached slip an invalidation in deterministically. Invalidate bumps the generation before clearing; TestStaleGenerationIsNotServed covers the window between the two.
- **Mutation check.** 19 mutants across perm.go and mutate.go.
  - 18 are killed.
  - One survives: in evaluate, `v.Member()` changed to `v.UserID != 0`. It is equivalent, because Check has already turned an inactive viewer into Anonymous(), whose UserID is 0.
  - The layering test was checked by adding a stray SetUserRole call in a scratch package; it failed as it should.
- **Benchmark.** BenchmarkCheckCached runs at 56 ns/op with 0 allocs on this machine.
- **Admin screens.** TKT-01M29CXK1X still has to say "applies within 60 s" for any change made outside perm. Every change made through perm applies at once.

## Summary

Landed as `internal/perm` and `internal/domain/viewer.go`.

- **Evaluation.** The result is the union over the viewer's groups, with per-group fallback to global rows. Visibility gates first: a members forum ignores everyone, and a restricted forum ignores global rows. An active admin is allowed everything without touching the database. An inactive viewer is evaluated as anonymous. role=moderator adds moderate.
- **Decisions.** A restricted forum without read is Hidden (404), and so is an unknown forum. Anything else missing is Deny (403).
- **Cache.** Decisions are cached per session for 60 s. Every decision-changing write goes through an Evaluator method that invalidates on commit, and a layering test keeps those dbq calls out of every other package.
- **Tests and measurement.** Table-driven tests cover all five permissions. A cached check costs 56 ns/op with 0 allocs. docs/04 "permissions" records the rules.
- **Open question.** Archived-forum semantics are undecided; filed as draft TKT-01M39788AT.
