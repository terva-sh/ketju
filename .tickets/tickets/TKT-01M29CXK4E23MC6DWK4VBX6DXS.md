---
schema: 3
id: TKT-01M29CXK4E23MC6DWK4VBX6DXS
title: Build the user settings screens
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC0X7SY4SHJ46FV40GNW
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M3V3CFMA067MC9AX37KN5Z6T
blocks_on: none
references:
  - ref: design:url-scheme
    path: docs/07-api-and-clients.md
  - ref: pr:342
    path: null
claim:
  actor: agent:claude-code/phase3-settings-client
  branch: run/cxk4e-settings-screens
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ab6a07aa04ce32dcc
  commit: d2b4f2083bb6f70ec049944728db2cea5c877a36
  session: null
  claimed_at: 2026-10-01T21:17:18Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-02T02:58:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-settings-client
  name: ""
extensions: {}
---

## Description

`/settings`, `/settings/sessions`, and `/settings/identities`: timezone, page size, theme, the session list with sign out here and everywhere, and linked identities with add and remove.

The session list is the screen that makes server-side sessions worth their cost. Seeing every signed-in device and killing one is the feature people reach for when a laptop goes missing.

## Acceptance criteria

- [x] Timezone, page size and theme apply at once on /settings, and a second browser of the same member shows them on its first load; the account's theme wins over the browser's stored copy
- [x] /settings/sessions shows device, IP and last use, marks this device, signs out another session in place, and signs out everywhere after a dialog that says this browser signs out too, then loads /
- [x] e2e: signing out another session ends it server-side (its next /me has user null), using a dedicated member, with paceAuth and a row in e2e/session.ts
- [x] The own profile's Account block links to Settings and keeps Link a sign-in and Sign out
- [x] Tested at 390 and 1440 px with one axe scan per test
- [ ] The committed dist is regenerated (go generate ./internal/web/spa) and npm run budget stays within 70000 B, with the before and after figures in the PR
- [x] /settings/identities links through /auth/link and removes an identity; an identity whose removal the API marks not removable has Remove disabled and explained, a conflict answer shows the explanation, and signed_out true loads /

## Implementation plan

### Settled in grooming

