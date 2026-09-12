---
schema: 3
id: TKT-01M29BW9PTAQ34EC78E61AHZM0
title: Implement the log header, record frame, and CRC32C codec
type: task
status: done
status_reason: Owner accepted the four docs/03 edits in review. MaxFrameSize is carried to TKT-01M29C5Y1E for deliberate re-examination at the format freeze.
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
  - TKT-01M29BSPDQRV0P5JQN866V1FKN
blocks_on: none
references:
  - ref: design:storage-format
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-12T22:48:47Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/review
  name: ""
extensions: {}
---

## Description

The 64-byte file header and the record frame from docs/03, with CRC32C over both.

The frame splits deliberately: a fixed 32-byte header holding type, encoding, flags, ordinal, revision, timestamp, and actor, then the variable body. The split is what lets the reindexer and the index builder walk an entire log without decoding a single body.

Reserved bytes exist in both structures and stay reserved. They are the escape hatch for segment rollover if a log ever approaches the tens-of-GB range.

## Acceptance criteria

- [x] Encode and decode round-trip every field of the header and the frame
- [x] A frame with a corrupted body fails its CRC check and is reported rather than returned
- [x] A frame header can be parsed and skipped without decoding the body, and a test walks a log doing exactly that
- [x] Reserved bytes are written as zero and a non-zero value in them is not an error, so a future version stays readable

## Implementation plan

Lands in `internal/store/threadlog/` per the docs/02 module layout. This is the first production Go code in the repository, so it also creates the root `go.mod`.

### Shape

Two files. `header.go` for the 64-byte file header, `frame.go` for the record frame, both with CRC32C (Castagnoli) through `hash/crc32`.

The parser returns views into the caller's buffer rather than copying. The read path in TKT-01M29C0WDJ coalesces consecutive entries into one `ReadAt` and slices the result, so a codec that insists on owning its bytes would force a copy per post on the hottest path.

```go
type FileHeader struct {
    Version    uint16
    ThreadID   uint64
    CreatedMS  int64
    ForumID    uint64
}
func ParseFileHeader(b []byte) (FileHeader, error)
func (h FileHeader) AppendTo(dst []byte) []byte

type FrameHeader struct {
    Type, Encoding byte
    Flags          uint16
    Ordinal        uint32
    Revision       uint32
    TimestampMS    int64
    Actor          uint64
}
// Walk a log without touching bodies: parse, then skip TotalSize.
func ParseFrameHeader(b []byte) (h FrameHeader, totalSize int, err error)
// Parse and verify CRC. Body aliases b.
func ParseFrame(b []byte) (h FrameHeader, body []byte, totalSize int, err error)
func AppendFrame(dst []byte, h FrameHeader, body []byte) []byte
```

`ParseFrameHeader` is what satisfies criterion 3 and what the reindexer and index builder use. It never looks past byte 32 and never verifies the body CRC, because verifying would mean reading the body it exists to avoid.

### Frame arithmetic, so it is written down once

`L` at offset 0 counts the fixed part after the length field plus the body, so `L = 28 + len(body)`. Total on-disk size is `4 + L + 4`. The CRC covers bytes `0 .. 4+L-1`, which includes the length field, so a corrupted length cannot pass as valid.

### Three things docs/03 does not say, which this ticket has to settle

1. **Byte order is unspecified.** The tables give offsets and widths and never name an endianness. Proposing little-endian: it is native on every target and needs no swapping, and the format is explicitly single-node so cross-endian portability is theoretical. Whatever is chosen goes into docs/03 before the freeze in TKT-01M29C5Y1E.

2. **No maximum frame length.** The parser reads `L` off disk and would allocate from it. It needs a declared cap so a corrupt length is refused before anything is sized from it. Proposing 1 MiB: docs/07 caps a post body at 64 KB and attachments live in blobs, not the log, so 1 MiB is roughly sixteen times the largest legitimate frame.

3. **Reserved-byte policy.** Criterion 4 requires writing zeros and tolerating non-zero on read. That is a forward-compatibility rule rather than a validation rule, and it needs a test that constructs a header with reserved bytes set and asserts a clean parse.

### Tests

Table-driven round trip over every field including boundary values. A corrupted-body case per record type asserting the CRC error names the ordinal. A synthetic multi-frame log walked by header alone, asserting the body is never read, with a counting reader proving it. Reserved bytes set to 0xFF parsing clean. Malformed lengths (zero, shorter than the fixed part, longer than the cap, longer than the buffer) each refused without allocation.

No panics on any input: TKT-01M29C2WTV fuzzes this parser next, and the fuzz target is far more useful against a parser already written to return errors rather than to trust its input.

## Notes

**agent:terva/mieli** at 2026-09-12T00:41:21Z

### docs/03 had the body size wrong in the frame table

