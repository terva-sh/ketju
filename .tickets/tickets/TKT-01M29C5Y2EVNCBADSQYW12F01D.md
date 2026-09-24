---
schema: 3
id: TKT-01M29C5Y2EVNCBADSQYW12F01D
title: Implement permission evaluation with a per-session cache
type: task
status: ready
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

Resolve a viewer's permissions for a forum as the union of their groups' rows, falling back to global rows, cached per session for about 60 seconds as docs/02 specifies.

Every read and every write in the system passes through this, so it is both the hottest path and the one where a bug is a security bug rather than a glitch. An admin is implicitly a member of every group, which is a rule to implement once here rather than to check at each call site.

The 60-second cache means a permission change takes up to a minute to apply. That is a deliberate trade and it needs saying in the admin UI, not just in a comment.

## Acceptance criteria

- [ ] Evaluation is the union of group rows for the forum, falling back to global rows, with table-driven tests for each permission
- [ ] An admin passes every permission check without needing explicit group rows
- [ ] A restricted forum is invisible rather than forbidden to a viewer without read, and a test covers the difference
- [ ] Cached decisions live at most 60 s (docs/02) and any group or permission write invalidates the cache at once; a test shows a revoked permission denied on the next request

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

The cache TTL is 60 s, as docs/02 "Read a thread page" step 3 and this ticket already say, and any group or permission write invalidates it at once. Grooming briefly wrote 30 s into the criteria; that was a mistake and has been corrected.

### Approach
1. New package `internal/perm`. It needs only the DB, so it is not a `service/` package in the docs/02 sense. Types:
   - `perm.Permission`, an alias of the BYYHG constants: read, create_thread, reply, upload, moderate.
   - `perm.Decision`, one of Allow, Deny, or Hidden.
   - `perm.Evaluator` holds the DB, a clock, and the cache.
2. Entry points:
   - `Check(ctx, v domain.Viewer, forumID int64, p Permission) (Decision, error)` is the primitive.
   - `Can(...) (bool, error)` is `Check(...) == Allow`.
   - `ReadableForums(ctx, v) ([]int64, error)` supports list endpoints.
3. Evaluation:
   - If `v.Role == admin`, return Allow without touching the DB. This implements "an admin is implicitly a member of every group" once, here.
   - Otherwise, build the viewer's group set. Anonymous viewers get {everyone}. Active members get {everyone, members} plus their `group_members` rows. The implicit everyone and members membership depends on the BYYHG open item.
   - Load the permission rows for the forum and the global rows. The result is the union over the viewer's groups of the forum rows. If there are no forum rows for any of the viewer's groups, fall back to the global rows. Confirm at claim whether "falling back" means per-group or per-forum. docs/04's one sentence allows both. The candidate reading is per-forum: a forum with any rows is governed only by its rows.
4. Invisible versus forbidden: a forum with `visibility = 'restricted'` where the viewer lacks `read` returns Hidden for every permission, and callers answer 404 (`problem.CodeNotFound`). Otherwise a missing permission is Deny (403). The docs do not say how `public` and `members` visibility interact with the read rows. The proposal: visibility is a coarse gate evaluated before the rows (members means anonymous gets Deny), and the rows then decide. Record whichever reading is chosen in docs/04 "permissions".
5. Cache: a map keyed by `v.SessionKey` (all anonymous viewers share one key). Each entry holds the viewer's group set and the per-forum permission sets computed so far, an expiry of now+60 s, and the generation at fill time. An atomic generation counter is bumped by `Invalidate()`. Expired or stale-generation entries are recomputed. A sweep on each fill drops expired keys, so memory is bounded by active sessions.
6. Invalidation by construction: the mutations that change evaluation are exposed as `Evaluator` methods that write and then `Invalidate()`: permission rows, group membership (including the idp replace from TKT-01M29CKRF9), forum visibility and parent, and `users.role`. Mutation queries for these tables are not called from outside `perm`. A layering test greps the generated `dbq` call sites to enforce that.

### Files and packages
New: `internal/perm/{perm.go,cache.go,mutate.go,perm_test.go,cache_test.go,layering_test.go}`, and `internal/domain/viewer.go` if no other ticket has created it (see the interfaces file). Reuses the BYYHG queries through `dbq`, and `db.DB.Read` and `db.DB.Write`.

### Tests
- AC1: a table-driven `TestUnionWithGlobalFallback` over each of the five permissions × {forum rows only, global rows only, both, neither} × {anonymous, member, member of an extra group}. Expected results are written out by hand.
- AC2: `TestAdminPassesEverything` uses an admin with zero group rows and a restricted forum, and every permission must Allow. A query counter asserts zero DB reads.
- AC3: `TestCacheExpiresAt60s` uses a fake clock: a direct row insert that bypasses `Invalidate` is invisible at 59 s and visible at 61 s. `TestWriteInvalidatesImmediately` checks that the same change through an `Evaluator` mutation is visible on the next call.
- AC4: `TestRestrictedIsHiddenNotForbidden` checks that a restricted forum without read gives Hidden, a public forum without reply gives Deny, and `ReadableForums` omits the restricted one.
- Benchmark `BenchmarkCheckCached` covers the hot path, and the result is recorded in the commit body.

### Design
docs/04-data-model.md "forums", "groups, group_members", "permissions". docs/05-identity-and-accounts.md "Roles and groups". docs/02-architecture.md "Read a thread page" step 3.

### Interactions
Consumes the BYYHG schema and queries. Consumes `domain.Viewer`, which is produced by the identity lane's session middleware (TKT-01M29C5Y3E). Consumed by TKT-01M29CA9A5 (forums), TKT-01M29CA9AX (thread page), TKT-01M29CA9B9 (create and reply), and the admin screens TKT-01M29CXK1X, which must say "applies within 60 s".

### Open items
docs/05 has a `role` column and also a `moderators` group, and nothing says whether role=moderator implies membership of the moderators group, or grants `moderate` directly. Decide at claim.

### Settled in grooming (2026-09-24)

- Fallback is per group, per forum. A group that has any row for this forum uses those rows. Otherwise it uses its global rows.
- Visibility gates first (docs/04). public: the everyone grant applies. members: needs an active member. restricted: explicit per-forum rows only, and global rows are ignored.
- role=moderator grants moderate globally, for fast checks (docs/05). The moderators group exists for per-forum grants. Neither implies the other.
- TTL is 60 s (docs/02) plus immediate invalidation on writes.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'A bounded lifetime' became a 30 s TTL plus immediate invalidation on group or permission writes. The TTL is a backstop; invalidation is what makes a revocation take effect at once.

**agent:claude/phase1-groom** at 2026-09-24T05:16:10Z

This corrects the grooming note above. The TTL is 60 s, not 30 s. docs/02 (Read a thread page, step 3) and this ticket's description already said 60 s, and the 30 s in the rewritten criterion came from grooming, not from any decision. The criterion now matches the docs. The part of the earlier rewrite that still stands is the immediate invalidation on writes.
