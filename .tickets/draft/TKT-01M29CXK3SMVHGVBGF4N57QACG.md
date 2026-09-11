---
schema: 3
id: TKT-01M29CXK3SMVHGVBGF4N57QACG
title: Build the release pipeline for the single binary
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
blocks_on: none
references:
  - ref: design:roadmap-phase-4
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Tagged builds producing a static binary per platform with the client embedded, version and commit stamped in, checksums published, and a container image for people who deploy that way.

No cgo anywhere, which is what makes cross-compilation trivial and is worth asserting in the pipeline rather than assuming. A dependency that quietly pulls in cgo would otherwise be found by a user on a musl system.

This is the v1.0 exit criterion in docs/09, so the release notes and the upgrade procedure belong here too.

## Acceptance criteria

- [ ] A tag produces static binaries for the supported platforms with no cgo, asserted by the build
- [ ] Version and commit are stamped in and reported by the binary
- [ ] Checksums are published with the artifacts
- [ ] The documented upgrade path from the previous version is tested, including migrations
