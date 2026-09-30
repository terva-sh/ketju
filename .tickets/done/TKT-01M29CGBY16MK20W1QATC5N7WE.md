---
schema: 3
id: TKT-01M29CGBY16MK20W1QATC5N7WE
title: Add Playwright smoke tests at both viewports
type: task
status: done
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
  - ref: pr:82
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:23:08Z
updated_at: 2026-09-30T13:28:27Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-s1tvpd
  name: ""
extensions: {}
---

## Description

A Playwright run against a dev server with the fake OIDC issuer, covering sign in, read a thread, reply, and see the unread divider, executed at a 390 px phone viewport and a 1440 px desktop viewport.

Both viewports in CI is the mechanism behind the docs/09 mitigation for phone layout being bolted on late. A phone layout that is only ever checked by hand is a phone layout that breaks between releases and nobody notices until a member says so.

## Acceptance criteria

- [x] The suite runs at 390 px and 1440 px and a failure at either width fails CI
- [x] The fake issuer supports sign-in with no external network access
- [x] The run covers sign in, read, reply, and the unread divider end to end
- [x] The suite passes 20 consecutive local runs with no flake before the CI job becomes a merge gate, and the run count is recorded in a note

## Implementation plan

Revised at claim (2026-09-25), after reading the code as it stands.

### Approach
1. **`internal/auth/oidc/oidctest`.**
   - `Start()` runs the IdP without a `testing.T`, and `New(t)` wraps it.
   - A `Users` list puts a sign-in page in front of the approval. Each user is a link carrying `login_as`, and the chosen user is the token's `sub`. Without `Users` it behaves as before.
   - The import guard now also allows a file whose `//go:build` line excludes it from every build without the `e2e` tag.
2. **`cmd/ketju/e2e.go`** (`//go:build e2e`) registers `ketju e2e -dir DIR -port N`. It:
   - makes the directory and starts the IdP with alice and carol;
   - writes a config with `site_url=http://localhost:N`, fsync never, and the fake provider;
   - seeds the data directory: the two users, each with an identity at the fake provider; a public forum; a 60-post thread; and carol's read position at 60;
   - writes `seed.json`, logs to `serve.log`, and runs the real `serve` with `idpClient` set to trust the IdP's certificate.
   `main.go` gains `buildCommands` for it. A release build has none.
3. **`web/client/playwright.config.ts`.**
   - Two projects: phone (390×844, isMobile, hasTouch) and desktop (1440×900).
   - One worker and no retries.
   - `webServer` builds the e2e binary and runs it in a fresh directory under `$TMPDIR` on :8787.
   - `ignoreHTTPSErrors` covers the IdP's own certificate.
   - `PLAYWRIGHT_CHROMIUM_EXECUTABLE` selects a system Chromium.
4. **`e2e/smoke.spec.ts`**, per project:
   - alice signs in through `/auth/fake/start` and the IdP's page (the client has no sign-in screen yet);
   - she reads the seeded thread, then replies (the reply bar and sheet at 390, the inline composer at 1440) and sees her post;
   - carol signs in, lands on the thread with the divider directly before #61, and sees alice's reply below it.
5. **CI.** A step added to Build and Test runs `apk add chromium` and then `npx playwright test` against `/usr/bin/chromium`.

### Settled in grooming (2026-09-24)

- The fake issuer moves to an importable package (C7ZZ9 plan: oidctest). The harness runs `ketju serve` built with an `e2e` build tag that registers the fake issuer and a fixture seeder. Neither is compiled into release builds.
- It runs inside the existing Build and Test job on a Debian-based image (docs/12, Changing CI). Do not add a new required check.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:06:25Z

Acceptance criteria rewritten during Phase 1 grooming (2026-09-24), agreed with the user as part of preparing the autonomous run. Rewritten criteria move to the end of the list, so their numbers changed. 'Stable enough to gate merges' was subjective. It now means 20 consecutive local runs with no flake before the job is made required.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T22:33:27Z

The "reply" step needs the composer to post. That is draft TKT-01M3ART992NAPPBS8PCYAWRT0J (Post a reply from the composer), filed during TKT-01M29CEE79. It depends on TKT-01M29CA9B9 (create and reply), which is blocked by TKT-01M29P9RS0. I did not link it as a dependency of this ticket, because that would block the gate on a draft only a person can promote. Whoever promotes it should consider adding the link.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:07:14Z

