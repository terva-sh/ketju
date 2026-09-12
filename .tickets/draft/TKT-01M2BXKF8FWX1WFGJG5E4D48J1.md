---
schema: 3
id: TKT-01M2BXKF8FWX1WFGJG5E4D48J1
title: Declare the frame and index flag bits as constants
type: task
status: draft
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
updated_at: 2026-09-12T23:00:24Z
created_by:
  id: agent:claude-code/review
  name: ""
updated_by:
  id: agent:claude-code/review
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

### Acceptance criteria

- [ ] Frame flag bit 0 is an exported, documented constant in `threadlog`, and docs/03 points at it rather than restating the number
- [ ] The reserved bits are named or documented so the next assignment is unambiguous
- [ ] The parser still preserves unknown flag bits, with `TestFrameUnknownFlagBitsRoundTrip` unchanged and passing
