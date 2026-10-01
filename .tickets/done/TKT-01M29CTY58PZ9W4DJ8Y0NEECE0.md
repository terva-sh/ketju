---
schema: 3
id: TKT-01M29CTY58PZ9W4DJ8Y0NEECE0
title: Implement the author lens and the only-OP button
type: task
status: done
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
updated_at: 2026-10-01T13:10:36Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-lenses
  name: ""
extensions: {}
---

## Description

`?lens=author:handle` filtering on the index's author_id, with "Only OP" as a one-click button that applies it for the thread author. Question 15 in docs/10 leans toward the button living in the thread header, and this ticket settles it.

The header shows "showing 7 of 212 posts", which is the pattern every lens uses so nobody loses track of what they are not seeing.

No database query. The author id is in the index entry, which is the concrete payoff for the 40-byte entry decision.

## Acceptance criteria

- [x] The author filter reads the in-memory index alone: an author-lens page runs the canonical page's statements plus one handle lookup, the same count on a 10-post and a 2000-post thread (db.Options.Trace)
- [x] ?lens=author:<handle> keeps the author's posts shown as themselves, matching the handle a member holds now in any case; an unknown handle answers as a handle with no posts here, and a released handle is not followed
- [x] The thread header carries author on every ThreadHeader answer, at no extra statement on the thread page, so the Only <handle> button TKT-01M29CTY71 draws builds the same URL as the author lens; the goldens show it
- [x] An author whose every post here was removed yields shown 0 and one empty page, never an error
- [x] TestLensPostsMatchCanonical gains the author rows for every viewer class and removed state, a suspended author's hidden posts among them

## Implementation plan

