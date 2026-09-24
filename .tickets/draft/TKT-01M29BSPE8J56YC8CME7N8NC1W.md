---
schema: 3
id: TKT-01M29BSPE8J56YC8CME7N8NC1W
title: Decide the pure-Go SQLite driver
type: spike
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-24T05:21:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Question 6 in docs/10, to be decided at the start of Phase 1.

The lean is `github.com/ncruces/go-sqlite3` for speed and for Go FTS5 tokenizers, which matter later for multi-language stemming. The risk named in docs/09 is that it is pre-1.0 and its API may churn.

The mitigation is already a design constraint: schema and SQL stay driver-agnostic so modernc is a drop-in. This spike is partly about confirming that constraint is real, by checking that nothing we need from ncruces is unavailable in modernc.

## Acceptance criteria

- [ ] Both drivers are benchmarked on the write path the projection writer will use, and the numbers are recorded here
- [ ] The FTS5 story is confirmed for the chosen driver, including whether custom tokenizers are reachable
- [ ] Any API surface used from the chosen driver that modernc lacks is listed, so the fallback cost is known rather than assumed

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

The driver is already decided as ncruces (docs/10 item 6, 2026-09-24). This spike confirms the choice and records numbers. It does not reopen the choice. It stops for a human only if ncruces is more than 2x slower than modernc on the projection write path.

### Approach
1. Create `spikes/sqlite-driver/` as a separate module, the same way `spikes/format-decisions/` is one, so neither driver enters the main go.mod before BW9Q1 adds ncruces. Pin current releases of both drivers.
2. Write a draft projection schema that matches docs/04 "Projection tables": `threads`, `post_meta` with both indexes, `mentions`, `thread_tags`, `projection_state`. Keep it throwaway; BYYJ8 owns the real schema.
3. Benchmark the write shape the projection writer (C2WW5) will use. Each unit is one `BEGIN IMMEDIATE` transaction per thread batch. It upserts a `threads` row, inserts N `post_meta` rows and their mentions, and advances `projection_state`. Run with batch sizes 1, 16, and 256 and with 1, 4, and 8 connections. Every run uses WAL and an explicit busy_timeout. Use only `database/sql` for both drivers, so the benchmark also demonstrates the driver-agnostic SQL.
4. FTS5: in ncruces, create an `fts5` table over `(thread_id, ordinal, revision, title, source_plain)` (docs/04 "search (FTS)"), insert, and run a MATCH query with `snippet()`. Then register a custom tokenizer written in Go through the ncruces API and confirm that a query uses it. Record the package path and function used.
5. List every ncruces-specific call the spike needed: DSN parameters, tokenizer registration, and any `driver.Conn` raw access. For each one, say whether modernc has an equivalent. This list is the fallback cost.
6. Record the ns/op table, the machine, and the Go version on the ticket. Write a README in the spike that states the decision and the numbers. Update docs/01 "SQLite" row and docs/10 item 6 only if the numbers contradict them.

### Files and packages
New: `spikes/sqlite-driver/{go.mod,bench_test.go,fts_test.go,README.md}`. Touched: `.forgejo/workflows/ci.yml` gets a build-and-vet step for the new spike module, following the existing "build the format spike" step. No code under `internal/`.

### Tests
- AC1: `go test -bench` in the spike, on a named machine rather than the CI runner, because the CI step already says runner numbers mean nothing. Numbers go to the ticket. If ncruces is more than 2x slower, stop and block the ticket.
- AC2: `fts_test.go` asserts that the FTS5 table exists, that MATCH returns the row, and that the custom tokenizer's effect is observable. One example: a stemming tokenizer matching "running" to "run".
- AC3: the README table of ncruces-only surface, each row naming the modernc equivalent or "none".

### Design
docs/01-research.md SQLite driver table (line 13 area). docs/04-data-model.md opening "Engine" paragraph, plus "search (FTS)". docs/09-roadmap.md "Risks and mitigations", the ncruces row. docs/10-open-questions.md "Database" item 6.

### Interactions
Unblocks BW9Q1, which adopts the DSN and pragma set proven here. The FTS findings feed TKT-01M29CRJ3B, the FTS5 search index, in Phase 3. If a custom tokenizer is ncruces-only, CRJ3B inherits that fallback cost.