**Evidence**
- **AC1 (both widths, either one fails the run).** One `npx playwright test` runs both projects with no `--project` filter, inside the required Build and Test job.
  - Proven locally: I changed the phone step's selector to one that does not exist. The phone project failed, desktop passed, and the run exited 1. Then I restored it.
- **AC2 (no external network).** I ran the whole suite in `unshare -rn` with only loopback up. There was no DNS, and an outbound HTTPS request failed, but both projects passed (4.6s).
  - The IdP is an `httptest` TLS server on 127.0.0.1. The unit tests for its sign-in page run over loopback only.
- **AC3 (sign in, read, reply, divider).** `e2e/smoke.spec.ts` covers all four in each project:
  - sign-in through the real `/auth/fake/start`, the IdP's page, and the callback, confirmed by `/api/v1/me`;
  - reading the seeded thread;
  - a reply through the phone sheet at 390 and the inline composer at 1440, with the post seen after the permalink route;
  - carol landing with `#unread` named "N new posts below" and "last read #60", directly before `post-61`, and alice's reply after it.
- **AC4 (20 consecutive local runs).** 20 consecutive full runs, each building and starting a fresh server: **20 passed, 0 failed**, each 3.4–4.6s (2026-09-25, logs in `~/.cache/ketju-gotmp/flake-*.log`). This was done before the step joined the required job.
- **CI image.** I also ran the suite in `container.local.sothr.com/library/golang:1.27-alpine` with `apk add chromium` (Chromium 152.0.7977.82) and a clean `npm ci`. Both projects passed.

**Deviation from the settled plan**
- Grooming said to run on a Debian-based image. The curated mirror has only `golang:1.27-alpine`: `1.27`, `1.27-bookworm` and `1.27-trixie` all answer "not found", with 1.27-alpine as a control that does pull.
- So the step uses Alpine's Chromium through `PLAYWRIGHT_CHROMIUM_EXECUTABLE`. That is the plan's own first option, and within docs/12, which allows adding steps.
- If a Debian image is mirrored later, Playwright's own Chromium would pin the browser version. Today it floats with Alpine's package.

**Decisions**
- **Sign-in starts at the server.** It starts at `/auth/fake/start` rather than a button, because the client has no sign-in screen yet ("Sign in" links to the /settings placeholder).
- **Seed ids.** The seed's thread id is random, and the suite reads it from `seed.json`.
- **One worker.** Both projects share one server, so each asserts only what its own run caused, or what holds however many replies there are.
- **`@playwright/test` 1.63.0** is pinned exact, as every client dependency is.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T02:12:44Z

Review 1 on PR #82 (head d96aae8, run 422) raised two findings. Both are fixed.
- High: the oidctest import guard judged a //go:build line by setting every other tag true, so e2e || (linux && !darwin) passed although a Linux build without e2e includes it. The guard now tries every assignment of the line's other tags with e2e false, and a line naming more than 16 tags fails. TestE2EOnly gains two disjunctions with negation, both of which the old check wrongly passed, and two lines that are truly e2e-only.
- Medium: playwright.config.ts put its paths into webServer.command unquoted. They are now single-quoted for the shell. The suite passed with KETJU_E2E_DIR under a directory named "q <time> it's; $HOME".
CI run 423 passed on d96aae8.

**agent:claude-code/phase2-run-s1tvpd** at 2026-09-30T13:28:27Z

How "The run covers sign in, read, reply, and the unread divider end to end" stays met after TKT-01M3S1TVPD (Stop the full Playwright run spending more /auth calls than it refills). smoke.spec.ts now starts from alice's stored session and no longer signs in, because its sign-in spent more of the /auth/* burst than the run refilled. It still covers read, reply and the unread divider at 390 px and 1440 px. Sign-in at each width is covered by phase2-exit.spec.ts's member sign-in, which goes from the client's Sign in link through /auth/signin, the fake provider and the callback to / as a member, and by sign-in.spec.ts, which covers the client's own Sign in control (the top bar at 1440 px, the Me tab at 390 px) to the provider's start. The setup project signs the four seeded readers in once. smoke.spec.ts's header comment says the same.

## Summary

Added a Playwright smoke suite (web/client/e2e) that signs in, reads a thread, replies, and checks the unread divider at 390 px and 1440 px. It runs against ketju serve built with -tags e2e, which seeds a data directory and signs in through the fake OpenID provider on loopback. It runs inside the required Build and Test job, using Alpine's Chromium because the mirror has no Debian Go image. 20 of 20 consecutive local runs passed.
