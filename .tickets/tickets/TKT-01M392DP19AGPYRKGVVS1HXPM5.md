---
schema: 3
id: TKT-01M392DP19AGPYRKGVVS1HXPM5
title: Report the ncruces directory-fsync bug and drop ketju's workaround
type: bug
status: blocked
status_reason: "Needs a person: post the upstream report (text in a note; the run may not publish to an external service), then wait for an ncruces release that fixes vfsFile.Sync. v0.35.6 and main 6eeb006 still have the bug, so the workaround stays."
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
updated_at: 2026-09-28T14:09:44Z
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

## Implementation plan

Read on 2026-09-28: ketju pins ncruces/go-sqlite3 v0.35.6, still the newest release (proxy.golang.org version list). Upstream main's `vfs/file.go` still has `osSync(f.File, ...)` in the `_FLAG_SYNC_DIR` branch of `(*vfsFile).Sync` (last commit to the file 6eeb006, 2026-08-25). No upstream issue or PR mentions it (GitHub issue search for "sync directory" and "fsync" in the repo). So no fixed release exists and the workaround stays.

1. Keep the workaround exactly as W9Q1 left it: `syncDir` after `ensureWAL` in `Open`, `FCNTL_PERSIST_WAL` in `openPool`, and TestOpenSyncsDirectoryAfterWAL. No fsync is removed or weakened.
2. Pin the bug: `internal/store/db/driver_test.go` gains TestNcrucesDirSyncWorkaroundIsPinned, which reads the test binary's build info and fails on any ncruces version (or a replace) not in a list of versions read and found buggy. Upgrading becomes a decision: read `Sync`, then either extend the list or drop the workaround per this ticket. The test compares a module path string and imports no driver, so the one-driver-file rule holds.
3. docs/04 says upstream had no fix as of v0.35.6 and that the pin exists.
4. Write the upstream report text in a note for a person to post. Posting it is an external publication, which docs/12 does not let the run do.
5. WAL recreation: settled by W9Q1. FCNTL_PERSIST_WAL keeps the -wal file across the last close, and one process per data directory means the WAL is created once, at Open, where the directory is fsynced. TestOpenSyncsDirectoryAfterWAL checks the file survives Close.
6. Block the ticket: its remaining work (post the report, wait for a fixed release, drop the workaround) needs an external service and an upstream release.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:08:39Z

Upstream report text, ready for a person to post at https://github.com/ncruces/go-sqlite3/issues/new. Not posted: docs/12 does not let the run publish to an external service. Checked on 2026-09-28 that no existing issue or PR covers it and that main is unchanged.

---

Title: vfs: (*vfsFile).Sync syncs the file twice instead of its directory

In `vfs/file.go`, when `_FLAG_SYNC_DIR` is set (on Unix, for a WAL or journal file that `OpenFilename` created), `(*vfsFile).Sync` opens the parent directory as `d` and then syncs `f.File` a second time:

```go
d, err := os.Open(filepath.Dir(f.File.Name()))
if err != nil {
	return nil
}
defer d.Close()
err = osSync(f.File, f.flags, flags)   // should be d
```

The directory handle is opened and closed but never synced, so the directory entry for a new `-wal` or `-journal` file is not made durable. After a power loss, the WAL file can be missing even though transactions committed to it reported success at `synchronous=FULL`. `Delete` in the same file syncs its directory handle correctly (`osSync(f, 0, SYNC_FULL)`).

Present in v0.35.6 and on main at 6eeb006.

A likely fix is to sync the directory, as `Delete` does:

```go
err = osSync(d, 0, flags)
```

We work around it by fsyncing the database directory ourselves once the WAL exists and by setting `SQLITE_FCNTL_PERSIST_WAL`, so the WAL is not recreated later.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:09:36Z

in-progress to blocked: Needs a person: post the upstream report (text in a note; the run may not publish to an external service), then wait for an ncruces release that fixes vfsFile.Sync. v0.35.6 and main 6eeb006 still have the bug, so the workaround stays.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:09:44Z

Blocked rather than closed: every remaining step (post the report, a fixed upstream release, dropping the workaround) is this ticket's own description, so a new draft would duplicate it. No draft filed. Ticket left with no acceptance-criteria section, as it was filed; nothing ticked. Mutation tried: listing only v0.35.5 as checked made TestNcrucesDirSyncWorkaroundIsPinned fail naming v0.35.6; reverted.

## Summary

Partly done, then blocked on outside action. Upstream ncruces/go-sqlite3 has no fix: v0.35.6 is still the newest release and main's `(*vfsFile).Sync` still syncs `f.File` where it should sync the directory handle. No upstream issue tracks it. The workaround in `internal/store/db` (directory fsync after the WAL exists, FCNTL_PERSIST_WAL) therefore stays unchanged, and no fsync was removed or weakened.

Landed: TestNcrucesDirSyncWorkaroundIsPinned in `internal/store/db/driver_test.go` fails on any ncruces version or replace nobody has read for the bug, so an upgrade has to check `Sync` and then either extend the list or drop the workaround. docs/04 records the upstream state and the pin. The upstream report text is in a note, ready for a person to post. WAL recreation needs nothing new: W9Q1's FCNTL_PERSIST_WAL keeps the -wal file across the last close, and TestOpenSyncsDirectoryAfterWAL checks that.

Remaining: a person posts the report. Once a fixed release exists, pin it in go.mod, run the SQLite driver spike, remove the workaround, keep a test that the directory is still fsynced once the WAL exists, and update docs/04.
