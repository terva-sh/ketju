---
schema: 3
id: TKT-01M29CXK3SMVHGVBGF4N57QACG
title: Build the release pipeline for the single binary
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies:
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CXK35WS83Z6CAZBAVDFRD
blocks_on: none
references:
  - ref: design:roadmap-phase-4
    path: docs/09-roadmap.md
  - ref: design:architecture-ops
    path: docs/02-architecture.md
  - ref: pr:358
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-10-02T22:06:49Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cxk3s
  name: ""
extensions: {}
---

## Description

This ticket builds and checks the release artifacts and runs the whole pipeline dry in CI. The artifacts are a static binary per platform with the client embedded, the version and commit stamped in, and a checksums file. Publishing them from a tag, the container image and signing were split out to TKT-01M3YSVEDJ (Publish release binaries, checksums and an image from a tag) during Phase 4 grooming. That part lies outside the repository (docs/15 stop class 3) and outside CI's allowance (docs/12 "Changing CI"), so it waits on the maintainer while this ticket proceeds.

No cgo anywhere. That is what makes cross-compilation trivial, and it is worth asserting in the pipeline rather than assuming. Otherwise a dependency that quietly pulls in cgo would be found by a user on a musl system.

This is the v1.0 exit criterion in docs/09, so the release notes and the upgrade procedure belong here too.

## Acceptance criteria

- [x] `go run ./tools/release` builds ketju for linux/amd64 and linux/arm64 with CGO_ENABLED=0 and -trimpath. It fails unless every binary records CGO_ENABLED=0 and no build tags, and every ELF has no interpreter and no needed library
- [x] The run fails, naming the package, if any non-standard package in cmd/ketju's dependency graph has cgo files (TestNoDependencyUsesCgo)
- [x] The version and commit are stamped into internal/buildinfo, and `ketju version` reports them with the Go version and platform. An unstamped build reports its VCS revision, and the lock record and serve's start log carry the same version
- [x] The run writes SHA256SUMS in sha256sum format and verifies it
- [x] CI's Build and Test job runs the dry run as an added step and publishes nothing. No trigger, permission or job changes
- [x] The README's upgrade procedure runs as a test against a data directory that the build at ed82f15e wrote, migrations included (TestREADMEUpgradeProcedure)
- [x] The v1.0 release notes are drafted in docs/releases/v1.0.md, naming the platforms and the upgrade procedure

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e.

### Settled in grooming

- **No cgo, asserted rather than assumed** (docs/00 "Single static Go binary"; docs/07, where CI builds `./cmd/ketju` with `CGO_ENABLED=0`). CI already has the step "the binary builds without cgo" (TKT-01M29CYS9W). This ticket adds the assertions that step cannot make: the binary's recorded build settings, a static ELF, and no dependency with cgo files.
- **What stays with the maintainer** (docs/15 "The stop rule, changed", class 3):
  - publishing a binary or an image;
  - creating a signing key or a registry credential;
  - pushing a tag, v1.0 included.
- **What CI may change** (docs/12 "Changing CI", which docs/13 to docs/15 keep). Steps may be added to the `Build and Test` job. No new job, no new required check, no trigger or permission change, and no edit to `terva-review.yml`.
- **The split.** A tag-triggered workflow is a new trigger, and publishing needs write permission and a credential. Both lie outside that allowance, so they moved to TKT-01M3YSVEDJ (Publish release binaries, checksums and an image from a tag), which carries the open decision. This ticket is the dry run: everything the pipeline does before it publishes.
- **The platforms** are `linux/amd64` and `linux/arm64`. This is a run decision, pending maintainer review, recorded in docs/02 in this grooming PR. Windows lost because `serve` refuses to start without the unix data-directory lock (`internal/lock/lock_other.go`); macOS, FreeBSD and 32-bit ARM lost because no runner starts them.
- **`/healthz` carries no version** (docs/02 "Observability"). The version goes in `ketju version`, the lock record (`lock.Holder.Version`, already there) and serve's start log line. It does not go in `/healthz`.

### Approach

1. **`internal/buildinfo`.**
   - The unexported variables `version` and `commit`, set by `-ldflags -X github.com/terva-sh/ketju/internal/buildinfo.version=…`.
   - `Get() Info` returns `{Version, Commit, Modified, GoVersion, OS, Arch}`. When the variables are unset, it falls back to `debug.ReadBuildInfo`: `Main.Version`, `vcs.revision` and `vcs.modified`. A development build then still reports its commit, and `(devel)` as its version.
   - `internal/lock`'s `version()` calls it.
