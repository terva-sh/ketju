---
schema: 3
id: TKT-01M3XNQ77222FNGZHA4HM380Z8
title: Tell an unread alert's thread link apart by more than colour
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase3-alert-link
  branch: run/nq772-alert-link
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a444afb857f0255ac
  commit: db61da04270000ba6ed54b5a735206fb6ad5c8a1
  session: null
  claimed_at: 2026-10-02T06:56:41Z
  expires_at: null
archive: null
created_at: 2026-10-02T06:44:40Z
updated_at: 2026-10-02T06:57:02Z
created_by:
  id: agent:claude-code/phase3-exit-check
  name: ""
updated_by:
  id: agent:claude-code/phase3-alert-link
  name: ""
extensions: {}
---

## Description

Found by the Phase 3 exit spec (TKT-01M3V3CFRE, `e2e/phase3-exit.spec.ts`, the notifications test) at 1440 px, light theme. It fails axe's `link-in-text-block` (serious, WCAG 2.1 1.4.1 Use of Color):

    link-in-text-block (serious): Links must be distinguishable without relying on color
      li[data-alert="2"] > .alert-main > .alert-what > .alert-thread.hit

### What is wrong

On an **unread** alert row, `.alert-row.unread .alert-what` is `font-weight: 600`. `.alert-thread` is also 600, with `text-decoration: none` (underline on hover only). The thread link and the sentence around it then differ only in colour: accent `rgb(47, 95, 179)` against ink `rgb(27, 33, 48)`, which is under the 3:1 that axe accepts in place of a non-colour cue. A read row passes, because its sentence is weight 400 and the link 600. A probe at 1440 px showed both read rows clean in light and dark.

The failing row is a `thread_you_watch` row, "Carol Ruiz posted in <link>, which you watch", whose link has text on both sides. The unread `mention` row in the same scan, whose link ends the sentence, was not flagged. `alerts.spec.ts` scans only an unread mention, so it never met the case. At 390 px the same test's scan passed.

### Where

`web/client/src/screens/notifications.css`: `.alert-row.unread .alert-what` and `.alert-thread`. The owning ticket is TKT-01M29CTY86 (Build the notifications UI and alerts tab).

### Why phase-3

It fails Phase 3's exit criterion 1 (TKT-01M3V3CFRE): every named test passes at both widths with one axe scan. The exit spec is not changed to step around it.

## Implementation plan

On an unread row the sentence is weight 600, so the 600 thread link differs from it only by colour (accent on ink, under 3:1), and axe's `link-in-text-block` fails when text follows the link (`thread_you_watch`).

1. `web/client/src/screens/notifications.css`: underline `.alert-thread` on unread rows (`.alert-row.unread .alert-thread { text-decoration: underline }`, with a small `text-underline-offset`). Read rows keep their weight contrast (400 sentence, 600 link) and their hover-only underline. `docs/mockups/` has no notifications mockup (the stylesheet's header says it follows threadlist.css and threadrow.css), so there is nothing there to match.
   - Lost: underline on every row (changes read rows, which already pass, for no reader gain); raise the link to 700 on unread rows (600 against 700 is a weak cue); drop the sentence weight on unread rows (loses the non-colour unread cue that the weight gives with the dot).
2. Regression check in `web/client/e2e/alerts.spec.ts`: a test that answers `/api/v1/notifications` with one unread `thread_you_watch` row, whose link has text on both sides, and runs the one axe scan at both widths. It fails at 1440 px before the fix.
3. Prove the exit spec's notifications test passes with the spec copied in from `run/cfre-phase3-exit`, uncommitted, at 390 and 1440 px.
4. tsc, vitest, `go test -race ./...`, the full Playwright suite; regenerate and commit the dist.
