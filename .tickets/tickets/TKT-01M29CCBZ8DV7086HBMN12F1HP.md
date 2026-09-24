---
schema: 3
id: TKT-01M29CCBZ8DV7086HBMN12F1HP
title: Add the healthz endpoint
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:21:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`/healthz` checks that the database opens and the data directory is writable, which are the two failures that make the process useless while it is still listening.

A health check that returns 200 whenever the HTTP server is up tells a supervisor nothing it did not already know from the TCP connection.

## Acceptance criteria

- [ ] Returns non-200 when the database cannot be queried or the data directory is not writable
- [ ] The endpoint requires no authentication and leaks no version or configuration detail
- [ ] Each request costs at most one SELECT 1 plus a data-directory writability probe cached for no more than 10 s, and a benchmark shows p99 under 5 ms

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

Decided: healthz does no disk I/O beyond one `SELECT 1`, with p99 under 5 ms. Lag goes to healthz. There is a tension with docs/02 "Observability" and with this ticket, which both say the check covers whether "the data directory is writable". The plan below resolves it by moving the write probe off the request path. Confirm at claim.

### Approach
1. New package `internal/web/health` with `health.Handler(Options{DB Pinger, Dir *DirProbe, Lag func() projection.Lag, Timeout})`. It answers GET and HEAD only.
2. Per request: `db.PingContext` with `SELECT 1` on the read pool, under a short context deadline. The deadline value is open; 250 ms is a candidate. Then read the last `DirProbe` result from an atomic. Then read `Lag()`. No other I/O.
3. `DirProbe` is a background goroutine started by serve. Every N seconds it creates, writes, fsyncs, and removes `data/.healthz-probe`, then stores `{ok, checkedAt}` atomically. It covers `threads/`, `blobs/`, and `db/` by probing the data root; checking each subdirectory is open. A probe result older than 3×N counts as failed, so a hung probe does not read as healthy. N is unstated in any doc; 10 s is a candidate.
4. Response: 200 when both checks pass, and 503 otherwise. The body is `{"status":"ok"|"unavailable","checks":{"db":bool,"data_dir":bool},"projection_lag_bytes":int,"projection_lag_threads":int}`, with `Cache-Control: no-store`. No version, no paths, no error text, and no config. Failure details go to the log at warn, once per transition. Lag does not change the status code. Whether a lag threshold should turn healthz to 503 is open, and no doc gives one.
5. Mounting: `internal/web/server.go`'s mux has no `/healthz`, so the catch-all would serve the SPA shell. Add `Groups.Health http.Handler` and register `mux.Handle("/healthz", group(g.Health))` as an exact pattern. The handler sits inside the chain, so it gets security headers and panic recovery. It needs no session, and GET is not rate limited (`limiterFor`). A poll every few seconds writes an info request line each time. Whether to log `/healthz` at debug is an open item for the logging middleware.

### Files and packages
New: `internal/web/health/{health.go,probe.go,health_test.go}`. Touched: `internal/web/server.go` (the Groups field and route) and `server_test.go`. Reuses `problem`-style fixed bodies, `db.DB` (a Ping method, added here if BW9Q1 did not add one), and `projection.Writer.Lag` from C2WW5. Serve (CCBYX) wires it.

### Tests
- AC1: `TestHealthzFailsWhenDBUnqueryable` closes the DB and expects 503. `TestHealthzFailsWhenDirNotWritable` uses a probe over a directory chmod 0500; skip when running as root, and use an injected fault as the fallback. `TestHealthzStaleProbeFails` uses a fake clock. All return 200 when healthy.
- AC2: `TestHealthzDoesNoDiskIO` counts through an injected FS hook and asserts zero file operations per request. `BenchmarkHealthz` plus a test that runs 1000 requests through `httptest` and asserts p99 < 5 ms. Under CI `-race` that may be noisy; give it a generous guard, record the named-machine number in the commit body, and move the assertion to a non-race step if it flakes.
- AC3: `TestHealthzLeaksNothing` sends no cookie and no auth and expects 200. A golden test of the body, with `-update`, asserts the exact key set, and that no header or body contains the version, `data_dir`, or listen address.

### Design
docs/02-architecture.md "Observability" and "Background work". The `internal/web/server.go` package doc and the `Groups` comments. The `internal/logging` request-log rules.

### Interactions
Consumes `db.DB` (BW9Q1) and `projection.Writer.Lag` (C2WW5), and optionally the last catch-up `Report` (C5Y1S). Serve (CCBYX) starts the `DirProbe` and passes `Groups.Health`. The metrics endpoint (TKT-01M29CEE7S) reports lag in more detail.

### Settled in grooming (2026-09-24)

- The data-directory write probe runs in the background every 10 s, and healthz reads its cached result.
- Register /healthz explicitly ahead of the SPA catch-all.
- Report quarantined threads (C5Y1S) as degraded.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Cheap enough to poll every few seconds' became concrete: one SELECT 1, a data-directory writability probe cached for at most 10 s (the first criterion still requires detecting an unwritable directory), and p99 under 5 ms.
