---
schema: 3
id: TKT-01M29CCBZ8DV7086HBMN12F1HP
title: Add the healthz endpoint
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: run/cczb8-healthz
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-af9304538f7e7714e
  commit: 056e35786d1d3ab907452a609829e6c004b70f02
  session: null
  claimed_at: 2026-09-28T08:18:40Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-28T08:19:26Z
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

- [ ] Returns non-200 when the database cannot be queried or the data directory is not writable
- [ ] The endpoint requires no authentication and leaks no version or configuration detail
- [ ] Each request costs at most one SELECT 1 plus a data-directory writability probe cached for no more than 10 s, and a benchmark shows p99 under 5 ms

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
