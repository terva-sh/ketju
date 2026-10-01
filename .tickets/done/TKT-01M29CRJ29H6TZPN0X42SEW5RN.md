---
schema: 3
id: TKT-01M29CRJ29H6TZPN0X42SEW5RN
title: Implement watches and mutes
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
blocks_on: none
references:
  - ref: design:data-model-viewer
    path: docs/04-data-model.md
  - ref: pr:294
    path: null
  - ref: pr:305
    path: null
  - ref: pr:308
    path: null
  - ref: pr:315
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T14:00:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-crj29
  name: ""
extensions: {}
---

## Description

Watch or mute a thread or a whole forum. A watch drives the `thread_you_watch` notification and the `/watching` list; a mute suppresses both notification and unread badge.

Precedence needs deciding here and writing down: a muted thread inside a watched forum is muted, because the more specific setting wins. Getting this backwards makes mute useless exactly when somebody needs it.

## Acceptance criteria

- [x] Thread-level settings override forum-level settings, tested in both directions in Go and in SQL (TestEffectiveWatch, TestEffectiveWatchSQLAgrees)
- [x] A muted thread or forum gives no unread badge in /me, the forum index or the thread-list row, and a watched thread in a muted forum still counts (TestMutedThreadHasNoUnreadBadge)
- [x] Replying to or starting a thread leaves its watch level unchanged, as docs/06 records (TestReplyDoesNotWatch)
- [x] The thread viewer block carries watch and forum_watch and the forum thread list carries watch, in the goldens; neither enters the response cache (TestWatchStaysOutOfTheResponseCache)
- [x] POST /threads/{id}/watch and POST /forums/{slug}/watch set, change and clear a level, refuse as the thread page does, and are uncharged for agents (TestWatchRoutes, TestAgentWatchIsUncharged)
- [x] docs/07 documents both routes and their form-post rows (TestEveryViewerStateRouteHasAFormPost); TestMeCountsScansNoTable and TestMeBudget still pass

## Implementation plan

### Settled in grooming

- **Precedence.** A thread's effective level is its own `watches` row, then the row for the forum it is in now, then none. A forum row covers that forum's own threads, not its children's (docs/04 `watches`, run decision 2026-10-01).
- **What mute removes.** Mute removes every notification kind and every unread count: `/me`, the forum index's `unread`, the thread list row's `unread`, and the cross-forum lists. Watch adds `thread_you_watch` (docs/04 `watches`, run decision 2026-10-01).
- **Replying or starting a thread does not change its watch level** (docs/06 "Thread list views", run decision 2026-10-01). Criterion 3 is reworded to say so.
- **The route** is `POST /threads/{id}/watch` `{level}`, with the HTML form post `POST /t/{id}/watch` (docs/07 resources table and "Form posts for viewer state", where the row says this ticket builds it).
- **No migration.** `watches` exists (migration 0005), with `watches_thread` and `watches_forum` unique per user. The generator's lookup indexes by thread and by forum belong to TKT-01M29CRJ2T, which is the only reader that needs them.
- **The viewer block is outside the response cache** (docs/07 "Thread response shape": everything above the viewer block is cached, and the ETag hashes the page body as written). So a watch change needs no new cache key.
- **The client control** is TKT-01M3V6Y87D (Add the watch and mute control to thread and forum screens), split out in grooming because client PRs queue for the committed dist.

### Approach

Revised on claim (2026-10-01) against main at a6a37a9a. The groomed plan stands. Four points are settled differently, each marked **Changed on claim**.

1. **Domain.** Add `WatchNone = ""` beside `WatchWatch` and `WatchMute` in `internal/domain/viewer_state.go`. Add `ValidWatchLevel(string) bool` and `EffectiveWatch(thread, forum string) string`, which returns the thread level when it is set and the forum level otherwise. This is the one Go statement of the precedence. SQL states it as `coalesce(tw.level, fw.level, '')`, and a test holds the two to the same table.
2. **Queries** in `internal/store/db/queries/viewer_state.sql`, regenerated with sqlc:
   - `SetThreadWatch` upserts on `(user_id, thread_id) WHERE thread_id IS NOT NULL` and sets `level` and `updated_at` from `excluded` only.
   - `ClearThreadWatch` deletes the row.
   - `SetForumWatch` and `ClearForumWatch` are the same for `(user_id, forum_id)`.
   - `ForumWatchLevel` sits beside the existing `ThreadWatchLevel`.
