---
schema: 3
id: TKT-01M29BW9Q17KEBMHJ91G6QHW36
title: Build the database access layer and migration runner
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:db
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BSPE8J56YC8CME7N8NC1W
blocks_on: none
references:
  - ref: design:data-model-engine
    path: docs/04-data-model.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-24T07:08:06Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`internal/store/db`: opening the database in WAL mode, the connection pool capped at 4 to 8, `busy_timeout` set explicitly rather than left to chance, and writes wrapped in `BEGIN IMMEDIATE` so a write transaction never upgrades mid-flight into a busy error.

Plus the migration runner that every schema ticket depends on: ordered, forward-only, recorded in the database, and refusing to start against a database newer than the binary.

SQL stays driver-agnostic. That constraint is the mitigation for the ncruces pre-1.0 churn risk in docs/09, and it only holds if it is enforced from the first query.

## Acceptance criteria

- [x] The database opens in WAL mode with an explicit busy_timeout, verified by a test that reads back the pragmas
- [x] Concurrent writers hit BEGIN IMMEDIATE and serialize rather than failing with SQLITE_BUSY
- [x] Migrations run in order, are recorded, and are idempotent on a second start
- [x] A binary older than the recorded schema version refuses to start with a clear message
- [x] sqlc is configured with the migration directory as its schema source, its version is pinned, and generated code is committed
- [x] CI regenerates the sqlc output and fails when it differs from what is committed
- [x] A test or lint fails if a package outside the access layer imports a SQLite driver directly
- [x] Before the first authoritative write, the opener fsyncs the data directory after the WAL file exists, working around ncruces skipping the directory fsync (TKT-01M392DP19); a test proves the directory sync is issued

## Implementation plan

Revised at claim, 2026-09-24, against main at 9aa4cf1 after TKT-01M29BSPE8 merged. Nothing under internal/ touches SQL yet.

### Changes from the pre-groom plan
- **sqlc runs under `go run` with CGO_ENABLED=0.** v1.31.1 parses SQL through WebAssembly (wasilibs), which was checked locally. No pinned binary is needed, and CI needs no cgo for it.
- **synchronous=FULL, not NORMAL.** In WAL mode NORMAL can lose the latest commits on power loss, and this file holds authoritative tables. BSPE8 measured FULL at about 0.6 ms per commit on a desktop NVMe, so a single writer still manages more than 1,000 commits a second. Settled here as an internal default and written into docs/04.
- **Directory-fsync workaround (the new AC8).** Every connection sets `FCNTL_PERSIST_WAL`, so SQLite never deletes the `-wal` file on last close. The opener creates the WAL by setting journal_mode=WAL on the write connection, confirms the file exists, and fsyncs the data directory. Only then does Open return, and migrations run after it. A package variable `syncDir` lets a test record the call and assert that the `-wal` file already existed when it ran.
- **Write connection.** The write pool has MaxOpenConns=1, and `Write` runs `BEGIN IMMEDIATE` through `_txlock=immediate`. The read pool has 4 connections by default, clamped to 4–8, and is opened with `_pragma=query_only(1)`, so a stray write through `Read` fails instead of contending.
- **Busy errors.** One helper in `driver.go` recognises the driver's SQLITE_BUSY and wraps it as `db.ErrBusy`. `internal/problem` maps `ErrBusy` and `ErrSchemaNewer` to CodeUnavailable.
- **The sqlc parse smoke is kept live, not run once.** `sqlc.yaml` has a second entry over `internal/store/db/testdata/smoke/`. Its schema uses CHECK, a trigger with RAISE, an expression index, WITHOUT ROWID, a partial index and FTS5. The CI regen-diff step regenerates it too, so a sqlc upgrade that breaks one of those constructs fails CI. Go ignores `testdata/`, so the output never ships.

### Unchanged
The layout from the Files section; the migration runner (embedded FS, numeric order, refuses gaps, duplicates and checksum drift, forward-only, `ErrSchemaNewer`, one transaction per migration); the `views/` rule; the layering test (only `driver.go` imports a SQLite driver); `Close` runs `wal_checkpoint(TRUNCATE)`; and the tests listed for AC1–AC4.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:08:51Z

