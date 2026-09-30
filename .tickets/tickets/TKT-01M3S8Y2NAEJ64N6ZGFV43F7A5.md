---
schema: 3
id: TKT-01M3S8Y2NAEJ64N6ZGFV43F7A5
title: Stop ApplicationDetail.test.tsx failing in full vitest runs
type: bug
status: ready
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
references: []
claim: null
archive: null
created_at: 2026-09-30T13:44:15Z
updated_at: 2026-09-30T19:35:26Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

`web/client/src/screens/admin/applications/ApplicationDetail.test.tsx` failed in 2 of 6 full vitest runs on the new-thread-screen lane (TKT-01M3NS16VH, local branch at 6b5dc9a, 2026-09-30). It passes when run alone, and that lane does not touch it. So some state leaks in from other test files, or the test depends on timing that a full parallel run disturbs.

### What to do

Reproduce the failure with the full suite (`npx vitest run`, repeated). Name the failing assertion and the cause, whether that is shared module state, a fake timer, an unawaited promise, or a fetch mock another file leaves behind. Fix the cause, not the symptom: no retries, no larger timeouts, no skipped test. Show the file passing 20 full runs in a row.

Reported by the Phase 2 run orchestrator on 2026-09-30.

## Acceptance criteria

- [ ] Before any code change, a note names the failing test, its failing assertion and message, whether Vitest reported an assertion failure or an unhandled error, and the cause, with the runs that separate it from the other candidates in the plan (file alone under load, full suite with one worker, resolved isolate setting)
- [ ] The diff fixes the named cause and adds no retry, no larger timeout or settle bound, and no skip, todo or only; the 200 x 5 ms bound in web/client/src/testing.tsx is unchanged
- [ ] The 16 existing tests in ApplicationDetail.test.tsx keep their assertions
- [ ] A note records 20 consecutive unfiltered npx vitest run passes in web/client with ApplicationDetail.test.tsx passing in each, with the command, the CPU load, and the commit
- [ ] A note records that the same runs under the same load reproduce the failure on main before the fix at least once

## Implementation plan

Groomed 2026-09-30 against main at 13709f0 (TKT-01M3M9RR26). The cause is not known. The first step is to find it, and the fix is planned against the candidates that reading the code turns up.

What exists at 13709f0:
- `web/client/src/screens/admin/applications/ApplicationDetail.test.tsx` has 16 tests in `describe('an application')`. Each renders the whole app with `renderAt('/admin/applications?id=N')` from `web/client/src/testing.tsx`, over `stubServer` routes. `beforeEach` sets `viewer.value = asModerator()`. `afterEach` runs `unmount()`, restores the viewer, and calls `vi.unstubAllGlobals()`. The file uses real timers.
- `settle(root)` in `testing.tsx` loops up to 200 × 5 ms until `root.querySelector('[data-screen]')` matches, then waits one more 5 ms timer. Its comment says the 1 s bound was raised because "the first import of the lazy admin chunk in a test worker transforms every admin screen" and took longer than 250 ms under a full parallel run (TKT-01M29CXK1X).
- `AdminRoot` (lazy in `routes.tsx`) renders `<main data-screen="admin-applications">` at once. `ApplicationDetail` renders "Loading…" until `useResource` (`api/resource.ts`) has an answer to `apiGet(appUrl(id))`. So `settle` can return while the detail is still "Loading…". After it returns, the test's first query (`.form-read`, `[data-application="42"]`, `[data-decided]`, `[data-blocked]`) depends on the stubbed fetch, `Response.json()` and the re-render all finishing inside one 5 ms timer. The local `click()` helper calls `settle` after `data-screen` is already present, so there it is a single 5 ms wait for the POST, any GET that follows it, and the render. 'says so when another denial set the block' waits for three requests that way.
- `vite.config.ts` sets `environment: 'happy-dom'` and `css: true` and sets neither `pool` nor `isolate`. With Vitest's defaults (`vitest` 5.0.1 in `package.json`), each test file gets its own module graph and its own happy-dom window. If that holds, no module state, fake timer or `vi.stubGlobal` from another file can reach this one.
- Twelve other files use `vi.useFakeTimers()`: `state/drafts.test.ts`, `state/newThread.test.ts`, `state/composer.test.ts`, `state/readTracker.test.ts`, `composer/Post.test.tsx`, `composer/Composer.test.tsx`, `composer/ComposerSheet.test.tsx`, `keyboard/shortcuts.test.tsx`, `screens/NewThreadScreen.test.tsx`, `screens/ThreadListScreen.test.tsx`, `screens/ThreadScreen.test.tsx` and `screens/admin/SuspensionControls.test.tsx`. Its neighbours `ApplicationsScreen.test.tsx` and `Decision.test.tsx` use real timers and the same `unstubAllGlobals` teardown.
- The failure was seen in 2 of 6 full runs on the TKT-01M3NS16VH lane (local branch at 6b5dc9a, 2026-09-30). The file passes alone, and that lane did not touch it. The failing assertion was not recorded.

### Settled in grooming