3. **Service** in `internal/service/threads/viewerstate.go`:
   - `SetThreadWatch(ctx, v, id, level)` runs `MayKeepState(v)`, then `s.access(ctx, v, id)`, so a thread hidden from the viewer is `not-found` as on its page. Then it validates the level, writes, and answers `dto.ThreadWatch{watch, forum_watch}`, read in the write's transaction.
   - `SetForumWatch(ctx, v, slug, level)` admits the viewer with `readForum`, the thread list's access, and answers `dto.ForumWatch{watch}`.
   - `""` deletes the row. **Changed on claim:** any other unknown value is `validation-failed` with `errors: [{field: "level", code: "not_an_option"}]`, and a non-string `level` (null included) is `wrong_type`, as `PATCH /settings` answers a theme. docs/07's form-code table is a closed list that a test holds the code list to, and `not_an_option` already means this. A new `invalid` code would widen the list for no gain. A body without `level` is `bad-request`.
   - Archived threads and forums are allowed, because watching only needs read.
4. **Viewer block and list.**
   - `threads.View` reads `ForumWatchLevel` in the same read as `ThreadWatchLevel`. `dto.ThreadViewer` gains `forum_watch`. `watch` stays the thread's own setting, where `""` means it follows the forum.
   - `dto.ThreadList` gains `watch`, the viewer's level on that forum, `""` for an anonymous viewer and on a cross-forum list.
   - New DTOs `ThreadWatch` and `ForumWatch` get goldens. **Changed on claim:** `web/client/src/api/dto.ts` and `dto-contract.ts` take the matching type-only change, because the contract test requires every golden to be imported and every field typed. Types are erased, so `go generate ./internal/web/spa` leaves the dist byte-identical, and the PR says so.
   - Regenerate the goldens in `internal/dto/testdata/golden` and `internal/web/api/testdata/golden`.
5. **Mute in the counts.**
   - **Changed on claim:** the forum-level row is read by `LEFT JOIN watches AS fw ON fw.user_id = ? AND fw.forum_id = ...`, a search on `watches_forum`, rather than a new CTE. That keeps the plan tests' scan allowlists as they are.
   - `MeCounts` and `ForumUnread`: a forum's count is its untouched unread threads (the present `active` less the touched ones), which is 0 when the forum's row is `mute`, plus its touched threads that are unread by the effective level. So a thread-level watch in a muted forum counts, and a thread-level mute anywhere does not.
   - `participated` and `watching` follow the same effective level. `watching` stays a thread-level `watch` (docs/06).
   - The shape stays one statement that starts from the viewer's own rows, so `TestMeCountsScansNoTable` and `TestMeBudget` hold unchanged.
   - `views.ThreadList`'s `rowColumns` gains the effective level. `threads.List` reports `unread: 0` for a muted row and keeps `last_read_ordinal`. A global pin from another forum takes its own forum's row.
6. **Routes** in `internal/web/api/api.go`: `{post, "/api/v1/threads/{id}/watch"}` and `{post, "/api/v1/forums/{slug}/watch"}`, both `no-store`, both JSON-only (415 otherwise, from the CSRF stage). Both paths join `unchargedPaths` in `agents.go`. Anonymous is `unauthenticated` and a pre-user is `forbidden` before the body is read. The pre-user walk and `TestEveryRouteIsInTheTable` read `routes()`, so the gate tables take both routes from there. No admin route is added and no audit action is written: a watch is the viewer's own state, as a read position is.
7. **docs/07.**
   - Resources table rows for both routes, each with its rules and refusals.
   - `forum_watch` beside `watch` under "What the thread endpoint does".
   - `watch` on the thread list, and a muted row's `unread` is 0.
   - The existing form-post row becomes `SetThreadWatch`. A new row is added for `POST /api/v1/forums/{slug}/watch`, whose form post is `POST /f/{slug}/watch` and which calls `SetForumWatch`.
   - Both routes join the served list in `TestEveryViewerStateRouteHasAFormPost`.
   - GET /forums's "mutes not counted" becomes "a muted thread or forum not counted (04 `watches`)".
