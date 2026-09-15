---
schema: 3
id: TKT-01M29PARE1WYBA7PAKRWSN5QD0
title: Define what a reader does with an unknown record type
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:storage-format
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-12T02:14:50Z
updated_at: 2026-09-15T03:03:02Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

Found while writing the agent-native positions in TKT-01M29GGG3W. The format is explicit about forward compatibility everywhere except here.

Unknown frame flag bits round-trip untouched, guaranteed by `frame.go:65` and enforced by `TestFrameUnknownFlagBitsRoundTrip`. Unknown reserved header bytes are not an error, guaranteed by `header.go:82`. But nothing in docs/03 or in the code says what a reader does with a **record type** it does not recognise, and docs/10 item 4 already plans to add one: the hard-redaction lean is "rewrite with a new type, keep CRCs valid".

So an older binary will meet a record type from a newer one, and the behaviour is undefined today.

The catalogue assigns 15 of 255 values in deliberate bands: 1-5 post lifecycle, 10-16 thread events, 20-21 split and merge, 30 archive. The banding is visible but docs/03 never states it as a rule, and there is no registry and no reserved range.

This has to be settled before TKT-01M29C5Y1E freezes the format, because "skip unknown types" and "refuse the log" are not interchangeable once data exists. Note that skipping is mechanically easy here, since the frame length lets a reader step over a record it cannot interpret without understanding it. The hard part is deciding when skipping is safe: a skipped `Delete` would render a post that a moderator removed.

## Acceptance criteria

- [x] docs/03 states what a reader does with an unrecognised record type, and the choice is justified rather than asserted
- [x] The rule distinguishes a type that can be skipped safely from one that cannot, or states that no such distinction is possible and why
- [x] docs/03 states how a new record type is claimed: whether the bands are a rule, and whether any range is reserved for future use
- [x] internal/store/threadlog or its caller implements the rule, with a test that feeds a reader an unknown type
- [x] TKT-01M29C5Y1E does not close until this is decided

## Implementation plan

Decision taken with the person working this ticket: PARTITION THE TYPE SPACE by
the high bit of the type byte, and refuse the WHOLE THREAD rather than render
part of it.

    0x01-0x7F  advisory  a reader that does not know this type may skip it
    0x80-0xFF  critical  a reader that does not know this type must refuse
    0x00       never assigned

The rule that decides which range a NEW type goes in:

  A record may be skipped by a reader that does not understand it only if
  ignoring it cannot cause the reader to reveal content, or permit an action,
  that the record exists to withhold or prevent.

Why a range and not a flag bit: the class is a property of the TYPE, not of an
individual record, so deciding it once at assignment is cheaper and harder to
get wrong than asking every writer to set a bit correctly on every record. It
also costs none of the 15 reserved frame flag bits, and it is readable from the
type byte a reader has already parsed.

Why whole-thread refusal: a partial render is the same exposure bug wearing a
friendlier face. If the unknown critical record withheld post 3, rendering
posts 1-10 without applying it leaks post 3 exactly as skipping would. Refusal
has to be all or nothing to mean anything.

Existing numbers are NOT renumbered. Every type in 1-30 is in the v1 catalogue,
so the range is a promise about what an IGNORANT reader does and is vacuous for
a type every build knows. The two live cases are the reserved split and merge
numbers, which today's build genuinely does not implement — and skipping those
fails to stale and duplicated rather than to exposed, so advisory is correct for
them.

The first real customer is already scheduled: docs/10 item 4 leans toward a
`Redacted` record type for hard redaction before Phase 2. That one is critical
and gets a 0x80+ number.

## Notes

**agent:claude-code/storage** at 2026-09-15T03:02:44Z

Two corrections to the analysis this started from, both of which changed the
work rather than just the wording.

Split and merge do NOT need renumbering. They were first classified as failing
open; that is too strong. Skipping a PostsSplitOut leaves the source posts
rendering as full text instead of as pointers — content that was already visible
in that thread. It fails to stale and duplicated, not to disclosed, which is a
correctness bug and not a safety one. They stay advisory at 20 and 21, and the
format freeze does not have to move them.

