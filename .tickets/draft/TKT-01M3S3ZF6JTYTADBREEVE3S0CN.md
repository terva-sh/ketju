---
schema: 3
id: TKT-01M3S3ZF6JTYTADBREEVE3S0CN
title: Show the thread's starter, time, post count and breadcrumb in its head
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:38Z
updated_at: 2026-10-01T07:55:58Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` and `MobileThread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergences T1, T2 and P5). T1 was listed in TKT-01M3S3ZZSZ (Close the small visual gaps between the client and the mockups). Grooming moved it here on 2026-10-01, because both redraw the same thread head.

Under the title the mockups draw a meta line, "started by mara · 3 days ago · 212 posts", then the tag chip, the pin and the watch control. Other tickets own three of those parts:
- the tag chip, TKT-01M29CYSCP (Implement thread tags);
- the pin note, TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen);
- watching, TKT-01M29CRJ29 (Implement watches and mutes).

Nothing owns the first part: who started the thread, when, and how many posts it has. Above the title the desktop mockup draws a breadcrumb, "Forums › General" (T1). The client's thread head draws only the forum's name and the title (`web/client/src/screens/ThreadScreen.tsx`, `thread-head`).

### What to do
Draw "started by HANDLE · TIME · N posts" under the title at both widths, from the thread page's DTO, and add the starter to that DTO. Draw the breadcrumb above the title. Times are absolute (TKT-01M3S3ZF48, decided 2026-10-01).

## Acceptance criteria

- [ ] At 390 px and 1440 px the thread head reads started by HANDLE · TIME · N posts under the title, with HANDLE the thread's starter, TIME an absolute PostTime of created_at and N the thread's post_count
- [ ] Above the title the forum is a breadcrumb, Forums › FORUM, linking the front page and the forum
- [ ] ThreadHeader carries author on every route that answers one (golden, dto-contract.ts and docs/07 updated), and a starter's rename or anonymization shows on the next view through the response cache (TestThreadHeaderAuthorFollowsRename)
- [ ] The thread page passes axe at 390 px and 1440 px (e2e/a11y.spec.ts) and thread-*.png are refreshed
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **The meta line.** Decided by the run (2026-10-01, pending maintainer review) and recorded in docs/06 "The thread head".
  - It reads "started by HANDLE · TIME · N posts" at both widths.
  - HANDLE is the thread's author as its thread-list row names it: `ThreadRow.author.handle`, from `threads.author_id`.
  - TIME is `ThreadHeader.created_at` through `PostTime`.
  - N is `ThreadHeader.post_count`, the count the pager uses, deleted posts included.
- **Times are absolute** (docs/06 "Post anatomy", maintainer 2026-10-01, TKT-01M3S3ZF48). The description's "AGE" is TIME.
- **The breadcrumb, divergence T1, moves here from TKT-01M3S3ZZSZ.** Both redraw `thread-head` in `ThreadScreen.tsx`. Above the title the forum is "Forums › FORUM", linking `/` and `/f/{slug}` (`docs/mockups/generate.py`, `thread()`).
- **The header lives in the response cache's per-class part** (docs/07 "The response cache"). The starter is the same for every viewer, so it belongs in `ThreadHeader` and not in the per-viewer `ThreadViewer`.
- **Nothing widens access.** The starter's handle reaches only a viewer who may read the thread. The thread list's row already shows that viewer the same handle.

### Approach

1. **Server.** `dto.ThreadHeader` gains `Author Author json:"author"`, the thread's starter. It is built with `member.Author` from the `users` row for `threads.author_id`, as `threads.listDetails` builds `ThreadRow.Author`, so an anonymized starter shows `deleted-user-<id>`. Find every producer of a `ThreadHeader` and fill it in each: the thread page (`threads.View`), `CreatedThread`, and the moderation answers that return a header.
2. **Cache.** Read how the cached page gets its posts' author blocks after a rename or an anonymization, and make the header's author take the same path. If the posts' authors are stamped outside the cache, stamp the header's there too. If they are inside, check that the key or the invalidation that moves them also moves the header.
3. **Contract.** Update the golden under `internal/dto/testdata`, `api/dto.ts` and `api/dto-contract.ts`, and docs/07 "Thread response shape (abridged)", which adds `"author"` to `thread`.
4. **Client** (`ThreadScreen.tsx`, `thread-head`):
   - Replace the forum link with `<nav class="breadcrumb" aria-label="Breadcrumb">`, holding Forums, then ›, then the forum.
   - Under the `h1`, add `<p class="thread-meta">started by <span class="starter">HANDLE</span> · <PostTime at={created_at}/> · N post(s)</p>`.
   - The locked and pinned notes stay where they are.
5. **CSS** (`thread.css`). `.breadcrumb` and `.thread-meta` are muted at 13 px, and the meta line wraps (`flex-wrap`). The phone block keeps its 16 px padding.
6. **Screenshots and build.** Refresh `thread-*.png` at both widths. Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader. Estimated at about +150 B gzip. Measure it.

### Tests

- `TestThreadHeaderNamesItsStarter` (`internal/service/threads`): the page's `thread.author` is the thread's author, not the last poster.
- `TestThreadHeaderAuthorFollowsRename`: after the starter's handle changes, the next view, cached or not, names the new handle. Run the same test for an anonymized starter.
- `TestCreatedThreadHeaderHasAuthor` (`internal/web/api`): the create answer's header names the creator, as a later read does.
- `ThreadScreen.test.tsx`:
  - "the head names the starter, the time and the post count": checks the text and a `time` element with `datetime`.
  - "the breadcrumb links the front page and the forum".
  - "one post reads 1 post".
- `e2e/thread.spec.ts`:
  - `.thread-meta` matches `^started by carol · .+ · \d+ posts$` at both widths. carol writes the seed's `ThreadCreated`.
  - The breadcrumb's links are `/` and `/f/general`.
  - Mask `.thread-meta time` in `capture`.
- `e2e/a11y.spec.ts`'s thread scans at 390 px and 1440 px pass.

### Out of scope

- Tag chips: TKT-01M29CYSCP.
- The watch control: TKT-01M29CRJ29 and the screens that use it.
- Moving the pinned note into the meta line. It stays as TKT-01M3QGH66W built it.
- Linking the handle to a profile: TKT-01M3V3CFMA. If that ticket links post authors, it links this handle too.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:55:58Z

run decision: pending maintainer review. The thread head reads 'started by HANDLE · TIME · N posts' at both widths. TIME is absolute, and N is the thread's post_count, the number the pager counts in. A breadcrumb, Forums › FORUM, sits above the title. Recorded in docs/06 'The thread head' with the alternatives. T1, the breadcrumb, moved here from TKT-01M3S3ZZSZ in grooming.
