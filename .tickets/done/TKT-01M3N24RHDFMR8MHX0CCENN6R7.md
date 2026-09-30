---
schema: 3
id: TKT-01M3N24RHDFMR8MHX0CCENN6R7
title: Make archived forums read-only in permission evaluation
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:182
    path: null
claim: null
archive: null
created_at: 2026-09-28T22:28:37Z
updated_at: 2026-09-30T04:49:40Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

An archived forum is read-only (docs/04 "permissions", decided 2026-09-28 in TKT-01M39788AT, Decide what an archived forum does to permissions). `read` is unchanged. `create_thread`, `reply` and `upload` are denied to everyone except admins, and that includes moderators.

`internal/perm` reads the forum row already, so the evaluation change is small. `POST /forums/{slug}/threads` answers `conflict` for an archived forum to everyone today (docs/07). It must let an admin through, and must keep answering `conflict` for everyone else, so that the refusal still says why.

## Acceptance criteria

- [x] In an archived forum perm.Check denies create_thread, reply and upload to anonymous viewers, members, role moderators and moderators-group members, leaves read and moderate unchanged, and allows all five to an admin; CanEach agrees
- [x] A member's edit, delete and restore of their own post in an archived forum answer conflict and append nothing
- [x] perm.Evaluator.SetForumArchived writes forums.archived and invalidates cached decisions, so a cached reply Allow is Deny on the next check
- [x] docs/04 and docs/07 describe the behaviour as built
- [x] POST /forums/{slug}/threads and POST /threads/{id}/posts in an archived forum answer conflict to every signed-in non-admin and succeed for an admin; a restricted archived forum stays not-found to a viewer without read
- [x] The posting refusal reads forums.archived on every request, so an archive written outside SetForumArchived refuses the next post at once (TestArchivedRefusalIgnoresAStaleCache)
- [x] In an archived forum a moderator can still hide, restore, lock, pin and move a thread out, and moving a thread in is conflict to everyone but an admin

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a. Revised 2026-09-29 at claim, against main at 43a844d.

### Settled in grooming

