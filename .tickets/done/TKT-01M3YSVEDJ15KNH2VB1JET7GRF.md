---
schema: 3
id: TKT-01M3YSVEDJ15KNH2VB1JET7GRF
title: Publish release binaries, checksums and an image from a tag
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: TKT-01M29CXK3SMVHGVBGF4N57QACG
dependencies:
  - TKT-01M29CXK3SMVHGVBGF4N57QACG
blocks_on: none
references:
  - ref: design:roadmap-phase-4
    path: docs/09-roadmap.md
  - ref: design:phase4-run
    path: docs/15-phase4-run.md
  - ref: pr:358
    path: null
  - ref: pr:385
    path: null
claim: null
archive: null
created_at: 2026-10-02T17:16:07Z
updated_at: 2026-10-03T01:01:02Z
created_by:
  id: agent:claude-code/phase4-groom-ops
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-svedj
  name: ""
extensions: {}
---

## Description

The part of the release pipeline that leaves the repository. It was split out of TKT-01M29CXK3S (Build the release pipeline for the single binary) during Phase 4 grooming, so that the dry run can proceed while this part waits on the maintainer.

TKT-01M29CXK3S builds the binaries, asserts that they have no cgo, stamps the version and commit, writes `SHA256SUMS`, and runs all of it as a dry run in CI. This ticket takes those artifacts and publishes them when a tag is pushed: the binaries and checksums, a container image for people who deploy that way, and any signature.

Every part of it falls in docs/15 stop class 3, which keeps these with the maintainer:
- publishing a binary or an image;
- creating a signing key or a registry credential;
- pushing a tag.

A tag-triggered workflow also lies outside docs/12 "Changing CI", which allows only added steps in the one required job, with no new trigger or permission. The plan's open decision lists what the maintainer must choose.

## Acceptance criteria

- [ ] A pushed v* tag runs the release workflow. The workflow builds with tools/release, making the same assertions as the dry run, and publishes the binaries and SHA256SUMS where the maintainer decided
- [ ] If the maintainer chose an image, it is built from the release binaries with CA certificates, runs as a non-root uid, and serves from a mounted data directory
- [x] Signing is done as the maintainer decided, or left out if they decided against it
- [x] No credential is in the repository. The workflow reads each one from a secret the maintainer created
- [ ] A pre-release tag the maintainer pushes publishes artifacts that verify with sha256sum -c on a clean machine

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e. Revised at claim, 2026-10-03, against main at 5f50eb27, with the maintainer's answers to the open decision.

### Settled in grooming

- **This ticket is the part of the release pipeline that leaves the repository.** TKT-01M29CXK3S (Build the release pipeline for the single binary) builds and checks the artifacts and runs them dry in CI. This ticket publishes them.
- **Everything here is the maintainer's** (docs/15 "The stop rule, changed", class 3, and "Never, added"):
  - publishing a binary or an image;
  - creating a signing key or a registry credential;
  - pushing a tag.
- **CI's allowance** (docs/12 "Changing CI") does not cover a new trigger, a new permission, a new job or a secret. A tag-triggered workflow needs all of them.
- **The image must work as `serve` needs.** OIDC discovery calls the identity provider over HTTPS, so the image carries CA certificates. The zone database is already embedded (`time/tzdata` in `cmd/ketju/main.go`). The data directory is a mounted volume, and the process runs as a non-root uid that owns it.

### Decided by the maintainer (2026-10-02, the ticket's notes; docs/15)

1. **The workflow.** `.forgejo/workflows/release.yml`, on `push: tags: ['v*']`, with the write permission an upload needs; not a required check. A grant beyond docs/12 "Changing CI" for that file only, with the Dockerfile and the README section this plan names. `ci.yml` is not touched.
2. **Where.** A release on git.local.sothr.com and a release on the public mirror github.com/terva-sh/ketju.
3. **The image.** `scratch` with CA certificates, on `container.local.sothr.com/terva-sh/ketju`, tagged with the version and `latest`.
4. **Signing.** None for v1.0.
5. **The tag and the credentials** stay with the maintainer. The run writes the workflow to read secrets by name; it does not create, read or test them, and does not run the publishing jobs.

### Approach (as built)

