---
schema: 3
id: TKT-01M3S3Z9E1RX86KA4WX4DQKWBJ
title: Match the phone post header and footer to docs/06 Mobile
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3S3ZZQDXM4RX4HK6V5X8VX7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:32Z
updated_at: 2026-10-01T19:03:49Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-s3z9e1
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/thread-phone-{light,dark}.png` with `docs/mockups/MobileThread{Light,Dark}.dc.html` (TKT-01M39WBN4Q, divergences P2 and P3). P3 was filed as TKT-01M3S3Z9GT (Order the phone post footer reply, quote as docs/06 says). Grooming merged it into this ticket on 2026-10-01 and archived it.

- **P2, the header.** docs/06 "Mobile" says: "One column, 30 px avatars, author and time stacked". The mockup draws the avatar, the name and the role badge on the first line, and "#61 Tue 14:02" on the second. On the phone the client draws one line, "#51 Name time", the same as the desktop header (`web/client/src/components/thread/Post.tsx`, the `post-header`).
- **P3, the footer.** docs/06 "Mobile" says: "Footer actions are reply, quote, and the reply expander". The mockup draws the expander, then "reply quote … ⋯". On the phone the client draws "quote reply … ⋯", the desktop order from docs/06 "Post anatomy".

### What to do
At the phone breakpoint, stack the header and put reply before quote in the footer. Leave the desktop as it is. Refresh `thread-phone-*.png` with `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts`.

## Acceptance criteria

- [ ] At 390 px a post's footer reads the reply expander when offered, reply, quote, then More at the right edge, in DOM and focus order; at 1440 px the order is unchanged (Post.test.tsx, e2e/thread.spec.ts)
- [ ] The thread page passes axe at 390 px and 1440 px in both themes (e2e/a11y.spec.ts), e2e/targets.spec.ts passes, and thread-phone-{light,dark}.png are refreshed
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth
- [ ] At 390 px a post's header draws the 30 px avatar, author and role badge on the first line, and ordinal, time, edited mark and then the reply-to indicator on the second, with the reply-to at that line's right end so focus order follows reading order. The body takes the full width below. At 1440 px the header is one line as before (e2e/thread.spec.ts, by geometry and by Tab from #post-55's ordinal)

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **Merged with TKT-01M3S3Z9GT** (Order the phone post footer reply, quote as docs/06 says), which is archived. Both change one post at the phone breakpoint in `components/thread/Post.tsx` and `thread.css`, and both refresh `thread-phone-*.png`. Two PRs would queue for the dist one after the other to redraw the same 390 px post.
- **docs/06 "Mobile", Posts**: one column, 30 px avatars, author and time stacked, the reply-to indicator at the right of the header. Footer actions are reply, quote and the reply expander, and link, flag, unread from here and history collapse under "more".
- **Order of the phone footer.** Decided by the run (2026-10-01, pending maintainer review), recorded in docs/06 "Mobile". The docs/06 sentence names what stays in the footer, not its order. The footer reads the expander when offered, then reply, then quote, then More at the right edge, as the phone mockup draws it. The desktop order is unchanged (docs/06 "Post anatomy").
- **Header at 390 px**, from the same docs/06 line and the phone mockup (`docs/mockups/generate.py`, `mobile_thread().post`). The avatar sits in the header's first row. Author and role badge share one line, with ordinal, time, edited mark and the removal mark on the next. The reply-to sits at the right end of the second line, after the ordinal, time and marks, so the header's two focusable items, the ordinal then the reply-to, read left to right on one line in focus order (requirement carried from review 1725). The body takes the full width below.
- **Layout switches in CSS, and one set of components renders at every size** (docs/06 "Breakpoints").
- **Times are absolute** (`PostTime`; docs/06 "Post anatomy", maintainer 2026-10-01).

### Approach

1. **Header markup** (`Post.tsx`, `post-header`) is unchanged. Its DOM order stays ordinal, author, badge, time, edited mark, removal mark, then reply-to. So the desktop and assistive technology read it as they do now. Focus order is the same at both widths, because the ordinal and the reply-to are the header's only focusable items and keep their relative order. Grouping the header into wrapper spans lost: the desktop would then need CSS `order` to put the ordinal first, and its reading order would no longer match what it shows.
2. **Phone CSS** (`thread.css`, the `max-width: 719.98px` block):
   - `.post` becomes a grid of `30px 1fr`.
   - `.post-main` becomes `display: contents`. The avatar sits in column 1 of row 1 and the header in column 2. The body, attachments, confirmation, footer, replies and refusal span `1 / -1`.
   - The header stays a wrapping flex row. CSS `order` puts `.author` and `.role-badge` first. A full-width break follows, a `::after` with `flex-basis: 100%`. Then come `.ordinal`, `time`, `.edited` and `.deleted-mark`, and last `.reply-to`, which sits at the right end of that second line with `margin-left: auto`. It does not go on the first line, because the ordinal on the second line comes before it in focus order (review 1725).
   - `.post--recessed` (the reply expander's copies) and `.popover-post` keep their own layouts. Check both in the phone screenshot.
3. **Footer order** (`Post.tsx` footer). When `isPhone.value`, render reply before quote. The footer already branches on `isPhone` for `PostMore`, so DOM order, focus order and visual order stay the same. The expander leads at both widths. On a phone `.post-more` takes `margin-left: auto`.
4. **Screenshots.** Refresh with `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts`, then compare by eye with `MobileThread{Light,Dark}.dc.html` and `Thread*.dc.html`. The desktop screenshots should not change.
5. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`, and state the reader's growth in the PR.

**Reader bundle.** All of this is in the reader bundle, since the thread page is the reader. Estimated at about +120 B gzip, mostly CSS. Measure it.

