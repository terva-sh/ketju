---
schema: 3
id: TKT-01M38WW7GTCX5SDANEBY7ADKMN
title: Tidy go.mod and the stale CI comment
type: chore
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
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T05:05:41Z
updated_at: 2026-09-24T05:05:41Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

Two small drifts found while surveying the repository during Phase 1 grooming.

`go.mod` lists `github.com/coreos/go-oidc/v3` and `github.com/go-jose/go-jose/v4`
as `// indirect`, but `internal/auth/oidc` imports both directly
(`keyset.go`, `oidc.go`). `go mod tidy` would move them into the direct block.
Nothing breaks today. The file just misstates what the module depends on, and
the first Phase 1 ticket that adds a dependency and runs `tidy` would carry
this unrelated change in its diff.

`.forgejo/workflows/ci.yml` still has a comment saying there is no `cmd/`
directory yet. `cmd/ketju` has existed since Phase 0.

## Acceptance criteria

- [ ] go mod tidy produces no diff, and go-oidc and go-jose sit in the direct require block
- [ ] The ci.yml comment about cmd/ describes the repository as it is
