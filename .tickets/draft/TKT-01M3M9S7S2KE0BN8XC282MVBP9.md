---
schema: 3
id: TKT-01M3M9S7S2KE0BN8XC282MVBP9
title: Deploy a local dogfooding instance of ketju on this machine
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
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:22:53Z
updated_at: 2026-09-28T15:22:53Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Phase 1's exit criterion (docs/09) is "a friend group can hold a conversation on it". Passing tests do not prove that, and nothing yet serves ketju anywhere a phone or a second person can reach. This ticket stands up a long-running local instance on this machine, as the lampi and git-ticket-canvas projects already run theirs, so that ketju can be used for real.

The same instance is what the real-device check TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices) needs. A phone has to reach a served build, over the network, through the TLS-terminating reverse proxy that docs/02 assumes. So that ticket depends on this one.

### Before planning
The hostnames, ports, service manager, reverse proxy, TLS setup, data locations and deploy steps come from how lampi and git-ticket-canvas are deployed on this machine, and from their documentation. Read those first and follow the same pattern. Do not guess a path, port or hostname (the machine-wide agent rules forbid it). Check that any path or port is free before using it.

### What the design already fixes
- One process per data directory, with the data-directory lock (docs/02, docs/10).
- A reverse proxy terminates TLS; ketju serves plain HTTP behind it (docs/02).
- `ketju config check` validates the config before first start (PR #106).
- `/healthz` answers 503 when the database or data dir check fails, and 200 `degraded` on quarantine (PR #107, docs/02 "Observability").
- `ketju admin promote --root DIR <handle>` and `bootstrap.admin_subjects` / `bootstrap.admin_emails` make the first admin (PR #97).
- `registration.mode = "open"` works; approval mode is Phase 2 (PR #102).
- The README's "Recovering a lost or damaged database" and `ketju reindex` cover recovery (PR #100). There is no `ketju backup` yet (TKT-01M29CXK35, phase-4), so the instance needs a documented interim backup of the data directory.

### Decisions for a person
- An OIDC provider for the dogfood instance, and its client credentials. Only secret references go in the repository or a ticket, never the values.
- Who is invited, and whether registration is open.
- How upgrades are deployed: which build, from which commit, and how often.

## Acceptance criteria

- [ ] ketju serves from a documented data directory under a service manager, restarts on failure, and passes ketju config check
- [ ] A reverse proxy terminates TLS and a phone on the local network reaches the client at a documented URL
- [ ] Sign-in works through the chosen OIDC provider, and the first admin is bootstrapped
- [ ] /healthz is monitored or at least documented as the check to run
- [ ] The deploy, upgrade, and interim backup steps are written down where the other local deployments keep theirs
- [ ] No secret value is committed or written into a ticket
