---
schema: 3
id: TKT-01M29CXK19AFDY0WTH5MZAZVP5
title: Build the admin forum management screens
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYHGZY39T1F2NGJX8MED
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
  - TKT-01M3N24RHDFMR8MHX0CCENN6R7
blocks_on: none
references:
  - ref: design:data-model-perms
    path: docs/04-data-model.md
  - ref: pr:213
    path: null
  - ref: pr:219
    path: null
  - ref: pr:221
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-30T11:26:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cxk19
  name: ""
extensions: {}
---

## Description

Create, edit, reorder, and archive forums, including the one level of nesting and the visibility setting.

Reordering is the fiddly part and the one an admin does most, since forum order is how a community signals what it is about. Positions need to survive an insert in the middle without a renumbering that reorders everything else.

Archiving a forum must not orphan its threads, and what happens to them needs deciding here rather than discovering later.

## Acceptance criteria

- [x] An admin can create, rename, move under a root, reorder, archive and unarchive a forum through /api/v1/admin/forums, and a second level of nesting is validation-failed (TestForumNestsOneLevel)
- [x] A move writes only the moved forum's row while its neighbours leave a gap, and otherwise renumbers only its siblings in their existing order (TestMoveWritesOnlyTheMovedRow, TestMoveKeepsOrderUnderRandomMoves)
- [x] A visibility change goes through perm, applies on the viewer's next request (TestVisibilityChangeAppliesOnNextRequest), and the screen says it takes effect within a minute
- [x] Every forum write appends its audit_log row in the same transaction, and a failed audit insert leaves the forum unchanged (TestForumWritesAuditInTheSameTransaction)
- [x] Every /api/v1/admin/forums route is admin-only: a moderator by role and a member who holds moderate through a group are 403 (TestAdminForumsAreAdminOnly, TestAdminRoutesAreGated)
- [x] The screen lives under src/screens/admin, TestTheAdminBundleIsSeparate passes, the reader total stays at 47415 B, and the dist from go generate ./internal/web/spa is committed (ForumsScreen.test.tsx)
- [x] Archiving leaves every thread in place and read access unchanged, so whoever could read the forum still can, whatever its visibility. It refuses new threads and replies to everyone but admins, moderators included, while a moderator may still lock, pin, hide and move threads out. docs/04 says so, and the screen says 'Anyone who could read it still can. Only posting stops, except for admins.' for every visibility (TestArchivedForumEffect, ForumsScreen.test.tsx 'the archive text says read access is unchanged')

## Implementation plan

Groomed 2026-09-29. Refreshed 2026-09-30 against main at 5124209.

### Settled in grooming

- **Archived forums are read-only except for admins.** Decided 2026-09-28 (TKT-01M39788AT, docs/04 "permissions"). `read` is unchanged, so the forum and its threads stay visible to whoever could see them. `create_thread`, `reply` and `upload` are denied to everyone but admins, moderators included. The evaluation change and the admin exemption at `POST /forums/{slug}/threads` are TKT-01M3N24RHD, a dependency. This answers the description's question about threads: archiving a forum leaves every thread where it is, with no log record written and nothing moved or archived. It is one audited column change.
- Forums nest one level. Triggers `forums_depth_on_insert` and `forums_depth_on_update` enforce it on every write path (docs/04 "forums", TKT-01M29BYYHG).
- `public`, `members` and `restricted` mean what docs/04 "permissions" says. A `restricted` forum without `read` is Hidden (404).
- `internal/perm` owns every write that can change a decision, and clears its 60 s cache after the commit. `TestMutationsGoThroughPerm` keeps those `dbq` calls out of every other package (docs/04). Visibility, and archive once TKT-01M3N24RHD lands, are decision writes.
- Every administration action appends to `audit_log` (docs/08), through `internal/audit` (TKT-01M3NRYCP5, a dependency, which also supplies the admin gate and the admin screen scaffold).
- Admin screens are the lazy chunk, outside the 60000 B budget. The built client is committed: run `go generate ./internal/web/spa`.

