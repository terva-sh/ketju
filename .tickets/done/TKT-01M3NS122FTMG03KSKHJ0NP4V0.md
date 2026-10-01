---
schema: 3
id: TKT-01M3NS122FTMG03KSKHJ0NP4V0
title: Clear the search index in the hard-redaction tool
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - area:storage
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
  - TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
  - TKT-01M29CTY9D04J3NR7E5HH59M24
blocks_on: none
references:
  - ref: pr:317
    path: null
claim: null
archive: null
created_at: 2026-09-29T05:08:32Z
updated_at: 2026-10-01T14:33:21Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase3-search-cli
  name: ""
extensions: {}
---

## Description

The hard-redaction tool (TKT-01M29CXJZ9, Build the offline hard-redaction tool) must clear the search index's copy of a redacted post. docs/03 "Hard redaction" and that ticket's description both say so: "a body erased from the log and still returned from a cache is a redaction that did not happen".

This build has no search index. FTS5 arrives with TKT-01M29CRJ3B (Build the FTS5 search index and indexer, phase-3), so the redaction tool, planned for phase-2, has no index to clear, and its plan hands this part here. Whichever of the two lands second has to do this, and the dependencies below make that this ticket.

### What to do
- In `ketju redact`, after each log is swapped and before the journal is deleted, delete the FTS rows for every (thread, ordinal) the journal lists. Every revision's row goes, not only the current one.
- The indexer ignores a `Redacted` record's post: a catch-up or `ketju reindex --search` over a redacted log indexes nothing for it. Because the post's frames carry no source after the rewrite, this should hold without special handling. A test proves it.
- A search for a word unique to the redacted text returns nothing after the command, whether the index was built before the redaction or rebuilt after it.

## Acceptance criteria

- [x] ketju redact deletes the search row of every (thread, ordinal) it redacts, copies included, before it deletes its journal, and a redacted title's row holds only its replacement (TestRedactClearsSearch)
- [x] A search for a word found only in the redacted text returns nothing, whether the index was built before the redaction or rebuilt after it by ketju reindex --search (TestRedactClearsSearch, TestRedactThenReindexSearch)
- [x] A run interrupted at any step and resumed ends with the same search rows, and a database without the search tables is redacted without error (TestRedactResumesSearchClear, TestRedactWithoutSearchTables)
- [x] A database backup from before the redaction, restored afterwards, loses the redacted rows when the indexer catches up (TestRestoredBackupDropsRedactedText)
- [x] After the command, which runs optimize and VACUUM every time, neither ketju.db nor its WAL holds the last six bytes of the redacted post's current word or of a word only in an earlier, already-replaced revision, and a control shows the earlier word was in the file before the run (TestRedactLeavesNoTermsInTheDatabase)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). Revised at claim, 2026-10-01, against main at ca5e50c, after TKT-01M29CRJ3B (the index, #301) and TKT-01M29CTY9D (reindex --search, #313) landed. `ketju redact` (TKT-01M29CXJZ9, `cmd/ketju/redact.go`) rewrites each log, brings its `.idx` current and projects it, and does not yet touch the search index.

### Settled in grooming
- **The redaction tool must clear the index's copy** (docs/03 "Hard redaction"; this ticket's description; TKT-01M29CXJZ9's plan): "a body erased from the log and still returned from a cache is a redaction that did not happen". Whichever of the two tickets lands second does it, and the dependencies make that this ticket.
- **The command's structure stays as TKT-01M29CXJZ9 built it.** It is offline under the data-directory lock. A journal is written before any log, and the journal is deleted last. Resume runs from the journal at each named step, through the `redactOpts.fail` seam. Titles are redacted with `Redacted{title: true}` and a replacement `ThreadRenamed`. The database is opened with `openDatabase(..., migrate=false)`.
- **The index holds one row per visible post, at its current revision, and a title row at ordinal 0** (docs/04 "search (FTS)", decided by the run for TKT-01M29CRJ3B). So a redacted post has no row once the indexer has seen its `Redacted` record, and no older revision has a row of its own: an edit replaced the row in place. The pages that held an earlier revision's row were freed by that edit, during ordinary indexing, and may still hold its terms (Terva review 1719).
- **Run decision of 2026-10-01, pending maintainer review**, in docs/03 "Hard redaction", "The search index":
  - bring the thread's search rows current after each swap;
  - delete every journaled (thread, ordinal)'s row before the journal goes, and rewrite a title's row;
  - then FTS5 `optimize` and a `VACUUM`, every run, so no page of `ketju.db` keeps the text's terms, freed pages from earlier revisions included;
  - a database without the search tables has nothing to clear;
  - a restored older backup heals through catch-up.

  The alternatives are listed there.

