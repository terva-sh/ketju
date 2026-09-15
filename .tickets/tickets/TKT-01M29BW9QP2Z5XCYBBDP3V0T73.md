---
schema: 3
id: TKT-01M29BW9QP2Z5XCYBBDP3V0T73
title: "Stand up CI: build, vet, test, and the ticket store check"
type: chore
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - area:test
  - effort:M
assignees: []
milestone: phase-0
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
claim:
  actor: agent:claude-code/ci
  branch: main
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju
  commit: c58a0eb29d93285fe9653918f8d9e8c78b42e269
  session: null
  claimed_at: 2026-09-15T00:14:10Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-15T00:16:30Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/ci
  name: ""
extensions: {}
---

## Description

The pipeline every other ticket's tests run in. Build the binary, `go vet`, run tests with the race detector, and gate on `git ticket check --fix --dry-run --strict` so a malformed ticket store fails the build rather than rotting.

Worth doing in Phase 0, before there is much to test, because a test suite that was never run in CI is a test suite that stops passing quietly. The storage property and crash tests arrive within weeks and they are exactly the kind that pass locally and fail on another filesystem.

CI reports repairs and never commits them, the same way it reports unformatted code rather than reformatting behind your back.

## Acceptance criteria

- [ ] A pull request runs build, vet, and race-enabled tests, and a failure blocks the merge
- [ ] `git ticket check --fix --dry-run --strict` runs and a pending repair fails the build
- [ ] The pipeline runs on a clean checkout with no local state, so a missing generated file is caught

## Implementation plan

One file, `.forgejo/workflows/ci.yml`, one job, modelled on the same file in
`terva-sh/git-ticket` — the closest sibling: a Go repository on this instance
that keeps its own `.tickets/` store and gates on it.

Decisions, each of which has a cheaper-looking wrong answer:

1. **Forgejo Actions, not GitHub Actions.** Runners are enabled on
   `git.local.sothr.com`. Actions resolve against *this* Forgejo, so a bare
   `actions/checkout@v4` is fetched from this instance's own `actions/checkout`
   path and 404s before a single step runs. Checkout comes from the public
   mirror org: `Actions-Mirrors/forgejo-actions-checkout@v6`.

2. **`golang:1.27-alpine`, not the `1.25-alpine` that 46 other workflows on this
   instance use.** `go.mod` requires `go 1.26.2`; 1.25 cannot build this
   repository at all. 1.27-alpine is already proven here by `terva`, so this is
   a curated tag rather than a guess — the internal registry mirror is not a
   full Docker Hub passthrough and an arbitrary `library/*` tag may be absent.

3. **`apk add gcc musl-dev`.** `-race` needs cgo, and on alpine without a C
   toolchain the race step fails to *link*. That reads as a broken toolchain
   rather than a missing one, which is the confusing way to learn this.

4. **`git-ticket` by `go install pkg@version`, pinned in `env:`.** Not a `tool`
   directive in `go.mod`: terva measured that, and `go get -tool` pulls
   git-ticket's TUI dependency graph into the consumer's module and lets a lint
   tool change what the shipped binary builds against. `go install pkg@version`
   builds outside the main module and touches neither `go.mod` nor `go.sum`.
   Pinned at v0.17.1, the current release, rather than terva's older v0.14.3.
   `GOBIN=/usr/local/bin` so the step does not rest on the image's `PATH`.

5. **`--fix --dry-run --strict`**, exactly as `AGENTS.md` promises CI runs it.
   `--strict` is the flag doing the work: `epics_index_stale` is a warning, so
   without it a stale `epics.md` exits 0 and the step goes green over one of the
   conditions it exists to catch.

6. **`build` is `go build ./...`.** There is no `cmd/` yet. The criterion says
   "build the binary" and the honest reading today is "compile every package";
   when `cmd/ketju` lands the step already covers it with no edit.

7. **The spike module gets build and vet, but not its tests.**
   `spikes/format-decisions/` is a *separate* Go module, so root `./...` never
   reaches it and it can rot silently — and its README says decision 1 is
   expected to be reopened when json/v2 lands, which a module that no longer
   compiles makes impossible. Its tests are measurement reports
   (`TestFsyncLatency`, `TestIndexWidthReport` builds a 114 MB log), not
   assertions, so running them in CI would report a runner's disk rather than
   gate anything.

Triggers: `pull_request`, plus `push` to `main`. Both, because this repository
commits to `main` directly today; when the work moves to topic branches the
push trigger can go, for the reason terva's file records — a duplicate
concurrent run on the same runner is what flakes timing-sensitive tests.

`permissions: contents: read`. Nothing here writes to the repository.

Verification: every step run locally first against the real tree, then the file
committed and pushed so the first real run is observed rather than assumed.

## Notes

**agent:claude-code/ci** at 2026-09-15T00:16:30Z

Verified before committing, locally, against the real tree rather than assumed:
gofmt clean, `go build ./...`, `go vet ./...`, `go test -race ./...` (ok
internal/store/threadlog 1.068s), and `go -C spikes/format-decisions build ./...`
plus vet. The workflow YAML parses and resolves to one job, nine steps, on
push-to-main and pull_request.

The store gate was proven by breaking a copy of the store in /tmp, twice:

- Appended a line to epics.md. `check --fix --dry-run --strict` reported
  `epics_index_stale`, "would rewrite", and exited 1. The SAME store without
  `--strict` printed the identical warning and exited 0. That is the whole case
  for the flag, and it is now measured rather than argued.
- Moved a draft ticket into tickets/. Reported `location_mismatch`, "would move",
  exited 1, and left the file exactly where it was — the dry-run wrote nothing,
  which is the property AGENTS.md promises.
