---
schema: 3
id: TKT-01M29CKRJ9DTBAC2BSBYAT9CQC
title: Render tombstones for deleted and moved posts
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

"Post removed by author" or "Post removed by a moderator", with the ordinal preserved so numbering, permalinks, and reply lenses keep working. Moderators see the original inline on a distinct background.

Replies to a deleted post keep their reply_to link. A tombstone is a gap in content, never a gap in the sequence, and that is the property that keeps a linear thread readable after moderation.

## Acceptance criteria

- [ ] Replies to a removed post keep reply_to, and GET …/replies of the removed post and the client's expander under its tombstone still list them
- [ ] For an author-deleted and a moderator-hidden post, anonymous viewers and other members get no html and no revisions from the page, the single post, replies, user posts and history; a moderator gets the html in each
- [ ] The author of a hidden-by-mod post gets its html with removal moderator on the page, the single post, their user posts and its history; their own self-deleted post stays a tombstone without html
- [ ] A page holding the viewer's own hidden post is cached under class author:<id>, and no other member's answer carries its html in either request order (TestResponseCacheSeparatesTheHiddenPostsAuthor)
- [ ] The client renders 'Post removed by author' and 'Post removed by a moderator', shows a removed post's body to those served it on a distinct background, and renders a pointer to /t/{moved_to} when moved_to is set (component test; the server fills moved_to in TKT-01M29CYSAM)
- [ ] A removed post keeps its ordinal: ?ordinal=N serves the page holding it, and a member receives that post with removal set and no html
- [ ] Removing one post changes nothing for visible posts: members and anonymous viewers still get their html, and their history follows public_edit_history as before (TestVisiblePostsUnchanged)
- [ ] The author of a hidden-by-mod post reads its history even with public_edit_history off, and only for that post while it is hidden (TestHiddenPostsAuthorReadsHistoryWithSettingOff)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **Wording and ordinals** (docs/08 "Tombstones"): "Post removed by author" or "Post removed by a moderator". The ordinal, the permalink and the reply lenses keep working, and replies keep their `reply_to`. Moderators see the original inline on a distinct background.
- **Tombstones come from the index alone** (docs/03 "Read path"): `Reader.ReadEntries` never reads a tombstoned post's bytes, and `Reader.HiddenEntry` is the one path that does.
- **The author sees their own hidden post.** Maintainer decision, 2026-09-28, on TKT-01M29CRJ5J and in docs/08: "The author still sees the post, marked as removed by a moderator". The same holds for an auto-hidden post, marked "hidden pending review" (decision of 2026-09-28 on TKT-01M29CP2PJ, docs/08 "Flags"). A post the author deleted themselves stays a tombstone without a body to its author (docs/07 `/users/{handle}/posts`).
- **A new viewer class: the author of a hidden post.** docs/07 names three classes: anon, member and moderator. Serving a member their own hidden post's content from the `member` class would hand it to every member. So when the page's snapshot holds a hidden-by-mod entry whose `Author` is the viewer, the class is `author:<user id>`. That puts the viewer in `respcache.Key` for exactly those pages and no others. Overlaying the content after a `member` cache hit lost: it would be a second assembly path that has to agree with the first, which is how a leak starts. Bypassing the cache for such pages lost only on cost; it would also be correct.
- **Moved posts.** Nothing in Phase 2 writes a moved post: split is Phase 4 (TKT-01M29CYSAM, TKT-01M29CXJZZ; types 20 and 21 are held). This ticket builds the client pointer from `dto.Post.moved_to`, and the split ticket fills `moved_to` on the server.

### Approach

1. **DTO**: `dto.Post` gains `removal`, one of `"author"`, `"moderator"` or `"review"`, and null for a visible or moved post. It is derived from the entry flags: `EntryDeleted` is author, `EntryHiddenByMod` with `EntryAutoHidden` is review, and `EntryHiddenByMod` alone is moderator. `EntryAutoHidden` arrives with TKT-01M29CP2PJ, so until then only author and moderator occur. `deleted` stays true for every removal, for compatibility. Update the `internal/dto` goldens and `web/client/src/api/dto.ts`.
2. **Who gets a removed post's content** is one function, `revealRemoved(a access, e threadlog.IndexEntry) bool`, in `internal/service/threads/view.go` (narrowed for Terva review 1352 on PR #138, finding 2). It answers only for a removed entry, one where `!e.Flags.Visible()`, and it is never asked about a visible one. It is true for a moderator, and for the entry's author when `EntryHiddenByMod` is set. It is false for a moved entry, which has no content here, and false otherwise. A visible post keeps the path it has today: its content goes to everyone admitted to the thread, and its history goes to whoever `History` admits now (members when `public_edit_history` is on, and moderators always). Every read uses the same rule:
   - `postsDeps` replaces `p.Tombstone && moderator` with `p.Tombstone && revealRemoved(a, e)`. The page, `Post`, `Replies` and `UserPosts` (`historyBatch` and `historyShows` in `userposts.go`) all assemble through it.
   - `History` in `posts.go` admits the author of a hidden-by-mod post to that post's history whatever `public_edit_history` says, before the setting check (revised for Terva review 1359 on PR #138). This follows from the maintainer's 2026-09-28 decision that the author still sees their hidden post (docs/08): a moderator may have edited it before hiding it, and the history is how the author sees what they are shown. The bypass is narrow. It applies only to the viewer's own post, only while that post is hidden-by-mod, and it grants nothing for visible posts, self-deleted posts or anyone else's. Everyone else keeps today's admission check: anonymous is `unauthenticated`, a member is `forbidden` when the setting is off, and a moderator is always admitted. After admission, revisions are withheld only when the entry is removed and `revealRemoved` is false. Qualifying the promise to "only while `public_edit_history` is on" lost, because it would make an author's view of their own hidden post depend on a setting that exists to govern what other members read.
   - Render quotes are unchanged: a quote of any tombstone renders as a tombstone to everyone.
   - A single predicate over every entry, visible ones included, lost to this narrower one. "May this viewer see this post" already has an answer for visible posts, in `access` and the history setting, and one function answering both questions invites the mistake review 1352 found.
