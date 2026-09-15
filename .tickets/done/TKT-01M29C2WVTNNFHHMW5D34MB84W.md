---
schema: 3
id: TKT-01M29C2WVTNNFHHMW5D34MB84W
title: Ship ketju fsck
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29C0WDWVPADC8A2J4VHCGP2
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T07:25:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Walk every log, verify every CRC, rebuild every index, and report drift between logs and database projections.

The drift report is the part that earns its keep. An index can be rebuilt whenever, but a projection silently behind its log is the failure mode that looks like "a post vanished" to a member and looks like nothing at all in the logs.

## Acceptance criteria

- [x] Every log is walked and every CRC verified, with a summary naming any damaged thread
- [x] Indexes are rebuilt and the command is safe to run against a live data directory
- [ ] Drift between each log length and its projection_state is reported per thread
- [x] Exit code distinguishes clean, drift found, and corruption found

## Notes

**agent:claude-code/storage** at 2026-09-15T07:25:35Z

Criterion 3, drift between each log length and its projection_state, is left
unticked because it cannot be implemented yet.

`projection_state` is created by TKT-01M29BYYJ8 (Add the projection tables and
projection_state), which is a Phase 1 draft, and the database epic
TKT-01M29BKBKV has not started. There is nothing to compare a log length
against.

What is in place so the check slots in rather than being retrofitted:

- `threadReport.DriftBytes` exists as a nullable field, absent from output
  until it can be filled.
- `fsckSummary.DriftChecked` is reported as false, and the text output says
  "projection drift not checked: there is no database yet". A clean fsck run
  must not be mistakable for a run that checked drift, which is the failure
  this ticket's description is actually about — a projection silently behind
  its log looks like nothing at all.
- Exit code 3 is defined and documented for drift, and is currently
  unreachable.

There is a test asserting the tool says it did not check, so this cannot be
quietly forgotten: TestFsckSaysTheDriftCheckDidNotRun.

Picking this up later means filling DriftBytes, setting DriftChecked, and
returning errDrift. Suggest it is done under TKT-01M29C5Y1S (startup
projection catch-up) or alongside TKT-01M29C5Y25 (ketju reindex), since both
already have to reason about how far a projection is behind.

## Summary

`cmd/ketju/fsck.go`, 10 tests. Three of four criteria met; the drift check
cannot be implemented yet and is unticked with a note saying why.

Walks every log under the threads directory, verifies every checksum, counts
records and posts, and names any damaged thread in both the text and JSON
reports. An operator with a thousand threads needs to know which one.

A torn tail is reported separately from damage. Calling an interrupted write
corruption would send somebody looking for a failing disk after an ordinary
power cut. The torn figure is the bytes past the last complete record, which
is what an operator is actually looking at — my first test confused that with
the bytes removed, which are different numbers.

Safe against a live data directory, and tested that way rather than asserted:
a thread is appended to while fsck runs over it, and the scan must report no
damage. The size is taken once when the log is opened, so an append landing
after that is simply not counted yet.

Nothing is written without --rebuild, because a check that repairs by default
surprises whoever ran it. --rebuild writes only the index, to a temporary file
renamed into place so a reader sees the whole old index or the whole new one;
a half-written index is the one outcome that turns a repair into the damage it
was meant to fix. The rebuilt index is asserted to open as Trusted, since an
index that still triggers a replay has not been repaired.

Also reports a log sitting in the wrong shard or not named after an id, since
something moved it or wrote it by hand and the tool that finds it is the one
that should say so.

Exit codes: 0 clean, 1 corruption, 2 usage, 3 drift. Three is currently
unreachable and documented as such.