8. **Losing access** leaves the row in place. Nothing is served from it, because the generator and the lists check access when they act, and both routes answer `not-found` for a thread or forum the viewer can no longer see.

### Tests

- `TestEffectiveWatch` (domain): the 9 combinations of thread and forum level. `TestEffectiveWatchSQLAgrees` (views): the same table through the thread list's `coalesce`, with real `watches` rows.
- `TestWatchRoutes` (api): set, change and clear on a thread and on a forum, and each answer's shape. Refusals: anonymous, pre-user, inactive, a hidden thread or forum `not-found`, a members-only forum to an anonymous viewer `unauthenticated`, an unknown level `not_an_option`, a non-string `wrong_type`, a missing level `bad-request`, form encoding 415. A refusal writes nothing.
- `TestMutedThreadHasNoUnreadBadge` (api): one seeded world, checked against `/me` `unread`, `/forums` `unread` and the thread-list row's `unread`, for a thread mute, a forum mute, a thread watch in a muted forum (counts) and a thread mute in a watched forum (does not). A forum mute does not reach a child forum.
- `TestThreadRuleMatchesPostRule` (users) gains random forum-level rows, with `domain.EffectiveWatch` as the reference.
- `TestMeCountsScansNoTable`, `TestForumIndexQueriesScanNoTable`, `TestPlansSeek` and `TestMeBudget` are unchanged and pass.
- `TestReplyDoesNotWatch`, `TestWatchStaysOutOfTheResponseCache` and `TestAgentWatchIsUncharged` (api), and `TestEveryViewerStateRouteHasAFormPost` with both routes.

### Out of scope

- The client control: TKT-01M3V6Y87D.
- Generating `thread_you_watch` and suppressing notifications for mutes: TKT-01M29CRJ2T.
- The `/watching` list and its use of the precedence: TKT-01M29CTY7K.
- The HTML client's form posts: Phase 5. This ticket documents their rows only.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T07:54:09Z

run decision: pending maintainer review. Three decisions bind this ticket, recorded in docs/04 'watches' and docs/06 'Thread list views' on 2026-10-01. (1) Precedence: the thread's own row wins, then the forum's row, and a forum row covers only that forum's own threads, not its children's. (2) Mute removes every notification kind and every unread count: /me, the forum index, the thread-list row and the cross-forum lists. (3) Neither replying nor starting a thread changes its watch level, so criterion 3 now says that. The client control was split out to TKT-01M3V6Y87D (Add the watch and mute control to thread and forum screens).

**agent:claude-code/phase3-crj29** at 2026-10-01T12:42:00Z

Built against main at a6a37a9a. What the next ticket needs, and what was settled on claim (each is in the revised plan, marked "Changed on claim"):

- The rule lives in `domain.EffectiveWatch(thread, forum)`. SQL states it as `coalesce(tw.level, fw.level, '')`, with the forum row found by `watches_forum` on the thread's own forum. `views.ThreadRow.Watch` carries the effective level, so TKT-01M29CTY7K's cross-forum lists can reuse `rowColumns` and get mute for free. TKT-01M29CRJ2T can call `EffectiveWatch` or copy the coalesce, and `TestEffectiveWatchSQLAgrees` shows how to hold a new query to it.
- `MeCounts` and `ForumUnread` now count a forum as its untouched unread threads, which is 0 when the forum's row is `mute`, plus its touched threads that are unread by the effective level. The plan tests' scan allowlists are unchanged, because the forum row is a `LEFT JOIN watches AS fw` search rather than a new CTE.
- Field errors: an unknown level is `not_an_option` and a non-string level is `wrong_type`, rather than the groomed `invalid`. docs/07's form-code table is closed and held to `forms.Codes` by a test, and `not_an_option` already means a value outside a closed set, as `PATCH /settings` uses it for a theme. A missing `level` is `bad-request`.
- The body is parsed before access is checked, as `PUT /read` and `PUT /drafts` do, so a malformed body answers the same for a thread that exists and one that does not. A well-formed body for a hidden thread or forum is `not-found` whatever level it names (TestWatchRoutes), so neither route says that a hidden thread or forum exists.
- Client types: `ThreadViewer.forum_watch`, `ThreadWatch` and `ForumWatch` are required in `web/client/src/api/dto.ts`. `ThreadList.watch` is optional there (`watch?: string`), because `src/keyboard/shortcuts.test.tsx` builds `ThreadList` literals and this ticket may change only dto.ts and dto-contract.ts on the client side. The server always sends it. TKT-01M3V6Y87D (Add the watch and mute control to thread and forum screens) should make it required when it adds the control and those fixtures gain the field. `go generate ./internal/web/spa` leaves the dist byte-identical.
- No migration, no audit action, no admin route. Both routes come from `routes()`, so the pre-user walk and `TestEveryRouteIsInTheTable` cover them unchanged.

