---
schema: 3
id: TKT-01M3S3ZZQDXM4RX4HK6V5X8VX7
title: Seed a moderator post, a tombstone and an edit in the e2e thread
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
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-10-01T07:54:06Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/` (TKT-01M39WBN4Q, divergences T11 and P8).

The mockups' thread page shows a moderator's post with its role badge, a tombstone ("Post removed by its author."), an "edited" mark, a quote, an attachment and a tag chip. The e2e seed's main thread (`cmd/ketju/e2e.go`) has only members' posts, removes nothing, edits nothing, attaches nothing, and quotes only on page 1. So the committed screenshots cannot show those parts, and `e2e/thread.spec.ts` cannot assert the badge or the tombstone row.

### What to do
Seed, on page 3 of the main thread, a post by the seeded moderator (mona), a post removed by its author, and an edited post, keeping every ordinal and reply the suite already names. Then assert the role badge and the tombstone row in `e2e/thread.spec.ts` and refresh the thread screenshots. Attachments wait for TKT-01M29CXK0 (the blob store).

## Acceptance criteria

- [ ] The e2e seed's main thread has, on page 3, a post by mona (moderator) at #56, #57 removed by its author and #52 edited by its author past the grace window, with every ordinal, reply and seedPosts value the suite names unchanged
- [ ] e2e/thread.spec.ts asserts #56's role badge, #57's tombstone row and #52's (edited) mark at 390 px and 1440 px, and thread-*.png are refreshed
- [ ] The whole Playwright suite passes in both projects with every axe scan, and no client source changes

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **The seed is for the screenshots and the assertions.** It changes no product behaviour. The screenshots are review evidence, not a pixel gate (`e2e/thread.spec.ts` header).
- **Every ordinal and reply the suite names stays as it is.**
  - `e2e/thread.spec.ts` names #51, #53 (last read), #54 and #55, which answers #1.
  - `replies.spec.ts` names #30 and #55.
  - `shortcuts.spec.ts` names #50 and #51.
  - `seedReplies` is unchanged, and `seedPosts` stays 60.
  - No spec names #52, #56 or #57 (grep of `web/client/e2e` at a599b48), so those three carry the new parts.
- **Authors alternate.** An even ordinal is alice's and an odd one is carol's (`seedUsers[i%2]`). `thread.spec.ts` runs as alice.
- **The edit must fall outside the grace window, or no edited mark is drawn.** An author's edit within `posts.grace_edit_seconds` (default 300 s, `internal/settings`) is stored but does not mark the post edited (`PostRevision.grace`). The main thread's posts are a minute apart, ending at the seed's `now`. #52 is written at `now − 8 min`, so an edit at `now` is past the window.
- **Attachments wait for the blob store**, TKT-01M29CXK0K. Tag chips wait for TKT-01M29CYSCP.

### Approach

1. **mona.**
   - In `cmd/ketju/e2e.go`, create mona's user and identity in `seed()`, after alice and carol and before the threads, so the main thread can carry her post. She keeps her ids: alice is 1, carol 2 and mona 3, as now.
   - `seedReview` stops creating her. It does not use her id.
2. **#56.** Let the main thread's body function name an author. Add a map `seedAuthors = map[int]string{56: seedModerator}`, read by `thread()` in place of the alternation for the main thread. #56 is mona's. Its source keeps "Seeded post 56." so nothing that reads text changes.
3. **#52 and #57.** After the 60 posts, append to the main thread:
   - a `TypeEdit` of #52 by alice, its author, at `now`, with a source that ends "(edited in the seed)";
   - a `TypeDelete` of #57 by carol, its author, at `now`.

   Use the record bodies `threadlog/records.go` defines. Check that the projection counts the delete, since the forum's `post_count` drops by one, and that no spec asserts General's post count.
4. **thread.spec.ts.**
   - Replace the comment that lists the badge and the tombstone as gaps in the seed.
   - Assert that `#post-56 .role-badge` reads `moderator`, that `#post-57` is a `.tombstone` reading "Post removed by author", and that `#post-52 .edited` reads `(edited)`.
   - Refresh `thread-*.png` at both widths.
5. **Full suite.** Run it in both projects. Every spec passes, a11y's thread scans included.

**Reader bundle.** No client source changes, so there is no dist change.

### Tests

- `e2e/thread.spec.ts` "the thread page renders the mockup's parts", at 390 px and 1440 px: the three assertions above.
- The whole Playwright suite in both projects, with every axe scan passing: the seed change breaks no other spec.
- `go vet` and `go build -tags e2e ./cmd/ketju` pass. CI's Playwright step builds the e2e binary.

### Out of scope

- Seeding an attachment: TKT-01M29CXK0K, then TKT-01M29CYS9W.
- Seeding a tag: TKT-01M29CYSCP.
- The tombstone's wording, "Post removed by author" against the mockup's "Post removed by its author.". No ticket asks for it, and docs/08 "Tombstones" owns it.
