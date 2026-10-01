---
schema: 3
id: TKT-01M3SZ3GCESETRH2R5MY9N01KN
title: Stop admin-applications.spec.ts timing out under CI load
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
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:255
    path: null
claim:
  actor: agent:claude-code/phase2-run-sz3gce
  branch: run/sz3gce-applications-flake
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-af3af22297b167b1c
  commit: 8a849ca5d354d83e801dc93eb07177a414939f69
  session: null
  claimed_at: 2026-10-01T01:58:31Z
  expires_at: null
archive: null
created_at: 2026-09-30T20:11:42Z
updated_at: 2026-10-01T01:58:32Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
extensions: {}
---

## Description

Found by the TKT-01M3QS0TMV lane (PR #245) on 2026-09-30: main's push run #1592 on 46c1081 failed in the Playwright step on the desktop project. `web/client/e2e/admin-applications.spec.ts:50` timed out after 5 s waiting for `[data-application-link="2"]` to appear, and the Playwright step took 9.4 min, against its usual length.

46c1081 changed only docs and the ticket store (PR #242), PR run #1595 passed on the next head, and the test passed in the lane's two local full runs, so this looks like a flake under runner load. It is the only failure seen: the other recent push runs on main were cancelled by later pushes rather than finishing.

### What to do
Reproduce under load (for example, the full suite with more workers than the runner has, or with CPU throttled), then name what the test waits on. The likely candidates are a 5 s default expect timeout against an admin list that first waits on the lazy admin chunk, or a row that appears only after a refetch. Fix the cause. No retries, no longer timeouts, no skips, the same rule as TKT-01M3S8Y2NA.

## Acceptance criteria

- [ ] Before any code change, a note names the failing step and assertion as reproduced locally, what the page showed when the 5 s ran out (from the trace or error-context.md), when GET /api/v1/admin/applications?status=pending was sent and what it answered, and the cause, with the runs that separate it from the other candidates in the plan
- [ ] A note records that the proof command, under the load named in it, failed at least once on main before the fix, with the command, the load, the load average and the commit
- [ ] The diff fixes the named cause and adds no retry (retries, --retries, toPass, or a poll or reload loop around an assertion), no larger timeout (a timeout on an expect, a timeout change in playwright.config.ts, test.slow or test.setTimeout), and no skip, fixme, fail or only; playwright.config.ts keeps workers: 1 and retries: 0
- [ ] admin-applications.spec.ts keeps every assertion it has on main, including the three scans, at both widths
- [ ] A note records 20 consecutive green runs of the proof command on the fix under the same load, with admin-applications.spec.ts passing at both widths in each, plus 3 green unfiltered npx playwright test runs, with the command, the load, the load average and the commit
- [ ] A note records a mutation that puts the cause back and fails at least once under the same load
- [ ] go generate ./internal/web/spa leaves internal/web/spa/dist unchanged; or, if the fix changes web/client/src outside tests, the dist is regenerated and a note records that no open PR changed internal/web/spa/dist when this one was pushed

## Implementation plan

Groomed 2026-10-01 against main at f3ba70d, after reading run #1592's Playwright step log (see the grooming note). The cause is not assumed. TKT-01M3S8Y2NA (PR #250) found that a vitest file's first import of the lazy admin chunk outran `settle`'s bound, and fixed it in `renderAt` in `web/client/src/testing.tsx`. That fix does not reach Playwright, and #1592's log already rules the chunk out as the thing the 5 s ran out on (candidate 5 below).

### Settled in grooming

- **The fix addresses the cause.** No retries (`retries` in playwright.config.ts, `--retries`, `toPass`, an `expect.poll` or a reload loop around the assertion), no larger timeout (a `timeout:` on an expect, `expect.timeout` or `timeout` in playwright.config.ts, `test.slow()` or `test.setTimeout`), and no `skip`, `fixme`, `fail` or `.only`. playwright.config.ts keeps `workers: 1` and `retries: 0`. This comes from the ticket's description, which is the same rule as TKT-01M3S8Y2NA. A wait on the condition the assertion reads is not a retry, if it is bounded by the default 5 s expect timeout that the test has today.
- **The investigation comes first and is recorded.** Before any code change, a note names what the page showed when the 5 s ran out, what the list request answered and when, and the cause, with the runs that separate it from the other candidates below. This is the rule TKT-01M3S8Y2NA followed, applied here by the orchestrator's instructions for this ticket (2026-10-01).
- **The proof holds over repeated runs under load.** 20 consecutive runs of the proof command (Approach step 3), under the load that reproduced the failure on main, with admin-applications.spec.ts passing at both widths in every run. Also 3 unfiltered full-suite runs under the same load.
- **Playwright runs on port 18991** (`KETJU_E2E_PORT=18991`), so this lane does not collide with another lane's Playwright run on the same host.
- **The client dist.** The fix is expected to change only e2e or test code, and `go generate ./internal/web/spa` must then leave `internal/web/spa/dist` unchanged. If the cause needs a change under `web/client/src` outside tests, regenerating the dist is allowed. Before pushing it, check that no open PR changes `internal/web/spa/dist`, and record that check in a note (docs/13 "Running two lanes, changed").

### Approach

1. **Reproduce on main, before any code change.** Work in `web/client` with `KETJU_E2E_PORT=18991`. The load is 48 unpinned busy `yes` loops on the 12-CPU host, as TKT-01M3S8Y2NA used, started before each batch and killed by a trap after it. Record the load average of each batch. Run:
   - (a) the full suite, `npx playwright test`, which is what failed in #1592: the desktop test ran after every phone spec, so the state those specs leave is part of the repro;
   - (b) the admin specs alone, `npx playwright test e2e/admin-`, at both widths with the setup project;
   - (c) (b) together with the specs that write applications, `e2e/apply.spec.ts` and `e2e/phase2-exit.spec.ts`.
   Each failing run keeps its trace (`trace: 'retain-on-failure'` is already set) and `error-context.md` under the run's `results/` directory. If 48 loops do not reproduce the failure in 10 runs of (a), raise the load, for example to 96 loops or by pinning the server and the browser to 2 CPUs with `taskset`, and record each attempt. If no load reproduces it, stop and report with the ticket still `in-progress`, rather than fixing a cause that was not seen.
2. **Name the cause.** Read each failing trace. The trace's network panel gives `GET /api/v1/admin/applications?status=pending`: when it was sent, its status, and when it answered. The DOM snapshot at the timeout gives what `ApplicationList` (web/client/src/screens/admin/applications/ApplicationList.tsx) was showing. Separate these candidates:
   1. **The list request is slow.** The page still shows "Loading…" at the timeout, and the request had not answered, or answered late. If so, find where the time goes: the handler `listApplications` in internal/web/api/applications.go, then `applications.List` and the `ListApplications` query in internal/store/db/queries/applications.sql; a write lock or SQLite busy wait held by something else in the server; or the browser queueing the request behind other connections.
   2. **The list request fails.** An `ErrorPanel` for "The applications" is showing. Its status names the cause: 401 or 403 means mona's session from `stateFile('mona')`, 429 means a limiter in internal/web/ratelimit.go, and 5xx means the server.
   3. **The list answers without #2.** The table rendered, but not the row. Either another spec decided application 2 (the desktop project's seeded application, `seedPending` in cmd/ketju/e2e.go), or more than 25 newer pending applications pushed it off the first page.
   4. **Something in the spec.** For example, the navigation raced a client-side redirect, or an earlier step's state leaked into this one.
   5. **The lazy admin chunk (ruled out by #1592's log unless the trace says otherwise).** `h1` "Applications" and `[data-no-email]` both passed before line 50, and both render inside `AdminRoot` after the chunk has loaded and the viewer has resolved.
   Write a note with the failing step and assertion, what the page showed, the list request's timing and status, the cause, and the runs from step 1 that separate it from the other candidates.
3. **Pick the proof command.** It is the smallest of (a), (b) and (c) that reproduced the failure on main in step 1. It must include `e2e/admin-applications.spec.ts` at both widths.
4. **Fix the cause** where the cause is:
   - in the spec (e2e/admin-applications.spec.ts, or a helper in e2e/), by waiting on the condition the page actually needs, inside the existing 5 s;
   - in the e2e seed (cmd/ketju/e2e.go), if another spec's writes reach application 2 or the list;
   - in the client (`ApplicationList`, `useResource`, `apiGet`), or in the server handler or query, if the list is slow or wrong for a real user too. A server or client fix gets a unit test that fails without it.
   None of these may use a retry, a longer timeout or a skip (Settled in grooming). Remove every temporary instrumentation before the fix commit.
5. **Prove it.** On the fix, under the same load: 20 consecutive runs of the proof command, all green, then 3 unfiltered `npx playwright test` runs, all green. Record in a note the command, the load, the load average, the commit and each run's result.
6. **Check what CI runs.** Run `test -z "$(gofmt -l .)"`, `go build ./... && go vet ./...` and `go test -race ./...`. In `web/client`, run `npx tsc --noEmit -p .` and `npm test`. Run `go generate ./internal/web/spa` and confirm with `git status` that `internal/web/spa/dist` is unchanged, or follow the dist rule in Settled in grooming.

### Tests

- 20 consecutive green runs of the proof command on the fix under the load that reproduced the failure on main, with admin-applications.spec.ts passing at both widths in each. At least one failure of the same command on main under that load, from step 1.
- 3 unfiltered `npx playwright test` runs on the fix under the same load, all green.
- A mutation that puts back the cause, for example the old wait or the old seed, is run under the same load and fails at least once. This shows the proof can catch the regression.
- If the fix is in the server or the client source, a unit test that fails without it: a Go test in the handler's or service's package, or a vitest test beside the screen.
- `npx tsc --noEmit -p .`, `npm test` and the Go checks in Approach step 6 pass.

### Out of scope

- Other specs that are slow under load but have not failed. If step 1 shows another spec failing under the same load, file a draft for it with its failing assertion, and do not fix it here.
- CI configuration: the container, its CPU count, the job's step, and the Playwright workers or retries. docs/12 "Changing CI" limits what a run PR may change, and this ticket needs none of it.
- The redundant `beforeAll` chunk preloads in AgentControls.test.tsx and QueueScreen.test.tsx, which TKT-01M3S8Y2NA left in place.

## Notes

**agent:claude-code/phase2-run-sz3gce** at 2026-09-30T23:54:35Z

Grooming refresh, 2026-10-01, against main at f3ba70d. This is a reading of run #1592's logs, not the investigation. The plan's first step still reproduces the failure and names the cause before any code change.

### What run #1592's Playwright step log says

- The step failed after 9m36s, and the reporter's total was 9.4 min, with 1 failed, 11 skipped and 174 passed. PR run #1595 on the next head passed the same step in 10m12s, with a reporter total of 9.9 min. So #1592 was not slower than usual overall. The description's "against its usual length" does not hold for the run as a whole.
- The failing test is the desktop project's `a moderator approves a pending application`, in the step `the pending list`. It took 9.0 s, 5 s of which was the wait at line 50. The same test took 9.5 s on desktop and 9.8 s on phone in #1595, and 10.8 s on phone in #1592.
- The two assertions before line 50 passed: the `h1` "Applications" and `[data-no-email]`. Both render inside `AdminRoot` (web/client/src/screens/admin/AdminRoot.tsx), and only after the lazy admin chunk has loaded and `section.may(me)` has passed. So the 5 s ran out after the chunk had loaded and the viewer had resolved. It ran out on `ApplicationList`: its request `GET /api/v1/admin/applications?status=pending`, the render of its answer, or a list without application 2. This separates the ticket from TKT-01M3S8Y2NA, whose cause was the chunk's first import.
- The desktop test ran 119th of 186, after every phone spec, including `apply.spec.ts` and `phase2-exit.spec.ts`, which write applications, and `admin-forms.spec.ts`, which saves a new version of the registration form. A repro therefore starts from the full suite.
- The trace and `error-context.md` were written under the CI container's /tmp and are gone. The first local failure's trace is the first evidence of what the page showed.
