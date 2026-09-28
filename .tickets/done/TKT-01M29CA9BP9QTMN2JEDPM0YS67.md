---
schema: 3
id: TKT-01M29CA9BP9QTMN2JEDPM0YS67
title: Implement edit, soft delete, and restore
type: task
status: done
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
  - ref: pr:95
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-28T05:30:38Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`PATCH /threads/{id}/posts/{ordinal}` writing an Edit record, `DELETE` writing a Delete record, and `POST …/restore` writing a Restore.

Every one of these is an append. Nothing overwrites a byte in the log, which is why edit history and soft deletion fall out of the format for free rather than needing a history table.

The author window rules land in the moderation epic. What this ticket owns is that the records are written correctly and that the index entry for the post moves to the current revision.

## Acceptance criteria

- [x] An edit appends a record, increments the revision, and repoints the index entry at the new revision
- [x] A delete leaves the original Post bytes intact and sets the deleted flag in the index
- [x] A restore clears the flag and the post returns at its current revision
- [x] Editing a deleted post is refused rather than silently reviving it

## Implementation plan

Revised at claim (2026-09-28) from the code on main at ee54c04. The reply path (TKT-01M29CA9B9) landed in `internal/service/threads`, not in a `posts` service, so these writes go beside it.

### Approach
1. Routes in `internal/web/api/mutate.go`: `PATCH /api/v1/threads/{id}/posts/{ordinal}` with `{source, reason?}`, `DELETE …` with an optional `{reason}` body, and `POST …/restore` with an optional `{reason}` body. Each asks `MayWrite` first, as reply does, and answers 200 with `dto.CreatedPost` (`{post, ordinal, revision, log_length}`), the post as a later read serves it to this viewer: the new revision after an edit, the tombstone after a delete. Reusing `CreatedPost` rather than a new DTO: the shape is identical, and one type means the client inserts every write's answer the same way. `no-store`, as reply.
2. Service in `internal/service/threads/mutate.go`: `Edit`, `Delete`, `Restore`, all through one `authorize(a access, entry, action)` seam that TKT-01M29CKRHS (windows) and TKT-01M29CRJ5J (moderator any-post) extend. Here: only the post's author (`IndexEntry.Author == viewer.UserID`) may act; anyone else is `forbidden`. A post hidden by a moderator is not the author's to touch (`forbidden`), since docs/08 says only a moderator's Restore clears it. Edit also needs `reply` in the forum, as posting does. Archived is `conflict` for all three; a lock is `conflict` for a member's edit only (docs/08 "Locked threads accept no posts or edits from members").
3. Edit: `checkPost`, then render the new source against the thread and the post's current `reply_to`, exactly as reply does; append `EditBody{Source, Reason, ReplyTo: entry.ReplyTo, Mentions: &rendered mentions}` through `posts.Writer`. Writing `reply_to` explicitly as well as fixing the index keeps an older build, which clears on zero, reading the same thing.
4. State preconditions checked inside the actor, so a racing Delete cannot slip between the service's check and the append: `threadlog.Append.Refuse EntryFlags` / `Require EntryFlags`, checked in `Actor.frameFor` against the entry's flags as the index holds them plus every earlier record staged in the same batch. A failed check is `threadlog.ErrPostState`, mapped to `conflict` in `internal/problem/domain.go`. Edit refuses deleted, moved and hidden; Delete refuses deleted, moved and hidden; Restore requires deleted and refuses moved.
5. Index: `applyEdit` keeps `ReplyTo` when the body's is zero (settled). Restore clears `EntryDeleted|EntryHiddenByMod` (settled: a moderator's Restore clears hidden-by-mod; the index cannot see the actor's role, and step 2 keeps any non-moderator from restoring a hidden post). The staged-flag walk in step 4 uses the same `postFlagEffect` table as `Apply`, so the two cannot disagree.
6. Projection: `SetPostRevision` keeps `reply_to` when the Edit carries none (`coalesce(sqlc.narg(reply_to), reply_to)`), mirroring the index; regenerate dbq.
7. Docs: docs/07 gains a paragraph for the three endpoints (codes, bodies, the idempotency answer below).

### Gaps settled here (internals / API detail, recorded in docs/07)
- Deleting a deleted post, or restoring a visible one, is `conflict` and appends nothing: the log keeps no no-op records, and the rule is the same one that refuses an edit to a deleted post.
- Responses reuse `dto.CreatedPost`.

