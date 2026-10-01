---
schema: 3
id: TKT-01M29CRJ3BEMZREJQRGAK3AVGK
title: Build the FTS5 search index and indexer
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29C0WF9GFGVG7QSAYRAKAA4
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:data-model-search
    path: docs/04-data-model.md
  - ref: pr:290
    path: null
  - ref: pr:296
    path: null
  - ref: pr:298
    path: null
  - ref: pr:301
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-01T12:21:50Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-run-crj3b
  name: ""
extensions: {}
---

## Description

An FTS5 table over thread_id, ordinal, revision, title, and plain source, fed from the event bus in batches.

The index is the largest object in the database, roughly the size of the plain text, and it is entirely rebuildable. Batching matters because a burst of posts should not mean a write transaction each.

Indexing plain source rather than HTML keeps markdown syntax out of the results and makes snippets readable.

## Acceptance criteria

- [x] Every connection, read-only ones included, has FTS5 through ext/fts5 with go.mod unchanged (TestEveryConnectionHasFTS5)
- [x] A burst of 200 posts is indexed in at most two write transactions, and an edit replaces the post's row rather than adding one (TestBurstIsBatched, TestEditReplacesTheRow)
- [x] The index holds plain source with markdown syntax and link destinations stripped, and no word found only in an earlier revision matches (TestPlainGolden, TestIndexIsPlainSource, TestOldRevisionTextIsNotIndexed)
- [x] Indexing incrementally and indexing each thread from its first frame give equal rows, and FTS5 integrity-check passes on both (TestIncrementalEqualsFromScratch)
- [x] Indexer.Lag reports the threads and bytes behind and the log records each transition, and an append returns while the indexer is stalled (TestLagIsReported, TestAppendDoesNotWaitForTheIndexer)
- [x] serve indexes trailing threads in the background after the listener opens, and a batch reporting dropped events is caught up (TestServeAnswersWhileSearchIndexes, TestDroppedEventsAreCaughtUp)
- [x] The search tables are db.SearchTables, not db.ProjectionTables, and reindex --projections keeps their rows (TestSearchTablesAreMarked, TestClearProjectionsKeepsSearch)
- [x] The search query binds the viewer's readable forums inside the MATCH statement, so a page's rows and length ignore forums the viewer cannot read (TestSearchFiltersForumsInTheStatement)
- [ ] A post deleted by its author, hidden by a moderator (group grant included), auto-hidden, hidden by a suspension, moved or redacted loses its row through incremental delivery and through a from-scratch index alike, a restored post's row returns at its current revision, and every record type has an index decision (TestRemovedPostsLeaveTheIndex, TestEveryRecordTypeHasAnIndexDecision)
- [x] search_state never records a length past the frames scanned, so a later record is never skipped (TestStateNeverPassesTheScan)
- [x] A thread that fails partway through a batch keeps its earlier rows and search_state while the batch's other threads commit (TestAFailedThreadKeepsItsRows)
- [x] A version change or a missing search_meta row empties the index and records the version in one transaction, which serve runs before its listener opens, so no row of another version is ever served; every thread is then re-indexed once, a restart mid-pass resumes, and a restart after it re-indexes nothing (TestVersionChangeReindexes, TestVersionChangeServesNoOldRows, TestServePreparesSearchBeforeListening)
- [x] A search_state length that is not a frame boundary re-indexes the thread from its first frame, and catch-up after a redaction swap resumes on a boundary and drops the row (TestStateNotAtAFrameBoundaryReindexesTheThread, TestCatchUpAfterARedactionSwap)

## Implementation plan

Groomed 2026-10-01 against main at a599b48 (TKT-01M3V3BVPY, Phase 3 grooming, lane `search`). Nothing of search exists yet: no FTS5 table, no indexer, and `ketju reindex --search` refuses with a usage error.

### Settled in grooming
- **FTS5 in the same database file**, over `(thread_id, ordinal, revision, title, source_plain)`, always rebuildable (docs/04 "search (FTS)", docs/04 "Engine").
- **ncruces is the driver partly for FTS5** (docs/10 item 6). The spike's recipe is `ext/fts5` and `fts5.Register(conn)` in the connection callback (`spikes/sqlite-driver/README.md` "FTS5").
- **Checked in grooming, 2026-10-01:** the main module's driver has no FTS5 without registration. A probe test on main failed with `no such module: fts5`. With `fts5.Register` on the same pinned ncruces v0.35.6, the virtual table, `MATCH`, `snippet()`, `porter`, `contentless_delete` and an external content table all worked, on SQLite 3.53.4, and `go.mod` and `go.sum` did not change. The fact is recorded in docs/04 "search (FTS)".
- **The indexer is a bus subscriber, batched** (docs/02 "Background work", docs/03 "Write path" step 5). The bus is at-least-once with a visible drop count. A subscriber that sees `Batch.Dropped > 0` catches up from durable state (`internal/events/events.go`). `Bus.Subscribe` takes a per-subscriber queue depth because of this indexer (`bus.go`).
- **sqlc cannot parse whole-table `MATCH`** (note of 2026-09-24 from TKT-01M29BW9Q1). The query is hand-written in `internal/store/db/views/`, as docs/04 already makes the rule for dynamic view queries.
- **Search never widens access** (docs/14 "Invariants, added"). At this layer, that means the index holds nothing a reader of the thread could not see. TKT-01M29CTY8T checks each hit again when it is served.
- **Run decisions of 2026-10-01, pending maintainer review**, recorded in docs/04 "search (FTS)":
  - the index holds visible posts only, at their current revision, with the title in a row of its own (ordinal 0);
  - the shape is `search_docs` as the external content table of FTS5 `search`, beside `search_state` and `search_meta`;
  - the search tables are `db.SearchTables`, apart from `db.ProjectionTables`;
  - the tokenizer is `unicode61 remove_diacritics 2`, with no stemming.

  Each is listed with its alternatives there.

