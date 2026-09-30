---
schema: 3
id: TKT-01M3S8Y2NAEJ64N6ZGFV43F7A5
title: Stop ApplicationDetail.test.tsx failing in full vitest runs
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
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:44:15Z
updated_at: 2026-09-30T22:15:58Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-s8y2na
  name: ""
extensions: {}
---

## Description

`web/client/src/screens/admin/applications/ApplicationDetail.test.tsx` failed in 2 of 6 full vitest runs on the new-thread-screen lane (TKT-01M3NS16VH, local branch at 6b5dc9a, 2026-09-30). It passes when run alone, and that lane does not touch it. So some state leaks in from other test files, or the test depends on timing that a full parallel run disturbs.

### What to do

Reproduce the failure with the full suite (`npx vitest run`, repeated). Name the failing assertion and the cause, whether that is shared module state, a fake timer, an unawaited promise, or a fetch mock another file leaves behind. Fix the cause, not the symptom: no retries, no larger timeouts, no skipped test. Show the file passing 20 full runs in a row.

Reported by the Phase 2 run orchestrator on 2026-09-30.

## Acceptance criteria

- [x] Before any code change, a note names the failing test, its failing assertion and message, whether Vitest reported an assertion failure or an unhandled error, and the cause, with the runs that separate it from the other candidates in the plan (file alone under load, full suite with one worker, resolved isolate setting)
- [x] The diff fixes the named cause and adds no retry, no larger timeout or settle bound, and no skip, todo or only; the 200 x 5 ms bound in web/client/src/testing.tsx is unchanged
- [x] The 16 existing tests in ApplicationDetail.test.tsx keep their assertions
- [x] A note records 20 consecutive unfiltered npx vitest run passes in web/client with ApplicationDetail.test.tsx passing in each, with the command, the CPU load, and the commit
- [x] A note records that the same runs under the same load reproduce the failure on main before the fix at least once

## Implementation plan

Revised 2026-09-30 on claim, against main at 387c401, after the investigation recorded in the first note. The groomed plan listed five candidate causes. The evidence names candidate 2: the first import of the lazy admin chunk in a test file is transformed and evaluated inside `settle`'s 200 × 5 ms bound, and on a loaded host that import takes longer than the bound. The same failure shows in FormEditor.test.tsx, Decision.test.tsx and routes.test.tsx, so the fix is general and goes in testing.tsx, as step 4 of the groomed plan prescribes for a general cause.

### Settled in grooming

- **The fix addresses the cause.** No retries (`retry`, `--retry`, or a loop around an assertion), no larger timeout or longer `settle` bound, and no `skip`, `todo` or `.only`. This comes from the ticket's description, which the Phase 2 run orchestrator filed on 2026-09-30. A wait on a condition the assertion reads is not a retry, as long as its bound is no longer than the one `settle` has today (1 s).
- **The proof is 20 full runs in a row** with the file passing in every one. A full run is `npx vitest run` in `web/client`, with nothing filtered.
- **The investigation comes first and is recorded.** Before any code changes, a note names the failing test, the failing assertion with its message, and the cause, with the evidence that separates it from the other candidates below.

### Approach

1. In `web/client/src/testing.tsx`, add a table of the lazy chunks that `routes.tsx` splits off, keyed by the addresses that load them: `/admin` loads `./screens/admin/AdminRoot`, and `/apply` loads `./screens/apply/ApplyRoot`. `renderAt` awaits the import for its URL before it mounts anything. `routes.tsx`'s `lazy()` import still runs, and it resolves to the module already loaded. The comment names this ticket and the cause.
2. `settle` is unchanged: its 200 × 5 ms bound and its code stay as they are. Its comment is updated to say that `renderAt` now loads the chunk. The import has no bound of its own beyond Vitest's unchanged 5 s test timeout, because it waits on the chunk itself, not on a clock. No `retry`, no new or raised timeout, and no `skip`, `todo` or `only`.
3. `ApplicationDetail.test.tsx` is not changed, so its 16 tests keep their assertions. The `beforeAll` preloads in AgentControls.test.tsx and QueueScreen.test.tsx become redundant. They are left alone as out of scope, because removing them is churn this ticket does not need.
4. No change under `web/client/src` outside test support: testing.tsx is imported only by tests. `go generate ./internal/web/spa` must leave `internal/web/spa/dist` unchanged.

