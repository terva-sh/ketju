---
schema: 3
id: TKT-01M29CKRJ9DTBAC2BSBYAT9CQC
title: Render tombstones for deleted and moved posts
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
blocks_on: none
references:
  - ref: design:tombstones
    path: docs/08-moderation.md
  - ref: pr:170
    path: null
  - ref: pr:174
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T23:37:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-krj9
  name: ""
extensions: {}
---

## Description

"Post removed by author" or "Post removed by a moderator", with the ordinal preserved so numbering, permalinks, and reply lenses keep working. Moderators see the original inline on a distinct background.

Replies to a deleted post keep their reply_to link. A tombstone is a gap in content, never a gap in the sequence, and that is the property that keeps a linear thread readable after moderation.

## Acceptance criteria

- [x] Replies to a removed post keep reply_to, and GET …/replies of the removed post and the client's expander under its tombstone still list them
- [x] For an author-deleted and a moderator-hidden post, anonymous viewers and other members get no html and no revisions from the page, the single post, replies, user posts and history; a moderator gets the html in each
- [x] The author of a hidden-by-mod post gets its html with removal moderator on the page, the single post, their user posts and its history; their own self-deleted post stays a tombstone without html
- [x] A page holding the viewer's own hidden post is cached under class author:<id>, and no other member's answer carries its html in either request order (TestResponseCacheSeparatesTheHiddenPostsAuthor)
- [x] The client renders 'Post removed by author' and 'Post removed by a moderator', shows a removed post's body to those served it on a distinct background, and renders a pointer to /t/{moved_to} when moved_to is set (component test; the server fills moved_to in TKT-01M29CYSAM)
- [x] A removed post keeps its ordinal: ?ordinal=N serves the page holding it, and a member receives that post with removal set and no html
- [x] Removing one post changes nothing for visible posts: members and anonymous viewers still get their html, and their history follows public_edit_history as before (TestVisiblePostsUnchanged)
- [x] The author of a hidden-by-mod post reads its history even with public_edit_history off, and only for that post while it is hidden (TestHiddenPostsAuthorReadsHistoryWithSettingOff)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Wording and ordinals** (docs/08 "Tombstones"): "Post removed by author" or "Post removed by a moderator". The ordinal, the permalink and the reply lenses keep working, and replies keep their `reply_to`. Moderators see the original inline on a distinct background.
- **Tombstones come from the index alone** (docs/03 "Read path"): `Reader.ReadEntries` never reads a tombstoned post's bytes, and `Reader.HiddenEntry` is the one path that does.
- **The author sees their own hidden post.** Maintainer decision, 2026-09-28, on TKT-01M29CRJ5J and in docs/08: "The author still sees the post, marked as removed by a moderator". The same holds for an auto-hidden post, marked "hidden pending review" (decision of 2026-09-28 on TKT-01M29CP2PJ, docs/08 "Flags"). A post the author deleted themselves stays a tombstone without a body to its author (docs/07 `/users/{handle}/posts`).
- **A new viewer class: the author of a hidden post.** docs/07 names three classes: anon, member and moderator. Serving a member their own hidden post's content from the `member` class would hand it to every member. So when the page's snapshot holds a hidden-by-mod entry whose `Author` is the viewer, the class is `author:<user id>`. That puts the viewer in `respcache.Key` for exactly those pages and no others. Overlaying the content after a `member` cache hit lost: it would be a second assembly path that has to agree with the first, which is how a leak starts. Bypassing the cache for such pages lost only on cost; it would also be correct.
- **Moved posts.** Nothing in Phase 2 writes a moved post: split is Phase 4 (TKT-01M29CYSAM, TKT-01M29CXJZZ; types 20 and 21 are held). This ticket builds the client pointer from `dto.Post.moved_to`, and the split ticket fills `moved_to` on the server.


### Approach

Revised at claim, 2026-09-29, against main at 1dd6ed9. The grooming approach stands; what the code settled is below. The work lands in two PRs under this ticket: the server half first, then the client half once the client lane frees the committed dist (docs/13 "Running two lanes"). The server PR changes no file under `internal/web/spa/dist`: `dto.ts` gains a type field only, and `go generate ./internal/web/spa` reproduces the dist byte for byte.

1. **DTO**: `dto.Post.removal` (`*string`), one of `dto.RemovalAuthor`, `dto.RemovalModerator`, `dto.RemovalReview`, null for a visible or moved post. It is derived by `removal(flags)` in `internal/service/threads/view.go`. The deleted flag wins over hidden-by-mod when a hand-made log sets both, so such a post is `"author"` and its author is not served it. `deleted` stays true for every removal. The `internal/dto` fixture's #63 becomes a moderator removal, and a new #64 carries `moved_to`, so a moved post has `removal: null`. `web/client/src/api/dto.ts` gains `removal: string | null`, as a string like the DTO's other enums, because JSON goldens widen to string.
2. **`revealRemoved(a access, e threadlog.IndexEntry) bool`** in `view.go`. It is true for a moderator on any removal but a move, true for the author when `removal` is moderator or review and the viewer is an active member, and false otherwise, including every visible entry.
   - `posts` and `postsDeps` take the `access` in place of the `moderator` bool, read `HiddenEntry` only where `revealRemoved` says so, and render a body only for a post that is no longer a tombstone after that read.
   - `History` admits the member author of a removed post that `revealRemoved` serves them when the setting is off. The admission check moved inside the thread's snapshot, since it needs the entry. A member with the setting off still gets `forbidden` for an ordinal past the end, as before.
   - `UserPosts` builds an `access` with `canModerate` set to staff, as its old `staff` bool did.
