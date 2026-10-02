---
schema: 3
id: TKT-01M29CEE7SVHNRGVDH53QAFPN0
title: Add the metrics endpoint
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-10-02T17:18:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-groom-ops
  name: ""
extensions: {}
---

## Description

`/metrics` in Prometheus text format behind an admin token or a separate bind address, carrying request latency, cache hit ratios, thread store fsync latency, and projection lag summed as log length minus projected length.

Projection lag is the metric that matters most and the one no generic middleware would give you. It is the number that turns "a post seems to be missing" into a graph.

## Acceptance criteria

- [ ] /metrics is served only behind the guard the maintainer chooses (the plan's Open decision). On the main listener, a request without the guard answers 404 (TestMetricsGuard)
- [ ] The endpoint exports projection lag, log and index fsync latency, request latency by route pattern, and hit and miss counters for the response, render and thread caches
- [ ] Metric names, types, units and labels follow Prometheus conventions and are listed in docs/02 "Observability". A test fails on any name outside the rules or missing from that table (TestMetricNamesFollowConventions)
- [ ] A scrape takes no lock that the request path takes. The cache counters are atomic, and projection lag reads the read pool and stats the log files (TestScrapeTakesNoRequestPathLock)
- [ ] No label carries a thread id, a handle, a forum slug, a request path or a query (TestMetricsCarryNoIdentifiers, TestRequestLatencyLabelsArePatterns)

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e.

### Settled in grooming

- **What is exported** (docs/02 "Observability"):
  - request latency;
  - cache hit ratios;
  - thread store fsync latency;
  - projection lag, as log length minus projected length, summed.

  It is served in Prometheus text format. `/healthz` stays as docs/02 fixes it, and projection lag stays here, not there.
- **The hooks already on main** were added for this ticket:
  - `respcache.(*Cache).Stats` (hits, misses, stale);
  - `render` `Cache.Stats` (hits, misses, evictions);
  - `sse.(*Hub).Stats` (open, anonymous, refused, overflowed, lagged);
  - `events.(*Bus).Stats` (each subscriber's queued and dropped events; its name is "how an operator matches a lag metric to a component", `events.go`);
  - `threadcache.(*Cache).Stats` (opens, evictions).
- **Nothing secret or identifying reaches a log line** (docs/02 "Observability"). This ticket holds metric labels to the same rule. No label carries a thread id, a handle, a forum slug, a request path or a query string. Request latency is labelled by the route pattern the mux matched (`r.Pattern`), never by the path.
- **Internals settled here** (docs/12 "Gaps": a package boundary, a limit with no product meaning):
  - **The exposition.** It is written by hand in a new `internal/metrics` package: counters, gauges, and histograms with fixed buckets, all atomic. Callbacks are read at scrape time.
  - **Hit ratios.** They are exported as hit and miss counters, which is the Prometheus convention, and the ratio is computed in the query.
  - Alternatives: `prometheus/client_golang` lost, because it adds about ten modules (protobuf, procfs and others) to a binary that otherwise needs none of them, for three metric types. Exporting a ratio gauge lost, because a ratio cannot be summed or rated over a window.

### Open decision

**How `/metrics` is guarded.** This is a security-posture question (docs/14 "The stop rule, changed", class 1). docs/02 says "behind an admin token or bind address" and picks neither. docs/05 and docs/07 give no rule. This ticket stays in draft until the maintainer chooses. The options, with what each means:

1. **A separate bind address.** A new `metrics_listen` key in `config.toml` (for example `127.0.0.1:9090`), on its own `http.Server` that serves `/metrics` and nothing else, unauthenticated. The main listener never serves `/metrics`. The guard is where the address is reachable from. Whether the key defaults to empty (metrics off) or to a loopback address is part of the choice.
2. **An admin token.** `/metrics` on the main listener, which answers 404 unless the request carries `Authorization: Bearer <token>`. The token is read from a file named in `config.toml`, as `cookie_key_file` is, and compared in constant time. This is a new kind of credential: not a session and not an agent credential (docs/05). It reaches the endpoint through the reverse proxy, from wherever the proxy is reachable. Whether `/metrics` is off when no token file is set is part of the choice.
3. **Both.** Either one configured turns the endpoint on.
4. **An admin account's credential.** `GET /api/v1/admin/metrics` behind `adminRouteGates`, read with an admin session or an admin agent's bearer credential. The scraper then holds a credential that can do everything an admin can.

Nothing in the approach below depends on the choice except step 7.

### Approach

1. **`internal/metrics`.**
   - `Registry`, holding:
     - `Counter` and `Gauge` (`atomic.Uint64`, with float bits for gauges);
     - `Histogram` (fixed upper bounds, one atomic count per bucket, an atomic sum);
     - `CounterFunc` and `GaugeFunc`, read at scrape time.
   - `(*Registry).WriteText(w)` writes text exposition format 0.0.4, with `# HELP` and `# TYPE`. Names and label values are escaped, and families and labels are sorted so the output is stable.
   - Registering a name that breaks the rules below panics at startup.
2. **Request latency.**
   - Middleware in `internal/web` (`server.go`) wraps the mux and observes `ketju_http_request_duration_seconds{route,method,code}`. `route` is `r.Pattern` after the mux has run, or `unmatched`. `code` is the status class (`2xx` to `5xx`), which bounds the label set.
   - Buckets: 5 ms, 10 ms, 25 ms, 50 ms, 100 ms, 250 ms, 500 ms, 1 s, 2.5 s, 5 s.
   - `GET /events` (SSE) is left out of the histogram, because a stream lasts as long as the tab does. It is counted as the gauge `ketju_sse_streams{kind="member|anonymous"}`, with the counters `ketju_sse_refused_total`, `ketju_sse_overflowed_total` and `ketju_sse_lagged_total` from `Hub.Stats`.
3. **Caches.**
   - `ketju_cache_hits_total{cache}`, `ketju_cache_misses_total{cache}` and `ketju_cache_evictions_total{cache}`, for `cache="response"` (respcache; `stale` counts as a miss and is also `ketju_cache_stale_total`) and `cache="render"`.
   - For the thread cache: `ketju_thread_cache_opens_total`, `ketju_thread_cache_evictions_total` and the gauge `ketju_thread_cache_open_threads`. `threadcache.Cache.Stats` and `Len` take `c.mu`, the mutex every `Acquire` takes, so `opened`, `evicted` and the open count become atomics that a scrape reads without the mutex.
4. **fsync latency.**
   - `ketju_threadlog_fsync_duration_seconds{file="log|index"}`, a histogram.
   - Buckets: 0.1 ms, 0.5 ms, 1 ms, 2.5 ms, 5 ms, 10 ms, 25 ms, 50 ms, 100 ms, 250 ms, 1 s.
   - It is observed around each `Sync` in `threadlog.Actor.commit` and `flushHeader`. `threadlog.NewActor` gains an optional observer: a field on a small options value, or a `SetSyncObserver` called before the actor runs, whichever keeps the existing callers unchanged. `threadcache.Options.SyncObserver` passes it through.
   - With `fsync = "never"` nothing is observed, and the docs say so.
5. **Projection lag.**
   - The gauge `ketju_projection_lag_bytes` is computed at scrape time. One `ListProjectionLengths` read runs on the read pool; WAL readers never block the writer. Then `Cache.LogSize` (a stat) runs per thread. Only positive gaps are summed.
   - Quarantined threads are left out of the sum and reported as `ketju_threads_quarantined` (`proj.QuarantinedCount`).
   - A result younger than 10 s is reused, so a fast scraper cannot make the stat walk constant. Ten thousand threads stat in well under the 2 s catch-up bound.
   - From `Bus.Stats`: `ketju_events_queued{subscriber}` and `ketju_events_dropped_total{subscriber}`.
6. **Process.** `ketju_goroutines` and `ketju_heap_bytes`, from `runtime/metrics`. `ketju_build_info{version,commit}` is added only if `internal/buildinfo` from TKT-01M29CXK3S is on main at claim.
7. **Exposure, as the maintainer decides** (Open decision). Whatever the choice:
   - an unguarded request to the main listener answers 404, not 401, so the endpoint's existence is not advertised;
   - the response is `Cache-Control: no-store`;
   - the route skips session resolution, CSRF and the response cache, as `/healthz` does.
   - If option 4 is chosen, its row goes into `adminRouteGates`.
8. **Docs.** docs/02 "Observability" gains a table of every metric: name, type, unit, labels and meaning. The README gains "Metrics", which says how to scrape and which config key turns the endpoint on.

### Tests

- `internal/metrics`:
  - `TestTextExposition`: a golden file of a registry holding each metric type, and the output parses back with a small line parser;
  - `TestHistogramConcurrent`: under `-race`, observations from many goroutines sum exactly;
  - `TestRegisterRefusesBadNames`.
- `TestMetricNamesFollowConventions`. Every name serve registers:
  - matches `^ketju_[a-z][a-z0-9_]*$`;
  - ends in `_total` if it is a counter;
  - ends in `_seconds` or `_bytes` if it carries a unit;
  - and the set of names equals the docs/02 table, read from the file.
- `TestRequestLatencyLabelsArePatterns`. A request to `/api/v1/threads/<id>` is labelled with the pattern's `{id}`, never with the id itself, and an unknown path is `unmatched`.
- `TestMetricsCarryNoIdentifiers`. After a scripted session that touches threads, handles and forums, the scrape contains none of their ids, handles, slugs or paths.
- `TestProjectionLagSumsGaps`. Two threads ahead by known amounts give their sum. A quarantined thread is left out and counted in `ketju_threads_quarantined`.
- `TestFsyncLatencyObserved`. With `fsync = "group"`, appends add observations to `file="log"`, and closing an actor adds one to `file="index"`.
- `TestScrapeTakesNoRequestPathLock`. A scrape completes while the test holds `threadcache`'s mutex and respcache's lock. Under `-race`, scrapes run concurrently with appends and page reads.
- `TestMetricsGuard`, written for the chosen option. Without the guard, the main listener answers 404 and the body is not the metrics. With it, the metrics are served. With nothing configured, the endpoint is off, if the maintainer chooses that default.
- `TestHealthzUnchanged`. `/healthz`'s goldens still pass.

### Out of scope

- Alerting rules and dashboards. They belong to the operator, and no ticket owns them.
- A backup metric (TKT-01M29CXK35 out of scope).
- Per-thread or per-forum metrics. They are left out on purpose, because a label of either would name content.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:03Z

Open decision for the maintainer (docs/14 stop class 1, security posture): how `/metrics` is guarded. docs/02 says "an admin token or bind address" without choosing, and docs/05 and docs/07 give no rule. The plan lists four options with what each means, and it chooses none. The ticket stays in draft until the maintainer answers. Steps 1 to 6 and 8 of the plan do not depend on the answer, so if the answer is slow, the collectors could be split off and built first.
