---
schema: 3
id: TKT-01M3TNBKNQJNWM29JT10FQN3YQ
title: Print Playwright's error context into the CI log when a test fails
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:ci
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:258
    path: null
  - ref: pr:261
    path: null
claim: null
archive: null
created_at: 2026-10-01T02:40:36Z
updated_at: 2026-10-01T04:56:44Z
created_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-tnbknq
  name: ""
extensions: {}
---

## Description

Filed by the TKT-01M3SZ3GCE lane on 2026-10-01. When a Playwright test fails in CI, its evidence is lost with the job. Playwright writes `error-context.md` and `trace.zip` (`trace: 'retain-on-failure'`) under the run's `results/` directory, which `playwright.config.ts` creates with `mkdtemp` in the container's /tmp. The step log prints only the paths. TKT-01M3SZ3GCE could not be reproduced locally in about 70 full-suite runs under load (see its notes). The one failure, main's push run #1592, left no record of what the page showed when the 5 s ran out. A rare CI flake therefore cannot be diagnosed from the run that saw it.

### What to do

Add a step to the `Build and Test` job in `.forgejo/workflows/ci.yml`, after the Playwright step, that runs only when the job has failed (`if: failure()`). It prints every `error-context.md` under the run's results directory into the log, under a heading that names the test. docs/12 "Changing CI" allows a new step in this job. The file is Playwright's ARIA snapshot of the page at the failure, plain text of a few KB per test, which is small enough to read in the step log.

Find the files by a fixed path, not a guess. One way is to set `KETJU_E2E_DIR` in the Playwright step's environment to a known directory, which `playwright.config.ts` already honours. Another is to search `/tmp/ketju-e2e-*/results`.

Keeping `trace.zip` would need an artifact upload. That is a larger change, and it is not part of this ticket unless the forge's artifact support is confirmed first.

### Done when

- A deliberately failing assertion on a scratch branch shows its `error-context.md` in the new step's log. The PR records that run. The assertion is not merged.
- A green run skips the step.

## Acceptance criteria

- [x] playwright.config.ts lists ['list'] first and ['./scripts/evidence-reporter.mjs'] second, and keeps workers: 1, retries: 0 and trace: 'retain-on-failure'; the PR does not change .forgejo/workflows/ci.yml
- [x] For a result whose status differs from its expected status and is not skipped, scripts/evidence-reporter.mjs writes one block naming the project, file:line and title path, holding the error-context.md file verbatim (cut at 64 KiB) and the serve.log lines written between onTestBegin and onTestEnd (the last 120); a result that is skipped, or whose status equals its expected status, writes nothing
- [x] Only the first 10 failing results get a full block and each later one gets one line with its error-context.md path; an error inside the reporter writes one line and never throws
- [x] scripts/evidence-reporter.test.mjs covers every case named in the plan's Tests and passes under npm test
- [x] A note records a local run on port 18901 of a deliberately failing scratch spec at both widths, with the command and the commit, whose output holds two evidence blocks, each with the Locator: line, the YAML ARIA tree and GET /api/v1/admin/applications in the server lines; the scratch spec is not in the PR's diff
- [x] A note records a local full-suite run with no failures and zero 'failure evidence' lines in its output, and the PR's green CI Playwright step log has zero 'failure evidence' lines
- [x] go generate ./internal/web/spa leaves internal/web/spa/dist unchanged, and the PR's diff touches nothing under web/client/src
- [x] The implementation PR adds a note to TKT-01M3SZ3GCE, before review, saying its blocker is done and what a failing test now prints; TKT-01M3SZ3GCE stays blocked
- [x] The commit body lists the mutations tried against the unit test and whether each was caught

## Implementation plan

Revised at claim, 2026-10-01, against main at d643707. The only change from the groomed plan is the file naming in Approach step 1 and the mutation that covers it. Groomed 2026-10-01 against main at c3de983. A spike on that tree (Approach, "What the spike showed") ran a deliberately failing spec through a throwaway reporter, so the shape below has been run once rather than guessed.

