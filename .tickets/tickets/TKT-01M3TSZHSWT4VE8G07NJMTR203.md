---
schema: 3
id: TKT-01M3TSZHSWT4VE8G07NJMTR203
title: Stop the slow-mode e2e test failing on a phone under CI load
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
milestone: phase-3
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-tszhsw
  branch: run/tszhsw-slow-mode-phone
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a3b4596e38d57bd07
  commit: e855e1a120cb1b147f7f318af945769ed75e6f53
  session: null
  claimed_at: 2026-10-01T16:55:36Z
  expires_at: null
archive: null
created_at: 2026-10-01T04:01:24Z
updated_at: 2026-10-01T17:09:12Z
created_by:
  id: agent:claude-code/phase2-run-tnbknq
  name: ""
updated_by:
  id: agent:claude-code/phase3-tszhsw
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

## Acceptance criteria

- [x] A note names the cause from evidence before any fix: a failure under TKT-01M3SZ3GCE's load recipe with its trace and evidence block, or a probe that delays the thread's refetch, and whether the refetch answered 200 with the new slow_mode_seconds
- [ ] The fix is at the named cause: the diff adds no retry, no longer timeout and no skip, and playwright.config.ts is unchanged
- [ ] If the cause is the panel's stale initial value, ModerateDialog's slow-mode choice follows slow_mode_seconds until the moderator chooses, with Vitest cases that fail against main before the change
- [ ] e2e/thread-moderation.spec.ts passes in 20 consecutive phone-project runs under the load recipe, and the full suite passes in both projects with its axe scans
- [ ] If client code changed, the dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **The method of #250 and #256.** Reproduce under load and name the cause before any fix. No retry, no longer timeout and no skip (TKT-01M3SZ3GCE, TKT-01M3S8Y2NA). A cause that is not reproduced is recorded, and the ticket is blocked until the failure recurs, as TKT-01M3SZ3GCE was.
- **Evidence in CI.** Since #261 (TKT-01M3TNBKNQ), a failing test prints its `error-context.md` and the `serve.log` lines it caused into the step log (`scripts/evidence-reporter.mjs`). Read that block first if the failure recurs on any run.
- **The suite runs serially** (`playwright.config.ts`: `workers: 1`, `fullyParallel: false`, `retries: 0`). The desktop failure in #1686, the panel opening on 10 seconds, follows from the phone test never reaching its "Off" step, as the description says. It is not a second cause.
- **Where the stale value would come from** (`screens/ThreadScreen.tsx`, `components/thread/ModerateDialog.tsx`):
  - `ModerateDialog` is keyed `${thread} ${kind}`, so each opening mounts it fresh.
  - It takes `slowSeconds` from the page's `viewer.slow_mode_seconds` once, in `useState` initialisers (`choice`, `custom`).
  - After a save, `onChanged` is `res.retry`, which refetches the page, and the old data stays on screen until the answer lands.
  - The desktop step waits for the composer's `[data-slow-mode]` line, which appears only after the refetch. The phone step has no such wait, since its composer is the sheet.

### Approach

1. **Name the cause.** Use two independent routes.
   - **Under load.** Use TKT-01M3SZ3GCE's recipe: the CI image, three containers at 1 CPU each, 48 busy loops and the fsync loop. Run the full suite, or `e2e/thread-moderation.spec.ts` alone in the phone project, until the test fails or 20 runs pass. Keep the trace (`trace: 'retain-on-failure'`) and the evidence block. From the trace's network panel, record the order of the slow-mode `PUT`, the thread `GET` that follows it, and the second panel's mount.
   - **A probe**, not committed. With `page.route`, hold the thread page's `GET` after the `PUT` for 2 s, and reopen the panel. If the radio stays on Off for the whole 5 s, the stale-initialiser cause is shown deterministically. Then release the `GET` and check whether the radio follows.
   - Also rule out a stale answer: the `GET` after the `PUT` answered 200, not 304, with `slow_mode_seconds: 10`. The ETag hashes the viewer block (`threads.View`), so it should have.

   Write the timings and the verdict as a ticket note before changing code.
