---
schema: 3
id: TKT-01M29CTY5TZ1QMWK6BTGA21BWQ
title: Implement the reply chain lens
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
  - TKT-01M29CRJ1PY355KX3HX7A5N785
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
  - ref: pr:293
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T13:52:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-lenses
  name: ""
extensions: {}
---

## Description

`?lens=chain:42` showing the ancestor line of post 42, found by following reply_to upward, plus its direct replies, in ordinal order, with omitted ranges rendered as "… 5 posts …" dividers.

This is the lens that answers what a tree would have answered, without becoming one. The posts stay in ordinal order and the gaps are visible, so a reader always knows they are looking at a slice rather than the thread.

A reply_to cycle should be impossible, but the walk needs a bound anyway, because a corrupt index is a thing that happens and an infinite loop in a request handler is worse than a wrong answer.

## Acceptance criteria

- [x] The ancestor walk is bounded: on indexes holding a reply_to cycle, a self-reference, a forward reference and one past the last post, it stops within the thread's length and serves a page (TestChainWalkBoundedOnMalformedIndex)
- [x] ?lens=chain:N keeps N's ancestors, N and its direct replies in ordinal order, lens.gaps counts the posts skipped between them, and a removed post keeps its place as a tombstone
- [x] A chain page runs the canonical page's statements whatever the thread's length (db.Options.Trace)
- [x] A post with no reply_to and no replies yields itself alone (shown 1), and chain: past the last post is not-found
- [x] TestLensPostsMatchCanonical gains the chain rows for every viewer class and removed state

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`). Revised at claim on 2026-10-01 against main at 4e2ef49d, after the framework (#299) and the unread (#304) and author (#310) lenses merged.

### Settled in grooming

- `?lens=chain:N` shows N's ancestors, found by following `reply_to` upward, then N, then N's direct replies, in ordinal order. Gaps show as "… 5 posts …" dividers (docs/06 "Lenses"). It reads the index entry's `reply_to` and nothing else (docs/03 "Index entry").
- A removed post keeps its place in the chain as a tombstone, with its `reply_to` intact (docs/06, under "Decided by the run (2026-10-01): what each lens keeps of removed posts"; docs/08 "Tombstones").
- `chain:` past the last post is `not-found`, as `?ordinal=` is, and a malformed `chain:` serves the canonical page (docs/07, under "Decided by the run (2026-10-01): the `lens` parameter").
- Gaps come from the framework's `lens.gaps`, and the client draws them for every lens (docs/06, under "the lens bar").
- Built on the framework in TKT-01M29CRJ1P.

### Approach

1. **`lens.ReplyChain(ix *threadlog.Index, n uint32)`** builds the ordinal set inside the snapshot, before `Select` runs, because a filter must not call back into the index. It is not named `lens.Chain`, because that is the kind constant.
   - The ancestor walk starts at `cur = n`. It reads `ix.Entry(cur).ReplyTo`, and stops at 0, at a value at or above `cur`, at a value past `ix.Len()`, or after `ix.Len()` steps. It adds each ancestor.
   - A well-formed log only ever points backwards, so the `>= cur` check is what ends a cycle, a self-reference or a forward reference. The step cap is the second bound.
   - Replies come from `ix.Replies(n)`. The filter is `lens.Ordinals` over the set.
2. `resolveChain`, the `lens.Chain` row of `lensResolvers`, reads nothing from the database. `n` of 0 never reaches it (`Parse` reads it as canonical). `n` past `ix.Len()` is `not-found`, decided inside the snapshot by `ReplyChain`.
3. The cache key's `Lens` is `chain:<n>`. The selection is the same for every viewer.

### Tests

- `TestChainWalkBoundedOnMalformedIndex` (package `lens`): indexes built with `threadlog.NewIndex` holding a three-post and a two-post cycle, a self-reference, a forward reference, a `reply_to` past the last post and the largest `reply_to` each answer a page, with the walk stopped at the bad link. A well-formed 5000-post line is walked to its root.
- `TestChainKeepsAncestorsAndReplies` (package `lens`): ancestors, the post and its direct replies, but not replies' replies or siblings. `chain:0` and past the end are not-found.
- `TestChainOverCorruptIndex`: the same through the endpoint, with a cycle and a self-reference written into the sidecar index (`world.entryOffline`).
- `TestChainLensOrdinalOrderWithGaps`: a branch 3 → 9 → 20 with replies 22 and 31 serves 3, 9, 20, 22, 31, and `gaps` counts the posts between them, including the gaps before and after.
- `TestChainThroughRemovedPost`: an author-deleted ancestor appears as its tombstone, with `reply_to` kept and no body for a member, for its author or for an anonymous viewer. A moderator sees the body. Each matches the canonical page byte for byte.
- `TestChainLensSinglePost`: a post with no `reply_to` and no replies yields itself, `shown` 1.
- `TestChainPastEndNotFound` also covers `chain:0`, `chain:x`, `chain:` and `chain:-1`, which serve the canonical page.
- `TestChainLensStatements`: a chain page runs the canonical page's statements on a 10-post and a 2000-post thread (db.Options.Trace counter).
- `TestLensPostsMatchCanonical` gains the chain rows, the chain of every post in `accessThread`, `chain:10` running through a post a moderator hid. The rows cover every viewer class, the moderator by group grant and the suspended viewer included, and every removed state.

### Out of scope

- The "Conversation" entry on a post, the gap dividers, and the "not a reply and has no replies" message: `TKT-01M29CTY71`.
- Descendants beyond direct replies: docs/06 names only direct replies.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:11Z

run decision: pending maintainer review. The chain keeps a removed post in place as its tombstone, with reply_to intact (docs/06 'Lenses'). chain: past the last post is not-found and a malformed chain: serves the canonical page (docs/07). Every lens draws gap dividers, not only the chain (docs/06, the lens bar). The Conversation entry and the dividers are drawn in TKT-01M29CTY71. Decided in the Phase 3 grooming of 2026-10-01, lane lenses.

## Summary

The reply chain lens is served. lens.ReplyChain(ix, n) runs inside the snapshot. It walks reply_to upward and stops at 0, at a link at or past the post that names it, at a link past the last post, or after the thread's length in steps. It adds n and ix.Replies(n). n past the last post is not-found. resolveChain is the chain row of lensResolvers; it reads nothing from the database, and its key is chain:N. The walk is tested on corrupt indexes in package lens and through the endpoint, using a new world.entryOffline that edits a sidecar entry; flagOffline now uses it. A removed ancestor stays in the chain as the tombstone the canonical page shows. TestLensPostsMatchCanonical gained the chain of every post. Conversation, the dividers and the empty message are TKT-01M29CTY71.
