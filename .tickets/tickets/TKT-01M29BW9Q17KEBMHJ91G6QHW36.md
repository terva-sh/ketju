---
schema: 3
id: TKT-01M29BW9Q17KEBMHJ91G6QHW36
title: Build the database access layer and migration runner
type: task
status: ready
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
updated_at: 2026-09-24T06:53:50Z
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

- [ ] The database opens in WAL mode with an explicit busy_timeout, verified by a test that reads back the pragmas
- [ ] Concurrent writers hit BEGIN IMMEDIATE and serialize rather than failing with SQLITE_BUSY
- [ ] Migrations run in order, are recorded, and are idempotent on a second start
- [ ] A binary older than the recorded schema version refuses to start with a clear message
- [ ] sqlc is configured with the migration directory as its schema source, its version is pinned, and generated code is committed
- [ ] CI regenerates the sqlc output and fails when it differs from what is committed
- [ ] A test or lint fails if a package outside the access layer imports a SQLite driver directly
- [ ] Before the first authoritative write, the opener fsyncs the data directory after the WAL file exists, working around ncruces skipping the directory fsync (TKT-01M392DP19); a test proves the directory sync is issued

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

Decided (user, 2026-09-24): sqlc generates typed `database/sql` code from `.sql` query files, and the migration directory is sqlc's schema source. Generated code is committed, and CI fails on a regeneration diff. Dynamic lens and cursor view queries are hand-written `database/sql` beside the generated set, and that split is a rule. The migration runner stays hand-rolled. SQL stays driver-agnostic. Record this decision in docs/04 "Engine" and on the ticket, with the alternatives from the 2026-09-16 comment.

