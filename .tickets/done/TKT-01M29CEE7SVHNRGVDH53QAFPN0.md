---
schema: 3
id: TKT-01M29CEE7SVHNRGVDH53QAFPN0
title: Add the metrics endpoint
type: task
status: done
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
  - ref: pr:358
    path: null
  - ref: pr:369
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-10-02T19:39:09Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cee7s
  name: ""
extensions: {}
---

## Description

`/metrics` in Prometheus text format behind an admin token or a separate bind address, carrying request latency, cache hit ratios, thread store fsync latency, and projection lag summed as log length minus projected length.

Projection lag is the metric that matters most and the one no generic middleware would give you. It is the number that turns "a post seems to be missing" into a graph.

## Acceptance criteria

- [x] /metrics is served only behind the guard the maintainer chooses (the plan's Open decision). On the main listener, a request without the guard answers 404 (TestMetricsGuard)
- [x] The endpoint exports projection lag, log and index fsync latency, request latency by route pattern, and hit and miss counters for the response, render and thread caches
- [x] Metric names, types, units and labels follow Prometheus conventions and are listed in docs/02 "Observability". A test fails on any name outside the rules or missing from that table (TestMetricNamesFollowConventions)
- [x] A scrape takes no lock that the request path takes. The cache counters are atomic, and projection lag reads the read pool and stats the log files (TestScrapeTakesNoRequestPathLock)
- [x] No label carries a thread id, a handle, a forum slug, a request path or a query (TestMetricsCarryNoIdentifiers, TestRequestLatencyLabelsArePatterns)
- [x] Every request-latency label comes from a fixed set, and an unknown method is recorded as "other" (TestRequestLatencyLabelsAreBounded)

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e. Revised at claim, 2026-10-02, against main at 30834b8e: the open decision is answered, and steps 2 to 5 and 7 say what was built.

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

### Decided by the maintainer (2026-10-02)

How `/metrics` is guarded, the plan's former open decision: option 1, a separate bind address (note 2, docs/02 "Observability"). A `metrics_listen` key in `config.toml` starts a second listener that serves `/metrics` and nothing else, unauthenticated. An empty key, the default, starts no listener. The main listener never serves `/metrics`. The token, both-options and admin-credential options lost.

### Approach

1. **`internal/metrics`.**
   - `Registry`, holding `Counter`, `Gauge`, `HistogramVec` (fixed upper bounds, one atomic count per bucket, a CAS-updated float sum), and `CounterFunc`/`GaugeFunc`, read at scrape time.
   - A labelled series is found through a copy-on-write map that requests and scrapes load without a lock. Only the first observation of a new label set takes the family's mutex, which no scrape takes.
   - `WriteText` writes text exposition 0.0.4 with `# HELP` and `# TYPE`, escaped, families and series sorted. `Handler` serves it at exactly `/metrics` for GET and HEAD, `Cache-Control: no-store`, 404 elsewhere, 405 for other methods.
   - Registering a name or label that breaks the rules panics at startup.
2. **Request latency.**
   - `internal/web/requestmetrics.go` observes `ketju_http_request_duration_seconds{route,method,code}` between the request log and panic recovery, so a panic is a 5xx when it is recorded. `web.Options.Requests` takes the histogram; nil records nothing.
   - `route` is the pattern of the innermost `ServeMux` that matched, with its method stripped. `web.New` wraps its own mux and every group that is a `ServeMux` (API, auth, blobs) so the pattern each sets on the request it was handed is read back after it returns; the innermost returns first and wins. A request answered before any mux (rate limit, body limit, CSRF) is `unmatched`; a path only a catch-all matches carries `/` or `/api/v1/`.
   - `method` is one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, else `other` (Terva review 1943). `code` is the status class.
   - Buckets: 5 ms, 10 ms, 25 ms, 50 ms, 100 ms, 250 ms, 500 ms, 1 s, 2.5 s, 5 s.
   - `/events` is left out of the histogram and counted as `ketju_sse_streams{kind="member|anonymous"}` and the `ketju_sse_{refused,overflowed,lagged}_total` counters from `Hub.Stats`.
3. **Caches.**
   - `ketju_cache_{hits,misses,evictions}_total{cache="response|render"}` and `ketju_cache_stale_total`, read through a new lock-free `Counters()` on respcache and render (their `Stats` takes the mutex a lookup takes, for entries and bytes).
   - `ketju_thread_cache_opens_total`, `ketju_thread_cache_evictions_total` and `ketju_thread_cache_open_threads`. `threadcache`'s `opened`, `evicted` and the open count become atomics written under `c.mu`, so `Stats` and `Len` take no lock.
   - `sse.Hub.Stats` reads atomics kept beside `len(streams)` and `anon`; `events.Bus.Stats` reads the subscriber list frozen at `Start` without `b.mu`; `projection.Writer.QuarantinedCount` reads an atomic kept beside the quarantine map.
4. **fsync latency.**
   - `ketju_threadlog_fsync_duration_seconds{file="log|index"}`, buckets 0.1 ms to 1 s.
   - `threadcache.Options.SyncObserver` wraps the actor's log and index writers in types that time `Sync`, so `threadlog.NewActor` and all its callers are unchanged. The actor calls `Sync` only where its policy fsyncs, so under `fsync = "never"` nothing is observed.
5. **Projection lag.**
   - The gauge `ketju_projection_lag_bytes`: one `ListProjectionLengths` read on the read pool, then the `threads/` walk (`threadid.WalkLogs`) for every log's size. Each thread adds its log size minus its projected length when positive; a thread with no row counts from the end of its file header, as catch-up does. A quarantined thread's log is shorter than its projection, so it adds nothing; a log that is gone adds nothing.
   - `ketju_threads_quarantined` from `QuarantinedCount`.
   - A result younger than 10 s is reused.
   - From `Bus.Stats`: `ketju_events_queued{subscriber}` and `ketju_events_dropped_total{subscriber}`.
6. **Process.** `ketju_goroutines` and `ketju_heap_bytes` (`runtime/metrics`). No `ketju_build_info`: `internal/buildinfo` (TKT-01M29CXK3S) is not on main at claim.
7. **Exposure, as the maintainer decided.**
   - `metrics_listen` in `config.toml`, `KETJU_METRICS_LISTEN`, validated as `listen` is, and refused when equal to `listen`.
   - `serve` binds it before the main listener (an address in use stops the start), serves `Registry.Handler`, and stops it first at shutdown, before the stores. A failure while serving is logged and the forum goes on.
   - The main listener registers `/metrics` as a 404 problem, so it is neither the metrics nor the shell.
   - `internal/metrics/collect` holds the ketju families and the projection lag, so `internal/metrics` stays generic.
8. **Docs.** docs/02 "Observability" gains "The metrics": a table of every metric (name, type, unit, labels, meaning) and the label value sets. The README gains "Metrics". The config-check golden gains `metrics_listen`.

### Tests

- `internal/metrics`: `TestTextExposition` (golden, parsed back), `TestHistogramConcurrent`, `TestRegisterRefusesBadNames`, `TestHandlerServesOnlyMetrics`.
- `TestMetricNamesFollowConventions` (collect): the registered names equal the docs/02 table's, with its types and labels, and each follows the name rules and carries its unit.
- `TestRequestLatencyLabelsArePatterns` and `TestRequestLatencyLabelsAreBounded` (web), and `TestMethodAndCodeLabels`.
- `TestMetricsCarryNoIdentifiers` (cmd/ketju): a signed-in session that starts a thread, replies, reads it, opens a profile, lists a forum and searches; the scrape names none of the id, handle, slug, title or query.
- `TestProjectionLagSumsGaps` and `TestFsyncLatencyObserved` (collect).
- `TestScrapeTakesNoRequestPathLock` (collect) runs scrapes beside appends, opens, cache lookups and publishes under -race. That each hook takes no lock is proven where the lock is, by a test that holds it: threadcache `TestStatsTakeNoLock`, respcache and render `TestCountersTakeNoLock`, sse `TestStatsTakeNoLock`, events `TestStatsTakeNoLockOnceStarted`, projection `TestQuarantinedCountTakesNoLock`.
- `TestMetricsGuard` (cmd/ketju): `/metrics` on `metrics_listen` serves the metrics and nothing else; the main listener answers 404 and never the metrics; the metrics listener stops before the stores close; an empty key starts no listener. `TestMetricsGuardOnTheMainListener` (web) and `TestMetricsListenRefusesTheMainAddress`.
- `/healthz`'s goldens still pass unchanged.

### Out of scope

- Alerting rules and dashboards. They belong to the operator, and no ticket owns them.
- A backup metric (TKT-01M29CXK35 out of scope).
- Per-thread or per-forum metrics. They are left out on purpose, because a label of either would name content.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:03Z

Open decision for the maintainer (docs/14 stop class 1, security posture): how `/metrics` is guarded. docs/02 says "an admin token or bind address" without choosing, and docs/05 and docs/07 give no rule. The plan lists four options with what each means, and it chooses none. The ticket stays in draft until the maintainer answers. Steps 1 to 6 and 8 of the plan do not depend on the answer, so if the answer is slow, the collectors could be split off and built first.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T18:20:07Z

Maintainer decision, 2026-10-02, on the open decision: option 1, a separate bind address. Asked as a choice, the maintainer picked "Separate bind address (Recommended)", whose description was: a new `metrics_listen` config key (for example `127.0.0.1:9090`) with its own listener that serves only `/metrics`, unauthenticated, off when the key is empty, and the main site never serves `/metrics`. Written into docs/02 "Observability". Step 7 of the approach and criterion 1 follow it:
- `/metrics` answers only on `metrics_listen`;
- the main listener answers 404 for it;
- an empty key starts no listener.
The ticket can now be promoted.

## Summary

Landed `/metrics` as the maintainer decided: a `metrics_listen` key (and `KETJU_METRICS_LISTEN`) starts a second listener that serves `/metrics` and nothing else, unauthenticated; an empty key, the default, serves none; the main listener answers `/metrics` with a 404 problem.

- `internal/metrics`: a hand-written text exposition (counters, gauges, labelled histograms, scrape-time callbacks), lock-free on the request path; bad names panic at registration.
- `internal/metrics/collect`: the ketju families and projection lag (one read-pool statement plus the `threads/` walk, reused for 10 s).
- `internal/web`: request latency by innermost route pattern, a fixed method set with `other`, and the status class; `/events` is left out.
- Lock-free hooks: respcache and render `Counters()`, threadcache `Stats`/`Len`, sse `Hub.Stats`, events `Bus.Stats` after `Start`, projection `QuarantinedCount`, each proven by a test that holds the lock.
- fsync latency through `threadcache.Options.SyncObserver`, which wraps the actor's writers; `threadlog` is unchanged.
- docs/02 "Observability" gains the metrics table and label sets; the README gains "Metrics".

Every criterion is ticked. Mutations tried: labelling by path, passing custom methods through, and locking `respcache.Counters`; each was caught.
