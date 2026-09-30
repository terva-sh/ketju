---
schema: 3
id: TKT-01M3R5P3CNPZ6QWHZK46ECSJKR
title: Keep the Go race tests inside their timeout under CI runner load
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase2-run-r5p3cn
  branch: run/r5p3cn-template-db
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a3bcf1742c4a5390e
  commit: 53623117f3d0bf68f6f4043ebc63791773139d58
  session: null
  claimed_at: 2026-09-30T09:57:48Z
  expires_at: null
archive: null
created_at: 2026-09-30T03:28:14Z
updated_at: 2026-09-30T10:02:02Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-r5p3cn
  name: ""
extensions: {}
---

## Description

CI run 1240 (PR #193, TKT-01M29CKRH9) failed because `go test` in `internal/perm` hit Go's default 10-minute package timeout while its test fixture was still running migrations. The step took 19 minutes. That PR did not touch `internal/perm`; the retrigger (run 1257) passed every step. The cause was runner contention while several lanes' CI runs overlapped.

TKT-01M3MTQG5A covers the same failure mode for Playwright only. Nothing covers the Go suite.

### What to decide

Either:
- give the race step an explicit `-timeout` sized for a loaded runner; or
- make the `internal/perm` fixture cheaper, for example by migrating one template database and copying it per test.

Read the fixture and time it on an idle runner first, because a longer timeout alone only moves the failure.

Reported by the rename/move lane on 2026-09-30.

## Acceptance criteria

- [ ] dbtest.Open returns a database identical in schema and schema_migrations to a fresh Migrate (TestTemplateIsAFreshMigration), independent of every other (TestTemplateOpensAreIndependent), and migrates once per test binary (TestTemplateMigratesOnce)
- [ ] Every test fixture outside internal/store/db and cmd/ketju opens its database through dbtest.Open, and TestFixturesUseTheTemplate fails on a new .Migrate( call elsewhere
- [ ] A note records every package's go test -race -count=1 elapsed time before and after, on one named machine and base commit, and internal/perm and internal/web/api each take less than half their before time
- [ ] The race step and every other step in .forgejo/workflows/ci.yml are unchanged (maintainer decision 2026-09-30, docs/08 Decided 2026-09-30)

## Implementation plan

Revised at claim on 2026-09-30 by agent:claude-code/phase2-run-r5p3cn, against main at 5362311. The fixture list still matches every `_test.go` that calls `.Migrate(` (25 files, 26 calls; `internal/forms/schema_test.go` closes with `defer` and is converted by hand). Changes from the refreshed plan: `Open` refuses a path where a database or its `-wal` already exists instead of overwriting it; `TestTemplateIsAFreshMigration` also compares header pragmas and every row of every table (migrations seed groups, permissions and the registration form), leaving out only `schema_migrations.created_at`; `TestTemplateHasEveryMigration` holds the template to the files in `internal/store/db/migrations`, so a migration landing on main is in the template with nothing to regenerate; `TestFixturesUseTheTemplate` skips any directory with its own `go.mod` (spikes, nested checkouts) and fails if it finds no call even in the allowed directories. Before and after are measured back to back on one machine, from an exported tree of the base commit and from the branch.

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. Refreshed against main at 5124209 on 2026-09-30: the open decision is settled, and the fixture list was checked against every `_test.go` file that calls `.Migrate(` there. `cmd/ketju/flow_test.go` (TKT-01M3NTSCMN) is new since 9a8fc76 and stays on its own opening with the rest of `cmd/ketju`.

### What was measured

On an idle 12-core workstation at 9a8fc76 (Go 1.27.1):
- `go test -race -count=1 ./internal/perm/` took 119 s.
  - `TestUnionWithGlobalFallback` took 29 s, for 60 fixtures in its subtests.
  - `TestPreviewMatchesApply` took 21 s, and `TestCanEachAgreesWithCheck` 20 s.
  - The package has no `t.Parallel`.
- `go test -race -count=1 ./...` spent 966 s of package time across 43 packages. The slowest were:

  | package | seconds |
  |---|---|
  | `internal/web/api` | 154 |
  | `internal/perm` | 128 |
  | `cmd/ketju` | 90 |
  | `internal/web/auth` | 86 |
  | `internal/store/db` | 79 |
  | `internal/events/projection` | 69 |
  | `internal/store/db/views` | 66 |
  | `internal/service/threads` | 59 |

CI run 1240 failed in `internal/perm` at Go's default 10-minute timeout, with the fixture still in `applyMigrations` (TKT-01M29CKRH9 note 6), and run 1242 failed the same way (TKT-01M29CXJZ9 note 9). A runner slower than this workstation by a factor of five, sharing its CPU with another run, reaches that timeout. The perm fixture (`newFixtureWith` in `internal/perm/perm_test.go`) opens a fresh database and runs all 17 migrations for every test and subtest, about 150 times per package run. Each migration run is SQLite translated to Go (`ncruces/go-sqlite3`, `go.mod`), instrumented by `-race`, on a `synchronous(full)` database (`internal/store/db/db.go`).

### Settled in grooming

- **Make the fixture cheap; the timeout stays as it is** (engineering choice). The description itself notes that "a longer timeout alone only moves the failure". The migration cost is paid once per test binary instead of once per test. The same approach as TKT-01M3MTQG5A (Stop Playwright checks timing out under CI runner contention): bound the work and do not widen the budget. That ticket changes only `web/client/e2e`, so the two do not overlap.
- **The template is a file copy, not `VACUUM INTO` or the backup API** (engineering choice). `VACUUM INTO` cannot run inside the transaction that every `DB.Read` and `DB.Write` opens. The driver's `Conn.Backup` would break the rule that only `internal/store/db/driver.go` imports the driver (`TestOnlyDriverGoImportsADriver`). A database closed cleanly is whole in its main file, as `cmd/ketju/reindex_test.go` already relies on for its backup: closing checkpoints the WAL, and that test checks that the `-wal` file is empty.
- **The helper lives in `internal/store/db/dbtest`,** which exists for "test helpers over the database that more than one package's tests need" and is imported only from `_test` files. `internal/store/db`'s own tests keep calling `Migrate`, because they test it, and they could not import `dbtest` without a cycle. `cmd/ketju` keeps its own opening, because its tests exercise serve's real startup. `dbtest/dump_test.go` keeps its fresh migration, because it is the template's reference.
- **Workflow changes are outside this ticket.** docs/12 "Changing CI" lets a run add steps to `Build and Test` but does not list editing an existing step. The comment in `.forgejo/workflows/ci.yml` reads it the same way: "docs/12 lets a run add steps to this job and not edit the ones it has". So adding `-timeout` to `go test -race ./...` needs the maintainer. See the open decision.
- **The race step keeps its default timeout, and this ticket builds the template database** (maintainer decision 2026-09-30, docs/08 "Decided 2026-09-30": "The CI race step keeps its default timeout. The slow `internal/perm` fixture is fixed by a migrated template database instead (TKT-01M3R5P3CN)"). This settles the open decision the earlier plan carried, as its option (a). PR #208 merged it into main. So `.forgejo/workflows/ci.yml` does not change: neither `-timeout` nor the stale "The suite is seconds" comment on `go test (race)`.

### Approach

1. **`internal/store/db/dbtest/template.go`** (new):
   - `func Open(t testing.TB, opts db.Options) *db.DB`. When `opts.Path` is empty it becomes `t.TempDir()/db/ketju.db`.
   - It creates the parent directory, writes the template's bytes there with mode 0600, and calls `db.Open(t.Context(), opts)`. It registers `t.Cleanup(d.Close)` and returns the database, already migrated.
   - The template is built once per test binary behind a `sync.Once`. It opens a database in a directory from `os.MkdirTemp("", "ketju-dbtest-")`, calls `Migrate`, and closes it. It fails if the `-wal` file is not empty, then reads the main file into memory and removes the directory. A build error is kept and reported through `t.Fatal` by every `Open`.
   - `Open` does not call `Migrate` again. Its doc comment says the database it returns is exactly what `Migrate` makes, and the test in the Tests section holds it to that.
2. **Move the fixtures to `dbtest.Open`**, in this order, measuring after each:
   - `internal/perm/perm_test.go` (`newFixtureWith`, which keeps `Trace` through `opts`);
   - `internal/web/api` (`thread_test.go`'s `newWorld`, and `admin_test.go`, `me_test.go`, `apply_test.go`);
   - `internal/web/auth/auth_test.go`, `internal/web/preuser_test.go`, `internal/web/health/health_test.go`;
   - `internal/events/projection/writer_test.go`, `internal/store/db/views/threadlist_test.go`;
   - `internal/service/*` (threads, identity, applications, account, forms, notify, users);
   - `internal/auth/session`, `internal/auth/agentcred`, `internal/audit`, `internal/settings`, `internal/forms` and `internal/jobs`.

   A fixture that migrates and then seeds keeps its seeding. The only change is the open and the migrate.
3. **Keep new fixtures on the template.** Add `TestFixturesUseTheTemplate` in `dbtest`, in the style of `TestOnlyDriverGoImportsADriver`. It walks the module's `_test.go` files and fails on a `.Migrate(` call outside `internal/store/db` (its own package and `dbtest`) and `cmd/ketju`, naming the file and line.
4. **Measure.** Run `go test -race -count=1 -json ./...` on the same machine before and after, and record every package's elapsed time in a note on this ticket, with the commit and the machine.
5. No change to `.forgejo/workflows/ci.yml`, as the maintainer decided. That includes the "Phase 2 exit tests, by name" step (`go test -race ./cmd/ketju/ -run '^TestPhase2Exit' -count=1 -v`, TKT-01M3NTSCMN), which runs `cmd/ketju`'s exit tests a second time. `cmd/ketju` keeps its own opening, as settled, so that step is neither faster nor slower after this ticket.

### Tests

- `TestTemplateIsAFreshMigration` (new, `dbtest/template_test.go`): a database from `dbtest.Open` and one from a fresh `db.Open` plus `Migrate` have identical `sqlite_schema` rows (type, name and sql, ordered) and identical `schema_migrations` versions. It runs the query that `schemaSnapshot` in `internal/store/db/migrate_test.go` uses, copied into `dbtest` because that helper is unexported in package `db`.
- `TestTemplateOpensAreIndependent` (new): a row written through one `dbtest.Open` is absent from a second one opened afterwards, and both pass `PRAGMA integrity_check` and `PRAGMA foreign_key_check`.
- `TestTemplateMigratesOnce` (new): with a `db.Options.Trace` counter, a second and a third `dbtest.Open` run no `CREATE TABLE` statement.
- `TestFixturesUseTheTemplate` (step 3).
- `TestTemplateHasEveryMigration` (new): the template's `schema_migrations` names equal the `NNNN_name.sql` files in `../migrations`.
- `TestOpenRefusesAnExistingDatabase` (new): `Open` never overwrites a file, and never creates a database beside a stale `-wal`.
- The whole suite passes under `go test -race ./...` with no test changed except in how it opens its database.

### Out of scope

- Playwright's timeouts (TKT-01M3MTQG5A).
- Workflow `concurrency` and runner capacity, which are the maintainer's (docs/12 "Changing CI").
- `t.Parallel` in `internal/perm`. It would add a second change in behaviour to this one, and the template alone removes the cost.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:02:57Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Open decision for the maintainer: should the race step go test -race ./... also get an explicit -timeout (and its stale 'The suite is seconds' comment corrected)? docs/12 'Changing CI' does not let a run edit an existing step. (a) no, the template database removes the cause; (b) yes, -timeout 20m as a guard. Recommendation (a); the plan is written for it. Measured at 9a8fc76 on an idle 12-core workstation: internal/perm 119 s alone, 128 s within the full race suite; internal/web/api 154 s; the full suite 966 s of package time over 43 packages.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T08:43:12Z

Open decision settled by the maintainer on 2026-09-30: the CI race step keeps its default timeout, and the template-database fix is built as planned (docs/08, "Decided 2026-09-30"). The ticket has no open decision left.

**agent:claude-code/phase2-groom** at 2026-09-30T09:08:00Z

Plan refreshed against main at 5124209 on 2026-09-30. The open decision is settled as the maintainer decided on 2026-09-30 (docs/08 'Decided 2026-09-30', merged in PR #208, whose note above records the same): the CI race step keeps its default timeout and the template database is built. The Open decision section is replaced by a Settled item citing it; the earlier Settled items are carried verbatim. Step 5 now says ci.yml does not change at all, including the 'Phase 2 exit tests, by name' step added by TKT-01M3NTSCMN, which re-runs cmd/ketju's exit tests; cmd/ketju keeps its own opening, so that step's time is unaffected. The fixture list was checked against every _test.go that calls .Migrate( at 5124209 and is complete; cmd/ketju/flow_test.go is new and exempt with cmd/ketju. Criterion 4 now states the decision. Criterion 3 compares against the before run on the implementer's machine rather than 9a8fc76's figures, which were taken on a machine the implementer may not have. Re-applied onto main at a660ae5, after PR #208 merged, with its note kept.