### Approach
1. **No `secure_delete`** (Terva review 1719). The `VACUUM` in step 4 covers pages freed before the run and during it.
2. **Find the index.** `searchIndexer` reads `db.MissingSearchTables` (added by TKT-01M29CTY9D) once, right after the database opens and before the journal is looked at. All search tables missing means a database migrated before 0022: `r.search` is nil and every search step is skipped. Some missing, which only dropping one by hand produces, is refused with exit 4 before anything is touched, because the tables left may hold the text. Otherwise `r.search` is a `search.Indexer` with no thread cache, so it opens logs and `.idx` read-only.
3. **Bring the rows current per thread.** The run never calls `Prepare` (Terva review 1775): emptying an index of another version would leave every untouched thread without rows until serve's catch-up, and the journal's clear removes the redacted rows whatever the version. After `projected:<thread>`, `r.search.IndexThread(ctx, id, size)` and a `searched:<thread>` step. Catch-up reads the appended `Redacted` records: the post's entry is not `Visible()`, so its row is dropped, and a title's row 0 becomes the replacement.
4. **Clear by the journal** (`clearSearch`), after the audit row and blobs and before `fsutil.Remove(journalPath)`:
   - one `d.Write`: `db.DropSearchDoc` for every post target, the copy set and the skipped members included; for a title target, new `search.Indexer.PutTitle` writes row 0 from `j.Replacement`, with the creator and creation time from the log's first frame, exactly as the indexer would; FTS5 `optimize`; a `search-rows` step inside the transaction;
   - new `db.DB.Vacuum` (VACUUM on the write connection, outside any transaction), every run; step `vacuumed`;
   - new `db.DB.Checkpoint` (`wal_checkpoint(TRUNCATE)`), which returns `ErrBusy` when a reader holds it back, failing the run with the journal kept; step `search-cleared`.
   Every part is idempotent, so a resumed run repeats it harmlessly.
5. **Dry run.** The plan, and the pending-journal report, print `search index: N row(s) to clear or rewrite`, read only, or that there is no search index yet.
6. **Usage text** says the index is cleared, the database compacted every run, and that this needs free disk of about the database's size.
7. **docs/03** "The search index" gains an "As built" note for the three details beyond the run decision: skipped members' rows, leaving the index's version alone, and refusing partial tables.

### Tests
All in `cmd/ketju`; the new ones in `redact_search_test.go`:
- `TestRedactClearsSearch`: a post with a unique word, its copy in another thread, and a same-thread post quoting it. Afterwards the word finds nothing, neither (thread, ordinal) has a row, no search table holds the word's last six bytes (FTS5 shadow tables included), and the quoting post is still found and listed. Title variant: the title's word finds nothing, and row 0 holds "Title removed".
- `TestRedactClearsRowsCatchUpWouldMiss`: stop at `committed`, put a stale row back for the post (search_state already current, so catch-up skips it), resume: the journal's clear deletes it.
- `TestRedactThenReindexSearch`: reindex --search afterwards indexes nothing for the post.
- `TestRedactLeavesNoTermsInTheDatabase`: W1 at revision 1, edited to W2 through the indexer's ordinary path. A control shows W1's last six bytes in `ketju.db` before the run. Afterwards neither `ketju.db` nor `ketju.db-wal` holds the end of W1 or W2.
- `TestRedactResumesSearchClear`: stop at every step, the new `searched:A`, `searched:B`, `search-rows`, `vacuumed` and `search-cleared` included, then `--resume`. The rows and results match an uninterrupted run's.
- `TestRedactKeepsTheJournalWhenTheCheckpointIsBlocked`: a reader on another handle blocks the checkpoint; the run fails with `ErrBusy` and keeps the journal, and the resume finishes.
- `TestRedactKeepsAnIndexOfAnotherVersion`: over an index of version `Version-1`, the redacted word is gone, the untouched thread keeps its row, the version is unchanged, and serve's catch-up then rebuilds correctly.
- `TestRedactWithoutSearchTables`: a database whose migrations stop before 0022 is redacted, exit 0, and still has no search tables. `TestRedactRefusesPartialSearchTables`: a hand-dropped table is refused and nothing changes.
- `TestRestoredBackupDropsRedactedText`: a copy of the cleanly closed database is taken before the run and restored after it; `CatchUp` removes the row.
- `TestRedactDryRunWritesNothing` gains the search count.