Planned 2026-10-01 against main at a599b48 (Phase 3 grooming, lane `lenses`). Revised at claim on 2026-10-01 against main at 2e937eb7, after the framework (TKT-01M29CRJ1P, #299), the unread lens (TKT-01M29CTY4J, #304) and the thread head (TKT-01M3S3ZF6J, #303) merged.

### Settled in grooming

- `?lens=author:<handle>` filters on the index entry's author id, with no read of the log (docs/06 "Lenses"; docs/03 "Index entry", the 40-byte entry measured at 15 us against 19.8 ms). `Index.ByAuthor` already exists.
- The author lens keeps only the author's posts shown as themselves, and an author with none is an empty lens (docs/06, under "Decided by the run (2026-10-01): what each lens keeps of removed posts").
- The handle is the one a member holds now, matched in any case (`users.handle COLLATE NOCASE`, `dbq.GetUserByHandle`). A handle nobody holds answers as a handle with no posts here, and a released handle is not followed (docs/07, under "Decided by the run (2026-10-01): the `lens` parameter").
- "Only OP" is a button on the thread header, "Only <handle>", beside the lens switcher (docs/10 Q15, decided by the run 2026-10-01; docs/06, under "the lens bar"). The client draws it in TKT-01M29CTY71. This ticket gives it the handle.
- Built on the framework in TKT-01M29CRJ1P.

### What changed since grooming

TKT-01M3S3ZF6J (#303) has already added `author` to `dto.ThreadHeader`, the thread's starter, on every route that answers a header, with the goldens. It reads the starter with a `GetUser` of its own, one more statement on the thread page. This ticket keeps the field and its goldens, and moves the thread page's read into the page's existing `UsersByIDs` batch, which AC3 asks for.

### Approach

1. **Filter.** `lens.ByAuthor(id uint64)` keeps `e.Author == id && e.Flags.Visible()`. It is not named `lens.Author`, because that is the kind constant.
2. **Resolver.** `resolveAuthor`, the `lens.Author` row of `lensResolvers`:
   - It reads `dbq.GetUserByHandle` once, before the snapshot.
   - When no account holds the handle, it uses a filter that keeps nothing.
   - Either way the answer echoes the handle as the grammar reads it (`lens.Parse` lowercases ASCII) in `lens.value`, so its shape says nothing about whether the handle exists.
   - The cache key's `Lens` is `author:<user id>`, or `author:-` when no account matched. That is safe because the response cache holds only the posts. `lens.value` is built on every request outside the cached body, so two spellings of one handle share cached posts, and each answer still echoes its own spelling.
3. **The thread header's author on the thread page.** `View` adds `row.AuthorID` to the ids the page's `s.users` batch reads and takes the starter from it, so the header costs no statement. The posts are drawn from, and keyed by, the page's own authors only. `threadHeader` (the moderation answers) keeps its `starterOf` read.
4. **Deleted members.**
   - An anonymized starter reads as `deleted-user-<id>`. Its `author:` lens resolves through the users row that carries that handle.
   - A starter the database lacks is drawn with `member.Missing`'s placeholder, `deleted-user-<id>`, which no row holds (docs/04 "Projection tables"). When no account holds the handle and it is exactly `domain.DeletedMemberHandle(id)` for some id above 0, the resolver reads whether a users row has that id. That is one more statement, on this path only. When no row does, it selects author id `<id>` from the index, so Only OP shows a missing starter's posts.
   - When a row has that id, the handle answers as unknown, so `deleted-user-<id>` never reaches a live account's posts. The answer reveals nothing the page does not: a missing author's posts are already drawn under that placeholder, and an id with no posts here is an empty lens either way.

### Tests

- `TestAuthorLensFromIndex`: an author lens page runs the canonical page's statements plus the one handle lookup, with the same count on a 10-post and a 2000-post thread, for an anonymous viewer and a member (db.Options.Trace, the `statements` counter from `me_test.go`).
- `TestAuthorLensKeepsVisiblePosts`: on `accessThread`, the author's deleted, hidden, hidden-pending-review, suspension-hidden, moved and redacted posts are left out of `shown`, and `hidden` is the post count minus `shown`.
- `TestAuthorLensValueNotCached`: `author:Carol` then `author:carol`, and two different unknown handles. Each second request is a response-cache hit and echoes its own `lens.value`. The grammar lowercases, so the two spellings of one handle both echo `carol`.
- `TestAuthorLensCaseInsensitive`.
- `TestAuthorLensUnknownHandleAnswersAsNoPosts`: a handle nobody holds, a member with no posts here, a suspended member whose posts were hidden, a pending applicant, and a handle a rename released all answer byte-equal bodies apart from `value`, with the same status, to anonymous, member and moderator viewers. The renamed member is found by the handle they hold now.
- `TestAuthorLensEmptyAfterModeration`: an author whose every post here was hidden by a suspension gets `shown` 0, one empty page, 200.
- `TestAuthorLensMissingStarter`: on a thread whose starter the database lacks, `author:deleted-user-<id>` from the header selects their visible posts. `deleted-user-<id>` naming a live account's id answers as an unknown handle, and so do the malformed `deleted-user-0778`, `deleted-user-0` and `deleted-user-`. An anonymized author's placeholder selects their posts.
- `TestThreadHeaderAuthorCostsNoStatement`: a page the starter wrote nothing on and a page of their own posts each run one users lookup and the same number of statements, and both headers name the starter. The existing `TestThreadHeaderNamesItsStarter` and the goldens hold the field.
- `TestLensPostsMatchCanonical` gains the author rows for every author in the thread, an upper-case spelling, a member with no posts and a handle nobody holds. They cover every viewer class, the moderator by group grant and the suspended viewer included, and every removed state, including a suspended author's posts hidden by the suspension, which are absent for every viewer.

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

## Summary

The author lens is served. lens.ByAuthor(id) keeps that author's visible posts, and resolveAuthor is the author row of lensResolvers. It runs one GetUserByHandle, which is case-insensitive through COLLATE NOCASE. An unknown, released, pending, suspended or post-less handle answers the same body apart from the echoed value, so the lens cannot be used to probe handles. deleted-user-<id> selects a missing author's id only when no users row has that id. Cache key: author:<id>, or author:- when the handle names nobody. The header's author field was already added by TKT-01M3S3ZF6J (#303), at one extra GetUser statement. The thread page now reads the starter inside its UsersByIDs batch, so the header costs no statement, and keys the posts by the page's own authors. docs/07 says so. TestLensPostsMatchCanonical gained the author rows. The Only <handle> button is TKT-01M29CTY71.
