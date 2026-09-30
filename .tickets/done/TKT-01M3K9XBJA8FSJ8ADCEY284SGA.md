---
schema: 3
id: TKT-01M3K9XBJA8FSJ8ADCEY284SGA
title: Autosave new-thread drafts with their title
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:05:54Z
updated_at: 2026-09-30T10:14:33Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-k9xbja
  name: ""
extensions: {}
---

## Description

The drafts table (TKT-01M29BYYHQ) keys a new-thread draft by forum: `thread_id` null, `forum_id` set, one per user per forum through the `drafts_new_thread` index. TKT-01M29CA9CE built only the reply draft, `GET/PUT/DELETE /api/v1/drafts/{thread_id}`, because that is what its criteria name, and a new-thread draft has no thread id to put in that URL.

Two things are open, and the first is a product decision:

1. Whether a new-thread draft saves the thread's title. The table has no title column (docs/04 lists none), so as it stands the body autosaves and the title does not. Adding one is a one-line migration, but it changes what a member gets back on another device, which is a person's call.
2. The route. `/api/v1/forums/{slug}/draft` fits the forum-keyed row and the create route `POST /forums/{slug}/threads`; the same rules as the reply draft would apply (member session, forum readable, `create_thread` to save, 204 when there is none).

The form-post table in docs/07 "Form posts for viewer state" needs a row for whatever route is chosen, and the test in internal/web/api that checks it needs the route added to its list.

## Acceptance criteria

- [x] GET, PUT and DELETE /api/v1/forums/{slug}/draft save, read back and delete a member's title and source per user and per forum, answering 204 when there is none, and a PUT without title clears it (TestForumDraftRoundTrip, TestForumDraftIsPerUserAndForum, TestForumDraftPutReplacesTheTitle)
- [x] docs/07 Drafts and its resources table describe the routes, its Form posts for viewer state table has a row for each, and TestEveryViewerStateRouteHasAFormPost lists them
- [x] Saving needs create_thread (forbidden otherwise), an archived forum is conflict to a member and saves for an admin, a hidden forum is not-found, anonymous is unauthenticated, and a title over MaxTitle or with a control character is validation-failed
- [x] dto.ForumDraft has a golden fixture and an entry in the client contract, and dto.Draft's goldens carry revision
- [x] PUT and DELETE on /api/v1/forums/{slug}/draft are in unchargedPaths and TestAgentBucket shows an agent's bucket is not charged for them
- [x] The migration, at the next free number, adds drafts.title, nullable with a CHECK that keeps it null on reply drafts, and drafts.revision and drafts.deleted, and docs/04 drafts matches the schema and names both draft routes (TestDraftUniqueness)
- [x] A draft's revision only grows and reads no clock: with the service clock fixed and then stepped back, PUT, PUT, DELETE, PUT answer 1, 2, 3 in Draft-Revision on the 204, and 4, for each kind of draft (TestDraftRevisionSurvivesDeletion, TestDraftRevisionIsPerKey, TestDeletingNothingKeepsTheRevision)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Refreshed against main at 5124209 on 2026-09-30: the migration takes the next free number, TKT-01M3N24RHD's admin exemption has landed, the route table and the agent bucket are new since, and this ticket now also carries the server half of the draft revision that TKT-01M3NS1931's note of 2026-09-29 requires. Revised on 2026-09-30 for Terva review 1559 on PR #210: the revision is a per-key count kept across deletion by a tombstone, with no clock in it. Checked on claim against main at 5362311 on 2026-09-30 (agent:claude-code/phase2-run-k9xbja): nothing the plan names has changed since 5124209, so it stands as written, with the migration at 0018 unless main has taken it by push time.

### Settled in grooming
- **The title is saved.** Maintainer decision of 2026-09-28, recorded in this ticket's note and in docs/04 `drafts`: "a new-thread draft saves its title as well as its body ... a draft restored on another device should come back whole. The `title` column is added by that ticket's migration, and a reply draft leaves it null. That ticket also adds the route."
- A new-thread draft is keyed by forum: `thread_id` null, `forum_id` set, one per user per forum through the `drafts_new_thread` partial unique index (`internal/store/db/migrations/0005_viewer_state.sql:80`, TKT-01M29BYYHQ).
- It follows the reply draft's rules (docs/07 "Drafts"): a member session, the forum readable, 204 when there is none, `no-store`, and `PUT`/`DELETE` charged to the write limiter. Saving needs `create_thread`, as this ticket's description says, because a new-thread draft is a thread being written.