### Approach
1. `db.Open(ctx, Options{Path, MaxConns})` opens `data/db/ketju.db` through ncruces's `database/sql` driver. The driver import goes in exactly one file, `driver.go`. Pragmas are set on every connection: `journal_mode=WAL`, `busy_timeout` (value open, see Open items), `foreign_keys=ON`, `synchronous=NORMAL`. The write pool's DSN adds `_txlock=immediate`.
2. Use two pools. The read pool has MaxOpenConns clamped to 4–8. The write pool has MaxOpenConns=1, so in-process writers queue in Go. `DB.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error)` always runs BEGIN IMMEDIATE. `DB.Read(ctx, fn)` runs a read-only transaction on the read pool. Hand-written dynamic queries receive the same `*sql.Tx`.
3. Set up sqlc:
   - `sqlc.yaml` goes at the repo root, with `engine: sqlite`, `schema: internal/store/db/migrations`, and `queries: internal/store/db/queries`.
   - Generated output goes to `internal/store/db/dbq`, as package `dbq` with `emit_interface: true`. Type overrides map INTEGER timestamps to `int64`, never `time.Time` (sqlc-dev/sqlc#3736 is avoided because docs/04 already chose unix ms).
   - Each schema ticket adds `queries/<area>.sql`.
   - Hand-written dynamic SQL lives only in `internal/store/db/views/`, and a package doc there states the rule.
4. Pin sqlc. Use `//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@vX.Y.Z generate` in `internal/store/db/gen.go`. Prefer `go run pkg@version` over a `tool` directive: the CI comment on git-ticket gives the reason, which is that a tool directive pulls the generator's dependency graph into the shipped module's go.mod. Confirm at claim whether sqlc builds with `CGO_ENABLED=0`. Its PostgreSQL parser (pg_query_go) is C. If `go run` needs cgo, pin the release binary by version and sha256 in the CI step instead. The CI image already has gcc, but a slow cgo build on every run is a cost to weigh.
5. Add a CI step "generated database code is current" to `.forgejo/workflows/ci.yml`. It runs `go generate ./internal/store/db && git diff --exit-code -- internal/store/db/dbq`, beside the gofmt step.
6. Schema-parse smoke: `0001_init.sql` creates `schema_migrations`, and a throwaway test migration under `testdata/` exercises every construct the schema tickets plan: CHECK, a trigger with RAISE, an expression index, WITHOUT ROWID, a partial index, and FTS5. Run sqlc on it. Any construct sqlc's SQLite parser rejects is found here, before eight tickets depend on it.
7. The migration runner is hand-rolled. It reads `migrations/NNNN_name.sql` from an `embed.FS` in numeric order and refuses gaps and duplicates. It records `(version, name, sha256, applied_at)` in `schema_migrations`, and applies each migration in its own write transaction. It refuses when a recorded checksum differs from the embedded file. It is forward-only. If the recorded maximum version is above the highest embedded version, it refuses with `db.ErrSchemaNewer`, and the message names both versions and says "this binary is older than the database".
8. Driver-agnostic enforcement: a layering test modelled on `internal/dto/layering_test.go` asserts that only `internal/store/db/driver.go` imports `github.com/ncruces/...`. The generated code imports only `database/sql`.
9. `DB.Close()` runs `PRAGMA wal_checkpoint(TRUNCATE)` and then closes both pools.

### Files and packages
New: `sqlc.yaml` and, under `internal/store/db/`: `db.go`, `driver.go`, `migrate.go`, `gen.go`, `migrations/0001_init.sql`, `queries/migrations.sql`, the generated `dbq/`, `views/doc.go`, and the tests. Touched: `go.mod` (ncruces only, no sqlc), `.forgejo/workflows/ci.yml` (the regen-diff step), `internal/problem/domain.go` (map `ErrSchemaNewer` and busy errors to `CodeUnavailable`), docs/04 "Engine".

### Tests
- AC1: `TestOpenSetsPragmas` reads back `journal_mode`, `busy_timeout`, and `foreign_keys` on a connection from each pool.
- AC2: `TestConcurrentWritersSerialize` runs 32 goroutines doing read-modify-write increments through `Write`. The count must be exact, with no SQLITE_BUSY. A second variant uses two separate `DB` handles on one file, which proves that BEGIN IMMEDIATE and busy_timeout do the serializing, not the Go pool. Run under `-race`.
- AC3: `TestMigrateTwiceIsNoop` snapshots `sqlite_schema` and `schema_migrations` across two runs and asserts they are equal. `TestMigrateRefusesGapAndChecksumDrift` runs over an `fstest.MapFS`.
- AC4: `TestNewerSchemaRefused` asserts `errors.Is(err, ErrSchemaNewer)` and the message text.
- Also: the sqlc parse smoke and the CI regen-diff step. Mutation check: delete one generated method and confirm the diff step fails.

### Design
docs/04-data-model.md opening "Engine" paragraph and "Conventions". docs/02-architecture.md "Go module layout" (`store/db/`). docs/09-roadmap.md "Risks and mitigations", the ncruces row. The ticket's 2026-09-16 comment on sqlc.

### Interactions
Defines `db.DB`, `Open`, `Read`, `Write`, `Migrate`, `ErrSchemaNewer`, the `dbq.Queries` generated surface, the `queries/` and `migrations/` conventions, and the `views/` rule. Consumers: BYYH7, BYYHG, BYYHQ, BYYJ0, BYYJ8, TKT-01M29CKRD, TKT-01M29CRJ3B, CCBYX. Consumes the BSPE8 findings.

### Open items
- The busy_timeout value is unstated in the docs. 5 s is a candidate, not a given.
- Migration numbering across parallel branches will collide. Suggested rule: renumber at rebase.
- Whether sqlc's SQLite parser accepts CREATE TRIGGER bodies with RAISE is unverified. Step 6 exists to find out. If it does not, triggers go in a migration file that sqlc's schema list skips, and the plans that use triggers (BYYHG, BYYJ0) are affected.

### Settled in grooming (2026-09-24)

- busy_timeout is 5000 ms, stated in docs/04 in this PR.
- Migrations are numbered sequentially. The run has one lane, so collisions are rare; if two land, renumber on rebase before merge.
- sqlc under `go run` may need cgo (it embeds a C parser). CI already has gcc for -race. If that still fails, pin the release binary by version and sha256.
- Run a parse check for CREATE TRIGGER ... RAISE here, before BYYHG and BYYJ0 depend on it. If sqlc rejects it, keep the trigger DDL in a migration that sqlc is told to skip.

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
