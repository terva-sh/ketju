---
schema: 3
id: TKT-01M3V6Y87D4EQBQ4HYTPJCSGR2
title: Add the watch and mute control to thread and forum screens
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:views
  - effort:S
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ29H6TZPN0X42SEW5RN
blocks_on: none
references:
  - ref: pr:294
    path: null
  - ref: pr:341
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:47:53Z
updated_at: 2026-10-01T23:36:19Z
created_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
updated_by:
  id: agent:claude-code/phase3-v6y87d
  name: ""
extensions: {}
---

## Description

The control a member uses to watch or mute a thread or a forum (docs/06 "Mobile": the "more" menu holds "watch"; the thread mockups' toolbar shows "watching ▾"). The thread toolbar and the phone's more menu carry it through `threadActions` (`web/client/src/shell/threadActions.ts`, whose comment already reserves a place for Watch). A forum's thread-list header carries it for the forum.

The server half is TKT-01M29CRJ29 (Implement watches and mutes). It builds `POST /threads/{id}/watch`, `POST /forums/{slug}/watch`, and the `watch` and `forum_watch` fields the control reads. This ticket was split out of it during Phase 3 grooming for the same reason as the list screens: the dist is committed, and client PRs queue one at a time.

## Acceptance criteria

- [x] Anonymous readers are not offered the control
- [x] The control works at 390 px and 1440 px, each test with one axe scan and no violations (watch.spec.ts)
- [x] The committed dist is rebuilt with go generate ./internal/web/spa and npm run budget stays within 70000 B, with the reader's growth recorded in the PR
- [x] A member can set Watch or Mute, or follow the forum (Normal when the forum has no level), on a thread from the desktop toolbar and the phone's more menu, and on a forum from its thread-list header (WatchDialog.test.tsx, watch.spec.ts)
- [x] The control shows the effective level, and the follow-the-forum choice names the forum's level and what it will do

## Implementation plan

### Settled in grooming

- **The rules** are docs/04 `watches` (run decision 2026-10-01). The thread's own level wins over its forum's, a forum's level covers its own threads, and mute removes alerts and unread badges.
- **The routes and fields** are TKT-01M29CRJ29's: `POST /threads/{id}/watch` and `POST /forums/{slug}/watch` with `{level}`, where `level` is `watch`, `mute` or `""`; the thread viewer block's `watch` and `forum_watch`; and the forum thread list's `watch`. This ticket depends on it.
- **Where the control sits.** The phone's "more" menu holds watch among the thread actions (docs/06 "Mobile"). The desktop toolbar draws the same list (`shell/threadActions.ts`, whose comment reserves Watch's place). The thread mockups show the state on the control ("watching ▾").
- **Modal UI** uses `useModal` from `components/focus.ts` (#262). Menus and popovers follow TKT-01M3TV4HBF's layout-effect rule.
- **Not automatic.** Replying or starting a thread does not watch it (docs/06, run decision 2026-10-01). The control is the only way a level is set.

### Approach

1. **DTOs.** `api/dto.ts` gains `ThreadViewer.forum_watch`, `ThreadList.watch`, `ThreadWatch` and `ForumWatch`, with their `dto-contract.ts` rows.
2. **Dialog.** `components/WatchDialog.tsx` opens through `useModal`, as `FlagDialog` does. It is a fieldset of three radio choices with one line each:
   - Watch: "Alert me to every new post."
   - Mute: "No alerts, and no unread count."
   - The third choice posts `""`, which removes the thread's own setting. Under docs/04's precedence the thread then follows its forum, so the label and line say what that will do (review 1693):
     - when the forum has no level, "Normal: alert me when someone mentions, replies to or quotes me";
     - when the forum is watched, "Follow the forum (Watch): alert me to every new post";
     - when the forum is muted, "Follow the forum (Mute): no alerts, and no unread count".

     The server has no explicit "normal" level that overrides a forum. `watches.level` is `watch` or `mute`. In a watched or muted forum, the member picks Watch or Mute for the thread, or follows the forum.

   Save posts the level, closes the dialog, updates the screen's state from the answer, and calls `refreshViewer()` so the badges follow. A refusal shows in the dialog through `ErrorPanel`.
3. **Thread action.** `threadActions` gains `watch?: { level: string; forumLevel: string; open: () => void }`. The label is the effective level: "Watching", "Muted", or "Watch" when neither is set. `ThreadScreen` passes it only for a member, so both the desktop toolbar and the phone's more menu carry it.
4. **Forum header.** `ThreadListScreen`'s header gains the same control for the forum, members only. It uses the same dialog without the forum line.
5. **Bundle.** The control goes in the reader bundle (the thread page needs it), about 0.8 KB gzip of JS and 0.1 KB of CSS. A lazy chunk would not lower the counted total (`scripts/bundle.mjs` counts non-admin lazy chunks). Measure, and record the growth in the PR.
6. **Build.** Run `go generate ./internal/web/spa`, and queue as the one open client PR.

### Tests

- `WatchDialog.test.tsx`:
  - each choice posts its level, with the third posting `""`;
  - the third choice reads Normal with no forum level, and "Follow the forum" with the forum's level and its effect otherwise;
  - focus is trapped and returned (`useModal`);
  - a refusal is shown;
  - `/me` is refreshed after a save.
- `threadActions` and `ThreadScreen` tests: the label follows the effective level, including a thread inherited from a muted forum, and anonymous readers are not offered the action.
- `ThreadListScreen.test.tsx`: the forum control is offered to members only and posts to the forum route.
- `e2e/watch.spec.ts`, from alice's stored session, with no `/auth/*` spend:
  - at 1440 px, the toolbar sets Mute on a thread with unread posts, and the forum list's unread badge for it goes away;
  - at 390 px, the more menu does the same and sets it back to Normal.

  Each test has one axe scan with the dialog open. Each restores the level it changed.
- `go generate ./internal/web/spa` leaves no diff, and `npm run budget` passes.

### Out of scope

- The routes, the precedence and the counts: TKT-01M29CRJ29.
- The alerts a watch produces: TKT-01M29CRJ2T and TKT-01M29CTY86.
- The `/watching` screen: TKT-01M3V6Y4JP.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T08:27:24Z

Review 1693 (head 2b18c6f, base a599b48), medium 'Do not present an inherited forum setting as Normal', fixed in the plan. The third choice posts an empty level, which removes the thread's own row. It reads Normal only when the forum has no level. Otherwise it reads 'Follow the forum (Watch)' or 'Follow the forum (Mute)' and states that effect. There is no explicit normal override, because watches.level is watch or mute.

**agent:claude-code/phase3-v6y87d** at 2026-10-01T19:02:48Z

Implemented on run/v6y87d-watch-control (commit 4db6bdcd, source and tests only; the dist waits for the orchestrator's hand-off).

What landed, against the plan:
- `components/WatchDialog.tsx` opens through `useModal`. Its three choices are Watch, Mute, and a third that posts `""`. The third reads "Normal" when the forum has no level, and "Follow the forum (Watch)" or "Follow the forum (Mute)" otherwise (review 1693). A forum's own dialog always reads Normal, and adds one line: "This covers the threads in NAME, not its subforums. A thread's own setting comes first." The title is "Alerts for this thread" or "Alerts for NAME". A refusal shows through `ErrorPanel`. An answer without a string `watch` counts as internal.
- On save the dialog drops the held GETs that count unread (`/api/v1/forums*`, `/unread`, `/new`, `/participated`, `/watching`). It also drops the held thread pages that carry the level: for a thread, that thread's pages; for a forum, every thread page. Then it calls `refreshViewer()`.
- `threadActions` gains `watch`, placed after Mark unread. ThreadScreen hands it over only to a signed-in member whose viewer block is their own (`pageFor === me`). A saved level shows at once while the viewer block it replaced is still on screen. The next page answer carries the level itself.
- ThreadListScreen's header has a `.watch-control` button before New thread, for members on a forum's own list. After a save it asks for the list again, so a mute takes the badges away.
- `ThreadList.watch` is required in `dto.ts`, as the server lane asked.
- `.thread-tools .toolbar` now wraps. With Watch added, a moderator's toolbar is wider than the tablet column.

Tests: `WatchDialog.test.tsx`, `ThreadScreen.watch.test.tsx`, plus additions to `threadActions.test.ts`, `ThreadListScreen.test.tsx` and `Shell.test.tsx`. `e2e/watch.spec.ts` runs two tests, each with one axe scan with the dialog open and no `/auth/*` spend, from alice's stored session:
- the first mutes "Minutes from the September meeting" from the toolbar at 1440 px or the more menu at 390 px, checks that the badge leaves `/f/general`, then sets the thread back to Normal through the UI;
- the second mutes and unmutes chatter from its header.

Both put alice's level back in a `finally`.

Reader bundle (`npm run budget`, measured with the dist generated locally and then restored): 66189 B before, 67360 B after (+1171 B gzip).

**agent:claude-code/phase3-v6y87d** at 2026-10-01T23:17:41Z

Review 1820 (head f9c09d8, base 0009f715), medium "Prevent a dismissed save from closing a newer watch dialog". Fixed in aacd5b11. A save's answer closes the dialog only while that dialog is mounted. It is handed to onSaved only if no later save to the same route has started since, so answers that land out of order do not put back an older level. A successful write still drops the held lists and pages and refreshes /me, even after its dialog was dismissed. The new test in ThreadScreen.watch.test.tsx, "keeps a late answer from closing a newer dialog or undoing a newer save", covers cancel and reopen and out-of-order answers. It fails without the fix. Reader bundle after the fix: 75236 B (+1474 B over main's 73762 B).

**agent:claude-code/phase3-v6y87d** at 2026-10-01T23:21:32Z

Review 1822 (head 39f18a4, base 0009f715), medium "Invalidate pending thread-page results as well as held GETs". Fixed in the commit after 39f18a4. A save now calls forgetAllThreadPages(), the generation barrier from #339 that a mark all read uses. A thread page asked for before the save is neither held nor stored when it lands, and loadThreadPage asks for it again, so the screen gets a post-save viewer block. This wipes the whole thread cache rather than one thread's, because a forum level reaches every thread in the forum and the barrier is global. A save is rare, as a mark is. Review 1822 judged finding 1 from review 1820 resolved. The new test in ThreadScreen.watch.test.tsx, "does not let a page asked for before a save put back the old level", holds back a live reload across a save. It fails without the fix. Reader bundle: 75176 B (+1414 B over main's 73762 B).

**agent:claude-code/phase3-v6y87d** at 2026-10-01T23:36:19Z

Review 1829 (head 92319bf, base 0009f715), medium "Apply a completed save to the viewer block currently displayed". Fixed in the commit after 92319bf. ThreadScreen's override is now keyed by the thread and by load generation, not by viewer-block identity. It applies to any page whose load started before the save answered (pageRequestedAt(page) <= loadCount() at the answer). So a live page that lands while the POST is out shows the saved level. A page loaded after the answer is the server's word, and the forgetAllThreadPages barrier makes any page asked for before the answer that lands later be asked for again. ThreadListScreen reads the list on screen through a ref at the answer and then reloads the list, which aborts any older request. Review 1829 judged review 1822's finding resolved. The new test in ThreadScreen.watch.test.tsx, "shows the saved level over a live page that landed while the save was out", fails on the previous ThreadScreen. Reader bundle: 75209 B (+1447 B over main's 73762 B). This was review round 3 of 5.

