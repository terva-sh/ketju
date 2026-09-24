---
schema: 3
id: TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
title: Implement ketju serve with graceful shutdown
type: task
status: done
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
  - ref: pr:69
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T18:14:01Z
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

- [x] SIGTERM drains in-flight requests within the timeout and then exits zero
- [x] Every open thread's index header is flushed before exit, so a restart trusts every index with no replay
- [x] Startup order brings up config, database, stores, event subscribers, then the listener, and a failure in any step exits non-zero with a clear message
- [x] A shutdown that exceeds the drain timeout still flushes actors rather than aborting

## Implementation plan

Revised at claim (2026-09-24), after reading the code. Built after TKT-01M29CCBZN (lock) and TKT-01M29C5Y1S (catch-up), which it wires; see the first note.

### Approach
1. `cmd/ketju/serve.go`: `ketju serve [--config PATH]`. The path is --config, else KETJU_CONFIG, else ./config.toml, as settled for BYYJG. The testable core is `serve(ctx, path, serveOpts)`. `runServe` wraps it in a SIGTERM/SIGINT context.
2. Startup order, with each failure naming its step ("serve: locking the data directory: …"):
   1. `config.Load`, then `logging.New`.
   2. `MkdirAll(data_dir)`, then `lock.Acquire(data_dir, "serve")`. Serve creates a missing data_dir; that resolves the open item.
   3. `MkdirAll` for threads/ and blobs/, then `db.Open` and `Migrate`.
   4. `threadcache.New`, with Archived read from the projection row.
   5. `projection.New`, whose Sweep is `CatchUp`; `bus.Subscribe`.
   6. `CatchUp` completes, then `bus.Start` and `proj.Run`.
   7. Services, shell, and `web.New`, with the session middleware; then `net.Listen`; ready; `Serve`.
3. Shutdown. Closers are pushed as startup succeeds and run in reverse, so a failure partway up closes exactly what was opened:
   1. `srv.Shutdown` with a 10 s drain. At the deadline `srv.Close` cuts what remains, and shutdown carries on.
   2. `bus.Close` (5 s), then cancel the background context.
   3. `cache.Close`, which closes every actor and flushes and fsyncs its index.
   4. `db.Close`.
   5. `lock.Release`.
   Every step runs, and the errors are joined.
4. A relative data_dir now resolves against the config file, as settled for BYYJG. This lives in config.Load, so `config check` agrees with serve.
5. Not wired yet, because their tickets are not done: healthz (CCBZ8), the job scheduler (C5Y2S), OIDC and auth routes (C7ZZ9), and metrics (CEE7S). Each mounts on this when it lands; web's 501 default covers the rest.

### Tests (cmd/ketju/serve_test.go)
- AC1: `TestSIGTERMDrainsAndExitsZero` runs the test binary as `ketju serve`. A 300 ms request is in flight at SIGTERM; it completes 200, and the process exits 0.
- AC2: `TestShutdownFlushesEveryIndex` appends through the running cache, shuts down, and checks that the cache is closed (ErrClosed) and that every thread reopens Trusted with nothing replayed.
- AC3: `TestStartupOrderAndShutdown` checks the step order both ways. `TestStartupFailures` covers a missing config, a bad config, the lock held, a database newer than the binary, and the address in use; each exits non-zero naming its step, never reaches the listener, and releases the lock.
- AC4: `TestDrainTimeoutStillFlushes`: a request blocked past a 100 ms drain is cut, and the actors still flush and the lock is released.
- Carried from other tickets: `TestServeWaitsForCatchUp` (C5Y1S AC3); the lock's serve-level checks inside the SIGTERM test (CCBZN AC1 and AC3); and `TestServeQuarantinesACorruptThread`.

### Design
docs/02-architecture.md "Background work" and "Go module layout". docs/03-thread-storage.md "Write path" and "Durability and recovery".

### Interactions
Integrates CCBZN, C5Y1S, C2WW5, BW9Q1, C5Y3S and C801M. Dependents: CEE7S (metrics) and CXK35 (backup) reuse the shutdown sequence. CCBZ8, C5Y2S and C7ZZ9 add their pieces here.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:05:49Z

Claimed, read the code, and handed back before building. Its plan acquires the lock (TKT-01M29CCBZN, Add the single-process lock file) before anything touches data, and runs catch-up (TKT-01M29C5Y1S, Implement startup projection catch-up) before the listener. Neither exists yet, and projection.New refuses to build a Writer without the Sweep that catch-up supplies (review 389). Serving without the lock is the quiet two-writer corruption that ticket exists to refuse. Order taken instead: CCBZN, then C5Y1S, then this ticket, so serve wires real pieces rather than stand-ins. The declared dependencies are unchanged, because the two are integrations the plan names, not links the store records.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:08:15Z

