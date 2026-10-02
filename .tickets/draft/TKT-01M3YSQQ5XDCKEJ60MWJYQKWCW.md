---
schema: 3
id: TKT-01M3YSQQ5XDCKEJ60MWJYQKWCW
title: Decide whether an archived thread can be unarchived, and by whom
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - decision
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T17:14:05Z
updated_at: 2026-10-02T17:20:29Z
created_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
extensions: {}
---

## Description

TKT-01M29CRJ64 (Implement thread archival) was filed with the criterion "Unarchiving is possible and recorded", and docs/15 "Invariants, added" says an archived thread is read-only "until a moderator unarchives the thread". The design documents say otherwise:
- docs/08 "Primitives": "Archive thread | admin | `ThreadArchived` | no: there is no unarchive record in v1 (03)".
- docs/04 "forums": "A thread's archive cannot be undone only because the log has no unarchive record."
- docs/03 "Record types" names `ThreadArchived` (30, body `reason`) and no record that undoes it.

Unarchiving needs a record in the thread log, because the archived state comes from the log and must survive a database rebuild (TKT-01M29CRJ64 criterion 3). docs/03 does not name one. A new record type outlives the release, so this is docs/15 stop class 2. The documents also disagree on who unarchives (docs/08's admin against docs/15's moderator), which is class 6. Phase 4 grooming split the question out of TKT-01M29CRJ64 so that archiving itself, whose record docs/03 fixes, can proceed.

### Open decision

Nothing is chosen. The options:
- (a) Keep the design: v1 has no unarchive. TKT-01M29CRJ64 ships archiving alone, and docs/15's "until a moderator unarchives" is corrected to match docs/08. Nothing is claimed in the log format.
- (b) Claim `ThreadUnarchived` by docs/03's procedure: number 31, in the archival band, body `{reason}`. It is advisory, because a build that skipped it would leave the thread archived, which fails closed. The projection clears `threads.archived`, and the open-thread cache stops treating the thread as archived. Only an admin may unarchive, as only an admin may archive (docs/08).
- (c) As (b), but anyone holding `moderate` in the thread's forum may unarchive, as docs/15 words it. Archiving stays an admin's.

What each needs written: (a) changes docs/15 only. (b) and (c) add a row to docs/03's table and to the catalogue in `internal/store/threadlog/records.go`, a projection decision in `internal/events/projection/apply.go`, a `thread.unarchived` audit action, a route, and docs/04 and docs/08 changes.

## Acceptance criteria

- [ ] The maintainer has chosen (a), (b) or (c), or stated another answer, and it is recorded in a note
- [ ] docs/03, docs/04, docs/08 and docs/15 agree with the answer, and a record type, if one is claimed, is in the docs/03 table with its number, body and class
- [ ] If the answer is (b) or (c), an implementation ticket is filed with milestone phase-4, depending on TKT-01M29CRJ64
