---
schema: 3
id: TKT-01M3S3ZZYA9BPJE1VM260806WK
title: Head the forum index with the site name and show draft-saved status
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
dependencies:
  - TKT-01M29CXK2HYBSGKYBBRHJ5NA0T
  - TKT-01M3NS19315QTWBKNZN782VRF1
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-10-01T07:56:06Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/Thread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergences F1 and T9). Both are small and each extends a ready ticket, so they are filed together.

- F1: the forum index's heading is "Forums". The mockup's is the community's name, "The Thursday Group". The note of 2026-09-25 on TKT-01M29CEE5Y left it out because the API had no site name. TKT-01M29CXK2 (Build the admin site settings screen) puts `site_name` into /me and the wordmarks, but its criteria do not mention the page heading.
- T9: the mockup's composer footer shows "Draft saved 12 seconds ago". TKT-01M3NS19 (Autosave reply drafts from the composer) saves drafts, but none of its criteria is a visible saved status.

### What to do
Once those two are done, head the forum index with `site_name` (falling back to "Forums"), and show when the composer last saved the draft.

## Acceptance criteria

- [ ] At 390 px and 1440 px the forum index's heading is the site_name setting, the value the wordmark shows
- [ ] The draft note reads Draft saved TIME, TIME an absolute PostTime of the server's updated_at for the draft, in the reply composer, the phone sheet and the new-thread screen (drafts.test.ts, Composer.test.tsx, e2e/drafts.spec.ts)
- [ ] The forum index and the composer pass axe at 390 px and 1440 px, and forum-index-*.png are refreshed
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **Both dependencies are done.** TKT-01M29CXK2H put `site_name` in `/me` and in the wordmarks (`state/viewer.ts` `siteName`). TKT-01M3NS1931 built autosave (`state/drafts.ts`).
- **F1, the forum index's heading.** Decided by the run (2026-10-01, pending maintainer review) and recorded in docs/06 "Thread list views (forum index)". The heading is `site_name`, the same `siteName` the wordmark shows.
  - The description's "falling back to Forums" does not arise. `site_name` defaults to "Ketju" (`settings.SiteName`, docs/04 settings), `/me` always carries it, and `siteName` falls back to "Ketju" before `/me` answers. So the heading falls back as the wordmark does.
  - The name now appears in the bar and in the heading. That is the cost of docs/07's decision that the wordmark shows `site_name` (TKT-01M29CXK2H). The mockups avoid it only because their wordmark is "Ketju".
- **T9, when the draft was saved.** Decided by the run (2026-10-01, pending maintainer review) and recorded in docs/06 "Composer". The note reads "Draft saved TIME", where TIME is the `updated_at` of the server's latest answer for the draft (`dto.Draft` and `dto.ForumDraft`, docs/07 "Drafts"), through `PostTime`.
- **Times are absolute** (maintainer 2026-10-01). The mockup's "12 seconds ago" is not a divergence.

### Approach

1. **F1** (`ForumIndex.tsx`). `<h1>{siteName}</h1>`. Update every test that expects "Forums" in that heading: `ForumIndex.test.tsx`, `e2e/forum-index.spec.ts`, and any focus test that names it.
2. **T9, the state** (`state/drafts.ts`).
   - `Entry` gains `savedAt: Signal<Millis | null>`.
   - It is set from each PUT answer's `updated_at`, and from the GET's when a draft loads. A DELETE clears it, and so does `resetDrafts`.
   - The keepalive PUT on pagehide is never read, so it sets nothing.
   - Export `draftSavedAt(key)` beside `draftNote`.
3. **T9, what is drawn.** Where `draftNote` says "Draft saved", the note renders "Draft saved " followed by `<PostTime at={savedAt}/>`. "Saving…", "Draft not saved" and "Restored from this browser" are unchanged. That covers:
   - the reply composer's footer (`composer/Composer.tsx`, `draft-note`), which the phone's sheet reuses;
   - the new-thread screen's note (`NewThreadScreen.tsx` through `state/newThread.ts`).

   The note's `role="status"` stays as it is.
4. **Screenshots and build.** Refresh `forum-index-*.png`, and `thread-desktop-*.png` if the composer's footer shows. Mask the note's `time` in `capture`. Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader. Estimated at about +60 B gzip. `PostTime` is already there.

### Tests

- `drafts.test.ts`:
  - "a saved draft carries the server's updated_at".
  - "a loaded draft carries the GET's updated_at".
  - "a discard clears it".
  - "a refused save keeps the last time and says not saved".
- `Composer.test.tsx`: "the footer reads Draft saved and a time element once a save answers".
- `NewThreadScreen.test.tsx`: the same, for a forum draft.
- `ForumIndex.test.tsx`: "the heading is the site name from /me".
- `e2e/forum-index.spec.ts`: the `h1` reads the wordmark's text, at 390 px and 1440 px.
- `e2e/drafts.spec.ts`: after typing, the note reads `Draft saved` with a `time` whose `datetime` is set, in the desktop composer and the phone sheet.
- Each spec's axe scan passes at both widths.

### Out of scope

- The wordmark. docs/07 "Site settings" fixes it as `site_name`.
- The mockup's "12 unread posts across 5 threads since Tuesday evening" summary line, which no divergence asked for.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:56:06Z

run decision: pending maintainer review. Two decisions. (1) The forum index's heading is site_name, the same value the wordmark shows, recorded in docs/06 'Thread list views (forum index)'. The name then appears twice, a cost of docs/07 putting site_name in the wordmark (TKT-01M29CXK2H). The description's 'Forums' fallback does not arise, because site_name defaults to Ketju. (2) The draft note reads 'Draft saved TIME', TIME being the server's updated_at through PostTime, recorded in docs/06 'Composer'. Both dependencies are done.
