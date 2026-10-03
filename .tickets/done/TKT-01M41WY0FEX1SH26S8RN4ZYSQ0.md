---
schema: 3
id: TKT-01M41WY0FEX1SH26S8RN4ZYSQ0
title: Release through the organization's twin Forgejo and GitHub workflows
type: chore
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:phase4-run
    path: docs/15-phase4-run.md
  - ref: ticket:TKT-01M3YSVEDJ
    path: null
  - ref: pr:410
    path: null
claim: null
archive: null
created_at: 2026-10-03T22:07:40Z
updated_at: 2026-10-03T23:16:58Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

The maintainer asked on 2026-10-03, before the first tag: "Match our release mirroring and deployment process to the other repositories in our terva-sh org." They had asked what the two release tokens are for, because no other repository in the organization has them.

### What the other repositories do

Surveyed 2026-10-03 from git-ticket, lampi, rihma, terva and git-ticket-canvas (`.forgejo/workflows/release.yml`, `.github/workflows/release.yml`, AGENTS.md):

- **Two twin workflows fire on the same `v*` tag.** `.forgejo/workflows/release.yml` publishes the Forgejo release with `BOT_TOKEN`, the organization secret `terva-review.yml` already uses. `.github/workflows/release.yml` runs only on github.com, guarded by `github.server_url == 'https://github.com'`, and publishes the GitHub release with the `GITHUB_TOKEN` Actions provides. Neither forge reaches into the other, so no extra token exists.
- **The mirror is a plain push.** The maintainer pushes `main` and the tag to the `github` remote (`git push github main --follow-tags`), which starts the public release, and then pushes the tag to `origin`, which starts the internal one. Each forge builds for itself, so the bytes may differ between them.
- **The image is on GHCR only** (`ghcr.io/terva-sh/<repo>`). The GitHub workflow pushes it with `GITHUB_TOKEN` and `packages: write`. The Forgejo workflow builds the image and pushes nothing. Image tags drop the `v`: `1.0.0`, `1.0` and `latest`, and a pre-release gets only its own version.

### What ketju does now

`.forgejo/workflows/release.yml` (TKT-01M3YSVEDJ) publishes all three from this forge: the Forgejo release with `RELEASE_FORGEJO_TOKEN`, the GitHub release with a fine-grained `RELEASE_GITHUB_TOKEN`, and an image on `container.local.sothr.com` with the `CONTAINER_PUSH_REPO_*` pair. That needs two secrets no other repository has, and an image registry no other release uses.

### What this ticket does

It moves ketju to the organization's pattern and keeps what ketju checks that the others do not: `tools/release`'s assertions, the anonymous `sha256sum -c` download check, the image's uid, and the published-once rule. The builder stays `tools/release` rather than goreleaser. The other repositories use goreleaser, but `tools/release` is what CI's dry run and the Phase 4 exit evidence rest on.

Outside the repository (docs/15 stop class 3), for the maintainer: the GitHub repository `terva-sh/ketju` does not exist yet, or is private (the API answered 404 on 2026-10-03). Creating it and pushing to it are the maintainer's.

## Acceptance criteria

- [x] The Forgejo release workflow publishes with BOT_TOKEN, and no workflow reads RELEASE_FORGEJO_TOKEN, RELEASE_GITHUB_TOKEN or the CONTAINER_PUSH_REPO_* pair
- [x] .github/workflows/release.yml, guarded to github.com, builds with tools/release, publishes the GitHub release and SHA256SUMS with GITHUB_TOKEN, and checks the download anonymously
- [x] The image is pushed only to ghcr.io/terva-sh/ketju, from the GitHub workflow, tagged like the other repositories; the Forgejo workflow builds and smoke-tests it and pushes nothing
- [x] README, the release notes, the v1.0 checklist, docs/02 and docs/15 describe the new process, and tests cover both workflows

## Implementation plan

