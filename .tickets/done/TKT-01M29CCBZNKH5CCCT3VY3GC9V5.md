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
updated_at: 2026-09-24T17:18:09Z
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

- [x] A second serve against the same data directory exits non-zero naming the holding process
- [x] A lock left by a killed process is detected as stale and does not require manual deletion
- [x] The lock is released on graceful shutdown
- [x] The refusal message tells the operator what to check rather than only that it failed

## Implementation plan

Revised at claim (2026-09-24), after reading the code; revised again for review 443, which moved the flock from the file to the directory.

Decided: a lock in `data/` refuses a second `serve`, and the process stays single-process (docs/10 item 24, README; TKT-01M29BW9PB is done).

### Approach
1. New package `internal/lock`: `Acquire(dir, purpose) (*Lock, error)`, `(*Lock).Release() error`, `HeldError{Dir, Holder, ReadErr}`, `Holder{pid, host, purpose, started_at, version}`.
2. The lock is an exclusive, non-blocking flock on the data directory itself, opened O_RDONLY|O_DIRECTORY, with EINTR retried. It is not on a file in the directory, because a file's inode can be deleted or replaced under its holder, and the next process would lock the new one (review 443). The kernel drops the flock when the holder dies, so a killed holder leaves nothing stale.
3. `<data_dir>/ketju.lock` is only the record of who took the lock. Once the directory is locked, it is opened with O_NOFOLLOW, refused unless it is a regular file with one link (review 433), flocked exclusively, truncated, written as JSON, and fsynced. The holder keeps that flock for as long as it holds the lock (reviews 445 and 446). Deleting the file loses a name, not the lock.
4. On EWOULDBLOCK, the refusal opens the record (O_NOFOLLOW, O_NONBLOCK, regular files only) and believes it only if the record is flocked, which only a live holder can do. It then reads it (bounded to 64 KiB, every field required: review 440) and returns `*HeldError`. The message names the directory, pid, host and start time, and what to check: ps, systemctl status, docker ps, and a network filesystem. If the record is unreadable or incomplete, it says so and still refuses.
5. Release empties the record and lets go of it, then of the directory. The file stays in place.
6. `lock_unix.go` (`//go:build unix`) holds the flock. `lock_other.go` refuses before touching anything (review 441).
7. Acquire does not create the data directory; that is serve's decision (CCBYX).
8. Version: `debug.ReadBuildInfo().Main.Version`, which is "(devel)" in a local build.
9. No new exit code. A refusal maps to exitInternal (4) until serve decides otherwise (CCBYX AC3).

### Tests
`internal/lock/lock_test.go`. A second process is this test binary re-run with `KETJU_LOCK_HELPER` set.
- AC1: the second process's refusal is non-zero and names the pid; an in-process second Acquire is refused with the record.
- AC2: the holder is killed with SIGKILL, and Acquire then succeeds.
- AC3: after Release the lock can be taken again, by another process and by this one.
- AC4: a golden holds the two messages.
- Also covered:
  - the record deleted or replaced under a holder, which still refuses a second process;
  - a data dir that is a file;
  - a symlink, dangling symlink, hard link, fifo or directory at the record path;
  - unreadable, incomplete and symlinked records;
  - a longer old record leaving no tail;
  - a missing directory, which is not created.

### Design
docs/10-open-questions.md "Operations" item 24. docs/02-architecture.md "One process per data directory". README "One process per data directory". docs/03-thread-storage.md "Write path" (exactly one writer per file).

### Interactions
Consumed by CCBYX (serve), first after config load and released last; the serve-level checks for AC1 and AC3 are its to carry. Also consumed by C5Y25 (reindex), with purpose "reindex". `ketju fsck --rebuild` does not take the lock, as planned.

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

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:21:29Z

Answered review 436 (medium). On refusal, Acquire closed the contended descriptor and then re-read the holder record by path, so a holder that left in between could be replaced in the message by whoever came next. It now reads the record through the refused descriptor before closing it, bounded to 64 KiB. That narrows the window and never resolves the path again; it cannot close the window entirely, because a record read is not atomic with a lock test. No deterministic test tells the two apart, since the difference exists only inside that race; the existing refusal tests hold the behaviour, and a mutant that drops the holder, or bounds the read below a whole record, is killed (15 of 15 lock mutants killed). CI run 279 on d16e971 passed, with internal/lock in the race step.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:26:47Z

Answered review 440 (medium). readHolder accepted any valid JSON, so {} or null decoded into a holder with pid 0 and no host, and a refusal would have named that as the holder. A record must now carry a positive pid, a host, a purpose and a start time. Otherwise it is a read error naming what is missing, and the refusal says 'another process'. TestUnreadableRecordStillRefuses covers: not JSON, empty, {}, null, each field missing in turn, a negative pid, and a whole record that is named. Mutation check: 20 of 20 lock mutants killed, the 15 before plus one per field check and one on the missing-list test. CI run 281 on 3689e8f passed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:31:11Z

Answered review 441 (low). On a non-unix platform, openFile created ketju.lock before tryLock refused, and a missing directory gave an open error instead of the refusal. The non-unix openFile now returns the unsupported-platform error before touching anything. Checked with GOOS=windows go vet and GOOS=plan9 go build. No test runs there: CI is linux only. CI run 283 on a233731 passed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:38:19Z

Answered review 443 (medium): replacing the lock file permitted two holders. The finding was right. A flock belongs to an inode, so deleting or renaming over ketju.lock while serve held it would have let a second process lock the new file.

