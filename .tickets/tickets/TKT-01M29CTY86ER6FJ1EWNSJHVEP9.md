---
schema: 3
id: TKT-01M29CTY86ER6FJ1EWNSJHVEP9
title: Build the notifications UI and alerts tab
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:views
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ2TJG29ZBV037PYC943
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references:
  - ref: design:mobile-nav
    path: docs/06-thread-views.md
  - ref: pr:294
    path: null
claim:
  actor: agent:claude-code/phase3-cty86
  branch: run/cty86-notifications-ui
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a95b9c9d7b3f72a71
  commit: 86752c422f9d6152ce015e2c1b52cdd721f33910
  session: null
  claimed_at: 2026-10-01T18:09:33Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-10-01T18:09:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-cty86
  name: ""
extensions: {}
---

## Description

The notification list with its API, a badge in both navigation layouts, and the Alerts tab on phones.

Each notification links to the exact ordinal that caused it, which is the only thing anybody wants from it. Marking read happens on view for the ones displayed, with an explicit mark-all.

In-app only in v1, with no email and no push, so this list is the entire notification story and it needs to be good enough to bring someone back.

## Acceptance criteria

- [ ] Each notification links to the post that caused it, landing on the right page with the post anchored (alerts.spec.ts, NotificationsScreen.test.tsx)
- [ ] Badge counts match the list and clear on read in the desktop bell and the phone's Alerts tab, from the displayed rows and from mark-all (alerts.spec.ts, NotificationsScreen.test.tsx)
- [ ] The Alerts screen works at 390 px and 1440 px with the same content, each test with one axe scan and no violations (alerts.spec.ts)
- [ ] An empty state explains what would appear there
- [ ] The committed dist is rebuilt with go generate ./internal/web/spa and npm run budget stays within 70000 B, with the reader's growth, measured eager and lazy, recorded in the PR

## Implementation plan

### Settled in grooming

- **The API is TKT-01M29CRJ2T's.** This ticket is the client only. The ticket text says "the notification list with its API", but in grooming `GET /notifications` and `POST /notifications/read` moved to the generator ticket, so that the serve-time access rule and its security tests land with the rows they guard. Client PRs also queue for the committed dist (docs/13 "Running two lanes, changed").
- **Where it lives.** The Alerts tab and the desktop bell route to `/notifications` (decided at claim for TKT-01M29CEE4K, `web/client/src/shell/nav.ts`). Both already draw `badges.alerts`, from `/me`'s `notifications`.
- **What a row carries.** Each notification carries its kind, actor, thread title, forum and ordinal, and never post text (docs/04 `notifications`, run decision 2026-10-01). It links to the permalink `/t/{id}/{slug}/{ordinal}`, which serves the page holding the post and anchors it (docs/07 "What the thread endpoint does").
- **Marking read.** Rows are marked read on view, for the ones displayed, and there is an explicit mark-all (ticket description).
- **In-app only** (docs/10 item 12).
- **Times are absolute** through `PostTime` (docs/06, decided 2026-10-01).

### Approach

1. **DTOs.** `web/client/src/api/dto.ts` gains `Notification`, `NotificationList` and `NotificationsRead`, matching TKT-01M29CRJ2T's goldens. `dto-contract.ts` gains their rows, so `contract.test.ts` holds the client to `internal/dto`.
2. **State.** `web/client/src/state/notifications.ts` does three things:
   - `loadNotifications(cursor)` goes through `api/resource.ts`;
   - `markViewed(ids)` posts `{ids}` to `/api/v1/notifications/read` and then calls `refreshViewer()`, so the bell and the tab drop at once;
   - `markAll(upTo)` posts `{up_to}` with the first row's id on the first page. The list is in `id` order, so that is the highest id, and every unread row at or below it is cleared, including rows not yet paged in (review 1709).