1. `.forgejo/workflows/release.yml`: the `forgejo` job reads `BOT_TOKEN`. Drop the `github` job. The `image` job builds both platforms, checks arch and uid, smoke-tests `ketju version`, and pushes nothing, as lampi's does. The Forgejo build passes the internal `CERTS_IMAGE`.
2. New `.github/workflows/release.yml`, every job guarded by `github.server_url == 'https://github.com'`:
   - `release`: checkout with full history; Go from `go.mod`; check the tag's form and that it is on main; build with `go run ./tools/release`; publish with `gh` and `GITHUB_TOKEN`, using the notes in `docs/releases/${TAG}.md`; apply the same published-once asset rule; check the download anonymously.
   - `image`: download and check the GitHub release; buildx for amd64 and arm64; push to `ghcr.io/terva-sh/ketju` as `1.0.0`, `1.0` and `latest` (a pre-release gets its version alone), and keep a version that is already there; then pull each platform back (QEMU for arm64) and check `ketju version` and the uid.
3. `Dockerfile`: `CERTS_IMAGE` defaults to public `alpine:3.22`, so the GitHub build needs no internal host.
4. `tools/release/workflow_test.go`: the secrets test covers the Forgejo workflow, so README lists `BOT_TOKEN` alone. Add tests that every GitHub job carries the guard and that the GitHub workflow builds with `tools/release`. Apply the env-only rule to both files.
5. Docs: README (installing, the container image, publishing a release), the v1.0.0 notes, the v1.0 checklist part 3, docs/02 if it names the registry, and docs/15 "Decided by the maintainer".

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T22:07:45Z

Maintainer decision, 2026-10-03: "Match our release mirroring and deployment process to the other repositories in our terva-sh org." This replaces the docs/15 decision of 2026-10-02 that the image is on container.local.sothr.com, and the release secrets RELEASE_FORGEJO_TOKEN, RELEASE_GITHUB_TOKEN and the CONTAINER_PUSH_REPO_* pair. It is also the grant for adding .github/workflows/release.yml, beyond docs/12 "Changing CI", for that file only. The request is the promotion of this ticket.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T22:43:36Z

Review 2084 (head 3759d4e, base edfad27), medium, "Preserve the minor image tag for two-component releases": fixed. The tag step took ${VERSION%.*}, which gives 1 for an accepted v1.0. It now takes the first two components with cut -d. -f1-2, and skips the alias when it equals the version. TestImageTagsFollowTheOrganization runs the step's own script for v1.0.0, v1.2.3, v1.0 and v1.0.0-rc.1.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T23:16:58Z

Review 2085 (head 7f2b3c0, base edfad27), medium, "Keep immutable two-component versions separate from floating minor aliases": fixed. A v1.0 release's image tag 1.0 was the alias a later v1.0.1 moves, and the existence check looks only at the version tag. Both workflows now accept only vMAJOR.MINOR.PATCH[-PRE]. That also suits the maintainer's choice of v1.0.0 and is the form Go's module tooling reads as a version. The README says so. The alternative of keeping two-part tags and mapping them to an unambiguous image name (such as 1.0.0) lost, because it publishes an image under a name the tag does not carry. TestReleaseTagsHaveThreeParts runs each workflow's own tag pattern against accepted and refused tags.

## Summary

The release now follows the terva-sh organization's pattern, as in git-ticket and lampi. .forgejo/workflows/release.yml publishes this forge's release with the organization secret BOT_TOKEN. It builds and runs the image but pushes it nowhere. The new .github/workflows/release.yml has every job guarded to github.com. It builds with tools/release and publishes the GitHub release with GITHUB_TOKEN. It then pushes the image to ghcr.io/terva-sh/ketju as 1.0.0, 1.0 and latest; a pre-release gets only its version. Finally it pulls each platform back and checks the version line and uid 65532. The workflows no longer read RELEASE_FORGEJO_TOKEN, RELEASE_GITHUB_TOKEN or CONTAINER_PUSH_REPO_*. The Dockerfile's CA source defaults to the public alpine:3.22, and the Forgejo job passes its registry copy. Tests: TestReleaseWorkflowsSplitByForge, both workflows under the env-only and tools/release tests, and README listing BOT_TOKEN alone. Checked locally on 2026-10-03: actionlint is clean on the GitHub workflow; a dry-run release built into the image with podman and answered 'ketju v0.0.0-ci (commit …)' as 65532:65532 amd64; go test -race ./... passes. The builder stays tools/release, not goreleaser, because CI's dry run and the Phase 4 exit evidence rest on it. Outside the repository, for the maintainer: create the public github.com/terva-sh/ketju (404 on 2026-10-03), add a github remote, and make the ghcr.io package public after its first push.
