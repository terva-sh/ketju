---
schema: 3
id: TKT-01M40PCE7PDJB4X6Z4N90BXY9N
title: Give a removed post's footer buttons the contrast its links have
type: bug
status: in-progress
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
references: []
claim:
  actor: agent:claude-code/phase4-run-mg0nx
  branch: run/mg0nx-phase4-exit
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a09ced2aaf4c2bc25
  commit: 6478721098bc90f7ca7bd0dcccb6caed3521fb5e
  session: null
  claimed_at: 2026-10-03T10:54:07Z
  expires_at: null
archive: null
created_at: 2026-10-03T10:53:58Z
updated_at: 2026-10-03T10:56:52Z
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

- [ ] At 1440 px, the footer buttons of a removed post (quote, reply, unread from here, restore) meet 4.5:1 against the removed post's background, in the light and dark themes
- [ ] e2e/phase4-exit.spec.ts's archive test, which scans a thread holding a deleted post with axe at 1440 px, fails without the fix and passes with it
- [ ] The committed dist is regenerated and the reader bundle stays within 120000 B gzip

## Implementation plan

Add .post.removed .post-footer button to the selector list of the .post.removed rule in web/client/src/components/thread/thread.css that recolours to --ink2 (specificity 0,3,1 beats composer.css's .post-footer button at 0,1,1, whatever the load order). Regenerate the dist only when the orchestrator hands this lane the dist; the exit check's PR (TKT-01M3YMG0NX) carries both. The failing assertion is e2e/phase4-exit.spec.ts's archive test, whose desktop axe scan covers a thread holding a deleted post.

## Notes

**agent:claude-code/phase4-run-mg0nx** at 2026-10-03T10:56:52Z

Evidence, 2026-10-03, on main at fbe569d4 with the committed dist: e2e/phase4-exit.spec.ts 'an admin archives a thread with a reason...' fails at 1440 px with axe color-contrast (serious) at #post-3 footer button[data-post-action="unread"] (#3 is a reply its author deleted before the archive). A throwaway probe as mona on a live thread with a deleted post flagged the quote, reply, unread-from-here and restore buttons at 4.44:1 (#65707f on #eef1f6, 12.5 px). With the one-line CSS change and the dist rebuilt locally (not committed; the dist is queued), the spec passes in both projects (18 passed). Reader bundle: 90416 B gzip before, 90422 B after (+6 B, 120000 B budget). Contrast after: --ink2 #4a5568 on --surface2 #eef1f6 is 7.0:1 in light; in dark, #b8c0ce on #222835.
