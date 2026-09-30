---
schema: 3
id: TKT-01M3RQP0TGP43KRWGW11XSN2T7
title: Let forum moderators by grant lock, pin, move and set slow mode
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:moderation
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-30T08:42:45Z
updated_at: 2026-09-30T09:37:16Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, not the role (docs/08, "Decided 2026-09-30"). A member who holds `moderate` in a forum through a group already works that forum's queue (`moderatesGate`, TKT-01M29CRJ4E) and hides and restores posts. The role-based `staffGate` still refuses them these routes:

- `POST /api/v1/admin/threads/{id}/lock`, `/unlock`, `/pin` and `/unpin` (`internal/web/api/moderate.go`, TKT-01M29CKRGS)
- `POST /api/v1/admin/threads/{id}/move` (TKT-01M29CKRH9)
- `PUT /api/v1/admin/threads/{id}/slow-mode` (`internal/web/api/slowmode.go`, TKT-01M29CRJ50)

Each service already requires `moderate` in the thread's forum, and move requires it in both forums. So the change is mostly the route gate, plus the docs/07 wording ("staff's by role") and tests.

Admin screens and account actions keep their role gates: users, applications, suspension, and the audit log.

This blocks the thread-screen moderator controls. Those controls must be offered to exactly the viewers the server allows, which is `can_moderate` on the thread page.

Filed by the Phase 2 run orchestrator on 2026-09-30.

## Acceptance criteria

