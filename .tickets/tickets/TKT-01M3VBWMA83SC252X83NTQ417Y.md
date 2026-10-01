---
schema: 3
id: TKT-01M3VBWMA83SC252X83NTQ417Y
title: Find the nine-second server stall on a thread page after a read reset
type: bug
status: blocked
status_reason: docs/14 stop class 3, outside the repository. CI runner b3cc8959 froze three jobs at once (runs 1790, 1794, 1798) from 09:11:24 to 09:11:33 UTC on 2026-10-01. The thread GET's 8892 ms is that freeze, measured from the one request in flight. A probe under CPU and fsync load found nothing in ketju that the GET waits on (note of 2026-10-01). Unblocked when TKT-01M3VDYENN (Find why CI runner b3cc8959 freezes every job for about ten seconds) is done, which needs the runner's host.
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
dependencies:
  - TKT-01M3VDYENNNEC92416YVG2J2QH
blocks_on: none
references:
  - ref: pr:272
    path: null
  - ref: ci:1790
    path: null
claim: null
archive: null
created_at: 2026-10-01T09:14:22Z
updated_at: 2026-10-01T09:51:14Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-vbwma8
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

## Acceptance criteria

- [x] Before any code change, a note names what the thread GET waited on in run 1790, with the runs and profiles that separate it from each candidate in the description
- [ ] No retry, no longer timeout and no skip is added for this stall, and no ketju code changes unless a cause in ketju is seen and named in a note
- [ ] After TKT-01M3VDYENN is done, a note lists the CI runs on the changed runner in which smoke.spec.ts passed at both widths and no thread-page request in a printed serve.log took over 1 s

## Implementation plan

Groomed 2026-10-01 against main at f3538e6, after the investigation note of the same date.

### Settled in grooming

- **The fix addresses the cause.** No retries, no longer test timeouts, and no skips (the description).
- **The cause is the CI runner, not ketju** (investigation note, 2026-10-01). Runner `b3cc8959` froze three jobs in three containers at 09:11:24 to 09:11:33 UTC, run 1790 among them, while a job on runner build-0 kept making progress. A probe of the real `serve` under CPU and fsync load never caught the thread GET waiting on a lock, a pool connection, the writer or the projection. Nothing in ketju is changed for this ticket. A change made without a seen cause is what the description forbids.
- **Blocked under docs/14 stop class 3** (anything outside the repository). Finding and fixing what freezes the runner needs its host. TKT-01M3VDYENN (Find why CI runner b3cc8959 freezes every job for about ten seconds) holds that work. This ticket depends on it.

### Approach

1. Wait for TKT-01M3VDYENN to be done.
2. Then read the Playwright step of every CI run on the changed runner since the change. If no thread-page request in a printed serve.log takes over 1 s, and `smoke.spec.ts` passes at both widths, write a note listing the runs and close this ticket against TKT-01M3VDYENN.
3. If a slow thread GET comes back on a runner whose other jobs kept running through the same window, go back to the investigation note's method:
   - read the concurrent runs' step-log timestamps;
   - run the probe with its watchdog goroutine dump and block profile.
   Then re-plan against the cause that the dump names.

### Tests

None in ketju while the cause is outside it. If step 3 finds a cause in ketju, the re-plan names a test that fails before the fix, as the description asks: for example, a concurrency test asserting that `GET /threads/{id}` does not wait on `DELETE /threads/{id}/read`.

### Out of scope

- The runner itself, its capacity, memory and host: TKT-01M3VDYENN, and docs/12 "Changing CI" forbids it to a run PR anyway.
- TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load). The same runner freeze explains its run 1592, and a note there records the evidence. Its status is its owner's.
- Write latency under fsync contention. The probe saw writes take up to 3.3 s in SQLite's WAL `fdatasync` under three fsync loops. That is `synchronous(full)`, which docs/04 chose, and it is not what failed in run 1790.

## Notes

**agent:claude-code/phase3-run-vbwma8** at 2026-10-01T09:50:37Z

Investigation, 2026-10-01, on main at f3538e6: the stall is the CI runner, not ketju. Runner `b3cc8959-8e64-43c7-82c4-c0ba937d9d05` stopped every job it was running for about nine seconds. Nothing in ketju made the thread GET wait.

### The evidence: three jobs on one runner stalled at the same moment

Forgejo stamps every step-log line with the time the runner sent it. I read the Playwright step of every Build and Test run that was in progress at 09:11 UTC. Each run's set-up step names its runner.

