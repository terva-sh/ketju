---
schema: 3
id: TKT-01M29BW9P4PXMFKF4VAP8K9QPD
title: Confirm the landing position and phone navigation pattern
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:views
  - area:mobile
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-mobile
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Questions 14, 25, and 26 in docs/10. Three confirmations rather than investigations, all of which need a real device and a real reader instead of an argument.

Landing on return: last read or first unread. They are the same page in practice and the divider handles the distinction. Discourse chose last-read and the lean is to match.

Phone navigation: a five-item bottom tab bar, or a menu button. The lean is the tab bar as mocked up, because a hamburger hides the "what's new" surfaces that are the entire point of a forum.

Composer on phones: a full-screen sheet, or inline at the bottom. The lean is the sheet, because an inline composer under a virtual keyboard is cramped and hides the post being replied to.

## Acceptance criteria

- [ ] All three are confirmed against the mockups on a real iOS device and a real Android device
- [ ] Any change from the documented lean is written into docs/06 before the client work starts
