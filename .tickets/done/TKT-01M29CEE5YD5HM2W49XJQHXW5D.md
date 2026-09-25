---
schema: 3
id: TKT-01M29CEE5YD5HM2W49XJQHXW5D
title: Build the forum index screen
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA9A5PGMCFDSVWQAHT87G
blocks_on: none
references:
  - ref: design:mockups-front
    path: docs/mockups/Main.dc.html
  - ref: pr:85
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-25T03:45:23Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The front page: the forum tree with per-forum unread counts and recent activity, matching `docs/mockups/Main.dc.html` on desktop and `MobileFrontDark.dc.html` on phones.

This is the first screen anybody sees, and in a small community it is also the one that answers "is anything happening". The unread counts are the content.

## Acceptance criteria

- [x] Unread counts render for a signed-in viewer and are absent rather than zeroed for an anonymous one
- [x] A forum with no threads renders a usable empty state
- [x] The floating new-thread control appears above the tab bar on phones
- [x] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Implementation plan

Revised at claim (2026-09-25), after reading the code. `GET /forums` (TKT-01M29CA9A5) now carries `last_post`.

### Approach
1. **Screen.** `screens/ForumIndex.tsx` replaces the placeholder at `/`. It loads `useResource('/api/v1/forums')` and renders `ForumTree` in array order. A failure shows `ErrorPanel` with Retry.
2. **Grouping.** The mockup's groups ("Community") are labels over forums. Our tree has root forums with children, so the mapping is:
   - a root with children is a labelled group whose first row is the root itself, followed by its children;
   - consecutive roots without children share one unlabelled group.
   The order is the server's. This mapping is a divergence to list.
3. **Rows.** Per the mockup's `row()`:
   - the name links to `/f/{slug}`;
   - a lock icon marks a non-public forum, with a visually hidden "members only" or "restricted";
   - an "archived" tag; the description;
   - the activity column shows "N threads". Post totals wait on draft TKT-01M3B6A68B, a divergence.
   - the unread badge reads "N new" and links to the forum; with nothing unread it is "—" on desktop and absent on the phone.
   - the last post shows the title linked to the thread, then the handle and `PostTime`.
4. **Anonymous viewer.** An anonymous viewer gets no unread column at all: no header, no badge, no "—" (AC1). The branch is on `viewer.user`, not on the count.
5. **Empty states.** A forum with no threads says "No threads yet", with "Start one" linking to `/f/{slug}/new` for a signed-in viewer when the forum is not archived. An empty tree says there are no forums yet (AC2).
6. **Header.**
   - The h1 is "Forums". The site name is not in the API, a divergence.
   - A signed-in viewer's summary line is "N unread threads" from `/me`.
   - "New thread" (signed-in) opens the forum picker. "Mark all read" is left out until the viewer-state endpoints exist (TKT-01M29CA9CE), a divergence.
7. **Phone.**
   - Stacked cards per `mobile_front()`.
   - A 56 px floating "New thread" button, signed-in only, sits `16px` above the tab bar plus the safe area (AC3). It opens the forum picker (settled).
   - The picker is a modal dialog listing the non-archived forums, children indented, each linking to `/f/{slug}/new`, which is still a placeholder screen. Escape closes it and focus returns to the opener. `trapTab` moves from ComposerSheet into a shared `components/focus.ts`.
8. **Icons.** `lock` and `pen` come from the mockup's generator.
9. **Seed.** The e2e seed (`cmd/ketju/e2e.go`) grows a tree like the mockup's: roots with children, a members-only forum, and an empty forum, so the screenshots have something to show. The existing thread stays in `general`.

### Tests
- AC1: Vitest with the goldens: member (badges "7 new" and "2 new") and anonymous (no `[data-unread]`, no unread header, no "0").
- AC2: Vitest: a zero-thread forum shows the empty text and a Start link for a member, none for anonymous; an empty tree shows its message.
- AC3: Playwright at 390 px checks the FAB's bottom edge is above the tab bar's top edge and in the viewport. Vitest checks the FAB is absent on desktop and for anonymous viewers, and that the picker opens, closes on Escape, and returns focus.
- AC4: `e2e/forum-index.spec.ts` writes `e2e/screenshots/forum-index-{phone,desktop}-{light,dark}.png` when `KETJU_SCREENSHOTS=1`, and they are committed. CI runs the same spec's assertions without writing. The divergences are listed in the PR.

### Settled in grooming (2026-09-24)

- The forum-index FAB opens the composer with a forum picker, since /f/{slug}/new needs a forum.
- Activity fields come from CA9A5.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR. Automated pixel diffing lost, because the mockups are static HTML with placeholder data and would never match exactly.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:28:01Z

### Built
- `screens/ForumIndex.tsx` replaces the placeholder at `/`. `screens/ForumPicker.tsx` is new, and so is `screens/forums.css`: one markup, a grid on desktop and cards below 720 px.
- The `lock` and `pen` icons are added, and `trapTab` is moved to `components/focus.ts`, shared with ComposerSheet.
- **Server bug fixed** (from TKT-01M29CA9A5, already merged): `forums.Tree` asked `ForumLastPosts` for root forums only, so every child forum's `last_post` was null. The screenshots showed it. `TestForumTreeCounts` now checks a child's last post, and the reverting mutant fails.
- **e2e**:
  - The seed (`cmd/ketju/e2e.go`) is now a tree like the mockup's: two roots with children, a members-only forum, a restricted one alice cannot see, empty forums, and three short threads.
  - `e2e/forum-index.spec.ts` is new.
  - A `setup` project signs alice and carol in once and saves their sessions. It was needed because the new spec's sign-ins pushed the run past the `/auth/*` burst of 10 (docs/08), and the desktop smoke step timed out on the provider's page. The smoke test keeps its own real sign-in for alice.

