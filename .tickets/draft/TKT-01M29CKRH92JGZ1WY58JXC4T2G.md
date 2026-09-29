---
schema: 3
id: TKT-01M29CKRH92JGZ1WY58JXC4T2G
title: Implement thread rename and move
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29CKRGSF2FPY73MNNFPQ1FV
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T05:07:51Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Rename with a new slug, and move to another forum, both as thread log records. An author may rename their own thread within a window.

A rename changes the slug, and every URL ever shared carries the old one. The slug is advisory and a stale one redirects, which is what makes renaming safe enough to be a routine moderator action.

Retag was split out in Phase 2 grooming on 2026-09-29. Thread tags are Phase 4 (docs/09; the maintainer's 2026-09-28 deferral on TKT-01M3B12ZZ4), and TKT-01M29CYSCP (Implement thread tags) owns changing a thread's tags with a full-replacement `ThreadTagged` record.

## Acceptance criteria

- [ ] After a rename, /t/{id}/{old-slug}?page=2&lens=X#post-N and /t/{id}/{old-slug}/N/history are replaced by the same URLs under the new slug, keeping page, lens, ordinal and hash
- [ ] A move needs moderate in both forums: forbidden without it in the destination, not-found for a destination hidden from the mover, conflict for an archived destination unless the mover is an admin; a refusal appends nothing
- [ ] An author may rename their own thread until threads.rename_window_seconds after its creation, then gets rename-window-closed with the log unchanged; a moderator may rename at any time
- [ ] After ketju reindex a thread's title, slug and forum equal its last rename and move records, and forum_thread_counts follows the move
- [ ] A moderator's rename and every move write one audit_log row with the actor and the from and to values
- [ ] A page cached before a rename or a move is not served after it (TestRenameMovesTheResponseCacheKey)
- [ ] No ThreadTagged record is written and no tag route is served: retag belongs to TKT-01M29CYSCP (phase 4)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Retag is split out.** Thread tags are Phase 4. docs/09 lists "thread tags" under Phase 4, and the maintainer deferred tags on create to Phase 4 on 2026-09-28 (note on TKT-01M3B12ZZ4). TKT-01M29CYSCP (Implement thread tags, phase-4) already owns changing tags, by moderators, as a full-replacement `ThreadTagged` record, and it already depends on this ticket. This ticket writes no `ThreadTagged` record and serves no tag route. The title and the tag criterion change to match. A retag that only moderators can use lost: it would put tags into logs before TKT-01M3M4KVNW decides what a tag may be, and a log keeps every tag it is given forever.
- **No new record type.** `ThreadRenamed` (10, `{title, slug}`) and `ThreadMoved` (11, `{forum_id}`) are in the catalogue, and the projection already applies both (`applyRenamed`, `applyMoved`). The forum count triggers already follow a `forum_id` change (migration 0009, `forum_thread_counts_update`).
- **The slug is advisory** (docs/07: "a stale slug redirects to the current one"). The client already corrects a stale slug and keeps the ordinal, `/history`, page, lens and hash (`canonicalThreadUrl` in `web/client/src/view.ts`, used by `ThreadScreen`). This ticket proves that correction against a real rename rather than rebuilding it.
- **Who may rename**: a moderator at any time, and the thread's author (`threads.author_id`, the `ThreadCreated` actor) within a window (docs/08 "Primitives"). **Who may move**: a moderator (docs/08; docs/07 "Moderation and admin").
- **A move needs `moderate` in both forums.** It is `forbidden` without `moderate` in the destination, `not-found` when the destination is hidden from the mover, and `conflict` when the destination is archived and the mover is not an admin (docs/04 archived forums; TKT-01M3N24RHD). `create_thread` in the destination lost as the test: it would let a moderator of one forum push threads into a forum they do not moderate, where they could not move them back.
- **Response cache.** Both records are appends and move the log length in `respcache.Key`. The header is drawn from the projection row, uncached, and the synchronous projection in step 2 keeps `access` reading the new forum at once.

### Approach

1. **Setting**: `threads.rename_window_seconds` in `internal/settings/settings.go`, `atLeast(0)`, with a docs/04 settings row (`TestKeysDocumented`). The default is the open decision below. The window is measured from the thread's `created_at`.
2. **Service** in `internal/service/threads/moderate.go`, through the `moderate` helper from TKT-01M29CKRGS (append, synchronous projection, audit row):
   - `Rename(ctx, v, id, title)`: title through `checkTitle`, slug from `Slug(title)`. A moderator may rename at any time. The thread's author, without `moderate`, may rename within the window, needs `reply` in the forum, and is refused in a locked thread (`conflict`), as a member's edit is. Past the window the author gets the new code `rename-window-closed` (403). Anyone else is `forbidden`. An archived thread is `conflict`. A title equal to the current one is `conflict` (no no-op records). Only a moderator's rename is audited, as `thread.renamed` with `{from, to}`. An author renaming their own thread is not a moderation action, and it is in the log either way.
   - `Move(ctx, v, id, forumSlug)`: `canModerate` on the source, then the destination's refusals in the settled order. Moving to the current forum is `conflict`. Audited as `thread.moved` with `{from_forum, to_forum}`.
3. **Problem code** `rename-window-closed`, 403, with a fixed title, in `internal/problem/problem.go` and `testdata/catalogue.json`. It sits beside TKT-01M29CKRHS's `edit-window-closed` and `delete-window-closed`. `forbidden` with extra text lost: problem bodies never carry request-derived text (docs/07 "Error catalogue"), and a client cannot switch on a title.
4. **Routes**: `PATCH /api/v1/threads/{id}` with `{title}`, for both author and moderator, because the service decides who may. A second `/admin` rename route lost: it would double one action. `POST /api/v1/admin/threads/{id}/move` with `{forum}`, the destination slug. Both answer 200 `dto.ThreadHeader` with `no-store` and are charged to the write limiter.
5. **Client**: Rename in the thread actions for a moderator, and for the author while `created_at + window` has not passed. The window reaches the client in the thread viewer block as `rename_until` (ms, or null), which is per viewer and never cached. Move for a moderator, with a forum picker (`screens/ForumPicker.tsx`). After either, navigate to the header's new canonical URL. Regenerate `dist/`.
6. **Docs**: docs/07 gets the PATCH route and the move route, and the admin list says rename lives on the thread resource. docs/04 gets the setting.

### Tests

- `TestRenameWindow`: with a fixed clock, the author renames inside the window. One second past it, the author gets `rename-window-closed` and the log is unchanged. A moderator renames past the window.
- `TestRenameRefusals`: a member who is not the author is `forbidden`; a locked thread is `conflict` to the author; an archived thread is `conflict`; the same title is `conflict`. Each appends nothing.
- `TestMoveNeedsModerateInBothForums`: `forbidden` without `moderate` in the destination; `not-found` for a destination hidden from the mover; `conflict` for an archived destination to a moderator; success for an admin into an archived one. After a move into a restricted forum, a member without read there gets `not-found` on the next request, which the synchronous projection makes true at once.
- `TestRenameAndMoveSurviveReindex`: a log with two renames and a move reindexes to the last title, slug and forum, and `forum_thread_counts` moves the thread's counts.
- Audit: a moderator rename and a move each write one row with the actor and from/to. An author rename writes none.
- **Response cache invariant**, `TestRenameMovesTheResponseCacheKey`: a cached page is a miss after a rename, and the new header carries the new title and slug.
- **Stale slug**, client (`view.test.tsx` and `ThreadPage.test.tsx`): after the served header's slug changes, `/t/{id}/{old}?page=2&lens=author` is replaced by `/t/{id}/{new}?page=2&lens=author` with the hash kept, and `/t/{id}/{old}/7/history` by `/t/{id}/{new}/7/history`.

### Open decision

The default for `threads.rename_window_seconds`. docs/08 says an author may rename within a window but gives no length.
- 24 hours, the post edit window. Recommended: the title is the opening post's headline, and one window for both is one thing to explain.
- 15 minutes, the delete window.
- 0, meaning moderators only until an admin opens the window.

### Out of scope

- Retag and the tag vocabulary: TKT-01M29CYSCP and TKT-01M3M4KVNW (phase 4).
- Lock, pin and the moderation helper: TKT-01M29CKRGS.
- Server-side slug redirects for the non-JS client: Phase 5, docs/07 "Path to a non-JS client".

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:07:51Z

Open decision for the maintainer, from Phase 2 grooming on 2026-09-29: the default for threads.rename_window_seconds. docs/08 gives the author a rename window but no length. Options: 24 hours, matching the post edit window (recommended, since the title is the opening post's headline); 15 minutes, matching the delete window; or 0, meaning moderators only until an admin opens it. The plan's Open decision section carries the same. Retag was split out to TKT-01M29CYSCP (phase 4) in the same grooming.