### Tests

- The same load that reproduced the failure on main: 48 unpinned busy `yes` loops on the 12-CPU host. Run 20 consecutive `npx vitest run` in `web/client`, unfiltered, on the fix. Every run must be green, with ApplicationDetail.test.tsx passing in each. The same 20 runs on main, recorded in the first note and the next, must show the failure.
- `npx tsc --noEmit -p .` and `npm run build` in `web/client` pass. The dist is unchanged.

## Notes

**agent:claude-code/phase2-run-s8y2na** at 2026-09-30T21:03:46Z

Cause, found before any code change. Commit 87c567f (origin/main 387c401 plus the claim), web/client, 12-CPU host shared with other lanes. Load is N busy `yes` loops, unpinned and killed by a trap after each batch. Results, logs and the run script are in a scratch directory under ~/.cache/agent-scratch/ketju/.

### The failure

- Test: `an application > shows the answers through the form renderer, against the version they answered`, the first test in the file.
- Assertion: `expect(read).not.toBeNull()` at ApplicationDetail.test.tsx:48:22, message `AssertionError: expected null not to be null`.
- Vitest reports an assertion failure. Every run had 0 unhandled errors.
- A failing test takes 1371 to 1581 ms. A passing first test takes 150 to 600 ms. TKT-01M3NS16VH saw the failure "at about 1.4 s".

### The cause: candidate 2, the lazy admin chunk

The first import of the lazy admin chunk (`routes.tsx` `lazy(() => import('./screens/admin/AdminRoot'))`) in a test file has to be transformed and evaluated. On a loaded host, or while other workers ask Vite for transforms at the start of a parallel run, that takes longer than `settle`'s 200 turns of 5 ms. `settle` then returns with no `[data-screen]` in the root, and the test's first query finds nothing.

To see this, I ran instrumented runs with a temporary copy of `settle` that logged its turn count, its time, whether `[data-screen]` was present, and whether the detail still said "Loading…". The instrumentation was reverted before any other change.
- Alone, with the host at load average about 46: 2 of 4 runs failed. In both, `settle` used all 200 turns (1281 and 1598 ms) and returned with `screen: false`.
- Alone, 20 runs under 12 loops: all passed. The first test's first `settle` still used 55 to 161 of its 200 turns, 317 to 989 ms, on the chunk alone.

### What separates it from the other candidates

- (a) The file alone under load, `npx vitest run src/screens/admin/applications/ApplicationDetail.test.tsx`, 20 runs under 48 loops: 7 failed (runs 1, 2, 3, 5, 6, 9, 12). Every failure was the same test and the same assertion, at 1371 to 1581 ms. The cause is inside the file's own run, so state from another file (candidate 5) is not needed to produce it.
- (b) The full suite with one worker, `npx vitest run --maxWorkers=1`, under 48 loops: all 5 runs passed. That is 5 runs rather than the plan's 20, because each run takes about 5 minutes under this load. This file ran 10th to 20th, after 3 to 9 other admin files. With one worker, earlier admin files have already had the chunk transformed by the time this file runs, so its import only evaluates. The failure follows the chunk's first, cold import, not load on its own.
- (c) Resolved config, printed with `createVitest('test')` from `vitest/node` (Vitest 5.0.1 has no `--printConfig`): `pool: forks`, `isolate: true`, `environment: happy-dom`, `testTimeout: 5000`. Each file has its own module graph, which rules out candidate 5.
- Candidate 1 (`settle` returns before the data): ruled out. Across 735 `settle` calls in this file over 25 instrumented runs, none returned with `[data-screen]` present while the detail still showed "Loading…". The stubbed fetch, `Response.json()` and the re-render always finished inside the final 5 ms act.
- Candidate 3 (an unawaited promise after unmount): ruled out. There were 0 unhandled errors in every run, and the failing test is the first in the file, so no earlier test could leave a request running.
- Candidate 4 (shared state within the file): ruled out for the same reason. The failure is always the first test, before any other test in the file has run.

