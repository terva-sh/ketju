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
updated_at: 2026-10-01T20:40:29Z
created_by:
  id: agent:claude-code/phase3-run-vbwma8
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
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

## Notes

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T15:16:52Z

More evidence, 2026-10-01 (orchestrator):
- Main push run 2116 on 902c30e ran on b3cc8959 and failed one test: [desktop] e2e/read-tracking.spec.ts:66, where the dwelt post 25 was missing from the beacon's sent list [1..5]; the test took 6.1 s. The same spec on 902c30e locally, --repeat-each=5 across both projects: 54/54 passed at 2.4-3.3 s each.
- PR run 2069 (TKT-01M29CTY9D) failed only on a Playwright timeout in apply.spec, also attributed to this freeze by that lane.
Main's own push CI now fails on this runner as well as PR runs.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T20:40:29Z

Findings, 2026-10-01 (orchestrator, read-only over SSH as sothr; nothing changed on either host):
- Runner b3cc8959 (forgejo-runner v13.0.0) is build-1 (tailnet 100.105.215.101): KVM, 6 vCPU, 3.76 GiB RAM, 8 GB swap on a disk partition (/dev/sda5), ~420 MB in use. Runner build-0 (v12.1.0) is build-0 (192.168.1.35): KVM, 4 vCPU, 4.9 GiB RAM, 4 GB swap, ~1 GB in use, and it also hosts Forgejo, MariaDB and a dind container.
- build-1 live sample (120 s, 2-3 concurrent jobs, one ketju Build and Test): each job ~1 GiB; MemAvailable fell to 1.8 GiB; a full memory stall in 103 of 120 seconds (short); ~160 MB swapped in and out. No 10 s freeze caught in that window. The system journal for the two freeze windows is not readable from the sothr account (not in adm).
- build-0 at the same time: load 36 on 4 vCPU, CPU pressure some avg10 95%, run queue 52-62, swapping out 16-81 MB/s, 4 concurrent jobs plus Forgejo and its DB.
- Reading: build-1's freezes most likely come from memory overcommit (several heavy jobs in 3.76 GiB, swapping to disk). build-0 is CPU-starved but slows evenly. Recommended: build-1 capacity 1 or 8 GB RAM; build-0 capacity 1-2, or move its runner off the Forgejo VM.
- Main push run 2184 failed on build-0 in TestServeGeneratesNotifications (test race after delete/restore under load); a lane is fixing it.
- #328 -> #330 TKT-01M29CTY71 lens header and switcher merged (8e7c604, b1b9c2f, dfd23a6); reviews 1792 (medium: First unread kept unread:B above the reader) and 1793 (medium: Full thread lost position; lens header now sticky) fixed; clean 8f330c24 on c264e78 / base 367c90d; CI 2196; reader 63878 -> 66189 B (+2311). Run decisions (docs/06): header wording, divider only canonical/unread, switcher authors from visible posts, since date shows UTC day, sticky lens header; read-tracking decision amended for 1730/1741/1792. Remote branch run/cty71-lens-client (#328, closed, carried) left for end-of-run cleanup.
- #331 budget raised to 120000 B (maintainer) merged; clean on 0b55baa, CI 2193.
- #332 TKT-01M3W9VRTV notifications test race (run 2184) merged 004be7a; test-only (author's restore reaches the projection via the bus; docs/04 sets no time bound); reproduced 4/20 under load before, 60/60 after; clean 0963125a on 4ba1b6f / base 86752c4; CI 2201. No other racy Phase 3 tests in cmd/ketju.
- The maintainer was given the recommendation and the hosts' details; the runner configs (/home/runner/config.yml) are not readable or writable from the sothr account, so the change is theirs.
