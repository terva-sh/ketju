---
schema: 3
id: TKT-01M29BW9QP2Z5XCYBBDP3V0T73
title: "Stand up CI: build, vet, test, and the ticket store check"
type: chore
status: review
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
updated_at: 2026-09-15T00:40:13Z
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
- [x] `git ticket check --fix --dry-run --strict` runs and a pending repair fails the build
- [x] The pipeline runs on a clean checkout with no local state, so a missing generated file is caught

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

**agent:claude-code/ci** at 2026-09-15T00:18:26Z

Run #1 on d741853 (push to main): success, 32s, all nine steps green. Per-step,
from the web route rather than the summary — a green run that skipped its work
looks identical from the outside:

  Install dependencies  2s | Checkout Code  1s | Download modules  0s
  gofmt check  0s | build  4s | go vet  1s | go test (race)  10s
  build the format spike  2s | ticket store is consistent  11s

The three steps that could have passed by doing nothing did not:

  go test (race)   -> ok github.com/terva-sh/ketju/internal/store/threadlog 1.111s
  format spike     -> downloaded cbor/v2 and float16, so it really compiled
  ticket store     -> installed git-ticket v0.17.1, then "No problems found."

`Download modules 0s` is not a warm cache; this module has no require block.
That, plus one small package, is the whole explanation for a 32s run, and it is
worth knowing now so nobody reads the first slow run as a regression.

The store step incidentally confirmed the reason it uses `go install` rather
than a go.mod tool directive: installing git-ticket pulled chroma/v2 2.27.0,
go-runewidth 0.0.29, uax29/v2, regexp2/v2, uniseg, x/term and yaml.v3. A tool
directive would have put every one of those in this module's graph.

Criterion 1 is ticked only halfway and so left unticked. The workflow does
trigger on `pull_request` and does run build, vet and the race suite — but the
observed run was a push, because this repository commits to main directly and
there is no PR to point at yet. More to the point, "a failure blocks the merge"
is branch protection with required status checks, which is a REMOTE REPOSITORY
SETTING. The API answers 401 to an anonymous read of /branch_protections, so I
cannot even confirm the current state, and changing a remote's settings is not
something an agent does without the ticket saying so. Someone with admin on
terva-sh/ketju needs to require the `ci` context on main. That is the last
thing standing between this ticket and done.

**agent:claude-code/ci** at 2026-09-15T00:37:29Z

Branch protection is now on for main, and the required context reads
`ci / Build and Test (push)`. Confirmed against the API rather than taken on
trust: enable_status_check true, status_check_contexts exactly that one string.

That context will never arrive on a pull request, so as configured it blocks
every merge into main permanently.

Forgejo suffixes a status context with the EVENT that produced it. Both pushed
commits carry `ci / Build and Test (push)` and nothing else, because both were
pushes to main. A pull request from a topic branch produces a different string,
`ci / Build and Test (pull_request)` — and this workflow's push trigger is
`branches: [main]`, so pushing the topic branch runs nothing at all and posts no
`(push)` status anywhere. The required context is then simply absent from the
head commit, which Forgejo reads as pending, which is not passing.

terva learned the same distinction the expensive way; docs/plans/forgejo-workflow.md
in that repository has a section called "Only the `pull_request` runs gate a PR",
recording PR #240 merging with both `(pull_request)` gates green while all three
`(push)` runs showed failure. The two sets of statuses on one sha are unrelated.

Fix is one setting, no code change: require `ci / Build and Test (pull_request)`
instead. Requiring the `(push)` context gates nothing useful in any case — the
push run on main happens AFTER the merge it would be gating.

Do not "fix" this by adding topic branches to the push trigger. That posts a
second concurrent run of the identical suite on the same runner for every PR,
which is the contention terva's file explicitly warns flakes timing-sensitive
tests. The trigger is right; the required string is wrong.

Not verified end to end: no pull request has been opened against this repository
yet, so the exact `(pull_request)` context string is derived from how Forgejo
names contexts rather than read off a real run. Opening one throwaway PR would
settle both that and the first acceptance criterion in a single step.

**agent:claude-code/ci** at 2026-09-15T00:40:13Z

Proven end to end rather than predicted. PR #1 (chore/ci-gate-finding -> main):

- Pushing the topic branch ran NOTHING and posted NO status on 4edb121. The push
  trigger is `branches: [main]`, so there is no `(push)` status to be had.
- Opening the PR ran the workflow (run #3, event pull_request, success) and
  posted exactly one context: `ci / Build and Test (pull_request)`.
- `POST /pulls/1/merge` answered 405 with the router's bare HTML page, which is
  Gitea's shape for a refused merge — `ctx.Status(405)` carries no JSON body.

The refusal is isolated to the status check, because every other gate on that
protection is off: required_approvals 0, block_on_rejected_reviews false,
block_on_outdated_branch false, block_on_official_review_requests false,
enable_merge_whitelist false, require_signed_commits false, protected_file_patterns
empty. The only rule left is enable_status_check true with the single context
`ci / Build and Test (push)`, which this PR does not and cannot carry.

"Not allowed to merge" is ruled out too: the token read /branch_protections,
which is a repo-admin endpoint and 401s for anyone else.

So main is currently unmergeable by any route, and this PR is the demonstration.
Change the required context to `ci / Build and Test (pull_request)` and it goes
green. A glob such as `ci / Build and Test*` would also match, and would keep
matching if the job is ever renamed per event — but it has not been tested here
and the exact string has.

Reading the status rows needs care, and this run showed why: 4edb121 carries
three rows for one context — pending, pending, success. Taking the last row in
list order reports a finished job as pending. Rank by state, letting failure win
a tie, exactly as terva's pr.sh does.

## Summary

`.forgejo/workflows/ci.yml`: one job, nine steps, on push-to-main and
pull_request. gofmt, `go build ./...`, `go vet ./...`, `go test -race ./...`,
build+vet of the separate `spikes/format-decisions` module, and
`git ticket check --fix --dry-run --strict`. Green on its first run (#1,
d741853, 32s), with each step's log checked rather than the summary trusted.

Two of three criteria met. The third is not mine to finish: "a failure blocks
the merge" is branch protection on the remote, and the ticket does not authorize
changing a repository's settings. In `review` for that reason — the pipeline is
built and proven, and somebody with admin needs to require the `ci` status
context on main before this is done.

Three choices worth keeping, each with a cheaper-looking wrong answer:

golang:1.27-alpine, not the 1.25-alpine that 46 workflows on this instance use.
go.mod requires go 1.26.2, which 1.25 cannot build at all. 1.27 is proven here
by terva; the registry mirror is curated, so an arbitrary library/* tag is not
safe to assume.

git-ticket by `go install pkg@version` pinned in `env:` at v0.17.1, not a tool
directive in go.mod. The run proved the cost being avoided: installing it pulled
chroma/v2, go-runewidth, uax29/v2, regexp2/v2, uniseg, x/term and yaml.v3, and a
tool directive would have put all of them in this module's graph, where a lint
tool could change what the shipped binary builds against.

The spike module gets build and vet but not its tests. Root `./...` never
reaches a separate module, so it can rot unnoticed — and its README says
decision 1 is expected to be reopened when json/v2 lands, which a spike that no
longer compiles makes impossible. Its tests time an fsync and build a 114 MB
log, so running them would report the runner's disk and gate nothing.

`--strict` was measured, not argued: on a scratch copy of the store, a stale
epics.md exits 1 with it and 0 without it, printing the identical warning both
times.
