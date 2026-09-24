---
schema: 3
id: TKT-01M29CGBY16MK20W1QATC5N7WE
title: Add Playwright smoke tests at both viewports
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - area:test
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
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

A Playwright run against a dev server with the fake OIDC issuer, covering sign in, read a thread, reply, and see the unread divider, executed at a 390 px phone viewport and a 1440 px desktop viewport.

Both viewports in CI is the mechanism behind the docs/09 mitigation for phone layout being bolted on late. A phone layout that is only ever checked by hand is a phone layout that breaks between releases and nobody notices until a member says so.

## Acceptance criteria

- [ ] The suite runs at 390 px and 1440 px and a failure at either width fails CI
- [ ] The fake issuer supports sign-in with no external network access
- [ ] The run covers sign in, read, reply, and the unread divider end to end
- [ ] The suite passes 20 consecutive local runs with no flake before the CI job becomes a merge gate, and the run count is recorded in a note

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Stable enough to gate merges' was subjective. It now means 20 consecutive local runs with no flake before the job is made required.
