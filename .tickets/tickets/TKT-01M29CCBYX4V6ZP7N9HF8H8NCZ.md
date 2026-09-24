---
schema: 3
id: TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
title: Implement ketju serve with graceful shutdown
type: task
status: in-progress
status_reason: null
priority: urgent
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
  - TKT-01M29C0WE5GNEYS35WWVCRBWMY
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references:
  - ref: design:background-work
    path: docs/02-architecture.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/ccbyx-serve
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 945d82e1da5341d0e826e18c37f2b740ba7a9eba
  session: null
  claimed_at: 2026-09-24T15:04:58Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T15:04:58Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The command that wires everything together and takes it down cleanly: stop accepting, drain in-flight requests for up to 10 seconds, flush every thread actor so its index header is current, then close the database.

Flush order is the part worth testing. A shutdown that closes the database before the actors flush leaves indexes claiming a shorter log than the file, which is harmless but means every thread replays on the next start. Doing it right is the difference between a clean restart and a slow one.

## Acceptance criteria

- [ ] SIGTERM drains in-flight requests within the timeout and then exits zero
- [ ] Every open thread's index header is flushed before exit, so a restart trusts every index with no replay
- [ ] Startup order brings up config, database, stores, event subscribers, then the listener, and a failure in any step exits non-zero with a clear message
- [ ] A shutdown that exceeds the drain timeout still flushes actors rather than aborting

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add `cmd/ketju/serve.go` with `ketju serve [--config PATH]`. The flag's default is shared with `config check` (BYYJG). The testable core is `serve(ctx, cfg *config.Config, log *slog.Logger, opts serveOpts) error`, where `opts` carries a `ready func(addr string)` hook and a drain timeout that defaults to 10 s. `main` wraps it in `signal.NotifyContext(ctx, SIGTERM, SIGINT)`.
2. Startup order, one wrapped error per step. The message names the step and the path, for example "serve: opening database data/db/ketju.db: …":
   1. `config.Load`, then `logging.New`.
   2. `lock.Acquire(dataDir, "serve")` (CCBZN). This comes before anything touches data.
   3. `MkdirAll` for `threads/`, `blobs/`, and `db/`.
   4. `db.Open` and `db.Migrate` (BW9Q1). `ErrSchemaNewer` exits non-zero.
   5. `threadcache.New(dataDir, Options{Bound: cfg.Storage.OpenThreads, Sync: cfg.SyncPolicy(), Archived: <query threads.archived>})`.
   6. `events.New`, then `bus.Subscribe(projection.Writer)`, then `projection.CatchUp` (C5Y1S, which completes before the listener), then `bus.Start`.
   7. `jobs.Scheduler` (C5Y2S) and `health.DirProbe` (CCBZ8).
   8. `web.New(Groups{Health: …}, Options{Logger, Limits, SiteURL})`, then `net.Listen`, then `ready(addr)`, then `http.Server.Serve`.
3. Shutdown, on context cancel:
   1. `srv.Shutdown(ctx with 10 s)`. If that deadline passes, call `srv.Close()` to cut the remaining connections. Log it and continue. Never return early (AC4).
   2. Stop the scheduler and the probe.
   3. `bus.Close(short ctx)`, so the projection drains what is queued.
   4. `threadcache.Close()`. It closes every actor, including held ones. `Actor.Close` waits for in-flight appends, flushes the index header, and fsyncs the index.
   5. `db.Close()`, which checkpoints the WAL.
   6. `lock.Release()`.
   Collect errors from every step, keep going through the whole sequence, and exit non-zero if any step failed.
4. The flush order is the point: the cache closes before the DB. It does not strictly depend on the DB, but docs/02 fixes the order, and the test pins it.
5. Log one startup line: listen address, subscribers (from `bus.Subscribers()`), the catch-up summary, open-thread bound. Log one line on shutdown with the drain outcome.
6. `Groups.Session` stays nil until the identity lane's middleware (TKT-01M29C5Y3E) lands. `Groups.API` is filled in by the API tickets as they land. The 501 default covers the rest.

### Files and packages
New: `cmd/ketju/serve.go` and `cmd/ketju/serve_test.go`. Touched: `cmd/ketju/main.go` (dispatch, usage) and `cmd/ketju/exit.go` (a startup-failure code if a distinct one is wanted). Reuses `config.Load`, `logging.New`, `threadcache.New` and `Close`, `threadlog` outcome reporting, `events.Bus`, `web.New`, `lock` (CCBZN), `db` (BW9Q1), `projection` (C2WW5 and C5Y1S), `jobs` (C5Y2S), and `health` (CCBZ8).

### Tests
- AC1: `TestSIGTERMDrainsAndExitsZero` re-execs the test binary as a helper running serve on 127.0.0.1:0. The test starts a slow request (a test-only handler that sleeps 200 ms, mounted via an opts hook), sends SIGTERM, and asserts that the request completes with 200 and the process exits 0.
- AC2: `TestShutdownFlushesEveryIndex` appends to several threads through the cache inside a running serve, then cancels. After exit it reopens each thread with `threadcache.New(...).Acquire` and asserts `Thread.Outcome == threadlog.Trusted` and `Replayed == 0`. A mutation check for the commit body: swapping steps 4 and 5 of the shutdown, or skipping the cache close, must fail the test.
- AC3: `TestStartupOrderAndFailures` uses a recording hook per step to assert the order: config, lock, db, stores, subscribers and catch-up, listener. It also runs table-driven failures (bad config, lock held, DB newer than the binary, listen address in use), each asserting a non-zero `exitCodeFor` and a message naming the step. The ready hook must never fire before catch-up finishes.
- AC4: `TestDrainTimeoutStillFlushes` uses a 100 ms drain timeout and a handler blocked on a channel. It asserts that `srv.Close` ran, that the indexes are trusted on reopen, and that the lock is released.

### Design
docs/02-architecture.md "Background work" (the graceful shutdown paragraph) and "Go module layout". docs/03-thread-storage.md "Write path" step 4 (lazy header) and "Durability and recovery". The `internal/store/threadcache/cache.go` comments on `closeThread` and `Close`. The `internal/events/bus.go` comments on `Publish` before `Start` and on `Close`.

### Interactions
Integrates BW9Q1, CCBZN, C2WW5, C5Y1S, C5Y2S, and CCBZ8. The dependency TKT-01M29C5Y3S (router) is done. Dependents: TKT-01M29CEE7S (metrics) mounts on this, and TKT-01M29CXK35 (backup, Phase 4) reuses the pause and checkpoint sequence.

### Open items
The default config path is unsettled (see BYYJG). So is whether `serve` must create a missing `data_dir`: `validateDataDir` says "`ketju serve` creates it", and this plan does.