Settled here as engineering choices, to be written into docs/07 "Drafts":
- **Route:** `GET/PUT/DELETE /api/v1/forums/{slug}/draft`. It sits beside the create route `POST /api/v1/forums/{slug}/threads` and names the key the row has. The alternative `/api/v1/drafts/forum/{slug}` lost because it mixes two key spaces under one prefix, and `/drafts/{thread_id}` would have to tell a thread id from a slug.
- **Saving** goes through the same forum check as creating a thread, `forumForThread` (`internal/service/threads/create.go:217-233`): read access, `create_thread`, and an archived forum answers `conflict`. When TKT-01M3N24RHD gives admins their exemption in archived forums, drafts follow it with no change here. `GET` and `DELETE` need only read access, as for a reply draft.
- **Body:** `PUT` takes `{title?, source}`. `source` is required but may be empty, as for a reply. `title` is optional, and when absent the stored title is null: `PUT` replaces the whole draft. The title is checked with `checkTitle`'s rules (`create.go`) except two: it may be empty, and it is stored as typed, not trimmed, because it is unfinished. It is capped at `MaxTitle` and refused with control characters, both as `validation-failed`.
- **DTO:** a new `dto.ForumDraft{forum, title, source, updated_at}`, where `forum` is the slug and `title` is omitted when null. It is not a `title` field on `dto.Draft`: a reply draft never carries a title, and one type per key keeps both goldens honest.
- **Blob GC** keeps reading `drafts.source` only (`DeleteCollectableBlob`, `internal/store/db/queries/moderation.sql:214-220`). A title is plain text and never holds an attachment reference.

