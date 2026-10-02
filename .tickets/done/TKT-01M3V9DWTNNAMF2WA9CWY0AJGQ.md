---
schema: 3
id: TKT-01M3V9DWTNNAMF2WA9CWY0AJGQ
title: Let a member turn off the single-key shortcuts (WCAG 2.1.4)
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - effort:S
  - area:api
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:360
    path: null
  - ref: pr:370
    path: null
claim: null
archive: null
created_at: 2026-10-01T08:31:22Z
updated_at: 2026-10-02T20:27:56Z
created_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-9DWTN
  name: ""
extensions: {}
---

## Description

Found while grooming TKT-01M3S3ZZSZ (Close the small visual gaps between the client and the mockups) on 2026-10-01, against main at a599b48. The mockups draw a `/` key hint beside the desktop search field. That ticket leaves `/` out until this one lands, a run decision in docs/06 "Paged, not infinite".

Every desktop shortcut in docs/06 is a single character with no modifier: `j`, `k`, `g`, `u`, `n` and `?`. WCAG 2.1 success criterion 2.1.4, Character Key Shortcuts (level A), asks for one of three things for such shortcuts:
- a way to turn them off;
- a way to remap them so they include a modifier;
- shortcuts that act only while their component has focus.

`keyboard/shortcuts.ts` ignores keys typed in a text field and is absent on phones. Neither meets 2.1.4, and nothing records why the client does not. axe does not test 2.1.4, so the accessibility pass (TKT-01M29CGBXK) passed without it.

### What to do
Offer a setting that turns the single-key shortcuts off, the simplest of the three. It belongs with the user settings screens (TKT-01M29CXK4E), and docs/06 should say so. Once the setting exists, the `/` shortcut (TKT-01M3V7GE59) can be added with it. Alternatively, record in docs/06 or docs/07 why the client does not meet 2.1.4.

## Acceptance criteria

- [x] With the shortcuts off, for a member through Settings or a signed-out reader through the shortcut list, no character key acts on a thread page or the forum index: j, k, g and its digits, u, n and ? (shortcuts.test.tsx, ForumIndex.test.tsx)
- [x] GET /settings, PATCH /settings and /me carry shortcuts, a JSON boolean that reads true when missing or malformed; PATCH refuses any other type as wrong_type and keeps the other prefs (TestPrefs, TestUpdateSettingsValidates, TestUpdateSettingsKeepsOtherPrefs; goldens, dto.ts, docs/04 and docs/07 updated)
- [x] /settings has the switch, saved through the settings save queue; the shortcut list gives a member a link to Settings and a signed-out reader a button whose choice this browser keeps, and says when the shortcuts are off
- [x] At 720 px and above, while the shortcuts are on, the forum index footer reads Press ? for keyboard shortcuts and ? opens the list there; with them off it reads Keyboard shortcuts and ? does nothing; a phone shows no hint (TKT-01M3S3ZRYD criterion 2)
- [x] e2e/shortcuts.spec.ts and e2e/forum-index.spec.ts cover the switch and the footer at 390 px and 1440 px, with one axe scan per test
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **The way 2.1.4 is met: an off switch.** It is the first of WCAG 2.1 SC 2.1.4's three options, and the one the description asks for. Decided by the run, pending maintainer review, in docs/06 "Paged, not infinite" (2026-10-02), with the alternatives that lost.
- **On by default.** Today's desktop readers keep the shortcuts unless they turn them off.
- **Where the choice lives.**
  - A member's choice is stored on the account as `shortcuts` (a JSON boolean) in `users.prefs_json`. `/me`'s `settings` and `GET /settings` carry it, and `PATCH /settings` takes it. This is the theme's pattern (docs/07 "Preact client", "Theme").
  - A signed-out reader's choice is kept in the browser, under `localStorage` key `ketju.shortcuts`, as a signed-out reader's theme is.
- **Where it is changed.**
  - A member changes it on `/settings`.
  - A signed-out reader changes it in the shortcut list (`keyboard/HelpOverlay.tsx`).
  - For a member, the list links to `/settings` and does not save the account itself, so the settings screen's save queue (`saves` in `SettingsScreen.tsx`, Terva reviews 1838 and 1875) stays the one writer.
