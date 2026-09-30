---
schema: 3
id: TKT-01M3K9XBJA8FSJ8ADCEY284SGA
title: Autosave new-thread drafts with their title
type: task
status: ready
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
updated_at: 2026-09-30T08:58:36Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] GET, PUT and DELETE /api/v1/forums/{slug}/draft save, read back and delete a member's title and source per user and per forum, answering 204 when there is none, and a PUT without title clears it (TestForumDraftRoundTrip, TestForumDraftIsPerUserAndForum, TestForumDraftPutReplacesTheTitle)
- [ ] docs/07 Drafts and its resources table describe the routes, its Form posts for viewer state table has a row for each, and TestEveryViewerStateRouteHasAFormPost lists them
- [ ] Migration 0018 adds drafts.title, nullable, with a CHECK that keeps it null on reply drafts, and drafts.revision, and docs/04 drafts matches the schema and names both draft routes (TestDraftUniqueness)
- [ ] Saving needs create_thread (forbidden otherwise), an archived forum is conflict to a member and saves for an admin, a hidden forum is not-found, anonymous is unauthenticated, and a title over MaxTitle or with a control character is validation-failed
- [ ] dto.ForumDraft has a golden fixture and an entry in the client contract, and dto.Draft's goldens carry revision
- [ ] Every PUT of either kind of draft answers a revision: the save's server time in ms for a new row and one more than before for an existing one, so a draft deleted and started again is newer (TestDraftRevisionIsBumpedOnEverySave, TestADraftStartedAgainIsNewer)
- [ ] PUT and DELETE on /api/v1/forums/{slug}/draft are in unchargedPaths and TestAgentBucket shows an agent's bucket is not charged for them

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Refreshed against main at 5124209 on 2026-09-30: the migration is renumbered to 0018, TKT-01M3N24RHD's admin exemption has landed, the route table and the agent bucket are new since, and this ticket now also carries the server half of the draft revision that TKT-01M3NS1931's note of 2026-09-29 requires.

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
- **Drafts carry a server revision** (engineering choice, taken for TKT-01M3NS1931). That ticket's note of 2026-09-29, from the round-5 review of PR #143, makes it a requirement: "The local backup and the server draft are ordered by a revision counter shared by both sides, carried in the draft body and bumped on every save, never by comparing the client's clock with the server's updated_at." The counter is the server's, so this ticket's migration adds it and both draft routes answer it:
  - `drafts.revision INTEGER NOT NULL DEFAULT 0`;
  - a `PUT` that updates a row sets `revision = drafts.revision + 1`, and a `PUT` that inserts one sets it to the save's time in milliseconds on the server's clock. A new row therefore starts above any revision an earlier row for the same key reached, because a row gains one per save and the write limiter allows a burst of 60 and then two a second (`internal/web/ratelimit.go:50`), while the clock gains a thousand a second. A client comparing an old copy with a draft that was deleted and started again sees the new one as newer;
  - `dto.Draft` and `dto.ForumDraft` gain `revision`, a JSON number below 2^53;
  - `DELETE` still removes the row, and `GET` still answers 204 when there is none.

  The client's use of it is TKT-01M3NS1931's plan. Three alternatives lost:
  - A separate ticket for the column. It would land in draft and block TKT-01M3NS1931 until someone promoted it, and it would add a second migration to the same table.
  - A counter that restarts at 1 for a new row. A draft deleted and started again on another device could then compare as older than a stale local copy, and the stale copy would overwrite it.
  - Keeping a deleted draft as an empty row, so the counter never restarts. It changes `DELETE` and `GET` 204 for TKT-01M29CA9CE's route, which the millisecond start avoids.
- **Agents are not charged for drafts.** `unchargedPaths` in `internal/web/api/agents.go:47-51` lists `/api/v1/drafts/{thread_id}`, as docs/07 says ("The read position and drafts are not charged", `:870-872`). `/api/v1/forums/{slug}/draft` joins it.

### Approach
1. **Migration** `internal/store/db/migrations/0018_draft_title_revision.sql`, the next free number after `0017_thread_slow_mode.sql`:
   - `ALTER TABLE drafts ADD COLUMN title TEXT CHECK (title IS NULL OR thread_id IS NULL);`
   - `ALTER TABLE drafts ADD COLUMN revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0);`

   Regenerate `dbq` with `go generate ./internal/store/db`, so `dbq.Draft` gains `Title sql.NullString` and `Revision int64`. If another branch lands 0018 first, this one renumbers on rebase (`migrate.go:28-29`).
2. **Queries** in `internal/store/db/queries/viewer_state.sql`:
   - `UpsertThreadDraft` (`:196-203`) inserts `revision` as `sqlc.arg(now)` and sets `revision = drafts.revision + 1` on conflict;
   - beside `GetThreadDraft` (`:190-191`): `GetForumDraft :one` (`WHERE user_id = ? AND forum_id = ? AND thread_id IS NULL`), `UpsertForumDraft :one` (`ON CONFLICT (user_id, forum_id) WHERE thread_id IS NULL DO UPDATE SET title, source, updated_at, revision = drafts.revision + 1 ... RETURNING *`), and `DeleteForumDraft :execrows`.

   Drop the comment at `:188-189` that says new-thread drafts have no endpoint.
