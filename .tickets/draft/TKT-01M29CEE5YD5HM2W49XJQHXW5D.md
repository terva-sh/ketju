---
schema: 3
id: TKT-01M29CEE5YD5HM2W49XJQHXW5D
title: Build the forum index screen
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA9A5PGMCFDSVWQAHT87G
blocks_on: none
references:
  - ref: design:mockups-front
    path: docs/mockups/Main.dc.html
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
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

The front page: the forum tree with per-forum unread counts and recent activity, matching `docs/mockups/Main.dc.html` on desktop and `MobileFrontDark.dc.html` on phones.

This is the first screen anybody sees, and in a small community it is also the one that answers "is anything happening". The unread counts are the content.

## Acceptance criteria

- [ ] Unread counts render for a signed-in viewer and are absent rather than zeroed for an anonymous one
- [ ] A forum with no threads renders a usable empty state
- [ ] The floating new-thread control appears above the tab bar on phones
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR. Automated pixel diffing lost, because the mockups are static HTML with placeholder data and would never match exactly.