2. **Fix, if the cause is the stale initialiser.** The panel opened on page data from before the save. Fix it in the component, because a moderator on a slow phone sees the same stale panel.
   - `ModerateDialog`'s slow-mode `choice` and `custom` follow `slowSeconds` until the moderator touches a radio or the field. Keep a `touched` ref, and add a layout effect on `[slowSeconds]` that resets both while it is untouched.
   - The spec then needs no new wait. Its existing `toBeChecked()` retries within its 5 s and passes once the refetch lands.
   - Rejected: waiting for the refetch only in the phone spec. It hides a real stale-panel bug from the one user who meets it.
   - Rejected: keying the dialog by `slowSeconds`, which would discard a moderator's choice mid-edit when a refetch lands.
3. **If the evidence names another cause**, re-plan from that evidence with a note, under the same rules.
4. **If nothing reproduces it in 20 loaded runs and the probe**, record that and block the ticket on its recurrence, as TKT-01M3SZ3GCE is blocked.
5. **Build**, if client code changed. Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** `ModerateDialog` is in the reader. A fix costs about +40 B gzip.

### Tests

- `ThreadScreen.test.tsx`, or a new `ModerateDialog.test.tsx`:
  - "the slow-mode panel opened before the page's refetch takes the thread's value when it lands".
  - "a moderator's choice is kept when the refetch lands".

  Both must fail against main before the fix.
- `e2e/thread-moderation.spec.ts` "a moderator sets slow mode and turns it off": unchanged in what it asserts. It passes in 20 consecutive phone-project runs under the load recipe, and the full suite passes in both projects with its axe tests. Record the run count and the host in a note.
- No change to `playwright.config.ts`, and no `test.slow`, `retries` or longer `expect` timeout in the diff.

### Out of scope

- TKT-01M3SZ3GCE (admin-applications timing out). It is blocked on its own recurrence.
- The More menu's listener race on a phone: TKT-01M3TV4HBF. If the trace shows the panel opened through that race, say so in the note and let that ticket's fix land first.

## Notes

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:17:38Z

Milestone cleared, 2026-10-01: the orchestrator's scope note says drafts a lane files go in with no milestone, for the maintainer to place, so that the run does not grow its own scope. It was filed as phase-2 before that note.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T09:04:45Z

More evidence from load, 2026-10-01, from grooming PR #269. CI run #1779 on head 0a1c925 changed only .tickets/ and docs/06, and it failed the Playwright step: 13.3 min, 5 failed, 11 skipped, 188 passed. At least five grooming lanes' CI runs were active in the same window (runs #1785 to #1794). The phone failures were: a11y.spec.ts:82 ('#post-2 details.quote .quote-source' not found within 5 s); sign-out.spec.ts:36 (timed out at 48.5 s); thread-moderation.spec.ts:52, 'a moderator locks and unlocks a thread' (the Lock button resolved, but its click did not complete within the 30 s test budget, and waitForRequest timed out); and thread-rename-move.spec.ts:91 (axe's newPage hit 'Failed to find browser context', which means the browser itself went away). The desktop failure was a11y.spec.ts:82 with replies or the sheet open (the 30 s budget). The slow-mode test passed in this run. The same spec's lock test failing on a phone under load fits this ticket's load recipe, and it is the first failure in that spec not caused by slow mode. Read it with the evidence block when this ticket is worked.

**agent:claude-code/phase3-tszhsw** at 2026-10-01T17:09:08Z

Cause, 2026-10-01, named before any code change, on main at e855e1a. The panel takes its slow-mode choice once, when it mounts. If the panel reopens before the page's refetch lands, it opens on the old value and never follows the refetch. CI load only decides which of the two lands first. This is not the runner freeze (TKT-01M3VDYENN), as the CI evidence below shows.

### The probe (deterministic)

A probe spec, not committed, ran on port 18993 at both widths. It saved 10 seconds and held the thread page's `GET /api/v1/threads/{id}?page=1` that follows the PUT for 2 s with `page.route`. During the hold it reopened the panel, then released the GET. Phone timings, from the Save click:

