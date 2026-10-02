---
schema: 3
id: TKT-01M3YSVEDJ15KNH2VB1JET7GRF
title: Publish release binaries, checksums and an image from a tag
type: chore
status: draft
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
claim: null
archive: null
created_at: 2026-10-02T17:16:07Z
updated_at: 2026-10-02T17:18:03Z
created_by:
  id: agent:claude-code/phase4-groom-ops
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-ops
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
- [ ] Signing is done as the maintainer decided, or left out if they decided against it
- [ ] No credential is in the repository. The workflow reads each one from a secret the maintainer created
- [ ] A pre-release tag the maintainer pushes publishes artifacts that verify with sha256sum -c on a clean machine

## Implementation plan

Groomed 2026-10-02 for Phase 4 (TKT-01M3YMG0M1) against main at ed82f15e.

### Settled in grooming

- **This ticket is the part of the release pipeline that leaves the repository.** TKT-01M29CXK3S (Build the release pipeline for the single binary) builds and checks the artifacts and runs them dry in CI. This ticket publishes them.
- **Everything here is the maintainer's** (docs/15 "The stop rule, changed", class 3, and "Never, added"):
  - publishing a binary or an image;
  - creating a signing key or a registry credential;
  - pushing a tag.
- **CI's allowance** (docs/12 "Changing CI") does not cover a new trigger, a new permission, a new job or a secret. A tag-triggered workflow needs all of them.
- **The image must work as `serve` needs.** OIDC discovery calls the identity provider over HTTPS, so the image carries CA certificates. The zone database is already embedded (`time/tzdata` in `cmd/ketju/main.go`). The data directory is a mounted volume, and the process runs as a non-root uid that owns it.

### Open decision

Every item below is docs/14 class 3 (anything outside the repository), and the first is also outside the CI allowance docs/12 grants. The run chooses none of them. The ticket stays in draft until the maintainer answers.

1. **The workflow.** May the repository gain `.forgejo/workflows/release.yml`, triggered by `push: tags: ['v*']`, with the write permission an upload needs? It would be a new workflow and not a required check.
2. **Where the binaries and `SHA256SUMS` are published.** The choices are a Forgejo release on `git.local.sothr.com`, a GitHub release on the public mirror `github.com/terva-sh/ketju` (README "The name, and where this lives"), or both. The maintainer also decides which token the workflow uses and who creates it.
3. **The container image.** Whether v1.0 has one at all. If it does: which registry (the internal `container.local.sothr.com`, a public one, or both), the image name, the base (`scratch` with CA certificates, or a distroless static image), and the registry credential.
4. **Signing.** Whether `SHA256SUMS`, and the image if there is one, are signed. If they are: the tool (minisign, cosign or GPG), and who creates and holds the key.
5. **The tag.** The maintainer pushes `v1.0` when TKT-01M3YMG0NX's checklist is done.

### Approach

This is written so that the answers above slot in. It is revised at claim.

1. Add `release.yml`, running on a pushed `v*` tag. It checks out the tag, then runs `go run ./tools/release --version "$TAG" --out dist`, which makes the same assertions as the CI dry run. It refuses a tag that is not on `main`.
2. Upload `dist/*` and `SHA256SUMS` to the chosen release pages, with the token from a secret the maintainer created.
3. If an image is chosen, add a `Dockerfile` that copies the `linux/amd64` and `linux/arm64` binaries into the chosen base. It sets `USER 65532:65532`, `VOLUME /data`, `ENV KETJU_CONFIG=/data/config.toml` and `ENTRYPOINT ["/ketju", "serve"]`. Build and push it for both platforms with the registry credential from a secret.
4. If signing is chosen, sign `SHA256SUMS`, and the image, with the key from a secret.
5. Add "Installing a release" to the README: download, verify the sums (and the signature), and run. Add the image's run line too, if there is an image.

### Tests

- The workflow runs once on a throwaway pre-release tag that the maintainer pushes, such as `v0.99.0-rc1`, before `v1.0`. Its log shows the assertions, the uploads and the verified sums. Someone then downloads the published artifacts on a clean machine and checks them with `sha256sum -c`, and with the signature if there is one.
- If there is an image: `docker run` with an empty mounted volume and a minimal config starts serve. Its `/healthz` answers, and the process runs as uid 65532.

### Out of scope

- The build, the cgo assertions, version stamping, the checksums file and the dry run: TKT-01M29CXK3S.
- The v1.0 checklist and the Phase 4 exit tests: TKT-01M3YMG0NX.

## Notes

**agent:claude-code/phase4-groom-ops** at 2026-10-02T17:18:03Z

Filed 2026-10-02 in Phase 4 grooming, split from TKT-01M29CXK3S (Build the release pipeline for the single binary). It carries an open decision under docs/14/15 stop class 3, and also outside docs/12 "Changing CI". The plan lists five questions: the workflow and its trigger, where artifacts are published, the image, signing, and the tag. It stays in draft until the maintainer answers. TKT-01M3YMG0NX's criterion that every phase-4 ticket is done or archived therefore waits on this ticket too.
