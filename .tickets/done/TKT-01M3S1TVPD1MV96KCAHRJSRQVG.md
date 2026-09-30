---
schema: 3
id: TKT-01M3S1TVPD1MV96KCAHRJSRQVG
title: Stop the full Playwright run spending more /auth calls than it refills
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:test
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: ticket:TKT-01M29CGBY16MK20W1QATC5N7WE
    path: null
  - ref: ticket:TKT-01M3MTQG5ATJPTEPZHNBYHAHBF
    path: null
  - ref: ticket:TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
    path: null
  - ref: ticket:TKT-01M3QVWKAGAG6H77VNE0WEC3SH
    path: null
  - ref: pr:233
    path: null
claim: null
archive: null
created_at: 2026-09-30T11:40:10Z
updated_at: 2026-09-30T13:39:05Z
created_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-s1tvpd
  name: ""
extensions: {}
---

## Description

In a full local Playwright run, the desktop `smoke.spec.ts` fails at its sign-in step with a 429 from the `/auth/*` limiter. Two lanes reported it, main at f388921 fails the same way, and it reproduced on 2026-09-30 on the branch for TKT-01M3MTQG5A (Stop Playwright checks timing out under CI runner contention). Run alone, the desktop project passes. CI has not hit it yet.

### Cause
The e2e server listens on 127.0.0.1, so every browser in the suite is one address to the `/auth/*` limiter: a burst of 10, then one token every 6 s (`internal/web/ratelimit.go` authRate, docs/08 "Rate limits"). A full run spends 28 requests: setup 8, then 10 per project (apply 2, phase2-exit 5, sign-in 1, smoke 2). The run passes only if it is slow enough for the bucket to refill between them. The desktop phase2-exit spec waits out its 429s and leaves the bucket empty. sign-in.spec and smoke come about 10 s later, and smoke's `/auth/fake/start` is refused. `signIn` then waits for the provider's link until the 30 s test timeout. A faster machine fails and CI's slower, loaded runner passes. So this is the reverse of the contention TKT-01M3MTQG5A covers, and it was left out of that ticket.

The server's log from that run (`serve.log`, /auth requests only, the bucket modelled from their times):
```
  0.0-0.7s  setup: 4 sign-ins, 8 requests         bucket 2.1
 26.8s      phone apply                            bucket 4.5
 32.1-36.4s phone phase2-exit, 5 requests          bucket 1.1
 48.1s      phone sign-in.spec 1, smoke 2          bucket 0.0
 77.4s      desktop apply                          bucket 2.9
 82.6-90.8s desktop phase2-exit, one 429 waited out  bucket 0.1
101.1s      desktop sign-in.spec                   bucket 0.9
101.2s      desktop smoke /auth/fake/start  429
```

### Options (not decided here)
- **Smoke reuses alice's stored session** (`test.use({ storageState: stateFile('alice') })`), which saves 2 requests per project. But TKT-01M29CGBY1's criterion "The run covers sign in, read, reply, and the unread divider end to end" at 390 px and 1440 px would then rely on the setup project's sign-in (default viewport) and sign-in.spec for the per-width part. Say so in the spec, or have a person accept it.
- **The e2e build gets its own `/auth/*` rate.** `authRate` is unexported in internal/web and `serveOpts` has no limiter hook, so this needs a test-only option outside `cmd/ketju/e2e.go`. The e2e server would then no longer run production's numbers, and phase2-exit's 429 handling would stop being exercised.
- Not this: retrying around the limit in `signIn`.

## Acceptance criteria