### Settled in grooming

- **The step log is the only record that survives a CI failure.** The runner's artifacts go with its container, and Forgejo's v1 API has no artifact route (orchestrator's instructions for this ticket, 2026-10-01). So the evidence is printed into the Playwright step's log, and `trace.zip` stays out of scope, as the description says, until the forge's artifact support is confirmed.
- **What a failure must leave in the log:** Playwright's `error-context.md` (the error, the failing locator and its call log, the page's ARIA snapshot, the source frame), and the server's log lines from the failing test (orchestrator, 2026-10-01; the description asks for `error-context.md` under a heading that names the test).
- **A passing run's log does not grow.** A passing, skipped or expected-to-fail test prints nothing new (orchestrator, 2026-10-01; the description's "A green run skips the step").
- **No CI step is weakened and no retry is added.** `playwright.config.ts` keeps `workers: 1`, `retries: 0`, `trace: 'retain-on-failure'` and the `list` reporter. No step in `.forgejo/workflows/ci.yml` is removed, reordered or given `continue-on-error` (orchestrator, 2026-10-01; docs/12 "Changing CI").
- **The proof is a deliberately failing test run locally, and that test is never committed** (orchestrator, 2026-10-01). This replaces the description's scratch-branch CI run: see Tests for what the PR's own green CI run proves instead.
- **Playwright runs on port 18901** (`KETJU_E2E_PORT=18901`) in this lane.
- **The client dist does not change.** The ticket touches test config and a script, not `web/client/src`, so `go generate ./internal/web/spa` must leave `internal/web/spa/dist` unchanged and this lane does not hold the dist (docs/13 "Running two lanes, changed").

### Approach

The evidence is printed by a Playwright reporter that runs beside `list`, not by a CI step. The alternative and why it lost are at the end of this section.

1. **`web/client/scripts/evidence-reporter.mjs`**, a new Playwright reporter (plain ESM, like `scripts/bundle.mjs`; `package.json` is `"type": "module"`). It is a default-exported class with an optional `{ write }` option (default `process.stdout.write`) so the unit test can capture its output.
   - `onBegin`: the server log is `join(process.env.KETJU_E2E_DIR, 'serve.log')`. `playwright.config.ts` sets `KETJU_E2E_DIR` in the runner process before reporters load, and `ketju e2e` writes its request log there (`runE2E` in `cmd/ketju/e2e.go`).
   - `onTestBegin(test)`: record `serve.log`'s size under `test.id` (0 if the file does not exist yet).
   - `onTestEnd(test, result)`: take and delete the recorded offset. If `result.status === test.expectedStatus`, or the status is `skipped`, return and write nothing. A `skipped` result whose expected status is `passed` is a test that did not run, for example after a serial group's failure, and Playwright's summary counts it under "did not run". The failure that stopped it prints its own block. Otherwise write one block:
     - an opening line `── failure evidence: [<project>] › <file>:<line> › <title path> (<status>) ──`, with the file relative to the config file's directory (`dirname(config.configFile)`, falling back to `config.rootDir`). In Playwright 1.63, `rootDir` is the test directory, so it would drop the `e2e/` prefix that `list` prints (found in the first local run at claim);
     - the `error-context` attachment's file verbatim, under `error-context.md (<path>)`, cut at 64 KiB with a line naming the path and the bytes left out. If there is no such attachment, a line saying so;
     - `serve.log` from the recorded offset to its current size, under `serve.log during this test`, keeping the last 120 lines and saying how many were left out. If the file does not exist, a line saying so;
     - a closing line `── end of failure evidence ──`.
   - Only the first 10 failing results get a block. Each one after that gets a single line naming the test and its `error-context.md` path, so a run in which the server died prints a bounded log.
   - Every hook body is wrapped in `try`/`catch`. An error writes one line `failure evidence: could not print (<message>)` and never throws, so the reporter cannot change a run's outcome.
   - It implements no `onStdOut`, `onStdErr` or `printsToStdio`, so `list` keeps its own output unchanged.