- **What "off" means.** No character key acts on any page. That covers `j`, `k`, `g` and its digits, `u`, `n` and `?`, and `/` once TKT-01M3V7GE59 adds it. The "Keyboard shortcuts" buttons (`ThreadFoot`, the forum index footer) still open the list, which then says the shortcuts are off and how to turn them on. Escape inside a dialog is not a character key and keeps working.
- **The front page's hint, folded in here.** TKT-01M3S3ZRYD (Add the front page footer with member and post counts) is done with its criterion 2 unticked. That criterion is "Press ? for keyboard shortcuts" on the forum index, which opens the list. This ticket meets it, because the switch is what that criterion waited on. Decided by the run, pending maintainer review, in docs/06 "Thread list views (forum index)" (2026-10-02):
  - while the shortcuts are on, `?` is bound on the index and the footer button reads "Press ? for keyboard shortcuts";
  - while they are off, `?` does nothing there and the button reads "Keyboard shortcuts".
  - A separate follow-up ticket lost, because it would be a second client PR in the dist queue for about 20 lines, and the switch is what makes the hint true.
- **Phones.** Nothing changes on a phone. The shortcuts are absent there (docs/06 "Mobile"), and the setting has no effect there. The setting's hint says it is for desktop.
- **Security.** None. The setting changes only which keys the client listens to. It reaches no other viewer, and `PATCH /settings` keeps its gates: a member or an agent, anonymous `unauthenticated`, pre-user `forbidden`.

### Approach

1. **Server: `prefs_json` and the DTO.**
   - `users.Prefs` (`internal/service/users/me.go`) reads a third key, `shortcuts`. Anything but a JSON boolean reads as `true`, as each key that is missing or malformed reads as its default.
   - `settingsOf` fills `dto.Settings.Shortcuts bool json:"shortcuts"`.
   - The anonymous `/me` settings carry `shortcuts: true`.
2. **Server: `PATCH /settings`.**
   - `users.SettingsPatch` gains `Shortcuts json.RawMessage json:"shortcuts"`.
   - `UpdateSettings` takes a JSON boolean. Null or any other type is `wrong_type`. A valid value goes into the prefs patch.
   - `UpdateUserSettings` already merges named keys and keeps the rest, so the query does not change.
3. **Docs at implementation.** These say what was built, beside the decision lines already in docs/06:
   - docs/04 "users": `prefs_json` reads three keys.
   - docs/07: the `GET /settings` and `PATCH /settings` row, and the "Preact client" bullet.
   - goldens: `/me` and `/settings`, regenerated with `-update`.
   - `api/dto.ts`: `Settings.shortcuts: boolean`, required. `api/dto-contract.ts` imports the goldens.
4. **Client: one signal decides.** In `keyboard/shortcuts.ts`:
   - Export `shortcutsOn`, a `computed`. For a member it is `viewer.value.settings.shortcuts`. Otherwise it reads the browser's choice, held in a signal loaded from `localStorage` with the `safeStorage` guard `state/theme.ts` uses.
   - Export `setDeviceShortcuts(on)` for signed-out readers.
   - `useThreadShortcuts`'s `onKey` returns early when `!shortcutsOn.value`, beside `isPhone.value`, and ends a `g` in progress as it does now.
   - `bootstrap.ts`'s anonymous fallback gains `shortcuts: true`.
   - `state/viewer.ts` `setViewerSettings` compares `shortcuts` too. It compares field by field today, so a change to `shortcuts` alone would be dropped.
5. **Client: `/settings`.** `SettingsScreen.tsx` gains a checkbox fieldset after Theme: "Keyboard shortcuts", labelled "Use single-key shortcuts", with the hint "On a desktop: j, k, g, u, n and ?. Turn them off if they get in the way of a screen reader or voice control." It saves through `save('shortcuts', checked)`. The `Key` type and `fieldText` gain `shortcuts`, with "Choose on or off." for `wrong_type`.
6. **Client: the shortcut list.**
   - `HelpOverlay` shows a status line when the shortcuts are off: "Single-key shortcuts are off."
   - For a member it then shows a link, "Change this in Settings", to `/settings`.
   - For a signed-out reader it shows a button: "Turn shortcuts off" or "Turn shortcuts on", which calls `setDeviceShortcuts`.
   - Focus stays inside the dialog after the button (`useModal`).
