---
schema: 3
id: TKT-01M29CP2NGWH32C6M718QHB30X
title: Build the apply and status screens
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
  - TKT-01M29CP2KZPR0880MGFV3CPXR4
blocks_on: none
references:
  - ref: design:forms
    path: docs/05-identity-and-accounts.md
  - ref: pr:158
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T19:10:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2ng
  name: ""
extensions: {}
---

## Description

The client rendering the registration form from its stored schema, plus the status page an applicant returns to.

This screen is a stranger's first impression of the community, seen while they are being judged. It should look like the rest of the site rather than like an admin tool, and it has to work at 390 px because most people will meet it on a phone.

## Acceptance criteria

- [x] The apply screen draws its fields only from GET /apply/form: a test renders two different schemas and the inputs follow, and field types are named only in src/forms's one type-to-component map; an unknown type shows a notice and disables submit
- [x] A 422 body's errors attach to their fields (aria-invalid, aria-describedby, visible text) and focus moves to the first, tested from the golden problem body
- [x] A locked prefilled field renders read-only with its claim value and a visible lock label, and is left out of the POST /apply body, tested
- [x] A pre-user viewer on any other client route is sent to /apply/status under a reduced shell, and the status screen renders none, pending (with withdraw), approved (with a full-load sign-in link), denied and withdrawn
- [x] e2e/apply.spec.ts opens both screens at 390 px and 1440 px with no horizontal overflow and no axe violations, and submits the seeded form
- [x] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. Revised at claim on 2026-09-29 against main at 3fd598f, by agent:claude-code/phase2-run-cp2ng.

What exists at 3fd598f:
- The four `/api/v1/apply` routes exist and are on `web.PreUserRoutes` (TKT-01M29CP2KZ).
- `src/shell/ApplicantShell.tsx` draws the reduced shell for a pre-user viewer and renders the route table's `/apply` or `/apply/status` entry, both still placeholders. It does not change the address.
- `Form`, `FormField`, `ApplicationForm` and `ApplicationStatus` are typed in `src/api/dto.ts`, with goldens `apply-form.pre_user`, `apply-status-*.pre_user`.
- `validation-failed` bodies carry `errors: [{field, code}]` (`internal/problem/testdata/field-errors.json`). `src/api/problem.ts` does not read them yet.
- The reader bundle is 38673 of 60000 B gzip.

### Settled in grooming

