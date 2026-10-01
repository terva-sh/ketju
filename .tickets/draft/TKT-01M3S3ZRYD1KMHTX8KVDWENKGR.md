---
schema: 3
id: TKT-01M3S3ZRYD1KMHTX8KVDWENKGR
title: Add the front page footer with member and post counts
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:48Z
updated_at: 2026-10-01T08:30:43Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-polish
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/forum-index-desktop-*.png` with `docs/mockups/Main.dc.html` and `FrontDark.dc.html` (TKT-01M39WBN4Q, divergence F6, first listed as item 8 of the note of 2026-09-25 on TKT-01M29CEE5Y).

The mockup ends the front page with a footer: "34 members · 1,208 posts · Press ? for keyboard shortcuts · Guidelines · Markdown help". The client draws no footer. The counts are not in the API, and the guidelines and Markdown help pages do not exist.

### What to do
Add the member and post counts to the forum index's response, and draw the footer with the "?" hint, which opens the help overlay that exists. Leave Guidelines and Markdown help out until their pages exist, or file them.

## Acceptance criteria

- [ ] At 390 px and 1440 px the forum index ends with N members · M posts for a member and M posts for an anonymous viewer, M summing post_count over the listed forums and their children
- [ ] At 720 px and above the footer offers Press ? for keyboard shortcuts, which opens the help overlay; a phone shows no hint
- [ ] The forum index passes axe at 390 px and 1440 px and forum-index-*.png are refreshed
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth
- [ ] GET /forums carries members, the count of exactly the accounts the mention lookup can offer (active, handle not ending in -), to a member session only, and null to anonymous viewers and applicants (TestForumTreeMembersOnlyForMembers, TestForumTreeMembersCountsActiveAccounts; golden, dto-contract.ts and docs/07 updated)

## Implementation plan

Groomed 2026-10-01 for the Phase 3 run (docs/14), against main at a599b48.

### Settled in grooming

- **Footer content.** Decided by the run (2026-10-01, pending maintainer review), recorded in docs/06 "Thread list views (forum index)".
  - The footer reads "N members · M posts", then, at 720 px and above, "Press ? for keyboard shortcuts".
  - M is the sum of `post_count` over the forums the tree lists, children included, computed by the client. It needs no new field, and it counts only forums the viewer may read.
  - N counts exactly the accounts the mention lookup (`GET /users?prefix=`) can offer: `status = 'active'` with a handle that does not end in `-`. It is sent only to a member session, as `members` in `GET /forums`. An anonymous viewer or an applicant gets `null`, and their footer shows M alone.
  - The Guidelines and Markdown help links are left out, because neither page exists.
- **Why the count goes in `GET /forums`.** The forum index is the only screen that shows it, and it already fetches the tree. The tree is per viewer and `no-store` (`api/forums.go`), so no response cache is involved. `/me` lost, because it is read on every page and inlined in the shell. A new endpoint lost, because it would be a second request on the front page.
- **Why this is not a security question.** A member can already list every account N counts through the mention lookup, so N tells a member nothing new. Counting every active account lost (review 1705): it would include the few handles ending in `-`, which the lookup leaves out, and so tell members something the lookup does not. An anonymous viewer cannot use the lookup, so an anonymous viewer is not told N. M sums only forums the viewer may read.
- **No shortcut hint on a phone.** Shortcuts are absent there (docs/06 "Mobile"). The client gates them on `isPhone` in `keyboard/shortcuts.ts`.

### Approach

1. **Server.**
   - Add a sqlc query `CountActiveMembers`, `SELECT count(*) FROM users WHERE status = 'active' AND substr(handle, -1) <> '-'`, in `queries/identity.sql` beside `UsersByHandlePrefix`, and regenerate. A comment ties the two WHERE clauses together, and a test holds them equal.
   - `dto.ForumTree` gains `Members *int json:"members"`.
   - `forums.Service.Tree` reads the count in its second read transaction, only when `v.Member()`.
   - Update the golden, `api/dto.ts`, `api/dto-contract.ts`, and docs/07's `GET /forums` row.
2. **Client** (`ForumIndex.tsx`).
   - Add `<footer class="forum-index-foot">` after the groups. It holds `{members !== null && "N members · "}M posts`, with numbers through `toLocaleString()`, and singulars for 1.
   - When `!isPhone.value`, the footer also holds a button, "Press <kbd>?</kbd> for keyboard shortcuts", which opens the help overlay through the signal `?` sets (`keyboard/HelpOverlay`).
   - The footer is drawn only once the tree has loaded and lists a forum.
3. **CSS** (`forums.css`). Muted text at 12 px, with the mockup's gap. On a phone the footer sits above the tab bar and keeps clear of the floating new-thread button.
4. **Screenshots and build.** Refresh `forum-index-*.png`. Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** This is in the reader. Estimated at about +200 B gzip. Measure it.

### Tests

- `TestForumTreeMembersOnlyForMembers` (`internal/service/forums`): `members` is null for an anonymous viewer and for a pre-user, and a number for a member. The handler answers the same through `GET /api/v1/forums` (`internal/web/api/forums_test.go`).
- `TestForumTreeMembersCountsActiveAccounts`:
  - Pending, suspended, deactivated and anonymized accounts are not counted, and neither is an active account whose handle ends in `-`.
  - The count equals the number of distinct accounts `UsersByHandlePrefix` returns over every prefix: the parity this decision rests on.
  - An agent account is counted.
  - The count moves when a member is suspended.
- `ForumIndex.test.tsx`:
  - "the footer sums the listed forums' posts, children included".
  - "it names the members when the tree carries them, and leaves them out when null".
  - "the shortcut hint opens the help overlay above 720 px, and a phone shows none".
- `e2e/forum-index.spec.ts`: at 390 px and 1440 px, alice's footer reads `\d+ members · \d+ posts`, and M equals the sum of the rows' `data-posts`. The desktop shows the hint and the phone does not. One axe scan per test, as the spec already does.
- **Never widens access.** The restricted Moderators forum that alice cannot read adds nothing to her M (e2e above, against the seed). An anonymous request's `members` is null (Go test above).

### Out of scope

- Guidelines and Markdown help pages. No ticket owns them and no design document asks for them, so none is filed.
- The mockup's "12 unread posts across 5 threads since Tuesday evening" summary, which nothing in the divergence list asked for.

## Notes

**agent:claude-code/phase3-groom-polish** at 2026-10-01T07:55:58Z

run decision: pending maintainer review. The footer reads 'N members · M posts', then 'Press ? for keyboard shortcuts' at 720 px and above. M is summed by the client over the forums the tree lists, so no new field is needed. N needs one: members on GET /forums, the count of status-active accounts, sent to a member session only and null otherwise. A member can already list those accounts through the mention lookup, and an anonymous viewer cannot, so the count widens nothing. The question was checked against the docs/14 stop rule and is not a security posture question. Guidelines and Markdown help are left out. Recorded in docs/06 'Thread list views (forum index)' with the alternatives.

**agent:claude-code/phase3-groom-polish** at 2026-10-01T08:30:43Z

Terva review 1705 on PR #269 (head 8aa50f3, base a599b48), medium, 'Reassess whether the member count widens disclosure'. Fixed by making the parity exact. N now counts the accounts the mention lookup can offer, active with a handle not ending in '-', and a test holds the two WHERE clauses equal. Counting every active account is recorded as the alternative that lost, in docs/06 and in the plan. This supersedes the run-decision note's definition of N as 'status-active accounts'.