Decided with the user on 2026-09-24, during Phase 1 grooming: sqlc, with hand-written database/sql for the dynamic view queries. This answers the 2026-09-16 comment above, which set out the choice and verified that FTS5 parses and that the timestamp issue does not apply.

How the split works:
- sqlc generates typed database/sql code from .sql files and reads the migration directory as its schema.
- Its output is committed, and CI regenerates it and fails on any diff.
- Lens-filtered and cursor-paged view queries (docs/06) are hand-written database/sql beside the generated set. The rule is written into docs/04, so mixing the two is a decision rather than a discovery.
- The migration runner stays hand-rolled, as this ticket already says.

Two alternatives lost:
- Hand-written database/sql throughout would leave driver-agnosticism to review.
- sqlc for every query would force the lens queries into fixed variants.

Criteria added below for the pieces the decision creates.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T06:51:22Z

From TKT-01M29BSPE8 (Decide the pure-Go SQLite driver), 2026-09-24: step 2's single-connection write pool is required, not optional. With 4 to 8 writer connections contending, ncruces commits starved for seconds without returning SQLITE_BUSY, because its busy handler retries at random with no fairness. docs/04 'Engine' now says writes go through one connection. The spike also proved the DSN keys: _txlock=immediate and _pragma=journal_mode(wal)/busy_timeout(N)/foreign_keys(on) work the same on both drivers (spikes/sqlite-driver TestSharedDSNIsHonoured). Recognise SQLITE_BUSY in exactly one helper, because the error types differ between drivers. TKT-01M392DP19 covers the ncruces directory-fsync bug.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T07:08:06Z

### What the build found, 2026-09-24

- **sqlc v1.31.1 runs under `go run` with CGO_ENABLED=0.** It parses through WebAssembly (wasilibs). The CI regen step sets CGO_ENABLED=0 so it keeps proving this. No pinned binary.
- **sqlc parse smoke** (`internal/store/db/testdata/smoke/`, regenerated by CI):
  - These parse: CHECK constraints, WITHOUT ROWID, an expression index, a partial index, `CREATE TRIGGER ... BEGIN SELECT RAISE(ABORT, ...); END`, and `CREATE VIRTUAL TABLE ... USING fts5`. The pre-groom open question for BYYHG and BYYJ0 is closed: triggers can go in ordinary migrations.
  - FTS5 queries: `WHERE tbl.col MATCH ?` and `ORDER BY rank` parse. The whole-table `WHERE tbl MATCH ?` fails ("column does not exist"), and so does the table-valued `FROM tbl(?)`. See the note on TKT-01M29CRJ3B.
- **synchronous=FULL, not NORMAL.** Settled as an internal default and stated in docs/04. NORMAL in WAL mode can lose the latest commits on power loss.
- **Pending migrations apply in one write transaction**, not one each as the plan said. If a later migration fails, the earlier ones from the same run roll back too, so the schema never sits between two binaries. TestFailedMigrationLeavesNothing covers it.
- **The directory-fsync workaround (AC8):** every connection sets FCNTL_PERSIST_WAL, and Open fsyncs the data directory once the -wal file exists and before it returns. Migrate runs after Open, so no authoritative write can come first. The test swaps `syncDir`, asserts that the WAL existed at the moment of the sync, and asserts that the WAL survives Close.
- **Read pool:** `query_only(1)`, so a write through Read fails at once.
- **Flaky test, unrelated:** `threadlog.TestQueuedAppendsShareOneFsync` failed once in a full suite run and 10 times in 600 under stress. The test's premise is wrong, not the actor. Filed as the draft TKT-01M393S6P7 and left out of this PR to keep it in scope.