- **The form renders from its stored schema** (docs/05 "Forms"; this ticket's description). The client draws the fields `GET /api/v1/apply/form` returns and adds none of its own.
- **The status page is where a returning applicant lands** (docs/05 step 4). It is also where the applicant learns the decision, because email is out of v1 (docs/10 item 12, TKT-01M29CP2N0).
- **The screens look like the rest of the site rather than an admin tool, and work at 390 px** (this ticket's description; docs/07 "Responsive"; the shell's 720 px breakpoint from TKT-01M29CEE4K).
- **A null user stays the only signed-out signal.** An applicant is told apart by `me.pre_user` (dto.Me, docs/07 `GET /me`).
- **Remote images are links** (docs/07, decided 2026-09-28). The notice HTML arrives rendered by `internal/render`, so the client adds nothing for it.
- **The built client dist is committed.** Run `go generate ./internal/web/spa` after any client change. The thread-view budget of 60000 B gzip holds.

Engineering decisions taken here, with the alternative that lost:
- **The renderer is a shared module, `src/forms/`**: `FormView.tsx`, `fields.tsx` and `messages.ts`. The editor preview (TKT-01M29CP2P1) and the review screen (TKT-01M3NRX2NN) import it from the admin chunk. `fields.tsx` holds the single map from field type to component, and it is the only place the client names a field type. A renderer inside `screens/apply` lost, because the admin screens would copy it and the two would drift.
- **A multiple select renders as a group of checkboxes** and a single select as a native `<select>`. A multi-`<select>` is close to unusable on a phone.
- **An unknown field type renders a visible notice and disables Submit.** It never renders nothing. The server already fails loudly on one (TKT-01M29CP2KE), so this is the client's half of the same rule.
- **`/apply` and `/apply/status` are one lazy chunk, `src/screens/apply/`,** so a member's first paint never downloads them. They still count toward the budget, which is correct: `bundle.mjs` exempts only the admin screens.
- **A pre-user viewer is kept inside the application flow by the client too.** Any other client route replaces itself with `/apply/status`. The server's allowlist (TKT-01M29CKRG9) is the enforcement; this redirect only avoids showing a shell whose every request would be refused. For a pre-user viewer the shell draws the site name and Sign out, with no tabs, badges or search.

### Superseded by the maintainer, 2026-09-29

The "approved" status state (criterion 4, and the approved line of the old step 4) is superseded by the maintainer's decision on TKT-01M3Q2V25E, carried in this ticket's note of 2026-09-29: an approved applicant's pre-user session retires, so the status screen never receives `approved` in practice. The signed-out shell carries the hint "Applied? Sign in to see your decision" instead. The status screen still renders `approved` with its `sign_in` link defensively, because `dto.ApplicationStatus` keeps the value, and no test or criterion depends on reaching it.

### Approach

1. `src/api/problem.ts`: `ProblemError` gains `fieldErrors: Record<string, string>` (field to code, first code per field wins), read from a `validation-failed` body's `errors` extension. Entries that are not `{field: string, code: string}` are dropped.
2. `src/forms/messages.ts`: words for every code in docs/07's form-codes table. `fieldMessage(code, field)` falls back to the problem table's `validation-failed` text for a code it does not know.
3. `src/forms/fields.tsx`: the one map from field type to a `FieldKind`: whether the type takes an answer, its empty value, the control that draws it (label included), and how read mode shows an answer. No other file names a field type.
4. `src/forms/FormView.tsx`: props `form`, `prefill`, `values`, `errors`, `mode: 'fill' | 'preview' | 'read'`, `answers` (read mode), `provider`, `onChange`, `onSubmit`, `busy`. It renders `intro_html` and each field through `fields.tsx`, with `aria-invalid` and `aria-describedby` on each control pointing at its visible error text. When `errors` changes and is not empty, focus moves to the first invalid control in schema order. A locked field is read-only, holds its prefill, and carries a lock icon and "From your {provider} account". An unknown type is a visible notice and Submit is disabled. `answersOf(form, values)` builds the POST body's answers: every answering, unlocked field, and never a locked or display-only one. `initialValues(form, prefill)` seeds the values.
5. `src/screens/apply/ApplyScreen.tsx` (`/apply`): loads `GET /api/v1/apply/form`, submits `POST /api/v1/apply` with `{version, answers}`.
   - 201: replace to `/apply/status`.
   - 409: read the status. Pending goes to `/apply/status`. Otherwise the form changed: reload it, keep the values whose keys still exist, and say so.
   - 422: attach the field errors; a field error for no drawn field shows as a form-level message.
   - 403: "Applications are not being accepted right now", with no reason (a blocked identity is told nothing more, docs/07).
   - 429: when applying is possible again, from `Retry-After`.
   - The form load's refusals read the same way (409 goes to the status page).
6. `src/screens/apply/StatusScreen.tsx` (`/apply/status`): loads `GET /api/v1/apply/status` and renders none (Apply link, or when applying opens, or not accepting), pending ("Your application is being reviewed", with Withdraw behind an inline confirmation that posts `POST /api/v1/apply/withdraw` and shows the answer), approved (defensive: a full-load link to `sign_in`), denied and withdrawn (the date, then Apply again when `can_apply`, or the date from `can_apply_at`). A 401 means the session ended, which is what approval does: it says "Sign in to see your decision" with a full-load link to `/auth/signin`.
7. `src/routes.tsx`: `/apply` and `/apply/status` become two `lazy()` views of one module, `src/screens/apply/ApplyRoot.tsx`, so one chunk.
8. `src/shell/ApplicantShell.tsx`: any path but `/apply` and `/apply/status` is replaced with `/apply/status`, and focus moves to the new screen's heading on each change of path, as the Router's `routeFocuser` does for the member shell.
9. `src/shell/Shell.tsx`: for a signed-out viewer (null user, no `pre_user`) a one-line hint under the top bar, "Applied? Sign in to see your decision", linking to the same `/auth/signin?return_to=` as Sign in. The client does not know `registration.mode` (it is not in `/me`), so the hint shows to every signed-out viewer; the sentence reads correctly in either mode.
10. Styles in `src/forms/forms.css` and `src/screens/apply/apply.css`, from `src/styles/tokens.css`: at most 40 rem wide on desktop, full width with 16 px gutters under 720 px, 44 px targets on the phone.
11. e2e: `cmd/ketju/e2e.go` adds two applicant subjects to the fake provider's users with no account, one per Playwright project, so each project submits once. `registration.mode` is `approval` by default.
12. Docs: docs/07's pre-user paragraph says the client replaces the address with `/apply/status`.
13. Build: `npm run build`, `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, record the budget figure in a note.

### Tests

- `src/api/problem.test.ts`: the golden field-errors body parses into `fieldErrors`; malformed entries are dropped; other types carry none.
- `src/forms/messages.test.ts`: every code in docs/07's form-codes table has words, and the table names no other; an unknown code falls back.
- `src/forms/FormView.test.tsx`: renders every type; two schemas render different inputs; an unknown type shows a notice and disables submit; a locked field is read-only, labelled, holds the prefill and is absent from the answers; the golden 422 body attaches errors (aria-invalid, describedby, visible text) and focuses the first; read mode shows answers; no file outside `fields.tsx` names a field type.
- `src/screens/apply/ApplyScreen.test.tsx`: submit posts `{version, answers}` without the locked field and lands on status; 409 pending goes to status; 409 form-changed reloads and keeps values; 422; 403; 429 with Retry-After.
- `src/screens/apply/StatusScreen.test.tsx`: none, pending, approved, denied, withdrawn from goldens; withdraw asks first and posts; 401 offers sign-in.
- `src/routes.test.tsx`: a pre-user viewer at `/f/general` ends at `/apply/status`; a signed-out viewer sees the hint, and a member and a pre-user do not.
- `e2e/apply.spec.ts`: a pre-user session through the fake provider. Both screens at 390 px and 1440 px with no horizontal overflow and no axe violations (WCAG 2.1 A and AA, both themes); submits the seeded form and lands on pending; a member route replaces itself with `/apply/status`.
- `npm run budget` passes, `TestTheAdminBundleIsSeparate` passes.

### Out of scope

- The pre-user session, its allowlist, and the callback redirect: TKT-01M29CKRG9.
- The API: TKT-01M29CP2KZ. The schema and the `errors` extension: TKT-01M29CP2KE.
- The editor preview and the review screen, which reuse `src/forms`: TKT-01M29CP2P1 and TKT-01M3NRX2NN.
- A message to a denied applicant beyond the status. The maintainer decided on 2026-09-29 that the applicant sees the status only, never the moderator's note (TKT-01M29CP2MG), so the status screen shows none.

## Notes

**agent:claude-code/phase2-run-cp2kz** at 2026-09-29T18:18:21Z

2026-09-29, superseding the "approved" status state (criterion 3 and plan step 4's "approved: a Sign in link to `sign_in`"). Maintainer decision on TKT-01M3Q2V25E (Decide what an approved applicant sees once their pre-user session ends): an approved applicant's pre-user session retires. Once approval links the identity, `LookupSession` stops resolving it and the session middleware clears the cookie, so `/me` turns anonymous and `GET /api/v1/apply/status` answers 401. In practice the status screen never receives `approved`.

What this ticket builds instead: the signed-out shell carries a hint like "Applied? Sign in to see your decision". Signing in gives the applicant a member session, where the approval notification waits (TKT-01M29CP2N0). The status screen can still render `approved` with `sign_in` defensively, since `dto.ApplicationStatus` keeps the value, but no test or criterion should depend on reaching it. The other states (none, pending with withdraw, denied, withdrawn) are unchanged. Recorded by TKT-01M29CP2KZ's agent, which carried the decision.

**agent:claude-code/phase2-run-cp2ng** at 2026-09-29T19:05:10Z

Built on run/cp2ng-apply-screens from main at 3fd598f. Facts the next ticket needs:

- **Budget.** `npm run budget`: 44811 B gzip of 60000 (74.7%), up from 38673. The apply chunk is 4692 B JS and 1140 B CSS gzip, and it counts, as planned. The entry grew by the field-error parsing and the signed-out hint.
- **`src/forms` is ready for the admin screens.** `FormView` takes `mode: 'fill' | 'preview' | 'read'`; `initialValues`, `answersOf` and `unknownFields` are exported. `fieldMessage` has words for every code in docs/07's form-codes table, schema codes included, so the form editor (TKT-01M29CP2P1) can show a save's field errors with it. `messages.test.ts` holds the table to the document in both directions. Importing `FormView` from the admin chunk puts `forms.css` in that chunk.
- **`ProblemError.fieldErrors`** is a `Record<string, string>` from field to code, read only on `validation-failed`.
- **The signed-out hint** (maintainer decision, TKT-01M3Q2V25E) is shown to every signed-out viewer, at both widths, as one line under the top bar linking to the same `/auth/signin?return_to=` as Sign in. `/me` does not carry `registration.mode`, so the client cannot show it only in approval mode; the sentence reads correctly in any mode. Adding the mode to `/me` would be a server change for a line of text, and no ticket asks for it.
- **The e2e fixture** has two applicant subjects with no account, `dana` and `erin` (`cmd/ketju/e2e.go` `seedApplicants`), one per Playwright project, since an identity may submit once a day. A first sign-in lands on `/apply`, not `/apply/status`: the callback sends an applicant with no application to the form. The fake provider sends no name or email claim, so the locked email field is empty in e2e; the unit tests cover a prefilled lock.
- **Test helpers.** `src/testing.tsx` gained `stubServer`, `problemAnswer` and `unmount`. A test that restores the viewer in `afterEach` must `unmount()` first, or the app renders again for the restored viewer and fetches after the stub is gone (happy-dom then prints an AbortError at teardown).
- **Docs.** docs/07's pre-user paragraph now says the client replaces the address with `/apply/status`.

**agent:claude-code/phase2-run-cp2ng** at 2026-09-29T19:05:15Z

Criterion 4, as the note of 2026-09-29 supersedes it: approved is drawn, with its sign_in link as a plain anchor to /auth/..., which is outside clientScope and so a full page load (StatusScreen.test.tsx 'draws approved with its sign-in link'). Nothing depends on reaching it: approval retires the pre-user session, so the status route answers 401, which the screen draws as 'If you applied to join, sign in to see your decision', and the signed-out shell carries the hint. The other four states, and the pre-user redirect under the reduced shell, are tested in StatusScreen.test.tsx, routes.test.tsx and e2e/apply.spec.ts.

**agent:claude-code/phase2-run-cp2ng** at 2026-09-29T19:10:33Z

Terva review 1453 (run 6c8c21ce-1a88-420e-91ed-4232efe2ad4c, Actions run 984) of head 15009d064b2d7e0339c1a373dcf1e524fb1edbc7 on base 3fd598f8245f03a25774b2c22ccaf516d412a5fc: two medium findings, both fixed, neither disputed. (1) 'Identify a pending application by status, not withdrawal permission': a 409 on submit now goes to the status page when the status is pending or can_apply is false (approved, denied or withdrawn within the day, blocked, closed), and reloads the form only when the applicant may still apply, since the server checks every other refusal before the version. (2) 'Do not preserve answers when a field changes type': ApplyScreen's exported carried() keeps an answer across a form change only when the field keeps its kind in fields.tsx's map and its multiple flag, and every chosen option is still offered. Tested in ApplyScreen.test.tsx. Also, found in my own read of the diff: a member opening /apply is refused 403 and was told registration is closed; they are now told they are already a member. Budget after the fix: 44979 B gzip of 60000.

## Summary

The apply and status screens are built. /apply draws the registration form from GET /api/v1/apply/form through a shared renderer, src/forms (FormView, the one type map in fields.tsx, field error words in messages.ts), and handles every refusal the API gives. /apply/status draws none, pending with a confirmed withdraw, approved defensively, denied and withdrawn from the server's can_apply, can_apply_at and can_withdraw, and a 401 as 'sign in to see your decision'. A pre-user viewer's other addresses are replaced with /apply/status under the reduced shell, and the signed-out shell carries 'Applied? Sign in to see your decision' (TKT-01M3Q2V25E). Both screens are one lazy chunk; the reader budget is 44811 of 60000 B gzip. e2e/apply.spec.ts signs in a seeded applicant per project, scans both screens with axe in both themes at 390 and 1440 px, and submits the form.
