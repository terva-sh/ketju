---
schema: 3
id: TKT-01M3NS122FTMG03KSKHJ0NP4V0
title: Clear the search index in the hard-redaction tool
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:32Z
updated_at: 2026-10-01T09:38:45Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-search
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

- [ ] ketju redact deletes the search row of every (thread, ordinal) it redacts, copies included, before it deletes its journal, and a redacted title's row holds only its replacement (TestRedactClearsSearch)
- [ ] A search for a word found only in the redacted text returns nothing, whether the index was built before the redaction or rebuilt after it by ketju reindex --search (TestRedactClearsSearch, TestRedactThenReindexSearch)
- [ ] A run interrupted at any step and resumed ends with the same search rows, and a database without the search tables is redacted without error (TestRedactResumesSearchClear, TestRedactWithoutSearchTables)
- [ ] A database backup from before the redaction, restored afterwards, loses the redacted rows when the indexer catches up (TestRestoredBackupDropsRedactedText)
- [ ] After the command, which runs optimize and VACUUM every time, neither ketju.db nor its WAL holds the last six bytes of the redacted post's current word or of a word only in an earlier, already-replaced revision, and a control shows the earlier word was in the file before the run (TestRedactLeavesNoTermsInTheDatabase)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). `ketju redact` is done (TKT-01M29CXJZ9, PR #192, `cmd/ketju/redact.go`). It rewrites each log, then brings the thread's `.idx` current and runs `projection.Writer.ProjectThread`. It has no search index to touch, because none exists before TKT-01M29CRJ3B.

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
1. **No `secure_delete`.** An earlier plan turned it on for the run. That cannot reach pages freed before the run, which is where an earlier revision's terms are (Terva review 1719). Step 4's `VACUUM` rewrites the whole file from its live content, which covers those pages and the run's own, so the pragma would add nothing.
2. **Find the index.** `db.HasSearchTables(ctx)` reads `sqlite_schema` once. A database whose migrations stop before the search migration has nothing to clear, and every search step below is skipped. That database is reached through `migrate=false`, and `serve`'s migration later builds its index from the already-redacted logs.
3. **Bring the rows current per thread.** In `redactor.run`, after `w.ProjectThread` and the `projected:` step, call `idx.IndexThread(ctx, id, size)`. `idx` is a `search.Indexer` built with no open-thread cache, so it opens the log and `.idx` read-only. Add a `searched:<thread>` step. Catch-up reads the appended `Redacted` record, the entry's redacted flag makes the post not `Visible()`, and its row is dropped. For a title, the appended `ThreadRenamed` sets row 0 to the replacement.
4. **Clear by the journal.** Before `fsutil.Remove(journalPath)`, add `r.clearSearch(ctx, j)`: one `d.Write` with `db.DropSearchDoc` for every post target in `j.Targets`, the copy set included. For a title target, it calls `db.PutSearchDoc` for row (thread, 0) with `j.Replacement` (or the default), the thread's author and its creation time. Then `INSERT INTO search(search) VALUES ('optimize')`. After that write commits, run `VACUUM` on the write connection, outside any transaction, every time and not only when terms are found. It rebuilds `ketju.db` from its live pages, so a page freed by an earlier edit, deletion or optimize keeps nothing. Its cost is a rewrite of the database and free disk of about its size, which the usage text states; a takedown is rare and offline. Add a `vacuumed` step. Then run `PRAGMA wal_checkpoint(TRUNCATE)` on the write connection and require it to report not busy. Until a checkpoint completes, `ketju.db` itself still holds the pages as they were before, terms included. A checkpoint that does not complete fails the run with the journal kept, so a resume tries again. Only then comes the `search-cleared` step. Every part is idempotent, so a resumed run repeats it harmlessly. This holds even when the thread's `search_state` was stale or its rows wrong before the run, which catch-up alone would not fix.
5. **Dry run.** `r.report` and `printPlan` say how many search rows the run will clear, from a read, and write nothing.
6. **docs/03.** The run-decision paragraph in "Hard redaction" is this ticket's contract. The implementing PR changes it only where the code differs, and says why.

