---
schema: 3
id: TKT-01M29CA9AXKRTS6KMN5CTH7461
title: Implement the thread page endpoint
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
  - TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
  - TKT-01M29C0WF04HEBYMB9DXCB9WCB
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
  - TKT-01M29BYYJ87H5ERS3RXC66XF44
  - TKT-01M29C2WW5N8TR3V76VEB3TGK7
  - TKT-01M29BYYHQ7PTYNYA3VBBD4PDB
blocks_on: none
references:
  - ref: design:api-thread-shape
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T09:24:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The load-bearing endpoint: `GET /threads/{id}?page=&lens=&after_log_length=` returning the thread header, a page of rendered posts, lens metadata, and the viewer's read position.

The flow from docs/02: resolve the viewer, check permissions, open the thread, apply the lens to the in-memory index, fetch and render the selected posts through the post cache, then layer on viewer data. The viewer block is the only per-user part, and keeping it the only one is what makes the response cacheable across members.

`ETag` is `(thread_id, log_length, renderer_version)` with `Vary: Cookie`, and `If-None-Match` is honored. The log length is the thread's version number, available without a database query, which is what makes this cheap.

## Acceptance criteria

- [x] The response matches the documented shape in docs/07, with a golden test per viewer class
- [x] ETag is derived from thread id, log length, and renderer version, and a matching If-None-Match returns 304
- [x] A permalink to an ordinal resolves to the page containing it
- [x] `after_log_length` returns only posts past that point, which is what the SSE client uses to reconcile
- [x] Deleted posts return tombstones for members and original content for moderators

## Implementation plan

Revised at claim (2026-09-24), after reading the code as it stands and landing the three missing dependencies (TKT-01M29BYYJ8, TKT-01M29C2WW5, TKT-01M29BYYHQ).

### Approach
1. **threadlog additions** (API only; the format is unchanged):
   - `Actor.LogLength()`. An atomic, published only after the index has applied the batch. Reading it and then the index snapshot therefore never yields an index older than the length.
   - `Post.RevisionMS`: the current revision's frame timestamp, which gives edited_at.
   - `Reader.Hidden(ordinal)`: reads a tombstoned post's body, for moderators.
   - `Index.ReplyCounts(ordinals)`: one pass instead of one Replies call per post.
2. **Viewer on the request.** `domain.WithViewer` and `domain.ViewerFrom(ctx)`. The session middleware (TKT-01M29C5Y3E) will set it; until then every request is anonymous.
3. **`internal/service/threads.View`:**
   - Parse the id, then load the threads projection row. A missing row is 404.
   - Check `perm.Check(read)`: Hidden and a missing thread are 404, and Deny is 403.
   - Load the forum ref and tags.
   - Open the thread through threadcache: read the log length, then the index snapshot. post_count comes from the index, so it agrees with log_length.
   - **Paging.** The page size is 25; the user setting arrives with /settings. If `?ordinal=N` is given, use its page. Otherwise use `?page`. With neither, a signed-in viewer with a read position lands on the page holding last_read_ordinal+1, and anyone else on page 1. A page past the last is 404, and a malformed parameter is 400.
   - **Lens.** Only the identity lens exists: any `lens` value is 400 until TKT-01M29CRJ1P.
   - **`after_log_length=L`** narrows the selected page to posts whose current revision was written at or after L. That covers new posts and edits; deletes and restores are not included, as settled in grooming.
   - **Viewer class.** anon when not a member; moderator when `Can(moderate)` on the forum; member otherwise.
   - **Viewer block.** last_read_ordinal, watch, can_moderate, and can_reply = `Can(reply) && (!locked || can_moderate)`.
   - **ETag, computed before rendering:** `"<id>-<log_length>-r<render.Version>-<class>-p<log_length_projected>-v<hash of the viewer block>"`. A matching If-None-Match returns 304 with no rendering. The viewer hash covers the grooming decision (read position) and also watch and permissions changes. The projected length covers a header that catches up under an unchanged log length.
   - **Posts** come from `Reader.Range`. HTML comes from the render cache, and a miss runs `render.RenderWith` with index-backed quote resolution and users-backed mention resolution. Authors come from one `UsersByIDs` per page.
     - `reply_to_suppressed` is `reply_to == ordinal-1`.
     - machine_authored comes from the entry flag.
     - `deleted` covers both the deleted and the hidden-by-mod flags.
     - A tombstone carries empty HTML for anon and member, and the original HTML for a moderator.
     - moved_to is null, because splits are Phase 4.
     - Attachments carry only the hash until the blobs table (TKT-01M29BYYJ0) supplies the metadata.
4. **`internal/web/api`**: the `GET /api/v1/threads/{id}` handler, with ETag, `Vary: Cookie`, and 304.
5. **docs/07**: the grooming decisions (landing page, `?ordinal=`, the ETag's extra parts, after_log_length's page scope, deletes refetching), plus the ETag shape.