Mutations tried, each caught:
- MeCounts ignoring the forum row: TestThreadRuleMatchesPostRule and TestMutedThreadHasNoUnreadBadge fail.
- ForumUnread ignoring a forum mute for untouched threads: both fail.
- The list's coalesce with forum before thread: TestEffectiveWatchSQLAgrees and TestMutedThreadHasNoUnreadBadge fail.
- `threads.List` not zeroing a muted row: TestMutedThreadHasNoUnreadBadge fails.
- Both watch routes skipping their access check: TestWatchRoutes fails on the hidden thread and forum, and on the refusal writing a row.
- `EffectiveWatch` with forum before thread: TestEffectiveWatch, TestThreadRuleMatchesPostRule and TestEffectiveWatchSQLAgrees fail.

**agent:claude-code/phase3-crj29** at 2026-10-01T13:13:34Z

Review record. PR #308 (replaces #305) head 27029771907daab0b2f5d9b0f0baa7abf6ad3279, base 2e937eb728b7ca438b01f1a3d3e0d7b849688976: Terva run 699f0b83-b005-4b0d-9b3b-449a9b4a6786 (Actions run 2044), clean, coverage complete, no findings. Its caveat that the schema and unchanged callers were not supplied is the reviewer's standard limitation, not an unjudged file. The watches table (migration 0005) and its partial unique indexes are exercised by TestWatchRoutes's upserts and clears, and by TestEffectiveWatchSQLAgrees. #305's review was also clean: head 144c84ed52af46dd91210b90b143940542d3ddaa, base 54580e5c2b3a31b2b4b41fe4fba13e9a536f6145, run 00e212c2-cb35-4f2b-afde-e98384013e70 (Actions run 2029). #305 was closed when main conflicted in internal/service/threads/view.go.

**agent:claude-code/phase3-crj29** at 2026-10-01T14:00:13Z

Review record for PR #315, which replaces #308 and #305: head a5bf82177dbab4367c6bfa7bdc18698319645567, base 4e2ef49d380063d4e1527fc8b9590794bbcb2c18, Terva run 5298f40a-fb5c-4a55-84a8-c7ee12e727e3 (Actions run 2090). Clean, coverage complete, no findings. Its remark that it could not judge the unchanged database schema or callers outside the diff concerns files the diff does not touch, so it is not a truncated review. Every changed file was judged (coverage.complete is true). The watches schema (migration 0005) is exercised by TestWatchRoutes and TestEffectiveWatchSQLAgrees. #308 was closed when the author lens (#310) conflicted in internal/service/threads/view.go. Its CI passed on head bad6c1b4 (run 2057).

## Summary

Watches and mutes are built. POST /threads/{id}/watch and POST /forums/{slug}/watch set, change and clear the viewer's level, answer ThreadWatch and ForumWatch, are not-found for a thread or forum hidden from the viewer whatever the body says, and are not charged to an agent's bucket. A thread's effective level is its own row, then its forum's, then none: domain.EffectiveWatch in Go, a coalesce in MeCounts, ForumUnread and the thread list's rowColumns, held together by TestEffectiveWatch, TestEffectiveWatchSQLAgrees and TestThreadRuleMatchesPostRule. Mute removes the thread from /me, the forum index's unread and the thread-list row's badge. The viewer block gains forum_watch and the thread list gains watch, both outside the response cache. docs/07 documents both routes and both form-post rows. Every criterion is ticked. No migration and no audit action. The client type ThreadList.watch is optional until TKT-01M3V6Y87D adds the control.
