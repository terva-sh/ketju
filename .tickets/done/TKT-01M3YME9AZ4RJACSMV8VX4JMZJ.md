---
schema: 3
id: TKT-01M3YME9AZ4RJACSMV8VX4JMZJ
title: Record quotes as backlinks, as docs/06 promises
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:362
    path: null
  - ref: pr:380
    path: null
claim: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-02T23:26:31Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-9az
  name: ""
extensions: {}
---

## Description

docs/06 says, for quotes: "the server records the quote as a backlink". `internal/render/render.go:82-84` says the render's quote list is "for the projection writer to record as backlinks". Nothing does this:
- The thread log's `Post` record carries `reply_to` and `mentions`, but no quotes (docs/03 record types).
- `internal/events/projection/apply.go` writes `post_meta.reply_to` and `mentions`, and stores no quotes. No migration has a quote or backlink table.
- The notifier finds quotes by rendering the source again, with an empty `render.Context{}` rather than the create-time resolver (`internal/events/notifier/notifier.go:655-672`): "Quotes are not in the log, so the source is rendered to find them".

Found on 2026-10-02 by the check of TKT-01M29BKBM7's criterion 3 ("Mentions, quotes, and reply_to extracted at render time match what the projection writer records").

### What to do
Decide where backlinks come from. Two options:
- Re-render the source in the projection writer. The log stays as it is, and reindex can rebuild the result.
- Add a quotes field to the `Post` record. That changes the frozen format, which is docs/15 stop class 2, so it needs the maintainer.

Prefer the first option. Then:
- Record the backlinks.
- Have the notifier read them, instead of rendering again with an empty context.
- Show them wherever docs/06 draws them.

## Acceptance criteria

- [x] A migration adds the post_quotes projection table (PROJECTION marker, in db.ProjectionTables), and the projection writer records it from Post and Edit sources through render.QuotedOrdinals, skips split and merge copies, and clears it on a Redacted post; TestQuotesAreRecordedAsBacklinks passes
- [x] TestProjectedQuotesMatchTheCreateRender and TestReindexRebuildsTheBacklinks pass (TKT-01M29BKBM7 criterion 3)
- [x] The notifier reads quoted ordinals from post_quotes and no longer renders; TestQuotedReadsTheRecordedBacklinks passes, and TestStrongestKindWins, TestMuteSuppressesEveryKind and TestGeneratorSkipsInactiveAndUnreadable pass unchanged
- [x] dto.Post carries quoted_by, always an array, listing only quoting posts that are not deleted or moved in post_meta and Visible() in the index, the same for every viewer class, and empty on a post served without HTML; TestQuotedByListsOnlyVisibleQuotingPosts and TestAMovedQuotingPostIsNotABacklink pass
- [x] TestQuotedByFollowsTheProjection passes: a page drawn before the projection catches up is not served after it
- [x] render.Version is unchanged, the thread log format is unchanged (format.go, records.go untouched), docs/04 documents post_quotes and docs/07 shows quoted_by, and the catch-up bound test passes with a quotation corpus whose time a ticket note records

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e.

### Settled in grooming