- **The fix addresses the cause.** No retries (`retry`, `--retry`, or a loop around an assertion), no larger timeout or longer `settle` bound, and no `skip`, `todo` or `.only`. This comes from the ticket's description, which the Phase 2 run orchestrator filed on 2026-09-30. A wait on a condition the assertion reads is not a retry, as long as its bound is no longer than the one `settle` has today (1 s).
- **The proof is 20 full runs in a row** with the file passing in every one. A full run is `npx vitest run` in `web/client`, with nothing filtered.
- **The investigation comes first and is recorded.** Before any code changes, a note names the failing test, the failing assertion with its message, and the cause, with the evidence that separates it from the other candidates below.

### Approach

1. **Reproduce.** In `web/client`, run the full suite repeatedly and keep every result, for example:
   ```
   for i in $(seq 30); do npx vitest run --reporter=json --outputFile="$SCRATCH/run-$i.json" >"$SCRATCH/run-$i.log" 2>&1; done
   ```
   `$SCRATCH` is a `mktemp -d` under `~/.cache/agent-scratch/ketju/`. For each failing run, record the test name, the assertion, and whether Vitest reported an assertion failure or an unhandled error attributed to the file. If 30 runs show nothing, add CPU load the way TKT-01M3MTQG5A and TKT-01M3RZXGBA did (`taskset -c 0,1` with busy `yes` loops, and a trap that kills them), and record that load in the note.
2. **Separate timing from leakage** with three runs of 20:
   - (a) The file alone under the same load: `npx vitest run src/screens/admin/applications/ApplicationDetail.test.tsx`. If it fails alone under load, the cause is timing inside the file, not state from another file.
   - (b) The full suite with one worker (`--maxWorkers=1`). If it passes serialized but fails in parallel, the cause is contention.
   - (c) The printed resolved config (`npx vitest --config vite.config.ts --printConfig`, or the Vitest 5 equivalent), to confirm `isolate: true` and the pool. Isolation off would put the cross-file candidates back in play.
3. **Name the cause** from these candidates, found by reading the file and its neighbours. Record in the note which one the evidence points to and which it rules out:
   1. **`settle` returns before the data.** `[data-screen]` appears with "Loading…", and the one 5 ms timer races the stubbed answer's `Response.json()` and the re-render. This is the most likely cause: it passes alone and fails only under a loaded parallel run, and every assertion after `renderAt` or `click` depends on it.
   2. **The lazy admin chunk takes more than 1 s** to import in a busy worker. Then `settle` runs out with no `[data-screen]` at all, and the first query finds nothing. This is the same class of failure TKT-01M29CXK1X met, when it raised the bound instead.
   3. **An unawaited promise from the previous test.** A request the unmounted app, or an aborted `Next pending` (`pending.current?.abort()`), starts after `vi.unstubAllGlobals()` would reach happy-dom's real `fetch` with a relative URL and reject. Vitest reports that as an unhandled error against the file.
   4. **Shared module state within the file.** `mounted` in `testing.tsx`, the `api/client.ts` cache (`resetApiCache`), `state/threads.ts`'s cache, the `viewer` signal, and `history` and `location`, which 'goes back to the filter it was opened from' changes with two `renderAt` calls in one test.
   5. **State from another file.** A fake timer left on, a `fetch` stub never removed, or module state such as `viewer`. This can happen only if step 2(c) shows isolation off.
4. **Fix the named cause at its source:**
   - If it is (1) or (2), make the wait follow the condition, not a clock. `renderAt` and the file's `click` wait until the screen has finished loading, within the existing 1 s bound. That means waiting for the element the test reads next, or for the detail's "Loading…" to be gone. The engineering choice between changing `settle` in `testing.tsx` for every screen and adding a condition helper used by this file follows from the evidence. If the early return is general, the fix goes in `testing.tsx` and every existing caller keeps passing. If it is specific to this screen's load, the fix goes in the file. Record the choice and the alternative in the note. Do not raise the 200 × 5 ms bound.
   - If it is (3), await or abort the stray request in the screen's cleanup, or in the test's teardown before the stub is removed, and add an assertion that `server.sent` has nothing after unmount.
   - If it is (4) or (5), reset the state in the owning module's reset function (`resetApiCache`, `resetThreadCache`, or a new one) and call it from `renderAt` or `afterEach`. Or restore the fake timer or stub in the file that left it.
5. If the cause is in `testing.tsx`, check the other callers of `settle` and `renderAt` under the same full runs. The 20-run proof covers the whole suite, so a fix that breaks another file shows there.

### Tests

- The 16 existing tests in `ApplicationDetail.test.tsx` keep their assertions. No test is removed, skipped, retried or given a longer timeout.
- **20 consecutive full runs** (`npx vitest run` in `web/client`, unfiltered), under the same load that reproduced the failure in step 1, with `ApplicationDetail.test.tsx` passing in all 20 and the suite green in all 20. A note records the command, the load, the commit, and the pass count.
- The same 20 runs on main, before the fix, show the failure at least once, so the proof is not a run too light to reproduce it. If main passes all 20 under that load, raise the load until main fails. Record both.
- If the fix changes `settle` or `renderAt` in `testing.tsx`, its comment names this ticket and the cause, and every other file that calls them passes in the same 20 runs.

### Out of scope

- Raising any timeout, the `settle` bound, or Vitest's `testTimeout`, and turning on `retry` in `vite.config.ts`.
- Other flaky files, unless the same cause shows in them during the 20 runs. If one fails for a different reason, file it as its own `phase-2` ticket rather than fixing it here.
- Changes to `ApplicationDetail.tsx` itself, unless the cause is in the screen, such as a request it fails to abort on unmount.
