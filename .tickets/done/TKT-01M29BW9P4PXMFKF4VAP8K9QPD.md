---
schema: 3
id: TKT-01M29BW9P4PXMFKF4VAP8K9QPD
title: Confirm the landing position and phone navigation pattern
type: spike
status: done
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
updated_at: 2026-09-24T05:05:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Questions 14, 25, and 26 in docs/10. Three confirmations rather than investigations, all of which need a real device and a real reader instead of an argument.

Landing on return: last read or first unread. They are the same page in practice and the divider handles the distinction. Discourse chose last-read and the lean is to match.

Phone navigation: a five-item bottom tab bar, or a menu button. The lean is the tab bar as mocked up, because a hamburger hides the "what's new" surfaces that are the entire point of a forum.

Composer on phones: a full-screen sheet, or inline at the bottom. The lean is the sheet, because an inline composer under a virtual keyboard is cramped and hides the post being replied to.

## Acceptance criteria

- [ ] All three are confirmed against the mockups on a real iOS device and a real Android device
- [x] Any change from the documented lean is written into docs/06 before the client work starts

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:11Z

This ticket is closed with criterion 1 unticked, deliberately. It needs a real phone and a real reader, which no agent has. The user accepted the leans as decisions instead of waiting on that confirmation. The confirmation itself moves to TKT-01M38WTKSQ, which waits on the Playwright gate. Criterion 2 is ticked: no decision changed from the documented lean, and docs/06 now marks all three as decided before any client work starts. Filed alongside: dependency links from this ticket to the shell TKT-01M29CEE4K, the sheet TKT-01M29CGBVN, and the thread page TKT-01M29CEE6V, which build on these three choices.

## Summary

Decided with the user on 2026-09-24, on the documented leans. A reader lands at the last-read position, phone navigation is a five-item bottom tab bar (Forums, Unread, New, Alerts, Me), and the phone composer is a full-screen sheet. The reasoning is in docs/06 (Landing on return; Mobile), and docs/10 Q14, Q25 and Q26 are marked decided. Criterion 1, confirmation on real iOS and Android devices, is not met and moves to TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices).
