---
schema: 3
id: TKT-01M29CKRH92JGZ1WY58JXC4T2G
title: Implement thread rename and move
type: task
status: done
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
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
  - ref: pr:190
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-30T02:12:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-krh9
  name: ""
extensions: {}
---

## Description

Rename with a new slug, and move to another forum, both as thread log records. An author may rename their own thread within a window.

A rename changes the slug, and every URL ever shared carries the old one. The slug is advisory and a stale one redirects, which is what makes renaming safe enough to be a routine moderator action.

Retag was split out in Phase 2 grooming on 2026-09-29. Thread tags are Phase 4 (docs/09; the maintainer's 2026-09-28 deferral on TKT-01M3B12ZZ4), and TKT-01M29CYSCP (Implement thread tags) owns changing a thread's tags with a full-replacement `ThreadTagged` record.

## Acceptance criteria

- [x] After a rename, /t/{id}/{old-slug}?page=2&lens=X#post-N and /t/{id}/{old-slug}/N/history are replaced by the same URLs under the new slug, keeping page, lens, ordinal and hash
- [x] A move needs moderate in both forums: forbidden without it in the destination, not-found for a destination hidden from the mover, conflict for an archived destination unless the mover is an admin; a refusal appends nothing
- [x] An author may rename their own thread until threads.rename_window_seconds after its creation, then gets rename-window-closed with the log unchanged; a moderator may rename at any time
- [x] After ketju reindex a thread's title, slug and forum equal its last rename and move records, and forum_thread_counts follows the move
- [x] A page cached before a rename or a move is not served after it (TestRenameMovesTheResponseCacheKey)
- [x] No ThreadTagged record is written and no tag route is served: retag belongs to TKT-01M29CYSCP (phase 4)
- [x] threads.rename_window_seconds defaults to 86400, and an author's rename writes no audit row (TestAuthorRenameIsNotAudited)
- [x] A moderator's rename writes one thread.renamed audit row with details {thread, log_offset} and no title text, and every move writes one row with the actor and {from_forum, to_forum} (TestRenameAuditCarriesNoTitle)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a. Revised the same day for the title decision on TKT-01M3NS147C. Revised 2026-09-30 at claim, against main at b41b6db.

### Settled in grooming

- **Retag is split out.** Thread tags are Phase 4. docs/09 lists "thread tags" under Phase 4, and the maintainer deferred tags on create to Phase 4 on 2026-09-28 (note on TKT-01M3B12ZZ4). TKT-01M29CYSCP (Implement thread tags, phase-4) already owns changing tags, by moderators, as a full-replacement `ThreadTagged` record, and it already depends on this ticket. This ticket writes no `ThreadTagged` record and serves no tag route. The title and the tag criterion change to match. A retag that only moderators can use lost: it would put tags into logs before TKT-01M3M4KVNW decides what a tag may be, and a log keeps every tag it is given forever.
- **No new record type.** `ThreadRenamed` (10, `{title, slug}`) and `ThreadMoved` (11, `{forum_id}`) are in the catalogue, and the projection already applies both (`applyRenamed`, `applyMoved`). The forum count triggers already follow a `forum_id` change (migration 0009, `forum_thread_counts_update`).
- **The slug is advisory** (docs/07: "a stale slug redirects to the current one"). The client already corrects a stale slug and keeps the ordinal, `/history`, page, lens and hash (`canonicalThreadUrl` in `web/client/src/view.ts`, used by `ThreadScreen`). This ticket proves that correction against a real rename rather than rebuilding it.
- **The author's rename window is 24 hours.** Maintainer decision 2026-09-29, accepting the grooming recommendation. `threads.rename_window_seconds` defaults to 86400, the post edit window, because the title is the opening post's headline and one window is one thing to explain. 15 minutes (the delete window) and 0 (moderators only) lost.
- **Who may rename**: a moderator at any time, and the thread's author (`threads.author_id`, the `ThreadCreated` actor) within a window (docs/08 "Primitives"). **Who may move**: a moderator (docs/08; docs/07 "Moderation and admin").
- **A move needs `moderate` in both forums.** It is `forbidden` without `moderate` in the destination, `not-found` when the destination is hidden from the mover, and `conflict` when the destination is archived and the mover is not an admin (docs/04 archived forums; TKT-01M3N24RHD). Moving a thread out of an archived forum is a moderator's to do (maintainer decision 2026-09-29 on TKT-01M3N24RHD). `create_thread` in the destination lost as the test: it would let a moderator of one forum push threads into a forum they do not moderate, where they could not move them back.
- **Response cache.** Both records are appends and move the log length in `respcache.Key`. The header is drawn from the projection row, uncached, and the synchronous projection in step 2 keeps `access` reading the new forum at once.
- **A rename's audit row carries no title text.** This is a maintainer decision of 2026-09-29 on TKT-01M3NS147C (Decide how a thread title is hard-redacted), recorded in docs/03 "Hard redaction". A moderator's `thread.renamed` row records `{thread, log_offset}` only, never the old or new title. The log is the record of what the title was. `audit_log` is append-only, so a title copied into it could never be removed by `ketju redact` (TKT-01M29CXJZ9). `{from, to}` titles lost for that reason. A move keeps `{from_forum, to_forum}`, which are ids, not user text.

- **The archived rule, restated at claim (2026-09-30, from the run's orchestrator):** moderators may move a thread out of an archived forum. Moving a thread in is `conflict` except to an admin. Move reads the destination's `Archived` field itself and does not call `readOnlyForum`. This completes TKT-01M3N24RHD's seventh criterion.

### Approach

Revised at claim. What changed from the groomed plan, and why, is marked "Revised".

1. **Setting**: `threads.rename_window_seconds` in `internal/settings/settings.go`, `atLeast(0)`, default 86400, with a docs/04 settings row (`TestKeysDocumented`). The window is measured from the thread's `created_at` and read with `within`, as the post windows are, so a window of 0 is never open.
2. **Audit rows (revised).** The projection writer already writes the rows (`internal/events/projection/audit.go`); no second writer. Two changes there: a `thread.renamed` row's details drop `title` and `slug`, leaving the `{thread, log_offset}` that `writeAudit` adds; and a `thread.moved` row carries `{from_forum, to_forum}` instead of `{forum_id}`. `from_forum` is the thread row's `forum_id` read before the record is applied, in the same transaction, which is the forum the log put it in before the move, on a live projection and on a reindex alike. The author test for a rename stays as it is.
3. **Service** in `internal/service/threads/moderate.go`:
   - Revised: one helper, `appendDecided`, decides and appends under the thread at a log length, as the suspension does (`authorThread`): read the log length `at`, project up to it, run `access` from the row that gives, decide, and append with `AtLength: at`. A record that lands in between fails the append with `ErrLogMoved`, and the request is decided again, up to `threadAttempts`. So a move is always judged against the forum the thread is in when the record lands, and two racing moves cannot both pass on a forum one of them just left. `moderate` (lock, pin) keeps its accepted race.
   - `Rename(ctx, v, id, title)`: `MayWrite`; then under `appendDecided`: an archived thread is `conflict`. A viewer with `moderate` may rename at any time. The thread's author (`threads.author_id`) without `moderate` gets, in order, `conflict` in an archived forum (`readOnlyForum`, as for an author's edit of their own post), `conflict` in a locked thread, `forbidden` without `reply`, and `rename-window-closed` past the window. A moderator is not asked about the forum's archived flag, the lock or the window, as the criterion says ("a moderator may rename at any time"); a moderator's rename of their own thread is still unaudited, because the projection decides that from the actor. Anyone else is `forbidden`. Then `checkTitle` (`validation-failed`), then a title equal to the current one is `conflict`. The slug is `Slug(title)`.
   - `Move(ctx, v, id, forumSlug)`: `MayWrite`; the destination through `readForum` (`not-found` when missing or hidden from the mover, `forbidden` when it is not readable); then under `appendDecided`: an archived thread is `conflict`; `forbidden` without `moderate` in the source; `forbidden` without `moderate` in the destination; `conflict` when the destination's `Archived` is set and the mover is not an active admin, read from the forum row loaded on this request, never `readOnlyForum`; `conflict` when the destination is the current forum.
4. **Problem code** `rename-window-closed`, 403, with a fixed title, in `internal/problem/problem.go` and `testdata/catalogue.json`, beside `edit-window-closed`. Revised: the catalogue golden is read by `web/client/src/api/problem.test.ts`, so the client's `problem.ts` needs a message entry for it, which changes the committed dist. The orchestrator approved taking the dist after the moderator-queue lane's PR (TKT-01M29CRJ4E) merges. The client change is that one entry and the regenerated dist.
5. **Routes**: `PATCH /api/v1/threads/{id}` with `{title}`, for author and moderator, in the route table, charged to the write limiter. `POST /api/v1/admin/threads/{id}/move` with `{forum}`, the destination slug, behind the staff gate as lock and pin are. Both answer 200 `dto.ThreadHeader`, `no-store`, and bodies are decoded strictly and bounded at 4 KiB.
6. **Client (revised)**: the rename and move controls, the `rename_until` viewer field and the forum picker go to a draft, since the moderator-queue lane holds the dist. The stale-slug correction this ticket relies on is already in the client (`canonicalThreadUrl`) and tested in `web/client/src/view.test.tsx` for page, lens, ordinal, `/history` and hash; this ticket proves the server half, that after a rename the page is served under the new slug.
7. **Docs**: docs/07 gets the PATCH route in "Resources", a "Renaming and moving threads" section beside lock and pin, and the error catalogue row. docs/04 gets the setting and the audit details. docs/08's primitives line names the setting.

### Tests

- `TestRenameWindow`: with a fixed clock, the author renames inside the window. One second past it, the author gets `rename-window-closed` and the log is unchanged. A moderator renames past the window.
- `TestRenameRefusals`: a member who is not the author is `forbidden`; a locked thread is `conflict` to the author; an archived thread is `conflict`; the same title is `conflict`; a bad title is `validation-failed`. Each appends nothing.
- `TestMoveNeedsModerateInBothForums`: `forbidden` without `moderate` in the destination (a group moderator through the service); `not-found` for a destination hidden from the mover; `conflict` for an archived destination to a moderator; success for an admin into an archived one; a moderator moves a thread out of an archived forum. After a move into a restricted forum, a member without read gets `not-found` on the next request.
- `TestRenameAndMoveSurviveReindex`: a log with two renames and a move reindexes to the last title, slug and forum, and `forum_thread_counts` follows the move.
- `TestRenameAuditCarriesNoTitle`: a moderator's rename writes one `thread.renamed` row whose details are exactly `{thread, log_offset}`, and neither title appears anywhere in the row. A move writes one `thread.moved` row with the actor and `{from_forum, to_forum}`. `TestAuthorRenameIsNotAudited`: an author's rename leaves `audit_log` unchanged, including after a reindex.
- `TestRenameMovesTheResponseCacheKey`: a cached page is a miss after a rename and after a move, each page equals a fresh draw, and the new header carries the new title, slug and forum.
- Projection: `TestAuditRowsComeFromTheLog` expects the new rename and move details.

### Out of scope

- Retag and the tag vocabulary: TKT-01M29CYSCP and TKT-01M3M4KVNW (phase 4).
- Lock, pin and the moderation helper: TKT-01M29CKRGS.
- Server-side slug redirects for the non-JS client: Phase 5, docs/07 "Path to a non-JS client".
- The client controls for rename and move: a draft filed from this ticket.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:07:51Z

Open decision for the maintainer, from Phase 2 grooming on 2026-09-29: the default for threads.rename_window_seconds. docs/08 gives the author a rename window but no length. Options: 24 hours, matching the post edit window (recommended, since the title is the opening post's headline); 15 minutes, matching the delete window; or 0, meaning moderators only until an admin opens it. The plan's Open decision section carries the same. Retag was split out to TKT-01M29CYSCP (phase 4) in the same grooming.

**agent:claude-code/phase2-groom** at 2026-09-29T05:39:01Z

Maintainer decision, 2026-09-29: the author's thread rename window is 24 hours (threads.rename_window_seconds defaults to 86400), accepting the grooming recommendation. Supersedes the open decision in the earlier note. Moved into the plan's Settled section.

**agent:claude-code/phase2-run-krh9** at 2026-09-30T02:11:24Z

Criterion 1 evidence, in two halves. Server: TestRenameMovesTheResponseCacheKey reads the thread after a rename, by the plain path and by ?ordinal=4, and every class gets the new slug in the header. Client: the stale-slug correction was already built and tested before this ticket, in canonicalThreadUrl and the 'a stale slug' tests in web/client/src/view.test.tsx. Those cover ?page=2&lens=..., /42/history, /42#post-42 and a thread already on screen. No client test was added, because this ticket's client change was limited to the one problem.ts entry while the dist was shared. Settled at claim, not a new product decision: a moderator's rename skips the author's lock, window and archived-forum checks, because the criterion says a moderator may rename at any time. A moderator renaming their own thread is still not audited, because the projection decides that from the actor.

## Summary

Rename is PATCH /api/v1/threads/{id} {title}. The author may rename within threads.rename_window_seconds (default 86400), then gets the new code rename-window-closed (403); a moderator may rename at any time. Move is POST /api/v1/admin/threads/{id}/move {forum}, behind the staff gate. It needs moderate in both forums; a hidden destination is not-found, and an archived one is conflict except to an admin, read from the destination row, not readOnlyForum. Moving out of an archived forum stays open to moderators, which completes TKT-01M3N24RHD's criterion 7. Both are decided on the log they land in (appendDecided in internal/service/threads/moderate.go): project to the log length, admit from that row, append at that length, and retry on ErrLogMoved. The projection writer's thread.renamed row now carries only {thread, log_offset}, and thread.moved carries {from_forum, to_forum}, with from_forum read before apply. No record types were added. The client change is the rename-window-closed message and the regenerated dist. The rename and move controls and a rename_until viewer field are draft TKT-01M3QXV399 (Add rename and move to the thread screen).