### Out of scope
- The index and the indexer: TKT-01M29CRJ3B (Build the FTS5 search index and indexer).
- `reindex --search`: TKT-01M29CTY9D (Ship ketju reindex --search), which `TestRedactThenReindexSearch` needs.
- Copies outside the data directory: backups and filesystem snapshots taken before the run, which docs/03 already says the command cannot reach.
- Quotes of the redacted post in other posts. Those posts are other people's, and the index keeps their text, quotes included, exactly as their logs do, until the operator redacts them too (docs/03 "Quotes elsewhere"). The command already lists them. An attributed quotation is left out of the quoting post's row anyway (docs/04 "what the plain text leaves out").

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/03 "Hard redaction" (2026-10-01): besides bringing each thread's search rows current after its swap, the command deletes every journaled (thread, ordinal)'s row and rewrites a redacted title's row before deleting the journal, with PRAGMA secure_delete on and FTS5 optimize afterwards, so the terms leave ketju.db; a database without the search tables has nothing to clear; a restored older backup heals through catch-up. Grooming also added a dependency on TKT-01M29CTY9D (Ship ketju reindex --search), which criterion 2 needs.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:38:45Z

Terva review 1719 (PR #270, head 8572a70, base a599b48), fixed on groom/p3-search-2. (high) 'Purge terms freed before secure_delete was enabled': this supersedes the run-decision note's 'with PRAGMA secure_delete on'. The command now runs optimize and then VACUUM on every run, which rebuilds ketju.db from its live pages, and drops secure_delete, which could not reach pages freed by earlier edits. TestRedactLeavesNoTermsInTheDatabase now includes a word only in an earlier, replaced revision, with a control that the word was in the file before the run (criterion 5). docs/03 'Hard redaction' updated.

**agent:claude-code/phase3-search-cli** at 2026-10-01T14:09:07Z

Implementation decisions (recorded in docs/03 'Hard redaction', 'The search index', As built): (1) A database holding only some of the search tables (one dropped by hand) is refused with exit 4 before the journal is read or written, so nothing changes; the tables left may hold the text and the run could not say it cleared them. Alternative, treating it as 'no index' and skipping, lost because it would report a takedown complete over a table that may still hold the text. (2) The journal clear also deletes the rows of the copy set's skipped members (already redacted, or log gone), which should have none; deleting is harmless and closes the case of a log removed while its rows stayed. (3) run() calls Prepare first: an index of another version is emptied, as serve's start would, so it holds no text at all. (4) The title row is rewritten through a new search.Indexer.PutTitle, which builds row 0 exactly as the indexer does (DropSentinels, trim, creator and time from the log's first frame), so the clear and catch-up cannot write different rows. (5) The checkpoint is a new db.DB.Checkpoint that returns ErrBusy when a reader holds it back; the run then fails with the journal kept (TestRedactKeepsTheJournalWhenTheCheckpointIsBlocked). Mutations tried: no VACUUM, no optimize, no journal drops, no explicit checkpoint; each fails a test (see the commit).

**agent:claude-code/phase3-search-cli** at 2026-10-01T14:33:15Z

Terva review 1775 (PR #317, head 45f5b8a, base 902c30e; run ed6292d2), fixed. (high) 'Rebuild search rows for unaffected threads after Prepare clears the index': redact no longer calls Prepare. An index of another version keeps every row the redaction does not name, and serve's start empties and rebuilds it as it would have; the journal's clear, optimize and VACUUM remove the redacted text whatever the version. Alternative, a full CatchUp in the redact run after Prepare reset the index, lost because it makes a takedown re-index the whole forum offline for no security gain. This supersedes the implementation-decisions note's item (3). TestRedactKeepsAnIndexOfAnotherVersion covers it. docs/03 As built and the plan are updated. The first dispatch on this head (run e654c31e) ended 'superseded' because main moved during the run; it was re-dispatched.

## Summary

ketju redact clears the search index's copy of the text and every page of the database that held it. After each swap the thread's rows are brought current (searched:<thread>); before the journal goes, one transaction deletes every journaled post's row (skipped copy-set members included), rewrites a redacted title's row 0 through search.Indexer.PutTitle, and runs optimize; then VACUUM and a WAL checkpoint that must not be busy, every run (new db.DB.Vacuum, db.DB.Checkpoint). A blocked checkpoint fails the run with the journal kept. The index's version is left alone (review 1775): other threads keep their rows. No search tables: nothing to clear. Some missing: refused before anything changes. --dry-run counts the rows. Proved by searching for unique redacted words, scanning every search table (FTS5 shadow tables included) and the raw ketju.db and WAL bytes, with a control that an earlier revision's word was in the file before. All five criteria met.
