---
schema: 3
id: TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
title: Build the offline hard-redaction tool
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - area:ops
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BSPE2ZP7X2C736K2KVPBA
  - TKT-01M29C5Y1E782FS135JRVBFRJ6
blocks_on: none
references:
  - ref: design:storage-records
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The CLI-only operation that removes body text from a log for a legal takedown or a GDPR erasure. It is the one thing in the system that is not append-only, which is why it is a command run by an admin with the server stopped rather than a button.

Shape follows the decision from the redaction spike. It rewrites the log, writes an audit_log entry, and leaves the ordinal, the tombstone, and every permalink intact.

The rendered-post cache and the search index both hold copies of the text, so redaction has to clear them too. A body erased from the log and still returned from a cache is a redaction that did not happen.

## Acceptance criteria

- [ ] The body is unrecoverable from the log afterwards while the ordinal and its tombstone remain
- [ ] The rendered-post cache entry and the search index entry are both cleared
- [ ] The operation is recorded in audit_log with the operator and the reason
- [ ] The command refuses to run against a live server, or coordinates with it safely