### Approach
1. **FTS5 on every connection.** In `internal/store/db/driver.go`, import `github.com/ncruces/go-sqlite3/ext/fts5` and call `fts5.Register(c)` in `openPool`'s callback, before the `FCNTL_PERSIST_WAL` call. This is the one file allowed to import the driver (`TestOnlyDriverGoImportsADriver`). Every pool goes through `openPool`, the read-only one from `OpenReadOnly` included. Without the module, `ketju fsck` and a `VACUUM INTO` backup would fail on a database that has the table.
2. **Migration `0020_search.sql`.** Take the next free number at implementation. It creates three tables and one virtual table:
   - `search_docs (rowid INTEGER PRIMARY KEY, thread_id INTEGER NOT NULL, ordinal INTEGER NOT NULL CHECK (ordinal >= 0), revision INTEGER NOT NULL, author_id INTEGER NOT NULL, created_at INTEGER NOT NULL, title TEXT NOT NULL DEFAULT '', source_plain TEXT NOT NULL DEFAULT '', UNIQUE (thread_id, ordinal))`;
   - `CREATE VIRTUAL TABLE search USING fts5(title, source_plain, content='search_docs', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2')`. No `rank` is configured and nothing orders by it: bm25's statistics span the whole index, so posts in forums a viewer cannot read would reorder what they can (docs/07 "Search", Terva review 1728);
   - no triggers. `search` is kept in step with `search_docs` by the two writers of step 3 and by nothing else. Triggers lost because they would make step 3's `ClearSearch` unsafe: FTS5's `delete-all`, the one clear that works on a damaged index, followed by trigger-issued `delete`s on rows the index no longer holds, is undefined behaviour in FTS5's documentation;
   - `search_state (thread_id INTEGER PRIMARY KEY, log_length INTEGER NOT NULL, updated_at INTEGER NOT NULL)`;
   - `search_meta (id INTEGER PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL)`.

   Each plain table carries the marker comment `-- SEARCH INDEX: derived from thread logs; rebuild with ketju reindex --search`. As in 0004, no table has a foreign key.
3. **`internal/store/db/search.go`.** `SearchTables = []string{"search_docs", "search_state", "search_meta"}`.
   - **The only writers.** `PutSearchDoc(ctx, tx, doc)` and `DropSearchDoc(ctx, tx, thread, ordinal)` write `search_docs`. Before changing or removing an existing row, each issues FTS5's `delete` command with that row's old values. After writing the new row, `PutSearchDoc` inserts the new values into `search`. The plain reads and writes beneath them are sqlc queries in a new `queries/search.sql`, since none of them uses `MATCH`. `TestOnlySearchGoWritesTheIndex`, a layering test like `internal/store/db/layering_test.go`, holds every `search_docs` or `search` write to this file.
   - **`ClearSearch(ctx, tx)`** runs `INSERT INTO search(search) VALUES ('delete-all')`, then empties `search_docs`, `search_state` and `search_meta`. `delete-all` works on an index whose segments are damaged.
   - **`RebuildSearchIndex(ctx, tx)`** runs FTS5's `rebuild`, which rebuilds `search` from `search_docs` in one pass. It is `reindex --search`'s fast path (TKT-01M29CTY9D).
   - `ClearProjections` is not changed, so `reindex --projections` keeps every search row. `dbtest.DumpProjections` is not changed either; a `DumpSearch` beside it serves the equivalence tests.
4. **Plain text: `render.Plain(source string) string`** in a new `internal/render/plain.go`. It parses with the same goldmark parser and extensions as `Render`, and walks the tree:
   - it keeps text, code spans and code blocks, link text (not the destination), image alt text, mentions as `@handle`, and quote blocks' text;
   - it drops raw HTML and markdown punctuation;
   - it ends each block with a newline;
   - it removes U+E000 and U+E001, the snippet sentinels of step 7, so no post can forge a match marker.

   `render.PlainVersion` is a constant. Any change that can change `Plain`'s output bumps it, as `Version` works for `Render`, and a golden test with per-case hashes enforces this the same way. `search.Version` combines it with a schema number for the tokenizer and column set.
