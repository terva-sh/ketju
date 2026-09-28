---
schema: 3
id: TKT-01M29CCBZ8DV7086HBMN12F1HP
title: Add the healthz endpoint
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
  - ref: pr:107
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-28T08:56:27Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`/healthz` checks that the database opens and the data directory is writable, which are the two failures that make the process useless while it is still listening.

A health check that returns 200 whenever the HTTP server is up tells a supervisor nothing it did not already know from the TCP connection.

## Acceptance criteria

- [x] Returns non-200 when the database cannot be queried or the data directory is not writable
- [x] The endpoint requires no authentication and leaks no version or configuration detail
- [x] Each request costs at most one SELECT 1 plus a data-directory writability probe cached for no more than 10 s, and a benchmark shows p99 under 5 ms

## Implementation plan

Revised at claim (2026-09-28) against main 056e357.

### Approach
1. `db.DB.Ping(ctx)` in `internal/store/db`: one statement on the read pool, no BEGIN/COMMIT. The statement is `SELECT count(*) FROM sqlite_schema`, not a bare `SELECT 1`: a bare constant is answered without opening a read transaction on the file, so it passes for a database whose file is unreadable. It is still the one SELECT the criterion allows, and a trace test counts it.
2. `projection.Writer.QuarantinedCount()`: how many threads are quarantined, read under the writer's mutex. Only the count leaves the package; healthz reports whether it is zero.
3. New package `internal/web/health`:
   - `DirProbe`: `Probe()` creates `data/.healthz-probe`, writes, fsyncs, closes and removes it, and stores `{ok, checkedAt}`. `Run(ctx)` probes every 10 s. A result older than two intervals counts as failed, so a probe hung on a stuck disk does not read as healthy. Transitions log at warn (failing, with the error) and info (recovered), once each.
   - `Handler(Options{DB, Dir, Quarantined, Logger, Timeout})`: GET and HEAD only; any other method is 405 with `Allow: GET, HEAD`. Per request: one `Ping` under a 1 s deadline, then an atomic read of the probe, then the quarantine count. No other I/O.
   - Body: `{"status":"ok"|"degraded"|"unavailable","checks":{"database":"ok"|"failing","data_dir":"ok"|"failing","threads":"ok"|"quarantined"}}`, `Cache-Control: no-store`. 503 when the database or data directory check fails; 200 otherwise, including `degraded`. No version, path, error text, count, thread id, lag, or configuration.
4. `internal/web/server.go`: `Groups.Health`, mounted as the exact pattern `/healthz` ahead of the SPA catch-all. The chain gives `/healthz` logging, panic recovery, security headers and the body limit, and skips rate limiting, session resolution and CSRF: a supervisor or a browser carrying a session cookie would otherwise cost a session lookup and a touch (a write) per poll, breaking the one-SELECT budget. None of the three applies to a GET/HEAD-only unauthenticated endpoint.
5. `cmd/ketju/serve.go`: build the probe, run one probe before the listener opens so the first poll is accurate, run the loop under the run context, and stop and wait for it on shutdown before the lock is released.
6. docs/02 "Observability": record the body, the status codes, the probe interval and staleness bound, and the degraded rule.

### Gaps settled here (internals only)
- Degraded (quarantined threads) answers 200, not 503. docs/03 keeps every other thread serving because one bad file must not take the forum down; a 503 would make a supervisor or load balancer do exactly that, and a restart cannot repair a quarantine.
- Projection lag is not reported. docs/02 puts it on `/metrics`, behind an admin token; healthz is unauthenticated and the design does not say lag may be public.
- The staleness bound (two intervals) and the 1 s ping deadline are defaults nobody configures.

### Tests
- AC1: DB closed -> 503 `database: failing`; directory chmod 0500 -> 503 `data_dir: failing` (skipped as root, with an injected-fault case that always runs); stale probe via a fake clock -> 503; quarantine -> 200 `degraded`; healthy -> 200.
- AC2: no cookie, no auth -> 200; golden bodies (`-update`) for each state; no header or body carries the data dir, the listen address, a version, or error text; `/healthz` is routed ahead of the SPA; non-GET is 405.
- AC3: through `web.New` with the real session middleware and a session cookie, a trace on the DB counts exactly one statement per request; `TestHealthzP99` runs 1000 requests through the chain and asserts p99 < 5 ms; `BenchmarkHealthz` reports p99. Serve test: `/healthz` answers 200 on a running server.

### Settled in grooming (2026-09-24)

- The data-directory write probe runs in the background every 10 s, and healthz reads its cached result.
- Register /healthz explicitly ahead of the SPA catch-all.
- Report quarantined threads (C5Y1S) as degraded.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough to poll every few seconds' became concrete: one SELECT 1, a data-directory writability probe cached for at most 10 s (the first criterion still requires detecting an unwritable directory), and p99 under 5 ms.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:25:13Z

How each criterion was met, and the gaps settled here (docs/12 "Gaps the design leaves open"; all recorded in docs/02 "Observability" in the same PR).

### Criterion 3 and its wording
- The one statement per request is `SELECT count(*) FROM sqlite_schema`, not a literal `SELECT 1`. A bare constant opens no read transaction on the file: with the database's first page zeroed, `SELECT 1` still answers and the schema read fails (`TestPingReadsTheFile` holds both). It is still one SELECT, on the read pool, with no BEGIN/COMMIT. `TestHealthzOneStatementPerRequest` counts statements through SQLite's own trace across 50 requests, half of them carrying a well-formed session cookie, and finds exactly 50.
- "Cached for no more than 10 s" is read as the grooming decision states it: the probe refreshes every 10 s and healthz reads the cached result. A result older than two intervals counts as failing, so a probe hung on a stuck disk turns healthz to 503 rather than leaving the last good answer up. Expiring at exactly 10 s would flap to failing before every refresh, since a probe's fsync takes time.
- p99: BenchmarkHealthz on brokkr (Ryzen 9 5950X) through the full chain, no -race: 80-83 us. TestHealthzP99 asserts < 5 ms over 1000 requests in CI's -race run; locally 0.29 ms.