Engineering choices made here, each with the alternative that lost:
- **Sparse positions.** A new forum goes 1024 after its last sibling. A move puts the forum at the integer midway between its new neighbours, so it writes one row. Only when the neighbours have no integer between them does the move renumber that parent's children to multiples of 1024, in their existing order, in the same transaction. Rewriting every sibling on every move lost because a move then rewrites rows it did not change, which is what the ticket asks to avoid. Fractional string keys lost because `position` is `INTEGER` and a forum list is tens of rows.
- **Moves are relative.** `{before: id}` or `{after: id}` names a sibling, and the server computes the position inside the write transaction. Sending the whole ordered list lost because two admins' stale lists overwrite each other.
- **The slug is fixed at creation.** `/f/{slug}` is in every shared link, and no design covers redirecting an old forum slug. A handle's redirect (docs/05 "Changing a handle") is for users. Name and description are editable.
- **No forum delete.** "No cascading deletes" (docs/04 "Conventions"). Archiving is how a forum is retired.
- **Unarchive is allowed.** A forum's `archived` is a column that no log record fixes, and docs/08 asks every tool to be reversible. A thread's archive is irreversible only because the log has no unarchive record, and that does not apply to a forum.
- **Move up and down buttons and a parent select**, not drag and drop. They are keyboard-first (docs/06) and add no library.

