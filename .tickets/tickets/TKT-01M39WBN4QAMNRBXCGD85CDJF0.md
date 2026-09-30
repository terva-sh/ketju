---
schema: 3
id: TKT-01M39WBN4QAMNRBXCGD85CDJF0
title: Commit Playwright screenshots and the safe-area check for the mockups
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CGBY16MK20W1QATC5N7WE
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase2-run-39wbn4q
  branch: run/39wbn4q-screenshots-safe-area
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ae6a5be0267f09de1
  commit: 4918f907efd36aac88f588786a67b84e1a2137df
  session: null
  claimed_at: 2026-09-30T12:01:35Z
  expires_at: null
archive: null
created_at: 2026-09-24T14:15:52Z
updated_at: 2026-09-30T12:15:13Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

TKT-01M29CEE6V (Build the thread page with pager and unread divider) left its fourth acceptance criterion unticked: Playwright screenshots at 390 px and 1440 px, committed beside the tests and compared against the matching file in docs/mockups/, with every visible divergence listed. This ticket carries that criterion.

### Why it could not be met there
- No Playwright tests exist in the repository to commit screenshots beside. TKT-01M29CGBY1 (Add Playwright smoke tests at both viewports) brings the harness and CI's Chromium.
- The thread page is not yet inside the app shell (TKT-01M29CEE4K) and has neither the CSS system's tokens, dark theme and fonts (TKT-01M29CCC0F) nor the self-hosted IBM Plex and Newsreader. A screenshot taken now would diverge from the mockups mostly in ways those tickets remove. Listing those divergences would record work that is already scheduled, not the thread page's own.

### What was checked
The page was rendered in headless Chromium at 1440 and 390 px, locally and not committed, against a stubbed API. The layout matched the mockups' structure: the ordinal, name, badge and time header; the reply-to indicator on the right; the tombstone row; the amber divider with "last read #N"; the unread edge; the numbered pager on desktop and "Page N of M" on the phone. The console showed no errors.

### What to do
Once CGBY1, CEE4K and CCC0F are in, add Playwright captures of a seeded thread at 1440 and 390 px, light and dark. Compare them with Thread{Light,Dark}.dc.html and MobileThread{Light,Dark}.dc.html, commit them beside the test, and list every divergence in the PR.

## Acceptance criteria

- [ ] web/client/e2e/screenshots/ holds thread-{phone,desktop}-{light,dark}.png of the seeded thread with a read position partway, written by e2e/thread.spec.ts under KETJU_SCREENSHOTS=1, beside the forum-index PNGs already there
- [ ] Every visible divergence of the forum index and the thread page from Main, FrontDark, MobileFront{Light,Dark}, Thread{Light,Dark} and MobileThread{Light,Dark} in docs/mockups is listed in the pull request and in a note on this ticket, each either filed as a draft ticket or cited as an intended change
- [ ] At 390 px with a safe-area inset of 47 px top and 34 px bottom emulated through Emulation.setSafeAreaInsetsOverride, a Playwright test asserts the tab bar's and reply bar's computed padding-bottom is 34px, the phone top bar's padding-top is 47px, and their no-inset fallbacks; if the CI Chromium lacks that method, it asserts through CSS.getMatchedStylesForNode that the winning declaration for each is the env(safe-area-inset-*) one, and the PR says which ran (TKT-01M29CCC0F AC3)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). All three dependencies are done: TKT-01M29CGBY1 (Playwright), TKT-01M29CEE4K (the app shell) and TKT-01M29CCC0F (the CSS system). Refreshed against main at 5124209 on 2026-09-30: line references corrected, and the phone top bar's no-inset fallback corrected from 8px to 0px. Revised on claim, 2026-09-30, against main at 4918f90 (agent:claude-code/phase2-run-39wbn4q): the capture moved from page 1 to page 3, and the times are masked; see the Approach.

