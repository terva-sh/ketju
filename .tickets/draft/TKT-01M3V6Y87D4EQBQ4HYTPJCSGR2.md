---
schema: 3
id: TKT-01M3V6Y87D4EQBQ4HYTPJCSGR2
title: Add the watch and mute control to thread and forum screens
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-10-01T07:47:53Z
updated_at: 2026-10-01T07:54:01Z
created_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-unread
  name: ""
extensions: {}
---

## Description

The control a member uses to watch or mute a thread or a forum (docs/06 "Mobile": the "more" menu holds "watch"; the thread mockups' toolbar shows "watching ▾"). The thread toolbar and the phone's more menu carry it through `threadActions` (`web/client/src/shell/threadActions.ts`, whose comment already reserves a place for Watch). A forum's thread-list header carries it for the forum.

The server half is TKT-01M29CRJ29 (Implement watches and mutes). It builds `POST /threads/{id}/watch`, `POST /forums/{slug}/watch`, and the `watch` and `forum_watch` fields the control reads. This ticket was split out of it during Phase 3 grooming for the same reason as the list screens: the dist is committed, and client PRs queue one at a time.

## Acceptance criteria

- [ ] A member can set Watch, Normal or Mute on a thread from the desktop toolbar and the phone's more menu, and on a forum from its thread-list header (WatchDialog.test.tsx, watch.spec.ts)
- [ ] The control shows the effective level and says when it comes from the forum
- [ ] Anonymous readers are not offered the control
- [ ] The control works at 390 px and 1440 px, each test with one axe scan and no violations (watch.spec.ts)
- [ ] The committed dist is rebuilt with go generate ./internal/web/spa and npm run budget stays within 70000 B, with the reader's growth recorded in the PR

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
   - Normal: "Alert me when someone mentions, replies to or quotes me." When the forum has a level, it adds "This forum is set to Watch" or "...to Mute, which applies to this thread."
   - Mute: "No alerts, and no unread count."

   Save posts the level, closes the dialog, updates the screen's state from the answer, and calls `refreshViewer()` so the badges follow. A refusal shows in the dialog through `ErrorPanel`.
3. **Thread action.** `threadActions` gains `watch?: { level: string; forumLevel: string; open: () => void }`. The label is the effective level: "Watching", "Muted", or "Watch" when neither is set. `ThreadScreen` passes it only for a member, so both the desktop toolbar and the phone's more menu carry it.
4. **Forum header.** `ThreadListScreen`'s header gains the same control for the forum, members only. It uses the same dialog without the forum line.
5. **Bundle.** The control goes in the reader bundle (the thread page needs it), about 0.8 KB gzip of JS and 0.1 KB of CSS. A lazy chunk would not lower the counted total (`scripts/bundle.mjs` counts non-admin lazy chunks). Measure, and record the growth in the PR.
6. **Build.** Run `go generate ./internal/web/spa`, and queue as the one open client PR.

### Tests

- `WatchDialog.test.tsx`:
  - each choice posts its level, with Normal posting `""`;
  - the forum line appears only when the forum has a level;
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