Edit was under-weighted and is the sharper case than split. Skipping an Edit
renders the superseded revision, so an edit that removed a phone number is
undone by an old reader. It belongs with Delete, and it is what sharpened the
rule from "modifies state" to "withholds content or prevents an action" — the
distinction that makes the test decidable.

The implementation inverted an existing behaviour rather than adding to it.
SkipUnknown returned true for EVERY unrecognised type, which is the disclosure
bug this ticket was filed about, and two existing tests asserted that behaviour
using types 199 and 200 — both of which land in the critical range under the new
rule. They now use 99. That is the one place a reviewer should look hardest: a
test was changed to match new behaviour, which is exactly the move that hides a
regression when it is not deliberate. Here it is deliberate and it is the point
of the ticket.

ErrCriticalRecordType wraps ErrUnknownRecordType rather than sitting beside it,
so errors.Is(err, ErrUnknownRecordType) still answers "did this build know the
type" for a caller that does not care about the distinction, while
errors.Is(err, ErrCriticalRecordType) answers "and is ignoring it safe".

Type 0 is now explicitly ErrMalformed rather than an unknown type. It is never
assigned, so meeting one is damage from a flipped byte, and reporting it as a
record from the future would invite skipping corruption.

docs/10 item 4 gained a pointer: if hard redaction lands as a new record type it
takes a critical-range number. That is the first real customer for the range and
it is already scheduled for before Phase 2.

## Summary

Decided: the type space is partitioned by the HIGH BIT of the type byte.
0x01-0x7F advisory, an unknown type may be skipped by frame length. 0x80-0xFF
critical, an unknown type means the reader refuses the WHOLE thread. 0x00 is
never assigned, so meeting one is damage rather than a newer writer.

The test for assigning a new number, which is what makes the rule decidable:

  A record may be skipped by a reader that does not understand it only if
  ignoring it cannot cause the reader to reveal content, or permit an action,
  that the record exists to withhold or prevent.

Withholding is the line, not modifying. A skipped Delete renders a removed
post; a skipped Edit renders the superseded revision, undoing an edit that took
something out; a skipped ThreadLocked permits a reply. Those fail open. A
skipped Restore leaves a post deleted and a skipped ThreadRenamed shows a stale
title — those fail closed and are survivable.

A range rather than a frame flag, because the class is a property of the type
and not of an individual record: deciding it once when the number is handed out
is cheaper and much harder to get wrong than asking every writer to set a bit
correctly every time, and it spends none of the 15 reserved flag bits.

Refusal is whole-thread. Rendering what came before the unknown record and
stopping is the same disclosure wearing a friendlier face — if the record
withheld post 3, showing posts 1 to 10 without applying it leaks post 3 exactly
as skipping would.

docs/03 gained two sections, "Unrecognised record types" and "Claiming a new
record type". The latter settles the third criterion explicitly: the 1-5 /
10-16 / 20-21 / 30 bands are a CONVENTION, the partition is the RULE, a number
is never reused once assigned, and no range is reserved for private or
experimental use — a fork that invents a type takes its chances with a future
upstream number.

Code: RecordType.Critical/Advisory, CriticalTypeBit, and ErrCriticalRecordType
wrapping ErrUnknownRecordType so a caller can ask "did this build know the type"
and "is ignoring it safe" separately. This INVERTED existing behaviour rather
than extending it — SkipUnknown previously returned true for every unrecognised
type, which is the disclosure bug the ticket was filed about.

Two claims from the opening analysis were wrong and were corrected before any
code was written. Split and merge do not need renumbering: skipping them leaves
a stale, duplicated render rather than a disclosure, so they stay advisory at 20
and 21 and the freeze does not have to move them. And Edit, not split, is the
sharp case that sits beside Delete.

Nothing in the v1 catalogue is critical, and that is not a claim that ignoring
Delete would be harmless — the partition governs only a reader that does not
recognise a type, and every build recognises 1 through 30.

First real customer, already scheduled: docs/10 item 4 leans toward a Redacted
record type for hard redaction before Phase 2. It takes a 0x80-range number,
and docs/10 now says so.
