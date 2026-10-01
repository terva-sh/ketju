---
schema: 3
id: TKT-01M3VBWMA83SC252X83NTQ417Y
title: Find the nine-second server stall on a thread page after a read reset
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - area:storage
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:272
    path: null
claim: null
archive: null
created_at: 2026-10-01T09:14:22Z
updated_at: 2026-10-01T09:14:45Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

CI run 1790 (PR #270, which changes only docs and tickets) failed `e2e/smoke.spec.ts:29` on the desktop project. The failure evidence #261 prints (TKT-01M3TNBKNQ) shows the cause is on the server, not the test:

- 09:11:23.832: `DELETE /api/v1/threads/0387pph1rr000/read` for viewer 2 (carol), 3 ms. This is the test's `setReadPosition` helper.
- 09:11:24.04 to 24.22: carol's page loads its shell and assets, each in 0-23 ms.
- 09:11:33.065: `GET /api/v1/threads/0387pph1rr000` for viewer 2, **8892 ms**.
- 09:11:33.255: `PUT /api/v1/threads/0387pph1rr000/read` for viewer 1, 4 ms. It ran right after the stalled request ended.

Every other request in the window took under 25 ms. The page sat at "Loading…" and the 5 s expectation for `#unread` failed. The CI runner was heavily loaded at the time, with six grooming PRs' runs sharing it.

A thread page that can take nine seconds to answer fails Phase 3's daily-driver criterion, whatever the load that exposed it. The stall may also be what TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load) saw: that failure was never reproduced, and #1592 kept no server log.

### What to do
Find what the thread GET waited on. Candidates to rule in or out:
- SQLite lock waits: `busy_timeout`, a writer holding the lock, or the group-commit fsync;
- the response cache's single-flight or key computation after a read-position change;
- the projection or event-bus catch-up;
- the read-position delete's own transaction.

Reproduce under load (the #256 method) with the server log, and name the cause in a note before changing code. Fix the cause. No retries, no longer test timeouts.
