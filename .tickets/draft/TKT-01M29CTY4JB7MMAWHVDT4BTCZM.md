---
schema: 3
id: TKT-01M29CTY4JB7MMAWHVDT4BTCZM
title: Implement the unread lens and jump to first unread
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:views
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ1PY355KX3HX7A5N785
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T07:51:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-lenses
  name: ""
extensions: {}
---

## Description

`?lens=unread` showing posts after the viewer's last read ordinal, paged, and the jump-to-first-unread action that targets it.

This is the lens a daily reader uses most, and it has an awkward property worth handling deliberately: reading it moves the position that defines it. Decide whether the lens freezes its boundary for the duration of the view or re-evaluates on each page, and write the choice down, because the alternative is a lens that empties itself while somebody reads it.

## Acceptance criteria

- [ ] The boundary holds while the lens is read, as docs/06 decides: a bare ?lens=unread answers lens.value = the viewer's last_read_ordinal B, and page 2 of unread:B is unchanged after a PUT /read past it (TestUnreadLensHoldsItsBoundary)
- [ ] The lens's first post is last_read_ordinal + 1, the post the canonical first-unread permalink lands on, or post 1 with no position (TestUnreadLensStartsAtFirstUnread)
- [ ] An anonymous viewer's bare ?lens=unread answers 200 with kind unread, shown 0 and no posts, never an error; TKT-01M29CTY71 draws the sign-in message
- [ ] A fully read thread answers kind unread, shown 0 and one empty page; TKT-01M29CTY71 draws the empty state offering the full thread
- [ ] Two members at different positions never share a cached unread page, and TestLensPostsMatchCanonical gains the unread rows for every viewer class and removed state

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- `?lens=unread` shows the posts after the viewer's last-read ordinal, paged, and is what "jump to first unread" aims at (docs/06 "Lenses").
- The boundary holds while the lens is read. A bare `unread` answers as `unread:B`, with B the viewer's `last_read_ordinal`, and `unread:B` serves the posts after B for any viewer. See docs/06, under "Decided by the run (2026-10-01): the unread lens holds its boundary".
- B is `viewer.last_read_ordinal`, the integer `PageFor`'s landing, the client's `dividerBefore` and `firstUnread` (`web/client/src/keyboard/shortcuts.ts`) already use. The lens's first post is therefore `last_read_ordinal + 1`, where the canonical first-unread permalink lands. Last read rather than first unread is decided (docs/10 Q14, TKT-01M29BW9P4).
- The read beacon runs under this lens (docs/06, under "Decided by the run (2026-10-01): read tracking under a lens"). It moves the position forward only (docs/07 "Read position").
- An unread post keeps its place as a tombstone when it is removed (docs/06, "what each lens keeps of removed posts").
- Built on the framework in TKT-01M29CRJ1P, which serves a kind only once it has a resolver.

### Approach

1. **Resolver.** In `internal/service/threads/lenses.go`, add `lens.Unread(after uint32)`, whose filter keeps `ordinal > after`. The resolver handles three cases:
   - A bare `unread` for a member takes `after` from the `last_read_ordinal` that `View` already reads (`GetReadPosition`), so it costs no statement.
   - A bare `unread` for anyone who is not a member is an empty selection with value `""`, served 200.
   - `unread:B` uses B whoever asks. A shared link is a range and depends on no viewer.
2. `lens.value` is `strconv(B)` and the cache key's `Lens` is `unread:B`, so two members at different positions never share a cached page. The framework's `Selection` digest backs this up.
3. A B at or past the last post is an empty selection: one empty page, `shown` 0, `hidden` the post count.
4. Docs: none beyond the decisions already merged with this grooming.

### Tests

- `TestUnreadLensHoldsItsBoundary`: page 2 of `unread:B` is unchanged after a `PUT /threads/{id}/read` past it, and a bare `unread` asked again answers the new B.
- `TestUnreadLensStartsAtFirstUnread`: the lens's first post is `last_read_ordinal + 1`, the post `/t/{id}/{slug}/{last_read+1}`'s page anchors. With no position, it is post 1.
- `TestUnreadLensAnonymous`: an anonymous bare `unread` answers 200, kind `unread`, value `""`, `shown` 0, no posts. An anonymous `unread:B` serves the range.
- `TestUnreadLensFullyRead`: `shown` 0, one empty page, `total_pages` 1.
- `TestUnreadLensCacheSeparatesViewers`: two members with positions 10 and 20 each get their own posts, through the response cache.
- `TestUnreadLensStatements`: a bare unread page runs the canonical page's statements (db.Options.Trace counter).
- `TestLensPostsMatchCanonical` gains the unread rows: every viewer class, including the moderator by group grant and the suspended viewer, and the removed, redacted, hidden-pending-review, moved and suspended-author posts.

### Out of scope

- The client's sign-in message, empty state, address replacement and first-unread under the lens: `TKT-01M29CTY71`.
- Folding `users.mark_all_read_at` into B. Nothing sets it yet. Whichever ticket builds mark-all-read (`TKT-01M29CTY7K`, the cross-forum lists) must move the landing, the divider, first unread and this lens together, because each uses `last_read_ordinal` today.
- Jump-to-unread on the phone page picker: `TKT-01M29CGBZD`.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:04Z

run decision: pending maintainer review. The unread lens holds its boundary, as recorded in docs/06 'Lenses'. A bare ?lens=unread answers as unread:B, with B the viewer's last_read_ordinal, and the client replaces the address with that form. Re-evaluating on each page lost because page 2 would skip posts; server-held state lost because lenses are URL state. The read beacon runs under this lens and under since only. First unread aims at last_read + 1 from both views, so this ticket's 'jump lands on the right post through the canonical view as well as the lens' is satisfied by the shared integer, with the client part in TKT-01M29CTY71. Decided in the Phase 3 grooming of 2026-10-01, lane lenses.
