---
schema: 3
id: TKT-01M29CKRHSMCRM9T440314D2Q8
title: Implement author edit and delete windows with grace edits
type: task
status: in-progress
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
dependencies:
  - TKT-01M29CA9BP9QTMN2JEDPM0YS67
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim:
  actor: agent:claude-code/phase2-run-ckrhs
  branch: run/ckrhs-edit-delete-windows
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ac33eecb589b3ab26
  commit: 2897ac0ad92bfe874d32973bf7a8ed1e3262c88b
  session: null
  claimed_at: 2026-09-29T14:45:12Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T14:45:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckrhs
  name: ""
extensions: {}
---

## Description

An author may edit their own post for 24 hours and delete it for 15 minutes, both site settings. Edits within the first 5 minutes collapse into the original revision for display, while still being stored.

Grace edits exist because fixing a typo ten seconds after posting should not brand a post as edited forever. The revision is kept either way, so the history stays complete even when the display hides it.

## Acceptance criteria

- [ ] Past posts.edit_window_seconds an author's edit answers edit-window-closed, and past posts.delete_window_seconds their delete and restore answer delete-window-closed, measured from the post's creation, with the log unchanged
- [ ] An author's edit within posts.grace_edit_seconds of posting appends an Edit with grace: true, and the post is served with edited_at null while history lists the revision
- [ ] An edit after the grace period sets edited_at, and no later grace edit clears it
- [ ] A viewer with moderate edits and deletes their own post past both windows
- [ ] Grace is read from the log: changing posts.grace_edit_seconds afterwards changes neither the served marker, fresh or cached, nor the rebuilt index (TestGraceComesFromTheLog)
- [ ] No grace edit follows a visible edit, so a served edited_at is always the last visible edit time and equals post_meta.edited_at (TestEditedAtIsTheLastVisibleEdit)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **The windows and their defaults** (docs/08 "Primitives"): an author may edit their own post for 24 hours and delete it for 15 minutes. Edits in the first 5 minutes are grace edits. All three settings already exist in `internal/settings/settings.go`: `posts.edit_window_seconds`, `posts.delete_window_seconds` and `posts.grace_edit_seconds`. Every window is measured from the post's creation, the index entry's `CreatedMS` (revision 1).
- **Restore of one's own delete** is governed by the delete window (docs/08 "Restoring a moderator's delete": "A post an author deleted themselves is still theirs to restore within the window").
- **A moderator is bound by neither window** (docs/08, ticket criterion). Here that means a viewer with `canModerate` acting on their own post. Reach over other people's posts is TKT-01M29CRJ5J.
- **This extends `authorize`** in `internal/service/threads/mutate.go`. Its comment and docs/07 "Edit, delete and restore" name it as the seam for this ticket.
- **Grace is recorded in the log, not computed on read.** The index is rebuilt from the log alone (`Index.Apply`) and cannot read a setting. A grace decision made at read time would also put a setting into the cached posts payload without it being in `respcache.Key`. So the service decides at write time with the setting then in force and writes `grace: true` into the `Edit` body. Changing the setting later does not restyle old posts.
- **Frozen format.** `grace` is an optional key in the `Edit` body. docs/03 permits this without a bump, and `Edit.mentions` (2026-09-24) is the precedent. An older build ignores the key and marks the post edited. That fails safe: it shows a marker, and it discloses nothing.

### Approach