The options considered:
- **Document the limitation**, which the review allowed. Rejected: the lock is the one guard against quiet corruption, and "don't touch this file" is exactly what a botched deploy does.
- **Have the holder re-check its inode periodically.** Rejected: it notices only after the second process is already running.
- **Flock the data directory itself.** Taken. The directory's inode cannot be replaced without replacing the directory, and a replaced directory is other data. ketju.lock remains, but only as the record naming the holder, and nothing decides anything from it.

A consequence is that review 436's fix no longer applies: there is no refused file descriptor to read the record through. The refusal reads the record by path (O_NOFOLLOW, bounded, all fields required). A holder writes it just after locking, so in that moment a refusal reads the previous holder's record, or an empty file, which it reports as unnamed.

New tests:
- TestReplacingTheRecordDoesNotFreeTheLock covers the record deleted, then replaced by rename; both still refuse a second process.
- TestDataDirMustBeADirectory.
- A symlinked-record case in TestUnreadableRecordStillRefuses.
The golden changed by one word ("lock file" became "lock record") and was regenerated with -update.

Mutation check: 22 of 23 killed, against a green baseline. The survivor, dropping O_DIRECTORY from the directory open, is equivalent: a data dir that is a file then fails at writing the record, with the same ENOTDIR. The symlinked-record mutant survived the first run and is now killed by the case above. CI run 285 on 94af83f passed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T15:44:42Z

Answered review 445 (medium): a refusal could name the previous process as the current holder. The finding was right. A new holder writes its record just after taking the directory lock, and a refusal in between read whatever record was left. Naming the wrong process is worse than naming none, because it sends an operator to kill or inspect the wrong thing.

Two changes close the window:
- **Release empties the record** while still holding the lock. After a clean shutdown a refusal in that window finds an empty file and reports the holder as unnamed.
- **A refusal checks that the named process is running.** After a killed holder, the record left behind names a pid. When the record names this host, the pid is checked with signal 0; if the process is gone, the record is not believed and the refusal says why.

Two limits remain, and are stated in readHolder's comment:
- a record from another host cannot be checked;
- a pid the system has since reused passes.
Both are narrow: the first matters only on a shared (network) filesystem, which the message already tells the operator to check, and the second needs a pid reused within the moment between lock and record.

Rejected: writing the record before taking the lock, which would let a refused process overwrite a live holder's record. Also rejected: retrying the read until the record changes, since there is no signal that a record is the current holder's.

Test: TestRefusalNeverNamesAGoneHolder. It checks that Release leaves the record empty, then has a live holder standing behind an empty record and behind a record naming a reaped pid; both read as unnamed. A record from another host is still named.

Mutation check: 26 of 27 killed. The survivor is the equivalent O_DIRECTORY mutant from before. CI run 287 on 3074872 passed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T16:29:59Z

Answered review 446. Two mediums: the previous-holder naming, still present, and a fifo record that could hang a refusal.

The user decided how to finish this PR: fix both and re-review once. If that round raises only further refusal-message naming edges, merge and file a follow-up. Anything high, or outside naming, is still fixed first.

**Naming.** The signal-0 pid check is gone. A zombie pid passes it, and a record from another host cannot be checked at all. The holder now keeps the record file open with an exclusive flock for as long as it holds the directory lock, and Release lets go of the record before the directory. A refusal names the holder only if the record it reads is flocked by someone. The kernel drops that flock with the holder, whether it exited, was killed, is a zombie, or ran on another host. So a record left by an earlier holder is unlocked and reads as unnamed (errUnheldRecord), and so is a record replaced under a live holder, because the lock is on the inode the holder wrote. A new holder waits up to 2 s for the record's lock, since a refusal holds it only for the moment it takes to test it.

**Fifo.** openRecord opens with O_NONBLOCK and refuses anything but a regular file, so a fifo with no writer is reported rather than waited on.

Tests:
- TestRefusalNeverNamesAnEarlierHolder stands in for a holder that has the directory lock but has not recorded itself, behind three records: an empty one, one naming this process on this host (a live pid, which the old check would have believed), and one from another host. None is named.
- TestFifoRecordDoesNotBlockARefusal.
- TestUnreadableRecordStillRefuses now checks the whole-record case before it replaces the file, because a replaced record is correctly unheld.

Mutation check: 24 of 26 killed. Both survivors are equivalent:
- dropping O_DIRECTORY, as before;
- dropping O_NOFOLLOW on the record read: a symlinked record then resolves to a file nobody holds, and is refused as unheld anyway.
CI run 289 on ccafdbc passed.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T17:18:09Z

AC1 and AC3 are proven at serve level by TestSIGTERMDrainsAndExitsZero in cmd/ketju/serve_test.go (TKT-01M29CCBYX). A second ketju serve against the same data directory exits non-zero, and its output names the first's pid. After SIGTERM and a clean exit, lock.Acquire on the directory succeeds. TestDrainTimeoutStillFlushes also shows the lock released after a drain timeout, and TestStartupFailures shows it released after every failed startup. Ticked on that evidence.

## Summary

internal/lock: an exclusive flock on the data directory itself, so deleting or replacing a file inside it cannot free the lock. data/ketju.lock is a JSON record naming the holder (pid, host, purpose, start, version), written only while locked, with O_NOFOLLOW and single-link checks. A killed holder leaves nothing stale, because the kernel drops the flock. A refusal is a *HeldError whose message says what to check, and says so when the record is unreadable or incomplete. Non-unix platforms refuse before touching anything. AC2 and AC4 are met here. AC1 and AC3 are met at the lock level with a real second process, and are left unticked because they name serve; the serve-level checks are carried by TKT-01M29CCBYX.
