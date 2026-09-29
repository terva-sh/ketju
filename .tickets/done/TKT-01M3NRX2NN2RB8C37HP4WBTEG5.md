---
schema: 3
id: TKT-01M3NRX2NN2RB8C37HP4WBTEG5
title: Build the admin applications review screen
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
  - TKT-01M29CP2NGWH32C6M718QHB30X
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: pr:162
    path: null
claim: null
archive: null
created_at: 2026-09-29T05:06:22Z
updated_at: 2026-09-29T21:35:02Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-nrx2nn
  name: ""
extensions: {}
---

## Description

The client screen at `/admin/applications` (docs/07 URL scheme), which no ticket owned. TKT-01M29CP2MG builds the review API and TKT-01M29CRJ4 builds the unified queue. Without this screen, a moderator has no page to open an application, read its answers, and approve or deny it.

This is what makes the epic's exit criterion usable: a club can open registration to strangers only if a moderator can review them in the browser.

It lives under `web/client/src/screens/admin/`, so it stays in the lazy admin chunk that `TestTheAdminBundleIsSeparate` and `bundle.mjs` keep out of the reader's bundle. The answers render through the read-only mode of the shared form renderer in `web/client/src/forms/`, which TKT-01M29CP2NG builds. The same component the applicant filled in therefore shows the moderator what was answered, against the version it was answered on.

