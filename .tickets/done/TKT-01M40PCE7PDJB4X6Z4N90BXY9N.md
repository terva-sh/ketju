---
schema: 3
id: TKT-01M40PCE7PDJB4X6Z4N90BXY9N
title: Give a removed post's footer buttons the contrast its links have
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:404
    path: null
claim: null
archive: null
created_at: 2026-10-03T10:53:58Z
updated_at: 2026-10-03T13:16:11Z
created_by:
  id: agent:claude-code/phase4-run-mg0nx
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-mg0nx
  name: ""
extensions: {}
---

## Description

Found by the Phase 4 exit check (TKT-01M3YMG0NX), whose e2e/phase4-exit.spec.ts archive test fails its axe scan at 1440 px.

axe reports `color-contrast` (serious) on the footer buttons of a removed post: quote, reply, "unread from here" and restore. They render `--muted` (#65707f) on the removed post's `--surface2` (#eef1f6) at 12.5 px, which is 4.44:1. WCAG 2.1 SC 1.4.3 asks 4.5:1 for text that size.

It is not specific to archiving. A live thread with a post its author deleted shows the same failure to a moderator, who is offered those buttons on it.

### Cause

In `web/client/src/components/thread/thread.css`, the `.post.removed` rule recolours the footer's links to `--ink2` (#4a5568, 7.0:1 on `--surface2`) but not its buttons. The buttons keep `.post-footer button { color: var(--muted) }` from `web/client/src/composer/composer.css`. On a phone the buttons sit in the collapsed More menu, so only the desktop scan sees them.

### Fix

Add `.post.removed .post-footer button` to the selector list of that `color: var(--ink2)` rule, and regenerate the committed dist.

This blocks a Phase 4 exit criterion (TKT-01M3YMG0NX criterion 1), so it carries the `phase-4` milestone, and it lands in the exit check's PR, which holds the dist last.

## Acceptance criteria

- [x] At 1440 px, the footer buttons of a removed post (quote, reply, unread from here, restore) meet 4.5:1 against the removed post's background, in the light and dark themes
- [x] e2e/phase4-exit.spec.ts's archive test, which scans a thread holding a deleted post with axe at 1440 px, fails without the fix and passes with it
- [x] The committed dist is regenerated and the reader bundle stays within 120000 B gzip

## Implementation plan

Add .post.removed .post-footer button to the selector list of the .post.removed rule in web/client/src/components/thread/thread.css that recolours to --ink2 (specificity 0,3,1 beats composer.css's .post-footer button at 0,1,1, whatever the load order). Regenerate the dist only when the orchestrator hands this lane the dist; the exit check's PR (TKT-01M3YMG0NX) carries both. The failing assertion is e2e/phase4-exit.spec.ts's archive test, whose desktop axe scan covers a thread holding a deleted post.

## Notes

**agent:claude-code/phase4-run-mg0nx** at 2026-10-03T10:56:52Z

Evidence, 2026-10-03, on main at fbe569d4 with the committed dist: e2e/phase4-exit.spec.ts 'an admin archives a thread with a reason...' fails at 1440 px with axe color-contrast (serious) at #post-3 footer button[data-post-action="unread"] (#3 is a reply its author deleted before the archive). A throwaway probe as mona on a live thread with a deleted post flagged the quote, reply, unread-from-here and restore buttons at 4.44:1 (#65707f on #eef1f6, 12.5 px). With the one-line CSS change and the dist rebuilt locally (not committed; the dist is queued), the spec passes in both projects (18 passed). Reader bundle: 90416 B gzip before, 90422 B after (+6 B, 120000 B budget). Contrast after: --ink2 #4a5568 on --surface2 #eef1f6 is 7.0:1 in light; in dark, #b8c0ce on #222835.

**agent:claude-code/phase4-run-mg0nx** at 2026-10-03T13:08:00Z

Measured 2026-10-03 on main at c7fec0e8 with the dist regenerated: the footer buttons of a removed post now take --ink2, 6.65:1 on --surface2 in the light theme (#4a5568 on #eef1f6; the 7.0:1 in the description was rounded wrong) and 8.06:1 in the dark (#b8c0ce on #222835), against 4.44:1 and 4.89:1 before. e2e/phase4-exit.spec.ts passes in both projects, and a11y, profile and forum-index with it (53 passed). Reader bundle 92295 B gzip of 120000 B.

## Summary

A removed post's footer buttons now take --ink2 like its links (web/client/src/components/thread/thread.css), 6.65:1 light and 8.06:1 dark, up from 4.44:1 and 4.89:1. Found by the Phase 4 exit spec's desktop axe scan; it lands with the exit check (TKT-01M3YMG0NX) and its regenerated dist. Reader bundle 92295 B gzip.
