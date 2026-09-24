---
schema: 3
id: TKT-01M29CGBVNWMMSRTXA4N42XBBS
title: Build the mobile composer sheet and sticky reply bar
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - area:mobile
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
blocks_on: none
references:
  - ref: design:mobile-composing
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-24T05:06:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

On a thread page the tab bar is replaced by a sticky reply bar showing the reply-to chip and the first line of any draft. Tapping it opens the composer as a full-screen sheet with the same toolbar, preview tab, and server-rendered preview. Reply and quote from a post prefill that sheet.

docs/09 names the specific failure to avoid: a composer fighting the on-screen keyboard. The toolbar pins above the keyboard, and the sheet can show the quoted post above the text area, which is the argument for a sheet over an inline composer.

The post list needs bottom padding equal to the bar height, or the reply bar covers the last post, which is the one a reader most wants to see while replying.

## Acceptance criteria

- [ ] The post list has bottom padding equal to the reply bar height so the last post is never covered
- [ ] Reply and quote from a post open the sheet prefilled
- [ ] A draft in progress shows in the reply bar and survives closing the sheet
- [ ] The toolbar stays above the on-screen keyboard in Playwright's emulated phone viewport, positioned from the visualViewport API; real iOS and Android verification is in TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices)

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. The real-device keyboard check moved to TKT-01M38WTKSQ. What stays here is what an agent can verify: the emulated phone viewport and positioning driven by visualViewport.
