---
schema: 3
id: TKT-01M29CGBYHESRJT1AG0M65X7NN
title: Enforce the client bundle budget in CI
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:test
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
blocks_on: none
references:
  - ref: design:preact-client
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-24T05:21:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Fail the build when the thread view bundle exceeds 60 KB gzipped.

A budget nobody measures is a preference. This is cheap to add while the number is comfortably under, and it is the check that makes adding a component library a deliberate decision rather than an accident in a dependency bump.

## Acceptance criteria

- [ ] CI fails when the thread view bundle exceeds 60 KB gzipped
- [ ] The measured size is reported on every build so a trend is visible
- [ ] Raising the budget requires editing a committed number rather than a flag

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `web/client/budget.json` is committed and holds `{"threadViewGzipBytes": 61440}` (60 KiB; state whether 60 KB means 60000 or 61440 in the file's sibling README line). Only an edit to this file raises the budget. The script accepts no CLI or env override (AC3).
2. `web/client/scripts/budget.mjs` reads Vite's `dist/.vite/manifest.json` (enabled in CCC02). It walks the entry chunk plus the ThreadPage route's lazy chunk and every chunk they import statically, together with their CSS files. It gzips each at level 9 with Node's `zlib` and sums the results.
3. It prints one line per file and a total, such as `thread-view: 41.2 KiB gzip / 60.0 KiB budget (68%)`, then exits 1 if the total exceeds the budget.
4. `npm run build` runs `vite build && node scripts/budget.mjs`, so `go generate` and CI both run it. In `.forgejo/workflows/ci.yml`, the client steps from CCC02 already run in the required `Build and Test` job, so a failure there blocks the merge.
5. Trend: the per-build line in the step log satisfies "reported on every build". Optionally append the number to the job summary, if Forgejo Actions supports `$GITHUB_STEP_SUMMARY`; check at claim.
6. Add a guard asserting that `package.json` dependencies contain no markdown parser (marked, markdown-it, micromark, remark). That supports C2WWG AC4 and docs/06's "no client-side markdown parser".

### Files
`web/client/budget.json`, `web/client/scripts/budget.mjs`, `web/client/package.json` (scripts), `.forgejo/workflows/ci.yml` if a separate step is clearer.

### Tests
- AC1: a Vitest test for `budget.mjs` with a fixture manifest and files sized just under and just over the budget asserts exit codes 0 and 1. Prove it once in CI too: a branch that sets the budget to 1 KiB fails.
- AC2: read the CI step log on the PR (not the green summary) and quote the printed size line in the ticket note.
- AC3: the script reads only `budget.json`. A test asserts that `process.argv` and `process.env` are not consulted (a grep for `argv` and `env` in the script).

### Design
docs/07 "Preact client" (Target bundle < 60 KB gzipped for the thread view); docs/10 Q21 (checked against the budget); CEE52's lazy admin split; CCC0F's fonts.

### Interactions
Depends on CEE6V (the thread view exists) and BW9QP (CI). It relies on CCC02's manifest and Node-in-CI step, and on CEE52's route-level code splitting.
Open in docs: whether "thread view bundle" includes CSS and the self-hosted woff2 fonts. JS plus CSS is assumed and fonts are excluded; the three mockup families alone would come close to the budget.

### Settled in grooming (2026-09-24)

- The budget counts the thread view's JavaScript and CSS, gzipped. Fonts are excluded (docs/07).
