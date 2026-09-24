---
schema: 3
id: TKT-01M29CCBZNKH5CCCT3VY3GC9V5
title: Add the single-process lock file
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9PB11V2AHE52C0F49JG
blocks_on: none
references:
  - ref: design:open-questions-ops
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:21:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

A lock file in the data directory that refuses a second `serve` against the same data.

The entire storage design assumes one writer per log file. Two processes sharing a data directory would corrupt threads quietly, and the operator who does it by accident during a botched deploy deserves a refusal rather than a rescue. A stale lock from a killed process has to be detectable, or the refusal becomes the outage.

## Acceptance criteria

- [ ] A second serve against the same data directory exits non-zero naming the holding process
- [ ] A lock left by a killed process is detected as stale and does not require manual deletion
- [ ] The lock is released on graceful shutdown
- [ ] The refusal message tells the operator what to check rather than only that it failed

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

Decided: a lock file in `data/` refuses a second `serve`. The process stays single-process (docs/10 item 24, README). The dependency TKT-01M29BW9PB is still draft, but its decision is recorded in docs/10 item 24 in the working tree. Confirm it has been committed before claiming this ticket.

### Approach
1. New package `internal/lock`: `lock.Acquire(dataDir, purpose string) (*Lock, error)` and `(*Lock).Release() error`. The lock file is `data/ketju.lock`.
2. Mechanism: open or create the file, then call `flock(fd, LOCK_EX|LOCK_NB)` through `syscall.Flock`, which is pure Go with no cgo on Linux and the BSDs. The kernel drops a flock when the holding process dies, however it dies. So a lock left by a killed process is simply not held, and the next Acquire succeeds with no staleness heuristic and no manual deletion. The file stays on disk, and its contents are informational.
3. After locking, truncate the file and write JSON: `{pid, host, purpose ("serve"|"reindex"), started_at, version}`, then fsync. The holder is named from this file, and the lock state comes from flock.
4. On `EWOULDBLOCK`, read the JSON and return `*lock.HeldError{Holder}`. Its message: "data directory /x is in use by ketju serve (pid 1234 on host h, started 2026-…). Check for another running ketju (`ps`, `systemctl status`, `docker ps`). Two processes writing the same thread logs would corrupt them. If nothing is running, check whether the data directory is on a network filesystem, where locks may not work." If the file is unreadable, say so and still refuse.
5. Release: `LOCK_UN`, then close. Leave the file in place; deleting it would race a second process that has already opened it.
6. Platforms: `lock_unix.go` has `//go:build unix`. On other platforms a stub refuses to start with a clear message. The docs name no target platforms, so this is an open item rather than a decision.
7. Serve (CCBYX) acquires the lock first, right after config load and before opening the DB or any thread, and releases it last. Reindex (C5Y25) acquires it too, with purpose "reindex".

### Files and packages
New: `internal/lock/{lock.go,lock_unix.go,lock_other.go,lock_test.go}`. Touched by consumers only: `cmd/ketju/serve.go` (CCBYX) and `cmd/ketju/reindex.go` (C5Y25). `exit.go` may gain a distinct exit code for "in use"; decide at claim.

### Tests
- AC1: `TestSecondAcquireRefusedNamingHolder` acquires in the test, then re-execs the test binary as a helper process (the standard `os/exec` self-exec pattern) that tries to acquire. The helper exits non-zero, and stderr contains the first pid. An in-process second Acquire on a separate fd also fails, because flock is per open file description.
- AC2: `TestKilledHolderIsNotStale` starts the helper, has it acquire and print ready, then SIGKILLs it. The parent's Acquire succeeds with no file deleted.
- AC3: `TestReleaseAllowsReacquire`. The serve-level check ("released on graceful shutdown") goes in the CCBYX serve test: after SIGTERM and exit, a new Acquire succeeds.
- AC4: `TestHeldMessageSaysWhatToCheck` asserts the message names the directory, pid, host, and the three things to check. Use a golden test with `-update`.

### Design
docs/10-open-questions.md "Operations" item 24. docs/02-architecture.md, where the single-process posture is recorded per TKT-01M29BW9PB. The README paragraph "One process per data directory". docs/03-thread-storage.md "Write path" (exactly one writer per file).

### Interactions
Defines `lock.Acquire`, `Lock.Release`, and `HeldError`, consumed by CCBYX and C5Y25. `ketju fsck --rebuild` does not take the lock: its help text claims it is safe against a live directory. Leave that as it is.