2. **`web/client/playwright.config.ts`**: `reporter: [['list'], ['./scripts/evidence-reporter.mjs']]`, with a comment saying why. The list reporter stays first, so a failing test's `✘` line comes before its evidence block. Nothing else in the config changes.
3. **`web/client/README.md`**, in the Playwright paragraph: one sentence saying that a failing test prints its `error-context.md` and its `serve.log` lines into the run's output, which is what CI keeps.
4. **`.forgejo/workflows/ci.yml` is not changed.** The Playwright step's log is the stdout of `npx playwright test`, which already uses the config's reporters.
5. **Check what CI runs:** in `web/client`, `npm test` (which runs the new unit test) and `npx tsc --noEmit -p .`; at the root, `go generate ./internal/web/spa` followed by `git status --short internal/web/spa/dist` showing nothing. No Go code changes, but `test -z "$(gofmt -l .)"`, `go build ./... && go vet ./...` and `go test -race ./...` still run before the push.

### What the spike showed

A throwaway reporter loaded with `--reporter=list,<path>` ran a scratch spec that signs in as mona, opens `/admin/applications` and waits for `[data-application-link="999"]`. This is the same step and wait that failed in TKT-01M3SZ3GCE's run #1592. On the desktop project:
- At `onTestEnd`, `result.attachments` already held `error-context` and `trace`, with paths under the run's `results/` directory.
- The `error-context.md` file was 3.5 KB and held the test name and location, `Locator: locator('[data-application-link="999"]')`, `Timeout: 5000ms`, the call log, the ARIA tree of the whole page (the table "Pending applications, newest first" with rows `#2` and `#1`), and the source frame at line 10.
- The `serve.log` slice from `onTestBegin` to `onTestEnd` held 15 JSON lines: the page, the assets, the lazy `AdminRoot` chunk, and `GET /api/v1/admin/applications` with status 200, 281 bytes and 0 ms, each with its timestamp. That is the information the TKT-01M3SZ3GCE investigation had to rebuild by hand.
- The list reporter prints only `Error Context: <path>` for the same file.

### Alternative that lost: an `if: failure()` step in ci.yml

The description proposed a new step after the Playwright step that runs `if: failure()` and prints every `error-context.md` it finds. It lost for these reasons:
- It cannot give the failing test's server lines. The suite continues after a failure, so `serve.log`'s tail at the end of a 10-minute run shows the last spec's requests. Only the runner knows where each test started and ended.
- It needs a fixed directory. That means adding `KETJU_E2E_DIR` to the Playwright step, which edits an existing step. Also, `ketju e2e` calls `os.Mkdir`, so the directory's parent must be made first. Otherwise it searches `/tmp/ketju-e2e-*`, which the description calls a guess.
- Its heading would come from Playwright's output directory names, which are sanitized and shortened. The reporter has the project, the file, the line and the title path.
- It also runs when an earlier step fails, such as gofmt. And it never runs locally, so a local failure keeps printing only a path.

The reporter's risk is that it runs inside the Playwright process. It is contained by the `try`/`catch` in step 1, by the unit test, and by the PR's green CI run, which loads the reporter in the Alpine image.

### Tests

