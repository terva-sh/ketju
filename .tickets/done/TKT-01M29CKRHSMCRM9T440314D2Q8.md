---
schema: 3
id: TKT-01M29CKRHSMCRM9T440314D2Q8
title: Implement author edit and delete windows with grace edits
type: task
status: done
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
  - ref: pr:146
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T15:03:51Z
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

- [x] Past posts.edit_window_seconds an author's edit answers edit-window-closed, and past posts.delete_window_seconds their delete and restore answer delete-window-closed, measured from the post's creation, with the log unchanged
- [x] An author's edit within posts.grace_edit_seconds of posting appends an Edit with grace: true, and the post is served with edited_at null while history lists the revision
- [x] An edit after the grace period sets edited_at, and no later grace edit clears it
- [x] A viewer with moderate edits and deletes their own post past both windows
- [x] Grace is read from the log: changing posts.grace_edit_seconds afterwards changes neither the served marker, fresh or cached, nor the rebuilt index (TestGraceComesFromTheLog)
- [x] No grace edit follows a visible edit, so a served edited_at is always the last visible edit time and equals post_meta.edited_at (TestEditedAtIsTheLastVisibleEdit)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a. Revised at claim 2026-09-29 against main at 889870b.

### Settled in grooming

- **The windows and their defaults** (docs/08 "Primitives"): an author may edit their own post for 24 hours and delete it for 15 minutes. Edits in the first 5 minutes are grace edits. All three settings already exist in `internal/settings/settings.go`: `posts.edit_window_seconds`, `posts.delete_window_seconds` and `posts.grace_edit_seconds`. Every window is measured from the post's creation, the index entry's `CreatedMS` (revision 1).
- **Restore of one's own delete** is governed by the delete window (docs/08 "Restoring a moderator's delete": "A post an author deleted themselves is still theirs to restore within the window").
- **A moderator is bound by neither window** (docs/08, ticket criterion). Here that means a viewer with `canModerate` acting on their own post. Reach over other people's posts is TKT-01M29CRJ5J.
- **This extends `authorize`** in `internal/service/threads/mutate.go`. Its comment and docs/07 "Edit, delete and restore" name it as the seam for this ticket.
- **Grace is recorded in the log, not computed on read.** The index is rebuilt from the log alone (`Index.Apply`) and cannot read a setting. A grace decision made at read time would also put a setting into the cached posts payload without it being in `respcache.Key`. So the service decides at write time with the setting then in force and writes `grace: true` into the `Edit` body. Changing the setting later does not restyle old posts.
- **Frozen format.** `grace` is an optional key in the `Edit` body. docs/03 permits this without a bump, and `Edit.mentions` (2026-09-24) is the precedent. An older build ignores the key and marks the post edited. That fails safe: it shows a marker, and it discloses nothing.

### Approach (revised 2026-09-29 at claim, against main at 889870b)

The groomed approach held against the code, with these changes, each for a reason found while building:

1. **authorize** takes `now` and a `windows` struct read once per request by `Service.authorWindows` (one read transaction for the three settings), before `Threads.With`. The existing author, hidden-by-mod and reply checks run first, then a viewer with `canModerate` returns, then the windows. One `now` per request feeds the window, the grace decision and the record's `TimeMS`.
2. **Boundary.** A window is open for its first N seconds and closed from N on (`age < N`), so a window of 0 never opens. The groomed plan wrote `<=` for grace; strict `<` everywhere makes a grace setting of 0 mean off, which TestGraceComesFromTheLog relies on, and keeps "0 means the author may not act" true for the edit and delete windows. This is an internal boundary with no product meaning beyond what the plan already settled; docs/07 records it.
3. **Problem codes** `edit-window-closed` and `delete-window-closed`, 403, as planned. docs/07's catalogue table lists them, which `TestTheDocumentedCatalogueMatchesTheCode` requires.
4. **Grace on Edit**, as planned, plus a race guard: a grace Edit is appended with `Refuse: gone | EntryEdited`, so a visible edit that lands between the service's check and the append makes the grace edit `conflict` rather than letting a grace Edit follow a visible one.
5. **Index**: `postFlagEffect(t, body)`; a grace Edit's effect is (0, 0), which equals "sets nothing on an unedited entry, leaves the bit on an edited one". `isGrace` reads the body as a pointer or a value, so the actor's staged fold (`s.req.a.Body`) and `Apply` (decoded pointer) agree.
6. **Projection**: sibling query `SetPostRevisionKeepEdited` as planned. The test helper `agreesWithIndex` now also compares `edited_at` with the index's edited flag at the current revision's time, for every projection test.
7. **DTO**: `EditedAt` from `EntryEdited` at `RevisionMS`, as planned.
8. **Client controls moved out.** The client lane held the committed dist, so the `author_windows` field and the `Post.tsx` controls are TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client), a linked draft. Adding `author_windows` to the DTO here would fail the client's `dto-contract.ts` check. The two new codes cannot move out: the client test holds `src/api/problem.ts` to the catalogue golden, so this PR adds their words there, adjusts that test for a bare 403, and regenerates `dist/`, as the orchestrator allowed.
9. **Docs**: docs/03 record table, index flag row and a `Edit.grace` paragraph beside `Edit.mentions`; docs/07 "Edit, delete and restore" and the error catalogue.

