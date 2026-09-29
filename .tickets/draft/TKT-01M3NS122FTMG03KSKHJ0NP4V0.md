---
schema: 3
id: TKT-01M3NS122FTMG03KSKHJ0NP4V0
title: Clear the search index in the hard-redaction tool
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:search
  - area:storage
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
  - TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:32Z
updated_at: 2026-09-29T05:08:32Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The hard-redaction tool (TKT-01M29CXJZ9, Build the offline hard-redaction tool) must clear the search index's copy of a redacted post. docs/03 "Hard redaction" and that ticket's description both say so: "a body erased from the log and still returned from a cache is a redaction that did not happen".

This build has no search index. FTS5 arrives with TKT-01M29CRJ3B (Build the FTS5 search index and indexer, phase-3), so the redaction tool, planned for phase-2, has no index to clear, and its plan hands this part here. Whichever of the two lands second has to do this, and the dependencies below make that this ticket.

### What to do
- In `ketju redact`, after each log is swapped and before the journal is deleted, delete the FTS rows for every (thread, ordinal) the journal lists. Every revision's row goes, not only the current one.
- The indexer ignores a `Redacted` record's post: a catch-up or `ketju reindex --search` over a redacted log indexes nothing for it. Because the post's frames carry no source after the rewrite, this should hold without special handling. A test proves it.
- A search for a word unique to the redacted text returns nothing after the command, whether the index was built before the redaction or rebuilt after it.

## Acceptance criteria

- [ ] ketju redact deletes the search rows of every (thread, ordinal) it redacts, every revision included, before it deletes its journal
- [ ] A search for a word found only in the redacted text returns nothing, whether the index was built before the redaction or rebuilt after it by ketju reindex --search
