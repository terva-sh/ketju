---
schema: 3
id: TKT-01M29BN36Q9D32WQGYPFS336J0
title: "Operations: config, CLI, observability, and release"
type: epic
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:L
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: children
references:
  - ref: design:architecture-ops
    path: docs/02-architecture.md
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-11T23:32:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`cmd/ketju` and `internal/config`: what makes `./ketju serve` and a data directory the whole deployment.

The target operator is one person on a VPS or a home server, so ops burden must be close to zero. No cgo, no external database server, no Redis, no job runner. All background work is in-process goroutines: the projection writer, search indexer, notification generator, session sweeper, blob GC, index flush, and discovery refresh.

Configuration splits on a clear line. `config.toml` holds what must be readable before the database exists (bootstrap, providers, paths, listen address, fsync policy, cache sizes), and the `settings` table holds what admins change at runtime. Secrets load from files for container deployments, and `KETJU_*` environment variables override.

Backups are `rsync` of the data directory plus a consistent database snapshot. Recovery of content is `ketju reindex`, and content never needs restoring from a database backup.

## Acceptance criteria

- [ ] A single static binary with no cgo runs the whole system given only a data directory
- [ ] `ketju config check` validates the config and prints the effective values with secrets redacted
- [ ] Graceful shutdown stops accepting, drains in-flight requests within 10 s, flushes every thread actor's index header, and closes the database
- [ ] A second `serve` against the same data directory is refused by a lock file
- [ ] The documented backup procedure restores a working instance, with content coming from the logs rather than a database backup