- [x] web/client/e2e/smoke.spec.ts sets test.use({ storageState: stateFile("alice") }), calls neither signIn nor any /auth/* URL, and asserts that GET /api/v1/me answers alice before it reads the thread
- [x] smoke.spec.ts's read, reply and unread-divider steps keep every assertion they had, in both projects
- [x] smoke.spec.ts's header comment names phase2-exit.spec.ts's member sign-in and sign-in.spec.ts as where the run covers sign-in at 390 px and 1440 px, and a note on TKT-01M29CGBY1 says the same
- [x] A note records five full local runs of npx playwright test on the PR's base and five on the branch, on the same machine, with each run's result, and all five on the branch pass
- [x] A note lists one passing run's /auth/* requests from serve.log with time, path, status and the modelled bucket, and shows no 429 outside phase2-exit.spec.ts
- [ ] CI's Playwright step passes on the PR, with the smoke test passing in both projects
- [x] e2e/session.ts exports paceAuth(n), which asserts one worker with fullyParallel false, records a paced annotation and waits n x 6 s + 0.5 s before any request, and apply.spec.ts calls paceAuth(2) and sign-in.spec.ts calls paceAuth(1) before their first /auth/* request
- [x] e2e/session.ts's comment holds the per-spend budget table (setup 8 from a full bucket; per project apply 2 and sign-in.spec 1 after paceAuth, phase2-exit 5 through through(), smoke 0) and the rule that every /auth/* spender either calls paceAuth or waits out a 429 through through()
- [x] The diff leaves internal/web/ratelimit.go, cmd/ketju/e2e.go, cmd/ketju/serve.go, web/client/playwright.config.ts and signIn unchanged, and no new code waits on or retries after a 429 response
- [x] A note records five passing branch runs of the spenders alone (npx playwright test e2e/apply.spec.ts e2e/phase2-exit.spec.ts e2e/sign-in.spec.ts e2e/smoke.spec.ts) and the result of one such run on the PR's base

## Implementation plan

Groomed 2026-09-30 against main at 4918f90 (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- **The cause is the `/auth/*` budget, not runner contention** (this ticket's description). The e2e server listens on 127.0.0.1, so every browser in the run is one address to the limiter: a burst of 10, then one token every 6 s (`authRate`, `internal/web/ratelimit.go:34`; docs/08 "The transport-level ceiling is not a posting policy"). A full run spends 28 requests: setup 8, then 10 per project (apply 2, phase2-exit 5, sign-in.spec 1, smoke 2).
- **No retry around the 429 in `signIn`** (this ticket's description, "Not this"). No spec gains a new wait or retry on a 429. `through()` in `phase2-exit.spec.ts:52-67` stays the only one.
- **The production `/auth/*` limit is not weakened.** `authRate` and the docs/08 table stay as they are.
- **The e2e server keeps production's rate.** TKT-01M3QS0TMV (Link to the link-a-sign-in page from the client) settled this in its refresh of 2026-09-30: "Raising the limit for e2e lost, because the limiter is under test like the rest." This plan follows that choice.
- **Engineering choice: option (a).** smoke.spec.ts starts from alice's stored session, which saves 2 requests per project and brings the run to 24. Option (b) is an e2e-only `/auth/*` rate through a hook. It lost for four reasons:
  1. It needs hooks in two packages that production builds. `authRate` is unexported, `web.RateLimit(now)` (`internal/web/ratelimit.go:209`) takes no rate, and `serveOpts` (`cmd/ketju/serve.go:78`) is compiled into every build. A hook that no production build can reach needs three things: a `//go:build e2e` file in `internal/web`, an `e2e`/`!e2e` file pair in `cmd/ketju` to hand it to `web.Options.RateLimit`, and a guard test like `internal/auth/oidc/oidctest/guard_test.go`. That is three files and a guard to change a number that only one binary runs.
  2. The e2e server would stop running production's numbers. Today, a client change that spends more `/auth/*` requests per sign-in fails the run, just as a member would hit it. Under (b), that change passes.
  3. `through()` would never see a 429, so its wait path would be dead code. TKT-01M3QS0TMV and TKT-01M3QVWKAG are moving it into `e2e/session.ts` for their own specs. Under (a) it still runs whenever the run's timing drains the bucket, as it did in the desktop phase2-exit of the failing run.
  4. It reverses the choice TKT-01M3QS0TMV settled.
- **How TKT-01M29CGBY1's criterion stays met.** TKT-01M29CGBY1 (Add Playwright smoke tests at both viewports) has two criteria here. "The suite runs at 390 px and 1440 px and a failure at either width fails CI" is untouched. "The run covers sign in, read, reply, and the unread divider end to end" names the run, not smoke.spec.ts. After this ticket, each width still has one real member sign-in, and every other spec reuses a stored session, as `e2e/session.ts` already asks:
  - `phase2-exit.spec.ts:159-178`, "they sign in as a member, and the decision is waiting". In each project's viewport, it starts from the client's Sign in link and goes through `/auth/signin`, `/auth/fake/start`, the fake provider's page and `/auth/fake/callback`. It lands on `/` as a member, and `/api/v1/me` names them. This is the one real member sign-in per width.
  - `sign-in.spec.ts` covers the client's own Sign in control at each width: the top bar at 1440 px and the Me tab at 390 px, to the provider's start.
  - `apply.spec.ts` signs an applicant in at each width. The setup project signs the four seeded readers in once, at the default viewport.
  - smoke.spec.ts keeps read, reply and the unread divider at each width.
  smoke.spec.ts's header says this, and a note on TKT-01M29CGBY1 records it.
- **Engineering choice, added after Terva review 1579 on PR #225: each spend that does not wait out a 429 first waits for its own refill.** Option (a) alone left desktop apply's two requests with no fixed floor. They held only if 9.2 s of other work came before them, and one observed run is not a guarantee. A new helper, `paceAuth(n)` in `e2e/session.ts`, waits `n × 6 s + 0.5 s` before a spec spends `n` requests. It runs at the start of the test, before any request, and whatever answers came before, so it is pacing, not a retry of a 429. `apply.spec.ts` calls `paceAuth(2)` and `sign-in.spec.ts` calls `paceAuth(1)`. Every row of the table below then holds with no assumption about how fast anything runs. It adds about 38 s to a full run (2 × 12.5 s + 2 × 6.5 s), against a Playwright step of 2m19s in CI run 1435. The alternatives lost:
  - **A stored or pre-seeded session for apply.** A session stored by the setup project moves apply's 4 requests into setup. Setup already spends 8 of the burst of 10, so it would need the same wait. A session seeded by `cmd/ketju/e2e.go` would mint a pre-user session outside the sign-in flow. That is a new session-issuing path to keep honest, even if only the e2e build compiles it. It would also drop apply.spec.ts's check that a first sign-in lands on the form (docs/05 "mode = approval" step 1). TKT-01M29CP2NG's criteria would allow it, but it saves 26 s by adding a Go path.
  - **Ordering, so that a guaranteed refill comes first.** No spec before desktop apply has a fixed wait. Playwright `dependencies` order projects, not files, and do not add refill time. Every ordering therefore comes down to a wait. A wait in another file, such as the one read-tracking.spec.ts gives sign-in.spec.ts today, stops protecting the spend as soon as that file changes or a new spec sorts between them. `paceAuth` puts the wait in the spec that spends.
  - **Cutting apply's requests.** A sign-in is `/auth/fake/start` and `/auth/fake/callback`, and that is already the fewest it can make.
- **The run's arithmetic after the change** (24 requests). The limiter never takes a bucket below 0 (`allowAt`, `internal/web/ratelimit.go:153-182`). `playwright.config.ts` runs one worker with `fullyParallel: false`, so no two specs spend at once. `paceAuth` asserts both, as `admin-forms.spec.ts:42-43` does. Each row holds by itself:

  | spend | cost | what makes it hold, at any speed |
  |---|---|---|
  | setup | 8 | Setup runs first, against a fresh server (`reuseExistingServer: false`), because both width projects depend on it. It starts from a full bucket of 10. |
  | apply, each project | 2 | `paceAuth(2)`: from a bucket of 0 or more, 12.5 s refills more than 2 tokens. |
  | phase2-exit, each project | 5 | `through()` waits out each 429 as `Retry-After` says (`phase2-exit.spec.ts:52-67`). This is unchanged. |
  | sign-in.spec, each project | 1 | `paceAuth(1)`: 6.5 s refills more than 1 token. |
  | smoke, each project | 0 | Option (a): it starts from alice's stored session. |

  The only rule a new spec must follow: a spec that spends `/auth/*` either calls `paceAuth` with its count first, or waits out a 429 through `through()`.

### Approach
1. `web/client/e2e/smoke.spec.ts`:
   - Add `test.use({ storageState: stateFile('alice') })` at file level, and drop `signIn` from the import of `./session`.
   - Replace the step `'sign in'` (`:18`) with a step that asserts `GET /api/v1/me` answers `user.handle === 'alice'`. A stale state file then fails there with a clear message, not at the composer.
   - Rename the test from `'sign in, read a thread, reply, and see the unread divider'` to `'read a thread, reply, and see the unread divider'`, because it no longer signs in. No CI step or doc names the old title (grep of ci.yml and docs/, 2026-09-30).
   - Rewrite the header comment (`:6-8`) to say three things: what the spec covers; why it starts from a stored session, which is the `/auth/*` budget; and where the run covers sign-in at each width, as listed under Settled.
   - carol's context comment (`:51-52`) says "the sign-in flow is alice's step above", which is no longer true. Point it at phase2-exit.spec.ts and sign-in.spec.ts.
   - The read, reply and unread-divider steps and their assertions stay as they are.
2. `web/client/e2e/session.ts`:
   - Add `export async function paceAuth(n: number)`. It first asserts `test.info().config.workers === 1` and `test.info().config.fullyParallel === false`, with messages saying the `/auth/*` budget depends on them. It then records a `paced` annotation (`${n} /auth/* requests: waited ${ms} ms`) on the test, and waits `n * authEveryMs + 500` ms. The wait uses `setTimeout` in Node, so no page is needed. `authEveryMs = 6_000` is a named constant, and its comment points to `authRate` in `internal/web/ratelimit.go` and to docs/08. The comment above the helper says that it paces before any request and never answers a 429.
   - In the comment above `stateFile`, write down the run's `/auth/*` budget: the table under Settled and the one rule. `session.setup.ts:4-5`'s "four leave two over" stays true.
   - `signIn` does not change.
3. `web/client/e2e/apply.spec.ts`: add `await paceAuth(2)` as the first line of the step "sign in, and land on the form" (`:33`). The header comment (`:12-13`) says why.
4. `web/client/e2e/sign-in.spec.ts`: add `await paceAuth(1)` as the first line of the test, before `page.route`. In the comment at `:10-11`, "which the setup project and the smoke spec already draw on" becomes "which the setup project, apply.spec.ts and phase2-exit.spec.ts also draw on". The comment also says why the test paces.
5. These files do not change: `internal/web/ratelimit.go`, `cmd/ketju/e2e.go`, `cmd/ketju/serve.go`, `web/client/playwright.config.ts`, `phase2-exit.spec.ts` and docs/08. The config's header describes the suite, and the suite still covers sign-in.
6. Add a note on TKT-01M29CGBY1 that says how its third criterion is met now, as listed under Settled, and names this ticket.
7. There is no `web/client/src` change, so the committed dist and the bundle budget are untouched.

### Tests
- Before the change: on a machine where the failure reproduces, run the full suite (`npx playwright test` from `web/client`) five times on the PR's base. Each run starts a fresh server and so a full bucket. Record how many runs failed, and the desktop smoke's 429 in each failed run.
- After the change: five full runs on the branch on the same machine, all passing.
- From one passing run's `serve.log`, found at `$KETJU_E2E_DIR/serve.log` beside the seed: for every `/auth/*` request, list its time, path and status, and the bucket modelled from them, as this ticket's description does. It shows no 429 outside phase2-exit.spec.ts, and the `paced` annotations of apply.spec.ts and sign-in.spec.ts in both projects.
- **Worst case for speed:** run the `/auth/*` spenders back to back with nothing between them to refill the bucket: `npx playwright test e2e/apply.spec.ts e2e/phase2-exit.spec.ts e2e/sign-in.spec.ts e2e/smoke.spec.ts`. The setup project runs first as their dependency. Run it five times on the branch, and every run must pass. Run it once on the PR's base and record the result, which is expected to be a 429 in sign-in.spec.ts or apply.spec.ts. That run shows the pacing is what holds the budget.
- CI's Playwright step passes on the PR, with the renamed smoke test passing in both projects.

### Out of scope
- An e2e-only `/auth/*` rate, which is option (b). It lost for the reasons under Settled.
- The `/auth/*` spends that two other tickets will add. TKT-01M3QS0TMV adds `link-sign-in.spec.ts`, one `GET /auth/link` per project. TKT-01M3QVWKAG (Give signed-in members a Sign out control) adds `sign-out.spec.ts`, a sign-in and a logout per project. Both wait out a 429 through `through()`, which satisfies the rule, and each ticket adds its row to the budget in `e2e/session.ts` when it lands. `sign-out.spec.ts` sorts just before `smoke.spec.ts`. That order is harmless once smoke spends nothing.
- `signIn` itself, which stays as it is. The coordinator's note of 2026-09-30 on TKT-01M3QVWKAG puts that ticket's wait in its own sign-out spec and leaves `signIn` alone.
- Runner contention. That is TKT-01M3MTQG5A (done) and TKT-01M3RZXGBA (Bound the axe scans in the admin forms and agents Playwright tests).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T12:09:43Z

Groomed 2026-09-30 against main at 4918f90. The plan chooses option (a): smoke.spec.ts starts from alice's stored session. The Settled section says why the e2e-only rate lost, and it follows TKT-01M3QS0TMV's settled choice that the e2e server keeps production's limiter. TKT-01M29CGBY1's "The run covers sign in..." criterion stays met by phase2-exit.spec.ts's member sign-in and sign-in.spec.ts at each width.

One tension in the record for whoever picks this up. This ticket says "Not this: retrying around the limit in `signIn`". TKT-01M3QVWKAG (Give signed-in members a Sign out control) settled, in its refresh of 2026-09-30, that `signIn` itself goes through `through()`, which waits out a 429. Once this ticket lands, smoke no longer calls `signIn`, so that wait cannot hide smoke's budget. `signIn`'s callers are then the setup project and `sign-out.spec.ts`. This plan leaves `signIn` alone either way.

**agent:claude-code/phase2-groom** at 2026-09-30T12:23:08Z

Plan revised for Terva review 1579 on PR #225 (head f55e8f5, medium finding at line 104). Desktop apply's two /auth/* requests had no fixed refill floor, and the plan held only if 9.2 s of other work came first. Now every spend that does not wait out a 429 calls paceAuth(n) in e2e/session.ts first. paceAuth is an unconditional wait of n x 6 s + 0.5 s at the start of the test, before any request: apply.spec.ts waits for 2 requests and sign-in.spec.ts for 1. With it, every row of the budget table holds at any speed. This supersedes the table in the earlier plan, and the criterion that allowed no new wait of any kind. The cost is about 38 s per full run. Three alternatives lost. A stored session moves the spend into setup, which would need the same wait. A session seeded in e2e.go adds a session-issuing path. Ordering comes down to a wait kept in another file. Smoke still starts from alice's stored session (option a). A new criterion runs the /auth/* spenders back to back as the worst case for speed.

**agent:claude-code/phase2-run-s1tvpd** at 2026-09-30T13:28:22Z

Full local runs (`npx playwright test` from web/client, one fresh server and data directory each, port 8841), 2026-09-30, this machine (12 cores, load average 21-23 from other lanes throughout).

Base (origin/main fa86711), five runs, all passed: 97 passed and 11 skipped in each, 2.0-2.1 min. The desktop smoke 429 did not reproduce on the base today: the machine was loaded by other lanes, and base run 1's serve.log, modelled as below, never takes the bucket under 1.2, and smoke's desktop sign-in starts from 3.4. The failure needs a fast, idle run, which is what the pacing now removes the dependence on.

Branch (298e972), five runs, all passed: 97 passed and 11 skipped in each, 2.7, 2.7, 2.9, 2.7 and 4.0 min (the last under the heaviest load). The added ~38 s is the four paceAuth waits (2 x 12.5 s + 2 x 6.5 s). No run had a 429 on any /auth/* request.

One addition to the plan: paceAuth grows the test's timeout by its own wait (test.info().setTimeout(timeout + ms)), so the 12.5 s wait does not come out of apply.spec.ts's 30 s for its own steps and axe scans under load. It is not a retry and reads no response.

**agent:claude-code/phase2-run-s1tvpd** at 2026-09-30T13:28:23Z

/auth/* requests from branch run 1's serve.log (298e972), time from the first, path, status, and the bucket modelled as internal/web/ratelimit.go allowAt does (burst 10, one token per 6 s, capped at 10):

```
  0.0s  /auth/fake/start     302  bucket 9.0   setup alice
  0.1s  /auth/fake/callback  303  bucket 8.0
  0.3s  /auth/fake/start     302  bucket 7.0   setup carol
  0.3s  /auth/fake/callback  303  bucket 6.1
  0.5s  /auth/fake/start     302  bucket 5.1   setup mona
  0.5s  /auth/fake/callback  303  bucket 4.1
  0.7s  /auth/fake/start     302  bucket 3.1   setup ada
  0.8s  /auth/fake/callback  303  bucket 2.1
 41.8s  /auth/fake/start     302  bucket 8.0   phone apply, after paceAuth(2)
 41.9s  /auth/fake/callback  303  bucket 7.0
 47.8s  /auth/fake/start     302  bucket 7.0   phone phase2-exit
 47.9s  /auth/fake/callback  303  bucket 6.0
 52.6s  /auth/signin         302  bucket 5.8
 52.6s  /auth/fake/start     302  bucket 4.8
 52.7s  /auth/fake/callback  303  bucket 3.8
 71.0s  /auth/signin         302  bucket 5.8   phone sign-in.spec, after paceAuth(1)
124.2s  /auth/fake/start     302  bucket 9.0   desktop apply, after paceAuth(2)
124.3s  /auth/fake/callback  303  bucket 8.0
130.0s  /auth/fake/start     302  bucket 8.0   desktop phase2-exit
130.1s  /auth/fake/callback  303  bucket 7.0
134.8s  /auth/signin         302  bucket 6.8
134.8s  /auth/fake/start     302  bucket 5.8
134.9s  /auth/fake/callback  303  bucket 4.8
152.3s  /auth/signin         302  bucket 6.7   desktop sign-in.spec, after paceAuth(1)
24 /auth/* requests, 0 answered 429
```

Smoke spends none (24 = 8 + 2 x (2 + 5 + 1)). No 429 anywhere, so none outside phase2-exit.spec.ts. The same count and no 429 held in all five branch full runs.

The paced annotations, from a JSON-reporter run of apply, sign-in and smoke on the branch (12 passed): phone apply.spec.ts "2 /auth/* requests: waited 12500 ms", desktop apply.spec.ts the same, phone sign-in.spec.ts "1 /auth/* requests: waited 6500 ms", desktop sign-in.spec.ts the same.

**agent:claude-code/phase2-run-s1tvpd** at 2026-09-30T13:28:25Z

Worst case for speed: the /auth/* spenders alone, back to back (`npx playwright test e2e/apply.spec.ts e2e/phase2-exit.spec.ts e2e/sign-in.spec.ts e2e/smoke.spec.ts`, setup first as their dependency), same machine as the full runs.

Base (fa86711), one run: FAILED, 4 failed and 10 passed. sign-in.spec.ts got a 429 from /auth/signin in both projects ("Received: 429"), and smoke's sign-in timed out at 30 s in both projects after /auth/fake/start was refused. 31 /auth/* requests, 9 answered 429.

Branch (298e972), five runs, all passed, 14 passed in each, 89-95 s. Every 429 in them (4, 4, 5, 3 and 4) fell inside phase2-exit.spec.ts, between apply's callback and sign-in.spec.ts's /auth/signin, and through() waited it out as before. apply.spec.ts, sign-in.spec.ts and smoke.spec.ts saw none: any 429 would have failed apply's landing on /apply or sign-in.spec's 302 check. This is the case the pacing is for: phase2-exit leaves the bucket near 0, and paceAuth refills what the next spend needs.

**agent:claude-code/phase2-run-s1tvpd** at 2026-09-30T13:38:21Z

The runs in the three notes above were measured on base fa86711 and branch 298e972. Before the PR opened, main moved to 6d1e9ab (TKT-01M3NS1931, TKT-01M3R1WPNS and TKT-01M3QMRE47), and the unpushed branch was rebased onto it, so 6d1e9ab is the PR's base. The commits between add two specs, drafts.spec.ts and admin-suspension.spec.ts. Both start from a stored session and spend no /auth/*, so the budget table is unchanged. To check the rebase, one more run of each, same machine and port:
- full run on base 6d1e9ab: passed, 101 passed and 11 skipped, 28 /auth/* requests and no 429;
- full run on the rebased branch: passed, 101 passed and 11 skipped, 24 /auth/* requests and no 429;
- spenders alone on base 6d1e9ab: FAILED as on fa86711, with a 429 in sign-in.spec.ts and smoke's sign-in timing out, in both projects;
- spenders alone on the rebased branch: passed, 14 passed, all 5 of its 429s inside phase2-exit.spec.ts.

## Summary

Every /auth/* spend in the Playwright run now holds by itself at any speed. smoke.spec.ts starts from alice's stored session, asserts /api/v1/me answers alice, and spends no /auth/*; its header names phase2-exit.spec.ts and sign-in.spec.ts as the run's sign-in at 390 px and 1440 px (noted on TKT-01M29CGBY1). e2e/session.ts exports paceAuth(n): it asserts one worker and no parallel tests, records a paced annotation, grows the test's timeout by its wait, and waits n x 6 s + 0.5 s before any request, never reading a 429. apply.spec.ts calls paceAuth(2) and sign-in.spec.ts paceAuth(1). The comment above stateFile holds the budget table and the rule for a new spender. A full run spends 24 requests instead of 28 and takes about 38 s longer. The limiter, the e2e server, playwright.config.ts and signIn are unchanged. Locally, five full runs and five spenders-only runs passed on the branch; the spenders alone fail on the base with 429s in sign-in.spec.ts and smoke. The CI criterion is ticked once CI passes on the PR.