- **`web/client/scripts/evidence-reporter.test.mjs`** (Vitest, `// @vitest-environment node`, already within `include: ['scripts/**/*.test.mjs']`). It drives the class directly, with fake `TestCase` and `TestResult` objects, a temp `KETJU_E2E_DIR` with a `serve.log`, and an `error-context.md`. Named cases:
  - `a passing test prints nothing`: also covers skipped, expected-to-fail (`expectedStatus: 'failed'`, `status: 'failed'`), and did-not-run (`expectedStatus: 'passed'`, `status: 'skipped'`), and the captured output is the empty string;
  - `a failing test prints its error context and the server lines it caused`: lines appended to `serve.log` before `onTestBegin` are absent, lines appended between `onTestBegin` and `onTestEnd` are present, and the block names the project, the file and line, and the title;
  - `a timed-out test prints too`: `status: 'timedOut'`;
  - `the server lines are cut to the last 120`, with a line counting what was left out;
  - `a large error context is cut at 64 KiB`, naming the path;
  - `after ten failures only a line per test`;
  - `a missing error context or serve.log is said, not thrown`;
  - `an error inside the reporter writes one line and does not throw`: for example, an attachment path that is a directory.
- **Local proof, recorded in a note before the PR is opened.** On the implementation branch, with `KETJU_E2E_PORT=18901`:
  1. Write a scratch spec, `e2e/zz-scratch-fail.spec.ts`, that waits for a link that is never rendered, as in the spike. Run `npx playwright test e2e/zz-scratch-fail.spec.ts` at both widths. The output must hold two evidence blocks, each with the `Locator:` line, a `# Page snapshot` or YAML ARIA tree, and `GET /api/v1/admin/applications` in the server lines. Record the command, the commit and an excerpt in the note. Delete the spec, and show with `git status` that it is gone before committing.
  2. Run the full suite, `npx playwright test`, without the scratch spec. `grep -c 'failure evidence'` on its output must be 0. Record it in the note.
- **CI.** The PR's own green run is the proof that the reporter loads in CI's image and stays silent there. The Playwright step log holds no `failure evidence` line. Read the step log for this, not the run summary.
- **Mutations**, listed in the commit body with whether the unit test caught each one: printing on `result.status === 'passed'` only (an expected failure then prints); slicing `serve.log` from 0 instead of the recorded offset; dropping the 10-failure cap; naming files from `rootDir` instead of the config's directory.

### Out of scope

- Keeping `trace.zip` or any artifact upload. That waits until the forge's artifact support is confirmed (description). No ticket owns it yet, and none is filed, because the confirmation comes first.
- Changing the CI container, the Playwright step, workers, retries or timeouts (docs/12 "Changing CI"; Settled in grooming).
- Diagnosing TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load). That ticket stays blocked until the failure recurs. This ticket's implementation PR adds a note to it saying what the next failure will print.
- Global errors outside a test, such as the web server failing to start. Playwright already prints those, and the server's stderr is piped by `webServer`.

## Notes

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T03:46:14Z

Grooming, 2026-10-01, against main at c3de983. The plan and the criteria replace the description's "What to do" and "Done when" in two places, and the description is left as filed:

- **A reporter, not an `if: failure()` step.** A Playwright reporter beside `list` (`scripts/evidence-reporter.mjs`) prints the evidence inline. It is the only place that knows where each test started and ended in `serve.log`. A step that runs after a 10-minute suite would print the log's tail, which belongs to the last spec, not to the failing test. The step would also have to edit the existing Playwright step to fix the run directory. The plan's "Alternative that lost" has the full comparison. `.forgejo/workflows/ci.yml` does not change.
- **A local proof, not a scratch-branch CI run.** The orchestrator's instructions for this ticket name a deliberately failing test run locally, never committed. The PR's own green CI run proves that the reporter loads in CI's Alpine image and prints nothing there, because the Playwright step's log is the stdout of the same `npx playwright test`.