3. **Service** in `internal/service/threads/viewerstate.go`:
   - `ForumDraft(ctx, v, slug) (dto.ForumDraft, bool, error)`, `SaveForumDraft(ctx, v, slug, ForumDraftRequest{Title *string; Source string})` and `DeleteForumDraft(ctx, v, slug) error`;
   - each starts with `MayKeepState(v)` (`:31`), before any forum lookup, so an anonymous request is `unauthenticated` whatever the slug;
   - save uses `forumForThread`; get and delete use `readForum` (`list.go:210-234`), so a hidden forum is `not-found`;
   - `draftDTO` (`:233`) fills `Revision`, and a `forumDraftDTO` beside it builds the new type.
4. **Handlers** in `internal/web/api/viewerstate.go`: `getForumDraft`, `putForumDraft` and `deleteForumDraft`, beside `getDraft`, `putDraft` and `deleteDraft` (`:94`, `:118`, `:147`). They follow those handlers: `decodeStrict` (`create.go:99`) into `forumDraftRequest{Title *string "title"; Source *string "source"}`, a nil source is `bad-request`, the answers are 200, 204 and 204, and every one is `no-store`. Register the three in `routes()` in `api.go`, as `{Method, Path, Handler}` entries beside the reply draft's (`:127-129`) and the create route (`:131`). Add `/api/v1/forums/{slug}/draft` to `unchargedPaths` (`agents.go:47-51`).
5. **DTO** in `internal/dto/dto.go`:
   - `Draft` (`:536-541`) gains `Revision int64 "revision"`, and the goldens `draft.member.json` and `draft-plain.member.json` are regenerated;
   - a new `ForumDraft`, with a golden `internal/dto/testdata/golden/forum-draft.member.json` built in `fixtures_test.go` beside the draft builders (`:466-467`, `:621-630`).

   Add both to the client contract `web/client/src/api/dto-contract.ts` and the types to `web/client/src/api/dto.ts` (`Draft` is at `:296-299`), so the contract test covers them. No screen uses them yet. A type-only change leaves the dist byte-identical, as it did for PR #205. If it does not, run `go generate ./internal/web/spa` and keep the dist committed.
6. **docs/07**:
   - "Drafts" (`:786-812`): replace the "has no route yet" bullet (`:810-812`) with the three routes and the rules above, and say what `revision` is and that a client orders a local copy by it.
   - The resources table's drafts row (`:133`): add the routes.
   - "Form posts for viewer state" (`:994`): add three rows after the reply draft's (`:1010-1012`), whose HTML-client routes mirror the reply draft's:
     - `GET /api/v1/forums/{slug}/draft`: none (the new-thread page fills its form from the draft), `ForumDraft`;
     - `PUT`: `POST /f/{slug}/draft` (the new-thread form's "Save draft" button), fields `title`, `source`, `SaveForumDraft`;
     - `DELETE`: `POST /f/{slug}/draft/delete`, `DeleteForumDraft`.
   - The section's sentence "The routes live under the HTML client's `/t/{id}` paths" (`:1001-1002`) becomes "under the HTML client's `/t/{id}` and `/f/{slug}` paths".
7. **docs/04** `drafts` (`04-data-model.md:640-653`) already shows `title`. Add `revision` to the column list, and change "The API serves reply drafts only (07 "Drafts")" to name both routes.

### Tests
- `TestDraftUniqueness` (`internal/store/db/viewer_state_test.go:70`): add a reply draft with a title (refused by the CHECK), and a new-thread draft with a title and one without.
- Service and API, in `internal/web/api/viewerstate_test.go`:
  - `TestForumDraftRoundTrip`: PUT, then GET returns the same title and source, then DELETE, then GET answers 204;
  - `TestForumDraftIsPerUserAndForum`: alice's draft in `general` is not carol's, and not alice's in another forum;
  - `TestForumDraftSaveNeedsCreateThread`: `forbidden` without `create_thread`;
  - `TestForumDraftInAnArchivedForumIsConflict`: `conflict` for a member, and saved for an admin;
  - `TestForumDraftInAHiddenForumIsNotFound`;
  - `TestForumDraftTitleRules`: over `MaxTitle` or with a control character is `validation-failed`, empty is accepted, and whitespace is kept;
  - `TestForumDraftNeedsAMemberSession`: anonymous is `unauthenticated`, and pre-user is refused as for reply drafts;
  - `TestForumDraftPutReplacesTheTitle`: a PUT without `title` clears it;
  - `TestDraftRevisionIsBumpedOnEverySave`: for each kind of draft, the first PUT answers the server's time in milliseconds as `revision`, each later PUT answers one more, and GET answers what the last PUT did;
  - `TestADraftStartedAgainIsNewer`: after a DELETE, a new PUT for the same key answers a revision above the one the deleted draft reached.
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
