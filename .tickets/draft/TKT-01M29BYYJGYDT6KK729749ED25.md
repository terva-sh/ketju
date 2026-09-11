---
schema: 3
id: TKT-01M29BYYJGYDT6KK729749ED25
title: Ship neoforum config check
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-11T23:13:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

A subcommand that validates the configuration and prints the effective values with secrets redacted, exiting non-zero on a problem so it works in a deployment script.

This is the command an operator runs before restarting, and the one they paste into a support thread. Redaction has to be by construction rather than a denylist of key names, because the first secret somebody adds with an unexpected name is the one that leaks.

## Acceptance criteria

- [ ] Exit code is non-zero on any validation failure and the message names the key
- [ ] Effective output shows merged file, environment, and default values, and says which layer each came from
- [ ] No secret value appears in the output, including one loaded from a file, tested with a config carrying a known sentinel string
