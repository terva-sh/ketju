---
schema: 3
id: TKT-01M3TNBKNQJNWM29JT10FQN3YQ
title: Print Playwright's error context into the CI log when a test fails
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-10-01T02:40:36Z
updated_at: 2026-10-01T02:40:36Z
created_by:
  id: agent:claude-code/phase2-run-sz3gce
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-sz3gce
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
