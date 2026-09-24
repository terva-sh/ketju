---
schema: 3
id: TKT-01M392DP19AGPYRKGVVS1HXPM5
title: Work around the ncruces VFS skipping the directory fsync
type: bug
status: draft
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
updated_at: 2026-09-24T06:42:44Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

ncruces/go-sqlite3 v0.35.6, and upstream main as of 2026-09-24, has a bug in `vfs/file.go`, `(*vfsFile).Sync`. When SQLite requests a directory sync (`_FLAG_SYNC_DIR`, set when it creates a WAL or journal file), the method opens the parent directory and then calls `osSync(f.File, ...)` a second time instead of `osSync(d, ...)`. The directory entry is never made durable. `Delete` in the same package syncs its directory handle correctly.

### Consequence for ketju

The database holds authoritative tables (docs/04). If the machine loses power after the `-wal` file is created and before the directory entry reaches disk, the WAL can disappear, and with it every transaction committed to it and not yet checkpointed. The window is small: it opens only when a WAL file is created, at first open or after the WAL is deleted on last close. It is still a durability hole, and the same hole already exists in the thread log path.

### Options
- The workaround in ketju: after `internal/db` opens the database and runs its first write, fsync the data directory once. The WAL file stays in place for the life of the process, so this covers it. Whether any later WAL recreation, such as `journal_size_limit` truncation to zero, re-opens the window is for TKT-01M29BW9Q1 to decide.
- Report it upstream, which a person should do or approve, and drop the workaround once a fixed release is pinned.

Found by TKT-01M29BSPE8, the SQLite driver spike.
