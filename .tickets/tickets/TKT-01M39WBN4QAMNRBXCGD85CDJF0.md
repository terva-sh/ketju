---
schema: 3
id: TKT-01M39WBN4QAMNRBXCGD85CDJF0
title: Commit Playwright screenshots and the safe-area check for the mockups
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-24T14:15:52Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
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

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). All three dependencies are done: TKT-01M29CGBY1 (Playwright), TKT-01M29CEE4K (the app shell) and TKT-01M29CCC0F (the CSS system).

### Settled in grooming
- This ticket carries three criteria that other tickets left unticked (the note of 2026-09-24):
  - TKT-01M29CEE6V's fourth: thread-page screenshots at 390 and 1440 px, compared with the mockups;
  - TKT-01M29CCC0F's fourth: screenshots at 390 and 1440 px, light and dark, of every screen the mockups draw, meaning the front page and the thread, desktop and phone, with every divergence listed;
  - TKT-01M29CCC0F's third, as a Playwright check: at 390 px, the tab bar's and reply bar's padding comes from `env(safe-area-inset-*)`.
- Screenshots follow the pattern already in the repo. They are written only when `KETJU_SCREENSHOTS=1`, by `page.screenshot` into `web/client/e2e/screenshots/`, and committed (`forum-index.spec.ts:40-51`, `thread-list.spec.ts:78-85`). They are evidence for review, not a pixel comparison CI runs. A `toHaveScreenshot` gate lost because the seeded times are relative and fonts render differently between the dev host and Alpine's Chromium, so it would fail on noise.
- The mockups the screenshots are compared with (`docs/mockups/`):
  - front page: `Main.dc.html`, `FrontDark.dc.html`, `MobileFrontLight.dc.html`, `MobileFrontDark.dc.html`, against the committed `forum-index-{desktop,phone}-{light,dark}.png`;
  - thread: `ThreadLight.dc.html`, `ThreadDark.dc.html`, `MobileThreadLight.dc.html`, `MobileThreadDark.dc.html`.
- Settled here, as the engineering choice: the safe-area check emulates a non-zero inset through the Chrome DevTools Protocol, because Playwright has no device descriptor that sets one. The primary method is `Emulation.setSafeAreaInsetsOverride`, followed by a computed-style assertion. If the CI Chromium (Alpine's, `ci.yml:122-137`) does not implement that method, use `CSS.getMatchedStylesForNode` instead, asserting that the winning declaration for each property is the `env(safe-area-inset-*)` one. The Vitest test (`src/styles/breakpoints.test.ts:48-66`) only reads base.css as text. Unlike it, this check proves the cascade: `.shell .replybar` (`composer/composer.css:251-265`) and `.topbar.phone` (`shell/shell.css:179-191`) override base.css's left and right padding, and the check must see what finally applies.

### Approach
1. **Thread screenshots.** New `web/client/e2e/thread.spec.ts`, run in both projects, signed in as alice (`test.use({ storageState: stateFile('alice') })`):
   - open `${seed.thread}?page=1` and wait for `#post-2 details.quote .quote-source`, as `a11y.spec.ts` does;
   - under `KETJU_SCREENSHOTS=1`, capture `thread-${project}-${theme}.png` for light and dark into `e2e/screenshots/`, using the same full-height viewport handling as `forum-index.spec.ts:40-51`;
   - carol has read the whole seeded thread and alice has read none of it. For alice, set a read position partway with `setReadPosition` (`e2e/session.ts:38`) before the capture, so the amber "last read #N" divider and the unread edge the mockups draw are in the picture.
2. **Safe-area check.** In the same file, phone project only:
   - open a CDP session with `page.context().newCDPSession(page)` and send `Emulation.setSafeAreaInsetsOverride` with `{insets: {top: 47, bottom: 34, left: 0, right: 0}}`;
   - on the thread list, assert the computed `padding-bottom` of `[data-shell="tabbar"]` is `34px` and the `padding-top` of the phone `.topbar` is `47px` (`max(8px, 47px)`);
   - on the thread page, assert the `padding-bottom` of `[data-shell="replybar"]` is `34px`;
   - then clear the override and assert the fallbacks: `0px`, `8px` and `0px`.
   - If the method is missing (CDP answers "method not found"), use the `CSS.getMatchedStylesForNode` fallback settled above, and say in the PR which method ran.
3. **Run and commit.** Run `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts e2e/forum-index.spec.ts`, commit the four new thread PNGs, and refresh the forum-index PNGs if they changed.
4. **Divergences.** Open each mockup beside its screenshot. List every visible divergence in the PR description, one line each, grouped by screen and theme: layout, spacing, colour, type, missing or extra elements. Copy the same list into a note on this ticket, because a PR description is not in the repository. A divergence that is a real defect is filed as a draft ticket under TKT-01M29BN367 and named in the list. One that the design has since changed on purpose cites where.
5. No `web/client/src` change is expected, so the dist and the bundle budget are untouched. If a defect is fixed here instead of being filed, rebuild the dist with `go generate ./internal/web/spa` and keep it under the 60000 B gzip budget (`web/client/budget.json`).

### Tests
- `thread.spec.ts`: `the thread page renders the mockup's parts` (ordinal, name, badge and time header, reply-to indicator, tombstone row, divider, pager or "Page N of M") in both projects; the capture runs under `KETJU_SCREENSHOTS=1`.
- `thread.spec.ts` (phone): `the bars pad by the safe-area insets`.
- CI's Playwright step passes. Screenshots are not written there.

### Out of scope
- Fixing the divergences found. Each becomes its own draft ticket unless it is a one-line fix.
- The thread list screenshots, which are already committed and have no mockup.
- Real-device checks, which are TKT-01M38WTK.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T14:41:14Z

This ticket also carries two criteria from TKT-01M29CCC0F (Build the CSS system, themes, and breakpoints), left unticked there for the same reason: nothing in the repo runs Playwright until CGBY1. First, CCC0F's AC4: screenshots at 390 and 1440 px, light and dark, of every screen the mockups draw (docs/mockups/*.dc.html: the front page, the thread, and the mobile versions of each), with every divergence listed. Second, CCC0F's AC3 Playwright check: at 390 px, the tab bar's and reply bar's computed padding comes from env(safe-area-inset-*). base.css already carries the rules (.tabbar, .replybar, .topbar), and a Vitest source test holds them; the elements themselves arrive with CEE4K and CGBVN. Emulating a non-zero inset needs a device descriptor that sets one, or reading the rule through CSSOM.