- The three addresses are docs/07's: `/settings`, `/settings/sessions` and `/settings/identities`. They already route to placeholders in `routes.tsx`. A signed-out reader is offered sign in, and a pre-user never reaches them, because `ApplicantShell` takes every address but `/apply`.
- The endpoints and their shapes are TKT-01M29CCC0X's plan: `GET`/`PATCH /settings`, `GET`/`DELETE /settings/sessions`, `DELETE /settings/sessions/{id}`, `GET /settings/identities` and `DELETE /settings/identities/{id}`, with the `removable` and `current` flags.
- "Sign out everywhere" ends this browser's session too (docs/05, decided by the run 2026-10-01, pending maintainer review). After it, and after signing out the current row, the client does a full load of `/`, as the Sign out form does (`shell/SignOut.tsx`, #253). That drops every signal, and other tabs reload through `state/viewerGuard.ts`.
- Removing an identity ends the sessions signed in through it (docs/05, decided by the run). When the answer says `signed_out: true`, the client loads `/` the same way.
- Theme: the account's value wins on load and is copied to `localStorage` for first paint (docs/07 "Theme", decided by the run). `state/theme.ts` keeps its key and values, so `theme-early.js` and `theme-early.test.ts` need no change.
- Adding an identity is the existing "Link a sign-in" (`shell/LinkSignIn.tsx`, #245), a full load of `/auth/link` with `return_to=/settings/identities`.
- The settings screens join the lazy chunk TKT-01M3V3CFMA creates, `src/screens/account/AccountRoot.tsx`. Its Account block on the own profile gains a "Settings" link, which is how the phone's Me tab and the desktop top bar's handle reach settings (decided 2026-09-29, TKT-01M3QS0TMV: account actions are one press from every screen through the own profile).
- Bundle: the chunk counts toward the reader budget, because `scripts/bundle.mjs` counts every non-admin lazy chunk. Estimate: 3.0 to 3.8 KB gzip for JS and CSS for the three screens and the theme sync. That is close enough to the remaining room (the reader is at 60465 B of 70000 B before this group's tickets) that the PR measures it, both in the chunk and inlined, and keeps whichever counts fewer bytes. If the reader would exceed the budget either way, the docs/14 stop rule (class 5) applies, and the ticket blocks rather than raise the budget or change what `bundle.mjs` counts.
- Modal confirmation uses `useModal` (`components/focus.ts`, #262).

### Approach

1. **State.**
   - `state/theme.ts` gains `syncThemeFromAccount(me)`. When `me.settings.theme` is set for a member, it calls `setThemePref`, which stores the copy and applies it. `main.tsx` calls it after the bootstrap, and `refreshViewer` calls it after each `/me`. Update the comment that says syncing waits for phase 4.
   - The `theme-color` meta tags follow the applied theme. That is TKT-01M29CGBYY's, and that ticket makes `applyTheme` update them.
   - Add the `Settings`, `SessionList` and `IdentityList` DTOs to `api/dto.ts` with the contract rows. The server adds the goldens in TKT-01M29CCC0X.
2. **`/settings`** (`SettingsScreen`) is a form with three fields:
   - **Timezone:** a `<select>` of `Intl.supportedValuesOf('timeZone')`, plus `UTC` and the account's stored value, each added when the browser's list lacks it, with the account's value selected. The browser's list omits valid IANA names the server accepts, `UTC` among them, and a select with no option for the stored value would show some other zone and could send it (Terva review 1691). The field sends only on its own `change`. If the account still has the default and this device's zone (`Intl.DateTimeFormat().resolvedOptions().timeZone`) differs, the field offers "Use this device's timezone (Zone)". Where `supportedValuesOf` is missing, fall back to a text field.
   - **Page size:** a number field, 10 to 50.
   - **Theme:** System, Light and Dark, as radio buttons.

   Each change sends `PATCH /api/v1/settings` with that one key. On 200 it replaces `viewer.value.settings`, so `PostTime` and the theme change at once, and applies the theme. On `validation-failed` it shows the field error next to the field, using `forms/messages.ts` where its codes match. Nav links lead to Sessions and Sign-in identities.
3. **`/settings/sessions`** (`SessionsScreen`) lists `GET /api/v1/settings/sessions`:
   - Each row shows the device, IP, provider, "Signed in" date and "Last used" (`PostTime`). The current row is marked "This device".
   - Each row has "Sign out". The current row's sign-out confirms, then loads `/`. Another row's sign-out removes that row in place.
   - A "Sign out everywhere" button opens a `useModal` dialog. The dialog says this browser will be signed out too. Then it sends `DELETE /api/v1/settings/sessions` and loads `/`.
   - A `not-found` on a row (signed out elsewhere meanwhile) refreshes the list.
4. **`/settings/identities`** (`IdentitiesScreen`) lists `GET /api/v1/settings/identities`:
   - Each row shows the provider name (or "No longer configured"), email, linked date, last sign-in and "Used for this session".
   - Each row has "Remove". When `removable` is false, the button is disabled and the row explains: "This is your only way to sign in."
   - Remove confirms in a `useModal` dialog. If the identity is the current session's, the dialog says so. On `{signed_out: true}` it loads `/`; otherwise it refreshes the list. A `conflict` shows the same explanation and refreshes.
   - `LinkSignIn` sits below the list, with `return_to` pointing back here.
5. **Account block.** In `ProfileScreen` (TKT-01M3V3CFMA), add a "Settings" link above "Link a sign-in".
6. All three screens fit one column at 390 px, with 44 px controls (`.hit` where a control is drawn smaller). Styles go in `screens/account/settings.css`.
7. Run `go generate ./internal/web/spa`, commit the dist, and quote `npm run budget` before and after.

### Tests

Vitest (`screens/account/*.test.tsx`, `state/theme.test.ts`):
- `theme from the account wins and is stored`: with `me.settings.theme = 'dark'` and a stored `light`, the page has `data-theme="dark"` and the storage now holds `dark`. For `system`, the storage key is removed.
- `changing page size sends one PATCH and updates the viewer`, and likewise for timezone and theme. A field error is shown beside its field.
- `offers this device's timezone when the account has the default`.
- `the timezone select always holds the stored value`: with a stub `supportedValuesOf` that lacks `UTC` and `Asia/Calcutta`, an account stored as either has it as the selected option, `UTC` is always offered, and no PATCH is sent until the field itself changes.
- `sessions list marks this device and signs out another row in place`.
- `signing out this device or everywhere loads /` (session management): the `nav.leave` or `location` stub is called with `/` after a 204. Without a 204, nothing is loaded and the error is shown.
- `everywhere's dialog says this browser signs out too`, with focus trapped and Escape closing it (`useModal`).
- `the only identity's Remove is disabled and explained` (identities), and `a conflict answer shows the explanation and refreshes`.
- `removing the current session's identity loads / when signed_out is true`.
- `own profile's Account block links to Settings and keeps Link a sign-in and Sign out`.

e2e (`e2e/settings.spec.ts`, one axe scan per test, both projects, so 390 and 1440 px):
- `preferences apply at once and follow the member to a second browser`: set page size 10 and theme dark as a stored-session member, open a thread (10 posts to a page), then open a second context with the same stored state and see the dark theme on first load. Restore the defaults in `afterEach`, because other specs read the same stored member's thread pages at 25 to a page.
- `signing out another session ends it server-side` (session management): a dedicated member signs in in two contexts; context A signs out B's row; B's next `/api/v1/me` has `user: null`. Then A signs out everywhere, and A is signed out with `/api/v1/me` showing `user: null`. Use a dedicated seeded member, not one with a stored state file, so no other spec loses its session. This spends `/auth/*`, so call `paceAuth(n)` with its count and add its row to `e2e/session.ts`.
- `the only identity cannot be removed, and a linked one can` (identities): the dedicated member's single identity shows a disabled Remove. Link a second fake-provider subject through the existing link flow (as `link-sign-in.spec.ts` does), remove the first, and sign in again with the second. `paceAuth(n)` and a row in `e2e/session.ts`.

### Out of scope

- The endpoints: TKT-01M29CCC0X.
- Changing a handle and deactivating: TKT-01M3V6XTYG (Add handle change and self-deactivation to the settings screen).
- Locale: read-only (see TKT-01M29CCC0X).
- A theme control for signed-out readers. None exists today, and docs/09 lists theme among the account's settings.
- The `theme-color` meta tags and the manifest: TKT-01M29CGBYY.

## Notes

**agent:claude-code/phase3-groom-pages** at 2026-10-01T07:51:18Z

run decision: pending maintainer review. The account's theme wins over the browser's stored copy on every load and replaces it; the copy exists only for first paint (docs/07 Preact client, Theme, 2026-10-01). Sign out everywhere and removing the current session's identity sign this browser out (docs/05, 2026-10-01).

**agent:claude-code/phase3-groom-pages** at 2026-10-01T08:19:00Z

Terva review 1691 (head 2455be0, base a599b48), finding 'Include the account's current timezone in the selector': fixed in the plan. The select adds UTC and the stored value when the browser's list lacks them, sends only on its own change, and is covered by a named test.

**agent:claude-code/phase3-settings-client** at 2026-10-01T21:17:24Z

Acceptance criterion 6's budget figure (70000 B) was superseded by the maintainer's raise to 120000 B gzip on 2026-10-01 (#331, docs/14 class 5, docs/07 'Bundle'). This ticket is judged against 120000 B; the criterion text is left as written. The reader is 71808 B on d2b4f208 before this ticket.

**agent:claude-code/phase3-settings-client** at 2026-10-01T22:02:00Z

Implementation notes (Phase A, branch run/cxk4e-settings-screens, unpushed; the dist is not committed until the orchestrator hands it over).

### Where it landed

- `screens/account/SettingsScreen.tsx`, `SessionsScreen.tsx`, `IdentitiesScreen.tsx`, the shared `settings.tsx` (heading and links between the three, the `Confirm` dialog on `useModal`, `leaveHome`) and `settings.css`. They join `AccountRoot.tsx` and the reader's chunk.
- `state/theme.ts` `syncThemeFromAccount(me)`, called from `main.tsx` after the bootstrap and from `refreshViewer` after each `/me`. `Settings.theme` is required in `api/dto.ts`, typed `string` as the goldens are; the bootstrap's anonymous fallback carries `system`.
- The Account block on the own profile gains "Settings" above "Link a sign-in".
- e2e: `e2e/settings.spec.ts`, with two members seeded for it in `cmd/ketju/e2e.go` (`seedSettings`: `sid-phone` and `sid-desktop`, plus link subjects `sid-phone-link` and `sid-desktop-link` at the fake provider), one per project, because the identities test removes the identity its member signs in with. The budget row is in `e2e/session.ts` (4 + 7 /auth/* per project).

### Bundle, measured both ways

Reader before: 71808 B (d2b4f208). Inline in the reader's chunk: 75035 B at the final head (+3227 B). Measured both ways at one state of the code, inline was 74945 B and a lazy chunk of their own 77272 B, the split again adding a shared chunk, so they are inline. Judged against 120000 B (#331).

### Where the code departs from the plan, and why

- Field errors do not use `forms/messages.ts`. That module is in the lazy application chunk; importing it from the reader pulled the chunk's shared code into a new chunk the reader loads, 1040 B more. The three settings fields have one meaning per code, so each field has its own words.
- `nav` (the full-load hand-off) moved from `routes.tsx` to `src/leave.ts`, re-exported from `routes.tsx`, so a screen can call `nav.leave('/')` without importing the route table that imports the screens.
- A page size change drops every cached and held thread page (`forgetEveryThreadPage` in `state/threads.ts`). The thread cache keys pages by log length and page number, so a page cut at 25 would otherwise sit beside pages cut at 10.
- A settings field shows the value being saved until the answer comes, and keeps a refused value beside its reason. Drawn from the stored value alone, a radio or select jumped back while the PATCH was in flight (the e2e caught it: Playwright's `check()` saw the radio uncheck).
- A GET /settings answer is taken into the viewer only while no PATCH has been sent, so a slow GET cannot undo a change.

### Tested

tsc; vitest 2415 passed; gofmt, go build, go vet and `go test -race ./...` clean, TestPhase2Exit passed; the full Playwright suite at 390 and 1440 px, 237 passed and 13 skipped by width, and settings, profile and targets again on the final head. Criterion 6 stays unticked until the dist is regenerated and committed in Phase B.

**agent:claude-code/phase3-settings-client** at 2026-10-01T22:02:02Z

run decision: pending maintainer review. In the session list a row names its provider by the key /settings/sessions sends ("signed in with github"), and on /settings/identities an identity whose provider the operator removed reads "No longer configured" with that key. Asking /settings/identities for display names lost because it is a second request on every visit for a label; leaving the provider out lost because a member signed in through two providers tells their sessions apart by it. Recorded in docs/05 "Sessions and CSRF".

**agent:claude-code/phase3-settings-client** at 2026-10-02T00:38:49Z

Rebased onto dd840125 (#339, #340, #341). The page-size wipe now calls #339's forgetAllThreadPages in state/threads.ts, which also sets its wipe watermark, instead of a forgetEveryThreadPage of its own; the earlier note's name for it is superseded. routes.tsx's unused placeholder helper is removed, since #339 and this ticket replaced its last users. seedSettings members come after #339's nell in cmd/ketju/e2e.go. Reader after the rebase: 75237 B on dd840125, 78361 B with this ticket (+3124 B), of 120000 B.

**agent:claude-code/phase3-settings-client** at 2026-10-02T02:48:45Z

Terva review 1838 (head 27a8de90, base dd840125), medium 'Serialize preference saves instead of ordering snapshots by send number': fixed. Settings saves now go one at a time through a promise queue, each PATCH sent once the one before it has been answered, and every answer is adopted in turn, so the last answer is the account. A refused save does not hold up the next. Regression tests: two overlapping saves to different fields with the first answered late, and a refused save followed by another. Reader 78367 B of 120000 B.

**agent:claude-code/phase3-settings-client** at 2026-10-02T02:53:50Z

Terva review 1875 (head dfb1e736, base 25ad977a): finding 1 of 1838 resolved; both new mediums fixed. 'Keep preference-save serialization across settings-screen remounts': the save queue is the module's, not the screen's, and the GET /settings answer is taken only if no save was out when the screen asked and none was made since (module counters made and settled). 'Do not clear newer invalid input when an earlier save completes': every edit, sent or refused here, is numbered, and only the answer to a field's newest edit clears what it shows. Regression tests: a save after leaving /settings and coming back waits for the one still out; a GET answered after that save does not undo it; a refused value typed during a save survives its answer.

**agent:claude-code/phase3-settings-client** at 2026-10-02T02:56:20Z

Terva review 1876 (head f6c0fcdf, base 25ad977a): both 1875 findings resolved; the new medium 'Coordinate viewer refreshes with preference saves' is fixed. state/viewer.ts gains setViewerSettings, which counts each settings answer taken from GET or PATCH /settings; refreshViewer keeps the viewer's settings over its /me answer's when one was taken since the refresh was asked for, for the same viewer, and syncs the theme from the result. Regression tests in state/viewer.test.ts: a /me asked for before a save and answered after it keeps the saved settings and theme while taking its other fields; a /me asked for after a save takes its own settings.

**agent:claude-code/phase3-settings-client** at 2026-10-02T02:58:48Z

Terva review 1879 (head c2824d5e, base 25ad977a): the 1876 finding is resolved, and the new medium 'Invalidate held thread pages when /me changes the page size' is fixed. state/viewer.ts replaces the viewer through one setViewer, which drops every thread page (forgetAllThreadPages) whenever the page size changes, whether the change came from /settings or from /me. The settings screen no longer does it itself. Regression test in state/viewer.test.ts: a /me with the same page size keeps a populated thread cache, and a /me with another size empties it. This is review round 4 on #342.
