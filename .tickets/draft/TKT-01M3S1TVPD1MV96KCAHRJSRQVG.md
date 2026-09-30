---
schema: 3
id: TKT-01M3S1TVPD1MV96KCAHRJSRQVG
title: Stop the full Playwright run spending more /auth calls than it refills
type: bug
status: draft
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
claim: null
archive: null
created_at: 2026-09-30T11:40:10Z
updated_at: 2026-09-30T12:09:43Z
created_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] web/client/e2e/smoke.spec.ts sets test.use({ storageState: stateFile("alice") }), calls neither signIn nor any /auth/* URL, and asserts that GET /api/v1/me answers alice before it reads the thread
- [ ] smoke.spec.ts's read, reply and unread-divider steps keep every assertion they had, in both projects
- [ ] smoke.spec.ts's header comment names phase2-exit.spec.ts's member sign-in and sign-in.spec.ts as where the run covers sign-in at 390 px and 1440 px, and a note on TKT-01M29CGBY1 says the same
- [ ] e2e/session.ts states the run's /auth/* budget (setup 8; per project apply 2, phase2-exit 5, sign-in.spec 1; 24 in all), and sign-in.spec.ts no longer says the smoke spec draws on the burst
- [ ] The diff leaves internal/web/ratelimit.go, cmd/ketju/e2e.go, cmd/ketju/serve.go and web/client/playwright.config.ts unchanged, and adds no wait or retry on a 429 to any spec
- [ ] A note records five full local runs of npx playwright test on the PR's base and five on the branch, on the same machine, with each run's result, and all five on the branch pass
- [ ] A note lists one passing run's /auth/* requests from serve.log with time, path, status and the modelled bucket, and shows no 429 outside phase2-exit.spec.ts
- [ ] CI's Playwright step passes on the PR, with the smoke test passing in both projects

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
- **The run's arithmetic after the change** (24 requests). The table lists each spend that does not wait out a 429:

  | spend | cost | what makes it hold |
  |---|---|---|
  | setup | 8 | A full bucket of 10. It leaves 2. |
  | phone apply | 2 | The 2 that setup left. Holds at any speed. |
  | sign-in.spec, each project | 1 | It follows phase2-exit, which can leave the bucket empty. Between them, read-tracking.spec.ts sleeps a fixed 7.15 s (`:75`, `:80`, `:104`, `:122`, `:132`, `:136`), and on the phone shortcuts.spec.ts sleeps another 1.7 s (`:130`). 7.15 s refills 1.19 tokens, so this holds at any speed. |
  | desktop apply | 2 | This spend has no fixed floor. After the phone's sign-in.spec, the bucket holds at least 0.47, so 9.2 s of work must come between them: the phone's smoke, targets and thread-list, then desktop a11y (10 tests) and the four admin specs. The failing run took 29.3 s over that stretch (48.1 s to 77.4 s). |

  This holds while Playwright runs a project's files in today's alphabetical order, one worker, one file at a time (`playwright.config.ts`). The verification below records the timeline.

### Approach
1. `web/client/e2e/smoke.spec.ts`:
   - Add `test.use({ storageState: stateFile('alice') })` at file level, and drop `signIn` from the import of `./session`.
   - Replace the step `'sign in'` (`:18`) with a step that asserts `GET /api/v1/me` answers `user.handle === 'alice'`. A stale state file then fails there with a clear message, not at the composer.
   - Rename the test from `'sign in, read a thread, reply, and see the unread divider'` to `'read a thread, reply, and see the unread divider'`, because it no longer signs in. No CI step or doc names the old title (grep of ci.yml and docs/, 2026-09-30).
   - Rewrite the header comment (`:6-8`) to say three things: what the spec covers; why it starts from a stored session, which is the `/auth/*` budget; and where the run covers sign-in at each width, as listed under Settled.
   - carol's context comment (`:51-52`) says "the sign-in flow is alice's step above", which is no longer true. Point it at phase2-exit.spec.ts and sign-in.spec.ts.
   - The read, reply and unread-divider steps and their assertions stay as they are.
2. `web/client/e2e/sign-in.spec.ts:10-11`: "which the setup project and the smoke spec already draw on" becomes "which the setup project, apply.spec.ts and phase2-exit.spec.ts already draw on".
3. `web/client/e2e/session.ts`: the comment above `stateFile` gets the run's `/auth/*` budget. Setup spends 8. Each project spends 2 in apply.spec.ts, 5 in phase2-exit.spec.ts, which waits out a 429 with `through()`, and 1 in sign-in.spec.ts. That is 24 in all, against a burst of 10 and one token every 6 s. It also states the rule. A spec that spends `/auth/*` without waiting out a 429 must sit where the refill before it covers the spend. A spec that waits leaves the bucket empty for whatever follows it. `session.setup.ts:4-5`'s "four leave two over" stays true.
4. These files do not change: `internal/web/ratelimit.go`, `cmd/ketju/e2e.go`, `cmd/ketju/serve.go`, `web/client/playwright.config.ts`, `phase2-exit.spec.ts`, `apply.spec.ts` and docs/08. The config's header describes the suite, and the suite still covers sign-in.
5. Add a note on TKT-01M29CGBY1 that says how its third criterion is met now, as listed under Settled, and names this ticket.
6. There is no `web/client/src` change, so the committed dist and the bundle budget are untouched.

### Tests
- Before the change: on a machine where the failure reproduces, run the full suite (`npx playwright test` from `web/client`) five times on the PR's base. Each run starts a fresh server and so a full bucket. Record how many runs failed, and the desktop smoke's 429 in each failed run.
- After the change: five full runs on the branch on the same machine, all passing.
- From one passing run's `serve.log`, found at `$KETJU_E2E_DIR/serve.log` beside the seed: for every `/auth/*` request, list its time, path and status, and the bucket modelled from them, as this ticket's description does. It shows no 429 outside phase2-exit.spec.ts, and the bucket just before desktop apply.
- CI's Playwright step passes on the PR, with the renamed smoke test passing in both projects.

### Out of scope
- An e2e-only `/auth/*` rate, which is option (b). It lost for the reasons under Settled.
- The `/auth/*` spends that two other tickets will add. TKT-01M3QS0TMV adds `link-sign-in.spec.ts`, one `GET /auth/link` per project. TKT-01M3QVWKAG (Give signed-in members a Sign out control) adds `sign-out.spec.ts`, a sign-in and a logout per project. Both wait out a 429 through `through()`, and each ticket updates the budget in `e2e/session.ts` when it lands. `sign-out.spec.ts` sorts just before `smoke.spec.ts`. That order is harmless once smoke spends nothing.
- `signIn` itself. TKT-01M3QVWKAG's plan changes it to go through `through()`. After this ticket, `signIn`'s only callers are the setup project and that ticket's spec.
- Runner contention. That is TKT-01M3MTQG5A (done) and TKT-01M3RZXGBA (Bound the axe scans in the admin forms and agents Playwright tests).
- Making desktop apply's spend hold at any speed. It had a margin of about three times in the failing run. If it ever fails, file it with the run's timeline.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T12:09:43Z

Groomed 2026-09-30 against main at 4918f90. The plan chooses option (a): smoke.spec.ts starts from alice's stored session. The Settled section says why the e2e-only rate lost, and it follows TKT-01M3QS0TMV's settled choice that the e2e server keeps production's limiter. TKT-01M29CGBY1's "The run covers sign in..." criterion stays met by phase2-exit.spec.ts's member sign-in and sign-in.spec.ts at each width.

One tension in the record for whoever picks this up. This ticket says "Not this: retrying around the limit in `signIn`". TKT-01M3QVWKAG (Give signed-in members a Sign out control) settled, in its refresh of 2026-09-30, that `signIn` itself goes through `through()`, which waits out a 429. Once this ticket lands, smoke no longer calls `signIn`, so that wait cannot hide smoke's budget. `signIn`'s callers are then the setup project and `sign-out.spec.ts`. This plan leaves `signIn` alone either way.
