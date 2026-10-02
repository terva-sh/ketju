---
schema: 3
id: TKT-01M3ZE2211GDC23K09PZ1GJ8BQ
title: Stop the Phase 3 exit Unread surface test failing under CI load
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T23:09:15Z
updated_at: 2026-10-02T23:09:15Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`e2e/phase3-exit.spec.ts:151` ("a member finds a thread with new posts on the Unread surface…") failed in CI on two unrelated Phase 4 PRs on 2026-10-02, once at each width. Each lane used its one allowed re-run, and the re-run passed.

- **Run 2491** (PR #374, method-not-allowed; phone): `[data-unread]` read "1 new" where the test expects "2 new", until the 5 s timeout.
- **Run 2522** (PR #373, backup; desktop): the thread's row on /unread had no `[data-unread]` at all within 5 s.

Neither PR touches the unread surface, lists, or read positions.

The test starts a thread as carol, sets alice's read position to 1, has carol post two replies, then opens /unread straight away. Both failures look like the list being read before every reply is reflected in what /unread serves: one reply counted, then none. That points at a lag between a reply's acknowledgement and the list's projection or unread count under CI load, or at the read-position write racing the replies.

Find which, and fix the cause:
- If the product can serve /unread a stale count after a reply is acknowledged, that is a product bug and the fix goes there, with docs/06's promise in mind.
- If the lag is allowed by the design, the test must wait for the state it asserts (for example, poll the API until the thread's post count is 3) rather than assume it.

Do not raise the timeout without knowing which.

## Acceptance criteria

- [ ] The cause of runs 2491 and 2522 is named in a note: a product lag, or a test that asserts before the state exists
- [ ] The fix addresses that cause, and the test passes 20 times in a row under go test -race load locally
