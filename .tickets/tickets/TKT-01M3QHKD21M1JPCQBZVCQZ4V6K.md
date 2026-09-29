---
schema: 3
id: TKT-01M3QHKD21M1JPCQBZVCQZ4V6K
title: Show moderators' post controls and the edited-by-a-moderator marker
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: TKT-01M29CRJ5J9XMJZ30FMZX4D473
dependencies:
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
  - TKT-01M3PTT97XM3DCGRTDNHHBNM5M
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:37:14Z
updated_at: 2026-09-29T22:49:17Z
created_by:
  id: agent:claude-code/phase2-run-crj5j
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The server side of TKT-01M29CRJ5J (Allow moderators to edit and delete any post) lets a viewer with `moderate` in the forum edit, delete and restore any post, and serves `edited_by_moderator` on `dto.Post`. The client does not use either yet, because the Phase 2 run's server lane may not change the committed `internal/web/spa/dist`.

This is step 8 of that ticket's plan:
- `Post.tsx` offers Edit, Delete and Restore on every post to a viewer with `viewer.can_moderate`, not only on the viewer's own posts. A moderator's Restore applies to a post that is deleted or hidden by a moderator.
- A post with `edited_by_moderator: true` shows "(edited by a moderator)" where it would show "(edited)". It does not name the moderator; the post's history does (docs/08 "Post history").
- Regenerate `internal/web/spa/dist` with `go generate ./internal/web/spa`.

The tombstone wording and the author's view of their own hidden post are TKT-01M29CKRJ9, not this ticket.

## Acceptance criteria

- [ ] With viewer.can_moderate, the shared postControls rule from TKT-01M3PTT97X offers Edit and Delete on every visible, unmoved post and Restore on every deleted or hidden, unmoved post, at any age, and a member still gets only their own (postControls.test.ts)
- [ ] A moderator's delete of another member's post says that members will see it removed by a moderator and only a moderator can restore it, and delete and restore call the existing endpoints (PostControls.test.tsx)
- [ ] A post with edited_by_moderator true shows (edited by a moderator) in place of (edited), naming nobody (ThreadPage.test.tsx)
- [ ] The thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 1dd6ed9. The server half is merged: TKT-01M29CRJ5J (Allow moderators to edit and delete any post), PR #165. This ticket is its plan step 8.

**It shares one set of controls with TKT-01M3PTT97X** (Offer edit and delete only inside the author windows in the client). On main no client code offers Edit, Delete or Restore on any post. That ticket builds them: the offer rule `postControls` in `components/thread/postControls.ts`, the footer buttons in `Post.tsx`, Restore on `Tombstone.tsx`, `PostEditor.tsx`, and the refetch after a change. This ticket depends on it, widens the rule to moderators, and adds the marker.

### Settled in grooming

- **What a moderator may do** is `authorize` in `internal/service/threads/mutate.go` as merged: a viewer with `moderate` in the thread's forum (`access.canModerate`, which is `viewer.can_moderate` on the page) may edit, delete or restore any post, is bound by no window, and needs no `reply`. An archived thread is `conflict` to everyone, and a lock refuses only members' edits (docs/07 "Edit, delete and restore", docs/08).
- **The post states each action takes** (TKT-01M29CRJ5J): Edit and Delete refuse a post that is gone (deleted, hidden by a moderator, or moved). Restore takes a post that is deleted or hidden by a moderator (`RequireAny: EntryDeleted|EntryHiddenByMod`) and refuses a moved one.
- **Only a moderator restores a moderator's delete** (decided 2026-09-28, docs/08 "Restoring a moderator's delete"). A moderator's delete of someone else's post writes `Delete{by_moderator: true}`, which hides the post (hidden-by-mod) rather than deleting it.
- **The marker names nobody.** `dto.Post.edited_by_moderator` is true when the post shows as edited and its current revision's actor is not its author. The post shows "(edited by a moderator)" where it would show "(edited)". The moderator's handle is in the post's history, which is public by default (docs/08 "Post history"). The editor is not in the response cache's authors digest, which is why the page does not name them (TKT-01M29CRJ5J, settled there).
- **Moderators see a removed post's content inline** (docs/08 "Tombstones"): the page serves a moderator the post with its `html` and `deleted: true` (`HiddenEntry` in `view.go`). The wording and styling of that view, and of the tombstones, are TKT-01M29CKRJ9, which is being built now.
- **No viewer class or cache key changes.** `edited_by_moderator` is read from the frame, which the log length already keys. `TestModeratorHideMovesTheResponseCacheKey` (TKT-01M29CRJ5J) holds the cache invariant for the hide.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. The thread view stays under `threadViewGzipBytes` (60000).

