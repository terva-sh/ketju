---
schema: 3
id: TKT-01M38WW7GTCX5SDANEBY7ADKMN
title: Tidy go.mod and the stale CI comment
type: chore
status: done
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
updated_at: 2026-09-28T07:54:49Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
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

- [x] go mod tidy produces no diff, and go-oidc and go-jose sit in the direct require block
- [x] The ci.yml comment about cmd/ describes the repository as it is

## Implementation plan

Revised at claim (2026-09-28) against origin/main c066d91. 1. go.mod: go mod tidy already produces no diff on main. 209a68a (TKT-01M29BW9Q1, the database access layer) ran tidy and moved go-oidc v3.21.0 and go-jose v4.1.5 into the direct require block. This ticket changes neither go.mod nor go.sum; it verifies the criterion instead. 2. Rewrite the stale comment on the build step in .forgejo/workflows/ci.yml so it names cmd/ketju as existing. This is a comment-only change: no key, value, step, image, trigger, permission or name moves. 3. Verify: gofmt, go build/vet, go test -race, the sqlc regeneration diff and the SQLite driver spike steps run the way ci.yml runs them, then a second go mod tidy with no diff.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T07:54:44Z

AC1 was already met on main when this ticket was claimed. 209a68a (TKT-01M29BW9Q1) ran go mod tidy, which put go-oidc and go-jose in the direct block. Checked on c066d91 with go1.27.1: go mod tidy leaves go.mod and go.sum unchanged, and both modules are in the first require block. This PR does not touch go.mod or go.sum. Also run locally: gofmt, go build/vet, go test -race ./..., the sqlc regeneration diff (CGO_ENABLED=0 go generate ./internal/store/db, no diff), and the SQLite driver spike's vet, test and one-iteration benchmark. All pass.

## Summary

The stale comment on ci.yml's build step now says that go build ./... compiles cmd/ketju along with every other package. The change is comment-only. go.mod needed nothing: TKT-01M29BW9Q1 (209a68a) had already tidied it, so go mod tidy produces no diff and go-oidc and go-jose are in the direct block. go.mod and go.sum are unchanged.