7. **Client: the forum index.**
   - `ForumIndex.tsx` installs a `keydown` listener in a `useLayoutEffect` (the TKT-01M3TV4HBF rule). It acts only on `?`, and only when the viewer is not on a phone, the shortcuts are on, `ignored(e)` is false and the list is closed. It opens the list.
   - `IndexFoot`'s button reads "Press <kbd>?</kbd> for keyboard shortcuts" when the shortcuts are on, and "Keyboard shortcuts" when they are off.
   - The overlay's `lead` changes from "These keys work on a thread page, where ? opens this list." to one that names `?` as working here and the rest on a thread page.
8. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** All of this is in the reader: `keyboard/`, `ForumIndex.tsx` and the settings screens, which are inline (`screens/account/AccountRoot.tsx`). Estimated at +450 B gzip. Measure it and state it in the PR.

### Tests

- **Go (`internal/service/users`).** `TestPrefs` gains rows: missing, `null`, `"false"`, `0` and a column that is not an object read `true`, and `false` reads `false`.
- **Go (`internal/web/api/settings_test.go`).**
  - `TestUpdateSettingsValidates` gains rows: `"no"`, `0` and `null` for `shortcuts` are `wrong_type` and write nothing.
  - `TestUpdateSettingsKeepsOtherPrefs` gains a case: a `shortcuts` write keeps `page_size` and `theme`, and a `theme` write keeps `shortcuts`.
  - `TestSettingsApplyWithoutReLogin` gains a case: `PATCH {"shortcuts": false}` answers 200 with `shortcuts: false`, and the next `/me` carries it.
  - `TestSettingsRoutesRefuseAgentsAndAnonymous` already holds anonymous `unauthenticated` and a pre-user `forbidden` before the body is read. It gets a `shortcuts` body so the new key is covered too.
  - The goldens for `/me` (member and anonymous) and `/settings` are regenerated with `-update`.
- **Vitest.**
  - `keyboard/shortcuts.test.tsx`, "with the shortcuts off, no character key acts": `j`, `k`, `g 1 2 Enter`, `u`, `n` and `?` each leave focus, the address and `helpOpen` alone. One case is for a member (`settings.shortcuts: false`) and one for a signed-out reader (`ketju.shortcuts` off).
  - `keyboard/shortcuts.test.tsx`, "a member's account choice wins over this browser's".
  - `HelpOverlay` cases:
    - a signed-out reader's button turns the shortcuts off and on, and stores the choice;
    - a member sees the Settings link and no button;
    - "Single-key shortcuts are off." shows only when they are off.
  - `screens/account/SettingsScreen.test.tsx`: the checkbox sends `PATCH {shortcuts: false}` alone through the queue, and the answer becomes the viewer's.
  - `state/viewer.test.ts`: `setViewerSettings` reports a change when only `shortcuts` differs.
  - `screens/ForumIndex.test.tsx`:
    - "? opens the list on the index when shortcuts are on, above 720 px";
    - "with them off, ? does nothing and the button reads Keyboard shortcuts";
    - "on a phone ? does nothing".
    - The press goes one microtask after mount, outside `act`, as `shortcuts.test.tsx` does.
- **e2e.**
  - `e2e/shortcuts.spec.ts`, at 1440 px:
    - "turned off in Settings, no key acts": carol turns them off on `/settings`, then on a thread page `j` and `?` do nothing, and the "Keyboard shortcuts" button opens the list, which says they are off. `afterEach` puts carol's `shortcuts` back to `true`, as `settings.spec.ts` resets her preferences.
    - "a signed-out reader turns them off from the list", in a context with no stored session.
    - At 390 px, the existing "none of them at 390 px" test still holds, and the setting is on the settings screen.
  - `e2e/forum-index.spec.ts`: at 1440 px the footer reads "Press ? for keyboard shortcuts", and `?` opens the list. At 390 px there is no hint and `?` is inert, as today.
  - One axe scan per test, with the list open on desktop. No spec here calls `/auth/*`, so `e2e/session.ts` gains no row.