- 344 ms: `PUT …/slow-mode` answered 200 `{"seconds":10}`.
- 362 ms: the thread GET was sent and held (`If-None-Match` carried the old ETag).
- 363 ms: the panel closed. At 435 ms the panel reopened from the more menu.
- 435 to 1986 ms, while the GET was held: Off was checked and 10 seconds was not.
- 2514 ms: the GET answered **200, not 304, with `viewer.slow_mode_seconds: 10`**. The ETag covers the viewer block, so the answer was not stale.
- 3518 to 7579 ms, five checks after the answer: the panel was still on Off.

The desktop project behaved the same way. The page's `[data-slow-mode]` line showed 10 s, but the open panel still said Off. So the refetch answer is correct, and the panel ignores it. `ModerateDialog` reads `slowSeconds` only in the `useState` initialisers of `choice` and `custom`. It is keyed `${thread} ${kind}`, so a refetch re-renders it without remounting it.

The two Vitest cases that the plan names reproduce this without a browser: "the slow-mode panel opened before the page refetch takes the thread value when it lands", and "a moderator's slow-mode choice is kept when the page refetch lands". Both fail against main. Each expects "10" after the held page lands and gets "0".

### CI evidence

- **Run 1686** (the original failure) ran on runner b3cc8959. The phone test took 5.9 s in all, and 5 s of that was the `toBeChecked` wait at line 106. The steps before the reopen therefore took under 0.9 s. The call log shows 14 polls in the 5 s, so the browser was not frozen during the wait. The tests on either side took 2.2 s and 1.3 s. A ten-second runner freeze cannot fit in this window. The run predates #261, so no serve.log survives to show when the GET landed. On main it makes no difference: once the panel mounts before the refetch, it never shows 10.
- **Run 1779** (note 2) ran on the same runner. The slow-mode test passed there at both widths (2.0 s and 1.5 s). The phone lock test at line 52 failed for an unrelated reason: its 30 s test budget ran out. Its evidence block shows the server under heavy load. `GET /f/garden` took 3888 ms, the forum's thread list 2254 ms and the thread GET 4311 ms, while the assets took about 100 ms. 13 s passed between the list's answer (08:55:30.4) and the thread GET's start (about 08:55:43.4). The Lock click resolved its button but never acted, and no `POST /lock` reached the server. The next phone test then lost its browser ("Failed to find browser context"). That is the overload of five concurrent grooming runs, not the stale panel. Its fix would be a longer budget, which this ticket forbids. It is not filed again here: it is the same runner pressure as TKT-01M3VDYENN, and its evidence is now in this note.
- **The race margin.** Locally, the refetch lands about 20 ms after the PUT, and the phone's two clicks to reopen the panel take about 370 ms (serve.log, PUT to PUT). The GET therefore has to be about 0.35 s late for the panel to open first. Run 1779's server took 2 to 4 s on single GETs on the same runner, which is far more than that.

### Under load, locally

The full `e2e/thread-moderation.spec.ts` ran in the phone project with `--repeat-each=20 -x`, on the host (12 CPUs) at e855e1a:

- 48 unpinned busy loops, Playwright and the server pinned to CPUs 0-1, load average up to 62: 144 of 144 passed, 20 of 20 for the slow-mode test.
- 48 busy loops plus a loop writing 256 MiB with fsync, Playwright and the server pinned to CPU 0, load average up to 82: 144 of 144 passed, 20 of 20 for the slow-mode test.

Uniform CPU or fsync load slows the clicks as much as the GET, so it does not reproduce the ordering. This matches TKT-01M3SZ3GCE, which could not reproduce CI's per-request stalls either. The probe stands in for that stall.

### Verdict and fix

The cause is the panel's stale initial value (Approach step 2): a correct 200 refetch that the open panel never reads. A moderator on a slow phone sees the same thing. The fix is in `ModerateDialog`, as the plan says. `choice` and `custom` follow `slowSeconds` until the moderator touches a radio or the field. The spec keeps its assertions and gains no wait.
