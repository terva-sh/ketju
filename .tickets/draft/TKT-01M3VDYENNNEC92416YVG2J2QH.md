---
schema: 3
id: TKT-01M3VDYENNNEC92416YVG2J2QH
title: Find why CI runner b3cc8959 freezes every job for about ten seconds
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ci:1790
    path: null
  - ref: ci:1592
    path: null
claim: null
archive: null
created_at: 2026-10-01T09:50:19Z
updated_at: 2026-10-01T09:51:14Z
created_by:
  id: agent:claude-code/phase3-run-vbwma8
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-vbwma8
  name: ""
extensions: {}
---

## Description

Two CI failures came from the same cause: Forgejo runner `b3cc8959-8e64-43c7-82c4-c0ba937d9d05` (act runner v13.0.0) froze every job it was running, for about ten seconds at a time. Run 1592 failed on 2026-09-30 (TKT-01M3SZ3GCE) and run 1790 on 2026-10-01 (TKT-01M3VBWMA8). The step-log timestamps show each freeze in two or three containers at once, with stalls that ended within a second of each other:

- **2026-09-30, 19:42:50 to 19:43:01 UTC.** Run 1592's `admin-applications.spec.ts:40` failed after 9.0 s. Run 1595's `targets.spec.ts:69` took 10.9 s, against 870 ms usually.
- **2026-10-01, 09:11:24 to 09:11:33 UTC.** Run 1790's thread GET spent 8892 ms in the handler, with no other request in flight. Run 1794's `admin-applications.spec.ts:40` took 14.4 s, against 7.2 s. Run 1798's thread-page axe scan took 11.3 s, against 1.5 s. Run 1792, on the other runner (build-0), made progress through the window.

TKT-01M3VBWMA8's investigation note has the tables and the probe that rules out a cause inside ketju.

### What to do

This is outside the repository (docs/14 stop class 3). It needs someone with access to the runner's host. Look at that host's records for the two windows above:
- memory pressure and swap (PSI in `/proc/pressure/*`, or `vmstat`);
- I/O stalls;
- CPU steal, or a pause of the host or VM;
- the number of jobs it took at once. Six ketju CI runs shared the runners at 09:11 on 2026-10-01.

Then change the runner, for example its job capacity or memory, so that a job is not frozen for seconds.

Once it is fixed, unblock TKT-01M3VBWMA8 and TKT-01M3SZ3GCE. Close each against this ticket unless its test fails again on a runner that no longer freezes.

## Acceptance criteria

- [ ] A note names what froze the runner in at least one of the two windows, from the host's own records
- [ ] The runner is changed so that the cause named does not freeze a job, and the change is recorded in a note
