---
schema: 3
id: TKT-01M29BSPEXA023NH3DRMWKW9HB
title: Decide whether auto_approve providers skip handle selection
type: spike
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:S
  - decision
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:open-questions-identity
    path: docs/10-open-questions.md
claim: null
archive: null
created_at: 2026-09-11T23:10:45Z
updated_at: 2026-09-28T21:32:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Questions 10 and 11 in docs/10, both small policy calls that shape the registration screens.

Should a provider marked `auto_approve` also take `preferred_username` as the handle without asking? The lean is yes, with a fallback screen when the name collides. For an organization's own SSO, a second screen asking for a name the directory already knows is friction with no payoff.

Question 11 rides along because it is the same screen's audience: public forums readable anonymously is in, anonymous posting is out. That needs confirming rather than investigating.

## Acceptance criteria

- [x] Both decisions are recorded in docs/05 and docs/10 is updated
- [x] If handle selection is skipped, the collision fallback screen is specified

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:32:46Z

Maintainer decision, 2026-09-28, in Phase 2 grooming (TKT-01M3M9RR26): "Yes, with fallback; guests as stated". Written into docs/05, under the approval-mode registration path, and docs/10 items 10 and 11.

## Summary

An auto_approve provider takes the normalized preferred_username as the handle when it is valid, not reserved and free. Otherwise the normal handle screen appears, prefilled, with the reason it could not be used. Guests are confirmed: public forums are readable anonymously, there is no anonymous posting, and profiles stay members-only.
