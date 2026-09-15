---
schema: 3
id: TKT-01M29C0WDJ9FETR8EFZ7ED5JYF
title: Implement the concurrent read path with coalesced reads
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:storage
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGYZ8FQB2W2XC14S2T3
blocks_on: none
references:
  - ref: design:storage-read-path
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-15T04:47:26Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Reading a range of ordinals: slice the in-memory index, then `ReadAt` each record, verify its CRC, and decode. Readers never take the actor's channel, because `os.File.ReadAt` is safe for concurrent use and a reader queueing behind a writer would make a busy thread slow to read.

When consecutive entries are contiguous on disk, which is the common case for unedited posts, the reader coalesces them into one `ReadAt` and slices the buffer. That is the difference between 25 syscalls and one for a typical page.

No mmap in v1. It adds portability and lifecycle complexity for little gain at this scale, and the OS page cache already does the work. Revisit only if profiling says otherwise.

## Acceptance criteria

- [x] A page of contiguous posts is fetched with one ReadAt, counted in a test
- [x] Reads run concurrently with appends to the same thread under the race detector with no reader ever blocking on the actor
- [x] A record failing its CRC returns an error naming the ordinal rather than returning bytes
- [x] Tombstoned and moved posts return from the index without a log read

## Implementation plan

`reader.go`: a Reader over an io.ReaderAt and an Index, holding nothing from
the actor. Range slices the index, walks the entries building runs of
CONTIGUOUS VISIBLE entries, and issues one ReadAt per run.

Two things had to change underneath before a reader could exist at all.

Index gained an RWMutex. The actor mutates an entry in place when a post is
edited or deleted, so a concurrent reader was a data race whatever the reader
looked like. The lock guards the entries only and is never held across I/O,
which is what makes "readers never block on the actor" true in the sense that
matters: a reader's worst case is one batch of in-memory updates, never an
fsync.

Index.Entries now returns a COPY rather than a subslice. The alias was cheaper
and became wrong the moment a reader could run beside the actor. A page is a few
dozen 40-byte entries, so the copy is under a kilobyte.

A post's entry points at its CURRENT revision, so the record at that offset is a
Post for an unedited post and an Edit for an edited one. Both carry the source
and both are accepted; anything else means the index and the log disagree, which
is reported as a bug rather than as damage.

## Notes

**agent:claude-code/storage** at 2026-09-15T04:46:53Z

Measured rather than asserted, through a ReadAt-counting wrapper:

  25 contiguous posts               1 ReadAt
  10 posts with post 5 edited       3 ReadAts (1-4, then 5, then 6-10)
  2 tombstoned posts                0 ReadAts
  6 posts, 2 and 3 deleted          2 ReadAts (post 1, then posts 4-6)

The "no reader ever blocking on the actor" half of criterion 2 is proven by
construction rather than by hoping. The actor is parked inside a blocked fsync
for the whole of TestReadsRunWhileTheActorIsBlockedInFsync — eight readers do
twenty page reads each and all finish — and only then is the gate opened. A
reader that needed anything from the actor would have deadlocked instead of
reaching the end of the test.

This ticket found a latent panic in TKT-01M29C0WD8, which is noted there in
full. The actor closed its request channel to signal shutdown; a send on a
closed channel panics rather than blocking, and the select guarding it could
not help because the send case becomes "ready" the instant the channel closes.
Its own test passed on timing alone, and adding the Index mutex here shifted the
schedule enough to make it panic every run. Fixed by never closing that channel.

Worth knowing for the benchmark ticket: an edit breaks a contiguous run, so a
page of heavily edited posts costs closer to one read per post than one read per
page. That is correct — those records genuinely are elsewhere — but it means the
"random-ordinal read under 100 us warm" target should be measured on both an
unedited and an edited thread, or the number will flatter the design.

## Summary

`reader.go`: a Reader over an io.ReaderAt and an Index that holds nothing from
the actor. All four criteria met, 6 tests, 192 in the package, green under
-race across repeated runs.

Coalescing measured through a ReadAt-counting wrapper: 25 contiguous posts cost
1 ReadAt; a page of 6 with two deleted costs 2; two tombstoned posts cost 0; and
10 posts with the fifth edited costs 3, since an edit genuinely does live
elsewhere and breaks the run.

"No reader ever blocking on the actor" is proven by construction. The actor is
parked inside a blocked fsync while eight readers do twenty page reads each and
all finish; the gate opens only afterwards. A reader that needed anything from
the actor would have deadlocked rather than reaching the end.

Two changes underneath had to land first. Index gained an RWMutex, because the
actor mutates an entry in place on edit and delete and a concurrent reader was a
data race whatever the reader looked like; it guards entries only and is never
held across I/O, which is what makes the non-blocking claim true in the sense
that matters. And Index.Entries now returns a copy rather than a subslice — the
alias was cheaper and became wrong the moment a reader could run beside the
actor.

A post's entry points at its CURRENT revision, so the record at that offset is a
Post when unedited and an Edit when edited. Both carry the source and both are
accepted; anything else means index and log disagree and is reported as a bug
rather than as damage.

This ticket found a latent panic in TKT-01M29C0WD8, noted in full on that
ticket and fixed here. The actor closed its request channel to signal shutdown,
and a send on a closed channel panics rather than blocking — selecting a quit
case alongside it does not help, because the send becomes "ready" the instant
the channel closes. Its own test passed on timing alone; adding the Index mutex
shifted the schedule and it panicked every run. Fixed by never closing that
channel: an inflight WaitGroup and a closed flag admit or refuse appends, Close
waits for admitted ones to be answered, then closes a separate quit channel.

For the benchmark ticket: measure the warm random-ordinal read on an edited
thread as well as an unedited one. A page of heavily edited posts costs nearer
one read per post than one per page, and measuring only the clean case would
flatter the design.
