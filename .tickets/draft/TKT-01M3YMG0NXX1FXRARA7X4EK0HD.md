---
schema: 3
id: TKT-01M3YMG0NXX1FXRARA7X4EK0HD
title: Check Phase 4's exit criterion and prepare the v1.0 release
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:test
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M29CEE7SVHNRGVDH53QAFPN0
  - TKT-01M29CRJ64G7PJBPK8Z52SE8NW
  - TKT-01M29CXJZZZN7X4EZX37JJBF91
  - TKT-01M29CXK35WS83Z6CAZBAVDFRD
  - TKT-01M29CXK3SMVHGVBGF4N57QACG
  - TKT-01M29CYSAMEVQ7MJG41HR7HPRH
  - TKT-01M29CYSBAVXNFWDZVAK5PD71S
  - TKT-01M29CYSC0Q4WHMQNVTKB6PN4B
  - TKT-01M29CYSCPQJ5ZZHCFDGV1S4TF
  - TKT-01M3B12ZZ403NEPFH3DASEW1D8
  - TKT-01M3M4KVNW0VTZFWHRZK8V28RZ
  - TKT-01M3V6XTYGE9R6MRRH27HYSMHM
  - TKT-01M3V6XVVGSTDA1SH15DKYP7NZ
  - TKT-01M3V73KM1VJ5XTC53MFCEJ0XW
  - TKT-01M3V7CFJ5RYC1RCZRNYD8RSQS
  - TKT-01M3V7GE5986PGNHTJJBZT1Z0H
  - TKT-01M3V9DWTNNAMF2WA9CWY0AJGQ
  - TKT-01M3VRFRFW9M4XY8D14FXAJA23
  - TKT-01M3VVZE4G37YMQG2RVXSWYM3Z
  - TKT-01M3YME9AZ4RJACSMV8VX4JMZJ
  - TKT-01M3YME9DMH3PMJGD8A4A7ZMNN
  - TKT-01M3YME9G0PM84B9NR4EQW4C2P
  - TKT-01M3YME9KGJ1XW7RHCRFZ621A0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:42:30Z
updated_at: 2026-10-02T16:40:26Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

docs/09 gives Phase 4's exit criterion as "v1.0 tag". Making the tag is the maintainer's act, because it is outside the repository (docs/15 stop class 3). This ticket proves that the work behind it is done, the way TKT-01M3V3CFRE did for Phase 3. It then prepares everything the maintainer needs to cut the release.

Grooming names the tests. At least:
- a moderator splits posts into a new thread and merges posts into an existing one, and both threads show bidirectional notices;
- a member replies as a new thread;
- a member creates a thread with tags under the decided rules, a moderator retags it, and a list filters by tag;
- a moderator archives a thread, and nobody can write to it afterwards;
- `ketju backup` produces a snapshot that restores and passes `ketju fsck`;
- `/metrics` serves only behind its guard;
- the release pipeline builds the binaries in a dry run without publishing anything.

Each browser test runs at 390 and 1440 px with one axe scan. The server invariants are proven through the real serve, as `TestPhase3Exit*` are. They cover docs/15 "Invariants, added":
- a cross-forum split or merge is refused unless the actor holds `moderate` in both forums;
- a moved post is served under the target forum's rules and by no other path: not a pointer, a notice, a notification, a search hit or a live event;
- the source's moved notice names the target only to viewers who may read it;
- an archived thread refuses every write, moderators' included.

## Acceptance criteria

- [ ] e2e/phase4-exit.spec.ts and cmd/ketju phase4_exit_test.go hold the plan's named tests, passing in CI by name on the merged head, quoted in the PR
- [ ] Every other phase-4 ticket except the epics and the tracking ticket is done or archived before this one closes
- [ ] A release checklist for v1.0 is written, and the release pipeline's dry run is green; the tag itself is left to the maintainer