### Settled in grooming
- This ticket carries three criteria that other tickets left unticked (the note of 2026-09-24):
  - TKT-01M29CEE6V's fourth: thread-page screenshots at 390 and 1440 px, compared with the mockups;
  - TKT-01M29CCC0F's fourth: screenshots at 390 and 1440 px, light and dark, of every screen the mockups draw, meaning the front page and the thread, desktop and phone, with every divergence listed;
  - TKT-01M29CCC0F's third, as a Playwright check: at 390 px, the tab bar's and reply bar's padding comes from `env(safe-area-inset-*)`.
- Screenshots follow the pattern already in the repo. They are written only when `KETJU_SCREENSHOTS=1`, by `page.screenshot` into `web/client/e2e/screenshots/`, and committed (`forum-index.spec.ts:40-51`, `thread-list.spec.ts:77-88`). They are evidence for review, not a pixel comparison CI runs. A `toHaveScreenshot` gate lost because the seeded times are relative and fonts render differently between the dev host and Alpine's Chromium, so it would fail on noise.
- The mockups the screenshots are compared with (`docs/mockups/`):
  - front page: `Main.dc.html`, `FrontDark.dc.html`, `MobileFrontLight.dc.html`, `MobileFrontDark.dc.html`, against the committed `forum-index-{desktop,phone}-{light,dark}.png`;
  - thread: `ThreadLight.dc.html`, `ThreadDark.dc.html`, `MobileThreadLight.dc.html`, `MobileThreadDark.dc.html`.
