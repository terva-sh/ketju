---
schema: 3
id: TKT-01M3MTQG5ATJPTEPZHNBYHAHBF
title: Stop Playwright checks timing out under CI runner contention
type: bug
status: in-progress
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
references: []
claim:
  actor: agent:claude-code/phase2-run-mtqg5a
  branch: run/mtqg5a-playwright-contention
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-aa13a7c1a9ba735db
  commit: 0ece8913bbe21d0ea8f65bc4b1f6ee530da827ca
  session: null
  claimed_at: 2026-09-30T11:03:50Z
  expires_at: null
archive: null
created_at: 2026-09-28T20:19:02Z
updated_at: 2026-09-30T11:03:54Z
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

- [ ] A note on this ticket names, for runs 728 and 751 and any later a11y timeout, the test, the action that timed out, Playwright's error text and the run that shared the runner, with each a11y test's duration from a green run
- [ ] e2e/a11y.spec.ts runs exactly one axe scan per test, with the same rule tags and the same zero-violation assertion, and closes its browser context when a test fails
- [ ] Under CPU contention on the same cores, npx playwright test e2e/a11y.spec.ts --repeat-each=5 passes on both projects, and a note records the command, the load and the per-test durations
- [ ] playwright.config.ts's timeouts are unchanged, or a note records the measured scan duration that justifies a raise scoped to a11y.spec.ts

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
