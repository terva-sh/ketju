---
schema: 3
id: TKT-01M3TSZHSWT4VE8G07NJMTR203
title: Stop the slow-mode e2e test failing on a phone under CI load
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
  - effort:S
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T04:01:24Z
updated_at: 2026-10-01T06:45:52Z
created_by:
  id: agent:claude-code/phase2-run-tnbknq
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

Filed by the TKT-01M3TNBKNQ lane on 2026-10-01. PR #258's CI run #1686, on head 7d28c7e, failed in the Playwright step. That head changes only `.tickets/`. The suite took 7.0 min, with 2 failed, 11 skipped and 191 passed.

- **Phone, `e2e/thread-moderation.spec.ts:106`.** "a moderator sets slow mode and turns it off" saved 10 seconds (the PUT answered 200 and the panel closed). It reopened the panel, then waited 5 s for the "10 seconds" radio to be checked. The locator resolved 14 times, and the radio was unchecked every time.
- **Desktop, line 96.** The panel opened on 10 seconds, not Off. The phone test never reached its own "Off" step, so it left the thread at 10 s. This failure follows from the first one.

### A likely cause, not yet proven

After the first Save, the desktop project waits for the composer's `[data-slow-mode]` line, which appears only once the thread page has been fetched again. The phone project has no such wait, because the phone's composer is its sheet. So on a phone, the panel can reopen from the page data from before the save. If the panel takes its initial choice when it opens, it then stays on Off even after the refetch lands. Under load, the refetch takes longer, which would explain why the test passed in every earlier run.

### What to do

Confirm the cause under load (TKT-01M3SZ3GCE's notes have a load recipe), from the trace or from the evidence TKT-01M3TNBKNQ prints once it lands. Then fix it where it is: either the spec waits on the refetch on a phone too, inside the existing 5 s, or the panel follows the thread's value. No retries, no longer timeouts and no skips, the same rule as TKT-01M3SZ3GCE.

## Notes

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:17:38Z

Milestone cleared, 2026-10-01: the orchestrator's scope note says drafts a lane files go in with no milestone, for the maintainer to place, so that the run does not grow its own scope. It was filed as phase-2 before that note.
