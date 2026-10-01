---
schema: 3
id: TKT-01M29CRJ29H6TZPN0X42SEW5RN
title: Implement watches and mutes
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T07:54:09Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
extensions: {}
---

## Description

Watch or mute a thread or a whole forum. A watch drives the `thread_you_watch` notification and the `/watching` list; a mute suppresses both notification and unread badge.

Precedence needs deciding here and writing down: a muted thread inside a watched forum is muted, because the more specific setting wins. Getting this backwards makes mute useless exactly when somebody needs it.

## Acceptance criteria

- [ ] Thread-level settings override forum-level settings, tested in both directions in Go and in SQL (TestEffectiveWatch, TestEffectiveWatchSQLAgrees)
- [ ] A muted thread or forum gives no unread badge in /me, the forum index or the thread-list row, and a watched thread in a muted forum still counts (TestMutedThreadHasNoUnreadBadge)
- [ ] Replying to or starting a thread leaves its watch level unchanged, as docs/06 records (TestReplyDoesNotWatch)
- [ ] The thread viewer block carries watch and forum_watch and the forum thread list carries watch, in the goldens; neither enters the response cache (TestWatchStaysOutOfTheResponseCache)
- [ ] POST /threads/{id}/watch and POST /forums/{slug}/watch set, change and clear a level, refuse as the thread page does, and are uncharged for agents (TestWatchRoutes, TestAgentWatchIsUncharged)
- [ ] docs/07 documents both routes and their form-post rows (TestEveryViewerStateRouteHasAFormPost); TestMeCountsScansNoTable and TestMeBudget still pass

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

1. **Domain.** Add `WatchNone = ""` beside `WatchWatch` and `WatchMute` in `internal/domain/viewer_state.go`. Add `ValidWatchLevel(string) bool` and `EffectiveWatch(thread, forum string) string`, which returns the thread level when it is set and the forum level otherwise. This is the one Go statement of the precedence. SQL states it as `coalesce(tw.level, fw.level, '')`, and a test holds the two to the same table.
2. **Queries** in `internal/store/db/queries/viewer_state.sql`, regenerated with sqlc:
   - `SetThreadWatch` upserts on `(user_id, thread_id) WHERE thread_id IS NOT NULL` and sets `level` and `updated_at` from `excluded` only (sqlc v1.31.1 does not bind a parameter inside `DO UPDATE` on SQLite).
   - `ClearThreadWatch` deletes the row.
   - `SetForumWatch` and `ClearForumWatch` are the same for `(user_id, forum_id)`.
   - `ForumWatchLevel` sits beside the existing `ThreadWatchLevel`.
3. **Service** in `internal/service/threads/viewerstate.go`:
   - `SetThreadWatch(ctx, v, id, level)` runs `MayKeepState(v)`, then `s.access(ctx, v, id)`, so a thread hidden from the viewer is `not-found` as on its page. Then it validates the level, writes, and answers `dto.ThreadWatch{watch, forum_watch}`, read in the write's transaction.
   - `SetForumWatch(ctx, v, slug, level)` admits the viewer with `readForum`, the thread list's access, and answers `dto.ForumWatch{watch}`.
   - `""` deletes the row. Any other unknown value is `validation-failed` with `errors: [{field: "level", code: "invalid"}]`. A body without `level` is `bad-request`.
   - Archived threads and forums are allowed, because watching only needs read.
4. **Viewer block and list.**
   - `threads.View` reads `ForumWatchLevel` in the same read as `ThreadWatchLevel` (`view.go` around the existing `watch` read). `dto.ThreadViewer` gains `forum_watch`. `watch` stays the thread's own setting, where `""` means it follows the forum.
   - `dto.ThreadList` gains `watch`, the viewer's level on that forum, `""` for an anonymous viewer, for the forum header's control.
   - Regenerate the goldens in `internal/dto/testdata/golden` and `internal/web/api/testdata/golden` (thread.*, the thread list).
5. **Mute in the counts.**
   - `MeCounts` and `ForumUnread` (`viewer_state.sql`) gain an `fw` CTE: the viewer's forum-level rows, which are few. In a forum whose row is `mute`, a forum's unread count is only its unread threads with a thread-level `watch`. Otherwise it is the present rule with the thread's effective level, so a thread-level watch in a muted forum counts and a thread-level mute anywhere does not.
   - `participated` and `watching` follow the same effective level.
   - The shape stays one statement that starts from the viewer's own rows, so `TestMeCountsScansNoTable` and `TestMeBudget` hold unchanged.
   - `views.ThreadList`'s `rowColumns` (`internal/store/db/views/threadlist.go`) gains the effective level. `threads.List` reports `unread: 0` for a muted row and keeps `last_read_ordinal`.
