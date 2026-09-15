---
schema: 3
id: TKT-01M29BYYGNN34ZP3AMFZ540R2H
title: Implement the record type catalogue and body codecs
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
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-15T00:56:44Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Every record type in the docs/03 table and its body: ThreadCreated, Post, Edit, Delete, Restore, and the thread events for rename, move, lock, unlock, pin, unpin, tag, and archive. Split and merge records are reserved here and implemented in Phase 4.

Bodies are self-describing maps so a field can be added without a format bump. An unknown field in a body is ignored rather than fatal, and an unknown record type is skipped with its frame length, which is how an older binary survives reading a newer log.

## Acceptance criteria

- [x] Every record type in docs/03 round-trips through encode and decode with its documented body fields
- [x] An unknown record type is skipped using the frame length and does not stop a replay
- [x] An unknown field in a known body is ignored and the rest of the body still decodes
- [x] Type numbers match the docs/03 table exactly, and reserved numbers for split and merge stay unused

## Implementation plan

One new file, `internal/store/threadlog/records.go`, beside the frame codec
rather than above it: a record type is meaningless without the frame that
carries it, and splitting them across packages would mean exporting the frame's
internals to let the catalogue read a type byte.

Type constants transcribed from the docs/03 table. Bodies are Go structs with
matching `cbor` and `json` tags, encoded as string-keyed maps — that is what
"self-describing" means on disk and what makes an added field survive an older
reader.

Dispatch is a `map[RecordType]func() any` called the catalogue. Membership in
that map IS the definition of "known", so skipping is not a special case
written into a walker: `DecodeBody` returns `ErrUnknownRecordType`, the caller
asks `SkipUnknown(err)`, and advances by the frame length it already has.

Split and merge get constants and names but no catalogue entry, so the numbers
are spoken for without being implemented.

## Notes

**agent:claude-code/storage** at 2026-09-15T00:56:29Z

Two decisions worth the words.

ErrUnknownRecordType deliberately does NOT wrap ErrMalformed, and neither does
ErrUnsupportedEncoding. Wrapping was the first version and it is wrong: a caller
that treats malformed as damage — stop the replay, report corruption — would be
told a log is broken because a newer build wrote it, which is the exact failure
the self-describing format exists to prevent. TestUnknownRecordTypeIsNotMalformed
pins it, and also pins that a checksum failure is never skippable.

The decoder is a configured cbor.DecMode, not package-level cbor.Unmarshal.
MaxArrayElements/MaxMapPairs 65536 and MaxNestedLevels 32 bound what a body can
ask the decoder to allocate; the frame parser guards the frame length, but
nothing under it guarded a body that parses yet claims a billion map pairs.
DupMapKeyEnforcedAPF refuses a body naming the same key twice — accepting one
lets two builds disagree about which value wins, which is a silent divergence
between replicas of one log.

The docs/03 table is transcribed by hand into `everyType` in the test rather
than derived from the catalogue. Deriving it would make the test agree with the
code by construction and prove nothing; as written, a type added to the code
without being added to docs/03 fails the length check.

Mentions []uint64 and Attachments []string are the least settled fields here and
also the cheapest to change, which is the property the body format is for.

## Summary

`internal/store/threadlog/records.go`: all thirteen implemented record types
from the docs/03 table with their bodies, both encodings, and the dispatch that
turns an unknown type into a skip rather than a failure. 26 round-trip subtests
(13 types x CBOR and JSON), all four criteria met.

The catalogue — `map[RecordType]func() any` — is the definition of "known".
Skipping is therefore not a branch a walker has to remember to write:
`DecodeBody` reports `ErrUnknownRecordType`, the caller asks `SkipUnknown(err)`
and advances by the frame length it already parsed. The skip test puts the
unknown record in the MIDDLE of a log with a 300-byte body, because a skip that
works at the end of a file proves nothing about landing on a frame boundary.

Split (20) and merge (21) have constants and names but no catalogue entry, so
the numbers cannot be handed to something else before Phase 4 implements them.
A Phase 4 log already replays on this binary: reserved types skip like any
other unknown.

Adds one direct dependency, github.com/fxamacker/cbor/v2 v2.9.3 — the module's
first. The decision to use CBOR was already measured in TKT-01M29BSPD (963 ns
against 7982 ns for a 2 KB body); this is the library that spike used.

Two things a later reader should not undo:

ErrUnknownRecordType and ErrUnsupportedEncoding do NOT wrap ErrMalformed. The
first draft did. A caller that treats malformed as damage would have been told a
log is corrupt because a newer build wrote it — the exact failure this format's
forward compatibility exists to prevent.

Bodies decode through a configured cbor.DecMode rather than cbor.Unmarshal.
The frame parser bounds the frame length; nothing bounded a body that parses
cleanly and claims a billion map pairs. Limits are 65536 elements/pairs and 32
levels, far above a 64 KB post and far below trouble. Duplicate map keys are
refused outright, because two builds disagreeing about which value wins is a
silent divergence between replicas of one log.