3. **Screen.** `web/client/src/screens/NotificationsScreen.tsx` replaces the `placeholder('notifications', 'Alerts')` entry in `routes.tsx`. It keeps screen `notifications` and the title "Alerts", through `useShellContext`.
   - It is an ordered list, one row per notification. Each row is a sentence by kind:
     - "*actor* mentioned you in *thread*"
     - "replied to you in"
     - "quoted you in"
     - "posted in *thread*, which you watch"
     - "Your application was approved"
     - "A new application is waiting", linking to `/admin/applications`
   - The thread title links to the permalink. The forum's name and a `PostTime` follow it. An anonymized actor is drawn as every author block draws one.
   - An unread row is marked by more than colour: it carries a visually hidden "Unread" label.
   - "Older" follows `next_cursor`. "Mark all read" sits in the screen's header. An `ErrorPanel` shows a failure.
   - The empty state says: "No alerts yet. You are alerted here when someone mentions you, replies to or quotes one of your posts, or posts in a thread you watch."
   - When a page of rows is shown, the ids of its unread rows are posted once. The rows keep their unread marking until the screen is left, so the reader can see what was new.
4. **Badges.** No new badge code. The TopBar bell and the TabBar's Alerts read `badges.alerts`, and `refreshViewer()` after a mark is what clears them. Coming back to the screen fetches again.
5. **Bundle.** The screen goes in the reader bundle as an eager import in `routes.tsx`. The estimate is about 1.6 KB gzip of JS and 0.3 KB of CSS. `scripts/bundle.mjs` counts every non-admin chunk, lazy ones included, so a lazy chunk would not lower the counted total. It would only add a round trip on the first visit. Measure both ways with `npm run budget` and record the reader's growth in the PR. If the screen is made lazy anyway, add its row to `lazyChunks` in `testing.tsx` (#250).
6. **Build.** Run `go generate ./internal/web/spa` and commit the dist. This PR waits its turn in the one-client-PR queue.

### Tests

- `NotificationsScreen.test.tsx` (Vitest, `renderAt('/notifications')`):
  - each kind's sentence and link, including the two application kinds;
  - the permalink href;
  - the empty state;
  - exactly the displayed unread ids posted once, then `/me` fetched;
  - "Mark all read" posting the first row's id;
  - "Older" appending the next page;
  - an error shown through `ErrorPanel`.
- `nav` and `Shell` tests: the bell and the Alerts tab show the count from `/me` and drop after a mark.
- `e2e/alerts.spec.ts` at 390 and 1440 px, from stored sessions (carol and alice), with no `/auth/*` spend, so no `paceAuth` row is needed:
  1. Carol replies to one of Alice's posts, mentioning her, through the API with carol's stored state.
  2. Alice's badge appears in the top bar at 1440 px and in the tab bar at 390 px. The test polls `/me`, because generation is asynchronous.
  3. Alice opens Alerts and sees the row. Activating it lands on the page holding the post, with `#post-N` in view.
  4. The badge has cleared.

  A second test checks the empty state for a member with no alerts. There is one axe scan per test, and a failure prints its error context to the CI log (#261).
- `go generate ./internal/web/spa` leaves no diff, and `npm run budget` passes.

### Out of scope

- The API, generation and the access rules: TKT-01M29CRJ2T.
- The cross-forum lists: TKT-01M29CTY7K and TKT-01M3V6Y4JP.
- A live badge without a page load: the SSE tickets TKT-01M29CRJ3X and TKT-01M29CTYA.
- Push and email: docs/10 items 12 and 27.

## Notes

**agent:claude-code/phase3-groom-unread** at 2026-10-01T07:54:21Z

Scope change in Phase 3 grooming (2026-10-01): this ticket is now client only. GET /notifications, POST /notifications/read and the /me count's filter moved to TKT-01M29CRJ2T (Build the notification generator). The description's 'with its API' now means the client's use of that API. The rule that a row carries no post text is a run decision recorded in docs/04 'notifications' and is pending maintainer review.

**agent:claude-code/phase3-groom-unread** at 2026-10-01T09:03:52Z

Review 1709 (head 5c4846a, base a599b48), medium 'Use the list order, not the newest displayed ID, for Mark all read', fixed in the plans here and on TKT-01M29CRJ2T. The list is now keyset on id descending, the order the alerts arrived, with at shown only. Mark all read posts the first row's id as up_to, which clears every unread row at or below it.
