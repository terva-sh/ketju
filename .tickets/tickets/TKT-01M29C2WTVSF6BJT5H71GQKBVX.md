---
schema: 3
id: TKT-01M29C2WTVSF6BJT5H71GQKBVX
title: Fuzz the record frame parser
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:storage
  - area:test
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BW9PTAQ34EC78E61AHZM0
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:15:46Z
updated_at: 2026-09-15T00:52:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/storage
  name: ""
extensions: {}
---

## Description

A Go fuzz target over the frame parser and the body decoder. The parser reads a length field off disk and allocates from it, which is the classic shape of a memory blow-up, so a hostile or corrupt length must be refused before it is trusted.

The threat here is mostly corruption rather than an attacker, since logs are not user-supplied files. That does not change the failure mode: a panic in the parser takes down the process for every thread, not just the damaged one.

## Acceptance criteria

- [ ] The parser never panics on arbitrary input and always returns an error instead
- [ ] A length field larger than the remaining file is refused before any allocation of that size
- [ ] The fuzz target runs in CI for a bounded time and its corpus is committed
- [ ] Any crash found is added to the corpus with a regression test