- [ ] The lock, unlock, pin, unpin, move and slow-mode routes are registered with moderatesGate, and every other /api/v1/admin/ route keeps the gate the test's table lists for it (TestThreadModerationRoutesUseTheModeratesGate)
- [ ] A member granted moderate on forum A through a group locks, unlocks, pins and unpins with scope forum, sets slow mode, and moves to a second forum they moderate, each 200 with one record or row and one audit row naming them (TestForumModeratorActsInTheirForum)
- [ ] The same member is forbidden all six actions on a thread in a readable forum B they do not moderate, not-found in a forum hidden from them, and forbidden again after the grant is revoked, with nothing appended or audited (TestForumModeratorIsRefusedElsewhere)
- [ ] Move needs moderate in both forums: the member's move from A to B and from B to A is forbidden, and from A to a forum C they moderate is 200 (TestMoveNeedsModerateInBothForums)
- [ ] A member with no grant is forbidden by the gate on all six routes before the body is read, anonymous is 401, a pre-user 403, and moderators and admins by role still pass (TestThreadModerationGate)
- [ ] A global pin, an unpin of a global pin, and a forum pin replacing an active global pin are forbidden to a viewer who is not a moderator or admin by role and write nothing; a forum pin is allowed (TestGlobalPinStaysStaffs)
- [ ] Lock, unlock, pin and unpin go through appendDecided, and slow mode decides at a log length: with a move to a forum the member does not moderate appended and not projected, each is forbidden (TestThreadModerationJudgesAnUnprojectedMove), and none of the member's concurrent locks lands after such a move (TestConcurrentLocksAreDecidedOnTheLog)
- [ ] docs/07 and the comments in moderate.go, slowmode.go and admin.go describe these routes as behind the moderates gate with the forum decided by the service, docs/07 lists the global-pin refusal, and docs/08 Primitives says what moderator means for the thread actions

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at a660ae5 (5124209 plus the run's tracking commits). Read for it: `internal/web/api/admin.go`, `moderate.go`, `slowmode.go` and `admin_queue.go`; `internal/service/threads/moderate.go`, `slowmode.go` and `access.go`; `internal/perm/perm.go` (`Check`, `ModeratesAny`, `ModeratedForums`, `decide`); the tests `admin_test.go`, `moderate_test.go`, `slowmode_test.go`, `rename_test.go` and `archived_test.go`; docs/07 "Moderation and admin"; and docs/08 "Decided 2026-09-30".

**One correction to the description.** It says each service already requires `moderate` in the thread's forum, so the change is mostly the route gate. That holds for move and rename, which decide through `appendDecided` at a log length. It does not hold for lock, unlock, pin, unpin and slow mode. `Service.moderate` and `SetSlowMode` read `access` from the projection row before they write, so a move that lands between the read and the append takes the thread into a forum the actor does not moderate. Until now only staff by role reached these routes, and a moderator by role moderates every forum they can see, so the gap cost nothing. With forum-scoped moderators it is exactly an action outside their forum. So this ticket also moves those five actions onto a decision at a log length, and the effort is nearer M than the `effort:S` label says.

### Settled in grooming

- **Thread moderation follows the forum grant** (maintainer decision 2026-09-30, docs/08 "Decided 2026-09-30"): "Anyone holding `moderate` in a thread's forum, by role or through a group, may lock, unlock, pin, unpin and set slow mode there, and may move a thread when they hold `moderate` in both forums. Admin screens and account actions keep their role gates."
- **What keeps its role gate** (the same decision). Every other `staffGate` route stays on it: `/admin/users` and `/admin/users/{handle}` (`admin_users.go`), suspend, unsuspend and the bulk post hide and restore (`account.go`), and the six application routes (`applications.go`). Every `adminGate` route stays on it, the audit log included (admin-only, maintainer decision 2026-09-29, TKT-01M29CP2R4).
- **Move needs `moderate` in both forums** (settled in grooming for TKT-01M29CKRH9, `Service.Move`), so a moderator of one forum cannot push a thread into a forum they could not move it back out of.
- **Archived forums** (maintainer decision 2026-09-29, TKT-01M3N24RHD, docs/08): moderators may still lock, pin and move threads out, and moving a thread in stays admin-only. `perm.decide` drops only the posting permissions in an archived forum, so a group grant of `moderate` survives there as a role's does. Nothing changes here.
- **Rename is unchanged.** `PATCH /api/v1/threads/{id}` is not an admin route and already admits anyone with `moderate` in the forum (`Service.Rename`, `a.canModerate`).
- **No viewer class or cache key changes.** `access` already makes a group moderator's class `ClassModerator`. Lock, unlock, pin, unpin and move append a record, which moves the log length in `respcache.Key` (`TestLockMovesTheResponseCacheKey`, `TestRenameMovesTheResponseCacheKey`). Slow mode is in the uncached viewer block (`TestSlowModeIsInTheViewerBlockNotTheCache`).

Engineering choices made here, each with the alternative that lost:
- **The six routes move to `moderatesGate`; no new gate.** `moderatesGate` admits an active member who moderates somewhere (`perm.ModeratesAny`), and the service then decides the forum. Three alternatives lost:
  - A per-thread gate that reads the thread's forum and checks `moderate` there. It would duplicate the service's check, and it would read the thread before `access` does, so it could answer `forbidden` for a thread that `access` answers `not-found`.
  - Moving the routes out of `/api/v1/admin/`. That breaks the paths the client tickets name, and `TestAdminPathsGoThroughAdminRoute` exists so that no admin route escapes a gate.
  - Keeping `staffGate` and widening `staff` to group moderators. That would open every account and application route too, which the decision rules out.
- **A global pin stays staff's by role.** `ThreadPinned` with scope `global` heads the thread list of every forum whose readers can read the thread (`views.PinnedThreads`, `MaxPinned` 50). So a global pin, unpinning one, and replacing one with a forum pin each change what other forums show, and they are not moderation "there". `Pin` and `Unpin` refuse them with `forbidden` to a viewer who is not `domain.Viewer.Staff()`. A forum moderator may still pin and unpin with scope `forum`. Allowing global pins to any forum moderator lost: a moderator of one forum could put up to 50 threads above every other forum's list, which is an action outside the forums they moderate. The decision says "there", and this reading keeps global pins where they are today. It is recorded in this ticket's note as a choice the maintainer may revisit.
- **Lock, unlock, pin and unpin decide at a log length, through `appendDecided`.** `moderate` keeps its refusal order and becomes a wrapper around `appendDecided`, whose `decide` checks `a.canModerate`, then `th.Archived()`, then calls the action's own decide. `appendDecided` projects the thread up to the log length L, admits the viewer from that row, and appends at L, and a record landing in between fails the append with `threadlog.ErrLogMoved` and is decided again. Keeping `appendEvent` lost for the reason above. A side effect is that two lock requests at once no longer both append: the second is decided again and answers `conflict`. `moderate`'s comment accepting the redundant record is rewritten.
- **Slow mode decides at a log length too.** It writes a database row and no record, so it cannot append at L. `SetSlowMode` instead runs inside `Threads.With`: it reads L, projects to L (`projectNow`, skipped for a quarantined thread as `appendDecided` does), and admits the viewer from that row through `access`. It checks `canModerate`, `th.Archived()` and the range, then writes the row and its audit row in one `DB.Write`. The transaction checks `th.Actor.LogLength() == L` before it returns and answers `threadlog.ErrLogMoved` otherwise, which rolls it back and is decided again, up to `threadAttempts`. A move that lands after that check is ordered after the slow mode change. The helper `admitAt(ctx, v, id, th) (access, int64, error)` is extracted from `appendDecided`'s loop body, and both callers use it. Recording slow mode in the log lost: docs/08 "Primitives" keeps it out of the frozen format.

### Why nobody gains an action outside the forums they moderate

1. **The gate performs nothing.** `moderatesGate` admits a member with `moderate` in at least one forum. Past it, the handlers only parse the path and the body. A `bad-request` answer says nothing about the thread.
2. **Every action re-checks the thread's forum where the record lands.**
   - Lock, unlock, pin and unpin: `appendDecided` gives `forbidden` when `access(...).canModerate` is false. `canModerate` is `perm.Can(v, row.ForumID, moderate)` on the row projected to the length the record is appended at.
   - Move: the same, and then `perm.Can(v, dest.ID, moderate)` on the destination. An archived destination is refused to all but an admin.
   - Slow mode: the same check on the row projected to L, with the length checked again in the transaction.
3. **Visibility comes first.** `access` answers `not-found` for a thread in a forum hidden from the viewer before `moderate` is asked, as it does for every read.
4. **Revocation is immediate.** `perm.Revoke` and every membership write bump the generation, so the next request sees the grant gone, at the gate and in the service (`TestModeratesGate`).
5. **The one cross-forum effect is refused.** A global pin changes other forums, so it stays staff's, as above.
6. **Inactive and pre-user viewers get nothing.** `member.Require` in the gate and `MayWrite` in the service refuse an anonymous viewer, a suspended or deactivated one (evaluated as anonymous), and a pre-user.

### Approach

1. **Gates.** In `internal/web/api/moderate.go` `registerModeration`, and in `slowmode.go` `registerSlowMode`, change `staffGate` to `moderatesGate` on the six routes. Rewrite their comments: "staff's by role" becomes "behind the moderates gate, and the threads service then decides the forum at the log length". In `admin.go`, the gate comment's "Only the moderator queue uses it" becomes the queue and the thread moderation routes.
2. **`internal/service/threads/moderate.go`.**
   - Extract `admitAt` from `appendDecided`.
   - Rewrite `moderate(ctx, v, id, decide func(a access, now time.Time) (threadlog.Append, error))` to call `s.MayWrite(v)` and then `s.appendDecided` with a decide that checks `!a.canModerate` (forbidden), then `th.Archived()` (conflict), then calls `decide(a, now)`. `appendDecided` sets the flags, the author, the time and `AtLength`.
   - In `Pin` and `Unpin`, first in each decide, add `staffPin(a, scope, now)`. It returns `forbidden` when `!a.viewer.Staff()` and either `scope == PinGlobal` or `pinActive(a.row, now) && a.row.PinnedScope.String == PinGlobal`. Document it on both functions.
3. **`internal/service/threads/slowmode.go` `SetSlowMode`**: restructure as above. The refusal order is unchanged: `forbidden`, then archived `conflict`, then `validation-failed`, then same-value `conflict`, all before anything is written. After `threadAttempts` moves it answers `conflict`, as `appendDecided` does.
4. **docs/07.** In "Locking and pinning threads", replace "Each route is staff's by role" with the `moderates` gate and the forum check at the log length. Add the global-pin refusal to the refusal list, after the archived-thread `conflict`: "`forbidden` for a pin or unpin with scope `global`, or a pin that would replace an active global pin, to a viewer who is not a moderator or admin by role". In "Slow mode", "The gate and the `moderate` check are the same as for locking and pinning" then holds as written. In "Renaming and moving threads", change "behind the staff gate, as lock and pin are" to the `moderates` gate. In "The moderator queue", change "Both routes are behind the `moderates` gate" to say the thread routes use it too.
5. **docs/08 "Primitives".** Below the table, change "Every "who" above names an account holding that role" to say that for the thread actions, lock, pin, move and slow mode, "moderator" means `moderate` in the thread's forum, by role or through a group (decided 2026-09-30), and that a global pin is staff's by role.

### Tests

All in `internal/web/api`, through the real chain (`w.chain()`), with gina a member of the `moderators` group granted `moderate` in `general` (and `den` where named) through `w.groupModerator`, carol a member with no grant, and bob a moderator by role.
- `TestThreadModerationRoutesUseTheModeratesGate` (new, `admin_test.go`): the admin route list has `moderates` on exactly the two queue routes and the six thread routes (lock, unlock, pin, unpin, move, slow-mode). Every route under `/api/v1/admin/users` (the agent routes included), `/admin/applications`, `/admin/groups`, `/admin/permissions` and `/admin/forms` keeps the gate listed for it in the test's table. A route missing from the table fails the test, so a later route, such as the audit log's, must be given a gate on purpose, and widening any other route fails it. `TestAdminRoutesAreGated` walks the six routes with their new gate, unedited.
- `TestForumModeratorActsInTheirForum` (new, `moderate_test.go`): gina locks, unlocks, pins with scope `forum`, unpins, and sets slow mode to 30 on a thread in `general`. She then moves it to `den`, which she also moderates. Each answers 200. Each appends exactly one record, or writes one `thread_slow_mode` row, and writes one audit row whose actor is gina. The page's ETag and the response cache key change after the lock.
- `TestForumModeratorIsRefusedElsewhere` (new): on a thread in `lounge`, a public forum gina reads but does not moderate, each of the six routes answers 403 `forbidden`. A thread in a restricted forum hidden from her answers 404 `not-found`. None changes the log, the slow mode row or `audit_log` (`w.unchanged`). After `perm.Revoke` of her grant, her next lock in `general` is 403.
- `TestMoveNeedsModerateInBothForums` (extended): the "gina through the gate" case now reaches the service and is still `forbidden`, because she lacks `moderate` in `lounge`, and its comment says so. New cases: gina moves `general` to `den` with 200, and gina moving a thread in `lounge` to `general` is `forbidden`.
- `TestThreadModerationGate` (new, table over the six routes): anonymous 401; a pre-user 403; carol 403 from the gate, before the body is read; gina in her forum 200; bob and an admin 200.
- `TestGlobalPinStaysStaffs` (new): each of these is 403 for gina and writes nothing: a pin with scope `global`; after bob pins globally, an unpin of `global`; and after bob pins globally, a pin with scope `forum` that would replace it. After bob unpins, gina's pin with scope `forum` is 200. An admin's pin with scope `global` is 200.
- `TestThreadModerationJudgesAnUnprojectedMove` (new, one subtest per action), after `TestRenameJudgesAnUnprojectedMove`: a thread in `general` has a `ThreadMoved` to `lounge` appended to its log and not projected. gina's lock, unlock (on a locked thread), pin, unpin (on a pinned thread) and slow mode are each `forbidden` and write nothing. Judged from the row as it stood, each would have passed.
- `TestConcurrentLocksAreDecidedOnTheLog` (new), after `TestConcurrentMovesAreDecidedOnTheLog`: 50 rounds of bob moving a `general` thread to `lounge` while gina locks it. In the log, every `ThreadLocked` whose author is gina precedes the `ThreadMoved`.
- Unchanged and still green: `TestModerationRefusals`, `TestSetSlowModeRefusals` (its "a member" row stays 403; its comment "Past the staff gate" is reworded), `TestModeratorsStillModerateInArchivedForums`, `TestModeratesGate`, `TestForumScopedModeratorUsesQueue`, `TestLockMovesTheResponseCacheKey`, and `TestAuditRowsComeFromTheLog` in `internal/events/projection`.
- The invariants the brief names: no session is issued and no role changes, so the chokepoint and `Promote` tests are untouched. No record type is added. No new viewer class is created: the response cache test for these records is `TestLockMovesTheResponseCacheKey`, and `TestForumModeratorActsInTheirForum` repeats its check for gina.

### Out of scope

- The thread screen's controls: TKT-01M3QGH66W (lock and pin), TKT-01M3QXV399 (rename and move) and TKT-01M3R0PM0K (slow mode). Each depends on this ticket and offers its controls on `viewer.can_moderate`.
- The moderator queue, which already uses `moderatesGate` (TKT-01M29CRJ4E).
- A moderator's edit, delete and restore of posts, which are thread routes and not admin routes, and already follow `moderate` (TKT-01M29CRJ5J).
- Account actions, applications, users and the audit log, which keep their role gates by the decision.
- Letting a forum moderator pin globally. That would need a new maintainer decision.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T09:16:55Z

Planned 2026-09-30 (TKT-01M3M9RR26) against main at a660ae5. Two findings widen the ticket beyond the gate swap, so effort is nearer M than the effort:S label. (1) Service.moderate (lock, unlock, pin, unpin) and SetSlowMode decide from the projection row before writing, so a move landing in between lets a forum moderator act on a thread that has just left their forum. The plan moves the four onto appendDecided and has slow mode decide at a log length. (2) A global pin heads every forum's thread list, so it is not moderation 'there'. As an engineering choice under the security rule, the plan keeps scope global, an unpin of a global pin, and replacing a global pin staff's by role. This keeps global pins as they are today. The maintainer may revisit it; allowing it would let a one-forum moderator put up to 50 threads above every forum's list.