### Tests
- threadlog: AC1 edit repoints Offset/FrameLength/Revision, sets Edited, keeps ReplyTo when the body's is zero; replay equals incremental. AC2 delete leaves the Post frame's bytes and CRC intact under `ScanFrames`, sets Deleted. AC3 edit→delete→restore reads revision 2. AC4 precondition table incl. same-batch staging; a `-race` test fires Delete and Edit concurrently 1000 times and asserts no Edit frame follows a Delete for that ordinal without an intervening Restore.
- API: each route's happy path read back through GET, refusals (not author, locked, archived, deleted, already-deleted, not-deleted, anonymous, pre-user) leave the log unchanged; a golden of the edit response.
- projection: an Edit without reply_to keeps post_meta.reply_to.

### Settled in grooming (2026-09-24)

- Restore by a moderator also clears hidden-by-mod (docs/08).
- An Edit that omits reply_to keeps the previous value. Fix the index code, which currently clears it.
- Edits carry optional `mentions` (docs/03, decided 2026-09-24).

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:23:19Z

Gaps settled here (internals and API detail, per docs/12 "Gaps the design leaves open"; written into docs/07 "Edit, delete and restore"):

- Deleting a deleted post, or restoring a visible one, is `conflict` and appends nothing, rather than an idempotent 200. The log keeps no records that change nothing, and it is the same rule that refuses an edit of a deleted post. Alternative considered: an idempotent 200 with no append, which lost because it needs the actor to answer "skipped" as a result, a second code path for a case only a double click reaches.
- The three answers reuse `dto.CreatedPost` rather than a new DTO: the shape is identical and the client inserts every write's answer the same way. Golden: `internal/web/api/testdata/golden/edit.member.json`.
- The state check is an `Append.Require/Refuse` flag precondition in the actor, not a service-side read. A service-side check lost because a Delete can land between the read and the append.
- Edit requires `reply` in the forum; delete and restore of one's own post do not. A lock refuses a member's edit only, reading docs/08 "Locked threads accept no posts or edits from members" literally.

Handed on:
- TKT-01M29CRJ5J (moderator any-post, phase-2) must decide whether an author may restore a post a moderator deleted. In this build only authors delete, so every Delete is the author's own; the index does not record who deleted.
- Nothing sets `EntryHiddenByMod` yet. The index clears it on every Restore (settled in grooming), and `authorize` keeps an author off a hidden post, so only a moderator path can restore one.
- The service lives in `internal/service/threads/mutate.go` beside Reply, not in a `posts` service as the pre-groom plan said.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:28:08Z

Review 1119 (run 72772705-131c-4ffb-b68e-36a6fb2d7fa5, Actions run #495) on PR #95, head a1a6b4affd81688cf1e1771c19603a47b11f34c3, base ee54c04ca6fe3a71a154c8af8d78048d770d8a97. It found two medium issues, both accepted and fixed in 2506946. finding-1: the answer mixed the append's log length and revision with a later read of the post. The post, revision and log length now come from one index snapshot taken after the append (TestWriteAnswerIsOneSnapshot). finding-2: the optional reason body was read whole with io.ReadAll. It is now streamed through a 4 KiB cap, and anything past it is payload-too-large (TestReasonBodyIsBounded). A fresh review of the new head has been requested.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T05:30:38Z

Review 1123 (run 10499142-913c-4fdc-a52a-7ffd1e1973e0, Actions run #500) on PR #95, head 9efc4b9d6e1e4778c3d85c0bc83ecbec36cf67d8, base ee54c04ca6fe3a71a154c8af8d78048d770d8a97. It marked both findings from review 1119 resolved and raised one new medium finding, accepted and fixed. finding-1: the batch fold left out the Edit's edited flag. postFlagEffect now covers the Edit and applyEdit goes through it; TestPostFlagEffectIsWhatApplyDoes holds Apply to the table for every type and every flag state.

## Summary

PATCH, DELETE and POST .../restore on a post append Edit, Delete and Restore through posts.Writer and answer 200 with dto.CreatedPost. The thread actor enforces post state through Append.Require/Refuse flag preconditions (threadlog.ErrPostState, mapped to conflict), and those checks see records staged earlier in the same batch, so an edit cannot revive a deleted post. An Edit with no reply_to keeps the post's link in the index and in post_meta. A Restore clears hidden-by-mod. For now only the author may act, through one authorize() seam that CKRHS and CRJ5J extend. docs/07 records the endpoints.
