---
schema: 3
id: TKT-01M29CCBZNKH5CCCT3VY3GC9V5
title: Add the single-process lock file
type: task
status: done
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
  - ref: design:single-process
    path: docs/02-architecture.md
  - ref: pr:66
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T15:14:09Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

A lock file in the data directory that refuses a second `serve` against the same data.

The entire storage design assumes one writer per log file. Two processes sharing a data directory would corrupt threads quietly, and the operator who does it by accident during a botched deploy deserves a refusal rather than a rescue. A stale lock from a killed process has to be detectable, or the refusal becomes the outage.

## Acceptance criteria

- [ ] A second serve against the same data directory exits non-zero naming the holding process
- [x] A lock left by a killed process is detected as stale and does not require manual deletion
- [ ] The lock is released on graceful shutdown
- [x] The refusal message tells the operator what to check rather than only that it failed

## Implementation plan

Revised at claim (2026-09-24), after reading the code. The pre-groom approach stands; what changed is recorded here.

Decided: a lock file in `data/` refuses a second `serve`, and the process stays single-process (docs/10 item 24, README; TKT-01M29BW9PB is done).

### Approach
1. New package `internal/lock`: `Acquire(dir, purpose) (*Lock, error)`, `(*Lock).Release() error`, `HeldError{Dir, Holder, ReadErr}`, `Holder{pid, host, purpose, started_at, version}`. The file is `<data_dir>/ketju.lock`.
2. An exclusive, non-blocking flock through `syscall.Flock`, with EINTR retried. The kernel drops it when the holder dies, so a killed holder leaves nothing stale and there is no pid probing.
3. The file is opened without O_TRUNC, so a refused open can still read the holder. After locking, it is truncated, the JSON record is written, and it is fsynced.
4. On EWOULDBLOCK the refusal reads the record and returns `*HeldError`. The message names the directory, pid, host, and start time, and what to check: ps, systemctl status, docker ps, and a network filesystem. If the record is unreadable, it says so and still refuses.
5. Release unlocks and closes, and leaves the file in place. Removing the file would race a process that has already opened the old inode.
6. `lock_unix.go` (`//go:build unix`) holds the flock. `lock_other.go` refuses on every other platform with a clear message.
7. Acquire does not create the data directory. Creating it is serve's decision (CCBYX), and a lock taken in a directory a typo just created protects nothing.
8. Version: the binary has no version string yet, so the record carries `debug.ReadBuildInfo().Main.Version`, which is "(devel)" in a local build.
9. No new exit code. A refusal maps to exitInternal (4) through `exitCodeFor` until serve decides otherwise; the AC asks only for non-zero. Deciding the code belongs with serve's startup failures (CCBYX AC3).

### Tests
`internal/lock/lock_test.go`. A second process is this test binary re-run with `KETJU_LOCK_HELPER` set.
- AC1: the second process's refusal is non-zero and names the pid; an in-process second open is refused as a `*HeldError` carrying the record.
- AC2: the helper holds the lock and is killed with SIGKILL; Acquire then succeeds, the file is not deleted, and the record is rewritten.
- AC3: after Release the lock can be taken again, by another process and by this one, and the file is still there.
- AC4: a golden (`testdata/held.golden`, `-update`) holds both messages, with the holder named and with the record unreadable.
- Also covered: an unreadable record still refuses, a longer old record leaves no tail, and a missing directory is not created.

### Interactions
Consumed by CCBYX (serve), first after config load, released last; the serve-level checks for AC1 and AC3 are its to carry. Also consumed by C5Y25 (reindex), with purpose "reindex". `ketju fsck --rebuild` does not take the lock, as planned.

### Design
docs/10-open-questions.md "Operations" item 24. docs/02-architecture.md "One process per data directory". README "One process per data directory". docs/03-thread-storage.md "Write path" (exactly one writer per file).

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:08:15Z

Built internal/lock with tests. go test -race passes, and so does go vet (also under GOOS=windows, which uses the refusing stub). A darwin build succeeds.

Mutation check, 10 mutants run and 10 killed. Each was a single change to the lock code:
- adding O_TRUNC to the open;
- dropping the Truncate;
- removing the file in Release;
- dropping the holder read on refusal;
- dropping the unreadable-record sentence;
- hard-coding the purpose (this one survived the first run, and TestRecordIsTheHolder now acquires as "reindex");
- zeroing the pid;
- returning the raw EWOULDBLOCK;
- taking a shared lock instead of an exclusive one;
- removing the double-release guard.
Not run, as equivalent: making unlock a no-op in Release, because closing the descriptor drops the flock anyway.

AC1 and AC3 are left unticked. Both name serve ("a second serve … exits non-zero", "released on graceful shutdown"), and serve does not exist yet. At the lock level both are proven with a real second process: the refusal exits non-zero naming the pid, and after Release the lock can be taken by another process. The serve-level halves are recorded on TKT-01M29CCBYX (Implement ketju serve with graceful shutdown), whose plan already puts them in its serve test.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:14:09Z

Answered review 433 on PR #66.

- **High, a symlinked lock file.** Acquire truncates and rewrites what it opens, so a symlink planted at ketju.lock would aim that rewrite at another file. The open now happens in openFile (lock_unix.go), which:
  - uses O_NOFOLLOW, so a symlink is refused, and a dangling one creates nothing;
  - then fstats the opened file and refuses anything but a regular file with one link. A fifo is refused as not a plain file; a directory is already refused by the open. The link check matters because a hard link is the same attack without a symlink.
  Each refusal says what it found and is not a HeldError, so the operator is not sent looking for a process. TestLockFileMustBeItsOwn covers a symlink, a dangling symlink, a hard link, a fifo and a directory, and checks the victim file is untouched.
- **Medium, the test holder.** It used a bare select{}, which with no other goroutine can die as a deadlock before the test kills it. It now sleeps for an hour instead.

Mutation check: 14 of 14 killed. That is the 10 from before (the O_TRUNC one moved to the new open) plus 4 on the new checks:
- dropping O_NOFOLLOW;
- dropping the regular-file check (killed once the test required the refusal wording; before that a fifo failed later, at Truncate, which made the mutant equivalent in outcome);
- dropping the link count check;
- dropping the symlink wording.
go test -race -count=30 -cpu 1,4 ./internal/lock passes.

CI run 277 failed in internal/store/threadlog, TestQueuedAppendsShareOneFsync ("17 appends cost 1 log fsyncs, want 2"). That is the known flake in draft TKT-01M393S6P7 (Order the group-commit fsync test so its first append is in flight), in a package this PR does not touch. internal/lock passed in that run.

## Summary

internal/lock: an exclusive flock on data/ketju.lock with a JSON record naming the holder (pid, host, purpose, start, version). A killed holder leaves nothing stale, because the kernel drops the flock. A refusal is a *HeldError whose message says what to check. Release leaves the file in place. Non-unix platforms refuse. AC2 and AC4 are met here. AC1 and AC3 are met at the lock level with a real second process, and are left unticked because they name serve; the serve-level checks are carried by TKT-01M29CCBYX.
