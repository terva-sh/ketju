---
schema: 3
id: TKT-01M39WBN4QAMNRBXCGD85CDJF0
title: Commit thread page screenshots at 390 and 1440 px against the mockups
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M29CGBY16MK20W1QATC5N7WE
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CCC0FVTZ2YXNXB2EK4R4Q
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T14:15:52Z
updated_at: 2026-09-24T14:15:52Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

TKT-01M29CEE6V (Build the thread page with pager and unread divider) left its fourth acceptance criterion unticked: Playwright screenshots at 390 px and 1440 px, committed beside the tests and compared against the matching file in docs/mockups/, with every visible divergence listed. This ticket carries that criterion.

### Why it could not be met there
- No Playwright tests exist in the repository to commit screenshots beside. TKT-01M29CGBY1 (Add Playwright smoke tests at both viewports) brings the harness and CI's Chromium.
- The thread page is not yet inside the app shell (TKT-01M29CEE4K) and has neither the CSS system's tokens, dark theme and fonts (TKT-01M29CCC0F) nor the self-hosted IBM Plex and Newsreader. A screenshot taken now would diverge from the mockups mostly in ways those tickets remove. Listing those divergences would record work that is already scheduled, not the thread page's own.

### What was checked
The page was rendered in headless Chromium at 1440 and 390 px, locally and not committed, against a stubbed API. The layout matched the mockups' structure: the ordinal, name, badge and time header; the reply-to indicator on the right; the tombstone row; the amber divider with "last read #N"; the unread edge; the numbered pager on desktop and "Page N of M" on the phone. The console showed no errors.

### What to do
Once CGBY1, CEE4K and CCC0F are in, add Playwright captures of a seeded thread at 1440 and 390 px, light and dark. Compare them with Thread{Light,Dark}.dc.html and MobileThread{Light,Dark}.dc.html, commit them beside the test, and list every divergence in the PR.

## Acceptance criteria

- [ ] Playwright screenshots of a seeded thread at 390 px and 1440 px, light and dark, are committed beside the tests
- [ ] Every visible divergence from the matching docs/mockups file is listed in the pull request