Carries two checks from TKT-01M29CCBZN (Add the single-process lock file), which left its AC1 and AC3 unticked because serve did not exist yet: (1) a second ketju serve against the same data directory exits non-zero and its stderr names the holding pid; (2) after SIGTERM and a clean exit, lock.Acquire on the directory succeeds. Serve takes lock.Acquire(dataDir, "serve") right after config load and before any MkdirAll inside it or db.Open, and releases it last. Acquire does not create the data directory; serve does that first if it creates one at all. A refusal is a *lock.HeldError; whether it gets its own exit code is serve's call (currently exitInternal).

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:22:34Z

Carries from TKT-01M29C5Y1S (Implement startup projection catch-up), whose AC3 is left unticked until serve exists.

Serve has to wire catch-up as follows:
1. Build `reopen := func(ctx, id) (int64, error) { cache.With(id, func(th) { n = th.Actor.LogLength() }) }`.
2. Construct the Writer with `Sweep: func(ctx) error { _, err := w.CatchUp(ctx, reopen); return err }`. The closure refers to the Writer after assignment.
3. Run `w.CatchUp(ctx, reopen)` after migrate and before `bus.Start`. A returned error (the query or the walk failed) exits non-zero. Report.Corrupt and Report.Failed do not stop startup.
4. Pass `Quarantined: w.Quarantined` to `threads.Service`.
5. Only then listen. TestServeWaitsForCatchUp should assert that projection_state equals the log sizes when the ready hook fires.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T17:18:09Z

Built `ketju serve` in cmd/ketju/serve.go, with tests in serve_test.go. A relative data_dir now resolves against the config file.

Decisions, with the alternatives that lost:
- **Exit codes.** A startup failure exits 4 (exitInternal) with a message naming the step. Separate codes per step lost: the AC asks only for non-zero and a clear message, and fsck's codes mean something else.
- **Shutdown order.** Closers run in reverse from a stack, so a failure halfway up closes only what was opened. A fixed shutdown function lost, because it would have to know what had been started.
- **The bus drains before its context is cancelled.** The reverse order would abandon queued projection work mid-batch. Catch-up would repair it at the next start, but a clean shutdown should not need that.
- **Archived** is read from the projection row, and treated as not archived on a read error. Evicting a live thread early costs a reopen; pinning an archived one costs a slot.
- **The listener binds after catch-up.** Binding first and refusing requests until ready lost: a load balancer would see the port open and route to it.

AC2, and what the test can see: the actor writes the index header after every commit, and Close adds the fsync. An in-process reopen therefore trusts the index whether or not shutdown closed the actors. The test additionally asserts that the cache refuses work afterwards (ErrClosed), which is how it sees the actors were closed; the fsync itself needs a power loss to observe. That assertion was added when the mutant dropping cache.Close survived the first run.

Mutation check: 13 of 13 serve mutants killed. They drop or reorder:
- the cache close and the lock release;
- catch-up;
- srv.Close past the drain, and the drain itself (a drain of 1 ms);
- the lock check and migrate;
- the quarantine wiring and the reverse order of closers;
- KETJU_CONFIG and the bus drain;
- the relative data_dir.
Three survived the first run and each gained a test: cache.Close (the ErrClosed check), srv.Close (the drain test now requires the blocked request to be cut), and the quarantine wiring (TestServeQuarantinesACorruptThread). An earlier test-harness bug cost 20 s per test and was fixed; the serve tests take about 3 s under -race.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T18:14:01Z

Answered review 470 on PR #69 (head aa18704).

- **High, the projection runner.** Shutdown cancelled the runner but closed the thread cache and the database without waiting for it. The runner now has its own closer, "stop projection", which cancels it and waits for it to return, up to 5 s; past that it reports an error rather than hanging. The closer is pushed before the bus's, so the order at shutdown is: the bus delivers what is queued, then the runner stops, then the stores close. The first placement put the runner's closer after the bus's, which would have cancelled the bus's deliveries mid-drain; it was caught while reordering.
- **Medium, listener errors.** A non-timeout error from srv.Shutdown, or an error from the srv.Close fallback, is now returned, joined with the closers' errors, and the closers still run. The Shutdown error path has no test. Nothing a test can arrange makes net/http's Shutdown fail except by deadline, so its mutant is listed as untestable, not as killed.

Test: TestShutdownWaitsForTheProjectionRunner holds the runner back 200 ms as it returns and checks that the cache was not closed first. The step order in TestStartupOrderAndShutdown now includes "stop projection" after "close bus".

Mutation check: 14 of 14 killed, including making the wait not wait and dropping the srv.Close error. CI run 316 on aa18704 passed.

## Summary

ketju serve wires the server together. Startup: config, lock, database and migrations, thread store, projection subscriber and catch-up, then the listener; each failure exits non-zero naming its step and closes what it opened. On SIGTERM or SIGINT it drains requests for up to 10 s, cutting any left, then closes the bus, every thread actor (flushing and fsyncing its index), the database and the lock, running every step even if one fails. A relative data_dir resolves against the config file. All four ACs are met, and serve's tests also prove C5Y1S AC3 and CCBZN AC1 and AC3. Healthz, jobs, auth routes and metrics mount here when their tickets land.