The record frame table said the body occupies `L-32` bytes. It does not. `L` is
defined in the same table as excluding the length field, which makes the body
`L-28`. The two disagree by exactly four bytes, and following the table puts the
trailing CRC four bytes past the end of the body.

The definition of `L` and the stated CRC offset (`4+L`) agree with each other,
so the table cell was the error. Implementing it is what surfaced the conflict:
both readings look reasonable in prose and only one of them round-trips.

Fixed in docs/03, and the arithmetic is now written out under the table rather
than left to be re-derived:

    L = 28 + len(body)
    body occupies [32, 4+L)
    whole frame occupies 4 + L + 4 bytes

The code carries the same constants: `FrameFixedSize = 32`, `frameLengthBase =
28`. Worth a look during the format freeze in TKT-01M29C5Y1E, since this is
exactly the class of thing a freeze is supposed to catch.

**agent:terva/mieli** at 2026-09-12T00:41:49Z

in-progress to review: All four criteria ticked with tests behind them, vet and gofmt clean, 20 tests passing. Held at review rather than done because the work settled three things docs/03 left open (little-endian, the 1 MiB frame cap, reserved-byte policy) and corrected a fourth (the L-32 body size), and those edits to a design doc are a person's call before the format freeze in TKT-01M29C5Y1E.

**agent:claude-code/review** at 2026-09-12T22:48:47Z

Accepted by the owner in review, with one item carried forward.

All four docs/03 edits stand: little-endian byte order, the 1 MiB MaxFrameSize
cap, the reserved-byte forward-compatibility policy, and the correction of the
body size from L-32 to L-28. The arithmetic was re-derived independently before
acceptance: L excludes the length field, so the body occupies [32, 4+L) and is
4+L-32 = L-28 bytes. The code constants agree (FrameFixedSize = 32,
frameLengthBase = 28), and the parser bounds L at MaxFrameSize-8, which
correctly accounts for the length field and the trailing CRC.

Carried forward: the owner asked that MaxFrameSize be re-examined against the
64 KB post body cap in docs/07 before the format is frozen. A note to that
effect is on TKT-01M29C5Y1E. The cap is not being changed here; it is being
made a deliberate item at the freeze rather than an inherited default.

**agent:claude-code/review** at 2026-09-12T22:48:47Z

review to done: Owner accepted the four docs/03 edits in review. MaxFrameSize is carried to TKT-01M29C5Y1E for deliberate re-examination at the format freeze.

## Summary

Landed in `internal/store/threadlog/`: `format.go` (package doc, magic, version, CRC32C table, error sentinels), `header.go` (the 64-byte file header), `frame.go` (the record frame). This is the first production Go code in the repository, so it also created the root `go.mod` at `github.com/terva-sh/neoforum`, which is a placeholder pending TKT-01M29GGG34.

All four acceptance criteria are ticked with tests behind them. `go vet ./...` is clean, `gofmt -l` is empty, and `go test ./internal/store/threadlog/` passes 20 tests in 8 ms.

### What the API looks like

`ParseFrameHeader` returns the fixed part and the frame's total on-disk size, reads no further than byte 32, and does not verify the CRC. That last part is deliberate: verifying would mean reading the body the function exists to skip. `ParseFrame` is the full path and verifies. Anything that consumes a body goes through it.

Parsing is zero-copy. `ParseFrame` returns a body aliasing the caller's buffer, so the read path in TKT-01M29C0WDJ can coalesce consecutive records into one `ReadAt` and slice the result instead of allocating per post.

Errors are sentinels reachable through `errors.Is`: `ErrShortBuffer`, `ErrBadMagic`, `ErrChecksum`, `ErrMalformed`, `ErrUnsupportedVersion`. A CRC failure also carries a `*ChecksumError` naming the ordinal and both sums.

### The three gaps in docs/03 this ticket had to settle

Byte order is little-endian, recorded in docs/03 under the format heading. `MaxFrameSize` is 1 MiB, checked before the length field is believed and before anything is sized from it, so a corrupt length is refused rather than allocated against. Reserved bytes are written zero and ignored on read, and the checksum spans them so a future version that uses them stays readable by this parser.

`ParseFileHeader` does not judge the version, because a reader that meets a newer file needs the version in order to report it. `FileHeader.CheckVersion` is the separate call, and version policy belongs to the thread-open path rather than to the codec.

### One correction to the design doc

The frame table in docs/03 gave the body size as `L-32`, which contradicts the same table's definition of `L` and puts the CRC four bytes past the body. Corrected to `L-28`, with the arithmetic spelled out below the table. See the note on this ticket.

### Left for the next tickets

`RecordType` is declared as a type with no constants. The catalogue belongs to TKT-01M29BYYGN and defining the numbers here would encroach on it. `TestParseNeverPanics` covers the obvious bit flips and every truncation, but the real coverage is the fuzz target in TKT-01M29C2WTV, which now has a parser written to return errors rather than to trust its input.
