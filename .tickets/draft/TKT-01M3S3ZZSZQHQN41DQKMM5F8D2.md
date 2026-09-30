---
schema: 3
id: TKT-01M3S3ZZSZQHQN41DQKMM5F8D2
title: Close the small visual gaps between the client and the mockups
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: null
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-09-30T12:17:55Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
extensions: {}
---

## Description

Found comparing the committed screenshots in `web/client/e2e/screenshots/` with `docs/mockups/` (TKT-01M39WBN4Q). Each is small, none is decided anywhere, and none has an owner. The IDs are the divergence list's in the note of 2026-09-30 on TKT-01M39WBN4Q.

- T1: the thread page names the forum only ("General"). The mockup's breadcrumb is "Forums › General".
- T4: the pager fills the current page with the accent colour, and its "posts 51–60 of 60" span is in the sans face. The mockup fills it with ink (#1B2130 light, near-white dark), draws wider 48 px page buttons, and sets the span in mono (`web/client/src/components/thread/thread.css`).
- T10: the thread column is 880 px wide (`thread.css`, `max-width`). The mockup's is 900 px.
- P1: the phone top bar's title is bold ink. The mockup's is muted and regular (`shell/shell.css`, `.topbar.phone .title`).
- P4: the phone pager's steps are text ("‹ Prev", "Next ›"). The mockup's are square icon-only buttons. The page picker itself is TKT-01M29CGBZ.
- M1: the phone front page centres the wordmark. The mockup left-aligns it (`shell.css` centres every `.title`).
- D2: the desktop top bar shows the handle as text. The mockup shows an avatar circle beside the name.
- D1: the desktop top bar's search is a compact button. The mockup's is a 240 px field, "Search threads and posts", with a "/" key hint. The search screen is TKT-01M29CTY8T.
- F9: on the phone front page the lock icon follows the forum's name. The mockup puts it at the card's right edge.

### What to do
Match each to the mockup, or record in docs/06 or docs/07 why the client differs. Refresh the affected screenshots with `KETJU_SCREENSHOTS=1 npx playwright test e2e/thread.spec.ts e2e/forum-index.spec.ts`.

## Acceptance criteria

- [ ] Each item in the description matches its mockup, or docs/06 or docs/07 says why it differs