Added in the 2026-09-30 refresh:
- **What moderators keep in an archived forum** (maintainer decision 2026-09-29, docs/08 "Decided 2026-09-29", TKT-01M3N24RHD): "In an archived forum, moderators may still hide, lock, pin and move threads out. Posting is refused to everyone but admins, and moving a thread in stays admin-only." The screen's archive words and `TestArchivedForumEffect` follow this.
- **The forum screens stay admin-only under the forum-grant decision.** The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, and that "Admin screens and account actions keep their role gates" (docs/08 "Decided 2026-09-30", on PR #208's branch; TKT-01M3RQP0TG). Holding `moderate` in a forum, by role or through a group, gives no access to these routes.

### What changed since the plan was written

Read from main at 5124209:
- **`perm.Evaluator.SetForumArchived` exists** (TKT-01M3N24RHD, done), with the `SetForumArchived` query in `queries/forums_groups.sql` and its entry in `decisionWrites` (`internal/perm/layering_test.go`). The earlier plan's "new `SetArchived`" and "add the query" steps are removed. Neither `SetForumArchived` nor `SetVisibility` takes an `audit.By` or writes an audit row yet. Only tests call them: `internal/perm/archived_test.go`, `cache_test.go`, `caneach_test.go` and `moderated_test.go`.
- `perm.Evaluator` already has the audited pattern this ticket copies: `ChangeRole`, `Grant`, `Revoke` and `AddMember` in `internal/perm/admin.go` take `by audit.By` and call `audit.Append` inside `write`.
- The posting refusal in an archived forum is on main: `readOnlyForum` in `internal/service/threads/access.go`, held by `TestArchivedForumIsReadOnly` (`internal/perm`) and `TestArchivedForumConflicts` (`internal/web/api/archived_test.go`).
- No `forum.*` audit action exists yet. docs/04 "audit_log" must gain them. TKT-01M29CP2R4 (the audit view) plans an `audit.Actions` list held to docs/04, and whichever of the two lands second adds the `forum.*` names to both.
- docs/07 "Moderation and admin" is now split into `####` subsections, one per area.
- The newest migration is 0017. This ticket needs none.

### Revised at claim, 2026-09-30

Read from main at 80e4818, which changed only tickets since 5124209. The approach below stands, with these changes:
- **The screen's row is `dto.ManagedForum`, in `dto.ManagedForumList`.** `dto.AdminForum` already exists: it is the forum as the permission grid names it (`{id, slug, name, visibility}`), and its goldens and client type are TKT-01M29CXK1X's. Widening it would change the grid's contract for nothing, so step 5's type takes a new name. The golden stays `admin-forums.admin.json`.
- **perm gains `SetVisibilityThen` and `SetForumArchivedThen`**, which read in the same transaction (`writeThen`), so an answer describes the committed rows, as `ChangeRoleThen` does (Terva reviews 1416 and 1423). `SetVisibility` and `SetForumArchived` call them with no read. A forum id that names nothing is the new `perm.ErrNoForum` (`not-found`), and a visibility that is not one of the three is `perm.ErrInvalidVisibility` (`validation-failed`), both in `problem`'s mapping. A change to the value the forum already has writes nothing, no audit row included.
- **The move body is `{parent_id, before?, after?}`.** `parent_id` is always sent, `null` for the top level. Neither `before` nor `after` puts the forum last under that parent. Naming both, or naming a forum that is not a sibling there, is `validation-failed`. A move that leaves the forum where it was writes nothing. Placing a forum first takes the integer midway between 0 and the first sibling, so positions stay positive, and a first sibling at 1 or below renumbers.
- **Every write answers with the whole `ManagedForumList`**, read in the write's transaction, because a move can renumber the siblings and the screen redraws the tree from it. Create answers 201.

### Approach

1. **`internal/perm/mutate.go`**: `SetVisibility(ctx, by audit.By, forumID, visibility)` appends `forum.visibility_changed {from, to}`, and `SetForumArchived(ctx, by audit.By, forumID, archived)` appends `forum.archived` or `forum.unarchived`. Each reads the row first and writes the audit row inside the same `write` transaction, so the cache is cleared only after both commit. Update the callers in the four perm test files to pass `audit.By{}`.
2. **`queries/forums_groups.sql`**: add `UpdateForum` (name, description, updated_at), `SetForumPosition`, `ForumSiblings` (`WHERE parent_id IS ?`, ordered by position and id) and `ForumChildCount`. `CreateForum`, `SetForumParent`, `GetForum`, `ListForums` and `ForumThreadCounts` exist already. Regenerate `dbq`.
3. **`internal/service/forums/admin.go`**, beside `tree.go`. The route gate is `adminGate`, and the service checks the admin role again.
   - `List(ctx, v) ([]dto.AdminForum, error)`: every forum, archived and restricted included, in `ListForums` order, with `threads` and `posts` from `ForumThreadCounts`.
   - `Create(ctx, v, by, NewForum{Slug, Name, Description, ParentID, Visibility})`. The slug is 1 to 60 characters of `a-z`, `0-9` and inner `-`, and a taken one is `conflict`. The name is 1 to 100 characters with no control characters, and the description at most 1000. The parent must exist and be a root. The position is the last sibling's plus 1024. Visibility defaults to `members`, the schema's default. It appends `forum.created`.
   - `Update(ctx, v, by, id, ForumEdit{Name, Description *string})` appends `forum.updated {field: {from, to}}`.
   - `Move(ctx, v, by, id, ForumMove{ParentID *int64; Before, After int64})` works in one `db.Write`. The parent must be a root. A forum with children cannot take a parent (`validation-failed`, checked before the trigger, which stays the backstop). It computes the position, and renumbers only when there is no gap. It appends `forum.moved {from_parent, to_parent, position, renumbered}`.
   - Visibility and archive call `perm`.
4. **`internal/web/api/admin_forums.go`**, with `registerAdminForums` called from `registerAdmin` in `admin.go`. Each route goes through `adminRoute` with `adminGate`:
   - `GET /api/v1/admin/forums`;
   - `POST /api/v1/admin/forums`;
   - `PATCH /api/v1/admin/forums/{id}`;
   - `POST /api/v1/admin/forums/{id}/move`;
   - `PUT /api/v1/admin/forums/{id}/visibility` with `{visibility}`;
   - `PUT /api/v1/admin/forums/{id}/archived` with `{archived}`.

   Writes are JSON-only and charged to the write limiter. All answers are `no-store`. An unknown id is `not-found`.
5. **`internal/dto`**: `AdminForum{ID, ParentID *int64, Slug, Name, Description, Position, Visibility, Archived, Threads, Posts}`. The golden `admin-forums.admin.json` has a root, a child and an archived restricted forum, so `parent_id` appears both null and set. Import it in `web/client/src/api/dto-contract.ts`.
6. **`web/client/src/screens/admin/ForumsScreen.tsx`**, registered in `AdminRoot`'s `built` map under `admin-forums`. The section is already in `sections.ts` (`isAdmin`) and `routes.tsx`. It holds:
   - the tree, roots with their children under them;
   - a create form;
   - inline edits of name and description;
   - Move up and Move down;
   - a "Place under" select listing the roots;
   - a visibility select, with the line "Takes effect within a minute" (the permission cache's lifetime, AC3);
   - an Archive toggle, with the effect in words: "Anyone who could read it still can. Only posting stops, except for admins. Moderators can still hide, lock, pin and move threads out, and only admins can move threads in." The text is the same for every visibility, because archiving changes no read access (review 1558 on PR #209). Text that depends on visibility lost: listing who can read a `restricted` forum would mean restating its grants, and "unchanged" is the whole of what archiving does to reading.

   Controls go through `screens/admin/common/` (`Field`, `ConfirmButton`, `send`, `Table`).
7. **Docs**: a new `####` subsection under docs/07 "Moderation and admin", "Forums (TKT-01M29CXK19)", documents the endpoints. docs/04 "forums" records sparse positions, the fixed slug and reversible archiving. docs/04 "audit_log" gains the six `forum.*` actions, against the forum, with their details.
8. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and run the budget check. The reader total stays at 47415 B.

### Tests

- `TestForumNestsOneLevel`: a child of a child, and a parent for a forum that has children, are `validation-failed`, and the trigger still refuses a direct write.
- `TestMoveWritesOnlyTheMovedRow`: with a gap, only the moved row's `position` and `updated_at` change. With no gap, only that parent's children are renumbered, in the same order, and another parent's rows are untouched.
- `TestMoveKeepsOrderUnderRandomMoves`: 500 random moves checked against a reference slice.
- `TestForumWritesAuditInTheSameTransaction`: a test trigger that aborts the audit insert leaves the forum row unchanged and the perm cache not cleared, for create, update, move, visibility and archive.
- `TestVisibilityChangeAppliesOnNextRequest`: a member lists a forum, the admin makes it `restricted`, and the member's next `GET /forums` lacks it without waiting for the TTL.
- `TestArchivedForumEffect`, after archiving:
  - `GET /forums` still lists the forum to its readers, and a thread in it still reads.
  - A member's reply and create are `conflict`, and so are a moderator's.
  - A moderator may still lock and pin a thread there, hide a post in it, and move a thread out to another forum. Moving a thread in is refused to the moderator and allowed to an admin.
  - An admin's create succeeds.
  - Unarchiving restores posting.
- `TestAdminForumsAreAdminOnly`, beside `TestAdminRoutesAreGated`: a moderator by role is 403, and so is a member who holds `moderate` in the forum through a group.
- `TestMutationsGoThroughPerm` passes unchanged. `SetForumArchived` is already listed.
- `TestArchivedForumIsReadOnly`, `TestArchivedForumConflicts` and the perm cache tests pass with the new signatures.
- Goldens. Client: `ForumsScreen.test.tsx` covers create, the move buttons and the visibility note. Its case `the archive text says read access is unchanged` checks that the archive toggle's words are exactly the text in step 6 for a `public`, a `members` and a `restricted` forum, and that no words name who can read it. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- The permission evaluation for archived forums: TKT-01M3N24RHD, done.
- Per-forum permission rows and the grid: TKT-01M29CXK1X, done.
- Moving threads between forums: TKT-01M29CKRH9, done. Who may move by grant: TKT-01M3RQP0TG.
- The audit view that reads the `forum.*` rows: TKT-01M29CP2R4.
- Changing a forum's slug, and deleting a forum (decided above). A slug change needs a redirect design first.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:54:55Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) perm.Evaluator.SetForumArchived, its query and its decisionWrites entry landed with TKT-01M3N24RHD, so the steps that created them are removed. This ticket adds by audit.By and the audit row to SetForumArchived and SetVisibility, as ChangeRole and Grant do in perm/admin.go, and updates the four perm test files that call them. (2) The maintainer's decision of 2026-09-29 that moderators may still hide, lock, pin and move threads out of an archived forum is now in the archive words and in TestArchivedForumEffect. Criterion 4 is rewritten. (3) The maintainer's decision of 2026-09-30 keeps admin screens on their role gates. A new criterion covers a member who moderates through a group. (4) docs/04 audit_log gains the six forum.* actions. TKT-01M29CP2R4's audit.Actions list must carry them too, and whichever ticket lands second adds them. (5) docs/07 now has one #### subsection per admin area, and this ticket adds one. (6) The admin chunk criterion is added. The reader total is 47415 B. No migration is needed.

**agent:claude-code/phase2-groom** at 2026-09-30T09:12:41Z

Terva review 1558 on PR #209 (head 608a468), one medium finding, accepted. The planned archive words 'Members and moderators can still read it.' promised more than a restricted forum gives. The screen now says 'Anyone who could read it still can. Only posting stops, except for admins.', followed by what moderators keep. The words are the same for every visibility, because archiving changes no read access. Words that depend on visibility lost, because they would have to restate a restricted forum's grants. The archive criterion is rewritten to quote that text. It names ForumsScreen.test.tsx 'the archive text says read access is unchanged', which checks the text for public, members and restricted forums.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T10:07:10Z

Built on run/cxk19-admin-forums from main at 80e4818. What the next reader needs:

- **Criterion 6, the reader total.** `go generate ./internal/web/spa` reports 47414 B, not 47415 B. The reader bundle's content is unchanged: its raw size is the same 99868 B, and the only difference is the hashed file names of the AdminRoot and ApplyRoot chunks it names in its lazy imports. The new names compress one byte better. I ticked the criterion on that evidence. Admin chunk: JS 18287 to 19700 B gzip (+1413), CSS 2188 to 2238 B (+50).
- **`dto.ManagedForum`, not `dto.AdminForum`.** `AdminForum` is the permission grid's forum, and widening it would have changed that contract. The golden is `admin-forums.admin.json`, as planned.
- **perm.** `SetVisibility` and `SetForumArchived` take `audit.By` and write `forum.visibility_changed` (with `from` and `to`), `forum.archived` or `forum.unarchived`. `SetVisibilityThen` and `SetForumArchivedThen` read in the same transaction. The new errors are `ErrNoForum` (not-found) and `ErrInvalidVisibility` (validation-failed). A change to the value the forum already has writes no audit row. The cache is still cleared after that commit, as it is for a no-op `ChangeRole`, which is safe.
- **What the forums service writes itself.** It writes `forum.created`, `forum.updated` and `forum.moved`, with no perm call. None of them is a decision write: nothing in perm reads a forum's name, description, parent or position, and a new forum has no rows. The tree still hides the children of a root a viewer cannot read. That check happens in `Tree` on every request and is not cached.
- **For TKT-01M29CP2R4 (the audit view).** docs/04 "audit_log" now lists the six `forum.*` actions. If that ticket lands second, its `audit.Actions` list must carry them.
- **Local checks.** gofmt, build, vet, `go test -race ./...`, `tsc --noEmit`, all 1497 Vitest tests, and the budget pass. I did not run Playwright locally because this lane had no e2e port. CI runs it.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T10:30:29Z

Terva review 1571 on PR #213 (head 371d4b2, base 5362311), two findings, both fixed. (1) medium, 'Require parent_id in move requests': an omitted parent_id decoded as nil and moved the forum to the top level. The route now decodes parent_id as raw JSON: absent, or anything but an id or null, is bad-request. TestAdminForumsCRUD covers {"before":N}, {} and a string. ForumMove lost its json tags because the route builds it. (2) low, 'Do not advertise an unregistered forum resource URL': create no longer sets Location, since no route reads one forum. docs/07 says both.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T10:31:00Z

This corrects the mutation claim in commit d18c5fb (answer review 1571). Dropping only the presence check does not make the test fail. The route still answers bad-request, because json.Unmarshal of an empty value fails, so that mutant survives and the behaviour stays right. The mutant that matters is the original bug, which treats an absent parent_id as null. It is caught: TestAdminForumsCRUD's 'a move with an empty body' then answers 200 and fails. I verified this by running it.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T10:45:15Z

Carried PR #213 (head 88b6a74) onto main at 94ce6d8 as one commit on run/cxk19-admin-forums-2 (brief item 10). Main had gained two guards: adminRouteGates and TestThreadModerationRoutesUseTheModeratesGate from #214, and TestFixturesUseTheTemplate with dbtest.Open from #216. Neither can be met by a commit on the old branch without merging main. The carry adds the six /api/v1/admin/forums routes to adminRouteGates as admin, per the 2026-09-30 decision, and opens newAdminFixture through dbtest.Open. Nothing else changed. My commits replayed onto main without conflict, sqlc regenerates with no diff, and the dist rebuilds byte-identical (reader 47414 B). go test -race ./..., tsc and all 1499 Vitest tests pass. #213 is closed in favour of the new PR. Its review of 88b6a74 is superseded, and review 1571's findings stay fixed in the carried tree.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T11:00:20Z

Terva review 1574 on PR #219 (head dbd36e6, base 94ce6d8), two findings, both fixed. (1) medium, 'Prevent older write responses from replacing a newer forum list': changes on different rows could be in flight at once, and a later-arriving older whole-list answer hid a committed change. The screen now saves one change at a time. A shared busy flag disables every control that changes something, and an in-flight guard refuses a change asked for anyway with a sentence, without sending it. ForumsScreen.test.tsx 'saves one change at a time' holds a move in flight and checks both, and removing the guard or the shared flag fails it. Serializing won over sequence-numbering answers, because send order is not commit order, so a newer answer could be discarded. It also won over reloading after each write, which costs a request and still races. (2) low, 'Reject non-positive parent IDs as bad requests': a parent_id of 0 or less, and a negative before or after, are now bad-request, and TestAdminForumsCRUD covers all four. docs/07 says so. The reader total is 47415 B after the rebuild, and the admin chunk JS is 19801 B gzip.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T11:12:11Z

Terva review 1576 on PR #219 (head 432e871, base 0ece891): status success. Review 1574's two findings are marked resolved. It has one low finding, fixed. 'Reject non-positive parent IDs on forum creation': the create route now answers bad-request to a parent_id of 0 or less, as the move route does, and TestAdminForumsCRUD covers 0 and -1. Removing the check turns both into 422 and fails them. docs/07's create row says so.

**agent:claude-code/phase2-run-cxk19** at 2026-09-30T11:26:03Z

Carried PR #219 (head fed1e25) onto main at ff2e95a as one commit on run/cxk19-admin-forums-3 (brief item 10). #220, the audit view, landed audit.Actions and docs/04's audit-actions table, and conflicted with #219 in admin.go, admin_test.go and dto-contract.ts. All three conflicts were additions on both sides, and both sides are kept. The carry adds the six forum.* actions to audit.Actions and to docs/04's table, each against target forum. TestActionsCoverTheWriters and TestActionsDocumented pass, and removing two of the six makes the first fail naming their writers. dbtest.Open and the adminRouteGates entries are in place, and all three guards pass. sqlc and the dist regenerate unchanged (reader 47415 B). go test -race ./..., tsc and all 1502 Vitest tests pass. #219's review of fed1e25 was dispatched and is superseded. Review 1576's low finding stays fixed in the carried tree.

## Summary

The /admin/forums screen and its six admin-only routes are built. An admin can create, rename, move (sparse positions, relative to a sibling, renumbering only when no gap is left), set the visibility of, and archive or unarchive a forum. Every write is audited in its transaction as one of forum.created, updated, moved, visibility_changed, archived or unarchived. Visibility and archiving go through perm, which clears the decision cache on commit. The screen is in the lazy admin chunk, and its archive words are the same for every visibility. All seven criteria are ticked. The reader bundle is unchanged apart from chunk file names (47414 B).
