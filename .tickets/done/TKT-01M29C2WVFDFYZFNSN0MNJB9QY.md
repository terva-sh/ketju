---
schema: 3
id: TKT-01M29C2WVFDFYZFNSN0MNJB9QY
title: Ship ketju dump
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
blocks_on: none
references:
  - ref: design:storage-recovery
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T07:20:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Print a thread log as JSON lines, one object per record, for inspection. This is the debugging tool for every storage bug that follows, so it is worth having before the bugs arrive.

It reads a log directly without opening the database or trusting the index, which is what makes it useful when the index or the database is the thing that is wrong.

## Acceptance criteria

- [x] Output is one JSON object per line carrying the frame header fields and the decoded body
- [x] The command works on a log whose index is missing or corrupt
- [x] A record failing CRC is reported on its own line rather than aborting the dump
- [x] Offsets are included so a line can be correlated with an index entry

## Summary

`cmd/ketju` with its first subcommand, plus `threadlog.ScanFrames` underneath
it. 6 dump tests and 6 scanner tests.

Dump reads the log directly. It never opens the index, which is checked rather
than asserted: the same output is required with the index missing, truncated,
filled with garbage, and replaced by a directory. That is the property the
tool exists for.

A checksum failure is reported on its own line and the walk continues, because
the length was structurally sound so the next frame is findable. One bad
record costs a reader one record rather than the other fifty thousand. A
length that is out of range or runs past the end of the file is different: the
next frame cannot be located, so that line is marked `fatal` and the scan
stops rather than inventing offsets.

A damaged frame still reports its header fields as the bytes state them. Those
fields may themselves be the corrupt part, but "ordinal 412 fails CRC" is
worth more to whoever is holding the broken thread than "a frame fails CRC".

Offsets and sizes are asserted against the index entries for the same records,
since correlating a dump line with an entry is the whole reason they are
printed.

`ScanFrames` went into threadlog rather than into the command because fsck
needs exactly the same walk, and frame arithmetic implemented twice is frame
arithmetic that will disagree once.

Exit codes are laid out for both tools now: 0 clean, 1 corruption, 2 usage,
3 drift, 4 internal. A tool that reports everything as 1 cannot be put in a
cron job, which is where these are meant to end up.
