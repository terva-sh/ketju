---
schema: 3
id: TKT-01M3RZXGBAN5GRYMZ7HA3A7VQG
title: Bound the axe scans in the admin forms and agents Playwright tests
type: chore
status: draft
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
  - ref: ticket:TKT-01M3MTQG5ATJPTEPZHNBYHAHBF
    path: null
  - ref: pr:223
    path: null
claim: null
archive: null
created_at: 2026-09-30T11:06:39Z
updated_at: 2026-09-30T12:36:01Z
created_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The a11y timeouts of runs 728, 751 and 950 came from four axe scans sharing one 30 s test budget while another CI run loaded the runner (TKT-01M3MTQG5A, Stop Playwright checks timing out under CI runner contention). That ticket split `a11y.spec.ts` into one scan per test and left the other specs out of scope, because none of them has timed out.

Two admin specs carry more scans in one test than a11y.spec.ts ever did:
- `admin-forms.spec.ts:35`, "an admin edits the registration form and saves a new version": eight axe scans (four states, two themes). On green CI runs 1396, 1403 and 1407 it took 9.5-14.7s.
- `admin-agents.spec.ts:45`, "an admin provisions an agent and manages its token and limits": ten axe scans (five states, two themes). It took 7.1-12.1s on the same runs.

The a11y thread page took 5-9s on green runs and still reached 30 s under load. These two tests are already slower than that.

### To do
- Watch for a timeout in either test. If one comes, or before one does, bound the scans the same way TKT-01M3MTQG5A did. The flow is stateful, so the split cannot be one test per scan. Options are `test.step` with a per-scan budget, or a serial `describe` whose tests share one page.
- Keep the rule tags, the zero-violation assertions and every scanned state.

## Acceptance criteria

- [ ] web/client/e2e/admin-forms.spec.ts runs its eight axe scans (the editor, the refused draft, the saved form and version 1 read-only, each in light and dark) one per test, and no test in the file runs more than one
- [ ] web/client/e2e/admin-agents.spec.ts runs its ten axe scans (the new agent form, the token panel, the agent detail, the issue confirmation and the agent without a token, each in light and dark) one per test, and no test in the file runs more than one
- [ ] Both files keep the rule tags wcag2a, wcag2aa, wcag21a and wcag21aa, the zero-violation assertion with its "${what}, ${colorScheme}" message, the horizontal-overflow check for each state, and every other assertion of the test they replace
- [ ] Each file runs under test.describe.configure({ mode: 'serial' }) with one context made in beforeAll from stateFile('ada') and info.project.use, and closed in afterAll
- [ ] playwright.config.ts is unchanged, and neither file adds test.setTimeout, test.slow or a test.step timeout
- [ ] A note records the mutation check: a dark-only image-alt violation fails only that state's dark test in each file, and the note says whether the failing test kept a trace
- [ ] CI's Playwright step passes on the PR, and a note records both files' per-test durations from that run
- [ ] A note records the contention run (14 busy loops pinned with the run to two cores, --repeat-each=5) on main and on the branch, with the command and each test's min, median and max duration per project. The loops are killed by the subshell's EXIT trap after each measurement, and pgrep -c -x yes shows 0 before the next one. Every test passes on the branch

## Implementation plan

