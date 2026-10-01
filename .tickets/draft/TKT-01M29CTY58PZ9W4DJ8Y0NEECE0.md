---
schema: 3
id: TKT-01M29CTY58PZ9W4DJ8Y0NEECE0
title: Implement the author lens and the only-OP button
type: task
status: draft
status_reason: null
priority: normal
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
blocks_on: none
references:
  - ref: design:lenses
    path: docs/06-thread-views.md
  - ref: pr:293
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T11:18:01Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-groom-lenses
  name: ""
extensions: {}
---

## Description

`?lens=author:handle` filtering on the index's author_id, with "Only OP" as a one-click button that applies it for the thread author. Question 15 in docs/10 leans toward the button living in the thread header, and this ticket settles it.

The header shows "showing 7 of 212 posts", which is the pattern every lens uses so nobody loses track of what they are not seeing.

No database query. The author id is in the index entry, which is the concrete payoff for the 40-byte entry decision.

## Acceptance criteria

- [ ] The author filter reads the in-memory index alone: an author-lens page runs the canonical page's statements plus one handle lookup, the same count on a 10-post and a 2000-post thread (db.Options.Trace)
- [ ] ?lens=author:<handle> keeps the author's posts shown as themselves, matching the handle a member holds now in any case; an unknown handle answers as a handle with no posts here, and a released handle is not followed
- [ ] The thread header carries author on every ThreadHeader answer, at no extra statement on the thread page, so the Only <handle> button TKT-01M29CTY71 draws builds the same URL as the author lens; the goldens show it
- [ ] An author whose every post here was removed yields shown 0 and one empty page, never an error
- [ ] TestLensPostsMatchCanonical gains the author rows for every viewer class and removed state, a suspended author's hidden posts among them

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`).

### Settled in grooming

- `?lens=author:<handle>` filters on the index entry's author id, with no read of the log (docs/06 "Lenses"; docs/03 "Index entry", the 40-byte entry measured at 15 us against 19.8 ms). `Index.ByAuthor` already exists.
- The author lens keeps only the author's posts shown as themselves, and an author with none is an empty lens (docs/06, under "Decided by the run (2026-10-01): what each lens keeps of removed posts").
- The handle is the one a member holds now, matched in any case (`users.handle COLLATE NOCASE`, `dbq.GetUserByHandle`). A handle nobody holds answers as a handle with no posts here, and a released handle is not followed (docs/07, under "Decided by the run (2026-10-01): the `lens` parameter").
- "Only OP" is a button on the thread header, "Only <handle>", beside the lens switcher (docs/10 Q15, decided by the run 2026-10-01; docs/06, under "the lens bar"). The client draws it in TKT-01M29CTY71. This ticket gives it the handle.
- Built on the framework in TKT-01M29CRJ1P.

### Approach

1. **Resolver.** In `internal/service/threads/lenses.go`, add `lens.Author(id uint64)`, whose filter keeps `e.Author == id && e.Flags.Visible()`. The resolver reads `dbq.GetUserByHandle` once, before the snapshot. When no account holds the handle, it uses a filter that keeps nothing. Either way the answer echoes the handle as typed in `lens.value`, so its shape says nothing about whether the handle exists. The cache key's `Lens` is `author:<user id>`, or `author:-` when no account matched. That is safe because the response cache holds only the posts: `lens.value` is built on every request outside the cached body (TKT-01M29CRJ1P step 6), so two spellings of one handle share cached posts and each answer still echoes its own spelling.
2. **The thread header's author.**
   - Add `Author dto.Author` (`author`) to `dto.ThreadHeader`, the thread's starter from `threads.author_id`, drawn as every author block is (`member.Author`, and `member.FillMissing` for an account the database lacks).
   - On the thread page, add `row.AuthorID` to the set the page's existing `s.users` batch reads, so the page runs no extra statement.
   - Every other route that answers a `ThreadHeader` reads it the same way: `CreatedThread`, `PATCH /threads/{id}`, move, lock, pin and slow mode.
   - Regenerate the dto and api goldens (`thread.{anon,member,moderator,author}.json` and the moderate goldens).
3. **Deleted members.**
   - An anonymized starter reads as `deleted-user-<id>`, and its `author:` lens resolves through the users row that carries that handle.
   - A starter the database lacks is drawn with `member.Missing`'s placeholder, `deleted-user-<id>`, which no row holds (docs/04 "Projection tables"). When no account holds the handle and it has exactly that form, the resolver reads whether a users row has that id, one more statement on this path only. When none does, it selects author id `<id>` from the index. Only OP therefore shows a missing starter's posts.
   - When a row has that id, the handle answers as unknown, so `deleted-user-<id>` never reaches a live account's posts. The answer reveals nothing the page does not: a missing author's posts are already drawn under that placeholder, and an id with no posts here is an empty lens either way.

### Tests

- `TestAuthorLensFromIndex`: an author lens page runs the canonical page's statements plus the one handle lookup, the same count on a 10-post and a 2000-post thread (db.Options.Trace, the `statements` counter from `me_test.go`).
- `TestAuthorLensKeepsVisiblePosts`: deleted, hidden, hidden-pending-review, moved and redacted posts by the author are left out of `shown`, and `hidden` is the post count minus `shown`.
- `TestAuthorLensValueNotCached`: `author:Mara` then `author:mara`, and two different unknown handles, each answer with their own `lens.value` while the second request is a response-cache hit.
- `TestAuthorLensCaseInsensitive`, and `TestAuthorLensUnknownHandleAnswersAsNoPosts`: an unknown handle and a handle with no posts here produce byte-equal bodies apart from `value`. A handle a rename released is not followed.
- `TestAuthorLensEmptyAfterModeration`: an author whose every post here was hidden by a suspension gets `shown` 0, one empty page, 200.
- `TestAuthorLensMissingStarter`: on a thread whose starter the database lacks, `author:deleted-user-<id>` from the header selects their visible posts. `deleted-user-<id>` naming a live account's id answers as an unknown handle.
- `TestThreadHeaderCarriesAuthor`: present on every `ThreadHeader` answer, with an anonymized starter and a missing starter drawn as deleted members. The page's statement count is unchanged.
- `TestLensPostsMatchCanonical` gains the author rows. They cover every viewer class, the moderator by group grant and the suspended viewer included, and every removed state, including a suspended author's posts hidden by the suspension, which are absent for every viewer.

### Out of scope

- The "Only <handle>" button, the lens header's "showing N of M", and the empty state: `TKT-01M29CTY71`.
- Following a released handle to its member: not planned. docs/07 refuses a stranger that answer.

## Notes

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T07:51:08Z

run decision: pending maintainer review. docs/10 Q15 is settled: Only OP is a button on the thread header, 'Only <handle>', beside the lens switcher, as the mockups draw it (docs/10 Q15, docs/06 'Lenses'). The author lens keeps only posts shown as themselves, and resolves the handle a member holds now; an unknown handle answers as one with no posts, and a released handle is not followed (docs/07). The button itself moves to TKT-01M29CTY71, so the dist changes in one client PR. This ticket adds the thread header's author field that the button needs. Decided in the Phase 3 grooming of 2026-10-01, lane lenses.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T08:13:59Z

Review 1692 (head 4065cf7, base a599b48), medium, 'Keep the requested author handle distinct in the response cache': answered in the plan, with a test added. The response cache holds only the posts. dto.Lens, including the echoed value, is built on every request outside the cached body (docs/07 'The response cache'; TKT-01M29CRJ1P step 6 now says so). A shared key for two spellings therefore shares only identical posts. TestAuthorLensValueNotCached proves each answer echoes its own spelling on a cache hit.

**agent:claude-code/phase3-groom-lenses** at 2026-10-01T09:22:53Z

Review 1714 (head 656ce4e, base a599b48), medium, 'Preserve a working Only OP link when the starter account is missing': fixed. A handle of the form deleted-user-<id> that no account holds selects author id <id> when no users row has that id. That costs one statement on this path only. The handle never reaches a live account's posts (docs/07, TestAuthorLensMissingStarter).