### Mutations tried, all caught
- deferred BEGIN on the write pool: busy failure in TestConcurrentWritersSerialize ("two handles")
- synchronous NORMAL: TestOpenSetsPragmas
- read pool without query_only: TestOpenSetsPragmas
- directory sync before the WAL exists: TestOpenSyncsDirectoryAfterWAL
- persist-WAL off: TestOpenSyncsDirectoryAfterWAL (the WAL is gone after Close)
- newer-schema check removed: TestNewerSchemaRefused
- checksum not compared: TestMigrateRefusesChecksumDrift
- busy never recognised: TestBusyIsWrapped
- a driver imported in views/: TestOnlyDriverGoImportsADriver
- a query added without regenerating: the CI regen-diff step, run locally, fails

## Comments

**agent:claude-code/phase1** at 2026-09-16T18:40:07Z

The access layer needs a decision this ticket does not yet record: how Go
reaches the tables. Nothing in docs/04, docs/09, docs/01 or any area:db ticket
names a tool, so "SQL stays driver-agnostic" is currently a rule with no
mechanism behind it.

Raised by the repository owner: use sqlc, generating from .sql files.

### Why it fits the constraint this ticket already states

sqlc's SQLite output targets database/sql. Both ncruces and modernc ship a
database/sql driver, so the "modernc is a drop-in" mitigation in docs/09 stops
depending on a reviewer noticing a driver-specific call and becomes something
the generator cannot emit. That is the difference between a documented rule and
an enforced one.

sqlc does not run migrations, which suits this ticket rather than conflicting
with it: the ordered, forward-only, recorded runner stays ours, and sqlc reads
the migration directory as its schema source.

### What was checked rather than assumed

- FTS5 parses. sqlc-dev/sqlc#1797 is closed-completed, so a
  `CREATE VIRTUAL TABLE ... USING fts5` statement in the schema path is not the
  blocker older write-ups describe. Worth confirming against the real search
  schema in docs/04 before committing.
- The known SQLite timestamp trap does not apply here. sqlc maps DATETIME and
  TIMESTAMP to time.Time, which fails against drivers storing epoch integers
  (sqlc-dev/sqlc#3736). docs/04 already specifies unix-millisecond INTEGER
  columns, so the trap is closed by a decision already made.

### The honest costs

- Dynamic queries are sqlc's weak point, and the lens-filtered thread views
  with cursors are exactly that shape. `sqlc.slice()` covers IN-lists; some
  view queries may still want hand-written SQL beside the generated set. That
  is a mixed layer, and mixing should be a decision rather than a discovery.
- Generated code must be committed and verified in CI with a diff step, or it
  drifts from the schema silently. Without that step the type safety is a
  claim about a file nobody regenerated.
- It is a build-time dependency on a code generator for every schema ticket
  that depends on this one, which is eight of them.

No decision recorded here. This is the shape of the choice and what was
verified, so that whoever decides is not starting from a blank page.

## Summary

internal/store/db exists. Open creates a single-connection write pool (BEGIN IMMEDIATE) and a query_only read pool of 4 to 8, all in WAL with busy_timeout 5 s, foreign keys on, and synchronous FULL. Every connection sets FCNTL_PERSIST_WAL, and the data directory is fsynced once the WAL exists, before Open returns: the ncruces directory-fsync workaround. Read and Write hand fn a dbq.Queries and the *sql.Tx; SQLITE_BUSY becomes db.ErrBusy. The migration runner reads embedded NNNN_name.sql files, refuses gaps, duplicates, stray names and checksum drift, records version, name, sha256 and created_at in schema_migrations, applies pending migrations in one transaction, and refuses a newer schema with ErrSchemaNewer ('this binary is older than the database'). sqlc v1.31.1 runs through go generate with CGO_ENABLED=0; the output is committed in dbq/, and CI regenerates and diffs it, including a parse smoke of CHECK, WITHOUT ROWID, expression and partial indexes, a trigger with RAISE, and FTS5. views/ holds the hand-written view queries by rule. A layering test allows only driver.go to import a SQLite driver. internal/problem maps ErrBusy and ErrSchemaNewer to unavailable and ErrMigrations to internal. docs/04 records synchronous FULL and the directory fsync.
