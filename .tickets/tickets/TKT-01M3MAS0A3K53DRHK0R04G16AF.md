---
schema: 3
id: TKT-01M3MAS0A3K53DRHK0R04G16AF
title: Add ketju admin setting so a fresh install can open registration
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:40:14Z
updated_at: 2026-09-28T15:40:24Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

A fresh Phase 1 install cannot register anyone. `registration.mode` defaults to `approval` (internal/settings/settings.go), and Phase 1 answers approval mode with a 501 page (TKT-01M29CKRG9 builds it in Phase 2). Settings live only in the database, and nothing in Phase 1 can change one: the admin settings screen is Phase 2 (TKT-01M29CXK2H) and the settings endpoints are Phase 4 (TKT-01M29CCC0X). The first admin cannot register either, so `bootstrap.admin_emails` never applies. The e2e seed never hit this, because it writes users directly. Found deploying the dogfood instance (TKT-01M3M9S7S2).

### Decided by the maintainer on 2026-09-28
"CLI to set a setting": `ketju admin setting` with `set KEY VALUE`, `get KEY` and `list`. The default stays `approval`, so a public install is closed until an operator opens it.

### Shape
- Follow `ketju admin promote` (PR #97): `--root DIR`, the data-directory lock (refuse while serve holds it), and exit codes 1 domain, 2 usage, 4 lock or database.
- Validate every value through the settings registry: unknown key or invalid value is exit 1, naming the key and the allowed values.
- Write an audit row in the same transaction, as Promote does. There is no actor for the CLI, so record it the way `admin promote` does.
- Add a settings writer (`Key.Set`, or a registry-level set by name) in internal/settings, so the CLI does not write the table around the registry.
- Name the command in README's first-run steps and in docs/02 or docs/05.

## Acceptance criteria

- [ ] ketju admin setting set registration.mode open opens registration on a stopped instance, and serve then admits a new member
- [ ] Every value is validated through the settings registry; an unknown key or bad value exits 1 naming the allowed values
- [ ] It refuses while serve holds the data-directory lock and follows admin promote's exit codes
- [ ] Each change writes an audit row in the same transaction
- [ ] README's first-run steps name the command
