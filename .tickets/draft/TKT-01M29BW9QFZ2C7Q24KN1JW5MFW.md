---
schema: 3
id: TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
title: Load and validate config.toml with env overrides and file secrets
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:config
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

`internal/config`: TOML loading, validation with errors that name the offending key, `NEOFORUM_*` environment overrides for container deployments, and `client_secret_file` style indirection so a secret never has to sit in the config file.

The split is the thing to get right. `config.toml` holds what must be readable before the database exists: bootstrap admins, OIDC providers, paths, listen address, fsync policy, and cache sizes. Everything an admin changes at runtime belongs in the `settings` table instead, and putting a key in the wrong half is the mistake this ticket exists to prevent.

## Acceptance criteria

- [ ] An invalid config fails at startup naming the key and the reason, never with a zero value silently substituted
- [ ] Environment overrides are applied and their precedence over file values is tested
- [ ] A secret loaded from a file never appears in a log line or in the effective-config output
- [ ] Every configured OIDC provider is validated for a reachable issuer at startup, and a failing one is disabled with a loud log line rather than aborting the server
