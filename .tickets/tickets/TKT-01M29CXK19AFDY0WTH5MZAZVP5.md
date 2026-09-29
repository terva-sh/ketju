---
schema: 3
id: TKT-01M29CXK19AFDY0WTH5MZAZVP5
title: Build the admin forum management screens
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-29T07:18:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Create, edit, reorder, and archive forums, including the one level of nesting and the visibility setting.

Reordering is the fiddly part and the one an admin does most, since forum order is how a community signals what it is about. Positions need to survive an insert in the middle without a renumbering that reorders everything else.

Archiving a forum must not orphan its threads, and what happens to them needs deciding here rather than discovering later.

## Acceptance criteria

- [ ] An admin can create, rename, move under a root, reorder, archive and unarchive a forum through /api/v1/admin/forums, and a second level of nesting is validation-failed (TestForumNestsOneLevel)
- [ ] A move writes only the moved forum's row while its neighbours leave a gap, and otherwise renumbers only its siblings in their existing order (TestMoveWritesOnlyTheMovedRow, TestMoveKeepsOrderUnderRandomMoves)
- [ ] A visibility change goes through perm, applies on the viewer's next request (TestVisibilityChangeAppliesOnNextRequest), and the screen says it takes effect within a minute
- [ ] Archiving leaves every thread in place and readable to whoever could read it, refuses new threads and replies to everyone but admins, and docs/04 and the screen say so (TestArchivedForumEffect)
- [ ] Every forum write appends its audit_log row in the same transaction, and a failed audit insert leaves the forum unchanged (TestForumWritesAuditInTheSameTransaction)

## Implementation plan

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

### Approach

1. `internal/perm/mutate.go`: `SetVisibility(ctx, by audit.By, forumID, visibility)` appends `forum.visibility_changed {from, to}` inside the same `write` transaction. New `SetArchived(ctx, by, forumID, archived bool)` appends `forum.archived` or `forum.unarchived`. Add the query `SetForumArchived` to `queries/forums_groups.sql` and to `decisionWrites` in `layering_test.go`. If `perm` does not take `audit.By` yet, this ticket changes the signatures; only tests call these methods today.
2. `queries/forums_groups.sql`: `UpdateForum` (name, description, updated_at), `SetForumPosition`, `ForumSiblings` (`WHERE parent_id IS ?`, ordered by position and id), `ForumChildCount`. `CreateForum` and `SetForumParent` exist already. Regenerate `dbq`.
3. `internal/service/forums/admin.go`, gated by `admin` from TKT-01M3NRYCP5:
   - `List(ctx, v) ([]dto.AdminForum, error)`: every forum, archived and restricted included, in `ListForums` order, with `threads` and `posts` from `forum_thread_counts`.
   - `Create(ctx, v, by, NewForum{Slug, Name, Description, ParentID, Visibility})`. The slug is 1 to 60 characters of `a-z`, `0-9` and inner `-`, and a taken one is `conflict`. The name is 1 to 100 characters with no control characters, and the description at most 1000. The parent must exist and be a root. The position is the last sibling's plus 1024. Visibility defaults to `members`, the schema's default. It appends `forum.created`.
   - `Update(ctx, v, by, id, ForumEdit{Name, Description *string})` appends `forum.updated {field: {from, to}}`.
   - `Move(ctx, v, by, id, ForumMove{ParentID *int64; Before, After int64})` works in one `db.Write`. The parent must be a root, and a forum with children cannot take a parent (`validation-failed`, checked before the trigger, which stays the backstop). It computes the position and renumbers only when the gap is gone. It appends `forum.moved {from_parent, to_parent, position, renumbered}`.
   - Visibility and archive call `perm`.
4. `internal/web/api/admin_forums.go`: `GET /api/v1/admin/forums`, `POST /api/v1/admin/forums`, `PATCH /api/v1/admin/forums/{id}`, `POST /api/v1/admin/forums/{id}/move`, `PUT /api/v1/admin/forums/{id}/visibility` with `{visibility}`, and `PUT /api/v1/admin/forums/{id}/archived` with `{archived}`. Writes are JSON-only and charged to the write limiter. All are `no-store`. An unknown id is `not-found`.
5. `internal/dto`: `AdminForum{ID, ParentID *int64, Slug, Name, Description, Position, Visibility, Archived, Threads, Posts}`. Golden `admin-forums.admin.json`, with a root, a child and an archived restricted forum, so `parent_id` appears both null and set. Import it in `dto-contract.ts`.
6. `web/client/src/screens/admin/ForumsScreen.tsx`: the tree with roots and their children, a create form, inline edit of name and description, Move up and Move down, a "Place under" select listing roots, a visibility select with the line "Takes effect within a minute" (the permission cache's lifetime, AC3), and an Archive toggle with the effect in words: "Members and moderators can still read it. Only admins can start threads or reply."
7. Docs: docs/07 "Moderation and admin" documents the endpoints. docs/04 "forums" records sparse positions, the fixed slug and reversible archiving.
8. Run `go generate ./internal/web/spa`, commit `dist`, and run the budget check. The reader total is unchanged.

### Tests

- `TestForumNestsOneLevel`: a child of a child, and a parent for a forum that has children, are `validation-failed`, and the trigger still refuses a direct write.
- `TestMoveWritesOnlyTheMovedRow`: with a gap, only the moved row's `position` and `updated_at` change. With no gap, only that parent's children are renumbered, in the same order, and another parent's rows are untouched.
- `TestMoveKeepsOrderUnderRandomMoves`: 500 random moves against a reference slice.
- `TestForumWritesAuditInTheSameTransaction`: a test trigger that aborts the audit insert leaves the forum row unchanged and the perm cache not invalidated, for create, update, move, visibility and archive.
- `TestVisibilityChangeAppliesOnNextRequest`: a member lists a forum, the admin makes it `restricted`, and the member's next `GET /forums` lacks it without waiting for the TTL.
- `TestArchivedForumEffect`: after archiving, `GET /forums` still lists the forum to its readers and a thread in it still reads. A member's reply and create are `conflict`, and an admin's create succeeds (TKT-01M3N24RHD). Unarchiving restores posting.
- `TestAdminForumsAreAdminOnly`, beside `TestAdminRoutesAreGated`: a moderator is 403.
- `TestMutationsGoThroughPerm` covers `SetForumArchived`.
- Goldens. Client: `ForumsScreen.test.tsx` covers create, move buttons, the visibility note and the archive text. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- The permission evaluation for archived forums (TKT-01M3N24RHD).
- Per-forum permission rows and the grid (TKT-01M29CXK1X).
- Moving threads between forums (TKT-01M29CKRH9).
- Changing a forum's slug, and deleting a forum (decided above). A slug change needs a redirect design first.