1. `release.yml`, three jobs, every value reaching a script through `env:`:
   - `forgejo` (golang:1.27-alpine): checks the tag is `vMAJOR.MINOR[.PATCH][-PRE]` and on `main`; runs `go run ./tools/release --version "$TAG" --out /tmp/ketju-release`, the CI dry run's command; creates or reuses the Forgejo release (body from `docs/releases/$TAG.md` when it exists), uploads each binary and `SHA256SUMS`; then downloads the release anonymously into an empty directory and runs `sha256sum -c`.
   - `github` (needs forgejo): downloads and checks the Forgejo release, waits up to 30 minutes for the tag on the mirror and refuses it if it names another commit, creates or reuses the GitHub release, uploads, and checks the GitHub downloads with `sha256sum -c`.
   - `image` (needs forgejo; the instance's buildah image): downloads and checks the Forgejo release, builds `Dockerfile` per platform from those binaries, checks each image's architecture and user, runs the native one's `ketju version`, logs in, and pushes one manifest list as the version and, unless a pre-release, `latest`.
   - A rerun keeps what is published if its bytes match and fails if they do not. A token reaches curl on stdin (`-K -`), never argv.
2. `Dockerfile`: a build-platform stage lays out the CA bundle, `/tmp` and a `/data` owned by 65532; the scratch stage copies them and the release binary for `$TARGETARCH`, `USER 65532:65532`, `ENV KETJU_CONFIG=/data/config.toml`, `VOLUME /data`, `ENTRYPOINT ["/ketju"]`, `CMD ["serve"]`.
3. README "Installing a release", with "The container image" and "Publishing a release", which names the four secrets: `RELEASE_FORGEJO_TOKEN`, `RELEASE_GITHUB_TOKEN`, `CONTAINER_PUSH_REPO_USER`, `CONTAINER_PUSH_REPO_PASSWORD`.
4. Signing: none.

### Tests

- `tools/release/workflow_test.go`: the secrets the workflow reads equal the README's table and the header comment's, with no reserved prefix; the workflow builds with `tools/release` and the Dockerfile copies the name it writes, with the image's user, volume and CA bundle; no run block holds an expression or `set -x`.
- Static: actionlint v1.7.7 (only the self-hosted `docker` label flagged, as for ci.yml), and every run block through `dash -n`, `busybox sh -n` and `bash -n`.
- A dry local run of the build step: `go run ./tools/release --version v1.0 --out DIR` on a clean checkout.
- The rest needs the maintainer's tag and is done through the release checklist (`docs/releases/v1.0-checklist.md`, TKT-01M3YMG0NX): a pre-release tag such as `v0.99.0-rc1` runs the workflow, its downloads verify with `sha256sum -c` on a clean machine, and `podman run` of the image with an empty volume and a minimal config starts serve as uid 65532 with `/healthz` answering.

### Out of scope

- The build, the cgo assertions, version stamping, the checksums file and the dry run: TKT-01M29CXK3S.
- The v1.0 checklist and the Phase 4 exit tests: TKT-01M3YMG0NX.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:03Z

Filed 2026-10-02 in Phase 4 grooming, split from TKT-01M29CXK3S (Build the release pipeline for the single binary). It carries an open decision under docs/14/15 stop class 3, and also outside docs/12 "Changing CI". The plan lists five questions: the workflow and its trigger, where artifacts are published, the image, signing, and the tag. It stays in draft until the maintainer answers. TKT-01M3YMG0NX's criterion that every phase-4 ticket is done or archived therefore waits on this ticket too.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T18:20:07Z

Maintainer decision, 2026-10-02, on the open decision. Asked as choices, the maintainer answered:
1. **The workflow.** Yes. They chose to publish from a workflow, which approves `.forgejo/workflows/release.yml`, triggered by `push: tags: ['v*']`, with the write permission an upload needs. It is not a required check. This is a maintainer grant beyond docs/12 "Changing CI", for this file only.
2. **Where to publish.** "Forgejo and GitHub": a Forgejo release on git.local.sothr.com, and a GitHub release on the public mirror github.com/terva-sh/ketju.
3. **Container image.** Yes. Base `scratch` with CA certificates, pushed to `container.local.sothr.com`. The image name is the run's to propose. The default is `container.local.sothr.com/terva-sh/ketju`, tagged with the version and `latest`.
4. **Signing.** No signing for v1.0. The maintainer did not pick minisign.
5. **The tag.** It stays with the maintainer, as docs/15 says.

What stays with the maintainer under stop class 3:
- creating the GitHub token, the Forgejo upload token and the registry credential, and storing them as repository secrets;
- pushing the `v1.0` tag.

The run may write the workflow so that it reads those secrets by name. It must not create them, read them, or run the publishing jobs. The PR that adds `release.yml` names each secret it expects. The workflow first runs on the maintainer's tag push.

**agent:claude-code/phase4-run-svedj** at 2026-10-03T00:58:40Z

Built under the maintainer's 2026-10-02 decision (note 2; docs/15 "Decided by the maintainer"). No agent created, read, printed or tested a credential, pushed a tag, ran the workflow, or logged in to a registry. ci.yml is unchanged.

**Criteria left unticked, and where they get done.** Three criteria are left unticked. Each is worked through on the maintainer's tag, under the release checklist `docs/releases/v1.0-checklist.md` (TKT-01M3YMG0NX, Check Phase 4's exit criterion and prepare the v1.0 release).
- **AC1 (a pushed v* tag runs the workflow and publishes).** The workflow runs only on a tag push, and the tag is the maintainer's.
- **AC5 (a pre-release tag's artifacts verify on a clean machine).** This needs the maintainer's pre-release tag, such as `v0.99.0-rc1`. The workflow checks its own Forgejo and GitHub downloads with `sha256sum -c` in empty directories. The clean-machine check is the README's install commands.
- **AC2 (the image is built from the release binaries with CA certificates, runs non-root, and serves from a mounted data directory).** The Dockerfile and the image job are written to do exactly that. The job builds from the downloaded, checked release, asserts each image's architecture and `65532:65532` user, and runs the native image's `ketju version` before pushing. But no image has been built: the coordinator limited the local dry run to tools/release. The criterion says the image *is* built and serves from a mounted directory, and neither has been observed, so it stays unticked. The check is the first image job run, then `podman run` with an empty volume and a minimal config, looking for `/healthz` and uid 65532, as the plan's Tests say.

**Run decisions.**
- **Secret names.**
  - Chose `RELEASE_FORGEJO_TOKEN` and `RELEASE_GITHUB_TOKEN`. Reusing `BOT_TOKEN` (the review bot's, used by terva-review.yml) lost, because the maintainer's note asks for a separate upload token, and a release token should not share a review bot's identity. A `GITHUB_`, `GITEA_` or `FORGEJO_` prefix lost, because Forgejo reserves those prefixes for secrets.
  - Kept the registry names `CONTAINER_PUSH_REPO_USER` (secret or variable) and `CONTAINER_PUSH_REPO_PASSWORD`, which every repository on this forge uses. The terva-sh organization may already hold them.
- **The image is built from the published binaries, downloaded and checked, not from a rebuild.** That is what AC2 says, and it means the image and the release cannot differ.
- **The GitHub release waits for the tag on the mirror.** The README says release commits reach the mirror by the maintainer's push. Creating the tag through the API with `target_commitish` lost, because it fails when the mirror lacks the commit, and it would create a tag the maintainer did not push. The job waits up to 30 minutes and refuses a mirror tag that names another commit.
- **A rerun is safe.** An existing release is reused, and an existing asset with the same bytes is kept. One with different bytes fails the run: a version is published once.
- **A tag must be `vMAJOR.MINOR[.PATCH][-PRE]` on main.** v1.0 has two components, as the maintainer named it. A tag with a hyphen is a pre-release and does not move `latest`.

**Validation.**
- actionlint v1.7.7, with only the self-hosted `docker` runner label flagged (ci.yml gets the same flag).
- All 15 run blocks pass `dash -n`, `busybox sh -n` and `bash -n`. shellcheck is not installed here.
- `go run ./tools/release --version v1.0` on a clean checkout (674c7e85) built both binaries. `sha256sum -c` passed, and the host binary printed `ketju v1.0 (commit 674c7e85aba7, …)`.
- The tag regex was tried against v1.0, v1.0.0 and v0.99.0-rc1 (accepted) and v1, 1.0, v1.0/x and `v1.0-` (refused).
- `tools/release/workflow_test.go` keeps the workflow, the README and the Dockerfile in step. Two mutations were each caught: an expression put in a run block, and a secret dropped from the README.

## Summary

Landed the release workflow, the image and the instructions. Nothing was run against a forge, a registry or GitHub. The workflow runs for the first time when the maintainer pushes a tag.

**`.forgejo/workflows/release.yml`** runs on `push: tags: ['v*']` with `contents: write`. It has three jobs.
- **forgejo.** It checks that the tag has the expected format and is on main. It builds with `go run ./tools/release`, the CI dry run's command. It publishes the binaries and `SHA256SUMS` as a Forgejo release, using `docs/releases/$TAG.md` as the description. Then it downloads the release anonymously and checks it with `sha256sum -c`.
- **github.** It waits for the tag on the mirror and checks that the tag names the same commit. It publishes the same, checked files as a GitHub release and verifies the downloads.
- **image.** It builds the `Dockerfile` (`scratch`, the CA bundle, `/tmp`, a `/data` volume owned by 65532, `USER 65532:65532`, `KETJU_CONFIG=/data/config.toml`) for amd64 and arm64 from the published binaries. It checks each image's architecture and user, runs the native image's `ketju version`, and pushes `container.local.sothr.com/terva-sh/ketju:$TAG`, plus `latest` when the tag is not a pre-release.

Values reach scripts only through `env:`, and tokens reach curl only on stdin. Reruns keep identical published bytes and fail on different ones.

**README.**
- "Installing a release": download, verify with `sha256sum -c`, install.
- "The container image": the `podman run` line.
- "Publishing a release": the four secrets the maintainer creates (`RELEASE_FORGEJO_TOKEN`, `RELEASE_GITHUB_TOKEN`, `CONTAINER_PUSH_REPO_USER`, `CONTAINER_PUSH_REPO_PASSWORD`) and the order to push in.

**Other changes.** docs/02 and docs/releases/v1.0.md record the decision as built. Releases are not signed.

**Tests and checks.** `tools/release/workflow_test.go` keeps the workflow, the README and the Dockerfile consistent. actionlint passes, and every run block passes the dash, busybox and bash syntax checks.

**Left unticked.** AC1, AC2 and AC5 need the maintainer's tag, and the image's first build. They are done through `docs/releases/v1.0-checklist.md` (TKT-01M3YMG0NX).
