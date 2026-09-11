---
schema: 3
id: TKT-01M29BN36JH4M1W5819D06PGPP
title: Moderation primitives and admin surfaces
type: epic
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:XL
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29BKBMCNDSYHYF9WXAPDNRN
blocks_on: children
references:
  - ref: design:moderation
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-11T23:32:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Few tools, each reliable, reversible, and recorded. Every action appends to `audit_log`, and when it concerns a thread it is also a record in that thread's log, so the history travels with the content and survives a database rebuild.

Soft deletion is the rule. A Delete record leaves the original Post bytes intact and the reader returns a tombstone from the index flags, so ordinals, permalinks, and reply lenses keep working across a deletion. Hard redaction exists for legal takedowns, is the one operation that is not append-only, and is CLI-only.

Split and merge are the answer to thread drift, which is the known cost of flat threads. Posts are copied into the target log as new Post records carrying an origin reference, and the source posts get a moved flag. Ordinals in the source are never renumbered, so every permalink ever issued keeps resolving.

docs/08 also lists what is deliberately absent: no karma, no vote-ranking, no trust levels, no word filters, no shadow banning. That list is policy.

## Acceptance criteria

- [ ] Every moderation action writes an audit_log row naming the actor, the target, and the time
- [ ] Every thread-scoped action is recoverable from the thread log alone after a database rebuild
- [ ] A soft-deleted post keeps its ordinal, and replies to it keep their reply_to link
- [ ] Splitting a thread renumbers nothing in the source, and every source permalink still resolves
- [ ] Moderators see tombstone contents inline; members see the tombstone only