### Tests

- `internal/web/api/windows_test.go`: TestAuthorWindows (AC1, AC4), TestGraceEdit (AC2, AC3), TestEditedAtIsTheLastVisibleEdit (AC6), TestGraceComesFromTheLog (AC5), TestNoGraceEditFollowsAVisibleOne (AC6 under concurrency). They run through the API, so they cover the service and the codes together; the plan placed TestAuthorWindows in `internal/service/threads`.
- `internal/store/threadlog`: TestGraceEditFlags, TestPostFlagEffectIsWhatApplyDoes with grace bodies as pointer and value, staged grace cases in TestPreconditionsSeeTheBatchAhead, TestEditGraceKeyIsOptional.
- `internal/events/projection`: TestGraceEditProjection, including replay over stale rows and into empty tables.
- `cmd/ketju` reindex property test writes random grace Edits.
- `internal/problem` catalogue golden; `web/client/src/api/problem.test.ts`.

### Out of scope

- A moderator's reach over other people's posts, and the moderator-attributed edited marker: TKT-01M29CRJ5J.
- The history screen that shows grace revisions: TKT-01M29CP2Q3.
- The rename window: TKT-01M29CKRH9.
- The client controls: TKT-01M3PTT97X.

## Notes

**agent:claude-code/phase2-run-ckrhs** at 2026-09-29T14:59:46Z

Why this server-lane PR touches web/client and the committed dist (2026-09-29).

The two new problem codes, `edit-window-closed` and `delete-window-closed`, are required by AC1 and land in `internal/problem/testdata/catalogue.json`. `web/client/src/api/problem.test.ts` iterates every catalogue code ("has words for", "reads the served", "maps a bare status"), so the client tests fail unless `src/api/problem.ts` gains both codes in the same change. `problem.ts` is bundled, so `internal/web/spa/dist` changes too. The orchestrator of the Phase 2 run approved this minimal client change (option A) and held the PR back until the client lane's scaffold PR #145 (TKT-01M3NRYCP5) had merged, so only one open PR changes the dist. The change adds the two message rows, makes the bare-status test map a bare 403 to `forbidden` (a bare status cannot say which window closed), and regenerates the dist.

The rest of the client work, the `author_windows` viewer field and the controls in `Post.tsx`, is TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client), filed as a draft that depends on this ticket. The DTO field moved there too, because adding it here would fail the client's `dto-contract.ts` check.

The boundary choice (a window is open for its first N seconds, so 0 never opens, and grace uses strict `<` rather than the plan's `<=`) is recorded in the revised plan and in docs/07.

**agent:claude-code/phase2-run-ckrhs** at 2026-09-29T15:03:51Z

Terva review 1378 (run 2fd82a91-271e-4584-a619-d94dd4bbe3f2, Actions run 890) on PR #146, head 4cdb5dc9fb97d4fe1bd60e9de0767c177b71ab00, base 889870b4f853046430ae18cc75db6f1dcd1debf4. One finding, medium: 'A zero-length window opens when the clock moves backward'. Accepted and fixed: within() now returns false for a window of 0 or less before comparing the age, so a post stamped after now by a clock that stepped back is inside every open window and no closed one, and a grace setting of 0 is off at any age. Tests: TestAuthorWindows gains a stepped-back clock against zero windows, and TestGraceOfZeroIsOff covers grace at age 0 and -60 s. Removing the d > 0 guard fails TestAuthorWindows.

## Summary

Author windows and grace edits are enforced on the server. authorize in internal/service/threads/mutate.go refuses an author's edit past posts.edit_window_seconds with edit-window-closed, and a delete or restore past posts.delete_window_seconds with delete-window-closed. Both codes are 403, both windows are measured from revision 1, and a viewer who may moderate the forum is exempt on their own post. An author's edit within posts.grace_edit_seconds, on a post with no visible edit, is written with the optional grace key on Edit (docs/03). The index's edited flag, post_meta.edited_at and the served edited_at all ignore such an edit, and a grace Edit is appended refusing the edited flag, so none follows a visible one. The client has words for both codes. The viewer's author_windows field and the edit and delete controls are TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client).
