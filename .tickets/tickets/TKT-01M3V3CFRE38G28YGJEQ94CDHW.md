---
schema: 3
id: TKT-01M3V3CFRE38G28YGJEQ94CDHW
title: Check Phase 3's exit criterion with named end-to-end tests
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:test
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CTY4JB7MMAWHVDT4BTCZM
  - TKT-01M29CTY7K2YTY0V54YKJTKX1W
  - TKT-01M29CTY86ER6FJ1EWNSJHVEP9
  - TKT-01M29CTY8TB2CHCBQH3ABHCG3Q
  - TKT-01M29CTYA0E5Y06WWYY2ETBF8V
  - TKT-01M29CYS9W8GAWNQVHFH7HZ7AR
  - TKT-01M29CGBZDVGWNDTKHJFHHSGY5
  - TKT-01M29CRJ1PY355KX3HX7A5N785
  - TKT-01M29CRJ29H6TZPN0X42SEW5RN
  - TKT-01M29CRJ2TJG29ZBV037PYC943
  - TKT-01M29CRJ3BEMZREJQRGAK3AVGK
  - TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
  - TKT-01M29CTY58PZ9W4DJ8Y0NEECE0
  - TKT-01M29CTY5TZ1QMWK6BTGA21BWQ
  - TKT-01M29CTY6EE6T8BWR1DGSG3WGV
  - TKT-01M29CTY71398QADNHHRP2HQVW
  - TKT-01M29CTY9D04J3NR7E5HH59M24
  - TKT-01M29CXK0KHC2QZQ286AG0KKGD
  - TKT-01M3NS122FTMG03KSKHJ0NP4V0
  - TKT-01M29CGBYYTJCDV43WDP2FE0Z5
  - TKT-01M29CCC0X7SY4SHJ46FV40GNW
  - TKT-01M29CXK4E23MC6DWK4VBX6DXS
  - TKT-01M3V3CFMA067MC9AX37KN5Z6T
  - TKT-01M3S3Z9E1RX86KA4WX4DQKWBJ
  - TKT-01M3S3ZF6JTYTADBREEVE3S0CN
  - TKT-01M3S3ZRYD1KMHTX8KVDWENKGR
  - TKT-01M3S3ZS0H5485CG25KMRCT5N8
  - TKT-01M3S3ZZQDXM4RX4HK6V5X8VX7
  - TKT-01M3S3ZZSZQHQN41DQKMM5F8D2
  - TKT-01M3S3ZZYA9BPJE1VM260806WK
  - TKT-01M3TV4HBFR3Y9DJR88AJRFTM0
  - TKT-01M3TSZHSWT4VE8G07NJMTR203
  - TKT-01M3V73DP6TZ1BDQVT7ATX0EC5
  - TKT-01M3V73J8KH0F1Q0H7D615HRA3
  - TKT-01M3VBWMA83SC252X83NTQ417Y
  - TKT-01M3V70X0F61FHCKS6TKTK69BQ
  - TKT-01M3VHSYFR176FWWBAPV8PHSEK
  - TKT-01M3V6Y4JP8W8WY0JG6NZV7M7S
  - TKT-01M3V6Y87D4EQBQ4HYTPJCSGR2
  - TKT-01M3XNQ77222FNGZHA4HM380Z8
  - TKT-01M3XP6ZRZQ968B8QPZ1RJBEGM
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-exit-check
  branch: run/cfre-phase3-exit
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a537b1aa8ea458004
  commit: 1fb5db0cdfc70f1b73799dd7aedb57116a2e51f4
  session: null
  claimed_at: 2026-10-02T06:29:52Z
  expires_at: null
archive: null
created_at: 2026-10-01T06:45:45Z
updated_at: 2026-10-02T06:53:30Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-exit-check
  name: ""
extensions: {}
---

## Description

docs/09 gives Phase 3's exit criterion as "daily-driver quality for an active community, on a phone as much as on a desktop". Phase 2's exit was checked by named tests (TKT-01M3NTSCMN, CI step "Phase 2 exit tests, by name"). This ticket does the same for Phase 3 once its features land.