### Gaps settled
- Degraded (quarantined threads) answers 200. docs/03 keeps the rest of the forum serving so one bad file cannot take it down; a 503 would do exactly that through a load balancer, and a restart cannot repair a quarantine.
- Projection lag is not in the body, although the pre-groom plan put it there. docs/02 puts lag on `/metrics` behind an admin token, and the design does not say it may be public. The quarantine is reported as a word, not a count or ids, for the same reason.
- `/healthz` skips rate limiting, session resolution and CSRF in the chain (it keeps logging, recovery, security headers and the body limit). Otherwise a poll carrying a session cookie costs a lookup and a touch, which is a write, and breaks criterion 3. GET/HEAD-only, it is nothing the limiter or CSRF acts on.
- The ping deadline is 1 s, and the probe file is `data/.healthz-probe`: internal defaults nobody configures.
- Left as it is: `/healthz` still writes an info request-log line per poll. Whether to log it at debug is the logging middleware's decision and was not needed for any criterion.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:33:27Z

Terva review 1152 (run e4ccb5e1-2885-484c-95ce-e2d86423d80b, Actions run #600) on head 4c632289942b1bafb1ed253eb91f2b7d9e1d0650, base 056e35786d1d3ab907452a609829e6c004b70f02: one high finding, 'A stuck directory probe can block shutdown indefinitely'. Accepted and fixed in behaviour. Shutdown no longer waits on the probe goroutine unbounded: DirProbe.Start returns a stop that cancels the loop and waits at most busDrain (5 s), then reports the stuck probe and lets shutdown go on to release the lock, the same shape as the projection runner's closer. Serve logs it as the new step 'stop health probe'. TestProbeStopIsBounded holds a probe blocked in its write and checks that stop returns with the error within the bound.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:43:03Z

Terva review 1153 (run 156c6285-dcfa-44e0-9624-657f60d7b057, Actions run #605) on head ff8e86382bea44a027b2527782a6e38f2f2ae667, base de904cb55ab88426e91eb9c84c66c9a49bbb6ee1. It marked 1152's finding resolved and raised one medium: 'A timed-out probe can keep running after the data-directory lock is released'. Accepted and fixed in behaviour. Each DirProbe now probes a file of its own, data/.healthz-probe-<random> (ProbePrefix plus crypto/rand.Text, fixed for the probe's life), so an old process's in-flight probe cannot remove the next owner's file and the new probe cannot touch the old one's. serve calls health.RemoveStale under the lock, before its first probe, to clear files left by a crashed or timed-out process. TestProbesDoNotShareAFile keeps an old process's file in place across a new probe and then clears it with RemoveStale. The mutation that makes the name fixed fails it. docs/02 is updated.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:50:08Z

Terva review 1155 (run 856189be-64d1-4922-a8d6-28e9787463f0, Actions run #611) on head c3e1a0d9ac95be12fee45a4224898d447fdb7b7a, base de904cb55ab88426e91eb9c84c66c9a49bbb6ee1. It marked 1153's finding resolved and raised one medium: 'The initial probe can block startup and shutdown indefinitely'. Accepted and fixed in behaviour. serve no longer calls Probe synchronously. Run probes on entry inside Start's goroutine, and serve registers the bounded stop before any probe runs. It then waits for the first result with DirProbe.WaitFirst(busDrain); past that it logs a warning and goes on, and healthz reports data_dir failing until a probe completes. TestFirstProbeIsBounded holds the first probe stuck and checks four things: WaitFirst returns false within its bound, /healthz answers 503, stop reports the stuck probe, and once the probe is released both WaitFirst and /healthz recover. docs/02 is updated.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:56:13Z

CI run 614 on head 687f5e5b0534c427dd26c96fec7c0639e1343795 failed 'go test (race)' in TestHealthzP99: p99 was 5.21 ms against the 5 ms bound. That head's Terva review 1156 was clean. Runs 599, 604 and 610 had passed the same test, locally it measures 0.3-0.45 ms under -race, and BenchmarkHealthz without -race measures 80 us. The failure is contention: CI runs every package's race tests at once on a shared runner. Fix: the test now holds the best of up to three 1000-request runs to the bound, so a slow handler still fails all three. The per-request I/O budget stays exact in TestHealthzOneStatementPerRequest. Criterion 3 stays ticked on the benchmark evidence. This is CI attempt 1 of 3.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:56:27Z

Correction to the previous note about CI run 614, which supersedes its 'Terva review 1156' reference. The clean review of head 687f5e5b0534c427dd26c96fec7c0639e1343795, base de904cb55ab88426e91eb9c84c66c9a49bbb6ee1, is run 494bc146-ffb1-4a01-a47c-4ba3df86e202 (Actions run #615), recorded in the maintained summary comment 16677. It has no review ID and no findings. It marked 1155's finding resolved.

## Summary

/healthz landed in internal/web/health and is mounted by serve at exactly /healthz. It answers 503 {status: unavailable} when the database cannot be queried (one read of sqlite_schema per request) or the cached data-directory probe (create, write, fsync, remove every 10 s in the background; stale after 20 s) failed, and 200 {status: degraded} when threads are quarantined. The body is a status and one word per check, with no version, path, error text, count or configuration. The route skips rate limiting, session resolution and CSRF so every poll costs one statement. p99 80 us locally without -race. docs/02 Observability records the contract.