Groomed 2026-09-30 against main at 4918f90 (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- **Follow PR #223 (TKT-01M3MTQG5A, Stop Playwright checks timing out under CI runner contention).** Each test runs one axe scan. The rule tags (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`), the zero-violation assertion and its `${what}, ${colorScheme}` message stay as they are. No timeout is raised: no `test.setTimeout`, no `test.slow` and no step timeout. `playwright.config.ts` is untouched. That ticket found that the 30 s timeouts of runs 728, 751 and 950 came from several scans sharing one test's budget while another CI run loaded the runner. Splitting brought its slowest test under 14 busy loops to 9.3 s (its notes of 2026-09-30).
- **Keep every scanned state and every assertion** (this ticket's To do). admin-forms scans four states in two themes, and admin-agents scans five states in two themes. The horizontal-overflow check runs once per state, and every non-axe assertion stays.
- **The numbers** (TKT-01M3MTQG5A's evidence note, green CI runs 1396, 1403 and 1407):
  - `admin-forms.spec.ts:35` runs eight scans in one test and took 9.5-14.7 s.
  - `admin-agents.spec.ts:45` runs ten scans in one test and took 7.1-12.1 s.
  - The a11y thread page, which timed out in run 950, took 5-9 s on those runs.
  - Neither admin test has timed out yet. This ticket bounds them before one does.
- **Engineering choice: a serial file whose tests share one page.** Each file becomes a sequence of tests in `test.describe.configure({ mode: 'serial' })`. The tests share one browser context and page, which `beforeAll` makes and `afterAll` closes. Each scan is its own test, so it gets its own 30 s. When a test fails, Playwright skips the rest of the file for that project. That matches today, where a failure ends the one test. Two alternatives lost:
  - `test.step(..., { timeout })`. A step timeout bounds the step, but the whole test still has one 30 s budget, so eight or ten scans still share it. Such a step can fail sooner, but the test gains no time.
  - One test per scan that replays the flow up to its state, as a11y.spec.ts does by loading a page. These flows change the server:
    - admin-forms saves two form versions per replay. A replay that failed midway would leave a changed form for `apply.spec.ts`, which fills that form in later.
    - admin-agents would create an agent per test, ten per project per run. It would also repeat the token issue and revoke, so the work would grow with the square of the states.
- **The shared context follows the pattern the other specs use:** `browser.newContext({ ...info.project.use, storageState: stateFile('ada') })`, as in `read-tracking.spec.ts:16` and `forum-index.spec.ts:29`. It carries each project's viewport, `isMobile` and `hasTouch`. The file-level `test.use({ storageState: stateFile('ada') })` goes, because no test uses the `page` fixture any more.
- **Where the theme is set.** The theme is switched with `page.emulateMedia({ colorScheme })` on the live page, as today's `scan()` does, because the state cannot be loaded again. PR #223 set it before `goto`, which cannot work here. The mutation check below proves that the theme is in effect for each dark scan.

### Approach
1. **Helpers**, the same in both files. Replace today's `scan(page, what)` with two functions:
   - `overflow(page, what)`: today's `scrollWidth - clientWidth <= 0` check with its message.
   - `scan(page, what, colorScheme)`: calls `emulateMedia({ colorScheme })`, runs one `AxeBuilder` with the same tags, and makes the same `expect(found, \`${what}, ${colorScheme}\`).toEqual([])`.

   The light test of each state runs `overflow` and then `scan(..., 'light')`. The dark test runs `scan(..., 'dark')` and then sets `light` back, as today's `scan` does at its end. The next state's actions then run in the light theme, as today. Each file keeps its own copy of the helpers. Sharing them with `apply.spec.ts` and `phase2-exit.spec.ts` would touch specs outside this ticket.
2. **`web/client/e2e/admin-forms.spec.ts`**:
   - Add `test.describe.configure({ mode: 'serial' })` at the top.
   - Add module-level `let context`, `page`, `start`, `n` and `save`, plus the `added` locator.
   - `beforeAll({ browser }, info)` does three things:
     - It keeps the two config guards from `:42-43` (`info.config.workers` is 1 and `fullyParallel` is false). They now fail the file before anything is saved.
     - It makes the context and page.
     - It reads `start` from `GET /api/v1/admin/forms/registration` (`formUrl`).
   - `afterAll` closes the context.
   - The tests, in order:
     1. `the editor passes the scan in the light theme`: the `goto('/admin/forms')` and the assertions from `:51-56`, then `overflow` and the light scan.
     2. `the editor passes the scan in the dark theme`.
     3. `the editor's phone frame is 390 px across`: `:61-66`. No scan.
     4. `a key the server refuses is marked on its field, and passes the scan in the light theme`: `:71-81`, then `overflow` and the light scan.
     5. The refused draft, dark.
     6. `the corrected form saves as a new version, and passes the scan in the light theme`: `:86-97`, then `overflow` and the light scan.
     7. The saved form, dark.
     8. `version 1 opens read-only, and passes the scan in the light theme`: `:102-107`, then `overflow` and the light scan.
     9. Version 1, dark.
     10. `the fields it started with are saved back`: closes version 1 (from `:109`), then `:113-120`. No scan.

   That is eight scans in eight tests. The header comment (`:5-17`) gains a paragraph saying why each scan is its own test, citing TKT-01M3MTQG5A.
3. **`web/client/e2e/admin-agents.spec.ts`**:
   - Add the same `configure`, and module-level `context`, `page`, `handle`, `detail`, `panel`, `tokenField`, `first` and `second`.
   - `beforeAll({ browser }, info)` makes the context and page, and sets `handle = \`bot-${info.project.name}-${Date.now().toString(36)}\``.
   - `afterAll` closes the context.
   - The tests, in order:
     1. `the new agent form passes the scan in the light theme`: `:53-56`, then `overflow` and the light scan.
     2. The new agent form, dark.
     3. `a new agent's token is shown once, and the token panel passes the scan in the light theme`: `:61-69`, then `overflow` and the light scan.
     4. The token panel, dark.
     5. `once Done is pressed, the token is nowhere on the page`: `:72-77`. No scan.
     6. `the write limits save, and the agent detail passes the scan in the light theme`: `:81-86`, then `overflow` and the light scan.
     7. The agent detail, dark.
     8. `the issue confirmation passes the scan in the light theme`: `:91-92`, then `overflow` and the light scan.
     9. The issue confirmation, dark.
     10. `a new token replaces the old one`: `:94-102`. No scan.
     11. `revoking stops the token, and the agent without a token passes the scan in the light theme`: `:105-108`, then `overflow` and the light scan.
     12. The agent without a token, dark.
     13. `closing the detail`: `:114-115`. No scan.

   That is ten scans in ten tests. The header comment (`:5-13`) gains the same paragraph.
4. **Not changed:** `playwright.config.ts`, `a11y.spec.ts`, `apply.spec.ts`, `admin-applications.spec.ts`, `admin-queue.spec.ts`, `phase2-exit.spec.ts`, and everything under `web/client/src`. The committed dist and the bundle budget are therefore untouched.

### Tests
- The split specs themselves, on both projects:
  - admin-forms goes from 1 test to 10, with 8 scans.
  - admin-agents goes from 1 test to 13, with 10 scans.
  - That is 46 tests across the two projects, where there were 4.
- **Contention run**, the same way TKT-01M3MTQG5A measured it (its note of 2026-09-30):
  ```
  (
    pids=''
    trap 'kill $pids 2>/dev/null; wait $pids 2>/dev/null' EXIT
    for i in $(seq 14); do taskset -c 0,1 yes >/dev/null & pids="$pids $!"; done
    KETJU_E2E_PORT=8843 taskset -c 0,1 npx playwright test e2e/admin-forms.spec.ts e2e/admin-agents.spec.ts --repeat-each=5
  )
  pgrep -c -x yes || echo 'load gone'
  ```
  The subshell records each load process's PID. Its `EXIT` trap kills them and waits for them when Playwright finishes, whether it passes, fails or is interrupted. Before the next measurement, `pgrep -c -x yes` must print 0 and exit 1, which the `|| echo` reports as `load gone`. If any `yes` is still running, stop it before measuring again, because leftover load would skew the next run's numbers. Run the measurement before the change (main) and after (the branch). Record the command, the load, and each test's min, median and max duration per project. admin-forms restores the form at the end of each repeat, and admin-agents names each agent for the moment it was created, so both can repeat.
- **Mutation check**, one state in each file:
  - Inject an `<img>` with no alt text that appears only while `prefers-color-scheme: dark` matches. Use the saved form in admin-forms and the agent detail in admin-agents.
  - Confirm that only that state's dark test fails, with `image-alt` under its message, and that the rest of that project's file is reported as skipped.
  - Record whether the failing test kept a trace under `trace: 'retain-on-failure'`, since the context now comes from `beforeAll`.
  - Revert the injection.
- CI's Playwright step passes on the PR. A note records the per-test durations of both files from that run.

### Out of scope
- The single-test scans in `apply.spec.ts`, `admin-applications.spec.ts`, `admin-queue.spec.ts` and `phase2-exit.spec.ts`. None has timed out, and each runs fewer scans. If one times out, the same split applies.
- Restoring the registration form when an admin-forms test fails partway. Today the restore is skipped on failure, and it still is. Moving it to `afterAll` would change what a failed run leaves, and no failure has needed it.
- Timeouts, the config and the CI image. They are held by the Settled items above and by TKT-01M3MTQG5A.
- The `/auth/*` budget. That is TKT-01M3S1TVPD (Stop the full Playwright run spending more /auth calls than it refills). These two specs start from `stateFile('ada')` and spend no `/auth/*` requests.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T12:36:01Z

Revised for Terva review 1582 on PR #225 (head f424f92, medium finding at line 137). The contention command started 14 background yes processes and never stopped them. It now runs inside a subshell that records their PIDs, and an EXIT trap kills and reaps them when Playwright finishes, whether it passes, fails or is interrupted. pgrep -c -x yes must show 0 before the next measurement. The contention criterion, which was number 6 and is now number 8, supersedes the old one and says the same.