Grooming names the tests. At least, a member at 390 px and at 1440 px:
- reads by the unread lens and jumps to the first unread;
- finds a post by search, with a snippet;
- sees a new reply arrive live through the pill;
- gets a notification for a mention and a watched thread;
- attaches an image and sees its thumbnail;
- moves between pages with the phone page picker.

Each runs with axe, and none needs an `/auth/*` call beyond `paceAuth`.

## Acceptance criteria

- [ ] e2e/phase3-exit.spec.ts holds the plan's named tests for lenses, unread, notifications, search, live updates, attachments, phone pages, the manifest, settings and the profile, each passing in the phone (390 px) and desktop (1440 px) projects with one axe scan per test; the page picker test runs on the phone only
- [ ] A CI step in the Build and Test job, Phase 3 exit tests by name, runs go test -race ./cmd/ketju/ -run '^TestPhase3Exit' -count=1 -v, and the PR quotes the step-log lines naming every TestPhase3Exit test and every phase3-exit.spec.ts test on the merged head
- [ ] No spec added here calls /auth/* (e2e/session.ts unchanged), and the merged head's npm run budget line shows the reader within 70000 B gzip
- [ ] Every other phase-3 ticket except the epics and TKT-01M3V3BVPY is done or archived before this one closes
- [ ] cmd/ketju phase3_exit_test.go's TestPhase3Exit tests prove through the real serve that search, notifications, lenses, live events and blobs answer by the viewer's effective permission: a plain member gets nothing for removed, redacted, hidden-pending-review and moved-out posts, and no more than the thread page shows for a suspended author's; a moderator by group grant gets exactly what the restricted forum's thread page serves them; and no snippet carries a redacted body or a superseded revision

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48. Phase 2's exit, TKT-01M3NTSCMN, is the model.

### Settled in grooming

- **The exit criterion** (docs/09 Phase 3): "daily-driver quality for an active community, on a phone as much as on a desktop". docs/14 "Order" puts this check after the phone sheets and the polish drafts.
- **This ticket is last in dependency order.** It depends on every other `phase-3` ticket except the epics and the tracking ticket, TKT-01M3V3BVPY. That covers the server roots, the screens, the lenses, the phone sheets, the polish drafts, and the two CI-health tickets.
- **Tests are named by behaviour, and selectors come later.** The features are being groomed now in other lanes: lenses, unread and notifications, search, live and attachments, and phone pages. Each lane's merged plan fixes the selectors and the seeded data its tests use. The spec takes them from those plans and from the code they land, and does not invent its own.
- **Conventions** (docs/13, docs/14 and the Phase 3 grooming brief):
  - stored sessions (`e2e/session.ts`, `stateFile`);
  - one axe scan per test, WCAG 2.1 A and AA, as `e2e/a11y.spec.ts` scans;
  - no `/auth/*` call beyond what `paceAuth(n)` allows, with a row in `e2e/session.ts` for any spec that calls it. The plan below calls none;
  - a failing test prints its error context into the CI log (#261);
  - both projects, `phone` at 390 px and `desktop` at 1440 px, run serially against one server.
- **The invariants of docs/14** are proven end to end through the real `serve`, as Phase 2's `TestPhase2Exit*` tests are. Search, notifications, lens results and live events never widen access, and a blob is served only to a viewer who may read a post that references it. Each feature ticket proves its own invariant in its package. This ticket proves the assembled server keeps them.
- **A green summary is not evidence** (AGENTS.md). The PR quotes the step-log lines that name each test on the merged head.

### Approach

1. **Seed** (`cmd/ketju/e2e.go`). Add only what the tests need and no feature ticket seeds:
   - a post in the restricted `moderators` forum, which alice cannot read, that holds a word nothing else holds (for example "quartzite");
   - a small PNG fixture under `web/client/e2e/fixtures/` for the attachment test.

   Reuse every ordinal and session that exists.
2. **`web/client/e2e/phase3-exit.spec.ts`.** Every test below except the phone-only page picker test (item 9) runs in both projects, with one axe scan in each, and uses alice and carol from their stored sessions. A second browser context at the same width plays the other member.
   1. **Lenses.** "a member reads a thread through the unread lens and jumps to the first unread". The phone reaches the lens from its lens sheet, and the desktop from the switcher.
   2. **Lenses.** "a member narrows a thread to its starter with only-OP and returns to the full thread at the same ordinal".
   3. **Unread.** "a member finds a thread with new posts on the Unread surface, opens it at the first unread, and the badge clears once they have read it". The phone uses its tab, and the desktop the top bar.
   4. **Notifications.** "a member is notified of a mention and of a reply in a watched thread, and opening each lands on its post and clears the Alerts badge".
   5. **Search.** "a member finds a post by a word in its body, sees a snippet with the word marked, and opens the post at its permalink".
   6. **Search.** "a member's search finds nothing from a forum they cannot read". alice searches the seeded restricted word and gets the empty state.
   7. **Live.** "a reply posted by another member while the reader is mid-thread raises the new posts pill, and show brings it in without moving the page"; and "at the bottom of the thread a new reply streams in directly".
   8. **Attachments.** "a member attaches an image to a reply, sees its thumbnail in the post, and opens the full image".
   9. **Phone pages.** On a phone only, "a member moves between pages with the page picker sheet and jumps to the first unread from it". The desktop project skips this one by `isMobile`, as the feature is phone-only (docs/06 "Pages"). It is a project condition, not a flake skip.
   10. **Installable.** "the web app manifest is linked from the shell and serves the name and icons".
   11. **Settings and profile**, pulled forward from Phase 4 (docs/14 "Scope"):
       - "a member changes their page size in settings and the thread pages by it". Run it as a member that no other spec pages through, and restore the size at the end. `thread.spec.ts` assumes 25 posts a page.
       - "a member opens another member's profile and sees their posts".
3. **`cmd/ketju/phase3_exit_test.go`.** Each test starts its own `serve` through `start()`, with people seeded as `flow_test.go`'s `seedPeople` does. Each test checks two sides, and the expected answer comes from the viewer's effective permission, never from a blanket "nothing".
   - **The deny side.** A plain member, who is neither the post's author nor a moderator of its forum, must get nothing for each of these: a removed post, a redacted post, a post hidden pending review, and a thread moved to a forum they cannot read.
   - **The suspended author.** A post by a suspended author must give a plain member no more than that thread page shows them for it: the same visibility, body and author block. It is not required to be empty, since suspension does not by itself remove posts. Read what the thread page serves at implementation, and assert equality with it.
   - **The allow side.** A moderator of the restricted forum by group grant (docs/08 "Decided 2026-09-30") must get exactly what that forum's thread page serves them. That covers results from the restricted forum, and removed and hidden posts with their bodies, as the moderator view shows them. A redacted body never reaches anyone. A moderator of another forum, who lacks this forum's grant, gets the plain member's answer.

   The tests:
   - `TestPhase3ExitSearchNeverWidensAccess`: on the deny side, no result and no snippet. On the allow side, the granted moderator finds the restricted forum's post. A snippet never carries a superseded revision's text or a redacted body, whoever asks.
   - `TestPhase3ExitNotificationsNeverWidenAccess`: a mention or a watch notifies, or lists, only what the recipient may read when it is served. The granted moderator is notified of a mention in the restricted forum, and a member without access is not.
   - `TestPhase3ExitLensesNeverWidenAccess`: the mentions lens and the moderator lens follow the thread page's rule. The moderator lens is refused to a member, and serves the granted moderator its tombstones with their reasons.
   - `TestPhase3ExitLiveNeverWidensAccess`: an SSE subscriber gets a hint only for a thread they may read. The granted moderator gets one for the restricted forum, and a member does not. A session that ends, or loses access, stops receiving.
   - `TestPhase3ExitBlobsFollowTheirPosts`: a blob and its thumbnail are served only to a viewer who may read a post that references them. The granted moderator is served one from the restricted forum, and a member is refused it. The blob is content-addressed, and an upload over `limits.attachment_mb` is refused.
4. **CI** (`.forgejo/workflows/ci.yml`). Add a step to the Build and Test job, "Phase 3 exit tests, by name": `go test -race ./cmd/ketju/ -run '^TestPhase3Exit' -count=1 -v`. docs/12 "Changing CI" allows adding a step. The Playwright step's `list` reporter already names each spec test.
5. **The PR** quotes, from the merged head's step log:
   - each `TestPhase3Exit*` line;
   - each `phase3-exit.spec.ts` line, in both projects;
   - the `npm run budget` line showing the reader within 70000 B.

### Tests

The tests above are the deliverable. Beside them:

- `TestOneDoorIntoSessions` passes with its caller lists unchanged. This ticket adds no session path.
- Try these mutations, and say in the commit whether each was caught:
  - dropping the `perm` check from search's result filter, which `TestPhase3ExitSearchNeverWidensAccess` should catch;
  - fanning out an SSE hint without the read check, for the live test;
  - serving a blob without the referencing-post check, for the blob test;
  - indexing a redacted body, for the search test.

### Out of scope

- Every feature under test. A failure here goes back to the owning ticket as a bug:
  - TKT-01M29CRJ1P, TKT-01M29CTY4J, TKT-01M29CTY58, TKT-01M29CTY5T, TKT-01M29CTY6E and TKT-01M29CTY71 (lenses);
  - TKT-01M29CRJ29, TKT-01M29CRJ2T, TKT-01M29CTY7K and TKT-01M29CTY86 (unread and notifications);
  - TKT-01M29CRJ3B, TKT-01M29CTY8T, TKT-01M29CTY9D and TKT-01M3NS122F (search);
  - TKT-01M29CRJ3X and TKT-01M29CTYA0 (live);
  - TKT-01M29CXK0K and TKT-01M29CYS9W (attachments);
  - TKT-01M29CGBZD and TKT-01M29CGBYY (phone pages and manifest);
  - TKT-01M29CCC0X, TKT-01M29CXK4E and TKT-01M3V3CFMA (settings and profile).
- The polling fallback's 30 s cadence. Each feature lane tests it with its own seam. An e2e run waiting 30 s per project buys nothing.
- Real devices. As in Phase 1, a person checks those. No agent ticks such a criterion.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:08:19Z

Terva review 1694 on PR #269 (head f969334, base a599b48), medium, 'Do not require empty search results for a moderator with a group grant'. Fixed. The access tests now check two sides by the viewer's effective permission. A plain member gets nothing for removed, redacted, hidden and moved-out posts, and no more than the thread page shows for a suspended author's. A moderator by group grant gets exactly what the restricted forum's thread page serves them. The plan's step 3 and criterion 5 say so.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:18:50Z

Terva review 1699 on PR #269 (head fb9d86d, base a599b48), medium, 'Make the suspended-author exit expectation consistent'. Fixed. A suspended author's post leaves the deny list and is its own case: a plain member gets no more than the thread page serves them for it, asserted equal to that page, not empty. Criterion 5 already said 'no more than the thread page shows'.

**agent:claude-code/phase3-exit-check** at 2026-10-02T06:53:30Z

Phase A implemented on run/cfre-phase3-exit, not yet pushed, waiting on TKT-01M3S3ZZSZ (PR #348).

### What landed on the branch
- `cmd/ketju/phase3_exit_test.go`: TestPhase3ExitSearchNeverWidensAccess, TestPhase3ExitNotificationsNeverWidenAccess, TestPhase3ExitLensesNeverWidenAccess, TestPhase3ExitLiveNeverWidensAccess and TestPhase3ExitBlobsFollowTheirPosts. Each starts its own serve and builds one world (newP3World):
  - five sign-ins, the burst /auth/* allows;
  - general and the restricted vault, with a keepers group (gail) granted everything in vault only and a wardens group (otto) granted everything in general only;
  - a removed, a hidden-pending-review (flags.auto_hide_threshold 1), a redacted (ketju redact, serve stopped and started again) and a suspended author's post, each with an image;
  - a thread moved from general to vault;
  - posts in vault.

  Each expected answer is read from the viewer's own thread page, then pinned by a table of hard expectations, so a page that widened would not carry a test with it.
- `e2e/phase3-exit.spec.ts`: the plan's 15 tests in both projects, the page picker on the phone only, with one axe scan each and no /auth/* call. `e2e/session.ts` is unchanged.
- Seed: one post by mona in the restricted moderators forum holding "quartzite", dated 60 days back so no list a member reads changes. seed.json names it as restricted_word.
- CI: the "Phase 3 exit tests, by name" step after the Phase 2 one.

### Readings of the plan (not product decisions; none of docs/14's stop-rule kind was needed)
- **Search and notifications serve only posts that every reader sees as themselves.** That covers moderators too: search judges each hit by `servesPost`, and notifications follow docs/04's run decision of 2026-10-01. So for those two, "exactly what the thread page serves" a moderator means the posts that page shows as themselves. The moderator view's removed bodies show up where the merged features serve them: the moderator lens and blobs. Both tests assert this.
- **Suspended author.** Suspension does not remove posts. pat's thread page shows sam's #5 as itself with author @sam, and search, notifications, the mentions lens and blobs are asserted equal to that page.
- **Settings test member.** It runs as ada. She is the one stored-session account no other spec reads a thread page as (her specs are the admin screens and search's API calls), and the spec may not sign anyone in. Her page size is restored in a finally block.
- **"show brings it in without moving the page"** is read as docs/06 "What show does": revealed in place, with no navigation, and the new post takes focus. That focus can scroll, so the test asserts that the post in view did not move before show, and that there was no navigation and focus on the new post after.
- **Attachment fixture.** The plan asked for a small PNG fixture. `e2e/fixtures/pixel.png` (#326) already is one, so the spec reuses it.
- **Manifest.** The test checks the link and what it serves. Installability needs a profile on disk and stays in install.spec.ts.
- **Leaving a thread.** The unread and alerts tests leave a thread with the browser's Back, because a phone's thread page draws no tab bar.

### Criterion 3
The criterion says 70000 B. The maintainer raised the budget to 120000 B on 2026-10-01 (#331), and that change supersedes the 70000 B figure. The reader is about 79 KB, so the 70000 half of criterion 3 is left unticked and its text unchanged, as other lanes did.

### A real bug, filed
At 1440 px the notifications test's axe scan fails `link-in-text-block` on an unread `thread_you_watch` row. Filed as TKT-01M3XNQ772 (Tell an unread alert's thread link apart by more than colour), phase-3, owner TKT-01M29CTY86. This ticket now depends on it. The spec is not changed to avoid the failure, so the desktop notifications test fails until the fix lands.

### A second real bug, filed
In the full suite at 1440 px, the Unread surface test found the list still showing "2 new" after alice read the thread and went Back, although the server held position 3. The list GET races the read beacon's keepalive PUT, and nothing refetches the list after the PUT. Filed as TKT-01M3XP6ZRZ (Show a thread as read on Unread after going back from reading it), phase-3, owner TKT-01M29CTY7K. This ticket depends on it. The test passed twice when run alone. The spec is not changed to avoid the race.

### Mutation checks (the plan's four)
- Search's readable forums computed as an admin's: caught by TestPhase3ExitSearchNeverWidensAccess (pat and otto found vault and moved-thread results).
- An SSE hint delivered without checkAll or the read check: caught by TestPhase3ExitLiveNeverWidensAccess (pat's stream of the moved thread was sent its later lengths and did not end).
- A blob served without the referencing-post check: caught by TestPhase3ExitBlobsFollowTheirPosts (pat was served vault's blobs and thumbnails).
- A redacted body left in the index:
  - Skipping only redact's own index steps was NOT caught. serve's search catch-up reads the Redacted record on the next start and drops the row anyway.
  - With that catch-up also made to ignore Redacted, it was caught by the index check (1 row for "redactedword"). The served answers stayed clean even then, because search's judge serves only Visible entries.
