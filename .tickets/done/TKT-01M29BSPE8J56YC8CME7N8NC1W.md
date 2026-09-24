---
schema: 3
id: TKT-01M29BSPE8J56YC8CME7N8NC1W
title: Decide the pure-Go SQLite driver
type: spike
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:db
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-db
    path: docs/10-open-questions.md
  - ref: pr:48
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-24T06:54:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

Question 6 in docs/10, to be decided at the start of Phase 1.

The lean is `github.com/ncruces/go-sqlite3` for speed and for Go FTS5 tokenizers, which matter later for multi-language stemming. The risk named in docs/09 is that it is pre-1.0 and its API may churn.

The mitigation is already a design constraint: schema and SQL stay driver-agnostic so modernc is a drop-in. This spike is partly about confirming that constraint is real, by checking that nothing we need from ncruces is unavailable in modernc.

## Acceptance criteria

- [x] Both drivers are benchmarked on the write path the projection writer will use, and the numbers are recorded here
- [x] The FTS5 story is confirmed for the chosen driver, including whether custom tokenizers are reachable
- [x] Any API surface used from the chosen driver that modernc lacks is listed, so the fallback cost is known rather than assumed

## Implementation plan

Revised at claim, 2026-09-24, against main at ac05a3c. Nothing under internal/ touches SQL yet, so the pre-groom plan stands with these changes:

1. `spikes/sqlite-driver/` is a separate module (`ketju.spike/sqlitedriver`) that pins ncruces v0.35.6 and modernc v1.59.0. All code lives in `_test.go` files, plus `doc.go` so the package builds.
2. Both drivers get **one** DSN query string: `_txlock=immediate` plus `_pragma` for WAL, `busy_timeout(5000)` and `foreign_keys(on)`. Both drivers parse those keys, so the connection setup is driver-agnostic as well as the SQL. `TestSharedDSNIsHonoured` proves it for each driver: it reads the pragmas back, then confirms that an open, unwritten transaction already blocks a second `BEGIN IMMEDIATE`.
3. `BenchmarkProjectionWrite` sweeps driver × batch {1, 16, 256} × conns {1, 4, 8}. One op is one committed transaction that upserts `threads`, inserts N `post_meta` rows, adds a mention for one post in four, and advances `projection_state`. It reports ns/post. synchronous stays at SQLite's default (FULL). Scratch files go to `$HOME/.cache/ketju-bench/sqlite-driver`, and tmpfs is refused, following the threadlog benchmark.
4. `TestFTS5WithGoTokenizer` runs on ncruces. It registers a Go tokenizer through `fts5.RegisterCustom` and `API.CreateTokenizer`, and checks that MATCH hits a stemmed form and that `snippet()` works. A unicode61 control shows the match comes from the tokenizer. `TestFTS5OnModernc` confirms FTS5 and the built-in porter tokenizer on the fallback driver, and that an unregistered tokenizer name fails loudly.
5. The README records the numbers and the ncruces-only surface, together with the modernc equivalent of each item or "none".
6. CI gets one step inside Build and Test that vets the spike, runs its tests, and runs the benchmark once at 1x. The tests are assertions, so unlike the format spike they run in CI.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T06:51:08Z

### Benchmark results, 2026-09-24

AMD Ryzen 9 5950X, ext4 on LVM, Go 1.27.1, ncruces v0.35.6 and modernc v1.59.0, both on SQLite 3.53.4. The command is `go -C spikes/sqlite-driver test -run '^$' -bench . -benchtime=1s -count=3`. Each figure is the median of three runs, in µs per post. One op is one transaction per thread batch: an upsert of `threads`, N `post_meta` rows, a mention on one post in four, and an advance of `projection_state`.

Single writer connection, which is the configuration TKT-01M29BW9Q1 plans:

| sync | batch | ncruces | modernc |
|------|------:|--------:|--------:|
| full | 1     | 645     | 804     |
| full | 16    | 52.9    | 87.4    |
| full | 256   | 18.4    | 22.3    |
| off  | 1     | 92.8    | 115     |
| off  | 16    | 18.7    | 20.3    |
| off  | 256   | 10.0    | 10.1    |

Sync floor on the same disk, for a 4 KiB overwrite: fsync 559 µs, fdatasync 467 µs.

**Verdict: ncruces is not more than twice as slow anywhere on the single-writer path, so the decision stands and the run does not halt.** With sync=off, ncruces runs at 0.71× to 1.20× of modernc across every batch size and across 1, 4 and 8 connections.

Three findings go beyond the criteria:
1. ncruces syncs with fdatasync. modernc is built without HAVE_FDATASYNC, so it uses a full fsync. That accounts for most of the sync=full gap.
2. With 4 or 8 writer connections, ncruces commits starved for seconds without returning SQLITE_BUSY. In a five-run rerun, 8 connections at batch 1 had a median of 225 ms per post, against 1.9 ms on modernc. The cause is ncruces's `Xgo_busy_timeout`: a random retry sleep of 0 to 2 ms with no fairness. Both drivers also returned a rare SQLITE_BUSY after the 5 s timeout. **TKT-01M29BW9Q1's single-connection write pool is therefore required, not optional.** The rest of the run should treat it that way. The first full run exposed this: a hard `database is locked` on ncruces at batch 256 with 8 connections, and that run was discarded.
3. `vfs/file.go` in ncruces syncs the file where it should sync the directory. Filed as the draft TKT-01M392DP19.

The raw output stays on the benchmark machine at `~/.cache/ketju-bench/bspe8-bench.txt` and `bspe8-rerun.txt`. The README in the spike carries the table and the fallback-cost list.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T06:54:12Z

Terva review 364 (run 39a0d3c3-51d1-4850-93f6-dab39b9e7652, Actions run 111), head 82b168b109ba558b984d5c5b600afba2c36f500e, base ac05a3caa3d2bfe29b47b0961d6fb1445bfb337a: one high finding, 'Do not treat the chosen driver as durable before the directory-fsync gap is addressed'. **Accepted and fixed.** The directory-fsync workaround is now an acceptance criterion of TKT-01M29BW9Q1, which writes the opener, so no authoritative write can land without it. docs/04 'Engine' and the spike README state that ncruces is conditional on it. The draft TKT-01M392DP19 is narrowed to the upstream report and removing the workaround later. A follow-up review of the new head is requested.

## Summary

ncruces stands, on condition that ketju works around its missing directory fsync; that workaround is now an acceptance criterion of TKT-01M29BW9Q1, before any authoritative write. On the single-writer projection write path ncruces matched or beat modernc at every batch size, with sync on and off; with the disk taken out it runs at 0.71× to 1.20× of modernc, well inside the 2× halt rule. FTS5 and Go tokenizers are confirmed through ext/fts5 RegisterCustom/CreateTokenizer; modernc has FTS5 and porter but no Go tokenizer API, which is the real fallback cost (listed in spikes/sqlite-driver/README.md). Also found: modernc syncs with fsync where ncruces uses fdatasync, and several ncruces writer connections starve each other, so W9Q1's single write connection is required (docs/04 updated). The draft TKT-01M392DP19 carries the upstream report. CI vets the spike, runs its tests, and runs the benchmark once.
