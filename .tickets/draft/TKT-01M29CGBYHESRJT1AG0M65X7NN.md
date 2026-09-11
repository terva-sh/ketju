---
schema: 3
id: TKT-01M29CGBYHESRJT1AG0M65X7NN
title: Enforce the client bundle budget in CI
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:test
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
blocks_on: none
references:
  - ref: design:preact-client
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-11T23:23:08Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Fail the build when the thread view bundle exceeds 60 KB gzipped.

A budget nobody measures is a preference. This is cheap to add while the number is comfortably under, and it is the check that makes adding a component library a deliberate decision rather than an accident in a dependency bump.

## Acceptance criteria

- [ ] CI fails when the thread view bundle exceeds 60 KB gzipped
- [ ] The measured size is reported on every build so a trend is visible
- [ ] Raising the budget requires editing a committed number rather than a flag