3. **Viewer class**: `pagePosts` computes the class from the snapshot. It is `ClassModerator` when `canModerate`. It is `"author:" + id` when any snapshot entry is hidden-by-mod and authored by the viewer. Otherwise it is `ClassMember` or `ClassAnon`. The class passes to `postsDeps` in place of the `moderator` bool. Update the `Class` doc comment on `respcache.Key` and docs/07 "Viewer class".
4. **Client**:
   - `components/thread/Tombstone.tsx` renders by `removal`: "Post removed by author" or "Post removed by a moderator", plus the review wording chosen by TKT-01M29CP2PJ's open decision. With `moved_to` set, it renders "Post moved to another thread" linking to `/t/{moved_to}`. The ordinal, anchor and reply indicator are kept.
   - `Post.tsx`, given a post with `deleted` and `html`, renders it on a distinct background, class `post removed`, with a label naming the removal. That covers both the moderator's view and the author's own hidden post.
   - `ReplyExpander` works under a tombstone.
   - CSS in `components/thread/thread.css` for both themes. Regenerate `dist/`.
5. **Docs**: docs/07 "Thread response shape" gains `removal` and a fourth viewer class. The `/users/{handle}/posts` row says the author sees their own hidden posts with the body. That replaces "A post hidden by a moderator is a deleted post here", which contradicts the 2026-09-28 decision. docs/06 needs no change.

### Tests

- `TestTombstoneKeepsOrdinalAndPermalink`: a deleted post 5 of 10 is served at `?ordinal=5` as `removal: "author"` with no `html`. Post 6, which replies to 5, keeps `reply_to: 5`. `GET …/posts/5/replies` answers 6.
- `TestMembersNeverReceiveRemovedContent`: for an author-deleted post and a moderator-hidden post, an anonymous viewer and a member other than the author get no `html` and no revisions. The same holds on the page, `GET …/posts/{n}`, `…/replies` of its parent, `/users/{handle}/posts`, and `…/history`. A moderator gets the `html` in each. A quote of the post renders as a tombstone to all three.
- `TestHiddenPostsAuthorReadsHistoryWithSettingOff`: with `public_edit_history` off, the author of a hidden-by-mod post gets its revisions. The same author is `forbidden` on the history of their own visible post and of their own self-deleted post, and another member is `forbidden` on the hidden post's history.
- `TestVisiblePostsUnchanged`: with a removed post on the page, a member and an anonymous viewer still get every visible post's `html`. A member still gets a visible post's history while `public_edit_history` is on, and `forbidden` when it is off.
- `TestAuthorSeesOwnHiddenPost`: the author gets `html` and `removal: "moderator"` on the page, the single post, their user posts and the history. For their own self-deleted post they get a tombstone with no `html`.
- **Response cache invariant**, `TestResponseCacheSeparatesTheHiddenPostsAuthor` (`internal/web/api/respcache_test.go`): with the cache on, request the page as the author, then as another member, then as the author again, and in the reverse order. The other member's answer never carries the hidden post's `html`, the author's always does, and the two use different keys (class `author:<id>` against `member`). A page with no hidden post of the viewer's still uses `member`, so it stays shared.
- Goldens: `thread.{anon,member,moderator}.json` in `internal/web/api/testdata/golden` gain a deleted post and a hidden post, and a new `thread.author.json` is added.
- Client: `Tombstone` renders each removal's text and the moved pointer. A moderator's view renders the removed body with the `removed` class. The reply expander opens under a tombstone.

### Out of scope

- Setting hidden-by-mod (moderator deletes): TKT-01M29CRJ5J. Auto-hide and its wording: TKT-01M29CP2PJ.
- Filling `moved_to` on the server: TKT-01M29CYSAM (phase 4).
- The moderator lens (`?lens=mod`): TKT-01M29CTY6E (phase 3).
- Redacted posts' tombstones: TKT-01M29CXJZ9.