- **Placement and origin.** Filed by the Phase 1 cleanup. The orchestrator's verification note on TKT-01M29BKBM7 (Markdown render pipeline and attachments), criterion 3, found quotes recorded nowhere. docs/15 "Scope" places this ticket in Phase 4. docs/09 lists "quote backlinks" under Phase 3. docs/01 "Techniques that expose structure without a tree" says docs/06 adopts them.
- **Where backlinks come from: the projection writer re-renders the source** (the lane brief, and the description's preferred option). The thread log is unchanged. The backlinks are a projection table, which `ketju reindex` rebuilds, so docs/15 stop class 2 does not apply ("A projection that reindex rebuilds is not in this class"). The alternative lost: a `quotes` field on the `Post` record. docs/15 class 2 names that change and stops it for the maintainer, and it would still leave `Edit` records and every existing log without the field.
- **What is recorded.** For each `Post` and `Edit`, the projection writer calls `render.RenderWith(source, render.Context{})`, but only when the source contains `[quote=#`, as the notifier checks today. It records each distinct ordinal in `Result.Quotes` other than the post's own. With no resolver, the renderer reports every quotation (`resolveQuotes`, `quote.go`). So the row set depends on the source alone, and a replay or reindex reproduces it exactly.

  Whether a quoted ordinal exists, and whether the quoting post is visible, is decided when the rows are read, not when they are written. That is how the read path renders: a quote resolves against the thread as it is now (`postResolver`, `view.go`).

  A quotation nested inside another counts, because `Result.Quotes` lists it and the notifier already treats it as a quote. Changing that would be a render change, outside this ticket.
- **The table: `post_quotes (thread_id, ordinal, quoted_ordinal)`**, a projection:
  - Its primary key is `(thread_id, quoted_ordinal, ordinal)`, `WITHOUT ROWID`, for the page's question "who quotes these posts".
  - An index on `(thread_id, ordinal)` serves the per-post replace.
  - It carries the `PROJECTION` marker and joins `db.ProjectionTables`.
  - It takes the next free migration number; on main that is `0025`. Check the number again at implementation, because other Phase 4 lanes add migrations.
- **How each record changes the table.**
  - A `Post` replaces the post's rows.
  - An `Edit` replaces them from its own source.
  - A `Redacted` post marker deletes them, as `applyRedacted` deletes mentions and attachments: the blanked source quotes nothing.
  - `Delete` and `Restore` leave the rows alone, and readers filter by visibility.
  - A `Post` with `Origin`, a split or merge copy, records none, as the notifier generates nothing for one. How a copy's quotations resolve in its new thread belongs to TKT-01M29CXJZZ (Implement split and merge records in the thread log).
- **The notifier reads the table** (the lane brief). `generate` reads the rows of each `Post` item from `post_quotes` and keeps the quoted ordinals below the item's ordinal, which is the rule `quoted()` applies today. It no longer renders anything.

  This is safe because the generator processes a range only up to the length the projection holds (`notifier.go` package doc), so the rows describe the range.

  One edge changes. If an `Edit` in the same unprocessed range changed the post's quotations, the generator now sees the edited set rather than the `Post` frame's. That is the post as readers see it. docs/04's decision that an `Edit` notifies only the members its `mentions` names is unchanged: an `Edit` still generates no `quoted`.
- **Run decision: how backlinks are shown** (docs/06 "Post anatomy", decided by the run 2026-10-02, pending maintainer review). The thread page's posts carry `quoted_by`: the ordinals of the posts that quote them, in ascending order. A quoting post is listed only when all three hold:
  - it has a `post_quotes` row;
  - its `post_meta` row is not deleted or moved;
  - its live index entry is `Visible()`.

  The list is the same for every viewer class, moderators included. A post the response carries without HTML has an empty `quoted_by`. The client ticket filed beside this one draws it.

  Alternatives:
  - listing removed quoting posts to moderators lost. An author also sees their own removed post, but the response cache keys that class only when the removed post is on the page, and a quoting post usually is not. So the cached posts would show one member's removed post to another.
  - a count without links lost, because a reader cannot follow a count.
- **Access** (docs/14 "Invariants, added"). A backlink is an ordinal in the same thread. A viewer who receives the page may already read the thread, so the backlink adds nothing about other threads or forums. The visibility filter means no backlink names a post the viewer is shown as a tombstone, and so it never reveals what a removed or moved post quoted. That covers docs/15's moved-post rule.
- **The response cache** (docs/13 invariant). `post_quotes` and `post_meta` are projections. The cache key already carries `Projected`, the projection's length, "so that a later field drawn from the projection cannot be pinned under a key that a lagging projection shares with a caught-up one" (`respcache.Key`). Index flags change only with the log length, which is in the key too. The ETag hashes the body, so it covers `quoted_by` with no change.
- **Display bytes.** None in this ticket. The client ticket estimates its own.

### Approach

1. Migration `00NN_post_quotes.sql` with the table, its index and the `PROJECTION` marker. Add `post_quotes` to `db.ProjectionTables`, and add the queries to `internal/store/db/queries/projection.sql`: `DeletePostQuotes`, `InsertPostQuote` (`INSERT OR IGNORE`), and `ListQuotedBy(thread_id, from, to)`. `ListQuotedBy` joins `post_meta` on the quoting post, keeps rows with `deleted = 0 AND moved = 0`, and orders by `quoted_ordinal, ordinal`. Regenerate sqlc.
2. `internal/events/projection/apply.go`: add `replaceQuotes(ctx, q, f, source string)`, called from `applyPost` (skipped when `b.Origin != nil`) and from `applyEdit`. `applyRedacted` calls `DeletePostQuotes`. Put the extraction in an exported helper `render.QuotedOrdinals(source string, self uint32) []uint32`, so the projection and any later reader share one definition. It does the substring check, the render with an empty `Context`, the self filter and the dedupe. It changes no HTML, so `render.Version` stays at 5.
3. `internal/events/notifier/notifier.go`:
   - `scan` stops calling `quoted()`, and the function is deleted.
   - `generate` reads the items' quotes from `post_quotes` in its transaction, with one new query `ListPostQuotes(thread_id, ordinals)`, and keeps `quoted < item.ordinal`.
   - The package doc's sentence on quotes changes to say they are read from the projection.
   - If nothing else in the package uses `render`, the import goes.
4. `internal/dto/dto.go`: add `QuotedBy []int` with the JSON name `quoted_by` to `Post`. It is always an array, never null. Add it to `web/client/src/api/dto.ts`, which needs no dist change because the type is erased. Update `internal/dto` goldens with `-update`.
5. `internal/service/threads/view.go` `postsDeps`:
   - Read `ListQuotedBy` for the page's ordinal range once per call.
   - Drop any quoting ordinal whose live `th.Index.Entry` is not `Visible()`.
   - Attach the result to each post the response carries with HTML.

   Lens pages, the replies expander, the single-post endpoint and `/users/{handle}/posts` all build through `posts`, so they all carry the field.
6. docs:
   - docs/04 gets a `### post_quotes` section under "Projection tables", saying what it holds, what changes it, and that the notifier reads it.
   - docs/07's thread response example gets `"quoted_by": []` with one sentence on its rule.
   - The docs/06 run decision is already written in the grooming PR.

### Tests

- `TestQuotesAreRecordedAsBacklinks` (`internal/events/projection`), table-driven over a real log. Each case asserts the `post_quotes` rows after catch-up:
  - a post quoting #1 and #2;
  - a duplicate quotation;
  - a self-quotation;
  - a quotation of an ordinal that does not exist;
  - a nested quotation;
  - an `Edit` that removes one quotation and adds another;
  - a grace `Edit`;
  - a `Redacted` post;
  - a `Delete` and a `Restore`, which leave the rows;
  - a split copy (`Origin` set), which records none.
- `TestProjectedQuotesMatchTheCreateRender` (`internal/service/threads` or `internal/web/api`): for posts created through the service with quotations of existing posts, the rows equal the create-time render's `Quotes` (`create.go`). This is TKT-01M29BKBM7's criterion 3.
- `TestReindexRebuildsTheBacklinks` (`projection/reindex_test.go`): reindex into an emptied database gives the same `post_quotes` rows as catch-up.
- `TestQuotedReadsTheRecordedBacklinks` (`internal/events/notifier`): a `quoted` notification is generated from the rows. A test that deletes the rows before generation shows that no render is involved. `TestStrongestKindWins`, `TestMuteSuppressesEveryKind` and `TestGeneratorSkipsInactiveAndUnreadable` keep passing, so `quoted` still reaches only readers.
- `TestQuotedByListsOnlyVisibleQuotingPosts` (`internal/web/api`, thread page): post 1 is quoted by visible post 3, by author-deleted post 4, by moderator-hidden post 5 and by auto-hidden post 6. Post 1's `quoted_by` is `[3]` for anon, member, moderator and post 5's author. A tombstoned quoted post has `quoted_by: []`. Run it for the canonical page, a lens page and `/posts/{ordinal}`.
- `TestQuotedByFollowsTheProjection` (`internal/service/threads`): a page drawn while the projection lags shows no backlink for the new quoting post. After catch-up, the next request (a new `Projected` in the key) shows it, and its ETag differs.
- `TestAMovedQuotingPostIsNotABacklink` (`internal/service/threads`, unit): the filter, given an index built with `threadlog.NewIndex` whose quoting entry has `EntryMoved`, drops it. No record sets `EntryMoved` yet, so this tests the filter directly. The moving-posts lane's end-to-end tests then cover it.
- Golden updates: `internal/dto` and `internal/web/api/testdata/golden/thread.*.json` gain `quoted_by`.
- The catch-up bound test (`catchup_bound_test.go`, run in CI without `-race`) still passes. Its corpus has no quotations, so add a case with one quotation in every hundredth post and record its time in a ticket note.

### Out of scope

- Drawing the backlinks in the client: the ticket filed beside this one, which depends on it.
- `ketju redact`'s own quote scan (`redactor.quotes`, `cmd/ketju/redact.go`). It reads every revision, not the current one, so the table cannot answer it.
- The Digest lens (docs/06, "Later").
- Changing whether a nested quotation counts.

## Notes

**agent:claude-code/phase4-groom-correctness** at 2026-10-02T17:28:19Z

run decision: pending maintainer review. Backlinks show as a 'Quoted by #51 #60' line between a quoted post's body and its footer, at both widths. Each link scrolls to the quoting post or opens the popover, as the reply-to indicator does, and the line ends 'and N more' past ten. A quoting post is listed only while every reader sees it as itself, so no backlink comes from a deleted, hidden, moved or redacted post, moderators included. The API serves the list as quoted_by. Recorded in docs/06 'Post anatomy', with the alternatives (a count, a footer place, under 'more', listing removed posts to moderators) and why each lost. The drawing is TKT-01M3YTHAZB (Show who quoted a post on the thread page), filed phase-4 under TKT-01M29BN367. Not a docs/15 stop class: the backlinks are a projection that reindex rebuilds, and the Post record is unchanged.

**agent:claude-code/phase4-run-9az** at 2026-10-02T22:59:43Z

Implementation notes (branch run/9az-quote-backlinks), against main at 56636cac. Where the code departs from the plan's wording, and why:

- Migration is `0025_post_quotes.sql` (0025 was still free on origin/main at push time). Primary key `(thread_id, quoted_ordinal, ordinal)` WITHOUT ROWID, index `post_quotes_quoting (thread_id, ordinal)`, a CHECK that a post never quotes itself.
- Query names: the page's read is `QuotedBy` (projection.sql) rather than `ListQuotedBy`, and takes a slice of quoted ordinals rather than a range, because the replies expander and `/users/{handle}/posts` ask about ordinals that are not contiguous. The notifier's read is `PostQuotes` (notifications.sql). Both are chunked at 500 ordinals.
- `visibleQuoting` (internal/service/threads/quotedby.go) also drops a quoting ordinal past the post count the page's snapshot was taken at, so a page never lists a post its own header says does not exist yet. The index check uses the live index, as `postResolver` does for quote tombstones.
- A removed post served with html (to a moderator, or to its author after a moderator's removal) carries `quoted_by`, as the plan says ("each post the response carries with HTML"). The listed quoting posts are visible to everyone, so this shows nothing about a removed post. A post served without html has `[]`.
- A split or merge copy clears its rows rather than skipping the call, so a replay over a reused row cannot leave stale ones.
- Tests: `TestReindexRebuildsTheBacklinks` sits in `internal/events/projection/quotes_test.go` beside `TestQuotesAreRecordedAsBacklinks`, not in reindex_test.go. `TestProjectedQuotesMatchTheCreateRender` is in `internal/service/threads/quotedby_test.go`: it renders each source through `renderSource`, which `Reply` renders with, then creates the post through `Reply` and compares the rows. `TestQuotedByListsOnlyVisibleQuotingPosts` (internal/web/api) covers author-deleted, moderator-hidden, auto-hidden, redacted and moved (sidecar flag) quoting posts, for anon, member, moderator and the hidden post's author, on the canonical page, its cached copy, a lens page and `/posts/1`. `randomRecord`'s Edits now quote the post above, so the replay-equivalence and reindex property tests cover post_quotes.
- Catch-up bound: the no-drift bound renders nothing, so quotations cannot move it. `TestCatchUpTenThousandUnderBound` now also catches up 10000 posts over 100 threads from nothing, once with none quoting and once with 100 quoting (one in a hundred), and holds the quoting run to twice the plain one plus a second. Projecting 10000 posts takes over 2 s on this shared machine with no quotations at all, so the 2 s bound could not apply to it. Measured locally (2026-10-02, 12-core shared host, other lanes running): no drift 53-94 ms; plain corpus 2.59 s; quoting corpus 2.74 s (+6%).
- Mutations tried: dropping the `Visible()` check in `visibleQuoting` fails TestQuotedByListsOnlyVisibleQuotingPosts (the moved #8 appears) and TestAMovedQuotingPostIsNotABacklink; dropping the notifier's `quoted < ordinal` filter fails TestQuotedReadsTheRecordedBacklinks.
- `web/client/src/api/dto.ts` gains `quoted_by: number[]`. `go generate ./internal/web/spa` leaves the committed dist byte-identical (type-only change), tsc and the 2566 Vitest tests pass. The drawing is TKT-01M3YTHAZB.

**agent:claude-code/phase4-run-9az** at 2026-10-02T23:26:31Z

Terva review 1998 (PR #380, head 207be9de, base ee77e54e, run 67cb0fd9-f3a6-4f2c-a74d-b0bddd1c01aa, Actions #2549): one medium finding, "Backfill quotes for already-projected threads when upgrading". Accepted and fixed.

The finding is right: migration 0025 created an empty table, and nothing would replay the Post and Edit records of a database projected before it, so quoted_by would miss every older quotation until a reindex, and the notifier would miss a quote in a post projected but not yet generated.

Fix: 0025 now ends with `UPDATE projection_state SET log_length = 64 WHERE log_length > 64`, which makes the next start's catch-up replay every log from the end of its header over the rows already there. That is safe because every apply sets absolute values and writeAudit writes one row per record (AuditExistsForRecord), and the generator processes a thread only up to the projection's length, so it waits for the replay. The rows are kept rather than deleted so a projection whose log is gone still classifies as corrupt.

Alternatives considered:
- Deleting projection_state: also replays, but loses the row that makes a missing log classify as corrupt (Classify, fsck).
- A Go backfill job that scans only Post and Edit frames: avoids replaying everything, but is a second writer of a projection table outside apply and reindex, with its own progress and crash story.
- Telling operators to run `ketju reindex`: a manual step on upgrade, and reindex needs the server stopped.
The cost of the chosen fix is one replay of every log at the first start after upgrading; docs/04 "post_quotes" says so.

Tests: TestPostQuotesMigrationRewindsTheProjection (internal/store/db: 0025 rewinds every projection_state row, keeps them, and leaves notification_progress alone), TestCatchUpBackfillsTheBacklinksAfterTheMigration (internal/events/projection: three projected threads, post_quotes emptied and projection_state rewound as 0025 does, catch-up ends on exactly the rows a projection that always had the table made, with no extra audit rows), and TestQuotedReadsTheRecordedBacklinks/"projected before the table existed" (internal/events/notifier: a post projected but not generated before the upgrade gives quoted after it).

## Summary

Quotes are recorded as backlinks in a post_quotes projection (migration 0025) that the projection writer fills from each Post and Edit source through render.QuotedOrdinals, and that reindex rebuilds; the thread log is unchanged. The notifier reads quoted from it instead of rendering. dto.Post carries quoted_by, listing only quoting posts every reader sees as themselves (post_meta not deleted or moved, index entry Visible()), empty on a post served without html. docs/04 and docs/07 describe both. Every criterion met; TKT-01M29BKBM7 criterion 3 is met once this merges. The client drawing is TKT-01M3YTHAZB.
