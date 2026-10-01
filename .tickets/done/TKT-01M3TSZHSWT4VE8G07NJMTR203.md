---
schema: 3
id: TKT-01M3TSZHSWT4VE8G07NJMTR203
title: Stop the slow-mode e2e test failing on a phone under CI load
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
milestone: phase-3
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:333
    path: null
claim: null
archive: null
created_at: 2026-10-01T04:01:24Z
updated_at: 2026-10-01T18:24:30Z
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
- [x] The fix is at the named cause: the diff adds no retry, no longer timeout and no skip, and playwright.config.ts is unchanged
- [x] If the cause is the panel's stale initial value, ModerateDialog's slow-mode choice follows slow_mode_seconds until the moderator chooses, with Vitest cases that fail against main before the change
- [x] e2e/thread-moderation.spec.ts passes in 20 consecutive phone-project runs under the load recipe, and the full suite passes in both projects with its axe scans
- [x] If client code changed, the dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth

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

**agent:claude-code/phase3-tszhsw** at 2026-10-01T17:25:52Z

Proof, 2026-10-01, on the fix (commit 22447505, branch run/tszhsw-slow-mode-phone on main at e855e1a). The host has 12 CPUs, and Playwright ran on port 18994. The dist was regenerated locally for these runs and not committed, because another lane holds it.

- **The probe on the fix.** The same probe held the refetch for 2 s at both widths. The panel stayed on Off while the GET was held, which is the honest state. Within 1 s of the GET's 200 it showed 10 seconds, and it kept showing 10 seconds for all five checks. On main it never showed 10 seconds (the cause note).
- **20 consecutive phone-project runs under load.** The command was `npx playwright test e2e/thread-moderation.spec.ts --project=phone --repeat-each=20 -x`. The load was 48 unpinned busy loops and a loop writing 256 MiB with fsync, with Playwright, Chromium and the server pinned to CPU 0. The load average peaked at 67.9. All 144 tests passed, and the slow-mode test passed 20 of 20 times, in 4.9 min.
- **The full suite in both projects.** An unfiltered `npx playwright test`, with no load, passed 206 tests and skipped 12. The skips are width-specific. Every axe scan passed.
- **Vitest.** All 2170 tests passed. The two new cases fail against main: each expects "10" after the held page lands and gets "0".
- `npx tsc --noEmit -p .`, `gofmt`, `go build ./...`, `go vet ./...` and `go test -race ./...` were all clean.
- **Reader bundle.** It grew from 63878 B to 63916 B gzip, which is +38 B against the 70000 B budget.

### What the diff does not change

`playwright.config.ts` and `e2e/thread-moderation.spec.ts` are unchanged. The diff adds no retry, no timeout and no skip. `ThreadScreen.test.tsx` gains the two cases and changes one helper, `checked`, which reads the chosen radio. It now reads each radio's `checked` property instead of the `:checked` selector. happy-dom's selector answered with the radio from before a click when no render followed the click, which would have hidden the second case's choice.

**agent:claude-code/phase3-tszhsw** at 2026-10-01T17:35:04Z

Proof under TKT-01M3SZ3GCE's exact recipe, 2026-10-01, on the fix at c7b16e85. The image was golang:1.27-alpine with Alpine's Chromium 152.0.7977.82, installed as ci.yml installs them. Three podman containers ran at once, each with --cpus=1. The host carried 48 busy loops and a loop writing 256 MiB with fsync, and its load average peaked at 74.4. Each container ran `npx playwright test e2e/thread-moderation.spec.ts --project=phone --repeat-each=20 -x` and passed 144 of 144 in 7.3 to 7.4 min. That is 60 consecutive phone runs of the slow-mode test across the three, with no failure. This adds to the host proof in the note before.

**agent:claude-code/phase3-tszhsw** at 2026-10-01T18:24:30Z

Terva review 1795 (run a622b0bf), head 01666fd, base 86752c4, found one medium issue: 'Mark an already-selected radio as touched when it is clicked'. It is fixed in 8eab293. The radios now mark the choice on click, not on change, because a click on the radio already chosen fires no change event. A new Vitest case, 'a click on the slow-mode radio already chosen is kept when the page refetch lands', clicks Off while the refetch is held, lets 10 land, and checks that Off stays chosen and that Save sends 0. The case fails with onChange ('10' where '0' was expected) and passes with onClick. The reader bundle is 66220 B gzip, 31 B over main at dfd23a6. The moderation spec passed 46 of 46 in both projects with --repeat-each=3. All 2245 Vitest tests pass. While running go test -race, I found TestServeGeneratesNotifications failing intermittently on main as well (1 in 45 runs at 86752c4). It is filed as the draft TKT-01M3WBBF4H (Stop TestServeGeneratesNotifications racing the restore's notification) and is unrelated to this change, which touches no Go code.

## Summary

Fixed in the client, at the cause the probe named. `ModerateDialog` took its slow-mode choice and Custom's seconds once, in `useState` initialisers. When a moderator saved and reopened the panel before the page's refetch landed, the panel opened on the old value and never read the refetch. The refetch answered 200 with the new `slow_mode_seconds`. On a loaded CI runner the refetch can land after the phone's two clicks that reopen the panel (run 1686). That ordering was the whole flake. The runner freeze (TKT-01M3VDYENN) was not involved.

The choice and the seconds now follow `slow_mode_seconds` through a layout effect until the moderator touches a radio or the field. After that, a later refetch leaves the moderator's choice alone. Two Vitest cases in `ThreadScreen.test.tsx` fail against main and pass on the fix. The test file's `checked` helper now reads each radio's `checked` property, because happy-dom's `:checked` selector can answer from before a click that no render followed. `e2e/thread-moderation.spec.ts` and `playwright.config.ts` are unchanged.

Proof is in the notes. With the refetch held 2 s, the panel shows 10 seconds within 1 s of the answer at both widths, where main never does. Under 48 busy loops and an fsync loop on the host, 20 of 20 phone runs passed. In the CI image, three 1-CPU containers at once ran 60 phone runs, all green. After the rebase onto dfd23a6, the full suite passed in both projects with its axe scans: 214 passed, 12 skipped by width.

The reader bundle grew by 32 B on main at dfd23a6, from 66189 B to 66221 B gzip of 70000 B.

Run 1779's phone lock-test failure (note 2) is a separate case. Its 30 s budget ran out while the server answered single GETs in 2 to 4 s. That is runner overload, recorded in the cause note and not fixed here.
