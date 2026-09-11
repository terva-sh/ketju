---
schema: 3
id: TKT-01M29CP2QKKZSK4PDB40EJHC3P
title: Implement user suspension from the moderator tools
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRFSDCGKH605X8Y4RT3C
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-11T23:26:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Suspend and unsuspend with a reason and a duration, from the moderator UI, with the option to hide the user's posts.

docs/08 rules out shadow banning explicitly: a suspension carries a reason the member can read. Deceiving a member of a small community about whether they are still in it is worse than removing them.

## Acceptance criteria

- [ ] Suspension revokes sessions immediately and shows the reason at the next login attempt
- [ ] A duration expires without manual intervention
- [ ] Hiding posts is optional and reversible
- [ ] Every suspension and unsuspension is in audit_log
