---
schema: 3
id: TKT-01M29CGBY16MK20W1QATC5N7WE
title: Add Playwright smoke tests at both viewports
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - area:test
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CEE6V358M5KCBJGHD58HM
  - TKT-01M29CEE79NN4Q0RXXEYVJHYYE
  - TKT-01M29BW9QP2Z5XCYBBDP3V0T73
  - TKT-01M29CCBYX4V6ZP7N9HF8H8NCZ
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
blocks_on: none
references:
  - ref: design:testing
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-24T05:21:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

A Playwright run against a dev server with the fake OIDC issuer, covering sign in, read a thread, reply, and see the unread divider, executed at a 390 px phone viewport and a 1440 px desktop viewport.

Both viewports in CI is the mechanism behind the docs/09 mitigation for phone layout being bolted on late. A phone layout that is only ever checked by hand is a phone layout that breaks between releases and nobody notices until a member says so.

## Acceptance criteria

- [ ] The suite runs at 390 px and 1440 px and a failure at either width fails CI
- [ ] The fake issuer supports sign-in with no external network access
- [ ] The run covers sign in, read, reply, and the unread divider end to end
- [ ] The suite passes 20 consecutive local runs with no flake before the CI job becomes a merge gate, and the run count is recorded in a note

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `web/client/playwright.config.ts` defines two projects, `phone` (390x844, `isMobile`, `hasTouch`) and `desktop` (1440x900). `webServer` starts the Go binary (`ketju serve`, CCBYX) against a temp data dir on :8080 with the fake issuer configured as the only provider.
2. Fake issuer: `internal/auth/oidc/fakeidp_test.go` is test-only and cannot be started by a dev server. Promote it (or a trimmed copy) to an importable package such as `internal/auth/oidc/oidctest`, plus a dev entry point (a `ketju dev-idp` subcommand or a small `cmd/fakeidp`) that serves discovery, JWKS, authorize and token on loopback with a fixed test user. It must make no external network calls.
3. Seed data: forums and threads must exist. Forum admin screens are phase-2 (CXK19), so seed through a Go test helper or CLI (for example `ketju dev seed` with a forum, a 60-post thread, and a second user) run in `globalSetup`. No ticket owns this yet.
4. `e2e/smoke.spec.ts`, run in both projects: sign in through the fake issuer; open the forum index; open the seeded thread and assert posts render; reply through the composer (the sheet at 390 px) and assert the new post appears; set the read position for the second user, add posts as the first, sign in as the second, and assert "N new posts below" at the right place.
5. `e2e/screenshots.spec.ts` writes the committed PNGs used for the "matches the mockups" by-eye comparison (CCC0F, CEE5Y, CEE6V). It asserts nothing, so a font-rendering difference cannot make it flake.
6. CI: Playwright's bundled browsers are glibc builds and do not run on the musl golang:1.27-alpine image. Either `apk add chromium` and set `launchOptions.executablePath` to it (chromium only), or run a separate job in a Playwright image, if the curated registry mirror carries one. To make it gate merges, the steps must run inside the required `Build and Test` job, or the branch protection must add a context. Changing branch protection is a remote-settings change and needs explicit ticket authorization.
7. Stability gate: run the suite 20 times consecutively on a local machine (`--repeat-each=20` or a loop), record the result in a ticket note, and only then make it required.

### Files
`web/client/playwright.config.ts`, `web/client/e2e/*`, `internal/auth/oidc/oidctest/` (new), a dev seed and dev-idp entry point (new), `.forgejo/workflows/ci.yml`.

### Tests
- AC1: two projects in one run; CI invokes `npx playwright test` with no `--project` filter, and the exit code fails the step. To prove it, break a phone-only selector once on a branch and watch CI fail.
- AC2: run the suite in CI with the network restricted, or assert that the fake issuer's URL is loopback. The Go unit tests for `oidctest` use no network.
- AC3: `smoke.spec.ts` covers the four steps above in each project.
- AC4: 20 consecutive local runs with no failure, logged in a ticket note, before the job is marked required.

### Design
docs/02 "Testing strategy" (Client); docs/09 Phase 1 exit and the risk table; docs/07 `/auth/{provider}/start`; the TKT-01M29C7ZZ9 description (the fake issuer that "makes the Playwright smoke tests possible later").

### Interactions
Depends on CEE6V, CEE79 and BW9QP (CI). It also needs CCBYX (serve), C7ZZ9 (OIDC flow), CA9B9, CA9CE and a seeding path, none of which are declared dependencies. TKT-01M38WTK (real devices) runs after this is green. CGBXK's a11y and target specs share the config.

### Settled in grooming (2026-09-24)

- The fake issuer moves to an importable package (C7ZZ9 plan: oidctest). The harness runs `ketju serve` built with an `e2e` build tag that registers the fake issuer and a fixture seeder. Neither is compiled into release builds.
- It runs inside the existing Build and Test job on a Debian-based image (docs/12, Changing CI). Do not add a new required check.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Stable enough to gate merges' was subjective. It now means 20 consecutive local runs with no flake before the job is made required.
