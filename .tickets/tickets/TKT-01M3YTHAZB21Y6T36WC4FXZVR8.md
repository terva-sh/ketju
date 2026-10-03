---
schema: 3
id: TKT-01M3YTHAZB21Y6T36WC4FXZVR8
title: Show who quoted a post on the thread page
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3YME9AZ4RJACSMV8VX4JMZJ
blocks_on: none
references:
  - ref: pr:362
    path: null
claim:
  actor: agent:claude-code/phase4-run-thazb
  branch: run/thazb-quoted-by
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ae0919645aa3f5e55
  commit: bb6faf740f9d86ddce3e467d62fea3ec387c6ec4
  session: null
  claimed_at: 2026-10-03T00:58:41Z
  expires_at: null
archive: null
created_at: 2026-10-02T17:28:04Z
updated_at: 2026-10-03T06:13:59Z
created_by:
  id: agent:claude-code/phase4-groom-correctness
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-thazb
  name: ""
extensions: {}
---

## Description

The display half of TKT-01M3YME9AZ (Record quotes as backlinks, as docs/06 promises). That ticket records backlinks and serves them on each post as `quoted_by`. This one draws them as docs/06 "Post anatomy" decides (run decision 2026-10-02, pending maintainer review): a "Quoted by #51 #60" line between a post's body and its footer, at both widths. Each link behaves as the reply-to indicator does.

Filed 2026-10-02 by the Phase 4 correctness grooming lane, because the client half queues for the committed dist and the server half should not wait with it.

## Acceptance criteria

- [x] A post with a non-empty quoted_by shows 'Quoted by' and one #N link per ordinal, at most ten, then 'and N more'; nothing shows for an empty list or a tombstone; the Post.test.tsx cases pass
- [x] A backlink scrolls to a quoting post on the page and opens PostPopover for one off it, with focus back on the link after Escape, through the existing a[data-reply-to] delegate; the ThreadScreen.test.tsx cases pass
- [x] Playwright at 390 px and 1440 px shows the line and opens the popover for an off-page quoting post, with one axe scan per test and no violations
- [ ] The committed dist is regenerated with go generate ./internal/web/spa and the reader bundle stays within the 120000 B gzip budget, its growth recorded in the PR

## Implementation plan

Filed and groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15e, by the correctness grooming lane. It is the display half of TKT-01M3YME9AZ (Record quotes as backlinks, as docs/06 promises), whose description asks to "show them wherever docs/06 draws them". The display is split out so the server half does not wait in the dist queue.

### Settled in grooming

- **What is drawn.** docs/06 "Post anatomy" has the run decision of 2026-10-02, pending maintainer review: a "Quoted by #51 #60" line between the body and the footer, at both widths. The ordinals are ascending. Each is a link that behaves as the `↩` indicator does. Past ten, the line ends with "and N more" as plain text. The line is absent when `quoted_by` is empty. The server already empties `quoted_by` for a tombstone and filters removed quoting posts out (TKT-01M3YME9AZ), so the client applies no visibility rule of its own.
- **The link widget.** `ThreadScreen.tsx` already delegates clicks on `a[data-reply-to], a.quote-source`. It scrolls to the post when it is on the page and otherwise opens `PostPopover`. The backlink anchors carry `data-reply-to={n}`, so they reuse that path with no new handler, and focus returns to the link on close as for a reply-to (`PostPopover.tsx`). Alternatives: a new attribute and selector lost, because it would duplicate the delegate for the same behaviour.
- **Bundle.** The line belongs in the reader bundle. `Post.tsx` is in the reader, and a lazy chunk for one line of links would cost more in loader code than it saves. The estimate is under 300 B gzip: a small component, a `.quoted-by` CSS rule, and the strings. The reader is about 79200 B of the 120000 B budget (docs/15 "Invariants"), so this stays well inside it.
- **Targets.** Each ordinal link uses the `hit` class, as the footer's links do, so it has a 44 px target on phones (docs/06 "Phones"). The links are separated by spaces inside a `<p>`, so they wrap.

### Approach

1. `web/client/src/components/thread/Post.tsx`: add `QuotedBy({ post })`, rendered after `<Attachments>` and before `<footer>` when `post.quoted_by.length > 0` and the post is not a tombstone (`isTombstone`). It renders `<p class="quoted-by">Quoted by <a class="hit" href="#post-N" data-reply-to={N}>#N</a> …</p>`, with at most ten links and then " and N more".
2. `thread.css`: a `.quoted-by` rule for the line's size and colour, following `.post-footer`'s secondary text.
3. `go generate ./internal/web/spa`, and commit the dist. This PR queues for the dist as docs/14 describes.

### Tests

- Vitest in `Post.test.tsx`:
  - `QuotedBy` renders nothing for `[]`;
  - it renders one link per ordinal up to ten;
  - for twelve ordinals it ends "and 2 more";
  - it renders nothing on a tombstone.
- Vitest in `ThreadScreen.test.tsx`: clicking a backlink to a post on the page scrolls to it, and clicking one off the page opens `PostPopover`. Escape returns focus to the link.
- Playwright (`e2e/`), one axe scan per test, at 390 px and 1440 px: a thread where post 3 quotes post 1 shows "Quoted by #3" on post 1. Tapping it at 390 px, with post 3 on the next page, opens the popover. Use stored sessions. If the spec calls `/auth/*`, add `paceAuth(n)` and a row in `e2e/session.ts`.

### Out of scope

- Recording and serving `quoted_by`: TKT-01M3YME9AZ.
- The Digest lens (docs/06, "Later").

## Notes

**agent:claude-code/phase4-run-thazb** at 2026-10-03T01:26:32Z

Phase A done on run/thazb-quoted-by (unpushed, commit f8c62d50 on main 5f50eb27). QuotedBy in Post.tsx after Attachments, before the footer; links carry data-reply-to and .hit, so the existing delegate scrolls or opens PostPopover. Departures from the plan: (1) the ThreadScreen cases live in src/screens/ThreadPage.test.tsx, where the reply-to and quote-source follow tests already are; (2) on a phone the line uses gap 28px 32px, margin-top 24px and padding-bottom 20px, because e2e/targets.spec.ts requires each target to be its own 21 px either side of its centre and a one-digit ordinal is about 14 px wide (a first try at 24px failed that spec on #2); (3) the e2e seed's #44 (page 2) now quotes #1 as #2 does (cmd/ketju e2e.go seedQuotesFirst), so the Playwright spec e2e/quoted-by.spec.ts has an off-page backlink; the full Playwright suite passes with it (292 passed). Reader bundle measured 79983 B before, 80135 B after (+152 B gzip). Dist not committed: waiting for the dist queue.

**agent:claude-code/phase4-run-thazb** at 2026-10-03T06:13:59Z

Phase B: rebased onto origin/main 1198e2b8 without conflict. Regenerated the dist: reader bundle 82880 B -> 83030 B gzip (+150 B) of 120000 B (the Phase A figure, 79983 -> 80135, was measured on the older base). TestServedGoldens passes unchanged, so neither quoted_by nor the seed change alters a served golden. go test -race -p 4 ./... passed; the full Playwright suite passed (301 passed, 25 skipped) on the rebased branch. Criterion 2's cases live in ThreadPage.test.tsx rather than ThreadScreen.test.tsx, as the implementation note records.
