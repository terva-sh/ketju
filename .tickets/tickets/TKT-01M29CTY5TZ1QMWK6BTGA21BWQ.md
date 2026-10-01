---
schema: 3
id: TKT-01M29CTY5TZ1QMWK6BTGA21BWQ
title: Implement the reply chain lens
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase3-lenses
  branch: run/cty5t-chain-lens
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a64c824ddbdb7377f
  commit: 4e2ef49d380063d4e1527fc8b9590794bbcb2c18
  session: null
  claimed_at: 2026-10-01T13:44:43Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T13:44:45Z
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

- [ ] The ancestor walk is bounded: on indexes holding a reply_to cycle, a self-reference, a forward reference and one past the last post, it stops within the thread's length and serves a page (TestChainWalkBoundedOnMalformedIndex)
- [ ] ?lens=chain:N keeps N's ancestors, N and its direct replies in ordinal order, lens.gaps counts the posts skipped between them, and a removed post keeps its place as a tombstone
- [ ] A chain page runs the canonical page's statements whatever the thread's length (db.Options.Trace)
- [ ] A post with no reply_to and no replies yields itself alone (shown 1), and chain: past the last post is not-found
- [ ] TestLensPostsMatchCanonical gains the chain rows for every viewer class and removed state

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- `?lens=chain:N` shows N's ancestors, found by following `reply_to` upward, then N, then N's direct replies, in ordinal order. Gaps show as "… 5 posts …" dividers (docs/06 "Lenses"). It reads the index entry's `reply_to` and nothing else (docs/03 "Index entry").
- A removed post keeps its place in the chain as a tombstone, with its `reply_to` intact (docs/06, under "Decided by the run (2026-10-01): what each lens keeps of removed posts"; docs/08 "Tombstones").
- `chain:` past the last post is `not-found`, as `?ordinal=` is, and a malformed `chain:` serves the canonical page (docs/07, under "Decided by the run (2026-10-01): the `lens` parameter").
- Gaps come from the framework's `lens.gaps`, and the client draws them for every lens (docs/06, under "the lens bar").
- Built on the framework in TKT-01M29CRJ1P.

### Approach

1. **`lens.Chain(ix *threadlog.Index, n uint32)`** builds the ordinal set inside the snapshot, before `Select` runs, because a filter must not call back into the index.
   - The ancestor walk starts at `cur = n`. It reads `ix.Entry(cur).ReplyTo`, and stops at 0, at a value at or above `cur`, at a value past `ix.Len()`, or after `ix.Len()` steps. It adds each ancestor.
   - A well-formed log only ever points backwards, so the `>= cur` check is what ends a cycle, a self-reference or a forward reference. The step cap is the second bound.
   - Replies come from `ix.Replies(n)`. The filter keeps membership in that set.
2. In the resolver, `n` of 0 never reaches here (the framework's `Parse` reads it as canonical). `n` past `ix.Len()` is `not-found`, decided inside the snapshot.
3. The cache key's `Lens` is `chain:<n>`. The selection is the same for every viewer.

### Tests

- `TestChainWalkBoundedOnMalformedIndex`: indexes built with `threadlog.NewIndex` holding a two-post cycle, a self-reference, a forward reference and a `reply_to` past the last post each answer within the thread's length, with the walk stopped at the bad link.
- `TestChainLensOrdinalOrderWithGaps`: a branch 3 → 9 → 20 with replies 22 and 31 serves 3, 9, 20, 22, 31, and `gaps` counts the posts between them, the gaps before and after included.
- `TestChainThroughRemovedPost`: an author-deleted ancestor appears as its tombstone with `reply_to` kept and no body for a member. A moderator sees the body, as on the canonical page.
- `TestChainLensSinglePost`: a post with no `reply_to` and no replies yields itself, `shown` 1.
- `TestChainPastEndNotFound`, and `TestChainMalformedServesCanonical`, which covers `chain:0` and `chain:x`.
- `TestChainLensStatements`: a chain page runs the canonical page's statements on a 10-post and a 2000-post thread (db.Options.Trace counter).
- `TestLensPostsMatchCanonical` gains the chain rows: every viewer class, the moderator by group grant and the suspended viewer included, and every removed state.

### Out of scope

- The "Conversation" entry on a post, the gap dividers, and the "not a reply and has no replies" message: `TKT-01M29CTY71`.
- Descendants beyond direct replies: docs/06 names only direct replies.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:11Z

run decision: pending maintainer review. The chain keeps a removed post in place as its tombstone, with reply_to intact (docs/06 'Lenses'). chain: past the last post is not-found and a malformed chain: serves the canonical page (docs/07). Every lens draws gap dividers, not only the chain (docs/06, the lens bar). The Conversation entry and the dividers are drawn in TKT-01M29CTY71. Decided in the Phase 3 grooming of 2026-10-01, lane lenses.
