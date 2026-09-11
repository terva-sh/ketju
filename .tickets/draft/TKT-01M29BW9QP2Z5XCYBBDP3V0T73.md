---
schema: 3
id: TKT-01M29BW9QP2Z5XCYBBDP3V0T73
title: "Stand up CI: build, vet, test, and the ticket store check"
type: chore
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - area:test
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
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

The pipeline every other ticket's tests run in. Build the binary, `go vet`, run tests with the race detector, and gate on `git ticket check --fix --dry-run --strict` so a malformed ticket store fails the build rather than rotting.

Worth doing in Phase 0, before there is much to test, because a test suite that was never run in CI is a test suite that stops passing quietly. The storage property and crash tests arrive within weeks and they are exactly the kind that pass locally and fail on another filesystem.

CI reports repairs and never commits them, the same way it reports unformatted code rather than reformatting behind your back.

## Acceptance criteria

- [ ] A pull request runs build, vet, and race-enabled tests, and a failure blocks the merge
- [ ] `git ticket check --fix --dry-run --strict` runs and a pending repair fails the build
- [ ] The pipeline runs on a clean checkout with no local state, so a missing generated file is caught