- **An archived forum is read-only.** This is the maintainer's decision of 2026-09-28, on TKT-01M39788AT (Decide what an archived forum does to permissions) and in docs/04 "permissions". `read` is unchanged. `create_thread`, `reply` and `upload` are denied to everyone except admins, moderators included. That matches an archived thread (docs/03 `ThreadArchived`), except that an admin may still post. Hidden-from-non-moderators lost because it makes archiving a delete in disguise. Listing-flag-only lost because "archived" would promise members nothing.
- **The refusal says why.** `POST /forums/{slug}/threads` answers `conflict` for an archived forum today (docs/07; `forumForThread` in `internal/service/threads/create.go`). It must keep answering `conflict` to non-admins and let an admin through. Replying follows the same rule, per the ticket's second criterion.
- **Moderation stays open in an archived forum.** Maintainer decision 2026-09-29, accepting the grooming recommendation. Moderators can still hide, restore, lock, pin and move threads out of an archived forum, and set slow mode there. Only posting is refused (`create_thread`, `reply`, `upload`, and an author's edit, delete and restore of their own post). Moving a thread into an archived forum stays admin-only (TKT-01M29CKRH9). Refusing every write to non-admins lost, because a moderator may still need to hide something found later or rescue a thread out of an archived forum. So `readOnlyForum` guards only the posting paths, and no moderation path calls it.
- **Admins pass in `perm.Evaluator.Check` before evaluation** (`internal/perm/perm.go`, "An active admin passes every check"). The admin exemption therefore needs no special case in the evaluator.

### Approach

Revised at claim. What changed from the groomed plan, and why, is marked "Revised".

1. **Evaluation.** `ForumAccessRows` in `internal/store/db/queries/forums_groups.sql` also selects `f.archived`. Revised: the rule goes in `decide` (`internal/perm/perm.go`), not in `evaluateMany`. `decide` is where the rules live for `Check`, `CanEach` and the admin grant `Preview` alike, so a preview of a grant in an archived forum reports what `Check` will answer. `collect` carries `archived` into `forumRows`. In an archived forum `decide` deletes `create_thread`, `reply` and `upload` from `a.perms` after the moderator-role grant, and leaves `read` and `moderate` alone. Admins pass before `decide` runs. Regenerate dbq with `go generate ./internal/store/db`.
2. **Cache invalidation.** `perm.Evaluator.SetForumArchived(ctx, forumID int64, archived bool)` in `internal/perm/mutate.go`, beside `SetVisibility`, through `write`, so it commits and then calls `Invalidate`. `Invalidate` bumps the generation that every Evaluator over the same `*db.DB` shares (`generationFor`), so every Evaluator in the process goes stale, not only the writer. New query `SetForumArchived`, added to `decisionWrites` in `layering_test.go` so nothing outside `perm` calls it. The admin forum screens (TKT-01M29CXK19) call the method. Nothing writes `forums.archived` today.
3. **Refusals that say why.** `access` in `internal/service/threads/access.go` reads the thread's forum row in the same read transaction as the thread row and records `forumArchived`. One helper, `readOnlyForum(a access) error`, answers `conflict` when the forum is archived and the viewer is not an active admin. It guards the posting paths only:
   - `Reply`, before `!a.canReply`;
   - `authorize`, when the viewer is the post's author, before the moderator early return. So an author's edit, delete and restore of their own post are `conflict`, a moderator's own post included, since a moderator may not post there either. Revised: a moderator acting on somebody else's post (hide, restore, edit) is moderation and is not refused, which is how the settled "moderators can still hide, restore" is built;
   - `forumForThread`: the archived-and-not-admin check moves before the `create_thread` check, so a non-admin gets `conflict` rather than `forbidden`, and an admin is let through.
   The refusal does not depend on the decision cache. `readOnlyForum` reads `forums.archived` from the row `access` loads on every request, so an archive written by another process, which no generation counter reaches, refuses the next post at once. A cached `reply` Allow can only turn `forbidden` into `conflict`; it cannot let a post through. A hidden forum stays `not-found` first, and an anonymous viewer stays `unauthenticated`, so being archived discloses nothing to anyone who could not see the forum.
4. **Viewer block.** Revised: `can_reply` in `View` also requires `readOnlyForum` to pass, so the thread page agrees with the refusal even while a stale cached Allow lives.
5. **Moderation paths.** `moderate` (lock, pin), a moderator's delete and restore of another's post, and the suspension hide do not call `readOnlyForum`. Revised: move does not exist on main yet. TKT-01M29CKRH9 (Implement thread rename and move) owns the move route, and its own criterion already makes an archived destination `conflict` to everyone but an admin. This ticket cannot test moving out or in; that half of criterion 7 stays unticked with a note naming TKT-01M29CKRH9.
6. **Docs.** docs/04 "permissions" says the rule is built and names `SetForumArchived`. docs/07's `POST /forums/{slug}/threads`, `POST /threads/{id}/posts` and edit, delete and restore rows say an archived forum is `conflict` except to an admin.

### Tests

- `internal/perm`: `TestArchivedForumIsReadOnly`. For a member, a moderator by role, a `moderators`-group member and an anonymous viewer on a public archived forum: `read` is unchanged, `create_thread`, `reply` and `upload` are `Deny`, and `moderate` is unchanged. An admin is `Allow` on all five. `CanEach` agrees with `Check`.
- `TestSetForumArchivedInvalidates`: two Evaluators over the same database each cache a `reply` Allow. `SetForumArchived` through the first makes the second answer Deny on its next check. `SetForumArchived` joins `TestEveryMutationInvalidates`.
- `TestArchivedRefusalIgnoresAStaleCache`: with a `reply` Allow cached, archive the forum by a direct `UPDATE forums`, bypassing `SetForumArchived`. The member's next `POST /threads/{id}/posts` is `conflict` and appends nothing.
- `TestModeratorsStillModerateInArchivedForums`: in an archived forum a moderator can hide and restore a member's post, and lock and pin a thread. Each writes its record.
- `internal/web/api`: `TestArchivedForumConflicts`. `POST /forums/{slug}/threads` and `POST /threads/{id}/posts` are `conflict` for a member and for a moderator, and 201 for an admin. A member's `PATCH`, `DELETE` and `…/restore` of their own post are `conflict`. `GET` of the thread and the list are unchanged. A restricted archived forum stays `not-found` to a viewer without read. Every refusal leaves the log unchanged.

### Out of scope

- The admin screen that archives a forum: TKT-01M29CXK19, which calls `SetForumArchived`.
- Archiving a thread (`ThreadArchived`): an admin action with no ticket in this group.
- Upload: the permission is denied here, and the upload route (TKT-01M29CXK0K, phase 3) inherits it.
- Moving a thread out of or into an archived forum: TKT-01M29CKRH9, which owns the move route and already carries the archived-destination rule.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:07:51Z

Open decision for the maintainer, from Phase 2 grooming on 2026-09-29: are moderation writes (lock, pin, rename, move out, hide, restore, slow mode) allowed in an archived forum? The 2026-09-28 decision names only the posting permissions, but also says an archived forum matches an archived thread, which is read-only to moderators. Options: let moderators moderate and refuse only posting (recommended: a moderator may still need to hide something or move a thread out, and moving into an archived forum stays admin-only); or refuse every write to non-admins. The plan's Open decision section carries the same.

**agent:claude-code/phase2-groom** at 2026-09-29T05:39:01Z

Maintainer decision, 2026-09-29: in an archived forum moderators can still hide, lock, pin and move threads out; only posting is refused, and moving a thread in stays admin-only. This accepts the grooming recommendation and supersedes the open decision in the earlier note. Moved into the plan's Settled section.

**agent:claude-code/phase2-run-n24rhd** at 2026-09-30T00:29:41Z

Criterion 7 is left unticked. The hide, restore, lock and pin half is built and tested (TestModeratorsStillModerateInArchivedForums: a moderator's hide and restore of a member's post, lock, pin and unlock in an archived forum each append). The move half cannot be met here: no move route exists on main at 43a844d. TKT-01M29CKRH9 (Implement thread rename and move) owns it, and its own criterion already requires a move into an archived destination to be conflict to everyone but an admin, with moving out a moderator's to do (its Settled section cites this ticket). When it lands, its tests cover the rest of this criterion; nothing in this ticket blocks it, and no posting guard (readOnlyForum) sits on any moderation path it would use.

Decisions taken inside the settled rules, for the record:
- The archived rule lives in perm's `decide`, not `evaluateMany`, so the grant Preview agrees with Check and CanEach.
- In an archived forum a moderator acting on somebody else's post (hide, restore, edit) is moderation and is allowed. A moderator acting on their own post is posting and is conflict, like a member's. The settled list refuses "an author's edit, delete and restore of their own post" and does not exempt moderators from posting.
- The viewer block's `can_reply` also follows the row-read archived flag, so the page does not offer a reply the server will refuse while a stale cached Allow lives.
- Draft saving (`PUT …/draft`) needs `reply`, so it is now forbidden in an archived forum through perm; it was not given its own conflict because a draft is not a post.

**agent:claude-code/phase2-run-n24rhd** at 2026-09-30T00:29:50Z

Correction to the previous note: the draft route is PUT /api/v1/drafts/{thread_id}, not PUT …/draft. The behaviour described is unchanged.

**agent:claude-code/phase2-run-krh9** at 2026-09-30T01:11:19Z

TKT-01M29CKRH9 (Implement thread rename and move) completes this ticket's seventh criterion, the move half it left open. POST /api/v1/admin/threads/{id}/move lets a moderator move a thread out of an archived forum, and moving one into an archived forum is conflict to everyone but an admin. The destination's flag is read from its forum row on the request; the move does not call readOnlyForum. TestMoveNeedsModerateInBothForums in internal/web/api/rename_test.go covers both directions. The box here is left as it was, since this ticket is done; this note records where the criterion was met.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T04:49:40Z

Criterion 7 ticked by the Phase 2 run orchestrator on 2026-09-30. The move half landed with TKT-01M29CKRH9 (Implement thread rename and move), PR #193 at 052db1d. Moving a thread out of an archived forum works for a moderator, and moving one in is conflict except for an admin. The archived-forum moderation paths (hide, restore, lock, pin) were already unguarded here. The rename lane noted the same on this ticket.

## Summary

An archived forum is read-only to everyone but admins. perm drops create_thread, reply and upload for an archived forum in `decide`, so Check, CanEach and the grant Preview agree; read and moderate are unchanged. `perm.Evaluator.SetForumArchived` is the one write of forums.archived and clears every Evaluator's cache over the database. The posting paths read forums.archived from the forum row on every request (`readOnlyForum` in internal/service/threads/access.go) and answer conflict to non-admins, whatever the cache holds: a reply, a new thread (checked before create_thread), and an author's edit, delete or restore of their own post. Moderation paths (hide, restore, lock, pin) are not guarded. docs/04 and docs/07 describe it. Criterion 7's move half is left to TKT-01M29CKRH9 (Implement thread rename and move), which owns the move route and carries the archived-destination rule.
