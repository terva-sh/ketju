---
schema: 3
id: TKT-01M3VRFRFW9M4XY8D14FXAJA23
title: Order a new post's index entry before the header that covers it
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:storage
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T12:54:32Z
updated_at: 2026-10-02T15:42:10Z
created_by:
  id: agent:claude-code/phase3-vhsyfr
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3VHSYFR (Write back index entries an Extended replay changes before trusting it), 2026-10-01.

### What can happen

`Actor.commit` (`internal/store/threadlog/actor.go`) writes a new post's index entry and then the index header, and fsyncs neither until `Close`. A power cut can persist the header page and not the page holding the new entry. On the next open, the header covers the log, so the index is trusted, and the new post's entry reads as zeros: offset 0, frame length 0, author 0, flags 0.

TKT-01M3VHSYFR fsyncs the index before the header for a batch that changes an existing entry, so a hide, delete, edit or redaction is never lost this way. It deliberately adds no fsync to a batch of new posts, because that would double the fsyncs on the most common write.

### Why it does not widen access

A post's entry starts visible, and every record that hides it is a mutation, which that ticket syncs. A zeroed entry reveals nothing hidden. The likely effect is a read error for that one post (a zero-length frame read at offset 0) until `ketju fsck --rebuild` or a discarded index rebuilds it. The filesystem may also refuse the case outright: with ext4 `data=ordered`, a size extension is not committed before its data.

### Possible fixes

- On open, check that every entry's offset and frame length name a frame boundary inside the log. This is cheap only with sampling, since checking every entry is the replay the trusted path avoids.
- Have the header lag: never let it cover a batch until a later index fsync has made that batch's entries durable.
- Measure first: try to provoke the case on ext4 and xfs before deciding it is worth code.