6. **Routes** in `internal/web/api/api.go`: `{post, "/api/v1/threads/{id}/watch"}` and `{post, "/api/v1/forums/{slug}/watch"}`, both `no-store`, both JSON-only (415 otherwise). Both paths join `unchargedPaths` in `agents.go`, because they record viewer state as the read position does. Anonymous is `unauthenticated` and a pre-user is `forbidden` before the body is read.
7. **docs/07.**
   - Resources table rows for both routes, each with its rules and refusals.
   - `forum_watch` beside `watch` under "What the thread endpoint does".
   - `watch` on the thread list.
   - The existing form-post row becomes `SetThreadWatch`. A new row is added for `POST /api/v1/forums/{slug}/watch`, whose form post is `POST /f/{slug}/watch` and which calls `SetForumWatch`.
   - Both routes join the served list in `TestEveryViewerStateRouteHasAFormPost`.
   - GET /forums's "mutes not counted" becomes "a muted thread or forum not counted (04 `watches`)".
8. **Losing access** leaves the row in place. Nothing is served from it, because the generator and the lists check access when they act, and both routes answer `not-found` for a thread or forum the viewer can no longer see.

### Tests

- `TestEffectiveWatch` (domain): a table of the 9 combinations of thread and forum level, in both directions. Thread mute in a watched forum is mute, thread watch in a muted forum is watch, and an unset thread follows the forum. `TestEffectiveWatchSQLAgrees` runs the same table through the `coalesce` the queries use.
- `TestWatchRoutes` (api):
  - set, change and clear on a thread and on a forum;
  - each answer's shape;
  - refusals: anonymous `unauthenticated`, pre-user `forbidden`, a hidden thread or forum `not-found`, a restricted forum without a grant `not-found` or `forbidden` as the thread page answers, an unknown level `validation-failed`, a missing level `bad-request`, form encoding 415.
- `TestMutedThreadHasNoUnreadBadge` (api): one seeded world, checked against `/me` `unread`, `/forums` `unread` and the thread-list row's `unread`:
  - a thread-level mute;
  - a forum-level mute;
  - a thread-level watch inside a muted forum, which still counts;
  - a thread-level mute inside a watched forum, which does not.
- `TestForumTreeCounts` and the MeCounts and ForumUnread hold-together test gain the mute rows. `TestUnreadMatchesUnreadCount` still holds for unmuted rows.
- `TestMeCountsScansNoTable` and `TestMeBudget` are unchanged and still pass. These cover the session and statement-budget invariants for `/me`.
- `TestReplyDoesNotWatch` (api): creating a thread and replying to one leave the viewer's `watch` `""`.
- `TestWatchStaysOutOfTheResponseCache` (api): two members with different levels read the same page. The cached post bodies are one entry (a `respcache` hit for the second), and only the viewer blocks and ETags differ. This is the response-cache key invariant.
- `TestAgentWatchIsUncharged` (api, beside the agent bucket tests): an agent at an empty bucket can still set a watch.
- `TestEveryViewerStateRouteHasAFormPost` with the two routes added.

### Out of scope

- The client control: TKT-01M3V6Y87D.
- Generating `thread_you_watch` and suppressing notifications for mutes: TKT-01M29CRJ2T.
- The `/watching` list and its use of the precedence: TKT-01M29CTY7K.
- The HTML client's form posts: Phase 5. This ticket documents their rows only.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T07:54:09Z

run decision: pending maintainer review. Three decisions bind this ticket, recorded in docs/04 'watches' and docs/06 'Thread list views' on 2026-10-01. (1) Precedence: the thread's own row wins, then the forum's row, and a forum row covers only that forum's own threads, not its children's. (2) Mute removes every notification kind and every unread count: /me, the forum index, the thread-list row and the cross-forum lists. (3) Neither replying nor starting a thread changes its watch level, so criterion 3 now says that. The client control was split out to TKT-01M3V6Y87D (Add the watch and mute control to thread and forum screens).