A spike ran a scratch spec that waits on `/admin/applications` for a link that is never rendered, through a throwaway reporter. It confirmed that `result.attachments` holds `error-context` at `onTestEnd`. It also confirmed that the file carries the locator, the call log and the page's ARIA tree, and that a `serve.log` slice between `onTestBegin` and `onTestEnd` holds exactly that test's requests (the plan's "What the spike showed"). The spike's files are in scratch and are not committed.

No open decision. Every choice in the plan is an engineering choice: where the code lives, the caps (64 KiB, 120 lines, 10 blocks), and reporter over step.

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:30:18Z

Proof, 2026-10-01, on commit 7da9abe (branch run/tnbknq-ci-error-context, on main at d643707). Both runs are from `web/client` on this host. The logs are in a scratch directory under ~/.cache/agent-scratch/ketju/.

### A deliberately failing spec, at both widths

The scratch spec `e2e/zz-scratch-fail.spec.ts` is not committed, and it was deleted before the run below. As mona, it opens `/admin/applications`, checks the `h1`, and waits for `[data-application-link="999"]`, which is never rendered. This is the step and the wait that failed in TKT-01M3SZ3GCE's run #1592.

```
GOROOT=/home/sothr/.local/share/mise/installs/go/1.27.1 KETJU_E2E_PORT=18901 npx playwright test e2e/zz-scratch-fail.spec.ts
```

Result: 2 failed (phone and desktop), 4 passed (setup), 14.5 s. Under each `✘` line the output held one block:

```
── failure evidence: [phone] › e2e/zz-scratch-fail.spec.ts:7 › scratch: a link that is never rendered (failed) ──
error-context.md (/tmp/ketju-e2e-gmoWKq/results/zz-scratch-fail-scratch-a-link-that-is-never-rendered-phone/error-context.md):
...
Locator: locator('[data-application-link="999"]')
Expected: visible
Timeout: 5000ms
...
  - table "Pending applications, newest first":
...
      - row "#2 Oct 1, 2026, 4:17 AM quinn@example.test verified via fake":
...
serve.log during this test (/tmp/ketju-e2e-gmoWKq/run/serve.log), 15 lines:
...
{"time":"2026-09-30T23:18:57.357299676-05:00","level":"INFO","msg":"request","method":"GET","path":"/api/v1/admin/applications","status":200,"bytes":281,"duration_ms":0,"viewer":3}
...
── end of failure evidence ──
```

The desktop block is the same, with 14 server lines. Each block holds the `Locator:` line, the YAML ARIA tree of the page, and `GET /api/v1/admin/applications` in the server lines. No server line from the setup project or from the other width appears in either block.

### The full suite, passing

```
GOROOT=/home/sothr/.local/share/mise/installs/go/1.27.1 KETJU_E2E_PORT=18901 npx playwright test
```

The run had 193 passed, 11 skipped and 0 failed, in 4.2 min. `grep -c 'failure evidence'` on its output gives 0. The output is 223 lines, which are the list reporter's alone.

### Also run on 7da9abe

- `npm test`: 69 files, 2040 tests, all passing, including the 8 in `scripts/evidence-reporter.test.mjs`.
- `npx tsc --noEmit -p .`: clean.
- `go generate ./internal/web/spa`, run after #259 changed the dist on main: `git status` shows nothing under `internal/web/spa/dist`.
- The eight mutations in the commit body were each caught by the unit test.

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:34:57Z

Review 1636 (Terva run 790d3c0e-0afb-441f-a3df-ea67f4788adf, Actions run #1703) on PR #261 reviewed head 771ce11 against base d643707. It had two medium findings, and both are fixed in f87aed9:

- **"Bound the server-log read before allocating memory"** (`evidence-reporter.mjs:106`). Fixed. The reporter now reads only the last 256 KiB of a test's `serve.log` slice (`maxLogTailBytes`), drops the first line when the read began inside one, and reports how many earlier bytes it left out. The new case `a large server log is read only from its last 262144 bytes` covers it.
- **"Count successfully printed blocks toward the ten-block cap"** (`evidence-reporter.mjs:69`). Fixed. The count goes up only after a block is written. The new case `a failure that could not be printed does not count toward the cap` covers it.

This note supersedes the plan's Approach step 1 in two places:
- The server lines are "the last 120 lines, read from at most the slice's last 256 KiB", and the block reports bytes left out rather than lines.
- "Only the first 10 failing results" means the first 10 blocks printed.

Criterion 2's "(the last 120)" is now a ceiling: a test whose log lines average over about 2 KiB shows fewer.

Re-run on f87aed9:
- The scratch spec, restored for the run and deleted again, gave 2 failed and 4 passed. Each width printed a block with `Locator:`, the ARIA tree, and `GET /api/v1/admin/applications` among "the last 14 lines" and "the last 15 lines".
- `npm test`: 69 files, 2042 tests.
- `npx tsc --noEmit -p .`: clean.
- 11 mutations, each caught.

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:38:01Z

Review 1637 (Terva run a00336c6-4d35-4940-b267-a91961b48aae, Actions run #1706) on PR #261 reviewed head 5a2bd22 against base b3cf1f4. It marked both of review 1636's findings resolved. It raised one low finding, 'Keep a complete line when the log tail starts at its boundary' (evidence-reporter.mjs:108), which is fixed in 80b69c4. The tail is now read from the byte before it, so the dropped first segment is the cut fragment or the empty string after a newline, never a whole line. The new case 'a line that starts exactly at the tail is kept' covers it, and the mutation that reads from the tail itself again is caught. npm test (69 files, 2043 tests) and tsc are clean on 80b69c4.

**agent:claude-code/phase2-run-tnbknq** at 2026-10-01T04:56:36Z

CI proof, 2026-10-01: PR #261's run #1707 on c37c38d (base b3cf1f4) is green. Its 'Playwright smoke at 390 px and 1440 px' step log, read from the step log rather than the summary, has 0 lines containing 'failure evidence' and no failed test: 193 passed and 11 skipped in 9.9 min. It is 409 lines and 32334 bytes, against 409 lines and 32333 bytes in main's green push run #1677 on c3de983, so a passing run's log did not grow. The run also proves the reporter loads in CI's Alpine image. Together with the local full-suite note, this satisfies criterion 6.

## Summary

Landed in PR #261. A failing Playwright test now leaves its evidence in the run's output, which is the only record CI keeps.

- **`web/client/scripts/evidence-reporter.mjs`** runs after `list` (`web/client/playwright.config.ts`). For each result that failed, timed out or was interrupted, it prints one block under the test's `✘` line, between `── failure evidence: [<project>] › <file>:<line> › <title path> (<status>) ──` and `── end of failure evidence ──`. The block holds:
  - Playwright's `error-context.md`, verbatim and cut at 64 KiB: the error, the locator and its call log, the page's ARIA tree, and the source frame;
  - the `serve.log` lines from that test only, at most the last 120 and read from at most the slice's last 256 KiB.
- A passed, skipped or expected-to-fail result prints nothing. After ten printed blocks, each further failure gets one line, and errors are written as one line and never thrown.
- **Unchanged:** `.forgejo/workflows/ci.yml`, `workers: 1`, `retries: 0`, `trace: 'retain-on-failure'`, and `web/client/src` and the dist.
- **Tests:** `scripts/evidence-reporter.test.mjs` has 11 cases, and 12 mutations were each caught.
- **Proof:** a deliberately failing scratch spec, never committed, printed both parts at both widths. A full local suite run, and the PR's green CI Playwright step log, held no evidence line.
- **Review:** three rounds. Review 1636 had two medium findings (an unbounded log read, and the cap counting failed attempts). Review 1637 had one low finding (a whole line dropped at the tail boundary). All three are fixed, and run a17d0335 was clean on c37c38d.
- **Why a reporter rather than the description's `if: failure()` step:** the plan's "Alternative that lost" explains it. In short, only the runner knows each test's slice of `serve.log`.
- **Still open:** `trace.zip` is still lost until the forge's artifact support is confirmed. TKT-01M3SZ3GCE stays blocked until its failure comes back, and its note says what that run will print. TKT-01M3TSZHSW was filed by this lane as a draft with no milestone.