### Tests
- **AC1:** handler goldens for anon, member and moderator over a seeded thread, decoded strictly into dto.ThreadPage.
- **AC2:** the ETag format; 304 on a match; 200 after an append; a changed read position changes the ETag.
- **AC3:** ordinal-to-page at sizes 25 and 10, including boundaries.
- **AC4:** 30 posts, then record L, append 3, and request with after_log_length=L: exactly those 3, plus an edit of an older post on the same page. Run under -race with concurrent appends.
- **AC5:** a deleted post as a tombstone for anon and member, and with content for a moderator.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Settled during Phase 1 grooming (2026-09-24), and written into docs/07 in this ticket's own PR:
1. When page is absent, the server picks the landing page. For a signed-in viewer with a read position, that is the page containing last_read_ordinal + 1 (docs/06). Otherwise it is page 1.
2. The permalink criterion uses ?ordinal=N, which resolves to that ordinal's page.
3. The ETag also includes the viewer class (anonymous, member, moderator) and the viewer's read position. Otherwise a 304 could hand a moderator a member's cached page, or serve a stale divider.
4. after_log_length does not report deletes and restores, because those change posts at or before that point. The SSE client refetches the page on a moderation event instead.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T08:17:43Z

Released at claim (2026-09-24): the ticket was missing three dependencies, so it is not blocked. The thread header (slug, title, locked, pinned, tags, current forum) exists only in the threads projection, which is what docs/04 and this ticket's own plan read. The index carries no thread-level state, and nothing in threadlog folds those records. The projection is TKT-01M29BYYJ8 (tables) plus TKT-01M29C2WW5 (the writer that fills them). The viewer block reads read_positions and watches from TKT-01M29BYYHQ. All three are now recorded as dependencies, and docs/11 already routes the data track through the projection writer ahead of this ticket. Reading the header by scanning the log per request lost, because it would put a second fold of the thread records beside the projection writer's, and the two would drift. The run takes those three first.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:24:13Z

Built and verified. Where things are and what was decided:

### Layout
- `internal/service/threads` (`Service.View`) owns every rule; `internal/web/api` only parses and writes. The HTML client (later) calls the same `View`.
- threadlog gained `Actor.LogLength()` (published after the index applies a batch, so a reader that takes the length and then the index never claims a length the index does not cover; `TestLogLengthNeverOutrunsTheIndex` kills a publish-before-index mutant 5/5), `Post.RevisionMS`, `Reader.Hidden`, `Index.ReplyCounts`.
- `domain.WithViewer`/`ViewerFrom` carry the viewer; C5Y3E's session middleware sets it. Until then there is no `serve` command to mount `api.New` into, so the handler is not wired — nothing to wire to.
- `UsersByIDs` query (sqlc.slice) loads a page's authors in one read.

### Decisions (alternatives considered)
- **ETag** is `"<id>-<log_length>-r<renderer>-<class>-p<log_length_projected>-v<hash>"`, extending docs/07's three parts. Class: a moderator page carries tombstone content. `p`: the header comes from the projection, which can catch up under an unchanged log length. `v`: digest of the viewer block + page + query, so moving your own read position or watch cannot be answered with a stale 304. Rejected: three parts only (stale 304s across viewers sharing a tag); a per-user id in the tag (still stale on a watch change, and leaks nothing more useful than the digest).
- **Landing page** without `page`: a member with a read position gets the page holding `last_read + 1`, clamped to the last page (docs/06 "Landing on return"). `?ordinal=` wins over `page`.
- **after_log_length** is scoped to the requested page and keeps posts whose *current revision* offset is >= L (new posts and edits). Delete/restore do not move an entry's offset, so the client refetches the page when `log_length` moves unaccounted for. Rejected: a whole-thread delta (unbounded response, and pagination then means nothing).
- **Access**: Hidden → 404, Deny anon → 401, Deny member → 403. `lens` → 400 until the lens ticket, rather than silently ignored.
- `post_count` comes from the index, not the projection, so it agrees with `log_length` and the page (`TestHeaderCountsFromTheIndex`).
- Attachments carry only the hash until the blobs table (BYYJ0).

### Tests and mutation
- Goldens: `internal/web/api/testdata/golden/thread.{anon,member,moderator}.json`, decoded strictly into `dto.ThreadPage`; regenerate with `go test ./internal/web/api -update`.
- 53 mutants over view.go and api.go. Killed all but two:
  - `if !v.Member() { v = Anonymous() }` removed is equivalent (perm and every later branch go through `Member()`); kept as the explicit statement.
  - `EntryHiddenByMod` in `Deleted` is unreachable: no record sets that flag yet. Kept so the flag counts as deleted when a hide record arrives.
- A dead `p.Tombstone = true` found by mutation was removed.
- docs/07 has a new "What the thread endpoint does" section and `ordinal=` in the resources table.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T09:24:23Z

Correction to the previous note: 46 distinct mutants, not 53 (45 in the first pass, of which 7 were rerun with compiling variants, plus 1 added). The two survivors named there are the only ones.

## Summary

GET /api/v1/threads/{id} is implemented in internal/service/threads (rules) and internal/web/api (handler): per-class goldens, an ETag over id/log length/renderer/class/projection/viewer digest with 304 on If-None-Match, ?ordinal= permalinks, page-scoped after_log_length, and tombstones with content for moderators. docs/07 documents the served behaviour. Not yet mounted: there is no serve command; C5Y3E supplies the viewer.
