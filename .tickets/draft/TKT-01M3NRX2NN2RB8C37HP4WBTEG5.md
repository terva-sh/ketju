---
schema: 3
id: TKT-01M3NRX2NN2RB8C37HP4WBTEG5
title: Build the admin applications review screen
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-09-29T05:06:22Z
updated_at: 2026-09-29T07:18:30Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] /admin/applications lists applications newest first from GET /api/v1/admin/applications, pending by default with a status filter, and pages with next_cursor
- [ ] Opening an application shows its answers through the shared src/forms renderer in read-only mode, against the form version it was answered on, with the identity's provider, email and verified flag, its prior applications and the moderators' notes
- [ ] A moderator can add a note, approve with the suggested handle prefilled and editable, and deny with a required note and a 'block this subject' checkbox
- [ ] A 409 from approve or deny re-fetches the application and says who decided it; a handle error from the errors extension attaches to the handle field
- [ ] The screen states that notifications are in-app only and that applicants learn the outcome on their status page
- [ ] The screen lives under src/screens/admin so TestTheAdminBundleIsSeparate passes, works at 390 px and 1440 px, the dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
- [ ] A blocked applicant's identity shows as blocked with an Unblock control for moderators and admins, calling POST /api/v1/admin/applications/{id}/unblock (maintainer decision 2026-09-29)
- [ ] The deny note field says the applicant never sees it, and a moderator on /admin/applications?id=N can reach every action with the keyboard (Decision.test.tsx)
- [ ] e2e/admin-applications.spec.ts: a seeded moderator approves a seeded pending application at 390 px and 1440 px, and axe reports no violations on the list or the detail

## Implementation plan

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