### Evidence
- AC1: Vitest, member golden: "7 new" and "2 new" badges, a dash for none, the unread column heading, the summary. Anonymous golden: no `[data-unread]`, no `.forum-unread`, no column, no summary, no "0 new". Playwright, anonymous at both widths: no `.forum-unread`, and members-only and restricted forums are absent.
- AC2: Vitest: "No threads yet." with a Start link to `/f/{slug}/new` for a member, none for anonymous, none in an archived forum; an empty tree says there are no forums, with no FAB. Playwright: `reading` shows a Start link.
- AC3: Playwright at 390 px: the FAB's bottom edge is at or above the tab bar's top edge and inside the viewport. It is hidden at 1440 px. Vitest: the picker is member-only, lists the open forums (archived left out), focuses the first, keeps Tab inside, and closes on Escape with focus back on the opener.
- AC4: four screenshots are committed, taken with `KETJU_SCREENSHOTS=1` at both widths and in both schemes. The divergences are listed in the PR and in the next note.
- Mutants (client): 13; the one survivor (the picker's archived-root filter) led to a test, and is now killed. Go: 1 on the fix, killed.
- Checks: vitest 652 pass, `go test -race ./...` passes, and Playwright passes 8 (setup 2, then 3 per width).

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:28:01Z

### Divergences from the mockups

Committed screenshots: `web/client/e2e/screenshots/forum-index-{desktop,phone}-{light,dark}.png`. They are compared against `Main.dc.html`, `FrontDark.dc.html` and `MobileFront{Light,Dark}.dc.html`.

**Data the API does not carry**
1. **Heading and summary.** The heading is "Forums", not the community's name ("The Thursday Group"): the API has no site name. The summary is "N unread threads." from `/me`. The mockup's "12 unread posts across 5 threads since Tuesday evening" needs a post count and a last-visit time the API lacks.
2. **Group labels.** The labels are the root forums' own names, and each root is also the first row of its group. The tree has no separate categories (docs/04: forums nest one level). Roots with no children share an unlabelled group, for example "Site feedback".
3. **Activity column.** It shows "N threads" only. Per-forum post totals ("3,912 posts") wait on draft TKT-01M3B6A68B.
4. **Unread badge.** "N new" counts threads with unread posts (settled for TKT-01M29CA9A5). The mockup's numbers read as posts.
5. **Last-post time.** It uses the thread page's `PostTime` ("03:26 AM", "Thu 09:26 PM"), not relative phrases ("14 min ago").
6. **Empty forums.** An empty forum's last-post cell says "No threads yet. Start one". The mockup has no empty forum.

**Controls not built yet**
7. **Desktop header.** It has "New thread" but no "Mark all read", which needs the viewer-state endpoints (TKT-01M29CA9CE).
8. **Footer.** The desktop footer ("34 members · 1,208 posts", shortcuts, Guidelines, Markdown help) is absent. It needs counts the API lacks and screens that do not exist yet.

**Shell, not this screen**
9. **Top bars.** On the phone, the bar shows the centred wordmark and search, not the mockup's left-aligned wordmark with search and a moderator-queue icon. On desktop there is no moderator-queue link or avatar. Both bars are the shell's (TKT-01M29CEE4K).

**Deliberate layout choice**
10. **Phone cards.** A card's name, unread badge, last-thread title and "Start one" are separate links. The mockup makes the whole card one link, but nested links are invalid, and the last post has its own destination.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:40:46Z

Review of PR #85 failed with context_limit (run 445): the diff is 260 KB, 190 KB of it dist; see TKT-01M3AZGKBP. The source is reviewed instead in review-only PR #86 (branch review/cee5y-source: #85 plus one commit restoring dist to main, 70 KB). #85 merges only if #86's review is clean and #85's CI is green. #86 is closed unmerged afterwards.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T03:45:23Z

### Review 1, on source-only PR #86 (head 4d7b98d, run 448): two medium findings, both fixed

1. **The picker could trap focus outside itself.** With every forum archived it had no links, so focus stayed behind the modal and Escape never reached it.
   - Fix: "New thread" and the FAB are offered only when `pickable(forums)` is not empty.
   - The picker also focuses its close button when it has no forum to offer.
   - Tests: all-archived shows neither control; a picker rendered with nothing to offer focuses Close and closes on Escape. Both mutants are killed.
2. **At 720 to 1100 px the fixed desktop columns crushed the forum names.**
   - Fix: a tablet range with narrower fixed columns (88 px, 72 px and 180 px, 16 px gaps). They stay fixed so the headings and rows align.
   - Test: Playwright at 720 px checks the name column is at least 200 px and nothing overflows. Removing the tablet block fails it (name width under 200).

## Summary

The forum index at / renders GET /forums: roots as labelled groups with their children, in server order. Each row shows its thread count, the viewer's unread badge (absent for anonymous viewers), and its last post, or an empty state with a Start link. A phone FAB above the tab bar and a desktop New thread button open a forum picker dialog. Screenshots at 390 px and 1440 px, light and dark, are committed with the divergences listed. Also fixed: forums.Tree left child forums' last_post null.