### Out of scope

- The `/` shortcut. TKT-01M3V7GE59 (Add the / shortcut that opens search on desktop) owns it, and depends on this ticket.
- Remapping keys, and shortcuts on a phone.
- A device-local copy of a member's choice for first paint. Nothing is painted from it, and `/me` is inlined in the shell, so the first key press already knows.

## Notes

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:16Z

run decision: pending maintainer review. The single-key shortcuts get an off switch, on by default. A member's choice is stored on the account as shortcuts in prefs_json and changed on /settings. A signed-out reader's choice is kept in the browser and changed from the shortcut list, which links a member to Settings instead. Off means no character key acts on any page. Recorded with the alternatives in docs/06 'Paged, not infinite' (2026-10-02).

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:16Z

run decision: pending maintainer review. TKT-01M3S3ZRYD (Add the front page footer with member and post counts) criterion 2 is folded into this ticket rather than filed as a follow-up: while the shortcuts are on, ? is bound on the forum index and the footer reads 'Press ? for keyboard shortcuts'; while off, it reads 'Keyboard shortcuts'. A follow-up lost because it would be a second client PR in the dist queue for about 20 lines, and the switch is what makes the hint true. Recorded in docs/06 'Thread list views (forum index)' (2026-10-02). TKT-01M3S3ZRYD is done and stays as it is; this ticket's criterion 4 is where that criterion is met.

**agent:claude-code/phase4-run-9DWTN** at 2026-10-02T19:40:58Z

Implemented as planned. Details the plan left open, settled here: users.Prefs now answers a UserPrefs struct (PageSize, Theme, Shortcuts) rather than two values; the shortcuts cases are in a new TestPrefsShortcuts beside TestPrefs. A signed-out reader's choice is stored as localStorage ketju.shortcuts = 'off', and the key is removed when they turn them back on. The list's status line is empty while they are on and reads 'Single-key shortcuts are off.' when off; a member's link reads 'Turn them off in Settings' while on and 'Change this in Settings' while off; a signed-out reader's button reads 'Turn shortcuts off' or 'Turn shortcuts on'. The index's lead line reads 'Here ? opens this list. The other keys work on a thread page.' while on, and 'These keys work on a thread page.' while off. The forum-index desktop screenshots are refreshed; the phone ones are unchanged. Reader bundle 79213 B -> 79833 B gzip (+620 B; the plan estimated +450).

**agent:claude-code/phase4-run-9DWTN** at 2026-10-02T20:27:52Z

Terva review 1967 (PR #370, head 3d05bcc, base 5b1a6da), medium 'Cancel the pending go-to-post timer when shortcuts are disabled': fixed. useThreadShortcuts now watches shortcutsOn with a signals effect and ends a g in progress, timer and hint, the moment they turn off; commit() also reads the switch again before going. New case 'turning them off during g cancels the pending go, with no key pressed after' fails with the fix removed. Reader bundle now 79859 B gzip (79213 B on main, +646 B).

## Summary

The single-key shortcuts can be turned off (WCAG 2.1 SC 2.1.4). prefs_json reads shortcuts, a JSON boolean that is true when missing or malformed; /me, GET /settings and PATCH /settings carry it, and PATCH refuses anything but a boolean as wrong_type. In the client, shortcutsOn (keyboard/shortcuts.ts) is the member's account choice, or for anyone else this browser's (localStorage ketju.shortcuts); with it false no character key acts on a thread page or the forum index, and a g in progress ends at once (Terva review 1967). /settings has a 'Use single-key shortcuts' checkbox saved through the save queue. The shortcut list says when they are off, and links a member to Settings or gives a signed-out reader a button. The forum index binds ? in a layout effect while they are on, and its footer reads 'Press ? for keyboard shortcuts' (meeting TKT-01M3S3ZRYD criterion 2), or 'Keyboard shortcuts' when off. Reader bundle 79213 B -> 79859 B gzip. Every criterion met.