### The same cause in other files, so the fix is general

Full runs on main, `npx vitest run` with nothing filtered: 9 under 4 loops pinned to CPUs 0 and 1, 10 under 12 loops and 10 under 36 loops all passed. 10 under 48 loops failed in 7 runs. Every failure was the first test to render the admin chunk in its file, at 1415 to 1896 ms, meaning `settle` used all 200 turns:
- FormEditor.test.tsx 'draws the active version as a draft of fields, and its form as the preview', `expected null not to be null`, in runs 4, 5 and 7;
- Decision.test.tsx 'prefills the suggested handle, which stays editable', `Cannot read properties of null (reading 'value')`, in runs 7 to 10;
- routes.test.tsx '/admin renders admin', `expected null to be 'admin'`, in run 4.

Vitest's sequencer runs a file that failed last time first in the next run, while every transform is still cold. So one failure makes the next more likely: Decision.test.tsx failed 4 times in a row here, and TKT-01M3NS16VH's lane saw this file fail 2 times in 6. AgentControls.test.tsx and QueueScreen.test.tsx already import `AdminRoot` in a `beforeAll` (PR #179 and d512bd3) and never failed.

### The fix

`renderAt` in testing.tsx awaits the import of the lazy chunk its address needs (the admin chunk for /admin, the apply chunk for /apply) before it renders. The test then waits on the chunk itself, not on a clock, and `settle`'s 200 × 5 ms bound is left for the screen's render and its requests. The bound is unchanged. The import is bounded only by Vitest's unchanged 5 s test timeout. `routes.tsx`'s lazy `import()` still runs, and it resolves to the module already loaded.

Alternatives considered:
- A `beforeAll` import in this file only, as AgentControls and QueueScreen do. This lost because the same failure showed in three other files in the same runs, and the plan puts a general cause in testing.tsx.
- A `beforeAll` with a 30 s hook timeout, as those files use. This lost because it adds a timeout, which the ticket rules out.
- Preloading inside `settle`. This lost because it would add an await to every `settle` call on an admin path while an app is mounted, which changes the timing of every such test. `renderAt` imports before it mounts anything.

**agent:claude-code/phase2-run-s8y2na** at 2026-09-30T21:54:19Z

Proof, on commit 2f7e300 (the fix, on main 387c401) against main itself. Everything ran in web/client on the same 12-CPU host. The load was 48 unpinned busy `yes` loops, started before each batch and killed by a trap after it, with other lanes also running. Load average was 66 to 74 during the proof batches. Each run was `npx vitest run --reporter=json --outputFile=<file>`: unfiltered, and the reporter only writes the result file.

This corrects the first note. It said the 10 unseeded full runs on main under 48 loops were red in 7 runs. They were red in 6: runs 4, 5, 7, 8, 9 and 10. The files and tests it named are right.

### Two conditions, the same on main and on the fix

- **Unseeded**: the plain command, with Vitest's results cache as the previous run left it.
- **Seeded**: before each run, ApplicationDetail.test.tsx is marked `failed` in `node_modules/.vite/vitest/*/results.json`, which is the state one failure leaves. Vitest's sequencer then runs the file first, while every transform is still cold. This is how TKT-01M3NS16VH's lane saw it fail 2 times in 6. Unseeded, the file runs after other admin files that pay for the chunk's first import, so on main the failure lands on them instead.

### Main, before the fix (387c401 plus ticket commits; the test code is main's)

- Unseeded, 20 full runs: the suite was red in 7 (6 in the first 10, 1 in the second 10). In every red run, the failing test was the first test to render the admin chunk in FormEditor.test.tsx, Decision.test.tsx or routes.test.tsx. ApplicationDetail.test.tsx failed in 0.
- Seeded, 20 full runs: ApplicationDetail.test.tsx failed in 8, and the suite was red in 8. Every time it was 'shows the answers through the form renderer, against the version they answered', with `expected null not to be null` at line 48. FormEditor and routes failed beside it in 3 of those runs.
- The file alone, 20 runs: 7 failed.
- 0 unhandled errors in any run.

### The fix, 2f7e300

- **Seeded, 20 consecutive full runs: 20 green.** 1878 of 1878 tests passed in each, ApplicationDetail.test.tsx passed in each, and there were 0 unhandled errors.
- **Unseeded, 20 consecutive full runs: 20 green.** Again 1878 of 1878 in each, ApplicationDetail.test.tsx passing in each, and 0 unhandled errors. Every other caller of `renderAt` and `settle` passed in all 40 runs.
- The file alone, 20 runs: 20 passed.

### Mutations

Each mutation ran 10 times, with the file alone under the same load:
- The import started but not awaited (`void` for `await`): 3 of 10 failed. Caught.
- The `/admin` entry removed from `lazyChunks`: 4 of 10 failed. Caught.

### Other checks

- `go generate ./internal/web/spa` leaves `internal/web/spa/dist` unchanged: `git status` is clean after it. The thread-view bundle is 56988 B gzip, unchanged.
- `npx tsc --noEmit -p .` passes. So do gofmt, `go build ./...`, `go vet ./...` and `go test -race ./...`.
- The diff is testing.tsx only. ApplicationDetail.test.tsx is unchanged, so its 16 tests keep their assertions. There is no retry and no new or raised timeout, `settle`'s 200 × 5 ms loop is byte for byte the same, and there is no skip, todo or only.

**agent:claude-code/phase2-run-s8y2na** at 2026-09-30T22:15:58Z

The proof held after the branch was carried onto the current main. Main moved 34 commits while the proof ran (TKT-01M3KFFFJR, TKT-01M3SK2HFE, TKT-01M3QV39H4 and TKT-01M3QS0TMV). None of them touches testing.tsx or the lazy chunks in routes.tsx. The branch had not been pushed yet, so it was rebased onto origin/main 4471ea6. The change the earlier notes call 2f7e300 is now 507b946, with the same diff, and the head is 363d093.

On 363d093, under the same 48 unpinned busy loops, with load average 62 to 71:
- 20 consecutive unseeded `npx vitest run`: 20 green, 1940 of 1940 tests passing in each, and ApplicationDetail.test.tsx passing in each.
- 20 consecutive seeded runs (ApplicationDetail.test.tsx marked failed in the results cache before each run): 20 green on the same terms.
- 0 unhandled errors in any run.

Other checks on this head: `npx tsc --noEmit -p .` passes. `go generate ./internal/web/spa` leaves `internal/web/spa/dist` unchanged. The thread-view bundle is 58151 B gzip, which is main's figure.

## Summary

Fixed in 2f7e300, which changes web/client/src/testing.tsx only.

The cause was candidate 2. A test file's first import of the lazy admin chunk was transformed and evaluated inside `settle`'s 200 × 5 ms bound. On a loaded host the import outran the bound, and the first test found no `[data-screen]`. That test was 'shows the answers through the form renderer' at line 48, and the same failure hit the first test of FormEditor.test.tsx, Decision.test.tsx and routes.test.tsx.

`renderAt` now awaits the lazy chunk its address needs (admin for /admin, apply for /apply) before it mounts anything. `settle`, its bound and every timeout are unchanged, and ApplicationDetail.test.tsx is unchanged.

Proof under 48 busy loops:
- On main: the suite was red in 7 of 20 unseeded full runs, and ApplicationDetail failed in 8 of 20 seeded full runs.
- On the fix: 20 of 20 seeded and 20 of 20 unseeded full runs were green.
- Both mutations were caught.
- The dist is unchanged.

The `beforeAll` preloads in AgentControls.test.tsx and QueueScreen.test.tsx, with their 30 s hook timeouts, are now redundant. They were left in place.
