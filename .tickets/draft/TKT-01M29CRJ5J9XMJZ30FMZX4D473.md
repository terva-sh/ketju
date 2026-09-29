---
schema: 3
id: TKT-01M29CRJ5J9XMJZ30FMZX4D473
title: Allow moderators to edit and delete any post
type: task
status: draft
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
  - TKT-01M29CA9BP9QTMN2JEDPM0YS67
  - TKT-01M29CKRHSMCRM9T440314D2Q8
  - TKT-01M29CKRGSF2FPY73MNNFPQ1FV
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-09-29T05:57:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

A moderator may edit or soft-delete any post, with the Edit or Delete record recording them as the actor rather than the author.

A moderator edit of somebody else's words is the most invasive routine action in the system, and the only thing that makes it acceptable is that it is visible. The actor is in the record, the edit shows in history, and the history is public by default.

## Acceptance criteria

- [ ] A moderator's edit of another member's post appends an Edit whose frame actor is the moderator, the index entry's author is unchanged, and history names the moderator
- [ ] That post is served with edited_by_moderator: true, and the client shows it as edited by a moderator
- [ ] A moderator's edit and delete of a member's post succeed past both author windows
- [ ] Every moderator edit, hide and restore of another member's post writes one audit_log row (post.edited, post.hidden, post.restored) naming the actor and the post
- [ ] A moderator's delete of another's post writes Delete with by_moderator: true and sets hidden-by-mod, not deleted; the author's restore, edit and delete are then forbidden and append nothing; a moderator's restore clears it
- [ ] A page cached before a moderator's hide is not served after it, and members then get the post as a tombstone without html (TestModeratorHideMovesTheResponseCacheKey)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Only a moderator may restore a post a moderator deleted.** This is the maintainer's decision of 2026-09-28, in the note on this ticket and in docs/08 "Restoring a moderator's delete". When a moderator deletes someone else's post, the post is `hidden-by-mod` rather than `deleted`. The author still sees it, marked as removed by a moderator, and the author's restore, edit and delete are `forbidden`. The existing guard in `authorize` already refuses those. Author-may-restore lost because it makes a moderator's delete a suggestion. Restore-by-request lost because it needs a flow of its own. A post an author deleted themselves stays theirs to restore within the window (TKT-01M29CKRHS).
- **The record actor is the moderator**, and the post keeps its author. The index's `Author` is revision 1's actor, and `applyEdit` never changes it (docs/03).
- **Moderators bypass the author windows** (docs/08; TKT-01M29CKRHS builds the windows and exempts `canModerate`).
- **A moderator's edit is never a grace edit** (TKT-01M29CKRHS, settled there): its visibility is what makes it acceptable (this ticket's description).
- **How the log says a Delete was a moderator's.** `DeleteBody` gains `by_moderator bool` (cbor and json `by_moderator,omitempty`), written when the actor is not the post's author. The index maps it to `EntryHiddenByMod`. An ordinary Delete still maps to `EntryDeleted`. This is an optional body key, which the docs/03 freeze permits. Deriving it from `actor != entry.Author` lost: a record's effect would then rest on comparing two frame fields rather than on what the record says, and `ketju dump` would need the rule to be read. No Phase 1 log holds a Delete by a non-author, because `authorize` refused one, so no existing record changes meaning. An older build ignores the key and shows a tombstone to everyone, which fails closed.
- **The edited marker attributes the edit without naming the moderator.** The post shows "edited by a moderator" when its current revision's actor is not its author. The handle is in the history, which is public by default (docs/08 "Post history"). The page does not name them, because the editor's user row would have to enter the authors digest in `respcache.Key`, and the editor is only known after the frame is read, which comes after the cache lookup. Attribution by role is read from the frame, which the log length already keys.

### Approach