### Tests

- `Post.test.tsx`:
  - "orders the phone footer expander, reply, quote, then More": with `isPhone` set, read the order of the footer's controls.
  - "keeps the desktop footer order": expander, quote, reply, link, flag and history.
  - "keeps the header's DOM order": ordinal, author, badge, time, then edited mark.
- `e2e/thread.spec.ts` "the thread page renders the mockup's parts":
  - At 390 px, on a post with a role badge (`#post-56`, seeded by TKT-01M3S3ZZQD), `.author`'s box is above `.ordinal`'s, the badge's box shares `.author`'s line, `time` shares `.ordinal`'s line, and `.post-body` starts at the post's left padding rather than beside the avatar.
  - At 390 px, on `#post-55`, which answers #1, `.reply-to`'s box shares `.ordinal`'s line and lies right of it, and Tab from the ordinal reaches the reply-to next.
  - The footer's controls are reply then quote.
  - At 1440 px the header is one line, with `.author` and `.ordinal` on the same y, and the footer is quote then reply.
- `e2e/a11y.spec.ts`'s thread scans at both widths, in both themes, unchanged and passing.
- `e2e/targets.spec.ts` passes: the reordered phone footer keeps every target at 44 px.

### Out of scope

- The seeded moderator post, tombstone and edit that the screenshots need: TKT-01M3S3ZZQD, a dependency.
- Attachments as the mockup draws them: TKT-01M29CXK0K and TKT-01M29CYS9W.
- Any desktop change.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:55:58Z

run decision: pending maintainer review. On the phone the footer reads the reply expander when it is offered, then reply, then quote, then More. The docs/06 Mobile sentence 'Footer actions are reply, quote, and the reply expander' is read as naming what stays in the footer, not its order. The header puts the avatar in its first row, with author and badge on one line beside it and ordinal and time on the next. Recorded in docs/06 Mobile with the alternatives. Grooming also merged TKT-01M3S3Z9GT into this ticket and archived it, and made this ticket depend on TKT-01M3S3ZZQD, because the seeded moderator post is what puts a role badge on page 3 to check.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:43:28Z

Plan revised during PR #269, before review round 5. The header's DOM order stays as it is, and the phone layout is done with CSS order and a line break, not with wrapper spans. Wrapper spans would have needed order on the desktop, so the desktop's reading order would no longer match what it shows.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T09:37:20Z

Requirement carried from review 1725 (2026-10-01; PR #273, head b2fb5bb, base 6dfd01d). This is review round 6, carried over from PR #269, and it overrides the plan. Finding, medium: 'Keep phone header focus order aligned with its visual order'. The plan puts .reply-to at the right of the first line and .ordinal on the second line, so Tab would go from the second line back up to the first. What the implementation must do: at 390 px put .reply-to at the right end of the second line, after the ordinal, time, edited mark and removal mark (margin-left: auto on that line), not on the first line. The header's two focusable items, the ordinal and then the reply-to, then sit on one line in reading order, and the DOM order stays as the plan says. The e2e assertion of approach step 2 changes with it: at 390 px the .reply-to box shares the .ordinal line. Add a check that Tab from #post-55's ordinal reaches its reply-to next (#55 answers #1, so it carries one).

**agent:claude-code/phase3-groom-polish** at 2026-10-01T10:25:03Z

Requirement carried from review 1737 (2026-10-01; PR #282, head e9be93d, base 3890b38). Finding, medium: 'Align the phone header plan with the carried focus-order requirement'. Resolved in the plan itself, not left as a conflict: approach step 2, the settled bullet, the e2e geometry test and criterion 4 all put .reply-to at the right end of the second line, after the ordinal, time and marks. The implementation must follow that. The plan and review 1725's carried requirement now agree, and nothing in this ticket still puts the reply-to on the first line.

**agent:claude-code/phase3-s3z9e1** at 2026-10-01T17:10:14Z

Implemented on run/s3z9e1-phone-post as the plan says, with the reply-to at the right end of the second line (reviews 1725, 1737). The phone grid applies to '.posts > article.post' only, so tombstones (a div), the reply expander's copies (inside .replies) and the popover post keep their flex layouts. A canonical post's open replies now take the full width (margin-left 0); a tombstone's keep the -42px pull. PostControls.test.tsx asserted the old phone order quote, reply and now asserts reply, quote. docs/06 Mobile's run-decision paragraph now names the reply-to's place. Desktop screenshots are not refreshed: the layout is unchanged and their only differences were incidental (unread count). Reader bundle measured with a local go generate: 63878 B before, 64024 B after (+146 B of 70000 B). Dist not committed yet: the lens lane holds it (docs/14 'Client lanes may prepare').

## Summary

At 390 px a post on the thread page is a grid (thread.css, '.posts > article.post'): the 30 px avatar beside a two-line header, author and role badge over ordinal, time, edited and removal marks, with the reply-to at the right end of the second line so Tab goes ordinal then reply-to on one line (reviews 1725, 1737). The header's DOM order is unchanged; CSS order and a ::after line break stack it. The body, attachments, footer and replies take the full width. The phone footer renders reply before quote (Post.tsx), so it reads expander, reply, quote, More; the desktop keeps quote first. Tombstones, the reply expander's copies and the popover keep their layouts. Tests: Post.test.tsx (phone and desktop footer order, header DOM order), PostControls.test.tsx (phone order), e2e/thread.spec.ts (geometry on #post-56 and #post-55, Tab from #55's ordinal to its reply-to, one-line header and quote-then-reply at 1440 px). thread-phone-{light,dark}.png refreshed; desktop screenshots unchanged. docs/06 Mobile's run-decision paragraph names the reply-to's place. Reader bundle 66220 B to 66361 B gzip (+141 B).
