---
schema: 3
id: TKT-01M29CEE6V358M5KCBJGHD58HM
title: Build the thread page with pager and unread divider
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:L
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA9AXKRTS6KMN5CTH7461
  - TKT-01M29BW9P4PXMFKF4VAP8K9QPD
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
blocks_on: none
references:
  - ref: design:canonical-view
    path: docs/06-thread-views.md
  - ref: design:mockups-thread
    path: docs/mockups/ThreadDark.dc.html
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

The canonical view: posts in order, the post anatomy from docs/06, the pager, and the "N new posts below" divider between read and unread.

docs/06 calls the divider the single most valuable piece of UI in a linear forum, and it ships in the first milestone rather than with the lenses. A returning reader lands on the page containing their last read position with the divider drawn in the right place.

The reply-to indicator is suppressed when the referenced post is immediately above, which is Discourse's rule and the reason most threads read as plain flat text.

## Acceptance criteria

- [ ] A returning reader lands on the page containing their last read ordinal with the divider drawn between read and unread
- [ ] The reply-to indicator is hidden when the referenced post is directly above
- [ ] Clicking a reply-to indicator scrolls to the post when it is on the page and opens a popover when it is not
- [ ] Tombstones render in place with the ordinal preserved
- [ ] Playwright screenshots at 390 px and 1440 px are committed beside the tests and compared against the matching file in docs/mockups/; every visible divergence is listed in the pull request description

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Matches the mockups' now names its method: committed Playwright screenshots at both widths compared against docs/mockups/, with every divergence listed in the PR.
