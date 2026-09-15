---
schema: 3
id: TKT-01M2BXKF8FWX1WFGJG5E4D48J1
title: Declare the frame and index flag bits as constants
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-12T23:00:24Z
updated_at: 2026-09-15T01:38:40Z
created_by:
  id: agent:claude-code/review
  name: ""
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

docs/03 assigns frame flag bit 0 to machine-authored, and mirrors it into the
index entry's flags field so a lens can filter agent posts without decoding a
body. TKT-01M29GGG3W settled that and docs/09 now lists it as a Phase 0 exit
criterion, worded as "format frozen at version 1 with reserved bytes and with
frame flag bit 0 assigned to machine-authored".

Nothing in the code says so. `internal/store/threadlog/frame.go` declares
`Flags uint16` and round-trips it verbatim, which is correct for the codec:
unknown bits must survive a read-modify-write, and `TestFrameUnknownFlagBitsRoundTrip`
holds that property. But the *assignment* of bit 0 lives only in a Markdown
table, and the format freeze in TKT-01M29C5Y1E is what makes that assignment
binding.

A bit assignment that exists only in prose is the kind of thing two callers
independently guess at. The server sets bit 0 from `users.kind` when it appends
a record (03), the index builder mirrors it into the entry flags, and a lens
reads it back. That is three places that must agree on the number 1, none of
which can currently reference a shared constant.

### Scope

Declare the frame flag bits as exported constants in `threadlog`, alongside the
index entry flag bits when TKT-01M29BYYGY defines that structure. Include the
reserved range, so the next bit assigned is obviously the next one and not a
guess. Keep the parser flag-agnostic: this adds names, not validation, because
rejecting unknown bits would destroy the forward compatibility the round-trip
test protects.

Worth doing before TKT-01M29C5Y1E rather than after, since the freeze is the
moment the numbering stops being editable. This is deliberately small and does
not need to grow: it is naming what is already decided.

## Acceptance criteria

- [x] Frame flag bit 0 is an exported, documented constant in `threadlog`, and docs/03 points at it rather than restating the number
- [x] The reserved bits are named or documented so the next assignment is unambiguous
- [x] The parser still preserves unknown flag bits, with `TestFrameUnknownFlagBitsRoundTrip` unchanged and passing

## Implementation plan

One new file, `internal/store/threadlog/flags.go`, holding BOTH flag sets, and
the index entry bits move there out of `index.go` where TKT-01M29BYYGY first
declared them.

Putting them together is the point rather than tidiness. The dangerous thing
about these two words is the relationship between them: machine-authored is bit
0 of a frame and bit 5 of an index entry, the same fact at two numbers. Kept in
separate files that is an invitation to write 1 where 32 belongs.

`FrameFlags` is a defined type, not bare uint16 constants, and distinct from
`EntryFlags`. The compiler is the only thing that reliably stops the two being
crossed, since both are 16 bits wide and describe overlapping facts.
`FrameHeader.Flags` changes type with it — every existing use passes an untyped
constant, so no test needs touching, which is what criterion 3 demands.

`FrameFlags.ToEntryFlags` is the single crossing point. Slightly beyond "name
what is decided", and included deliberately: the ticket's own argument is that
three call sites must agree on a number, and a reviewed function is how they do
that rather than by each writing a literal.

Nothing validates. Naming a bit must not start rejecting unknown ones.

## Notes

**agent:claude-code/storage** at 2026-09-15T01:37:43Z

Half of this ticket was already satisfied when it was picked up: TKT-01M29BYYGY
declared the index entry flag bits while implementing the index, which this
ticket anticipated ("alongside the index entry flag bits when TKT-01M29BYYGY
defines that structure"). What was left was the frame side, the reserved
ranges, and docs/03.

Criterion 3 is satisfied literally, not just in spirit. `git diff --stat` on
frame_test.go is empty: TestFrameUnknownFlagBitsRoundTrip is byte-for-byte
unchanged and passing. That worked out because every existing `Flags:` in the
package is an untyped constant (0xFFFF, 0x1234, math.MaxUint16), all of which
convert to a defined uint16 type without an edit.

Judgment calls a reviewer might want to reverse, both easy to strip:

ToEntryFlags is behaviour, and this ticket said it should not grow. It is in
because the ticket's stated problem is three call sites agreeing on a number,
and the two numbers DISAGREE (0 and 5) — the one case where "just use the
constant" is not enough, since using the wrong set's constant compiles.
TestMachineAuthoredHasDifferentBitsInFrameAndEntry exists to make that
permanent.

docs/03 keeps its bit tables rather than deferring to the code. For an on-disk
format the spec should stay readable without a Go compiler, so the tables gained
a Constant column and a paragraph saying code uses the name and never the
literal. That is "points at it" without gutting the spec, but it is an
interpretation of the criterion rather than the only reading.

Small trap worth recording: `-x` on an unsigned CONSTANT is a compile error in
Go, not a wraparound, so isolating the lowest reserved bit with `x & -x` has to
go through a variable. The test does that and says why.

**agent:claude-code/storage** at 2026-09-15T01:38:18Z

Filing fix: the acceptance criteria were written as a `### Acceptance criteria` heading inside the Description when this ticket was created, so they sat in prose where `git ticket ac` could not see them — `--check 1` answered "there is no item 1; the section has 0". Moved into the real section verbatim and the duplicate removed from the Description, so there is one copy rather than two that can drift. Worth watching for on anything else filed in that session.

## Summary

`internal/store/threadlog/flags.go` declares both flag sets, and the index
entry bits move there out of `index.go` where TKT-01M29BYYGY first wrote them.
docs/03 gained a Constant column on both tables and now says code uses the name
and never the literal. All three criteria met.

Half the ticket was already done on arrival — TKT-01M29BYYGY declared the entry
bits while implementing the index, which this ticket anticipated. What remained
was the frame side, the reserved ranges, and the docs.

Putting both sets in one file is the substance rather than tidiness. The
dangerous thing here is the relationship between the two words: machine-authored
is bit 0 of a frame and bit 5 of an index entry, one fact at two numbers. Kept
in separate files that invites writing 1 where 32 belongs, and it would compile.
`FrameFlags` is therefore a defined type distinct from `EntryFlags`, so the
compiler refuses the crossing, and `FrameFlags.ToEntryFlags` is the only place
the two numberings meet.

Criterion 3 holds literally: `git diff --stat` on frame_test.go is empty.
TestFrameUnknownFlagBitsRoundTrip is byte-for-byte unchanged and passing, which
worked because every `Flags:` in the package was already an untyped constant and
those convert to a defined uint16 type without an edit.

Nothing validates. Naming a bit must not start rejecting unknown ones, or the
forward compatibility the round-trip test protects would be gone.

Two judgment calls a reviewer can reverse cheaply. ToEntryFlags is behaviour and
this ticket said not to grow; it is in because the ticket's own problem
statement is three call sites agreeing on a number, and here the two numbers
DISAGREE, which is the one case where "just use the constant" is not enough.
And docs/03 keeps its bit tables rather than deferring to code, because an
on-disk format should stay readable without a Go compiler.

Also repaired the ticket itself: its acceptance criteria had been filed as a
`### Acceptance criteria` heading inside the Description, where `git ticket ac`
could not see them — checking item 1 answered "the section has 0". Moved into
the real section verbatim, duplicate removed. A scan of the rest of the store
found no other ticket with either that shape or an empty criteria section.
