---
schema: 3
id: TKT-01M3VHSYFR176FWWBAPV8PHSEK
title: Write back index entries an Extended replay changes before trusting it
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T10:57:46Z
updated_at: 2026-10-01T10:57:46Z
created_by:
  id: agent:claude-code/phase3-run-crj3b
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-crj3b
  name: ""
extensions: {}
---

## Description

Found while building the search indexer (TKT-01M29CRJ3B, Build the FTS5 search index and indexer), 2026-10-01.

### What happens

When a thread opens with outcome `Extended`, `threadlog.Open` replays the frames after the `.idx` header's recorded length into the in-memory index. If one of those frames is a `Delete`, `Restore`, `Edit` or `Redacted` record, the replay changes an existing entry in memory. Nothing writes that entry back to the `.idx` file. The actor starts with `headerDirty` false. Its next append writes only its own ordinal's entry, then `flushHeader` advances the header's `LogLength` to cover the whole log. The `.idx` on disk now claims to cover every frame, but the entry the replay changed is stale on disk. The next open trusts it.

### How to reach it

The actor commits in this order (`internal/store/threadlog/actor.go`, `commit`): write the frames to the log, fsync the log, apply them to the index, write their entries, flush the header. A crash or power loss after the log fsync and before the entry write leaves the log holding a `Delete` that the `.idx` does not show. This is the case `Extended` exists for.

A scratch test reproduced it on main at 3890b38:

1. Create a thread and post 1. Close the cache, and keep a copy of the `.idx`.
2. Reopen, append a `Delete` of post 1, close. Put the old `.idx` back, which simulates the crash.
3. Reopen (the outcome is `Extended`, and post 1 is deleted in memory). Append post 2. Close.
4. Reopen. The outcome is `trusted`, and post 1's entry has flags 0 and is `Visible()`.

The deleted post is served again to every reader, and so is a moderator's hide or an auto-hide. After a hard redaction, the post comes back with blanked text and without its redacted tombstone. `ketju redact` writes a full `.idx` after its swap (`reindex` in `cmd/ketju/redact.go`), so the command itself is not exposed. A crash during normal serving is.

### Likely fix

When `Open` returns `Extended` or `Rebuilt`, the thread cache (or the actor at start) writes every entry the replay touched, or the whole index, before the first header flush can mark it trusted. Mark the header dirty from the start in that case. Add a crash test shaped like the steps above.

### Why it is filed here

It is outside the search ticket's scope: the search index reads the same entries, so it inherits whatever the thread page shows. It touches what readers can see (docs/14 "Invariants, added"), so it is filed as a bug with priority high, for the maintainer or the orchestrator to place.