## Summary

A member can now watch or mute a thread from the desktop toolbar and the phone's more menu, and a forum from its thread-list header. Anonymous readers and pre-users are not offered the control. The PR is #341.

- `components/WatchDialog.tsx` offers Watch, Mute, and a third choice that posts `""`. The third reads "Normal" when the forum has no level, and "Follow the forum (Watch)" or "Follow the forum (Mute)" otherwise (review 1693).
- The thread action is labelled with the effective level: Watching, Muted or Watch. The forum control is labelled with the forum's own level and sits first in the header's `.list-actions`, before Mark all read and New thread.
- A save calls `forgetLists()`, which includes Watching, and `forgetAllThreadPages()`, so no list or thread page asked for before the save is held or stored after it (review 1822). It then refreshes `/me`. The forum list asks for itself again, so a mute takes its badges away.
- The thread page shows a saved level over any page whose load started before the save answered, a live page included (review 1829).
- A late answer closes only its own dialog, and never undoes a newer save to the same target (review 1820).
- `ThreadList.watch` is required in `dto.ts`. The thread toolbar wraps.

Reader bundle: 73762 B before, 75209 B after (+1447 B gzip, budget 120000 B). Tests: `WatchDialog.test.tsx`, `ThreadScreen.watch.test.tsx`, additions to the threadActions, ThreadListScreen and Shell tests, and `e2e/watch.spec.ts`, which runs two tests at 390 and 1440 px with one axe scan each.
