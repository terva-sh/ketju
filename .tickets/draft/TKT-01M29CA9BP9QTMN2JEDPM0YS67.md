---
schema: 3
id: TKT-01M29CA9BP9QTMN2JEDPM0YS67
title: Implement edit, soft delete, and restore
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`PATCH /threads/{id}/posts/{ordinal}` writing an Edit record, `DELETE` writing a Delete record, and `POST …/restore` writing a Restore.

Every one of these is an append. Nothing overwrites a byte in the log, which is why edit history and soft deletion fall out of the format for free rather than needing a history table.

The author window rules land in the moderation epic. What this ticket owns is that the records are written correctly and that the index entry for the post moves to the current revision.

## Acceptance criteria

- [ ] An edit appends a record, increments the revision, and repoints the index entry at the new revision
- [ ] A delete leaves the original Post bytes intact and sets the deleted flag in the index
- [ ] A restore clears the flag and the post returns at its current revision
- [ ] Editing a deleted post is refused rather than silently reviving it

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add three routes in internal/web/api/posts_mutate.go: `PATCH /api/v1/threads/{id}/posts/{ordinal}` with `{source, reason?}`, `DELETE …` with an optional `{reason}`, and `POST …/restore`. All require `viewer.Member()`.
2. Authorization, minimal by design: the author of the post (`IndexEntry.Author == viewer.User.ID`) may act. The windows and grace edits are TKT-01M29CKRHS, and moderator any-post is TKT-01M29CRJ5J. Leave one `posts.authorize(viewer, entry, action)` function as the seam both extend. On a locked thread, a member's edit is `conflict` (docs/08).
3. Edit: render-validate first, exactly as TKT-01M29CA9B9 does, then append `EditBody{Source, Reason, ReplyTo: current entry's ReplyTo}`. `Index.applyEdit` sets `ReplyTo` from the body, so leaving it zero would silently drop the post's reply link. Append through `posts.Writer`, which publishes the event.
4. Refuse an edit to a deleted post inside the actor, not only in the service, so a Delete racing the Edit cannot slip between check and append. Add a precondition to `threadlog.Append` (for example `RequireVisible bool`) checked in `Actor.frameFor` against the live entry flags, returning a new `threadlog.ErrPostDeleted`. Map that error to `conflict` in internal/problem/domain.go.
5. Delete: append `DeleteBody{Reason}`, and the index sets `EntryDeleted` (apply.go). Restore: append `RestoreBody{Reason}`, and the index clears it. Invalidate nothing. The render cache key carries the revision, and a tombstone is decided from flags at read time.
6. Response: the post as it now reads for this viewer, from the shared assembler (TKT-01M29CA9AX), plus `log_length`. Add a DTO and golden; the shape is not in docs/07 today.

### Files
internal/service/posts/{edit.go,delete.go,authorize.go}, internal/web/api/posts_mutate.go, internal/store/threadlog/{actor.go,errors} (precondition + sentinel), internal/problem/domain.go (mapping), internal/dto (+golden).

### Tests
- AC1: edit; then `Index.Entry(ord)` has `Revision==2`, `Offset` equal to the new frame's offset from `ScanFrames`, and `EntryEdited` set; `Reader.One` returns the new source, and the reply_to is preserved.
- AC2: delete; then `ScanFrames` still finds the original `TypePost` frame with the same bytes and CRC, `EntryDeleted` is set, and `Reader.One` is a tombstone.
- AC3: restore; the flag is cleared and the post reads at its current revision (edit, delete, restore → revision 2 content).
- AC4: an edit after delete is `conflict`, and the log size is unchanged. A `-race` test fires Delete and Edit concurrently 1000 times; no Edit frame ever follows a Delete for that ordinal without an intervening Restore.

### Design
Reread docs/03 "Record types" (Edit/Delete/Restore rows) and "Write path", docs/08 "Primitives", "Locking semantics", and "Tombstones", and docs/07 "Resources".

### Interactions
Consumes `posts.Writer` from TKT-01M29CA9B9, TKT-01M29C2WWG (render validation), and TKT-01M29C0WD8 (actor). The projection writer (TKT-01M29C2WW5) consumes the events. TKT-01M29CKRHS, TKT-01M29CRJ5J, and TKT-01M29CP2PJ extend `posts.authorize`.
Open: `EditBody` has no Mentions field, so an edit's mentions cannot reach the projection or notifier without re-parsing. That is at odds with TKT-01M29C2WWG AC4.
Open: `RestoreBody` clears only `EntryDeleted`, while docs/08 says a moderator's Restore also clears hidden-by-mod.
Open: whether deleting a deleted post, or restoring a visible one, is idempotent or a `conflict`.

### Settled in grooming (2026-09-24)

- Restore by a moderator also clears hidden-by-mod (docs/08).
- An Edit that omits reply_to keeps the previous value. Fix the index code, which currently clears it.
- Edits carry optional `mentions` (docs/03, decided 2026-09-24).