Settled in the refresh of 2026-09-30:
- **The admin exemption has landed.** TKT-01M3N24RHD is done (PR #182). `forumForThread` now answers `conflict` for an archived forum to everyone but an admin (`create.go:222`), before it checks `create_thread`. So a member's draft save in an archived forum is `conflict`, and an admin's is saved, with no code here for either.
- **Drafts carry a server revision that only grows** (engineering choice, taken for TKT-01M3NS1931; revised for Terva review 1559 on PR #210, finding 1). That ticket's note of 2026-09-29, from the round-5 review of PR #143, makes it a requirement: "The local backup and the server draft are ordered by a revision counter shared by both sides, carried in the draft body and bumped on every save, never by comparing the client's clock with the server's updated_at." The counter is the server's, so this ticket's migration adds it and both draft routes answer it. It is a count per draft key that survives the draft's deletion, and no clock enters it:
  - `drafts.revision INTEGER NOT NULL DEFAULT 0`, and `drafts.deleted INTEGER NOT NULL DEFAULT 0`, 0 or 1;
  - `DELETE` no longer removes the row. It turns a live row into a tombstone: `deleted = 1`, `source = ''`, `reply_to` and `title` null, and `revision + 1`. On a tombstone, or a key with no row, it changes nothing;
  - `PUT` inserts a new row at `revision = 1`, and otherwise sets `deleted = 0` with `revision + 1`, a tombstone included. Each is one statement in one write transaction;
  - `GET` answers 204 for a tombstone as for no row;
  - every answer carries the key's revision: the body's `revision` on a 200, and a `Draft-Revision` response header on a 204 from `GET` or `DELETE`, which is 0 when the key has never had a row. `dto.Draft` and `dto.ForumDraft` gain `revision`.

  A draft deleted and started again therefore always answers a higher revision than any copy of the old one, whatever the server's clock does. The partial unique indexes `drafts_thread` and `drafts_new_thread` keep one row per key, tombstone or live, so the upsert revives it. Tombstones are never swept: a sweep would restart the count. They number at most one per thread or forum a member has ever drafted in, and blob GC (`DeleteCollectableBlob`) finds no hash in their empty source.

  The client's use of it is TKT-01M3NS1931's plan. Five alternatives lost:
  - A new row starting at the server's time in milliseconds (this plan's first revision). A delete and recreate within one millisecond, or across a clock stepped back, answers a revision equal to or lower than the old one, and a stale local copy then wins (review 1559).
  - A per-user counter row incremented in the same transaction as every draft write. It is monotonic, but it counts every key's writes, so a save in another thread moves this key's revision too. The client's test that nothing but its own requests reached the key since the copy was made (`revision <= base + inflight`, TKT-01M3NS1931) would then fail after any other draft was saved, and the local copy would lose where it should win.
  - A separate per-key counter table. It repeats the draft's key and both partial unique indexes, and every write touches two tables where the tombstone touches one.
  - A counter that restarts at 1 for a new row. A draft deleted and started again could compare as older than a stale local copy.
  - Answering a tombstone as 200 with an empty draft instead of 204 with a header. It changes "204 when there is none", which the reply-draft route (TKT-01M29CA9CE), its tests and docs/07 promise.
- **Migrations are numbered when they are pushed.** This ticket's migration takes the next free number when it lands. At 5124209 that is 0018, and TKT-01M29CP2R4 also plans 0018, so whichever lands second renumbers (`migrate.go:28-29`).
- **Agents are not charged for drafts.** `unchargedPaths` in `internal/web/api/agents.go:47-51` lists `/api/v1/drafts/{thread_id}`, as docs/07 says ("The read position and drafts are not charged", `:870-872`). `/api/v1/forums/{slug}/draft` joins it.

### Approach
1. **Migration** `internal/store/db/migrations/NNNN_draft_title_revision.sql`, at the next free number when it is pushed (0018 at 5124209, which TKT-01M29CP2R4 also plans). Its header comment says why the columns exist and why tombstones are never swept, as `0017_thread_slow_mode.sql`'s does:
   - `ALTER TABLE drafts ADD COLUMN title TEXT CHECK (title IS NULL OR thread_id IS NULL);`
   - `ALTER TABLE drafts ADD COLUMN revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0);`
   - `ALTER TABLE drafts ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0 CHECK (deleted = 0 OR (deleted = 1 AND source = '' AND reply_to IS NULL AND title IS NULL));`

   Regenerate `dbq` with `go generate ./internal/store/db`, so `dbq.Draft` gains `Title sql.NullString`, `Revision int64` and `Deleted int64`. If another branch lands the same number first, this one renumbers on rebase (`migrate.go:28-29`).
2. **Queries** in `internal/store/db/queries/viewer_state.sql`:
   - `UpsertThreadDraft` (`:196-203`) inserts `revision = 1` and, on conflict, sets `deleted = 0` and `revision = drafts.revision + 1`;
   - `DeleteThreadDraft` (`:205-206`) becomes an `UPDATE ... SET deleted = 1, source = '', reply_to = NULL, revision = revision + 1, updated_at = sqlc.arg(now) WHERE user_id = ? AND thread_id = ? AND deleted = 0`, `:execrows`;
   - `GetThreadDraft` (`:190-191`) keeps returning the row, tombstone or not, so the service reads the revision from it;
   - beside them: `GetForumDraft :one` (`WHERE user_id = ? AND forum_id = ? AND thread_id IS NULL`), `UpsertForumDraft :one` (`ON CONFLICT (user_id, forum_id) WHERE thread_id IS NULL DO UPDATE SET title, source, updated_at, deleted = 0, revision = drafts.revision + 1 ... RETURNING *`), and `DeleteForumDraft :execrows`, the same tombstoning `UPDATE`.

   Every revision is written by SQL from the stored value. No query takes it from Go.

   Drop the comment at `:188-189` that says new-thread drafts have no endpoint.
3. **Service** in `internal/service/threads/viewerstate.go`:
   - `ForumDraft(ctx, v, slug) (dto.ForumDraft, bool, error)`, `SaveForumDraft(ctx, v, slug, ForumDraftRequest{Title *string; Source string})` and `DeleteForumDraft(ctx, v, slug) error`;
   - each starts with `MayKeepState(v)` (`:31`), before any forum lookup, so an anonymous request is `unauthenticated` whatever the slug;
   - save uses `forumForThread`; get and delete use `readForum` (`list.go:210-234`), so a hidden forum is `not-found`;
   - `draftDTO` (`:233`) fills `Revision`, and a `forumDraftDTO` beside it builds the new type;
   - `Draft` and `ForumDraft` answer `false` for a tombstone as for no row, with the revision beside it (0 for no row). `DeleteDraft` and `DeleteForumDraft` tombstone the row and answer the revision they leave, read back in the same write transaction.
4. **Handlers** in `internal/web/api/viewerstate.go`: `getForumDraft`, `putForumDraft` and `deleteForumDraft`, beside `getDraft`, `putDraft` and `deleteDraft` (`:94`, `:118`, `:147`). They follow those handlers: `decodeStrict` (`create.go:99`) into `forumDraftRequest{Title *string "title"; Source *string "source"}`, a nil source is `bad-request`, the answers are 200, 204 and 204, and every one is `no-store`. Every 204 from a draft route, the reply draft's `getDraft` and `deleteDraft` included, sets `Draft-Revision`. Register the three in `routes()` in `api.go`, as `{Method, Path, Handler}` entries beside the reply draft's (`:127-129`) and the create route (`:131`). Add `/api/v1/forums/{slug}/draft` to `unchargedPaths` (`agents.go:47-51`).
5. **DTO** in `internal/dto/dto.go`:
   - `Draft` (`:536-541`) gains `Revision int64 "revision"`, and the goldens `draft.member.json` and `draft-plain.member.json` are regenerated;
   - a new `ForumDraft`, with a golden `internal/dto/testdata/golden/forum-draft.member.json` built in `fixtures_test.go` beside the draft builders (`:466-467`, `:621-630`).

   Add both to the client contract `web/client/src/api/dto-contract.ts` and the types to `web/client/src/api/dto.ts` (`Draft` is at `:296-299`), so the contract test covers them. No screen uses them yet. A type-only change leaves the dist byte-identical, as it did for PR #205. If it does not, run `go generate ./internal/web/spa` and keep the dist committed.
6. **docs/07**:
   - "Drafts" (`:786-812`): replace the "has no route yet" bullet (`:810-812`) with the three routes and the rules above, and say what `revision` is, that it only grows because a deleted draft is kept as a tombstone, that a 204 carries it as `Draft-Revision`, and that a client orders a local copy by it.
   - The resources table's drafts row (`:133`): add the routes.
   - "Form posts for viewer state" (`:994`): add three rows after the reply draft's (`:1010-1012`), whose HTML-client routes mirror the reply draft's:
     - `GET /api/v1/forums/{slug}/draft`: none (the new-thread page fills its form from the draft), `ForumDraft`;
     - `PUT`: `POST /f/{slug}/draft` (the new-thread form's "Save draft" button), fields `title`, `source`, `SaveForumDraft`;
     - `DELETE`: `POST /f/{slug}/draft/delete`, `DeleteForumDraft`.
   - The section's sentence "The routes live under the HTML client's `/t/{id}` paths" (`:1001-1002`) becomes "under the HTML client's `/t/{id}` and `/f/{slug}` paths".
7. **docs/04** `drafts` (`04-data-model.md:640-653`) already shows `title`. Add `revision` and `deleted` to the column list, with a sentence on the tombstone, and change "The API serves reply drafts only (07 "Drafts")" to name both routes.

### Tests
- `TestDraftUniqueness` (`internal/store/db/viewer_state_test.go:70`): add a reply draft with a title (refused by the CHECK), and a new-thread draft with a title and one without; a tombstone with a source is refused, and a tombstone holds its key's slot in `drafts_thread` and `drafts_new_thread`.
- Service and API, in `internal/web/api/viewerstate_test.go`:
  - `TestForumDraftRoundTrip`: PUT, then GET returns the same title and source, then DELETE, then GET answers 204;
  - `TestForumDraftIsPerUserAndForum`: alice's draft in `general` is not carol's, and not alice's in another forum;
  - `TestForumDraftSaveNeedsCreateThread`: `forbidden` without `create_thread`;
  - `TestForumDraftInAnArchivedForumIsConflict`: `conflict` for a member, and saved for an admin;
  - `TestForumDraftInAHiddenForumIsNotFound`;
  - `TestForumDraftTitleRules`: over `MaxTitle` or with a control character is `validation-failed`, empty is accepted, and whitespace is kept;
  - `TestForumDraftNeedsAMemberSession`: anonymous is `unauthenticated`, and pre-user is refused as for reply drafts;
  - `TestForumDraftPutReplacesTheTitle`: a PUT without `title` clears it;
  - `TestDraftRevisionSurvivesDeletion`: for each kind of draft, with the service clock fixed at one instant and then stepped back an hour between requests, PUT answers `revision` 1, a second PUT 2, DELETE answers 204 with `Draft-Revision: 3`, GET answers 204 with `Draft-Revision: 3`, and a new PUT answers 4. No wall clock is read.
  - `TestDraftRevisionIsPerKey`: a save to another thread's or forum's draft leaves this key's revision where it was.
  - `TestDeletingNothingKeepsTheRevision`: DELETE on a key with no row answers 204 with `Draft-Revision: 0` and writes no row; DELETE on a tombstone answers its revision unchanged.
- `TestEveryViewerStateRouteHasAFormPost` (`viewerstate_test.go:363-412`): add the three routes to `served`, and extend the `strings.NewReplacer` at `:375` with `{slug}`.
- `TestAgentBucket` (`internal/web/api/agents_test.go:318`): add `PUT` and `DELETE` on `/api/v1/forums/general/draft` to its uncharged requests (`:343-346`).
- `TestPreUserRoutesAreExactlyTheAllowlist` (`internal/web/preuser_test.go:284`) walks every route in the table, so it covers the new ones unchanged.
- DTO goldens `draft.member.json`, `draft-plain.member.json` and `forum-draft.member.json`, and the client contract test.

### Out of scope
- A client screen. The new-thread route `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:119`), built by TKT-01M3NS16VH (Build the new-thread screen with draft autosave), which depends on this ticket.
- Reply-draft autosave in the composer, and the client's use of `revision`: TKT-01M3NS1931 (Autosave reply drafts from the composer), which now depends on this ticket for `revision`.
- The admin exemption in archived forums: TKT-01M3N24RHD, done.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: a new-thread draft saves the title. Add a nullable title column to drafts (null for reply drafts) in this ticket's migration. docs/04 'drafts' now shows the column and says this ticket adds it. Moved into phase-2 during grooming. Carry this into the plan's Settled section.

**agent:claude-code/phase2-groom** at 2026-09-30T08:58:36Z

Plan refreshed against main at 5124209 on 2026-09-30. Changed: the migration is 0018 (0011 to 0017 are taken; 0017_thread_slow_mode.sql is the latest). TKT-01M3N24RHD is done, so forumForThread already answers conflict for an archived forum to everyone but an admin: a member's draft save there is conflict and an admin's saves, and criterion 4 (was 3) says so. Routes are now a table in api.go's routes(). New since the plan: unchargedPaths in internal/web/api/agents.go exempts drafts from an agent's bucket, and the forum draft path must join it (new criterion, TestAgentBucket). Widened as an engineering choice: this ticket's migration also adds drafts.revision, the server half of the revision counter that TKT-01M3NS1931's note of 2026-09-29 (round-5 review of PR #143) requires. A new row starts at the save's server time in ms and each update adds one, so a draft deleted and started again is newer than any stale local copy. Both DTOs carry it. A separate ticket lost because it would land in draft and block TKT-01M3NS1931, and would add a second migration to drafts; a counter restarting at 1 and keeping deleted rows as empty tombstones lost for the reasons in the plan. Line references corrected throughout (forumForThread create.go:217-233, DeleteCollectableBlob moderation.sql:214-220, viewer_state.sql :188-203, readForum list.go:210-234, TestDraftUniqueness :70, the replacer :375, docs/07 :133, :786-812, :994-1012, routes.tsx:119); docs/07's 'routes live under /t/{id}' sentence (:1001-1002) is added to the doc step. The Settled items are carried verbatim except their corrected line numbers.

**agent:claude-code/phase2-groom** at 2026-09-30T09:23:42Z

Terva review 1559 on PR #210 (head 922a23c), finding 1 (high), accepted. A revision started from server milliseconds could repeat or fall after a delete and recreate within one millisecond or across a clock step back, letting a stale local copy win. Settled as an engineering choice: the revision is a per-key count that survives deletion. DELETE turns a live draft into a tombstone (deleted = 1, empty source, revision + 1) instead of removing the row; PUT inserts at 1 or revives with revision + 1; GET answers 204 for a tombstone; every 204 from a draft route carries Draft-Revision (0 for a key never saved). Every revision is computed in SQL from the stored value, in the write's own transaction, and no clock enters it. Lost, with reasons in the plan: server milliseconds (this finding); a per-user counter row, which is monotonic but counts other keys' writes and so breaks TKT-01M3NS1931's base + inflight test; a separate per-key counter table, which repeats the key and both partial unique indexes; restarting at 1; and a 200 tombstone body, which breaks '204 when there is none'. Tombstones are never swept, since a sweep would restart the count. Tests: TestDraftRevisionSurvivesDeletion (insert, update, delete, insert with the service clock fixed and then stepped back), TestDraftRevisionIsPerKey, TestDeletingNothingKeepsTheRevision. The migration is now named by the next free number, since TKT-01M29CP2R4 also plans 0018. Criteria 6 and 7 replace the old migration and millisecond criteria.

**agent:claude-code/phase2-run-k9xbja** at 2026-09-30T10:14:17Z

Built to the plan on run/k9xbja-forum-drafts (commit 930b8b7 on the branch). Migration 0018 was the next free number on main at 5362311. Settled while building, as engineering choices that affect only internals:
- 0018 sets every draft stored before it to revision 1. A stored draft was saved at least once, and a save answers 1 or more, so 0 keeps the one meaning the plan gives it: a key never saved. The alternative, leaving them at the column default 0, would let a live draft answer the revision a client reads as "never saved". Tested by TestMigrationGivesStoredDraftsARevision.
- The service's Draft and ForumDraft answer false for a tombstone and carry the key's revision in the returned DTO's Revision, with every other field zero. DeleteDraft and DeleteForumDraft return the revision they leave, read back in the delete's own write transaction. A separate return value lost because it would put a fourth result on both reads for one number the DTO already has a field for.
- A second golden, forum-draft-untitled.member.json, pins the omitted title, since `title` is `omitempty` and one golden cannot show both shapes.
- docs/07's form-post preamble now says a thread's slug is advisory and a forum's slug is its key, because the new rows live under /f/{slug}, where the slug is the key.
- The reply draft's DELETE of a draft that is already a tombstone, or was never saved, writes nothing, so a retried delete never moves the revision (TestDeletingNothingKeepsTheRevision).
Every criterion is met. CI's local set passed: gofmt, vet, go test -race ./..., sqlc regeneration unchanged, go generate ./internal/web/spa leaving the dist byte-identical, tsc, and vitest (1485 tests). Mutations tried are listed in the commit body, and each was caught.

## Summary

New-thread drafts with their title are served at GET/PUT/DELETE /api/v1/forums/{slug}/draft: per user and forum, saved through forumForThread (create_thread; archived is conflict but for an admin), read and discarded with read access, uncharged to an agent's bucket. Every draft now carries a revision that only grows: migration 0018 adds drafts.title, drafts.revision and drafts.deleted, a delete leaves a tombstone one revision on, revisions are computed in SQL with no clock, and every 204 from a draft route carries Draft-Revision (0 for a key never saved). Drafts stored before 0018 read as revision 1. dto.Draft gains revision, dto.ForumDraft is new; docs/04 and docs/07 describe both. The client half of the ordering is TKT-01M3NS1931, and the new-thread screen is TKT-01M3NS16VH. All seven criteria met.
