---
schema: 3
id: TKT-01M29BYYGYZ8FQB2W2XC14S2T3
title: Implement the offset index file and the in-memory index
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
  - TKT-01M29BW9PTAQ34EC78E61AHZM0
blocks_on: none
references:
  - ref: design:storage-index
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-15T01:01:49Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

The 32-byte index header and the 40-byte entry per ordinal, so the entry for ordinal n sits at `32 + 40*(n-1)` and a seek to any post is arithmetic rather than a scan.

The entry carries more than an offset on purpose: reply_to, author, created_at, revision, and flags. That is what lets the chain, author, and since lenses run without a database query, and what lets a tombstone be answered without reading the log at all.

The header records the log length the index is valid up to. That single field is what makes trusting an index a comparison against `stat()` rather than an act of faith.

## Acceptance criteria

- [x] Entry for ordinal n is read with one pread at the computed offset with no scan
- [x] Header CRC covers the header and a corrupt header forces a rebuild rather than a bad read
- [x] Tombstoned and moved posts are answerable from index flags with no log read, proven by a test that opens the log file read-only and counts reads
- [x] The in-memory form of a 50k-post index stays within the 2 MB stated in docs/03

## Implementation plan

One new file, `internal/store/threadlog/index.go`, holding three layers that
are easy to conflate and must not be: the on-disk header and entry codecs, a
single-entry reader over `io.ReaderAt`, and the in-memory `Index` with the
lenses docs/03 justifies the 40-byte width with.

`EntryFileOffset(n) = 32 + 40*(n-1)` is the whole design in one function.
`ReadEntryAt` is one `ReadAt` of exactly 40 bytes at that offset — it does not
read the header, because the caller has already made the trust decision.

Trust is `IndexHeader.Covers(logSize)`, a comparison against stat() rather than
a checksum of the log: verifying an index by rereading what it indexes costs
exactly what the index exists to avoid.

Entries are a slice, not a map. Ordinals are dense and one-based, so the slice
position IS the ordinal minus one, and a map would cost a hash and roughly three
times the memory to answer what subtraction already answers — which also matters
for the 2 MB budget.

## Notes

**agent:claude-code/storage** at 2026-09-15T01:01:35Z

Two criteria here are NEGATIVES — "with no scan" and "with no log read" — and a
negative is not shown by a test that merely gets the right answer. Both go
through a countingReaderAt that records every call, byte count and offset.

TestReadEntryAtIsOnePreadNoScan asks for the LAST of 5000 posts, which is the
case a scan is slowest at and a seek is indifferent to, then asserts exactly one
read, of exactly 40 bytes, at exactly 32+40*(n-1).

TestTombstonesAnswerWithoutTouchingTheLog writes a real log with real bodies to
a temp dir, opens it read-only as the criterion asks, and asserts zero reads
after answering tombstones plus the author and since lenses. Real bodies matter:
"no reads" has to mean the bytes were there to be read and were not.

The memory budget came out at 2 000 000 bytes for 50 000 entries — 1.91 MiB,
which is the figure docs/03 states, arrived at independently here. The test
pins unsafe.Sizeof(IndexEntry{}) == 40 first, because the budget is only met
while no field is reordered into padding, and that would break silently.

RebuildRequired started as `return err != nil`, which is a tautology dressed as
an API. It now discriminates: true for every way an index can be wrong about
itself, false for ErrNoSuchOrdinal, because asking for post 900 of a 40-post
thread is a bad question and must not condemn a healthy file.

IndexMagic is THRDIDX1, deliberately unlike the log's THRDLOG1, so a backup
that swapped a .log and a .idx fails on the first read instead of being
interpreted. TestIndexHeaderRejectsLogMagic holds it.

Not done here, and not in scope: the index BUILDER that walks a log and
populates entries. This ticket is the format and the in-memory form. The
builder needs body decoding for reply_to and will want the frame flag bit 0
constant that TKT-01M2BXKF8F declares, so it belongs after that lands.

## Summary

`internal/store/threadlog/index.go`: the 32-byte index header, the 40-byte
entry, a one-pread reader, and the in-memory Index with the tombstone, author,
since, chain and agent lenses. 14 tests, all four criteria met.

The two criteria that are negatives are proven by counting reads rather than by
getting the right answer. Reaching the last of 5000 posts takes exactly one
ReadAt, of exactly 40 bytes, at exactly 32+40*(n-1). Answering tombstones and
the author and since lenses over a real log — real frames, real bodies, opened
read-only — takes exactly zero reads of it.

Memory came out at 2 000 000 bytes for 50 000 entries: 1.91 MiB, which is the
number docs/03 states, reached independently here. The test pins
unsafe.Sizeof(IndexEntry{}) == 40 before measuring, because the budget holds
only while no field has been reordered into padding, and that would break
silently.

Header corruption is tested by flipping every byte in turn rather than a chosen
field, so the claim is coverage of the header rather than of whichever field
the author thought of. Each flip must both fail and satisfy RebuildRequired.

RebuildRequired began as `return err != nil` — a tautology dressed as an API.
It now discriminates: true for every way an index can be wrong about itself,
false for ErrNoSuchOrdinal, since asking for post 900 of a 40-post thread is a
bad question and must not condemn a healthy file.

IndexMagic is THRDIDX1, deliberately unlike THRDLOG1, so a backup that swapped
a .log for a .idx fails on the first read rather than being interpreted.

Explicitly NOT here: the builder that walks a log and populates entries. This
ticket is the format and the in-memory form. The builder needs body decoding
for reply_to and wants the frame flag bit 0 constant from TKT-01M2BXKF8F, so it
belongs after that lands.