2. **`ketju version`.** A subcommand, also reached as `ketju --version`. It prints one line, `ketju v1.0.0 (commit 0123abcd, go1.27.1, linux/amd64)`, and `-dirty` after the commit when `vcs.modified` is true. Add it to `main.go`'s usage and switch. serve's `ketju started` log line gains `version` and `commit`.
3. **`tools/release`.** A `main` package inside the module, so that `go build ./...`, `go vet ./...` and `go test ./...` cover it. It is never shipped. `go run ./tools/release --version V --out DIR`:
   1. reads the commit from `git rev-parse HEAD` and refuses if `git status --porcelain` is not empty, unless `--allow-dirty` is given (CI's checkout is clean);
   2. for each platform, runs `go build -trimpath -ldflags "-s -w -X …version=V -X …commit=C" -o DIR/ketju_V_<os>_<arch> ./cmd/ketju`, with `CGO_ENABLED=0`, `GOOS`, `GOARCH` and an empty `GOFLAGS`;
   3. **asserts no cgo**:
      - `debug/buildinfo.ReadFile` records `CGO_ENABLED=0`, `-trimpath=true`, the target `GOOS` and `GOARCH`, and no `-tags` (so an `e2e` build cannot be released) and no `-race`;
      - `debug/elf` finds no `PT_INTERP` program header and `ImportedLibraries()` is empty;
      - `go list -deps -f '{{if not .Standard}}{{if .CgoFiles}}{{.ImportPath}}{{end}}{{end}}' ./cmd/ketju`, run with `CGO_ENABLED=1` for each target, prints nothing. Without this check, a dependency that would quietly use cgo where it can is found only by a user on musl.
   4. writes `DIR/SHA256SUMS` in `sha256sum` format (`<hex>  <name>`, sorted), then reads it back and verifies every file;
   5. runs the binary for the host platform with `version` and checks that it prints V and C.

   The tool publishes nothing and has no publish mode. TKT-01M3YSVEDJ adds the publishing as workflow steps after it.
4. **CI.** Add one step to `Build and Test`, after "the binary builds without cgo": "release dry run (builds and checks, publishes nothing)", running `go run ./tools/release --version v0.0.0-ci --out "$(mktemp -d)"`. Its comment names this ticket and docs/15 class 3. Nothing else in `ci.yml` changes.
5. **The upgrade path.**
   - **The fixture.** Commit `cmd/ketju/testdata/upgrade/ed82f15e/`, a data directory written by the build at ed82f15e. That commit is main as Phase 4 grooming began, at migration 0024, before any Phase 4 migration. To make it:
     1. build that commit in a scratch worktree with `-tags e2e`;
     2. run `ketju e2e -dir` to seed a data directory;
     3. stop it with SIGTERM, which checkpoints the WAL;
     4. commit `db/ketju.db`, the `.log` files and `blobs/`.

     A `README` beside the fixture names the commit and those commands.
   - **The README "Upgrading" section.** The steps are:
     1. read the release notes;
     2. take a backup with `ketju backup` (TKT-01M29CXK35);
     3. stop serve;
     4. replace the binary;
     5. run `ketju config check`;
     6. start serve, which applies migrations at startup, each in one transaction (`TestFailedMigrationLeavesNothing`);
     7. run `ketju fsck`.

     The section also says that a migrated database refuses an older binary (`TestNewerSchemaRefused`), so going back means the old binary and the backup together.
   - **The test.** `TestREADMEUpgradeProcedure` runs the section's `sh` block over a copy of the fixture, as `TestREADMERecoveryProcedure` runs its block. The migrations apply; `ketju fsck` exits 0; serve starts; the seeded thread reads back with all 60 posts; and the seeded members sign in.
6. **Release notes.** Add `docs/releases/v1.0.md`, a draft with these parts:
   - what v1.0 holds, by docs/09 phase;
   - the supported platforms;
   - the upgrade procedure, as a link to the README;
   - the known limits: no non-JS client, single process, a reverse proxy for TLS;
   - the decisions still pending maintainer review.

   TKT-01M3YMG0NX's v1.0 checklist links it.

### Tests

- `tools/release`:
  - `TestCheckBuildInfo` refuses build info that records `CGO_ENABLED=1`, `-tags=e2e`, `-race`, or a wrong `GOARCH`;
  - `TestCheckStaticELF` passes a binary the test builds with `CGO_ENABLED=0` and refuses one built with cgo, skipped where no C compiler is found (CI has gcc);
  - `TestChecksums` writes the file, verifies it, and fails verification after one byte of one binary changes;
  - `TestNoDependencyUsesCgo` runs step 3's `go list` check for the host platform.
- `internal/buildinfo`:
  - `TestGetPrefersStamped`;
  - `TestGetFallsBackToVCS`, which reads build info whose version is unset and finds `vcs.revision`.
- `cmd/ketju`:
  - `TestVersionCommand` checks the line's shape with the variables set through a test seam;
  - `TestLockRecordsVersion` checks that the lock record carries `buildinfo.Get().Version`;
  - `TestREADMEUpgradeProcedure` (step 5).
- The CI step itself is the end-to-end test. A failing assertion fails `Build and Test`, and the step log shows the two binaries, their sizes and the verified sums.
- Mutations to try and record in the commit body: dropping `CGO_ENABLED=0` from the build env; adding `-tags e2e`; corrupting a sum.

### Out of scope

All of the following is TKT-01M3YSVEDJ, which waits on the maintainer:
- the tag-triggered workflow;
- uploading the binaries and checksums;
- the container image and its Dockerfile;
- signing;
- the v1.0 tag itself.

Reproducible builds (building twice and comparing) are left out, with no ticket.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:02Z

run decision: pending maintainer review (2026-10-02, Phase 4 grooming). Release binaries are built for `linux/amd64` and `linux/arm64`. Recorded in docs/02 after "Backups". Windows lost, because serve refuses to start without the unix lock. macOS, FreeBSD and 32-bit ARM lost, because no runner starts them.

Split during grooming: the tag workflow, publishing, the container image and signing moved to TKT-01M3YSVEDJ (Publish release binaries, checksums and an image from a tag). They are docs/15 stop class 3 and outside docs/12 "Changing CI", so that ticket carries an open decision. This ticket is the dry run, and it can proceed. It now depends on TKT-01M29CXK35 (Ship ketju backup and the backup documentation), because the README upgrade procedure it tests takes a `ketju backup` first.

**agent:claude-code/phase4-run-cxk3s** at 2026-10-02T22:06:26Z

Decisions made while building this, beyond the plan:

- **The upgrade backup and the schema.** Writing the upgrade test showed that `ketju backup`'s offline catch-up migrated the copy, so the backup an upgrade takes would have been one the old binary refuses. That was fixed in TKT-01M29CXK35 (PR #373) before it merged: a copy whose schema is not the binary's is left at it, neither migrated nor caught up (`db.Pending`, `TestBackupLeavesAnotherSchemaAlone`). The README's upgrade step 2 relies on it, so that the new binary can back up a data directory an older one wrote.
- **A binary without `backup`.** The README tells someone whose ketju has no `backup` command to stop serve and run the new binary's backup. With serve running, the new binary would ask an old serve for a snapshot over a socket the old serve does not have, and get exit 4.
- **The test runs the backup offline.** The old serve cannot run in the test, so step 2 runs the way the README gives it for an old binary. Serve is started and stopped in-process where the README says to, and the README's three sh blocks run as written.
- **Migrations in the fixture's upgrade.** At this merge main is still at migration 0024, which is the fixture's version, so today the upgrade applies no migration. The test asserts `db.Pending` is 0 after serve starts and that the backup still records the fixture's count. It logs both numbers, so the first Phase 4 migration to land is exercised with no change to the test. AC6 is ticked on that basis: the procedure, migrations step included, runs against the ed82f15e data directory.
- **The fixture** is db/ketju.db (516 KB, empty WAL after SIGTERM) and 68 logs, about 800 KB in all. No .idx files. blobs/ was empty and git keeps no empty directory, so the test makes it. Its README records the commit and commands.
- **`--allow-dirty`** exists for local runs only. CI's checkout is clean.
- **Mutations tried, each caught:**
  - dropping CGO_ENABLED=0 from the build env (`CGO_ENABLED="1", want 0`);
  - adding `-tags e2e` (`built with tags "e2e"`);
  - corrupting a written sum (the digest mismatch).

  A first sum mutation, overwriting the first hex digit with 0, survived, because both real digests happened to begin with 0. It was replaced with one that changes the last digit.

## Summary

Landed the release pipeline's dry run. Nothing is published, and publishing stays with TKT-01M3YSVEDJ.

- **`internal/buildinfo`** carries the version and commit, stamped with `-ldflags -X`. An unstamped build falls back to its VCS revision. `ketju version` (also `--version`) prints the version, commit, Go version and platform. The lock record and serve's `ketju started` line carry the same version.
- **`tools/release`** (`go run ./tools/release --version V --out DIR`) builds linux/amd64 and linux/arm64 with CGO_ENABLED=0, -trimpath and an empty GOFLAGS. It fails unless each binary:
  - records CGO_ENABLED=0, -trimpath and its target, with no tags and no -race;
  - is an ELF with no interpreter and no needed library;
  - has no non-standard dependency with cgo files. This is checked with `go list -deps` and cgo on, and the failure names the package.

  It then writes SHA256SUMS in sha256sum format, verifies it, and runs the host binary's `version`. A dirty tree is refused unless `--allow-dirty` is given.
- **CI.** One step was added to `Build and Test`, after "the binary builds without cgo". Nothing else in ci.yml changed.
- **Upgrade.**
  - The README has an "Upgrading" section: release notes, `ketju backup`, stop serve, replace the binary, `ketju version` and `ketju config check`, start serve (which migrates), then `ketju fsck`. It also says how to go back.
  - TestREADMEUpgradeProcedure runs the section's three sh blocks against a copy of `cmd/ketju/testdata/upgrade/ed82f15e/`, which is the ed82f15e e2e seed's db and logs. It starts serve where the README says to. It checks that the backup stays at the old schema, that nothing is pending after serve starts, that fsck is clean, that the seeded thread reads back with 60 posts, and that three seeded members sign in.
- **Release notes.** `docs/releases/v1.0.md` is a draft.
