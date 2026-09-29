---
schema: 3
id: TKT-01M3K9XBJA8FSJ8ADCEY284SGA
title: Autosave new-thread drafts with their title
type: task
status: draft
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
updated_at: 2026-09-29T05:09:57Z
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

- [ ] Migration 0011 adds drafts.title, nullable, with a CHECK that keeps it null on reply drafts, and docs/04 drafts matches the schema and names both draft routes (TestDraftUniqueness)
- [ ] GET, PUT and DELETE /api/v1/forums/{slug}/draft save, read back and delete a member's title and source per user and per forum, answering 204 when there is none, and a PUT without title clears it (TestForumDraftRoundTrip, TestForumDraftIsPerUserAndForum, TestForumDraftPutReplacesTheTitle)
- [ ] Saving needs create_thread (forbidden otherwise), an archived forum is conflict, a hidden forum is not-found, anonymous is unauthenticated, and a title over MaxTitle or with a control character is validation-failed
- [ ] docs/07 Drafts and its resources table describe the routes, its Form posts for viewer state table has a row for each, and TestEveryViewerStateRouteHasAFormPost lists them
- [ ] dto.ForumDraft has a golden fixture and an entry in the client contract

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- **The title is saved.** Maintainer decision of 2026-09-28, recorded in this ticket's note and in docs/04 `drafts`: "a new-thread draft saves its title as well as its body ... a draft restored on another device should come back whole. The `title` column is added by that ticket's migration, and a reply draft leaves it null. That ticket also adds the route."
- A new-thread draft is keyed by forum: `thread_id` null, `forum_id` set, one per user per forum through the `drafts_new_thread` partial unique index (`internal/store/db/migrations/0005_viewer_state.sql:80`, TKT-01M29BYYHQ).
- It follows the reply draft's rules (docs/07 "Drafts"): a member session, the forum readable, 204 when there is none, `no-store`, and `PUT`/`DELETE` charged to the write limiter. Saving needs `create_thread`, as this ticket's description says, because a new-thread draft is a thread being written.

Settled here as engineering choices, to be written into docs/07 "Drafts":
- **Route:** `GET/PUT/DELETE /api/v1/forums/{slug}/draft`. It sits beside the create route `POST /api/v1/forums/{slug}/threads` and names the key the row has. The alternative `/api/v1/drafts/forum/{slug}` lost because it mixes two key spaces under one prefix, and `/drafts/{thread_id}` would have to tell a thread id from a slug.
- **Saving** goes through the same forum check as creating a thread, `forumForThread` (`internal/service/threads/create.go:214-228`): read access, `create_thread`, and an archived forum answers `conflict`. When TKT-01M3N24RHD gives admins their exemption in archived forums, drafts follow it with no change here. `GET` and `DELETE` need only read access, as for a reply draft.
- **Body:** `PUT` takes `{title?, source}`. `source` is required but may be empty, as for a reply. `title` is optional, and when absent the stored title is null: `PUT` replaces the whole draft. The title is checked with `checkTitle`'s rules (`create.go`) except two: it may be empty, and it is stored as typed, not trimmed, because it is unfinished. It is capped at `MaxTitle` and refused with control characters, both as `validation-failed`.
- **DTO:** a new `dto.ForumDraft{forum, title, source, updated_at}`, where `forum` is the slug and `title` is omitted when null. It is not a `title` field on `dto.Draft`: a reply draft never carries a title, and one type per key keeps both goldens honest.
- **Blob GC** keeps reading `drafts.source` only (`DeleteCollectableBlob`, `internal/store/db/queries/moderation.sql:92-98`). A title is plain text and never holds an attachment reference.

### Approach
1. **Migration** `internal/store/db/migrations/0011_draft_title.sql`: `ALTER TABLE drafts ADD COLUMN title TEXT CHECK (title IS NULL OR thread_id IS NULL);`. Regenerate `dbq` with `go generate ./internal/store/db` so `dbq.Draft` gains `Title sql.NullString`. If another branch lands 0011 first, this one renumbers on rebase (`migrate.go:28-29`).
2. **Queries** in `internal/store/db/queries/viewer_state.sql`, beside `GetThreadDraft`:
   - `GetForumDraft :one` (`WHERE user_id = ? AND forum_id = ? AND thread_id IS NULL`);
   - `UpsertForumDraft :one` (`ON CONFLICT (user_id, forum_id) WHERE thread_id IS NULL DO UPDATE SET title, source, updated_at ... RETURNING *`);
   - `DeleteForumDraft :execrows`.
   Drop the comment at `:180-181` that says new-thread drafts have no endpoint.
