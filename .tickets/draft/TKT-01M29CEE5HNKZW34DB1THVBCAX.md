---
schema: 3
id: TKT-01M29CEE5HNKZW34DB1THVBCAX
title: Build the fetch layer and signals state
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCC02K8F14P0SKAYFMSE6
  - TKT-01M29CA99RTPEA1B3HG24B2KNJ
blocks_on: none
references:
  - ref: design:preact-client
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:22:04Z
updated_at: 2026-09-24T05:21:04Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

A small SWR-style fetch wrapper that honors ETags, plus `@preact/signals` for the viewer, notifications, and per-thread caches keyed by log length.

Keying the thread cache on log length rather than on time is what makes it correct for free: a cached page is valid exactly as long as the thread has not changed, and the server's own cache uses the same key.

Problem-details errors need one handling path here, mapping a stable type code to a message rather than each screen inventing its own.

## Acceptance criteria

- [ ] Conditional requests send If-None-Match and a 304 serves the cached body
- [ ] Per-thread client caches are keyed on log length and a new length replaces rather than merges
- [ ] Problem-details responses map to messages in one place
- [ ] A failed request has a retry path that does not leave a screen in a permanent spinner

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `src/api/client.ts`: `apiGet<T>` keeps a module-level `Map<url, {etag, body}>`, sends `If-None-Match`, and on 304 returns the stored body. `apiSend<T>` sends JSON with `Content-Type: application/json` (the only type the API accepts for mutations, docs/07 CSRF rule 1). Both parse `application/problem+json` into `ProblemError {type, status, title, retryAfter}`.
2. `src/api/problem.ts`: `problemMessage(type)` is the one table mapping each docs/07 catalogue code (`unauthenticated`, `forbidden`, `not-found`, `gone`, `conflict`, `validation-failed`, `rate-limited`, `unavailable`, and the rest) to user-facing text. For `unauthenticated` it offers sign-in; for `forbidden` it does not. `rate-limited` uses `Retry-After`. An unknown type falls back to `internal`.
3. `useResource<T>(key, fetcher)` returns `{data, error, loading, retry}`. It returns cached data at once and revalidates. On error it sets `loading=false` and exposes `retry`, and `<ErrorPanel>` renders the message and a Retry button. No screen shows a spinner without a terminal state.
4. `state/viewer.ts`: the `viewer` signal is seeded from `#bootstrap` JSON; `refreshViewer()` refetches `/me`.
5. `state/threads.ts`: `threadCache: Map<threadId, {logLength, pages: Map<pageKey, ThreadPage>}>`. `loadThreadPage(id, page, lens)` stores the response. If `thread.log_length` differs from the cached `logLength`, it replaces the whole `pages` map with only the new page rather than merging. The server keys its cache and ETag on the same log length (docs/07).
6. `src/api/dto.ts`: hand-written TS types mirroring `internal/dto`. `src/api/dto-contract.ts` imports each golden under `internal/dto/testdata/golden/*.json` (`resolveJsonModule`) and assigns it to the matching type, so `tsc --noEmit` fails when a golden gains a field the types lack or loses one they require.

### Files
`web/client/src/api/{client,problem,dto,dto-contract}.ts`, `src/state/{viewer,threads}.ts`, `src/components/ErrorPanel.tsx`.

### Tests (Vitest with a stubbed `fetch`)
- AC1: the first GET stores the ETag; the second sends `If-None-Match`; a stubbed 304 resolves to the first body.
- AC2: load page 1 at log_length 100, then page 2 at log_length 120. Assert that page 1 is gone from the cache and `logLength` is 120.
- AC3: one test per catalogue code asserts `problemMessage` returns non-empty text. A lint-style test asserts that no file under `src/screens` or `src/components` imports anything but `problemMessage` for error text; implement it as a grep for `.type ===`.
- AC4: a stubbed network error followed by a 200 on `retry()` asserts that `loading` goes false after the error and `data` is set after the retry.
- The contract: `npm run typecheck` in CI covers `dto-contract.ts`.

### Design
docs/07 "Preact client" (State), "Error catalogue", "Thread response shape", "Where the shapes live"; `internal/dto/dto.go` and its goldens; `internal/problem` (catalogue golden).

### Interactions
Depends on CCC02 and CA99R (`/me`). Every screen ticket calls `useResource`. The later SSE client (CTYA0) calls `loadThreadPage` with `after_log_length`.
Open in docs: whether the client should honor `Retry-After` automatically or only display it.
