---
schema: 3
id: TKT-01M29CEE5YD5HM2W49XJQHXW5D
title: Build the forum index screen
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/cee5y-forum-index
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: a60f29e96f3ef3ed3e3eaedd2665909783dea23c
  session: null
  claimed_at: 2026-09-25T03:18:14Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-25T03:18:14Z
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

- [ ] Unread counts render for a signed-in viewer and are absent rather than zeroed for an anonymous one
- [ ] A forum with no threads renders a usable empty state
- [ ] The floating new-thread control appears above the tab bar on phones
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `screens/ForumIndex.tsx` calls `useResource('/forums')` and renders `dto.ForumTree` in array order (no client sort; `dto.Forum` deliberately has no position field), with one level of children.
2. Desktop, per Main.dc.html (`front()` in `generate.py`): the columns are Forum, Activity, Unread, Last post; there are "Mark all read" and "New thread" controls, and the sidebar block that holds Guidelines and Markdown help. Phone, per MobileFront*.dc.html: stacked cards with the name, a lock icon for non-public forums, an unread badge, and the last thread title with its time.
3. Unread: render the badge only when `isSignedIn`. `dto.Forum.unread` is a non-null int that is 0 for anonymous viewers, so the client branches on `viewer.user` rather than on the count.
4. Empty state: when `forums` is empty, or a forum has `thread_count === 0`, render a row with a "No threads yet" message and a start-thread link when the viewer can post.
5. Phone FAB: a 56 px "New thread" control, positioned `bottom: calc(56px + 16px + env(safe-area-inset-bottom))` so it clears the tab bar (mockup: `bottom: 72px`). The URL scheme only has `/f/{slug}/new`, so from the index the FAB either opens a forum picker or goes to a default forum; the docs choose neither.
6. Recent activity and last post: `dto.Forum` has neither (interfaces item 6). Raise it with the API lane before building, or render those columns empty and note the divergence in the PR.

### Files
`web/client/src/screens/ForumIndex.tsx`, `src/state/forums.ts`, `src/styles/components.css`.

### Tests
- AC1, reworded: commit Playwright screenshots at 1440 px (light and dark) and 390 px (light and dark), compare them by eye against Main.dc.html, FrontDark.dc.html and MobileFront{Light,Dark}.dc.html, and list any divergence in the PR.
- AC2: Vitest renders with `forums.member.json` plus `me.member.json` and asserts badges; with `forums.anon.json` plus `me.anon.json` it asserts no `.unread-badge` elements exist, rather than "0".
- AC3: Vitest with a zero-thread forum fixture and with an empty tree asserts the empty-state text and link.
- AC4: Playwright at 390 px asserts the FAB's bottom edge is above the tab bar's top edge and inside the viewport.

### Design
docs/06 "Thread list views" and "Mobile" (Composing: floating new-thread button); `generate.py` `front()`, `mobile_front()`; Main.dc.html, FrontDark.dc.html, MobileFront*.dc.html.

### Interactions
Depends on CCC02 and CA9A5 (`GET /forums`), on CEE5H (`useResource`), and on CEE4K and CCC0F for chrome and tokens. CGBXK runs axe here. CGBY1 starts here.
Open in docs: the target of the FAB from the index (which forum?), and the per-forum activity data.

### Settled in grooming (2026-09-24)

- The forum-index FAB opens the composer with a forum picker, since /f/{slug}/new needs a forum.
- Activity fields come from CA9A5.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR. Automated pixel diffing lost, because the mockups are static HTML with placeholder data and would never match exactly.