3. **Service** in `internal/service/threads/viewerstate.go`: `ForumDraft(ctx, v, slug) (dto.ForumDraft, bool, error)`, `SaveForumDraft(ctx, v, slug, ForumDraftRequest{Title *string; Source string})` and `DeleteForumDraft(ctx, v, slug) error`. Each starts with `MayKeepState(v)`. Save uses `forumForThread`. Get and delete use `readForum` (`list.go:211-235`), so a hidden forum is `not-found`.
4. **Handlers** in `internal/web/api/viewerstate.go`: `getForumDraft`, `putForumDraft` and `deleteForumDraft`, registered in `api.go` beside `/drafts/{thread_id}` (`:62-64`). They follow the reply-draft handlers: `decodeStrict` into `forumDraftRequest{Title *string "title"; Source *string "source"}`, a nil source is `bad-request`, the answers are 200, 204 and 204, and every one is `no-store`.
5. **DTO** in `internal/dto/dto.go`: `ForumDraft`, with a golden `internal/dto/testdata/golden/forum-draft.member.json` built in `fixtures_test.go`. Add it to the client contract `web/client/src/api/dto-contract.ts` and the type to `web/client/src/api/dto.ts`, so the contract test covers it. No screen uses it yet. Adding a type to `dto.ts` does not change the bundle. If it does, run `go generate ./internal/web/spa` and keep the dist committed.
6. **docs/07**:
   - "Drafts": replace the "has no route yet" bullet with the three routes and the rules above.
   - The resources table row (`:93`): add the routes.
   - "Form posts for viewer state": add three rows, whose HTML-client routes mirror the reply draft's:
     - `GET /api/v1/forums/{slug}/draft`: none (the new-thread page fills its form from the draft), `ForumDraft`;
     - `PUT`: `POST /f/{slug}/draft` (the new-thread form's "Save draft" button), fields `title`, `source`, `SaveForumDraft`;
     - `DELETE`: `POST /f/{slug}/draft/delete`, `DeleteForumDraft`.
7. **docs/04** `drafts` already shows the column. Change "The API serves reply drafts only (07 "Drafts")" to name both routes.

### Tests
- `TestDraftUniqueness` (`internal/store/db/viewer_state_test.go:67`): add a reply draft with a title (refused by the CHECK), and a new-thread draft with a title and one without.
- Service and API, in `internal/web/api/viewerstate_test.go`:
  - `TestForumDraftRoundTrip`: PUT, then GET returns the same title and source, then DELETE, then GET answers 204;
  - `TestForumDraftIsPerUserAndForum`: alice's draft in `general` is not carol's, and not alice's in another forum;
  - `TestForumDraftSaveNeedsCreateThread`: `forbidden` without `create_thread`;
  - `TestForumDraftInAnArchivedForumIsConflict`;
  - `TestForumDraftInAHiddenForumIsNotFound`;
  - `TestForumDraftTitleRules`: over `MaxTitle` or with a control character is `validation-failed`, empty is accepted, and whitespace is kept;
  - `TestForumDraftNeedsAMemberSession`: anonymous is `unauthenticated`, and pre-user is refused as for reply drafts;
  - `TestForumDraftPutReplacesTheTitle`: a PUT without `title` clears it.
- `TestEveryViewerStateRouteHasAFormPost` (`viewerstate_test.go:363-412`): add the three routes to `served`, and extend the `strings.NewReplacer` at `:374` with `{slug}`.
- DTO golden `forum-draft.member.json` and the client contract test.

### Out of scope
- A client screen. The new-thread route `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:113`), and no ticket builds it. Filed as TKT-01M3NS16VH (Build the new-thread screen with draft autosave), which depends on this ticket.
- Reply-draft autosave in the composer. The server side landed with TKT-01M29CA9CE, but nothing in `web/client/src` calls `/api/v1/drafts`. Filed as TKT-01M3NS1931 (Autosave reply drafts from the composer).
- The admin exemption in archived forums. That is TKT-01M3N24RHD.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: a new-thread draft saves the title. Add a nullable title column to drafts (null for reply drafts) in this ticket's migration. docs/04 'drafts' now shows the column and says this ticket adds it. Moved into phase-2 during grooming. Carry this into the plan's Settled section.
