---
schema: 3
id: TKT-01M3NS147C2FBJF6TS3M6JA2WG
title: Decide how a thread title is hard-redacted
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:storage
  - effort:S
  - decision
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:35Z
updated_at: 2026-09-29T05:08:35Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

docs/03 "Hard redaction" covers a post's text: its `Post` and every `Edit`. A thread's title lives elsewhere, in the `ThreadCreated` body and in every `ThreadRenamed` body, and so does its slug. It is also copied into the `threads` projection row. A legal takedown or a GDPR erasure can cover a title as easily as a post, and today the only tool, a rename, leaves every earlier title in the log, readable by `ketju dump` and restorable by a reindex.

Found while planning TKT-01M29CXJZ9 (Build the offline hard-redaction tool), whose plan leaves titles out of scope because the design says nothing about them.

### To decide
- Whether `ketju redact` also takes a thread and blanks titles. It would blank `ThreadCreated.title`/`slug` and `ThreadRenamed.title`/`slug` the same way post bodies are blanked, with the same padding so the log does not shrink.
- What a thread with a redacted title then shows, since every surface needs some title.
- Whether the old slugs, which docs/03 keeps valid as redirects, stop resolving.
- Whether this needs a second critical record type or a `Redacted` body field naming the thread level (ordinal 0).

## Acceptance criteria

- [ ] The decision is recorded in docs/03 Hard redaction with the reasoning
- [ ] If titles are redacted, the work is filed or folded into TKT-01M29CXJZ9 before that ticket is claimed