| Run | Runner | What stalled | Ran from | Ended | Usual length |
|---|---|---|---|---|---|
| 1790 | b3cc8959 | carol's `GET /api/v1/threads/0387pph1rr000`, in the handler | 09:11:24.17 | 09:11:33.07 | 17 ms (alice's, 0.5 s earlier) |
| 1794 | b3cc8959 | `[desktop] admin-applications.spec.ts:40`, 14.4 s | 09:11:18.96 | 09:11:33.37 | 7.2 s in 1790 |
| 1798 | b3cc8959 | `[phone] a11y.spec.ts:82`, the thread page in the dark theme, 11.3 s | 09:11:23.37 | 09:11:34.68 | 1.5 s in 1790 |
| 1792 | build-0 | nothing: tests 66 and 67 finished at 09:11:26.3 and 09:11:29.4, in 3.2 s and 2.9 s | | | |

The three runs on b3cc8959 are three containers, each with its own ketju server, its own Chromium and its own data directory. All three lost about nine seconds that ended between 09:11:33.0 and 09:11:34.7. A run on the other runner, build-0, kept making progress through the same window. A cause inside one ketju process cannot reach the other two containers.

Run 1790's own log shows the same thing from inside one container. Playwright's 5 s expectation for `#unread` started at about 09:11:24.2 and should have failed at 09:11:29.2. Instead, the GET returned at 09:11:33.07. Carol's page then fetched a font and `GET /api/v1/drafts` at 09:11:33.28 and 09:11:33.30. Alice's page sent its closing `PUT /read` at 09:11:33.26 while the test was torn down. The test lasted 10.6 s: start 09:11:22.7, end 09:11:33.3. That fits a freeze from 09:11:24.2 to 09:11:33.0, after which the overdue timer fired and the response arrived together. It does not fit a GET that waited alone while the browser and Playwright ran on.

### What the server could have waited on, ruled out

Ketju was idle during the window. carol's GET was the only request in flight. The DELETE ended 0.34 s earlier and was the last write. No job logged a run (`jobs.Scheduler` logs every run and jitters its start, so the jobs of separate servers do not run together).

A probe, not committed, ran the real `serve` in-process with the smoke test's sequence 300 times: alice replies, alice reads the page, alice deletes her draft, carol `DELETE /read {ordinal: 60}`, carol `GET /threads/{id}`, carol `GET /drafts/{id}`. It was a 60-post thread, with the data directory on ext4. Block profiling was at 1 ms, mutex profiling was on, and a watchdog dumped every goroutine for any request that took over 700 ms. The script and dumps are in `~/.cache/agent-scratch/ketju/vbwma8-bZTq/`.

- **No load:** carol's GET p50 0.9 ms, max 1.6 ms.
- **36 busy loops and 3 loops writing 256 MiB files with fsync, on the 12-CPU host (load 30):**
  - Carol's GET: p50 1.3 ms, p99 193 ms, max 516 ms.
  - The slow requests were all writes, up to 3.3 s. Each watchdog dump shows them in SQLite's WAL `fdatasync`, `_pagerWalFrames` to `vfs.osSync`, or queued for the one write connection behind it. That is `synchronous(full)` working as docs/04 chose it.
  - No GET was ever caught waiting. The block profile has no read-pool connection wait, no lock, and no reader behind a writer.

So, for each candidate in the description:

- **SQLite lock waits:** ruled out.
  - A read transaction in WAL mode does not wait for the writer or its fsync, and the probe never caught one doing so.
  - `busy_timeout` (5000 ms) would end at 5 s with `ErrBusy` and an error status. The GET answered 200 after 8.9 s.
  - The read pool (4 connections) had no other user.
  - Group commit does not apply: e2e runs `fsync = "never"`, and the GET appends nothing.
- **The response cache:** ruled out. `pagePosts` keys on the log length, the projection, the page, the class and the authors. It has no single-flight and no lock held across a computation. A miss renders the page's posts in milliseconds, as the probe's p50 shows.
- **Projection or bus catch-up:** ruled out. `Bus.Publish` never blocks. The projection applies events on the write connection, and the GET reads the projection row through `access` without waiting for it.
- **The DELETE's own transaction:** ruled out. It committed in 3 ms, 0.34 s before the GET began.

### What this means

The 8892 ms is the runner's freeze, measured from inside the one request that was in flight. Nothing in ketju to fix is in evidence. A change made here would guess at a cause that was not seen, which the description forbids.

What froze b3cc8959 (memory pressure and swap, an I/O stall, or the host or VM pausing) cannot be seen from the repository. That is docs/14 stop class 3, anything outside the repository. The ticket is blocked on it.

The same runner shows the same pattern for TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load), run 1592. See the note there.

**agent:claude-code/phase3-run-vbwma8** at 2026-10-01T09:51:09Z

in-progress to blocked: docs/14 stop class 3, outside the repository. CI runner b3cc8959 froze three jobs at once (runs 1790, 1794, 1798) from 09:11:24 to 09:11:33 UTC on 2026-10-01. The thread GET's 8892 ms is that freeze, measured from the one request in flight. A probe under CPU and fsync load found nothing in ketju that the GET waits on (note of 2026-10-01). Unblocked when TKT-01M3VDYENN (Find why CI runner b3cc8959 freezes every job for about ten seconds) is done, which needs the runner's host.
