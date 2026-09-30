---
schema: 3
id: TKT-01M3N24RHDFMR8MHX0CCENN6R7
title: Make archived forums read-only in permission evaluation
type: task
status: in-progress
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
references: []
claim:
  actor: agent:claude-code/phase2-run-n24rhd
  branch: run/n24rhd-archived-forum-readonly
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a5fbd4fb2b15bf24e
  commit: 43a844dd4d572a6c2f4d90c6468acf6fb8061468
  session: null
  claimed_at: 2026-09-30T00:18:29Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:28:37Z
updated_at: 2026-09-30T00:18:32Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-n24rhd
  name: ""
extensions: {}
---

## Description

An archived forum is read-only (docs/04 "permissions", decided 2026-09-28 in TKT-01M39788AT, Decide what an archived forum does to permissions). `read` is unchanged. `create_thread`, `reply` and `upload` are denied to everyone except admins, and that includes moderators.

`internal/perm` reads the forum row already, so the evaluation change is small. `POST /forums/{slug}/threads` answers `conflict` for an archived forum to everyone today (docs/07). It must let an admin through, and must keep answering `conflict` for everyone else, so that the refusal still says why.

## Acceptance criteria

- [ ] In an archived forum perm.Check denies create_thread, reply and upload to anonymous viewers, members, role moderators and moderators-group members, leaves read and moderate unchanged, and allows all five to an admin; CanEach agrees
- [ ] A member's edit, delete and restore of their own post in an archived forum answer conflict and append nothing
- [ ] perm.Evaluator.SetForumArchived writes forums.archived and invalidates cached decisions, so a cached reply Allow is Deny on the next check
- [ ] docs/04 and docs/07 describe the behaviour as built
- [ ] POST /forums/{slug}/threads and POST /threads/{id}/posts in an archived forum answer conflict to every signed-in non-admin and succeed for an admin; a restricted archived forum stays not-found to a viewer without read
- [ ] The posting refusal reads forums.archived on every request, so an archive written outside SetForumArchived refuses the next post at once (TestArchivedRefusalIgnoresAStaleCache)
- [ ] In an archived forum a moderator can still hide, restore, lock, pin and move a thread out, and moving a thread in is conflict to everyone but an admin

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **An archived forum is read-only.** This is the maintainer's decision of 2026-09-28, on TKT-01M39788AT (Decide what an archived forum does to permissions) and in docs/04 "permissions". `read` is unchanged. `create_thread`, `reply` and `upload` are denied to everyone except admins, moderators included. That matches an archived thread (docs/03 `ThreadArchived`), except that an admin may still post. Hidden-from-non-moderators lost because it makes archiving a delete in disguise. Listing-flag-only lost because "archived" would promise members nothing.
- **The refusal says why.** `POST /forums/{slug}/threads` answers `conflict` for an archived forum today (docs/07; `forumForThread` in `internal/service/threads/create.go`). It must keep answering `conflict` to non-admins and let an admin through. Replying follows the same rule, per the ticket's second criterion.
- **Moderation stays open in an archived forum.** Maintainer decision 2026-09-29, accepting the grooming recommendation. Moderators can still hide, restore, lock, pin and move threads out of an archived forum, and set slow mode there. Only posting is refused (`create_thread`, `reply`, `upload`, and an author's edit, delete and restore of their own post). Moving a thread into an archived forum stays admin-only (TKT-01M29CKRH9). Refusing every write to non-admins lost, because a moderator may still need to hide something found later or rescue a thread out of an archived forum. So `readOnlyForum` guards only the posting paths, and no moderation path calls it.
- **Admins pass in `perm.Evaluator.Check` before evaluation** (`internal/perm/perm.go`, "An active admin passes every check"). The admin exemption therefore needs no special case in the evaluator.

### Approach

1. **Evaluation**: `ForumAccessRows` in `queries/forums_groups.sql` also selects `f.archived`. In `evaluateMany`, an archived forum deletes `create_thread`, `reply` and `upload` from `a.perms`. That happens after the moderator-role grant, and leaves `read` and `moderate` alone. The rules stay in one place, for `Check` and `CanEach` alike. Regenerate dbq.
2. **Cache invalidation**: add a mutation method `perm.Evaluator.SetForumArchived(ctx, forumID int64, archived bool)` in `internal/perm/mutate.go`, beside `SetVisibility`. It writes `forums.archived` and `updated_at` and calls `Invalidate` on commit. `Invalidate` bumps the generation counter that every Evaluator over the same `*db.DB` shares (`generationFor` in `internal/perm/perm.go`), so the cached decisions of every Evaluator in the process go stale, not only the writer's. That is what the second-evaluator test below proves (revised for Terva review 1343 on PR #138, finding 2). Add the query `SetForumArchived`. The admin forum screens (TKT-01M29CXK19) call it. Nothing writes `forums.archived` today.
3. **Refusals that say why**: `access` in `internal/service/threads/access.go` also reads the forum row and records `forumArchived`. Add one helper, `readOnlyForum(a access) error`, which answers `conflict` when the forum is archived and the viewer is not an admin. It runs before the permission checks that would otherwise answer `forbidden`:
   - `Reply`, before `!a.canReply`;
   - `Edit`, `Delete` and `Restore`, through `authorize`, for the post's author. An archived forum matches an archived thread, where all three are `conflict` (docs/04, "That matches an archived thread");
   - `forumForThread`: check `forum.Archived` and admin before `create_thread`, so a non-admin gets `conflict` rather than `forbidden`. An admin is let through.
   The refusal does not depend on the decision cache at all. `readOnlyForum` reads `forums.archived` from the row `access` loads on every request, so an archive written by another process, which no generation counter reaches, still refuses the next post at once. A cached `reply` Allow can then only make the refusal `conflict` instead of `forbidden`; it cannot let a post through.
   A hidden forum stays `not-found` first, and an anonymous viewer stays `unauthenticated`, so being archived discloses nothing to anyone who could not see the forum.
4. **Viewer block**: `can_reply` in `View` is already false through the permission. No change is needed.
5. **Docs**: docs/04 "permissions" says the rule is built and names `SetForumArchived`. docs/07's `POST /forums/{slug}/threads` and `POST /threads/{id}/posts` rows say an archived forum is `conflict` except to an admin. That satisfies the third criterion.

### Tests

- `internal/perm`: `TestArchivedForumIsReadOnly`. For a member, a moderator by role, a `moderators`-group member and an anonymous viewer on a public archived forum: `read` is unchanged, `create_thread`, `reply` and `upload` are `Deny`, and `moderate` is unchanged. An admin is `Allow` on all five. `CanEach` agrees with `Check`.
- `TestSetForumArchivedInvalidates`: two Evaluators over the same database each cache a `reply` Allow. `SetForumArchived` through the first makes the second answer Deny on its next check, through the shared generation.
- `TestArchivedRefusalIgnoresAStaleCache`: with a `reply` Allow cached, archive the forum by a direct `UPDATE forums`, as another process would, bypassing `SetForumArchived`. The member's next `POST /threads/{id}/posts` is `conflict` and appends nothing.
- `TestModeratorsStillModerateInArchivedForums`: in an archived forum a moderator can hide and restore a member's post, lock and pin a thread, and move a thread out. Moving a thread in is `conflict` to a moderator and succeeds for an admin.
- `internal/web/api`: `TestArchivedForumConflicts`. `POST /forums/{slug}/threads` and `POST /threads/{id}/posts` are `conflict` for a member and for a moderator, and 201 for an admin. A member's `PATCH`, `DELETE` and `…/restore` of their own post are `conflict`. `GET` of the thread and the list are unchanged. A restricted archived forum stays `not-found` to a viewer without read. Every refusal leaves the log unchanged.

### Out of scope

- The admin screen that archives a forum: TKT-01M29CXK19, which calls `SetForumArchived`.
- Archiving a thread (`ThreadArchived`): an admin action with no ticket in this group.
- Upload: the permission is denied here, and the upload route (TKT-01M29CXK0K, phase 3) inherits it.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:07:51Z

Open decision for the maintainer, from Phase 2 grooming on 2026-09-29: are moderation writes (lock, pin, rename, move out, hide, restore, slow mode) allowed in an archived forum? The 2026-09-28 decision names only the posting permissions, but also says an archived forum matches an archived thread, which is read-only to moderators. Options: let moderators moderate and refuse only posting (recommended: a moderator may still need to hide something or move a thread out, and moving into an archived forum stays admin-only); or refuse every write to non-admins. The plan's Open decision section carries the same.

**agent:claude-code/phase2-groom** at 2026-09-29T05:39:01Z

Maintainer decision, 2026-09-29: in an archived forum moderators can still hide, lock, pin and move threads out; only posting is refused, and moving a thread in stays admin-only. This accepts the grooming recommendation and supersedes the open decision in the earlier note. Moved into the plan's Settled section.
