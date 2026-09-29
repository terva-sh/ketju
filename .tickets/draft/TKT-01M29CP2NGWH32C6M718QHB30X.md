---
schema: 3
id: TKT-01M29CP2NGWH32C6M718QHB30X
title: Build the apply and status screens
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T05:38:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The client rendering the registration form from its stored schema, plus the status page an applicant returns to.

This screen is a stranger's first impression of the community, seen while they are being judged. It should look like the rest of the site rather than like an admin tool, and it has to work at 390 px because most people will meet it on a phone.

## Acceptance criteria

- [ ] The apply screen draws its fields only from GET /apply/form: a test renders two different schemas and the inputs follow, and field types are named only in src/forms's one type-to-component map; an unknown type shows a notice and disables submit
- [ ] A 422 body's errors attach to their fields (aria-invalid, aria-describedby, visible text) and focus moves to the first, tested from the golden problem body
- [ ] A locked prefilled field renders read-only with its claim value and a visible lock label, and is left out of the POST /apply body, tested
- [ ] A pre-user viewer on any other client route is sent to /apply/status under a reduced shell, and the status screen renders none, pending (with withdraw), approved (with a full-load sign-in link), denied and withdrawn
- [ ] e2e/apply.spec.ts opens both screens at 390 px and 1440 px with no horizontal overflow and no axe violations, and submits the seeded form
- [ ] The dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a.

What exists there:
- `/apply` and `/apply/status` are placeholder routes in `web/client/src/routes.tsx`.
- `Me.pre_user` is typed in `src/api/dto.ts`, with a `me.pre_user` golden.
- `src/api/problem.ts` turns a problem's type into words, and has no field-level errors yet.
- The thread-view bundle measures 38171 B gzip against the 60000 B budget in `web/client/budget.json`. `bundle.mjs` counts every chunk except `src/screens/admin/`, so these screens count even when lazy.

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

### Approach

1. `src/api/problem.ts`: parse the `errors` extension from TKT-01M29CP2KE into `fieldErrors: Record<string, string>` on the thrown problem. `src/forms/messages.ts` maps each code in docs/07's list to words. A code the client does not know falls back to the problem's own message.
2. `src/forms/FormView.tsx`:
   - Props: `form: Form`, `prefill`, `values`, `errors`, `mode: 'fill' | 'preview' | 'read'`, `onChange`, `onSubmit`, and `answers` for read mode.
   - It renders `intro_html` and each field through `fields.tsx`.
   - A locked field is a read-only input holding its prefill value, with a lock icon and "From your {provider} account". It is left out of the submitted `answers`.
   - Every input has a `<label>`, plus `aria-invalid` and `aria-describedby` pointing at its error text. After a failed submit, focus moves to the first invalid field.
   - `markdown_notice` HTML is set with `dangerouslySetInnerHTML`, as post HTML is, because the server sanitized it.
3. `src/screens/apply/ApplyScreen.tsx` (`/apply`):
   - It loads `GET /apply/form` and submits `POST /apply` with `{version, answers}`. A 201 navigates to `/apply/status`.
   - 409: a pending application goes to `/apply/status`. The form-changed case reloads the form and keeps the values whose keys still exist.
   - 422 attaches the field errors.
   - 403 shows the fixed not-accepting message.
   - 429 says when applying is possible again, from `Retry-After`.
4. `src/screens/apply/StatusScreen.tsx` (`/apply/status`) loads `GET /apply/status` and renders:
   - none: a link to `/apply`.
   - pending: "Your application is being reviewed", with a Withdraw button behind a confirmation that posts `POST /apply/withdraw`.
   - approved: a Sign in link to `sign_in`, as a full page load, since `/auth/*` is server-owned (`clientScope` in `routes.tsx`).
   - denied and withdrawn: the date, plus an Apply again link when `can_apply` is true, or the date from `can_apply_at` when it is not.
5. `src/routes.tsx`: replace both placeholders with a `lazy(() => import('./screens/apply/ApplyRoot'))`. `app.tsx` or `shell/Shell.tsx` redirects a pre-user viewer, and draws the reduced shell for one. If TKT-01M29CKRG9 already landed a minimal status page or redirect, this ticket replaces it.
6. Styles go in `src/screens/apply/apply.css` and use the tokens in `src/styles/tokens.css`. At most 40 rem wide on desktop, and full width with 16 px gutters under 720 px.
7. Build: `npm run build`, then `go generate ./internal/web/spa`, then commit `internal/web/spa/dist`. Record the `npm run budget` figure in a note.

### Tests

- `src/forms/FormView.test.tsx`:
  - `renders every field type` from a fixture with all six.
  - `follows the schema`: two different schemas render different inputs, with no field hard-coded.
  - `an unknown type shows a notice and disables submit`.
  - `a locked field is read-only, labelled, and absent from the submitted answers`.
  - `field errors attach` from the golden 422 body: aria-invalid, describedby, visible text, and focus on the first.
  - `read mode shows answers`.
- `src/api/problem.test.ts`: `errors` parse into `fieldErrors`, and an unknown code falls back.
- `src/screens/apply/ApplyScreen.test.tsx`: submit leads to status; 409 leads to status; the form-changed reload; 403; and 429 with Retry-After.
- `src/screens/apply/StatusScreen.test.tsx`: each status. Withdraw asks for confirmation and then posts. The approved state links to `sign_in` as a full load.
- `src/shell/Shell.test.tsx`: a pre-user viewer on `/` is replaced to `/apply/status`, and the reduced shell draws no tabs or badges.
- `routes.test.tsx` still diffs the route table against docs/07.
- `e2e/apply.spec.ts`: a pre-user session through the fake IdP in `e2e/session.ts` with `registration.mode` approval. Both screens load at 390 px and 1440 px with `scrollWidth <= clientWidth`, and axe reports no violations. It submits the seeded form and lands on pending.
- `internal/web/spa` `TestTheAdminBundleIsSeparate` still passes, and `npm run budget` passes.

### Out of scope

- The pre-user session, its allowlist, and the callback redirect: TKT-01M29CKRG9.
- The API: TKT-01M29CP2KZ. The schema and the `errors` extension: TKT-01M29CP2KE.
- The editor preview and the review screen, which reuse `src/forms`: TKT-01M29CP2P1 and TKT-01M3NRX2NN.
- A message to a denied applicant beyond the status. The maintainer decided on 2026-09-29 that the applicant sees the status only, never the moderator's note (TKT-01M29CP2MG), so the status screen shows none.
