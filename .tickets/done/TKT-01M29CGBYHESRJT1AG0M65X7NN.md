---
schema: 3
id: TKT-01M29CGBYHESRJT1AG0M65X7NN
title: Enforce the client bundle budget in CI
type: chore
status: done
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
  - ref: pr:105
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-28T08:10:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Fail the build when the thread view bundle exceeds 60 KB gzipped.

A budget nobody measures is a preference. This is cheap to add while the number is comfortably under, and it is the check that makes adding a component library a deliberate decision rather than an accident in a dependency bump.

## Acceptance criteria

- [x] CI fails when the thread view bundle exceeds 60 KB gzipped
- [x] The measured size is reported on every build so a trend is visible
- [x] Raising the budget requires editing a committed number rather than a flag

## Implementation plan

Revised at claim (2026-09-28) against main e070d97.

### What the code has now
- Vite writes `internal/web/spa/dist/.vite/manifest.json` (`build.manifest: true`, set for this ticket in CCC02).
- The thread page is not lazy. The only lazy chunk is the admin screen (`src/screens/admin/AdminRoot.tsx`), held separate by `TestTheAdminBundleIsSeparate`.
- The shell loads three files from /assets: `theme-early-*.js` (classic script, head), `index-*.js` (entry) and `index-*.css`.
- Measured at claim, gzip level 9: index JS 31058 B, index CSS 6511 B, theme-early 391 B. Total 37960 B, 63% of 60000.

### Approach
1. `web/client/budget.json` holds `{"threadViewGzipBytes": 60000}`. 60 KB is read as 60000 bytes, the stricter of the two readings, so nothing the 61440 reading would refuse gets through. Only an edit to this file raises the budget; the script reads no argv and no environment (AC3).
2. `web/client/scripts/budget.mjs` exports `measure(distDir)` and `check(distDir, budget)`. The roots are the /assets script and stylesheet tags in the built `index.html`. From each JS root it walks the manifest's static `imports` and each chunk's `css`, and it also follows `dynamicImports` except those under `src/screens/admin/`, so a future lazy reader chunk still counts. Fonts (.woff2) and other assets are never counted (docs/07). Each file is gzipped at level 9 with node:zlib.
3. It prints one line per file and a total line: `thread-view bundle: N B gzip / 60000 B budget (P%)`, and exits 1 when the total exceeds the budget.
4. `npm run build` runs it after `vite build`, so `go generate` reports the size on every build. CI gets a named step, `client bundle budget`, after "built client is current", running `npm --prefix web/client run budget`, so a failure names the budget rather than the build diff. Adding a step is what docs/12 "Changing CI" allows.
5. docs/07 gets the byte reading and the pointer to `budget.json`.

### Tests
- Vitest `scripts/budget.test.mjs` (include widened to `scripts/`): a fixture dist in a temp dir with incompressible bytes sized just under and just over the budget; asserts pass/fail, that admin chunks and woff2 are excluded, and that a static import and a non-admin dynamic import are counted. A subprocess run of the script against a fixture asserts exit codes 0 and 1.
- AC3: a test reads the script source and asserts it names neither `process.argv` nor `process.env`.
- AC2: quote the CI step log line in a ticket note.

### Out of scope
- The markdown-parser dependency guard from the pre-groom plan step 6: not an acceptance criterion here; it belongs to C2WWG.

### Settled in grooming (2026-09-24)

- The budget counts the thread view's JavaScript and CSS, gzipped. Fonts are excluded (docs/07).

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:57:03Z

Gap settled (docs/12 "Gaps the design leaves open", a limit read the way docs/07 states it): "60 KB" is read as 60000 bytes, the stricter of 60000 and 61440, so nothing the looser reading would refuse can pass. Written into docs/07 and web/client/README.md. The check fails when the total is strictly over the number (AC1's "exceeds").

What counts: the /assets scripts and stylesheets the built shell names (theme-early, the entry, its CSS), the manifest's static imports, every dynamic import except `src/screens/admin/`, and each chunk's CSS. Fonts are never reached (they sit in a chunk's `assets`), per docs/07 and the grooming decision.

Measured at claim on main e070d97, gzip -9: theme-early 391 B, index JS 31058 B, index CSS 6511 B; total 37960 B of 60000 B (63.3%), 22040 B of headroom.

AC1 evidence: scripts/bundle.test.mjs runs the unmodified `budget.mjs` in a copied tree against a fixture dist; budget 1 exits 1, a large budget exits 0, and `check` passes at exactly the total and fails one byte over. AC3 evidence: the same subprocess passes `--budget=1000000` and three plausible env vars and still exits 1; a source test asserts neither script names argv or process.env. The pre-groom plan's "prove it once in CI with a 1 KiB branch" was replaced by that subprocess test, which runs in CI on every build.

Mutations tried against the tests (all caught after one fix): following admin dynamic imports; dropping static imports; `<` for `<=`; dropping chunk CSS; gzip level 1 for 9 (survived at first because the fixture was incompressible random bytes; the fixture now has a compressible CSS file, and it is caught). A `.js`/`.css` filter over the result survived because nothing else can be reached; it was dead code and was removed.

Left out: the pre-groom plan's markdown-parser dependency guard (step 6). It is no criterion of this ticket and belongs with TKT-01M29C2WWG.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:57:12Z

Correction to the previous note's last line (Title: Return structured mentions, quotes, and reply_to from the render call): TKT-01M29C2WWG is already done, so the markdown-parser guard has no open home. It is not filed as a draft: no open criterion asks for it, and the bundle budget itself would catch a parser large enough to matter.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:04:12Z

AC2 evidence, CI run 585 (PR #105, head 7bbc47a) step log. Step 'built client is current' (npm run build) and step 'client bundle budget' each print: '394 B assets/theme-early-DbsvcKdk.js / 30997 B assets/index-DW5jTwpN.js / 6510 B assets/index-BSGVfO99.css / thread-view bundle: 37901 B gzip / 60000 B budget (63.2%)'. Step 'client tests' ran scripts/bundle.test.mjs (7 tests). The CI figure is 59 B under the local one (37960 B): Alpine's Node links a different zlib, so level-9 output differs by a few bytes between machines. The number that gates is CI's.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:10:40Z

Terva review: clean full review, run 9994b7d1-05bd-4a63-b723-d4bf6fb2a142 (request ready-review-b71a058, Actions run #587, id 13755), head b71a05868c731b8d356d5ddd087c6ecd8c450f59, base c066d918a5cd1110d992729876d98f23aee77c85; terva-review/code success, no findings at any severity. CI on that head: run 586, every step success, 'client bundle budget' among them. This note is a ticket-only commit, carried rather than re-reviewed.

## Summary

The thread view's gzipped JavaScript and CSS are measured on every client build and gated in CI against web/client/budget.json (60000 B, docs/07's 60 KB read strictly; fonts and the lazy admin chunk excluded). scripts/bundle.mjs measures, scripts/budget.mjs reports and exits 1 over budget with no flag or env override; npm run build ends with it and CI runs it as the 'client bundle budget' step. At merge time: 37901 B in CI (63.2%), about 22 KB of headroom. PR #105.
