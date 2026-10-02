---
schema: 3
id: TKT-01M3S3ZRYD1KMHTX8KVDWENKGR
title: Add the front page footer with member and post counts
type: task
status: done
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
references:
  - ref: pr:344
    path: null
claim: null
archive: null
created_at: 2026-09-30T12:17:48Z
updated_at: 2026-10-02T22:47:32Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found comparing `web/client/e2e/screenshots/forum-index-desktop-*.png` with `docs/mockups/Main.dc.html` and `FrontDark.dc.html` (TKT-01M39WBN4Q, divergence F6, first listed as item 8 of the note of 2026-09-25 on TKT-01M29CEE5Y).

The mockup ends the front page with a footer: "34 members · 1,208 posts · Press ? for keyboard shortcuts · Guidelines · Markdown help". The client draws no footer. The counts are not in the API, and the guidelines and Markdown help pages do not exist.

### What to do
Add the member and post counts to the forum index's response, and draw the footer with the "?" hint, which opens the help overlay that exists. Leave Guidelines and Markdown help out until their pages exist, or file them.

## Acceptance criteria

- [x] At 390 px and 1440 px the forum index ends with N members · M posts for a member and M posts for an anonymous viewer, M summing post_count over the listed forums and their children
- [x] At 720 px and above the footer offers Press ? for keyboard shortcuts, which opens the help overlay; a phone shows no hint
- [x] The forum index passes axe at 390 px and 1440 px and forum-index-*.png are refreshed
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 70000 B gzip, and the PR states the reader's growth
- [x] GET /forums carries members, the count of exactly the accounts the mention lookup can offer (active, handle not ending in -), to a member session only, and null to anonymous viewers and applicants (TestForumTreeMembersOnlyForMembers, TestForumTreeMembersCountsActiveAccounts; golden, dto-contract.ts and docs/07 updated)

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

**agent:claude-code/phase3-s3zryd** at 2026-10-01T19:32:37Z

run decision: pending maintainer review. The hint is a button that opens the shortcut list, and ? opens the list on the forum index as well, so "Press ? for keyboard shortcuts" is true there. The list opened from the index leads with "These keys work on a thread page." (HelpOverlay's new `lead` prop), since every other key in it is a thread page's. Alternatives that lost: the hint as plain text, because ? did nothing outside a thread page and the hint would have been false; front-page shortcuts of their own (j and k between forums), because nothing asked for them and the list would then differ by screen. Recorded in docs/06 "Thread list views (forum index)".

Implementation, prepared on run/s3zryd-front-footer (source only; the dist waits for the orchestrator):
- Server: `CountActiveMembers` in queries/identity.sql beside `UsersByHandlePrefix`, with a comment tying the two WHERE clauses. `forums.Service.Tree` reads it in its first read transaction, only when `v.Member()`, so an empty tree carries it too. `dto.ForumTree.Members *int`, goldens regenerated (anon null, member 34). dto-contract.ts needed no edit: it imports the goldens, and tsc passes with `members: number | null` required in dto.ts. docs/07's GET /forums row names the field.
- Caching: GET /forums is built per request by `forums.Service.Tree`, answered `no-store` with no ETag, and goes through neither the response cache (respcache, thread pages only) nor the shell. No key or split was needed. `TestForumTreeMembersNeverShared` (internal/web/api) alternates member and anonymous requests and holds every answer no-store with no ETag, and every anonymous answer null.
- Tests: `TestForumTreeMembersOnlyForMembers` (service and handler: anonymous, pre-user and an inactive signed-in account get null; a member gets the count), `TestForumTreeMembersCountsActiveAccounts` (pending, suspended, deactivated, anonymized and a handle ending in '-' not counted; an agent counted; equal to the distinct accounts UsersByHandlePrefix returns over all 36 first characters; moves on a suspension).
- Client: `IndexFoot` in ForumIndex.tsx; numbers through toLocaleString with the viewer's locale; `data-members` and `data-posts-total` carry the raw numbers for tests. Phone: the footer keeps 88 px clear on the right for the floating button.
- e2e: forum-index.spec.ts gains an anonymous footer test and a member footer test (390 px: no hint, ? inert, clear of the FAB, axe on the footer; 1440 px: hint opens the list, Escape returns focus, ? opens it, axe with the list open). forum-index-*.png refreshed.
- Reader bundle measured with go generate: 66220 B before, 66738 B after (+518 B gzip) of 120000 B.

**agent:claude-code/phase3-s3zryd** at 2026-10-01T19:33:21Z

For the PR body (orchestrator, 2026-10-01): binding ? on the forum index adds a single-key shortcut to a second page. TKT-01M3V9DWTN (Let a member turn off the single-key shortcuts (WCAG 2.1.4), draft, no milestone) owns the missing off switch. TKT-01M3S3ZZSZ left the / search key out until that ticket lands; ? is kept here because the hint's words would otherwise be false, and ? only opens a dialog that Escape closes. Phase B: name both in the PR body so the reviewer sees the tradeoff was weighed.

**agent:claude-code/phase3-s3zryd** at 2026-10-02T03:51:03Z

Terva review 1887 on PR #344 (head 3a231f6, base 14074f8), medium, 'Provide a safeguard before enabling the global character-key shortcut'. Fixed: no key is bound on the forum index. The footer's button reads 'Keyboard shortcuts' and opens the list, which leads with 'These keys work on a thread page, where ? opens this list.' This supersedes the run-decision note that bound ? on the index, and docs/06 records the new decision with the alternatives that lost. Criterion 2 is left unticked: it asks for the words 'Press ? for keyboard shortcuts', which would be false with no key bound. It can be met once TKT-01M3V9DWTN (Let a member turn off the single-key shortcuts (WCAG 2.1.4)) adds the off switch. The reader bundle is now 79006 B, +515 B over main's 78491 B.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:32Z

Criterion 2 met after close, 2026-10-02: TKT-01M3V9DWTN (Let a member turn off the single-key shortcuts) merged via PR #370 (d0bb743c). The forum index binds ? while shortcuts are on and the footer reads Press ? for keyboard shortcuts at 720 px and above.

## Summary

The forum index ends with 'N members · M posts', and from 720 px up a 'Keyboard shortcuts' button that opens the shortcut list. Server: GET /forums carries members, from the new CountActiveMembers query. It counts exactly the accounts UsersByHandlePrefix can offer (active, handle not ending in '-'). A member session gets the number; an anonymous viewer, a pre-user or an inactive account gets null. The response is built per request, no-store, with no ETag, and goes through no cache. Tests: TestForumTreeMembersOnlyForMembers (service and handler), TestForumTreeMembersCountsActiveAccounts and TestForumTreeMembersNeverShared. Goldens, dto.ts (members required) and the docs/07 row are updated. Client: IndexFoot in ForumIndex.tsx sums post_count over the listed forums and their children, and HelpOverlay gains a lead line. e2e/forum-index.spec.ts covers both widths with axe, and forum-index-*.png are refreshed. Run decision, pending maintainer review, in docs/06: no key is bound on the index (WCAG 2.1.4, Terva review 1887), so criterion 2's 'Press ? for keyboard shortcuts' waits on TKT-01M3V9DWTN. Reader bundle: 78491 B before, 79006 B after (+515 B gzip) of 120000 B.
