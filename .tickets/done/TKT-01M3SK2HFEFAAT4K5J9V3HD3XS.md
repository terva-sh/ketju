---
schema: 3
id: TKT-01M3SK2HFEFAAT4K5J9V3HD3XS
title: Restore the registration form after a failed admin-forms e2e test
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:246
    path: null
claim: null
archive: null
created_at: 2026-09-30T16:41:27Z
updated_at: 2026-09-30T20:30:03Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-sk2hfe
  name: ""
extensions: {}
---

## Description

If a test in `web/client/e2e/admin-forms.spec.ts` fails, the registration form it edited is left changed on the e2e server. Later tests and specs on the same server then see that form and fail in turn, which hides the real first failure. The TKT-01M3RZXGBA lane (PR #232) saw this both in main's load run and in a deliberate mutation run. The groomed plan for that ticket kept it out of scope.

### What to do

Restore the form whatever the outcome. Use an `afterAll` that puts back the version the spec started from through the admin API, or seed a separate form the spec owns. Prove it with a run where one admin-forms test is broken on purpose: only that test should fail, not the specs after it.

Reported by the Phase 2 run orchestrator on 2026-09-30.

## Acceptance criteria

- [x] admin-forms.spec.ts afterAll reads GET /api/v1/admin/forms/registration through context.request and, when its schema is not deep-equal to start.schema, posts start.schema to /api/v1/admin/forms/registration/versions with base_version set to the active version it read, expecting 201, all inside a try whose finally closes the context, so a failed request or expectation still closes it and its failure still reaches the report
- [x] The spec makes no /auth/* request, keeps serial mode, the one context from stateFile('ada') and info.project.use, the workers and fullyParallel checks, and its 10 tests with their names and assertions; playwright.config.ts is unchanged
- [x] The header comment says afterAll restores the seeded fields whatever happened, and cites this ticket
- [x] A note records a mutation run of e2e/admin-forms.spec.ts and e2e/apply.spec.ts on both projects with one admin-forms test broken after the save: only that test fails in each project, desktop's 'the corrected form saves as a new version' passes, and apply.spec.ts passes; the break is not committed
- [x] CI's Playwright step passes on the PR, and its step log shows admin-forms' tests passing on both projects

## Implementation plan

Groomed 2026-09-30 against main at 13709f0 (TKT-01M3M9RR26).

What exists at 13709f0:
- `web/client/e2e/admin-forms.spec.ts` has 10 tests under `test.describe.configure({ mode: 'serial' })`, sharing one page. `beforeAll` checks `workers === 1` and `fullyParallel === false`, opens `browser.newContext({ ...info.project.use, storageState: stateFile('ada') })`, and reads `start` (`active_version` and `schema`) from `GET /api/v1/admin/forms/registration`. `afterAll` only closes the context.
- The form goes back to how it started only through the last test, 'the fields it started with are saved back', which removes the added field in the editor, saves version n+2, and checks `after.schema` against `start.schema`. When any earlier test fails, Playwright skips the rest of the file for that project, and the form stays at version n+1 with a `pronouns` field.
- The same server serves both projects, phone and then desktop, and `apply.spec.ts` and `phase2-exit.spec.ts` fill in the registration form after admin-forms. TKT-01M3RZXGBA's mutation note (2026-09-30) records the knock-on. Phone's admin-forms failed after saving version n+1 and skipped the restore. Desktop's admin-forms then failed earlier, at 'the corrected form saves as a new version', on the 5 s `toContainText` for `[data-preview] [data-field="pronouns"]`. The same cause explains the three such failures in main's contention run.
- The API (`internal/web/api/forms.go`, `internal/service/forms/forms.go`):
  - `GET /api/v1/admin/forms/{key}` answers `dto.FormAdmin` with `active_version` and the canonical `schema`.
  - `POST /api/v1/admin/forms/{key}/versions` takes `{base_version, schema}`. It saves the next version, makes it active, and writes a `form.version_created` audit row in one transaction. It answers 201. A `base_version` that is not the active version is `conflict` (`ErrStaleVersion`).
  - No route creates a form. The only form is `registration`, which migration 0011 seeds (`formsvc.Registration`), and `FormEditor.tsx` is fixed to `/api/v1/admin/forms/registration`.
- The precedent is `web/client/e2e/admin-settings.spec.ts`. Its `afterAll` puts the site name back with `page.request.put(nameUrl, …)` "whatever happened, so the next project starts from it", and expects 200.

### Settled in grooming

- **#232's structure stays** (TKT-01M3RZXGBA, PR #232, its Settled items and summary). The file keeps serial mode, one context and page made in `beforeAll` from `stateFile('ada')` and `info.project.use`, and one scan per test. It keeps the `workers` and `fullyParallel` checks in `beforeAll`. The tests, their names, their order, their assertions and their timeouts stay as they are, and `playwright.config.ts` is not changed. The `TKT-01M3MTQG5A` timeout decisions hold.
- **Stored sessions only.** The spec starts from ada's stored session and makes no `/auth/*` request, so it spends nothing from the run's `/auth/*` budget (`e2e/session.ts`). The restore uses the same context's cookies.
- **This ticket reverses TKT-01M3RZXGBA's Out of scope item** ('Restoring the registration form when an admin-forms test fails partway … no failure has needed it'). The knock-on failures in that ticket's contention and mutation runs are the failure that now needs it.

Engineering decision taken here, with the alternative that lost:
- **An `afterAll` that restores the starting version through the admin API.** `afterAll` reads the form. If its `schema` is not deep-equal to `start.schema`, it saves `start.schema` as a new version, based on the version now active, and expects 201. It closes the context in a `finally`, so a failed restore still closes it.
  - The alternative that lost is **a form the spec owns**, seeded separately. Nothing can create a second form today. It would need a seed in `cmd/ketju/e2e.go` or a migration for a new form key, and a route or parameter so that `FormEditor.tsx`, which is fixed to `registration`, could open another key. Both are product surface added only for a test. The spec would also stop exercising the form that admins actually edit and that `apply.spec.ts` fills in. It would still share one server between the two projects, so each project would need its own form, or the phone run would again leave the desktop run a changed form.
  - The `afterAll` restore fits what already exists: the route admins use, the pattern `admin-settings.spec.ts` already follows, and a check that does nothing when the last test already restored the form.

### Approach

1. `web/client/e2e/admin-forms.spec.ts`, `test.afterAll`:
   - When `context` and `start` are both set (so `beforeAll` got as far as reading the form), `GET formUrl` with `context.request` and expect 200.
   - If `isDeepStrictEqual(now.schema, start.schema)` (from `node:util`) is false, `POST ${formUrl}/versions` with `{ base_version: now.active_version, schema: start.schema }` and expect 201. Then read the form again and expect its `schema` to equal `start.schema`.
   - The read, the POST and the checks run inside `try`, and `await context?.close()` runs in `finally`. A failed request or expectation then still closes the context, and the failure is not swallowed: `finally` does not catch, so it still fails the hook in the report (review 1608, PR #242).

   `context.request` rather than `page.request`, because it shares the context's cookies and still works if the page crashed or was closed by the failing test.
2. The restore bases its save on the version active now, read in step 1, not on `n` or `n + 1`. A failure can come before the save, after it, or between the save and its notice, so only the server knows which version is active.
3. The restore adds a version rather than rolling one back. The API has no rollback, and every version stays in the list. The tests already read `n` from `start.active_version` and count rows as `n + 1`. Every save, the restore included, makes the new highest version active, so the number of versions still equals `active_version` for the next project and the next run.
4. The header comment (`:13-17`) changes from "each run ends by saving the fields it started with back as the newest version" to say that the last test does this in the editor, and that `afterAll` does it through the API when any earlier test failed, whatever happened, so the next project and `apply.spec.ts` start from the seeded fields. Cite this ticket.
5. The last test, 'the fields it started with are saved back', stays. It checks the editor's own restore, and when it passes the `afterAll` check finds nothing to do.

### Tests

- **Clean run.** The full Playwright suite passes on both projects with the change. In the clean run the `afterAll` makes no POST, because the last test restored the form. Show this with the server's version count: the phone run leaves version n+2, and the desktop run's `start.active_version` is that same number, with no extra version from `afterAll` in between.
- **Mutation check.** Break one admin-forms test on purpose, after the save and before the last test. For example, make 'version 1 opens read-only' expect a heading that is not there. Then run `npx playwright test e2e/admin-forms.spec.ts e2e/apply.spec.ts` for both projects, with no load. Expected:
  - Only the broken test fails in each project, and the rest of admin-forms for that project is reported as skipped.
  - Desktop's admin-forms reaches the broken test. In particular, 'the corrected form saves as a new version' passes on desktop after phone's failure.
  - `apply.spec.ts` passes on both projects.

  Also run `--project=desktop` alone with the same break, which should give exactly one failure. Revert the break, and never commit it. A note records the command, the break, and each project's result.
- **Before the change, for comparison.** The same mutation run on main shows desktop failing at 'the corrected form saves as a new version', as TKT-01M3RZXGBA's note recorded. Cite that note rather than repeating the run if the numbers are unchanged.
- CI's Playwright step (`ci / Build and Test (pull_request)`) passes on the PR, and its step log shows admin-forms' 10 tests passing on both projects.

### Out of scope

- A second form, a form-creation route, or a key parameter for `FormEditor.tsx`: the alternative that lost, above.
- Restoring state in other specs. `admin-settings.spec.ts` already restores in `afterAll`. If another spec is found to leave shared state behind after a failure, file it as its own ticket.
- `playwright.config.ts`, timeouts, retries and the CI image.

## Notes

**agent:claude-code/phase2-run-sk2hfe** at 2026-09-30T20:06:44Z

Claimed against main at 387c401. Nothing under web/client/e2e, internal/web/api/forms.go, internal/service/forms or web/client/playwright.config.ts changed between the grooming base 13709f0 and 387c401 (`git log 13709f0..origin/main -- …` is empty), so the groomed plan stands as written and was not replaced; its `### Settled in grooming` items are untouched.

Built to it in 9d10fdf: `afterAll` reads the form with `context.request.get(formUrl)` (expects 200) when `context` and `start` are both set; if `isDeepStrictEqual(now.schema, start.schema)` (node:util) is false it POSTs `{ base_version: now.active_version, schema: start.schema }` to `${formUrl}/versions`, expects 201, reads again and expects `schema` to equal `start.schema`. All inside `try`; `await context?.close()` in `finally` (review 1608). The header comment now says the last test restores in the editor and afterAll restores through the API whatever happened, citing this ticket. The spec makes no /auth/* request; serial mode, the one context from stateFile('ada') and info.project.use, the workers/fullyParallel checks and the 10 tests are unchanged; playwright.config.ts is unchanged.

Dist: `go generate ./internal/web/spa` after the change leaves internal/web/spa/dist unchanged (`git add -N` then `git diff --stat` empty); reader bundle 56988 B / 70000 B.

**agent:claude-code/phase2-run-sk2hfe** at 2026-09-30T20:06:45Z

Clean run and version counts. Full suite, both projects, at 9d10fdf on port 18941 (`KETJU_E2E_PORT=18941 npx playwright test` from web/client): 175 passed, 11 skipped (the existing per-width skips in focus, shortcuts, targets and thread), exit 0. All 20 admin-forms tests passed, 10 per project.

To show afterAll makes no POST on a clean run, a temporary probe (console.log of `start.active_version` in beforeAll, and of the active version and the deep-equal result in afterAll; never committed) was run on `e2e/admin-forms.spec.ts e2e/apply.spec.ts`, both projects: 28 passed. Phone started at version 1 and its afterAll saw version 3 with equal=true; desktop started at version 3, the number phone left, and its afterAll saw version 5 with equal=true. No restore version was added in between.

**agent:claude-code/phase2-run-sk2hfe** at 2026-09-30T20:06:47Z

Mutation run (criterion 4). The break, never committed: in 'version 1 opens read-only, and passes the scan in the light theme' (after the save, before the last test) the heading name `'Version 1, read-only'` was changed to `'Version 1, not there'`, so its `toBeFocused()` times out. The same temporary probe logging as the clean run was in place. Reverted afterwards by restoring the committed file; `git status` clean.

- Both projects, no load: `KETJU_E2E_PORT=18941 npx playwright test e2e/admin-forms.spec.ts e2e/apply.spec.ts` gave 2 failed, 4 did not run, 22 passed. The only failures were phone's and desktop's 'version 1 opens read-only, and passes the scan in the light theme' (`Error: expect(locator).toBeFocused() failed`, element not found). In each project the two tests after it ('version 1, read-only passes the scan in the dark theme', 'the fields it started with are saved back') did not run. Phone's afterAll found version 2 with equal=false and restored the starting fields as version 3. Desktop started at version 3 and passed 'the corrected form saves as a new version, and passes the scan in the light theme' before reaching the broken test; its afterAll restored version 4 to version 5. apply.spec.ts passed both its tests on both projects.
- Desktop alone (`--project=desktop`, same files, same break): 1 failed (the broken test), 2 did not run, 13 passed; afterAll restored version 2 as version 3.
- Hook failure path, desktop alone, admin-forms only, with the break and afterAll's `toBe(201)` also changed to `toBe(299)`: the run finished (exit 1; 1 failed, 2 did not run, 11 passed) and the report carried both the test's error and the hook's `Error: save the starting fields back / Expected: 299 / Received: 201`. So a failed restore expectation still reaches the report, and finally still closed the context (the run did not hang).

Before the change, for comparison: the same shape of mutation on main is TKT-01M3RZXGBA note 4 (2026-09-30), where desktop admin-forms failed earlier, at 'the corrected form saves as a new version', on the 5 s toContainText for `[data-preview] [data-field="pronouns"]`. Not re-run, as the plan allows.

**agent:claude-code/phase2-run-sk2hfe** at 2026-09-30T20:30:02Z

CI and review on PR #246, head ba2f282, base d28aead. `ci / Build and Test (pull_request)`: success, run 1604, job 0. Every step succeeded; the step "Playwright smoke at 390 px and 1440 px" (7m32s) logged 175 passed, 11 skipped, and all ten admin-forms tests passed on phone (29-38) and on desktop (120-129), 'the fields it started with are saved back' included. Criterion 5 ticked.

Terva review: clean full review, run 1605 (run id 2b01ee21-86e6-45e1-b861-5f8dcc14d853, request ready-review-ba2f282), `terva-review/code` success on ba2f282, no findings ("I found no concrete correctness bug"). Main moved from 387c401 to d28aead (#245, TKT-01M3QS0TMV) after the branch; it touches no file this PR does, and its new link-sign-in.spec.ts does not touch the form.

## Summary

`web/client/e2e/admin-forms.spec.ts` now restores the registration form in `afterAll` whatever happened. It reads the form through `context.request` and, when the schema differs from the one the run started from, saves the starting schema back as a new version based on the version active now (201), then checks it. The read, the POST and the checks run in a `try`, and the context closes in `finally` (review 1608). When the last test passed there is nothing to restore, so a clean run adds no version: phone leaves version 3 and desktop starts from it.

A deliberate break after the save ('version 1 opens read-only') now fails only that test in each project. Desktop's 'the corrected form saves as a new version' passes after phone's failure, and apply.spec.ts passes on both projects. Before this change, desktop failed earlier, at that save (TKT-01M3RZXGBA note 4). A broken restore still fails the hook in the report. The notes of 2026-10-01 give the commands and results. The break was never committed.

#232's serial, shared-page structure, the stored session, the ten tests and playwright.config.ts are unchanged. The client dist is unchanged. Criterion 5 (CI's Playwright step on the PR) is ticked in a ticket-only commit once CI has run on the PR.
