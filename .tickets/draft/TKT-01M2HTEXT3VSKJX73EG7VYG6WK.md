---
schema: 3
id: TKT-01M2HTEXT3VSKJX73EG7VYG6WK
title: Decide whether AGENTS.md carries the short git-ticket instructions block
type: spike
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:ops
  - area:docs
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-15T06:00:56Z
updated_at: 2026-09-15T06:00:56Z
created_by:
  id: agent:claude-code/ci
  name: ""
updated_by:
  id: agent:claude-code/ci
  name: ""
extensions: {}
---

## Description

`AGENTS.md`: decide whether to adopt the short form of the generated
git-ticket instructions block.

v0.18.0 split the block into a short and a long form. `instructions --core`
now emits 132 lines against the 211 currently installed here, and the long
form remains available at `git ticket instructions` for an agent that wants
the reasoning behind a rule. The wording differs throughout, not only in
length: the short form leads with the failure each rule prevents.

This is a judgement call about what an agent reads first in this repository,
not a mechanical upgrade, which is why it is filed rather than applied. The
decision is whether AGENTS.md should carry the short form and let agents reach
for the long one, or keep the fuller block inline.

## Acceptance criteria

- [ ] A decision is recorded on this ticket, with the alternative and why it lost
- [ ] If the short form is adopted, AGENTS.md is regenerated with git ticket instructions --write and the diff reviewed rather than taken on trust