5. **The indexer: a new `internal/events/search` package**, shaped after `internal/events/projection`.
   - `Indexer` implements `events.Subscriber` with `Name() "search"`.
   - `Options{DB, Root, Threads, Quarantined, Logger, Now}`. `Threads` is the open-thread cache. It is only `Peek`ed, never `Acquire`d, so indexing never evicts a thread a reader has open.
   - **`IndexThread(ctx, id, logLength)`** reads `search_state` for the thread (absent means the file header's end). A recorded length past the log's end means the row is stale: search is disposable, so the thread is re-indexed from its first frame and logged, not quarantined. The same holds for a recorded length that is not a frame boundary, which `ScanFramesFrom` refuses. A hard redaction's rewrite keeps every frame at its original length, so a length recorded before it is still a boundary after it (docs/03 "Hard redaction": "The log never gets shorter"). Re-indexing on refusal covers whatever else could break that, such as a restored database paired with a log it never saw (Terva review 1728). The method walks `[from, logLength)` with `threadlog.ScanFramesFrom` and collects two things:
     - The *touched ordinals*: the ordinal of every record that can change a post's text or its `Visible()`, and nothing else changes either. Those records are:
       - `Post` and `Edit`, for text;
       - `Delete` in every form, for visibility. The docs/03 record table and `threadlog.postFlagEffect` read one record type here, with these effects:

         | `Delete` written by | Sets |
         |---|---|
         | the post's author | `EntryDeleted` |
         | a moderator's hide (`by_moderator`), whether the grant is by role or by group | `EntryHiddenByMod` |
         | the auto-hide (`auto`) | `EntryHiddenByMod` and `EntryAutoHidden`, together "hidden pending review" |
         | a suspension's "hide their posts" (`suspension`, always with `by_moderator`) | `EntryHiddenByMod` |
       - `Restore`, which clears all three;
       - a post `Redacted`, which sets `EntryRedacted`;
       - `PostsSplitOut`, whose body lists the ordinals it marks moved;
       - `PostsMergedIn`, whose body lists the ordinals it brings in.

       The indexer keeps a `decisions` table over the record catalogue, as the projection writer does: touches the frame's ordinal, touches the ordinals in the body, sets the title, or nothing, with a reason. `TestEveryRecordTypeHasAnIndexDecision` fails for a catalogued or reserved type with no decision. So a later record that changes visibility cannot be skipped silently.
     - The *last title*: from `ThreadCreated` or `ThreadRenamed`, with that frame's actor and time on `ThreadCreated`.

     It then takes the thread's index at or past `logLength`. When `Threads.Peek` has the thread, that is `th.Actor.Stable`. Otherwise it opens the log and `.idx` read-only with `threadlog.Open`, and never writes the `.idx`. Setting absolute values from the latest index converges, as projection does. For each touched ordinal:
     - an entry that is `Visible()` has its current revision read (`threadlog.Reader.One`), run through `render.Plain`, and `PutSearchDoc` with the entry's `Revision`, `Author` and `CreatedMS`;
     - any other entry is passed to `DropSearchDoc`.

     A last title, if one was seen, is put as row (thread, 0). `search_state` moves to `logLength`, the end of the frames scanned, and never past it, in the same transaction as the rows. A snapshot taken past `logLength` may already show a later edit or hide. Writing its values early is harmless, because they are absolute and a later write converges on them. Recording its length would be harmful: the frames between `logLength` and the snapshot were never scanned for touched ordinals or titles, so the next catch-up would skip them for good (Terva review 1695). The next delivery scans them and applies their values again.
   - **Batches.** `Deliver` indexes every thread in the batch in one `db.Write`. It commits and opens another at a thread boundary once the transaction holds 256 posts or 1 MiB of text, so the single write connection is never held for long. A burst is a few transactions, not one per post. Each thread in a transaction runs inside its own `SAVEPOINT`. A thread that fails partway, from a damaged frame or an unreadable log, rolls back to it, so the thread's rows and `search_state` stay as they were and the other threads in the transaction still commit. Like the projection writer, a failed thread joins a behind set that later deliveries and `Run` retry. `Deliver` returns an error only when its sweep after `Dropped > 0` fails.
   - **`CatchUp(ctx)`** compares every thread under `threads/` with its `search_state`, using the projection catch-up's directory walk (export it from `projection`, or move it to `threadid`, rather than copying it). It indexes each thread that trails and skips each quarantined one. `CatchUp` assumes `Prepare` (next bullet) has already run. `Indexing()` is true from the start of the first `CatchUp` until it returns.
   - **`Prepare(ctx)`** settles the index's version, synchronously, before anything can search it. It checks `search_meta.version` against `search.Version`. A missing row (a fresh migration, or after `ClearSearch`) counts as different. A different version is handled in one transaction: `db.ClearSearch` (FTS5 `delete-all`, and empty `search_docs`, `search_state` and `search_meta`) **and write `search_meta.version = search.Version`**. Every thread then trails and `CatchUp` re-indexes it from its first frame. No row built by another version's extraction survives the change, and because `Prepare` runs before the listener opens, no request can reach one in between (Terva reviews 1713 and 1719). Search answers from the rows the pass has rebuilt so far, with `indexing` true, as after an upgrade. Writing the version in that transaction, not at the end of the pass, means a restart mid-pass resumes thread by thread from `search_state`, and a finished pass is never repeated (Terva review 1707). With the version current, `Prepare` is one read. `ketju reindex --search` and `ketju redact` call it too, before they index anything.
   - **`Lag() Lag{Threads, Bytes}`**, with one log line when the index falls behind and one when it catches up, as `Bus` and `projection.Writer` do.
6. **Wiring in `cmd/ketju/serve.go`.** After the projection: `bus.Subscribe(indexer, 4096)`. In the startup sequence, beside the projection's catch-up and before `net.Listen`, `serve` calls `indexer.Prepare(ctx)`. A failure there fails startup, as a failed projection catch-up does. `CatchUp` then runs in a goroutine started after `bus.Start`, so the slow part never holds back the listener. Shutdown cancels it and waits, as for `proj.Run`. A mutex in the indexer serializes `IndexThread`, so `Deliver` and `CatchUp` never index one thread at once. The startup log line adds `search_indexing`. The `Indexing` and `Lag` functions are handed to the threads service in TKT-01M29CTY8T.
7. **The query: `views.Search(ctx, tx, SearchParams) ([]SearchRow, error)`** in a new `internal/store/db/views/search.go`, hand-written because of the sqlc limit above:
   ```sql
   SELECT d.thread_id, d.ordinal, d.revision, d.author_id, d.created_at, d.title,
          snippet(search, -1, char(57344), char(57345), '…', 24),
          t.slug, t.title, f.slug, f.name
   FROM search JOIN search_docs AS d ON d.rowid = search.rowid
   JOIN threads AS t ON t.id = d.thread_id JOIN forums AS f ON f.id = t.forum_id
   WHERE search MATCH ? AND t.forum_id IN (SELECT value FROM json_each(?)) AND t.post_count > 0
     [AND t.forum_id = ?] [AND d.author_id = ?] [AND d.created_at >= ?] [AND d.created_at < ?]
   ORDER BY d.created_at DESC, d.rowid DESC LIMIT ? OFFSET ?
   ```
   - The forum list is bound as one JSON parameter through `forumList`, as `UserPosts` does.
   - `t.post_count > 0` leaves out a thread with no post, as `threads.access` does.
   - `views.MatchAll(terms []string) string` builds the `MATCH` string. Each term is wrapped as an FTS5 string, with inner `"` doubled, and the terms are joined by spaces, so no operator, column filter or prefix reaches FTS5 from a reader.

   The API ticket parses `q` into terms.

### Tests
- `internal/store/db`:
  - `TestEveryConnectionHasFTS5`: the write pool, the read pool and `OpenReadOnly` each run `MATCH`.
  - `TestSearchTablesAreMarked`: the marked set equals `SearchTables`, `TestProjectionTablesAreMarked` still passes, and FTS5's shadow tables are named as the virtual table's, not as unclassified tables.
  - `TestClearProjectionsKeepsSearch`.
  - `TestClearSearchEmptiesTheIndex`: FTS5's `integrity-check` passes afterwards, including on an index whose `search_data` rows were damaged first.
  - `TestPutAndDropKeepTheIndexInStep`: random puts, replacements and drops, then `integrity-check`.
  - `TestOnlySearchGoWritesTheIndex`.
  - `dbtest`'s `TestTemplateIsAFreshMigration` still passes.
- `internal/render`:
  - `TestPlainGolden`: headings, emphasis, links (the URL is absent), code, quotes, mentions, raw HTML (dropped), and a nested list, with a per-case hash that refuses an update unless `PlainVersion` was bumped.
  - `TestPlainDropsSentinels`.
- `internal/events/search`:
  - `TestBurstIsBatched`: 200 posts queued before delivery are indexed in at most two write transactions, counted from `db.Options.Trace`.
  - `TestEditReplacesTheRow`: one row, at revision 2. Only the new text matches.
  - `TestRemovedPostsLeaveTheIndex`, a table with one row for each way a post leaves:
    - an author's `Delete`;
    - a moderator's (`by_moderator`), including a moderator holding the grant through a group (docs/08 "Decided 2026-09-30");
    - auto-hide (`auto`, hidden pending review);
    - a suspension's hide (`suspension`);
    - `PostsSplitOut`, which moves the post;
    - `Redacted`.

    Each row runs twice. The first time the record arrives through `Deliver`, in a batch of its own after the post was indexed, which is the incremental path. The second time it comes through `IndexThread` from the first frame. After each run the post has no row and its words match nothing. A `Restore` brings the row back at the current revision, also through `Deliver`.
  - `TestEveryRecordTypeHasAnIndexDecision`: every catalogued or reserved record type has an entry in the indexer's `decisions`. Each type for which `postFlagEffect` returns an effect, or which marks a post moved, is classed as touching ordinals.
  - `TestStateNeverPassesTheScan`: a snapshot is forced past the delivered length, where an edit and a hide landed after the event. `search_state` records the delivered length. The next delivery applies the later frames, and the row ends hidden.
  - `TestOldRevisionTextIsNotIndexed`: a word only in revision 1 matches nothing after the edit, incrementally and after `IndexThread` from the first frame.
  - `TestIndexIsPlainSource`.
  - `TestTitleRow`: create, rename and title redaction. The old title matches nothing after the replacement `ThreadRenamed`.
  - `TestIncrementalEqualsFromScratch`: random sequences of post, edit, delete, hide, auto-hide, restore, split, merge, rename and redact. Delivering them one batch at a time and indexing from the first frame into a fresh database give equal `DumpSearch` output, and `integrity-check` passes on both.
  - `TestDroppedEventsAreCaughtUp`.
  - `TestAFailedThreadKeepsItsRows`: in one batch with two healthy threads, a thread whose log is damaged after posts that were already put keeps its earlier rows and `search_state` unchanged. The healthy threads commit, and the failed one is reported by `Lag`.
  - `TestStateAheadOfLogReindexesTheThread`.
  - `TestStateNotAtAFrameBoundaryReindexesTheThread`: a `search_state` length inside a frame re-indexes the thread from its first frame, so no row is left over from before.
  - `TestCatchUpAfterARedactionSwap`: a database copy taken before `ketju redact` rewrites a log, then caught up against the rewritten log. The resume offset is a frame boundary, the `Redacted` record is scanned, and the row is gone.
  - `TestVersionChangeReindexes`: a different version, and a missing `search_meta` row, each re-index every thread. A restart after that pass finishes re-indexes nothing. A restart in the middle of the pass indexes only the threads it had not reached.
  - `TestServePreparesSearchBeforeListening` (`cmd/ketju`): a data directory whose index carries another version, with `CatchUp` stalled by a hook. The first search request after `ready` finds none of the old rows, and `search_meta.version` is already current.
  - `TestVersionChangeServesNoOldRows`: an index built by an earlier version whose extraction kept link destinations, then a version change with the pass stalled after one thread. A search for a link destination finds nothing. A search for a word in the unreached threads finds nothing, and `Indexing()` is true. The rebuilt thread is found.
  - `TestLagIsReported`: `Lag` while stalled, and one log line per transition.
  - `TestIndexerNeverWritesALogOrIndex`: files are opened read-only, as the projection's test holds.
  - `TestIndexerDoesNotFillTheThreadCache`: catching up more threads than the cache bound leaves `threadcache.Stats` opens unchanged.
- `internal/service/threads`: `TestAppendDoesNotWaitForTheIndexer`. A reply returns while the indexer's `Deliver` is blocked.
- `cmd/ketju`: `TestServeAnswersWhileSearchIndexes`. With catch-up stalled by a hook, `/healthz` and a thread page answer.
- `internal/store/db/views`:
  - `TestSearchFiltersForumsInTheStatement`: 30 matching posts in a forum the viewer cannot read and one in a forum they can. A `LIMIT 25` page holds the one row and nothing more, and the trace shows the forum list bound in the `MATCH` statement.
  - `TestSearchFiltersCombine`: forum, author, after and before, alone and together, with the edges included and excluded as docs/07 says.
  - `TestSearchSnippetMarksMatches`.
  - `TestMatchAllQuotesEveryTerm`: `"`, `*`, `NEAR(a b)`, `title:x`, `OR` and `^x` are matched as words and never raise an FTS5 syntax error.

### Out of scope
- The API: `q` parsing and limits, the access checks at serve time (revision, visibility, title, quarantine), the DTO and `indexing`. TKT-01M29CTY8T (Build the search API) owns these.
- The screen: TKT-01M3V70X0F (Build the search results screen).
- `ketju reindex --search`: TKT-01M29CTY9D (Ship ketju reindex --search).
- Clearing the index in `ketju redact`: TKT-01M3NS122F (Clear the search index in the hard-redaction tool).
- `Lag` on `/metrics`: TKT-01M29CEE7S (Add the metrics endpoint). `/healthz` stays as docs/02 fixes it.
- Stemming and Go tokenizers: docs/09 "Later".

### Revised at claim (2026-10-01, agent:claude-code/phase3-run-crj3b, against main at 3890b38)
The approach above stands, with the requirement carried from review 1731 (note of 2026-10-01) built in. These are the places where the build differs from it, or settles what it left open:
- **Migration 0022** was the next free number on origin/main at push time (0020 went to the blob store, TKT-01M29CXK0K, and 0021 to draft attachments, TKT-01M3V73DP6). `search_docs`'s key column is `id`, the rowid alias and FTS5's `content_rowid`, so that sqlc never models a column named `rowid`. The query joins on `d.id = search.rowid`.
- **Review 1731's requirement.** `db.DropSearchThread` deletes every row of a thread through `DropSearchDoc` and its `search_state` row. `indexThread` calls it in the thread's savepoint whenever it starts from the first frame: no `search_state`, a length past the log's end, or a length whose first frame does not parse (`errNotABoundary`). After a version reset, `ClearSearch` has already emptied everything. Both tests add a row at ordinal 99, which no log holds, and assert that it is gone.
- **The walk** moves from `projection` to `threadid.WalkLogs`. The projection's catch-up and reindex call it.
- **The transaction hook.** `db.Options.Trace` does not see the driver's BEGIN, although its comment says it does. TestBurstIsBatched therefore counts write transactions at the indexer's own per-transaction hook, not from the trace.
- **`render.Plain`** also drops attributed quotations (`[quote=#N]`) and autolinks. These are two run decisions, pending maintainer review, recorded in docs/04 "search (FTS)". `render.DropSentinels` strips the snippet sentinels from titles as well.
- **Split and merge** are reserved and have no body in this build, so the index marks no post moved. Their decision is a reason, not a touch, and TestEveryRecordTypeHasAnIndexDecision decides "touches" by applying each catalogued type to an index. The `PostsSplitOut` row of TestRemovedPostsLeaveTheIndex cannot be written in this build. TestMovedIsNotVisible holds the drop path for a moved entry instead.
- **Indexing()** is true from `New` until a `CatchUp` finishes with no thread left behind, and again from a `Prepare` that emptied the index. A catch-up that leaves threads behind leaves it true until the behind set first drains, and a thread falling behind afterwards does not set it again (Terva review 1753). A catch-up clears only the lag its snapshot covers, so a delivery that failed after its walk stays behind for Retry.
- **Run owns the sweep** (Terva review 1750). A sweep is owed from `New` until a `CatchUp` finishes, and again after a `CatchUp` fails, the dropped-events sweep included. `Run` runs `CatchUp` while one is owed, backing off from one second to a minute, and otherwise retries the threads that are behind. `serve` starts `Run` after its listener opens and does not call `CatchUp` itself. A log cut back to its header, or holding only its header with no `search_state`, is indexed from its first frame: its rows go, and its `search_state` is recorded at the header so the next catch-up passes it by (Terva reviews 1750 and 1755). `Retry` forgets a thread quarantined since it fell behind, as `Deliver` and `CatchUp` do. A result that reached its log's end clears a thread only if no later target was set after its run was asked for (Terva review 1755).
- **The test locations.** TestAppendDoesNotWaitForTheIndexer is in `internal/events/search`, through `posts.Writer` and a real bus, which is the append seam `threads.Service` uses. `cmd/ketju/search_test.go` holds TestServePreparesSearchBeforeListening and TestServeAnswersWhileSearchIndexes. Serve gains a `search` startup step and a `stop search` shutdown step.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:08:06Z

From TKT-01M29BW9Q1 (Build the database access layer and migration runner), 2026-09-24: sqlc v1.31.1 cannot parse FTS5's whole-table form `WHERE search MATCH ?` (it fails with 'column does not exist'), nor the table-valued `FROM search(?)`. It does accept `WHERE search.source_plain MATCH ?` with `ORDER BY rank`. A query across every column either goes in internal/store/db/views/ as hand-written SQL, or names one column and puts an FTS5 column filter such as `{title source_plain}: term` inside the MATCH string. The parse smoke is internal/store/db/testdata/smoke/.

**agent:claude-code/phase3-groom-search** at 2026-10-01T07:58:17Z

run decision: pending maintainer review. Recorded in docs/04 "search (FTS)" (2026-10-01): (1) the index holds only posts every reader of the thread may see, at their current revision, plus a title row at ordinal 0, so a moderator does not find hidden posts in search; (2) search_docs is the external content table of FTS5 search, with search_state and search_meta, written only through db.PutSearchDoc/DropSearchDoc (no triggers, so delete-all stays safe on a damaged index); (3) the search tables are db.SearchTables, apart from ProjectionTables; (4) tokenizer unicode61 remove_diacritics 2, no stemming. Grooming also checked that ncruces v0.35.6 needs ext/fts5's Register per connection (a probe on main failed with 'no such module: fts5' and passed with it, go.mod unchanged); that fact is in docs/04 too.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:11:25Z

Terva review 1695 (PR #270, head 2544ecc, base a599b48), both findings fixed in the plan. (high) 'Do not advance search_state past the frames actually scanned': search_state now moves to the scanned logLength, never to a newer snapshot's length; TestStateNeverPassesTheScan and criterion 10. (high) 'Account for visibility changes when collecting touched ordinals': every hide (moderator, group-grant moderator, auto-hide, suspension) is a Delete record, which the touched set already took; the plan now says so record by record against threadlog.postFlagEffect, adds a decisions table over the record catalogue with TestEveryRecordTypeHasAnIndexDecision, and runs TestRemovedPostsLeaveTheIndex through Deliver as well as from the first frame (criterion 9).

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:33:44Z

Terva review 1707 (PR #270, head 895c544, base a599b48), fixed. (medium) 'Persist the search version after catch-up': CatchUp now records search_meta.version in the same transaction that empties search_state on a mismatch (a missing row counts as one), so a finished pass is not repeated on restart and a restart mid-pass resumes per thread. docs/04 'search (FTS)' says the same; TestVersionChangeReindexes covers both restarts; criterion 11.

**agent:claude-code/phase3-groom-search** at 2026-10-01T08:50:46Z

Terva review 1713 (PR #270, head d0b6f16, base a599b48), fixed. (high) 'Do not serve old search rows during a version-change rebuild': a version mismatch now runs ClearSearch and records the new version in one transaction, so no row from another extraction version survives to be served; search answers from rebuilt rows with indexing true, as after an upgrade. docs/04 'search (FTS)' says the same; TestVersionChangeServesNoOldRows; criterion 12. This was review round 5 of 5 on PR #270, so the head carrying this fix has not been reviewed.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T09:14:08Z

Orchestrator, 2026-10-01: grooming PR #270 reached the five-round review cap (docs/13). Every finding was fixed, and the plan still has security-relevant detail that each round sharpened. The orchestrator allows one sixth review round as a recorded exception, on the head that carries this note. This commit also triggers a fresh CI run: run 1790 failed in e2e/smoke.spec.ts on a server stall unrelated to this PR, which is filed separately.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:38:45Z

Terva review 1719 (PR #270, head 8572a70, base a599b48), fixed on groom/p3-search-2. (high) 'Clear an obsolete index before search requests can reach it': the version check and atomic clear move out of background CatchUp into a synchronous Indexer.Prepare, which serve calls before net.Listen (and reindex and redact call before indexing); a failure fails startup. docs/04 'search (FTS)' says so; TestServePreparesSearchBeforeListening; criterion 12 rewritten.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:44:31Z

Terva review 1728 (PR #275, head b5fa248, base 8a2c... see PR), finding 1 (high) 'Do not resume a restored search index at an offset from a rewritten log': answered and hardened. docs/03 'Hard redaction' keeps every rewritten frame at its original length ('The log never gets shorter'), so a pre-redaction offset is still a frame boundary and catch-up scans the appended Redacted record; docs/03 now says so in the search paragraph. The plan adds a defensive rule: an offset ScanFramesFrom refuses re-indexes the thread from its first frame. Tests TestStateNotAtAFrameBoundaryReindexesTheThread and TestCatchUpAfterARedactionSwap; criterion 13. Finding 2 also removes the bm25 rank configuration from the migration (see TKT-01M29CTY8T).

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:44:40Z

Correction to the review 1728 note above: its base is f3538e6, main as PR #275 was opened; 'base 8a2c... see PR' was a typing error.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:44:51Z

Supersedes the correction above: review 1728's base is 6dfd01d, the main commit PR #275 branched from. f3538e6 is a later main commit.

**agent:claude-code/phase3-groom-search** at 2026-10-01T09:47:41Z

Requirement carried from review 1731 (2026-10-01; PR #275, head 64a8983, base 6dfd01d; medium, 'Remove old thread rows when re-indexing from the first frame'). This binds the implementing lane and overrides any plan text it contradicts. Whenever IndexThread starts from the first frame (no search_state, a recorded length past the log's end, a length ScanFramesFrom refuses, or a version reset), it first deletes every search_docs row of that thread (through DropSearchDoc, keeping FTS5 in step) and its search_state row, in the same transaction and savepoint as the scan, so an ordinal the log no longer holds cannot keep a row. TestStateNotAtAFrameBoundaryReindexesTheThread and TestStateAheadOfLogReindexesTheThread each add a row for an ordinal absent from the log, and assert it is gone. Also from the same review: reindex --search does not call Prepare separately (see the carried requirement on TKT-01M29CTY9D). Serve-time checks already drop a hit whose ordinal has no index entry, so neither gap widens access. Not fixed in the plan because PR #275 had used its two review rounds.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:02:02Z

run decision: pending maintainer review. Recorded in docs/04 'search (FTS)' (2026-10-01): render.Plain leaves out (1) an attributed quotation ([quote=#N]), whose text is another post's copy that the page drops once that post is deleted, so indexing it would let search show withheld text; a plain blockquote keeps its text; and (2) an autolink, whose text is its destination, so no URL reaches the index. Alternatives and why each lost are in docs/04.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:06:03Z

Criterion 9 is left unticked because of one word, 'moved'. Every other way a post leaves the index is tested, through Deliver and from the first frame, by TestRemovedPostsLeaveTheIndex: the author's delete, a moderator's hide by role and by group, the auto-hide, a suspension's hide and a hard redaction. The Restore case and TestEveryRecordTypeHasAnIndexDecision are tested too. A moved post cannot be produced in this build: PostsSplitOut is reserved with no body, so the actor cannot write one and the index never sets EntryMoved. TestMovedIsNotVisible holds only that a moved entry would take the same drop path. The split row belongs to TKT-01M29CXJZZ, which must add a touch decision for PostsSplitOut and PostsMergedIn; TestEveryRecordTypeHasAnIndexDecision will then require it.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:06:06Z

Found while building, filed as draft TKT-01M3VHSYFR (Write back index entries an Extended replay changes before trusting it), bug, priority high, no milestone. Reproduced on main at 3890b38. A crash after a Delete reaches the log but before its .idx entry does leaves a stale entry. The next append's header flush then marks the .idx as trusted, and the deleted post is served Visible again. The search index reads the same entries, so it would show what the thread page shows. ketju redact is not exposed, because it rewrites the whole .idx. The search test fixture does the same after its log rewrite, as redact does.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:21:45Z

Terva review 1750 (PR #290, head 23fbb20, base 6d10fe0, run 1930), both findings fixed. (high) 'Clear stale rows when a log contains only its header': indexThread now drops the thread's rows and search_state before returning for a header-only log, when it starts from the first frame. TestHeaderOnlyLogDropsItsRows covers it, and fails with the old order. (high) 'Retry a catch-up that fails before it discovers threads': the indexer now owes a sweep from New until a CatchUp finishes, and again after any CatchUp fails. Run runs CatchUp first, and keeps running it with backoff while it is owed, before it retries behind threads. serve now calls only Run after its hold. TestRunRetriesAFailedCatchUp covers a first sweep that fails, and TestRunRetriesALaterFailedSweep a dropped-events sweep that fails after one has finished. The second test catches removing the re-arm.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:40:18Z

Terva review 1753 (PR #290, head c94436d, base 6d10fe0, run 1952). It found both findings from review 1750 resolved, and raised two new findings, both now fixed. (high) 'Do not forget a newer failed delivery during catch-up': CatchUp's state-equals-size branch now calls forgetUpTo, which clears a thread only when nothing holds it past the snapshot's size. TestCatchUpKeepsANewerFailedDelivery covers this, and restoring the unconditional forget makes it fail. (medium) 'Keep Indexing true while the catch-up has failed threads': CatchUp clears indexing only when it leaves nothing behind. Otherwise, once no sweep is owed, transition clears it when the behind set first drains, and later lag never sets it again. docs/07's indexing field now says so. TestIndexingStaysTrueWhileThreadsFail covers this, and both an unconditional clear and no clear on drain make it fail.

**agent:claude-code/phase3-run-crj3b** at 2026-10-01T11:57:28Z

Terva review 1755 (PR #296, head 91980d9, base 0da0411, run 1970) raised three findings, all fixed. (high) 'Do not clear a newer failed delivery with an older indexing result': settle now clears a thread that reached its log's end only when b.target <= r.lag, meaning no later target was set after the run was asked for. A target past the log's end, left by a shorter swapped log, still clears once Retry asks for it. TestAnOlderResultKeepsANewerFailedTarget covers both halves. (medium) 'Check quarantine again when retrying a failed thread': Retry now forgets quarantined threads, as Deliver and CatchUp do; TestRetrySkipsAQuarantinedThread covers it. (medium) 'Clear orphan rows for a header-only log with no search state': CatchUp now sends a header-only log with no state through the first-frame path, which drops its rows and records search_state at the header, so later catch-ups pass it by with no transaction; a log shorter than a header is still skipped. TestHeaderOnlyLogWithoutStateDropsOrphanRows covers it, and TestHeaderOnlyLogDropsItsRows now expects state at the header. Each fix was mutated back and its test failed.

## Summary

Built to the groomed plan, with review 1731's requirement included. ext/fts5 is registered on every connection, and go.mod and go.sum are unchanged. Migration 0022 adds search_docs as the external content of FTS5 'search' (unicode61 remove_diacritics 2, no rank), with search_state and search_meta. These are db.SearchTables, apart from the projections. db.PutSearchDoc, DropSearchDoc, DropSearchThread and ClearSearch are the only writers. render.Plain and PlainVersion carry a hash-enforced golden. internal/events/search is the indexer: batched writes, one savepoint per thread, a decisions table, Prepare before listening, then CatchUp, Run, Retry and Lag. views.Search and MatchAll bind the readable forums in the MATCH statement and order newest first. serve has a 'search' startup step and a 'stop search' shutdown step. The walk moves to threadid.WalkLogs. Criterion 9 is unticked only for 'moved', which this build cannot produce (see note). There are two run decisions, pending maintainer review, in docs/04: attributed quotations and autolinks stay out of the plain text. Draft TKT-01M3VHSYFR is filed for a stale .idx after an Extended replay. The API (TKT-01M29CTY8T), reindex --search (TKT-01M29CTY9D) and redact's clearing (TKT-01M3NS122F) build on this.