Engineering choices made here, each with the alternative that lost:
- **Widen the shared rule; do not add a second one.** `postControls` gains one branch: when `viewer.can_moderate`, the viewer's own-post condition is dropped and no window applies (`author_windows` is null for such a viewer anyway). Edit and Delete are offered on a post that is not deleted and has `moved_to` null, and Restore on a post that is `deleted` and has `moved_to` null, whoever removed it. Building a moderator menu beside the author controls lost: it would duplicate the editor, the confirmation and the refetch.
- **Where a moderator's controls go on a removed post.** The page draws a moderator's copy of a removed post with its content. Restore goes in that post's footer. Until TKT-01M29CKRJ9 lands its moderator inline view, `ThreadScreen` draws every `deleted` post as `Tombstone`, so Restore stays on the tombstone as TKT-01M3PTT97X builds it. Whichever of the two tickets lands second puts Restore on the moderator's inline view.
- **The delete prompt says who will see what.** For a moderator deleting someone else's post: "Remove this post? Members will see that a moderator removed it. Only a moderator can restore it." For one's own post, TKT-01M3PTT97X's words stay. These sentences follow docs/08 and decide nothing new.
- **No reason field.** The routes take an optional `reason`, and docs/08 does not require one for a moderator's edit or delete. A required reason would be a product rule. The history shows the actor either way.

### Approach

1. **Rule** (`components/thread/postControls.ts`): the moderator branch above. It still answers `closesAt: null` for a moderator, so `useWindowClock` sets no timer.
2. **Marker** (`components/thread/Post.tsx`): where the header renders `(edited)` when `post.edited_at !== null`, render `(edited by a moderator)` when `post.edited_by_moderator` is also true. Keep the class `edited` and add `data-edited-by="moderator"`.
3. **Delete prompt** in the shared inline confirmation: the moderator's words when the post is not the viewer's own.
4. **Restore on a moderator-hidden post**: offered by the rule, and sent as `POST …/restore` with no body, as TKT-01M3PTT97X sends it.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- `components/thread/postControls.test.ts`:
  - `a moderator may act on every post`: with `can_moderate: true` and `author_windows: null`, edit and delete are offered on another member's visible post at any age, and restore on another member's deleted post.
  - `nothing on a moved post`: no control on a post with `moved_to` set, for a moderator or an author.
  - `a member still gets only their own`: with `can_moderate: false`, another member's post offers nothing (TKT-01M3PTT97X's case, kept).
- `components/thread/PostControls.test.tsx`:
  - `a moderator's delete of another's post says who sees what` and sends `DELETE` with no body.
  - `a moderator restores a hidden post`: `POST …/restore`, then the page is requested again.
- `screens/ThreadPage.test.tsx`, `the edited marker`: `edited_by_moderator: true` with `edited_at` set shows "(edited by a moderator)" and no handle other than the author's. `edited_by_moderator: false` shows "(edited)".
- The thread-view budget passes.

### Out of scope

- The author's controls, the editor, and the window rule: TKT-01M3PTT97X.
- The post source route that the editor reads: TKT-01M3QJYV4Z.
- Tombstone wording, `removal`, the author's view of their own hidden post, and the moderator's inline view: TKT-01M29CKRJ9.
- Naming the moderator on the page, which the response cache design rules out (TKT-01M29CRJ5J).
- Suspension's bulk hide: TKT-01M29CP2QK.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:25:28Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, on main 1dd6ed9 after PR #165. It depends on TKT-01M3PTT97X, which builds the shared post controls (none exist on main). This ticket widens the offer rule for viewer.can_moderate and adds the marker. Restore on a moderator's inline view of a removed post is placed by whichever of this ticket and TKT-01M29CKRJ9 lands second.
