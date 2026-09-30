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
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T11:40:10Z
updated_at: 2026-09-30T11:43:05Z
created_by:
  id: agent:claude-code/phase2-run-mtqg5a
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-mtqg5a
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