3. **Viewer class**: `pageClass(a, snap)` in `view.go`. It is the access check's class, or `ClassAuthor + id` (`"author:<id>"`) when a non-moderator's page holds an entry `revealRemoved` serves them. `pagePosts` takes the `access` and keys the cache on `pageClass`. The same function decides both the class and the reveal, so they cannot disagree.
4. **Client** (second PR): as in the grooming approach. `Tombstone.tsx` renders by `removal` and `moved_to`, and `Post.tsx` renders a removed post that has html on a distinct background, class `post removed`, with a label. `ReplyExpander` under a tombstone, CSS for both themes, regenerated dist.
5. **Docs**: docs/07 gains `removal` in the response shape, a fourth viewer class `author:<id>` in the cache key and the "Viewer class" bullet, a "Removed posts" section, the history bypass, and the `/users/{handle}/posts` row rewritten to the 2026-09-28 decision. The goldens line names `thread.author.json`. The `respcache.Key.Class` comment names the fourth class.

### Tests

Server PR:
- `internal/service/threads/reveal_test.go`: `TestRevealRemoved`, a table over flags and viewers, including the both-bits case and an inactive author session. `TestPageClass`.
- `internal/web/api/tombstones_test.go`: `TestTombstoneKeepsOrdinalAndPermalink` (post 27 of 31, `?ordinal=27` serves page 2), `TestMembersNeverReceiveRemovedContent`, `TestRemovedRepliesFromTheExpander`, `TestAuthorSeesOwnHiddenPost`, `TestHiddenPostsAuthorReadsHistoryWithSettingOff` (also forbidden again after a moderator's restore), and `TestVisiblePostsUnchanged`.
- `TestResponseCacheSeparatesTheHiddenPostsAuthor` in `internal/web/api/respcache_test.go`: both request orders, two entries, a third member sharing the member entry, and the author's page without her hidden post sharing it too.
- Goldens: `seedHidden` adds alice's #5 hidden by bob, and `thread.author.json` is alice's page. `TestModeratorDeleteHidesAndOnlyAModeratorRestores` and `TestModeratorHideMovesTheResponseCacheKey` now expect the author to see her hidden post.

Client PR: `Tombstone` renders each removal's text and the moved pointer. A removed post with html renders with the `removed` class. The reply expander opens under a tombstone.

### Out of scope

- Setting hidden-by-mod (moderator deletes): TKT-01M29CRJ5J. Auto-hide and its wording: TKT-01M29CP2PJ.
- Filling `moved_to` on the server: TKT-01M29CYSAM (phase 4).
- The moderator lens (`?lens=mod`): TKT-01M29CTY6E (phase 3).
- Redacted posts' tombstones: TKT-01M29CXJZ9.

## Notes

**agent:claude-code/phase2-run-krj9** at 2026-09-29T22:37:12Z

Server half built on run/krj9-tombstones, commit ac88afa against main at 1dd6ed9.

Split into two PRs under this ticket, as the orchestrator allowed: the client lane (TKT-01M29CP2P1) holds the committed dist. This PR changes no file under internal/web/spa/dist. `go generate ./internal/web/spa` reproduced it byte for byte after the `dto.ts` type change, so it cannot conflict with the client lane. The ticket closes in the client PR.

Ticked in this PR: criteria 2, 3, 4, 6, 7 and 8. Left for the client PR: criterion 1, whose server half (GET …/replies of a removed post lists its replies with reply_to) is TestTombstoneKeepsOrdinalAndPermalink and TestRemovedRepliesFromTheExpander, and whose client half is the expander under a tombstone. Criterion 5 is client work.

Settled while building, internals only:
- A post carrying both the deleted and hidden-by-mod bits, which only a log appended without the service's checks can hold, is `removal: "author"`, and its author is not served it. The alternative, "moderator", would have served the author a post they had asked to remove. Written into docs/07 "Removed posts".
- `dto.ts` types `removal` as `string | null`, not a union, because the client's contract test imports the Go goldens as JSON and TypeScript widens their strings.
- With public_edit_history off, a member asking for an ordinal past the end is still `forbidden`, as before, and not `not-found`. The admission check now runs after the entry is read, so this case is kept explicitly.
- The internal/dto fixture's #63 became a moderator removal with no moved_to, and a new #64 is the moved post, so no fixture claims to be both removed and moved. The client test that counts the golden page's posts now expects post-64.

## Summary

Landed in two PRs. The server half is PR #170, rebase-merged at 2826208, with the code at b6618fe. It adds dto.Post.removal ("author", "moderator", "review" or null). threads.revealRemoved is the one rule for who gets a removed post body and revisions: a moderator, and the author while a moderator removal holds it. The page, the single post, replies, user posts and history all use it. History admits the author of a hidden post to that post history with public_edit_history off. pageClass keys a page holding the viewer own hidden post under the cache class author:<id>. docs/07 gains a "Removed posts" section. The client half is the second PR. components/thread/removal.tsx holds the wording ("Post removed by author", "Post removed by a moderator", "Post hidden pending review") and the moved pointer to /t/{moved_to}. The tombstone keeps its ordinal, anchor and reply indicator, and offers the reply expander. A removed post served with its body is drawn with class removed on the sunken surface and labelled by who removed it, on the page, in the expander and in the popover. For TKT-01M29CP2PJ: dto.RemovalReview and its wording exist, and nothing sets them yet. revealRemoved already serves the author any removal that is not their own, so an auto-hidden post needs only removal() to read the new flag.
