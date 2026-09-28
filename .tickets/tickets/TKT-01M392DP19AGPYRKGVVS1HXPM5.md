---
schema: 3
id: TKT-01M392DP19AGPYRKGVVS1HXPM5
title: Report the ncruces directory-fsync bug and drop ketju's workaround
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:db
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBKV6HH8W0DN6QXQNZ0R
origin: null
dependencies:
  - TKT-01M29BW9Q17KEBMHJ91G6QHW36
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T06:42:36Z
updated_at: 2026-09-28T05:02:26Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

ncruces/go-sqlite3 v0.35.6, and upstream main as of 2026-09-24, has a bug in `vfs/file.go`, `(*vfsFile).Sync`. When SQLite requests a directory sync (`_FLAG_SYNC_DIR`, set when it creates a WAL or journal file), the method opens the parent directory and then calls `osSync(f.File, ...)` a second time instead of `osSync(d, ...)`. The directory entry is never made durable. `Delete` in the same package syncs its directory handle correctly.

### Consequence for ketju

The database holds authoritative tables (docs/04). If the machine loses power after the `-wal` file is created and before the directory entry reaches disk, the WAL can disappear, and with it every transaction committed to it and not yet checkpointed.

### Where the workaround lives

The workaround is **not** in this ticket. It is an acceptance criterion of TKT-01M29BW9Q1: the opener fsyncs the data directory once the WAL exists, before the first authoritative write. It moved there after the Terva review of PR #48 (review 364, high finding) pointed out that a draft nobody depends on is not a prerequisite. No authoritative write can land before W9Q1 does.

### What this ticket is for
- Report the bug upstream. That is an outside report, so a person makes it or approves it.
- Once a fixed ncruces release is pinned, remove the workaround and its test from `internal/store/db`, and say so in docs/04.
- Decide whether later WAL recreation needs the same treatment, for example deletion on last close followed by reopening. W9Q1's opener covers process start, and one process per data directory means the WAL lives as long as the process does.

Found by TKT-01M29BSPE8, the SQLite driver spike.
