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
updated_at: 2026-09-11T23:10:45Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
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