### Tests
All in `cmd/ketju/redact_test.go`, beside the existing ones, with the data directory built as `TestRedactBlanksAndMarks` builds it and the index built by the indexer before the run:
- **`TestRedactClearsSearch`.** A post holds a word found nowhere else, and also has a split copy in another thread. After `ketju redact`:
  - `views.Search` for the word returns nothing;
  - no `search_docs` row exists for any (thread, ordinal) in the journal;
  - no row exists for the post at any revision.

  The title variant redacts a title holding a unique word. The word finds nothing, and row 0 holds "Title removed".
- **`TestRedactThenReindexSearch`.** After the redaction, `ketju reindex --search` still finds nothing for the word, and the log alone indexes nothing for the post (the ticket's "should hold without special handling").
- **`TestRedactLeavesNoTermsInTheDatabase`.** The post is indexed at revision 1 holding a unique word W1, then edited by `serve`'s ordinary path to revision 2 holding W2 and not W1, so W1's row was replaced, and its pages freed, long before the redaction. After the command has closed the database, neither `ketju.db` nor `ketju.db-wal` contains the last six bytes of W1 or of W2. FTS5 stores terms prefix-compressed, so the test looks for the suffix rather than the whole word. A control asserts that W1's bytes were in `ketju.db` before the run, so the test proves the purge rather than an accident of layout.
- **`TestRedactResumesSearchClear`.** A failure at `searched:<thread>`, at `vacuumed`, at `search-cleared`, and at each step `TestRedactResumesAfterEachStep` already covers, then `--resume`, ends with the same search rows as an uninterrupted run.
- **`TestRedactWithoutSearchTables`.** A database migrated only to the migration before the search one is redacted, and exits 0.
- **`TestRestoredBackupDropsRedactedText`.** A `VACUUM INTO` backup is taken before the redaction and restored after it. The indexer's `CatchUp`, as `serve` runs it, removes the rows, and the word finds nothing.
- **`TestRedactDryRunWritesNothing`** gains the search count, and still writes nothing.

### Out of scope
- The index and the indexer: TKT-01M29CRJ3B (Build the FTS5 search index and indexer).
- `reindex --search`: TKT-01M29CTY9D (Ship ketju reindex --search), which `TestRedactThenReindexSearch` needs.
- Copies outside the data directory: backups and filesystem snapshots taken before the run, which docs/03 already says the command cannot reach.
- Quotes of the redacted post in other posts. Those posts are other people's, and the index keeps their text, quotes included, exactly as their logs do, until the operator redacts them too (docs/03 "Quotes elsewhere"). The command already lists them. The tests' unique word appears in no quote, and `TestRedactClearsSearch` also covers a quoting post: it stays found by its own words, and is listed for the operator.

## Notes

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:30Z

run decision: pending maintainer review. Recorded in docs/03 "Hard redaction" (2026-10-01): besides bringing each thread's search rows current after its swap, the command deletes every journaled (thread, ordinal)'s row and rewrites a redacted title's row before deleting the journal, with PRAGMA secure_delete on and FTS5 optimize afterwards, so the terms leave ketju.db; a database without the search tables has nothing to clear; a restored older backup heals through catch-up. Grooming also added a dependency on TKT-01M29CTY9D (Ship ketju reindex --search), which criterion 2 needs.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:38:45Z

Terva review 1719 (PR #270, head 8572a70, base a599b48), fixed on groom/p3-search-2. (high) 'Purge terms freed before secure_delete was enabled': this supersedes the run-decision note's 'with PRAGMA secure_delete on'. The command now runs optimize and then VACUUM on every run, which rebuilds ketju.db from its live pages, and drops secure_delete, which could not reach pages freed by earlier edits. TestRedactLeavesNoTermsInTheDatabase now includes a word only in an earlier, replaced revision, with a control that the word was in the file before the run (criterion 5). docs/03 'Hard redaction' updated.
