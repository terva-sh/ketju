---
schema: 3
id: TKT-01M3MAS08RJDT1RRJZVA6TB423
title: Accept an OIDC issuer that ends in a slash, as Authentik's does
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
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

`ketju config check`, and so `serve`, refuses an OIDC issuer that ends in a slash (internal/config/validate.go, "providers do not send a trailing slash"). Authentik does send one. Its per-provider issuer is `https://authentik.local.sothr.com/application/o/<slug>/`, and the discovery document's `issuer` field carries that trailing slash. Found deploying the dogfood instance (TKT-01M3M9S7S2), whose provider is `brokkr-ketju`.

With the slash, config is refused. Without it, go-oidc's discovery refuses the mismatch, because it compares the configured issuer with the discovered one exactly, which is the check that matters. So no Authentik provider can be configured at all. `insecure_skip_issuer_check` is not an acceptable workaround, because it weakens verification.

### Fix
Accept the issuer exactly as written. Keep the https and absolute-URL checks. Rely on go-oidc's exact comparison against discovery, and on the `iss` claim check, to catch a mismatch at startup, where the error names both values. Update the config test that pins the old rule. If docs/05 or docs/07 states the slash rule, correct it.

## Acceptance criteria

- [ ] An issuer ending in a slash passes ketju config check, and serve reaches discovery with it
- [ ] A mismatch between the configured and discovered issuer fails at startup with both values named
- [ ] The config test pinning the old rule is replaced by one pinning the new
