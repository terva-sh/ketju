---
schema: 3
id: TKT-01M3A8V7XCHBCPEH6HYHPVYM66
title: Publish quarantine before an interrupted live sweep returns
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T17:54:06Z
updated_at: 2026-09-28T18:06:55Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Review 468 on PR #67 (TKT-01M29C5Y1S, head 1979894, base 60f582f) raised a high that was deferred by the user's decision, not fixed. PR #67 was merged with it open.

### The finding
`Writer.CatchUp`, run as the live sweep (a batch reporting dropped events), collects corrupt threads in a local map and publishes them to `w.quarantined` only after the whole scan. Meanwhile its quarantine closure drops each thread from the behind set straight away. A sweep whose context is cancelled between those two points returns early. The thread is then neither held nor quarantined, so its endpoints serve a projection longer than its log.

It does not arise at startup. There a cancelled catch-up fails serve before the listener opens.

### Fix
Publish each quarantine as it is found, under `mu`: add to `w.quarantined` in the closure. At the end, replace the set only when the scan completed, so a completed scan still lets a repaired thread out.

Test: cancel the context from inside a Recover after a corrupt thread has been seen, then assert `Quarantined` is true and the thread is not in Lag.