1. **authorize** takes the post's age and the windows: `authorize(a access, e threadlog.IndexEntry, action postAction, now time.Time, w windows)`. `windows` is read once per request from the three settings, before `Threads.With`. For the author without `canModerate`: an edit past `edit_window_seconds` answers `edit-window-closed`, and a delete or restore past `delete_window_seconds` answers `delete-window-closed`. The existing author and hidden-by-mod checks stay as they are and run first. A window of 0 means the author may not act at all, which is what the number says.
2. **Problem codes** `edit-window-closed` and `delete-window-closed`, both 403 with fixed titles, in `internal/problem/problem.go` and `testdata/catalogue.json`. These make "each refusal names the expired window" true. A single code with an extension member lost: `internal/problem` has no extension mechanism, and adding one for this lone use costs more than two catalogue rows.
3. **Grace on Edit**: `threadlog.EditBody` gains `Grace bool` with cbor and json tag `grace,omitempty`. `Service.Edit` sets it when the editor is the post's author, `now - CreatedMS <= grace_edit_seconds`, and the entry does not already carry `EntryEdited`. A grace edit therefore only ever follows the original or other grace edits, never a visible edit (revised for Terva review 1368 on PR #138). An author's edit inside the grace period that follows a moderator's visible edit is itself visible. A moderator editing someone else's post is never grace: that edit exists to be visible (docs/08, TKT-01M29CRJ5J).
4. **Index**: `postFlagEffect` widens to see the record body: `postFlagEffect(h FrameHeader, body any)`. An `Edit` with `Grace` on an entry without `EntryEdited` sets nothing. Every other Edit sets `EntryEdited`, including a `Grace` Edit on an entry that already has the bit, which the service never writes but a hand-made log could hold. `EntryEdited` never clears. The result: whenever `EntryEdited` is set, the current revision is a visible edit, so its timestamp is the last visible edit time. `Actor.checkState` passes the staged request's body (`s.req.a.Body`), so the staged fold and `Apply` still agree. TKT-01M29CRJ5J widens the same function for Delete.
5. **Projection**: `applyEdit` leaves `post_meta.edited_at` untouched for a grace edit only while `edited_at` is still null, the projection's form of the index rule above. It uses a sibling query `SetPostRevisionKeepEdited` in `queries/projection.sql` that sets `edited_at = CASE WHEN edited_at IS NULL THEN NULL ELSE ? END`, the frame time otherwise. A grace Edit after a visible one then moves `edited_at` to its own time, exactly as the index moves the current revision, so the two read paths agree. It still advances `current_revision` and `log_offset`. Regenerate dbq.
6. **DTO**: in `postsDeps` (`view.go`), `EditedAt` is set from `p.Flags.Has(threadlog.EntryEdited)` rather than `p.Revision > 1`, at `p.RevisionMS`. By the rules in steps 3 to 5, that is the last visible edit time, and it equals `post_meta.edited_at`. User posts and the single-post endpoint share this assembly.
7. **Client controls**: `dto.ThreadViewer` gains `author_windows: {edit_seconds, delete_seconds} | null`, null for a moderator. It is per viewer and never cached. `Post.tsx` offers Edit and Delete on the viewer's own post only while `created_at` plus the window has not passed. A refusal code still shows its message if the clock disagrees. Regenerate `dist/`.
8. **Docs**: docs/03 record table: `Edit` body gains `grace` (optional, added after the freeze), and its effect becomes "edited flag unless grace". Add a short paragraph beside the `Edit.mentions` one. docs/07 "Edit, delete and restore" replaces "Only the post's author may act for now" with the windows and the codes. docs/08 needs no change.

### Tests

- `TestAuthorWindows` (`internal/service/threads`): with a fixed clock and windows set small, the author edits inside and gets `edit-window-closed` one second past; deletes inside and gets `delete-window-closed` past; restores their own delete inside the delete window and is refused past it. Each refusal leaves the log length unchanged. A moderator editing and deleting their own post past both windows succeeds.
- `TestGraceEdit`: an edit 10 s after posting stores revision 2 with `grace: true`. The post is served with `edited_at: null`, and `GET …/history` lists both revisions. An edit past grace sets `edited_at`. `TestEditedAtIsTheLastVisibleEdit`: a moderator edits a member's post 1 min after posting, then the author edits at 2 min. The author's edit is written without `grace` and is served as `edited_at` at 2 min, equal to `post_meta.edited_at`. A hand-made `grace: true` Edit after a visible one sets `edited_at` to its own time on both the index path and the projection path, so the two never disagree.
- `TestPostFlagEffectIsWhatApplyDoes` extended with grace and non-grace Edit bodies, and a same-batch staging case.
- `TestGraceComesFromTheLog`: after a grace edit, change `posts.grace_edit_seconds` to 0. The page, fresh and from the response cache, still shows no marker, and a full index rebuild gives the same `EntryEdited` bit. This is the **response cache invariant** for this ticket: nothing in the cached posts depends on a setting.
- Projection: a grace Edit keeps `post_meta.edited_at` null. `ketju reindex` reproduces it.
- API: each new code's status and title against the catalogue golden.
- Client: Edit and Delete disappear once the window passes (fake timers), and neither shows for a moderator's `author_windows: null`.

### Out of scope

- A moderator's reach over other people's posts, and the moderator-attributed edited marker: TKT-01M29CRJ5J.
- The history screen that shows grace revisions: TKT-01M29CP2Q3.
- The rename window: TKT-01M29CKRH9.