- Settled here, as the engineering choice: the safe-area check emulates a non-zero inset through the Chrome DevTools Protocol, because Playwright has no device descriptor that sets one. The primary method is `Emulation.setSafeAreaInsetsOverride`, followed by a computed-style assertion. If the CI Chromium (Alpine's, `ci.yml:122-137`) does not implement that method, use `CSS.getMatchedStylesForNode` instead, asserting that the winning declaration for each property is the `env(safe-area-inset-*)` one. The Vitest test (`src/styles/breakpoints.test.ts:48-66`) only reads base.css as text. Unlike it, this check proves the cascade: `.shell .replybar` (`composer/composer.css:251-265`) and `.topbar.phone` (`shell/shell.css:208-221`) override base.css's left and right padding, and the check must see what finally applies.


### Approach
1. **Thread screenshots.** New `web/client/e2e/thread.spec.ts`, run in both projects, signed in as alice through the stored session (`test.use({ storageState: stateFile('alice') })`); it adds no sign-in.
   - Read `seed.thread` from `${KETJU_E2E_DIR}/seed.json`, as `a11y.spec.ts` does.
   - Set alice's read position to 53 with `setReadPosition` (`e2e/session.ts`), since flag, replies, shortcuts and a11y move it, then open `${seed.thread}?page=3`.
   - Page 3, not page 1 as the refresh planned: the mockups draw page 3 of their thread, and page 3 is the seeded thread's last page, so the composer, both pagers, the divider ("last read #53") and a reply-to indicator (#55 answers #1) all fit in one capture. Page 1's full height was 3832 px on the desktop and its PNG 270 KB; page 3's is 134 KB.
   - Test `the thread page renders the mockup's parts`: ordinal, name and time header; the reply-to indicator; the divider between #53 and #54 and the unread edge after it; the numbered pager and the composer on the desktop, "Page 3 of N" and the reply bar on the phone. The seed has no moderator post and no removed post, so the badge and the tombstone row are listed as seed divergences, not asserted.
   - Under `KETJU_SCREENSHOTS=1` it captures `thread-{phone,desktop}-{light,dark}.png` at the page's full height, as `forum-index.spec.ts` does, after `document.fonts.ready`, with `animations: 'disabled'` and `caret: 'hide'`, and with each post's `<time>` masked, because the seeded times are relative to when the seed ran.
2. **Forum-index screenshots refreshed** with the same fonts wait and a mask over the last posts' times, so no committed mockup screenshot carries a time. The four PNGs are regenerated.
3. **Safe-area check** in the same file, phone project only (`the bars pad by the safe-area insets`):
   - `page.context().newCDPSession(page)`, then `Emulation.setSafeAreaInsetsOverride` with `{top: 47, bottom: 34, left: 0, right: 0}`;
   - on `/f/general`, `padding-bottom` of `[data-shell="tabbar"]` is `34px` and `padding-top` of `[data-shell="phone-topbar"]` is `47px`; on the thread page, `padding-bottom` of `[data-shell="replybar"]` is `34px`;
   - then an empty override, and each is `0px`;
   - if the method is not found, `CSS.getMatchedStylesForNode`: the last author declaration of each property is `env(safe-area-inset-*, 0px)` (the built CSS is minified, so the space is optional), and none is `!important`;
   - the test prints `safe-area check: <method>` and records it as an annotation, so the CI step log says which ran; `KETJU_SAFE_AREA_METHOD=matched` forces the fallback so it can be exercised on a Chromium that has both.
4. **Divergences.** Render each mockup beside its screenshot and list every visible divergence in the PR and in a note here, each filed as a draft under TKT-01M29BN367, cited as an intended change, or named as owned by an existing ticket.
5. No `web/client/src` or `internal/web/spa/dist` change. A real CSS bug the check finds is filed as a draft and its criterion is left unticked.

### Tests
- `thread.spec.ts`: `the thread page renders the mockup's parts` in both projects; `the bars pad by the safe-area insets` on the phone.
- CI's Playwright step runs both. Screenshots are not written there.

### Out of scope
- Fixing the divergences found. Each becomes its own draft ticket unless an existing ticket owns it.
- Seeding a moderator post and a removed post in `cmd/ketju/e2e.go`, which is outside this ticket's e2e-and-docs scope.
- The thread list screenshots, which have no mockup.
- Real-device checks, which are TKT-01M38WTK.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:41:14Z

This ticket also carries two criteria from TKT-01M29CCC0F (Build the CSS system, themes, and breakpoints), left unticked there for the same reason: nothing in the repo runs Playwright until CGBY1. First, CCC0F's AC4: screenshots at 390 and 1440 px, light and dark, of every screen the mockups draw (docs/mockups/*.dc.html: the front page, the thread, and the mobile versions of each), with every divergence listed. Second, CCC0F's AC3 Playwright check: at 390 px, the tab bar's and reply bar's computed padding comes from env(safe-area-inset-*). base.css already carries the rules (.tabbar, .replybar, .topbar), and a Vitest source test holds them; the elements themselves arrive with CEE4K and CGBVN. Emulating a non-zero inset needs a device descriptor that sets one, or reading the rule through CSSOM.

**agent:claude-code/phase2-groom** at 2026-09-30T08:52:14Z

Plan refreshed against main at 5124209 on 2026-09-30. Nothing it plans has landed: there is no e2e/thread.spec.ts and no thread PNG. Changed: thread-list.spec.ts's screenshot block is :77-88; .topbar.phone is shell/shell.css:208-221 (:179 is now .topbar.applicant); the phone top bar is asserted through [data-shell="phone-topbar"]. Corrected an error the plan carried from 7af2c5a: .topbar.phone sets no padding-top, so the winning rule is base.css's env(safe-area-inset-top, 0px), and the no-inset fallback is 0px, not 8px; max(8px, …) applies only to its left and right padding. The spec reads seed.thread from seed.json as a11y.spec.ts does, and sets alice's read position first because flag, replies, shortcuts and a11y specs move it. The budget reference is now 47415 of 60000 B. Confirmed unchanged: forum-index.spec.ts:40-51, a11y.spec.ts's ready selector, setReadPosition (e2e/session.ts:38), ci.yml's Chromium step :122-137 (PR #194's new step is Go only), composer.css:251-265, breakpoints.test.ts:48, the eight mockups, and TKT-01M38WTK. The criteria are unchanged and still checkable.