docs/10 keeps SMTP out of v1, so the screen states that notifications are in-app only and that applicants learn the outcome on their status page (TKT-01M29CP2N0's description asks for this to be stated in the admin UI rather than discovered).

Remember: the built client dist is committed. Run `go generate ./internal/web/spa` after any client change, and keep `npm run budget` within 60000 B gzip.

## Acceptance criteria

- [x] /admin/applications lists applications newest first from GET /api/v1/admin/applications, pending by default with a status filter, and pages with next_cursor
- [x] Opening an application shows its answers through the shared src/forms renderer in read-only mode, against the form version it was answered on, with the identity's provider, email and verified flag, its prior applications and the moderators' notes
- [x] A moderator can add a note, approve with the suggested handle prefilled and editable, and deny with a required note and a 'block this subject' checkbox
- [x] A 409 from approve or deny re-fetches the application and says who decided it; a handle error from the errors extension attaches to the handle field
- [x] The screen states that notifications are in-app only and that applicants learn the outcome on their status page
- [x] The screen lives under src/screens/admin so TestTheAdminBundleIsSeparate passes, works at 390 px and 1440 px, the dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
- [x] A blocked applicant's identity shows as blocked with an Unblock control for moderators and admins, calling POST /api/v1/admin/applications/{id}/unblock (maintainer decision 2026-09-29)
- [x] The deny note field says the applicant never sees it, and a moderator on /admin/applications?id=N can reach every action with the keyboard (Decision.test.tsx)
- [x] e2e/admin-applications.spec.ts: a seeded moderator approves a seeded pending application at 390 px and 1440 px, and axe reports no violations on the list or the detail

## Implementation plan

Revised at claim, 2026-09-29, against main at 855ca5d. The groomed plan below stands, with its `### Settled in grooming` unchanged; these are the corrections main forces:

- Step 1 is done: TKT-01M29CP2MG added `AdminApplicationList`, `AdminApplicationRow`, `AdminApplication`, `ApplicantIdentity`, `ApplicationNote` and `ApplicationDecision` to `dto.ts`, with goldens `admin-application*.moderator.json`. The tests use those goldens.
- The handle codes are `invalid_handle`, `reserved` and `taken` (docs/07 "Applications"), not forms.Codes, so `Decision.tsx` carries its own sentence for each. The deny note's codes (`required`, `too_long`, `invalid_text`) are forms.Codes and read through `forms/messages.ts`'s `fieldMessage`.
- An approve refused as `conflict` while the application is still pending means its identity is gone, has an account, or is blocked (docs/07); the screen says so and offers Deny. A deny-with-block refused while still pending gets the plan's "Deny without blocking" sentence.
- Unblock lifts only the block the newest denial set (review 1470). A `conflict` re-fetches and says the block was set by another of the identity's applications.
- Times use `screens/apply/when.ts` (the viewer's timezone and locale) rather than a second copy.
- e2e seed: `mona` (moderator, an identity at the fake provider, signed in by `session.setup.ts`) and two pending applications from identities with no user, one per Playwright project, since each project approves one. Their ids go into `seed.json` as `applications: {phone, desktop}`.


Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310.

What exists there:
- `/admin/applications` is in the route table (`web/client/src/routes.tsx`) and renders `screens/admin/AdminRoot.tsx`, a placeholder, through the lazy `Admin` import. `bundle.mjs` leaves `src/screens/admin/` out of the reader's budget and `TestTheAdminBundleIsSeparate` (`internal/web/spa/spa_test.go`) keeps it a separate chunk.
- `api/client.ts` has `apiGet`, `apiSend` and `peekGet`; `api/problem.ts` turns a problem into words. The `errors` extension (`fieldErrors`) arrives with TKT-01M29CP2NG.

### Settled in grooming

- **What the reviewer sees and does** (docs/05 "mode = approval" step 5): the answers, the identity's provider, email and verified flag, prior applications from the same identity, and the other moderators' notes. The actions are approve (choose or confirm the handle), deny with a note, and deny with "block this subject". "Request changes" is a later phase.
- **Who reviews** is the moderator role; admins pass every check (docs/08 "Primitives", docs/05 "Roles and groups"). The API is staff-gated by TKT-01M29CP2MG. The admin scaffold lists Applications for moderators and admins by role (TKT-01M3NRYCP5).
- **Maintainer decision 2026-09-29: the applicant never sees the moderator's note** (docs/08 "Decided 2026-09-29", TKT-01M29CP2MG). The deny note is written for colleagues, and the screen says so beside the field.
- **Maintainer decision 2026-09-29: moderators and admins can unblock an identity**, with an audit row (docs/08, TKT-01M29CP2MG): `POST /api/v1/admin/applications/{id}/unblock`. The application stays denied.
- **Approval issues no session** (TKT-01M29CP2MG). The applicant's status page says "approved" with a sign-in link, and their next sign-in makes the member session.
- **No email in v1** (docs/10 item 12, TKT-01M29CP2N0). The screen says notifications are in-app only and the applicant learns the outcome on their status page.
- **The answers render through the shared form renderer in read mode**, `src/forms/FormView.tsx` with `mode: 'read'` (TKT-01M29CP2NG), against the form version the application answered (`AdminApplication.form`).
- **The admin screens stay in the admin chunk.** The built client is committed: run `go generate ./internal/web/spa` after any client change, and `npm run budget` stays within 60000 B gzip with the reader total unchanged.

Engineering decisions taken here, with the alternative that lost:
- **One route, detail by query:** `/admin/applications?id=N` opens one application, and `?status=` filters the list. Lost: a `/admin/applications/{id}` route, which docs/07's URL scheme, `routes.test.tsx` and the future HTML client would all have to add.
- **The decision controls are one component**, `screens/admin/applications/Decision.tsx`, exported for the queue. TKT-01M29CRJ4E's queue shows "the approve and deny controls from TKT-01M29CP2MG" on an application item. If the queue lands first with controls of its own, this ticket moves them here and the queue imports them. Lost: two copies that drift.
- **The list is a table at desktop width and stacked rows under 720 px**, from `screens/admin/common/`'s table (TKT-01M3NRYCP5).
- **After a decision the screen shows the application as decided** and offers "Next pending" rather than returning to the list, so a moderator can see what they did.

### Approach

1. `web/client/src/api/dto.ts`: the types from TKT-01M29CP2MG, `AdminApplicationList`, `AdminApplicationRow`, `AdminApplication`, `ApplicantIdentity`, `ApplicationNote`, `ApplicationDecision`, if that ticket has not added them; they are in `dto-contract.ts` with its goldens.
2. `screens/admin/applications/ApplicationsScreen.tsx`, mounted by `AdminRoot` for `/admin/applications`. It reads `?id` and `?status` from the location and shows the detail or the list.
3. `ApplicationList.tsx`: `GET /api/v1/admin/applications?status=&cursor=`, pending by default. A status filter (pending, approved, denied, withdrawn, all) writes `?status=`. Each row shows the time, provider, email and verified flag, and links to `?id=`. "More" follows `next_cursor`.
4. `ApplicationDetail.tsx`: `GET /api/v1/admin/applications/{id}`.
   - The identity: provider, email, a verified or unverified marker, and "Blocked" when blocked, with an Unblock button for the viewer (every viewer who reaches this screen is staff). Unblock posts `/unblock` and shows the answer. When the identity is gone, it says so.
   - The answers through `FormView` in read mode, with the form title and version.
   - Prior applications from the same identity, each linking `?id=`.
   - Notes with author and time, and an add-note form (`POST …/notes`, 1–2000 characters).
   - For a decided application: who decided, when, and the note.
   - For a pending one: `Decision`.
   - The fixed line: "Applicants are not emailed. They learn the outcome on their application status page, and notifications are in-app only."
5. `Decision.tsx`:
   - Approve: a handle field prefilled with `suggested_handle` and editable, an optional note, and a confirm button. `POST …/approve` with `{handle, note?}`. On success: "Approved as @handle. They can sign in now."
   - Deny: a required note (the button is off while it is empty), labelled "Moderators only. The applicant never sees this note.", and a "Block this subject" checkbox explaining that the identity cannot apply again until unblocked. `POST …/deny` with `{note, block}`.
   - Errors: `fieldErrors.handle` (`invalid`, `reserved`, `taken`) attaches to the handle field with `aria-invalid` and `aria-describedby`. `conflict` re-fetches the application and says "Already decided by @x" from `decision.by`, or, for a block whose identity is gone or now has an account, "This identity can no longer be blocked. Deny without blocking." `forbidden` says the viewer may no longer review.
6. `applications.css`, with the tokens in `src/styles/tokens.css`; usable at 390 px with no horizontal scroll.
7. `cmd/ketju/e2e.go`: seed a moderator (`mona`, role moderator) and one pending application from an identity with no user, on the active registration form, through TKT-01M29CKRDQ's queries. TKT-01M3NTSCMN reuses the seed; whichever lands first adds it.
8. Build: `npm run build`, `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and record `npm run budget`'s figure in a note.

### Tests

- `ApplicationsScreen.test.tsx`: pending is the default; the filter writes `?status=`; `next_cursor` pages; `?id=` shows the detail.
- `ApplicationDetail.test.tsx`: the answers render through `FormView` read mode against the application's own form version, with a newer version active; the identity's fields, prior applications and notes show; adding a note appends it; the email line is present.
- `Decision.test.tsx`: the handle is prefilled and editable; Deny is off without a note; approve and deny send the right bodies; a 422 `errors[handle]` attaches to the field; a 409 re-fetches and names who decided; the block 409 message; Unblock posts and the Blocked marker goes.
- `AdminRoot.test.tsx` from TKT-01M3NRYCP5 still passes: a moderator sees Applications.
- `e2e/admin-applications.spec.ts` at 390 px and 1440 px: `mona` opens the seeded application, reads the answers, approves with the suggested handle, and sees it approved. Axe reports no violations on the list and the detail. The page has no horizontal scroll.
- `TestTheAdminBundleIsSeparate` and `npm run budget` pass with the reader total unchanged.

### Out of scope

- The review API, its DTOs and the unblock route: TKT-01M29CP2MG.
- The shared form renderer: TKT-01M29CP2NG.
- The queue and its badge: TKT-01M29CRJ4E, which imports `Decision`.
- The applicant's notification: TKT-01M29CP2N0.
- A message to the applicant: not built; the maintainer decided the applicant sees the status only.

## Notes

**agent:claude-code/phase2-run-nrx2nn** at 2026-09-29T21:21:21Z

Built on main at 855ca5d. What the next reader needs, and the calls taken while building:

- Budget: `npm run budget` reads 45325 B gzip of 60000 B, against 45219 B before. The 106 B are the exports the admin chunk imports from the apply chunk (FormView, the answer kinds, `when`). Left to itself, the bundler moved everything the apply and admin chunks share into a third chunk, which cost the reader's total 737 B, because two gzip streams compress worse than one. `vite.config.ts` now keeps `src/forms/` and `src/screens/apply/` in one `ApplyRoot` chunk (`build.rolldownOptions.output.codeSplitting`, with `includeDependenciesRecursively: false`). Without that flag the group pulled the entry's own modules in, and the entry then loaded the apply chunk statically. Alternatives that lost: a copy of the renderer in the admin chunk (the settled "same component" rule rules it out), and accepting the 737 B (the settled "reader total unchanged" rules it out).
- The list is the common Table at both widths. The planned stacked rows under 720 px were not needed. At 390 px the cells wrap with no horizontal scroll, and the e2e spec checks that. The Status column shows only under the All filter, because every other filter names the one status shown.
- `ConfirmButton` (common) gained `disabled`, and it now moves focus to Cancel when it asks and back to its button when it closes. Before this, a keyboard user who pressed Deny was left on a button that had gone. Users screen tests still pass.
- After a decision, the notice takes focus, since the controls that had it are gone.
- `src/api/one-path.test.ts` refuses `.status ===` anywhere under screens/. The application's own status is read through `hasStatus` (ApplicationList.tsx), and problem types through `isProblem`.
- Seed (cmd/ketju e2e.go `seedReview`): moderator `mona`, signed in by `session.setup.ts`, and two pending applications from identities with no account (`pat@example.test` for phone, `quinn@example.test` for desktop), on the active registration form. Their ids are in `seed.json` as `applications: {phone, desktop}`. TKT-01M3NTSCMN can reuse mona. The applications are approved by this spec, so that ticket should seed its own.
- A conflict on approve while the application is still pending tells the moderator that the identity was removed, already has an account, or is blocked, and to deny instead. A conflict on unblock while the identity is still blocked tells them that another denial set the block.

**agent:claude-code/phase2-run-nrx2nn** at 2026-09-29T21:26:33Z

Terva review 1472 (PR #162, head 8bff57c, base fbe4787, run 9ff78247, Actions run #1018): three findings, all fixed in the next commit.

- high, "A note response can restore stale pending state after a decision": the note's answer now updates the application as it stands when the answer arrives. It uses a functional `setFresh` and skips a note that is already present. It no longer uses the copy captured at render. Test: "keeps a decision that lands while a note is on its way" holds the note's answer back until a denial lands. Putting the old update back fails that test.
- medium, "Returning from a filtered detail loses the filter": `applicationHref(id, status)` carries `&status=` when the filter is not pending. The detail's link back returns to `listHref(status)` and names that list, for example "Denied applications". Earlier-application links keep the filter too. Tests cover the list's link, the link back, and the pending default.
- medium, "The detail URL rejects valid large application IDs": `applicationID` accepts any positive int64, checked with BigInt against 2^63-1, and keeps the digits as a string. A Number would round an id past 2^53. The detail requests those exact digits. Tests cover the boundary on both sides and a 19-digit id. One limit remains: ids that arrive in JSON bodies (list rows, `prior`) are still JS numbers, which is true of every id this client reads. SQLite rowids here are nowhere near 2^53.

Budget unchanged at 45325 B.

**agent:claude-code/phase2-run-nrx2nn** at 2026-09-29T21:29:14Z

Terva review 1474 (PR #162, head 24a48fb, base fbe4787, run da720bde, Actions run #1023) marks all three findings from review 1472 resolved. It raised one new finding, fixed in the next commit:

- medium, "A decision response can erase a concurrently added note": `changed` no longer replaces the application wholesale. It keeps any note already shown that the decision's answer lacks, ordered by id, which is oldest first. Test: "keeps a note whose answer lands before a decision that missed it" holds the denial's answer back until the note has landed. Going back to `setFresh(next)` fails that test.

Budget: 45328 B of 60000 B.

**agent:claude-code/phase2-run-nrx2nn** at 2026-09-29T21:32:15Z

Terva review 1476 (PR #162, head 035689c, base fbe4787, run 7a8b3b4b, Actions run #1025) marks the review 1474 finding resolved. It raised one new finding, fixed in the next commit:

- medium, "Cancel pending navigation when leaving the detail": Next pending now sends its request with an AbortController, aborts it when the detail unmounts, and neither routes nor updates state once aborted. Test: "forgets Next pending once the moderator has moved on" holds the answer back, routes to the Denied list, and then answers. The address stays where the moderator went. Dropping the aborted check fails that test.

I also checked the screen's other async paths for the same pattern. None of them navigates. After an unmount, each one at most sets state on a component that is gone, which Preact ignores.

**agent:claude-code/phase2-run-nrx2nn** at 2026-09-29T21:35:02Z

Terva review 1478 (PR #162, head aa62925, base fbe4787, run 228a470c, Actions run #1030) marks the review 1476 finding resolved. It raised one new finding, fixed in the next commit:

- medium, "A completed note request can erase a newer draft": the add-note field is cleared only if it still holds the note that was sent (`setBody((b) => (b === sent ? '' : b))`). A second note started while the first was on its way is kept. The field stays editable, which lets a moderator write on. Test: "keeps a draft written while the last note was being sent". Going back to the unconditional clear fails that test.

The next review is the fifth round on this PR, the docs/13 cap. Budget: 45330 B of 60000 B.

## Summary

The /admin/applications screen is built under web/client/src/screens/admin/applications. It has the pending-by-default list with a ?status= filter and next_cursor paging. ?id=N opens the detail: the answers through FormView read mode against the version the applicant answered, the identity with its blocked state and Unblock, earlier applications, notes with an add-note form, and the decision or the Decision controls, which the queue can import. The no-email line is on both views. The e2e seed adds moderator mona and one pending application per Playwright project, and e2e/admin-applications.spec.ts approves one at each width with axe clean in both themes. The reader bundle total rose 106 B, to 45325 of 60000, because a codeSplitting group keeps the apply chunk whole. Every acceptance criterion is ticked.