1. **authorize** (`internal/service/threads/mutate.go`): a viewer with `canModerate` may edit, delete or restore any post, and the author and hidden-by-mod checks apply only to viewers without it. A moderator's edit needs `moderate`, not `reply`. Archived stays `conflict` for everyone. The lock refuses members' edits only, as now.
2. **Delete**: `Service.Delete` sets `DeleteBody.ByModerator` when `v.UserID != entry.Author`. The Refuse set stays `gone`, so hiding a post that is already gone is `conflict`.
3. **Restore**: it must accept a post that is deleted or hidden. `threadlog.Append` gains `RequireAny EntryFlags`, and at least one of its bits must be set. `Restore` uses `RequireAny: EntryDeleted|EntryHiddenByMod`, `Refuse: EntryMoved`. `Actor.checkState` checks it with the same staged fold. Two appends lost: they would not be atomic.
4. **Index**: `postFlagEffect(h, body)` (widened by TKT-01M29CKRHS). A `Delete` with `ByModerator` sets `EntryHiddenByMod`, and otherwise `EntryDeleted`. `Restore` clears both, as now. Fix the `EntryHiddenByMod` comment in `internal/store/threadlog/flags.go`, which says the author "may not see it". That contradicts the 2026-09-28 decision.
5. **Projection**: `applyDeleted` sets `post_meta.deleted = 1` for both kinds. The forum and profile counts leave both out (docs/07 `GET /forums`: "the record that does must leave the total too"). `post_meta` gains no column; `TestPostMetaStaysTiny` holds.
6. **Editor on the read path**: `threadlog.Post` gains `EditorID uint64`, from the current revision's frame header in `decodePost`. `dto.Post` gains `edited_by_moderator bool`: `EntryEdited` is set and `EditorID != Author`. It is drawn in `postsDeps` and shared by the page, the single post, replies and user posts.
7. **Audit** (revised for Terva review 1352, finding 1): the projection writer writes the rows (TKT-01M29CKRGS step 1). A `Delete` with `by_moderator` is `post.hidden`. An `Edit` or a `Restore` whose actor is not `post_meta.author_id` is `post.edited` or `post.restored`. The target is `audit.Post(id, ordinal)` and the details are `{reason, thread, log_offset}`. An author's edit, delete or restore of their own post produces no row, and neither does a moderator acting on their own post. `TestOwnPostActionsAreNotAudited` holds this, including after reindex. The service calls `appendEvent` for the moderator paths, so the row exists when the handler answers.
8. **Client**: `Post.tsx` offers Edit, Delete and Restore on every post to a viewer with `viewer.can_moderate`, and shows "(edited by a moderator)" for `edited_by_moderator`. Regenerate `dist/`.
9. **Docs**: docs/03 record table: the `Delete` body gains `by_moderator`, and its effect is "hidden-by-mod when by_moderator, otherwise deleted". docs/07 "Edit, delete and restore" and the `dto.Post` shape get `edited_by_moderator` and the moderator rules. The docs/07 `dto` golden is regenerated.

### Tests

- `TestModeratorEditsAnyPost`: a moderator's edit of a member's post appends an Edit whose frame actor is the moderator. The index `Author` is unchanged, the post is served with `edited_by_moderator: true`, and history lists the moderator as the revision's actor. One `post.edited` audit row is written.
- `TestModeratorDeleteHidesAndOnlyAModeratorRestores`: a moderator's delete sets `EntryHiddenByMod`, not `EntryDeleted`. The author's restore, edit and delete are each `forbidden` and append nothing. A second moderator's restore clears it. One `post.hidden` and one `post.restored` row are written.
- `TestModeratorBypassesWindows`: past both author windows, a moderator's edit and delete of a member's post succeed.
- `TestPostFlagEffectIsWhatApplyDoes`: add Delete with and without `by_moderator`, and Restore under `RequireAny`, including same-batch staging.
- `TestRestoreRequireAny` (threadlog actor): Restore of a visible post is `ErrPostState`; of a deleted or a hidden post it succeeds; of a moved one it is refused.
- Reindex: a log with a moderator's Delete rebuilds the hidden-by-mod bit, and `post_meta.deleted` is 1.
- **Response cache invariant**, `TestModeratorHideMovesTheResponseCacheKey`: a member's cached page is a miss after the hide, and the post is then a tombstone with no `html`. The moderator's class still gets the content.

### Out of scope

- Tombstone wording, the author's view of their own hidden post, and the moderator's inline view: TKT-01M29CKRJ9, which depends on this ticket.
- Auto-hide by flags (`auto`): TKT-01M29CP2PJ.
- The author windows themselves: TKT-01M29CKRHS.
- Hard redaction: TKT-01M29CXJZ9.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: only a moderator may restore a post a moderator deleted. A moderator's delete of someone else's post sets hidden-by-mod, not deleted, so the existing authorize guard in internal/service/threads/mutate.go already refuses the author's restore, edit and delete as forbidden. The author still sees the post, marked as removed by a moderator. Author-may-restore lost because it makes a moderator's delete a suggestion. Restore-by-request lost because it needs its own flow. Written into docs/08 'Restoring a moderator's delete'. Carry this into the plan's Settled section.
