---
schema: 3
id: TKT-01M3MTQG5ATJPTEPZHNBYHAHBF
title: Stop Playwright checks timing out under CI runner contention
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:TKT-01M3RZXGBAN5GRYMZ7HA3A7VQG
    path: null
  - ref: ticket:TKT-01M3S1TVPD1MV96KCAHRJSRQVG
    path: null
  - ref: pr:223
    path: null
claim: null
archive: null
created_at: 2026-09-28T20:19:02Z
updated_at: 2026-09-30T11:52:32Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
extensions: {}
---

## Description

Twice during the hardening sweep on 2026-09-28, a Playwright check timed out on a PR whose code it does not touch, and passed on rerun. Both times another lane's CI run was on the same runner:
- run 728 (PR #125): the phone forum-index axe scan;
- run 751 (PR #128): the desktop thread-page a11y check, waiting for the reply expander.

Each costs a rerun, and under the docs/12 gate a red run blocks a merge. Two occurrences are not yet a pattern worth changing timeouts for blind.

### To do
- Collect the failing step logs of both runs, and any later ones, and name the waits that timed out.
- Decide between: waiting on the condition the test needs instead of a fixed timeout; raising the per-test timeout under CI only; and serialising Playwright on the shared runner.
- Don't loosen an assertion to make it pass.

## Acceptance criteria

- [x] A note on this ticket names, for runs 728 and 751 and any later a11y timeout, the test, the action that timed out, Playwright's error text and the run that shared the runner, with each a11y test's duration from a green run
- [x] e2e/a11y.spec.ts runs exactly one axe scan per test, with the same rule tags and the same zero-violation assertion, and closes its browser context when a test fails
- [x] Under CPU contention on the same cores, npx playwright test e2e/a11y.spec.ts --repeat-each=5 passes on both projects, and a note records the command, the load and the per-test durations
- [x] playwright.config.ts's timeouts are unchanged, or a note records the measured scan duration that justifies a raise scoped to a11y.spec.ts

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Refreshed against main at 5124209 on 2026-09-30: `a11y.spec.ts` and `playwright.config.ts` are unchanged since 7af2c5a, and no later a11y timeout is recorded. One figure is corrected: the thread-page scan covers page 1, which holds 25 of the seeded thread's 60 posts.

### Settled in grooming
- This ticket's own rules: name the waits that timed out before changing anything, and do not loosen an assertion to make a test pass.
- Where the two failures were, from the notes on the tickets whose PRs they hit:
  - Run 728, PR #125, recorded on TKT-01M39VNEC1: `[phone] e2e/a11y.spec.ts ... the forum index passes the scan in both themes`. "The axe `analyze()` call passed its 30 s test timeout. It was the first test after session setup." Run 724 was on the runner at the same time.
  - Run 751, recorded on TKT-01M3AFHGJD: `[desktop] e2e/a11y.spec.ts, 'the thread page passes the scan in both themes'. It hit the 30 s test timeout while waiting to click '#post-1 button.reply-expander'.`
- Neither failure is a wait on a condition that never came. Both tests are single `test()` bodies in `web/client/e2e/a11y.spec.ts:58-76`, and each runs up to four axe scans (light and dark, before and after `open`) inside Playwright's default 30 s per-test budget. `playwright.config.ts` sets no `timeout`. Under a second CI run on the same runner, the scans used up the budget, and the timeout landed on whichever step was running then. The thread-page scan covers page 1 of the seeded thread, 25 of its 60 posts (`DefaultPageSize` in `internal/service/threads/view.go:39`; `cmd/ketju/e2e.go:43`). So the fix is to spend the budget per scan, not to wait differently.
- Serialising Playwright on the shared runner is out of this ticket's reach. The workflow has one required job, and docs/12 "Changing CI" lets a PR add steps to it but not change the workflow's triggers or add jobs; a workflow-level `concurrency` group is not in the list. Runner capacity is infrastructure. The alternative that lost here is also the ticket's second option, raising the per-test timeout under CI. It hides a slow scan instead of bounding it, and it widens every test's budget to fix two.

### Approach
1. **Evidence first.** Read the step logs of runs 728 and 751 through the Forgejo web route (`POST https://git.local.sothr.com/terva-sh/ketju/actions/runs/<n>/jobs/<j>/attempt/<a>` with `{"logCursors":[{"step":N,"cursor":0,"expanded":true}]}`), plus any later a11y timeout found in `GET /api/v1/repos/terva-sh/ketju/actions/tasks`. Record in a note, for each failure:
   - the test;
   - the line and the action that timed out;
   - Playwright's error text;
   - which other run shared the runner.
   From one recent green run, record each a11y test's duration from the `list` reporter, which gives the headroom.
2. **One scan per test** in `web/client/e2e/a11y.spec.ts`: generate a test for each (screen, state, theme), where state is "as loaded" or the screen's `open` state. The name is `the ${name}${open ? ' with ' + open : ''} passes the scan in the ${theme} theme`. Each test sets `emulateMedia({colorScheme})` *before* `goto`, waits for `ready`, runs `open` when its state needs it, and runs one `scan`. The rule tags, the zero-violation assertion and the message stay as they are.
3. **Close contexts on failure.** Replace the hand-made `browser.newContext` with `test.use({ storageState: stateFile('alice') })` at file level and the built-in `page` fixture. Today's `ctx.close()` at the end of the body is skipped when an assertion fails, which leaves a context open for the next test on a runner already short of CPU.
4. **Prove it under contention.** Locally, pin the run and a CPU load to the same cores, for example `taskset -c 0,1` around both `npx playwright test e2e/a11y.spec.ts --repeat-each=5` and a `stress-ng --cpu 2` (or a concurrent `go test ./...`). Record the exact command, the load and the per-test durations in a note.
5. `playwright.config.ts` is not changed. If step 4 shows that one scan alone comes near 30 s under that load, stop and record the measurements in a note before raising anything. A timeout raise is then scoped to `a11y.spec.ts` with `test.setTimeout`, citing the measurement.
6. No `web/client/src` change, so the dist and the bundle budget are untouched.

### Tests
- The split `a11y.spec.ts` itself: 4 tests for the forum index, 2 for the thread list and 4 for the thread page per project, 20 in all, where there were 6.
- The contention run in step 4, recorded as evidence.
- CI's `npx playwright test` step passes on the PR.

### Out of scope
- The fixed dwell sleeps in `read-tracking.spec.ts` and `shortcuts.spec.ts`. They wait for the client's own dwell timers, not for the page, and neither has timed out.
- The axe scans in the other specs: `admin-agents`, `admin-applications`, `admin-forms`, `admin-queue`, `apply` and `phase2-exit` each scan both themes in one test. None has timed out. If one does, the same split applies. `phase2-exit.spec.ts:92` already raises its own budget with `test.slow()`, the kind of scoped raise step 5 allows only with a measurement behind it.
- The CI container image. docs/12 allowed a Debian image, and CI runs Alpine's Chromium (`ci.yml:122-137`). This ticket does not change it.
- Workflow `concurrency` and runner capacity. Changing either needs the maintainer, because docs/12 "Changing CI" does not permit it.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:52:39Z

Plan refreshed against main at 5124209 on 2026-09-30. The plan is current in substance: a11y.spec.ts (3 tests per project, the loop at :58-76, browser.newContext at :60 and ctx.close at :75) and playwright.config.ts (workers 1, retries 0, no timeout) are unchanged since 7af2c5a, and neither PR #194 nor #202 touched them. PR #194's new CI step ('Phase 2 exit tests, by name') is Go only. No a11y timeout after runs 728 and 751 is recorded in the store. Changed: the thread-page scan covers page 1, 25 of the seeded thread's 60 posts, not a 60-post page (DefaultPageSize 25). Added to Out of scope: the both-themes axe scans in admin-agents, admin-applications, admin-forms, admin-queue, apply and phase2-exit, none of which has timed out; phase2-exit.spec.ts:92's test.slow() is noted as the precedent for a scoped raise that step 5 allows only with a measurement. The Settled items and the criteria are unchanged. TKT-01M3R5P3CN (Keep the Go race tests inside their timeout under CI runner load) is the Go counterpart and does not overlap.

**agent:claude-code/phase2-run-mtqg5a** at 2026-09-30T11:06:55Z

Evidence, read from the Playwright step logs (step 11, "Playwright smoke at 390 px and 1440 px") through the web route, and from `GET /actions/tasks` for the runs that overlapped. The overlap is computed from each run's start and its step durations; `updated_at` on old tasks is rewritten and cannot be used.

### The three a11y timeouts
Every CI run from 751 to 1416 whose status is failure was read. Run 950 is the only later a11y failure. No other Playwright failure is recorded after 751.

- **Run 728** (PR #125, head 3fb6537), 2026-09-28. `[phone] e2e/a11y.spec.ts:69:3 › the forum index passes the scan in both themes (30.0s)`. It timed out in `scan` at a11y.spec.ts:60:26, the axe `analyze()`, called from :76, which is the first light/dark loop. It was the page as loaded, before `open`. The error was `Test timeout of 30000ms exceeded.` then `Error: frame.evaluate: Test ended.` at `AxeBuilder.runPartialRecursive`. It was the first test after session setup. The same test on desktop took 4.2s in that run. The other runs during its Playwright step (18:36:24-18:39:10) were run 724 (PR, `go test (race)`) and run 722 (push to main, the fuzz, replay, catch-up, benchmark and spike steps).
- **Run 751** (PR for TKT-01M3AFHGJD, head d2fbb36), 2026-09-28. `[desktop] e2e/a11y.spec.ts:69:3 › the thread page passes the scan in both themes (36.7s)`. The error was `Test timeout of 30000ms exceeded.` then `Error: locator.click: Test ended.` The call log held only `waiting for locator('#post-1 button.reply-expander')`, at a11y.spec.ts:39:61 in `open`, called from :78. The two as-loaded scans had already run, so the click got what was left of the 30 s. During its Playwright step (19:26:26-19:30:41), run 749 (PR, its own client tests and Playwright) and run 747 (push, `go test (race)`) were running.
- **Run 950** (PR for TKT-01M29CP2KZ, head 66b9669), 2026-09-29, a later one. `[phone] e2e/a11y.spec.ts:59:3 › the thread page passes the scan in both themes (31.2s)`. It timed out in `scan` at :53:26, the axe `analyze()`, with `frame.evaluate: Test ended.`, called from :72. That is the second loop, a scan with the composer sheet open, so it was the third or fourth scan. During its Playwright step (17:28:17-17:31:37), run 941 (PR, `go test (race)`) and run 954 (PR, setup, build and the sqlc check) were running.

### Green durations of the same tests
| run | phone index | phone list | phone page | desktop index | desktop list | desktop page | Playwright step |
|---|---|---|---|---|---|---|---|
| 1396 | 3.1s | 1.8s | 5.4s | 4.6s | 2.7s | 6.4s | 72 passed (2.8m) |
| 1403 | 5.1s | 2.6s | 7.7s | 4.4s | 1.9s | 8.8s | 72 passed (3.3m) |
| 1407 | 2.7s | 1.5s | 5.0s | 4.5s | 2.5s | 7.1s | 72 passed (2.6m), step 2m46s |

In the failing runs, the a11y tests that passed ran up to about twice as slow as on green runs: in 751 the desktop index took 8.8s against 4.4-4.6s, and in 950 the phone index took 10.7s against 2.7-5.1s. The load came and went over the step, so the test that met its peak was slower still. Each test ran up to four scans in one budget, and the thread page's four already take 5-9s on a green run. A slowdown of four to six times at the wrong moment uses up 30 s. The timeout then lands on whichever step is running: a scan in 728 and 950, and the expander click in 751. No wait was on a condition that never came.

### Other slow checks, from the same green runs (not a11y)
`admin-forms.spec.ts:35` "an admin edits the registration form and saves a new version" took 9.5-14.7s. `admin-agents.spec.ts:45` took 7.1-12.1s. Neither has timed out, but each runs all its axe scans in one test: admin-forms runs eight (four states in two themes) and admin-agents ten (five states in two themes), against the a11y thread page's four. The plan puts them out of scope, but under the same slowdown they are closer to the limit than the a11y thread page was on a green run.

A draft is filed for them: TKT-01M3RZXGBA (Bound the axe scans in the admin forms and agents Playwright tests).

**agent:claude-code/phase2-run-mtqg5a** at 2026-09-30T11:40:37Z

Contention run, on a 12-core Linux host, from web/client, the same load before and after the split.

### Command and load
The run and the load are pinned to the same two cores. The load is N busy loops, because stress-ng is not installed:
```
for i in $(seq N); do taskset -c 0,1 yes >/dev/null & done
KETJU_E2E_PORT=8843 taskset -c 0,1 npx playwright test e2e/a11y.spec.ts --repeat-each=5
kill <the loops>
```
The ketju e2e server and Chromium are children of the run, so they inherit the pinning. With N=14, Playwright, the server and Chromium share two cores with 14 busy loops, about 1/8 of the CPU they would have alone. Unloaded, the old spec's thread page took 4.0-4.7s.

### Before (main at 0ece891, 3 tests per project), per test over 5 repeats, min / median / max
| load | project | forum index | thread list | thread page |
|---|---|---|---|---|
| none | phone | 2.3 / 2.4 / 2.5s | 1.0 / 1.1 / 1.2s | 4.0 / 4.2 / 4.5s |
| none | desktop | 2.4 / 2.7 / 3.0s | 1.1 / 1.2 / 1.2s | 4.1 / 4.4 / 4.7s |
| N=6 | phone | 4.8 / 8.4 / 8.7s | 2.4 / 3.2 / 4.1s | 12.0 / 14.4 / 14.9s |
| N=6 | desktop | 6.9 / 7.6 / 8.2s | 2.5 / 3.5 / 4.2s | 9.7 / 15.0 / 15.2s |
| N=14 | phone | 8.2 / 13.1 / 14.9s | 6.0 / 7.0 / 7.6s | 22.0 / 26.9 / **29.6s** |
| N=14 | desktop | 10.7 / 14.1 / 15.7s | 2.4 / 6.5 / 7.4s | 17.5 / 24.7 / 27.4s |

At N=14 all 34 passed, but the phone thread page used 29.6s of its 30s budget. That is the same test and failure as run 950.

### After (this branch, 10 tests per project), N=14, all 104 passed (10.6m), per test min / median / max
| test | phone | desktop |
|---|---|---|
| forum index, light | 2.4 / 4.9 / 5.4s | 4.6 / 5.3 / 5.8s |
| forum index, dark | 1.4 / 4.7 / 6.0s | 4.2 / 5.2 / 5.9s |
| forum index with the picker open, light | 1.5 / 5.1 / 5.5s | 4.3 / 5.0 / 5.7s |
| forum index with the picker open, dark | 2.5 / 5.0 / 5.3s | 1.8 / 4.9 / 5.6s |
| thread list, light | 4.0 / 4.2 / 4.3s | 1.3 / 4.2 / 5.2s |
| thread list, dark | 3.8 / 4.3 / 4.5s | 1.5 / 4.0 / 4.3s |
| thread page, light | 7.6 / 8.2 / 8.4s | 2.8 / 7.8 / 8.3s |
| thread page, dark | 7.2 / 8.1 / 8.6s | 3.5 / 7.7 / 8.6s |
| thread page with #1's replies (desktop) or the composer sheet (phone) open, light | 8.2 / 8.8 / 9.3s | 7.5 / 7.9 / 9.0s |
| same, dark | 3.6 / 8.5 / 8.9s | 7.4 / 7.9 / 9.0s |

The slowest test under the load that nearly broke the old spec is 9.3s, 31% of the budget. Each test also pays a page load now, so the a11y file costs more in total. Unloaded, the tests of one pass add up to 19.9s, against 16.4s before. At N=14 the whole `--repeat-each=5` went from 8.2m to 10.6m wall, most of it the loaded page loads.

### Mutation
I injected an `<img>` with no alt text, only when `prefers-color-scheme: dark` matched at scan time, and ran the phone thread list and the phone thread page with the sheet open. Both dark tests failed with `image-alt (critical)`, under the messages `thread list, dark` and `thread page with the composer sheet, dark`. The light tests passed. So the theme is in effect when each scan runs, and the message still names the opened state. I reverted the injection.

### playwright.config.ts
It is unchanged, and so are all its timeouts. No test.setTimeout or test.slow was added.

**agent:claude-code/phase2-run-mtqg5a** at 2026-09-30T11:40:47Z

The orchestrator reported that desktop `smoke.spec.ts` fails with a 429 from `/auth/*` in a full local run. It reproduced here on this branch: the full suite ran unloaded in 2.3m, with 85 passed, 10 skipped, and 1 failed, the desktop smoke. The phone project and all 20 a11y tests passed. The cause is not runner contention but the reverse. The suite spends 28 `/auth/*` requests from one address against a burst of 10 that refills one token every 6 s, so it passes only when it runs slowly enough. The desktop phase2-exit spec waits out its 429s and leaves the bucket empty about 10 s before smoke signs in. This branch does not change that gap, because a11y runs before apply.

It is left out of this ticket. The ticket covers checks that time out because the runner is slow. Both root fixes also reach past its scope. Reusing alice's stored session in smoke changes how TKT-01M29CGBY1's "sign in ... at 390 px and 1440 px" criterion is met. An e2e-only auth rate needs a hook outside `cmd/ketju/e2e.go`. It is filed with the server log timeline and both options as TKT-01M3S1TVPD (Stop the full Playwright run spending more /auth requests than it refills).

**agent:claude-code/phase2-run-mtqg5a** at 2026-09-30T11:43:34Z

Correction to the previous note: TKT-01M3S1TVPD is now titled 'Stop the full Playwright run spending more /auth calls than it refills', shortened to pass check's 72-character title limit.

**agent:claude-code/phase2-run-mtqg5a** at 2026-09-30T11:52:32Z

CI on PR #223, run 1435, head 7ad9fa0 on base 163c33a: the Playwright step took 2m19s and reported 86 passed and 10 skipped, where it had 72 passed before (run 1407: 2m46s; runs 1396 and 1403: 2.8m and 3.3m inside the step). That is 14 more tests, from 6 to 20 in a11y.spec.ts. The 20 a11y tests took 0.72-1.8s each, 24.3s together, against 23.3s for the 6 in run 1407. Terva review run 8e8a6ad1 (Actions run 1436) on that head and base was clean, with no findings. Nothing measured the other load on the runner during run 1435, so the step times compare runs, not the change alone.

## Summary

`web/client/e2e/a11y.spec.ts` now runs one axe scan per test. There is a test for each screen, state and theme, 10 per project and 20 in all, where there were 6. Each test sets the colour scheme before `goto`, waits for the screen, opens its state if the test is for one, and runs one scan. The rule tags, the zero-violation assertion and the failure message are as before. The page comes from the built-in fixture with alice's stored session through `test.use`, so a failing test's context is closed too. `playwright.config.ts` is unchanged, and no timeout was raised.

Why: runs 728, 751 and later 950 timed out because up to four scans shared one 30 s budget while other CI runs loaded the runner. The timeout fell on whichever scan or click was running. Under 14 busy loops pinned to the same two cores, the old thread page reached 29.6s of 30s. The split version's slowest test was 9.3s, and 104 of 104 passed.

Filed while working, both drafts:
- TKT-01M3RZXGBA (Bound the axe scans in the admin forms and agents Playwright tests). admin-forms runs 8 scans and admin-agents 10 in one test, and both are already slower on green runs than the a11y thread page was.
- TKT-01M3S1TVPD (Stop the full Playwright run spending more /auth calls than it refills). A full local run's desktop smoke gets a 429 from the /auth limiter. It is not runner contention, so it is left out of this ticket.
