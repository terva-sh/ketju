---
schema: 3
id: TKT-01M29CEE5HNKZW34DB1THVBCAX
title: Build the fetch layer and signals state
type: task
status: done
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
updated_at: 2026-09-24T12:53:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

A small SWR-style fetch wrapper that honors ETags, plus `@preact/signals` for the viewer, notifications, and per-thread caches keyed by log length.

Keying the thread cache on log length rather than on time is what makes it correct for free: a cached page is valid exactly as long as the thread has not changed, and the server's own cache uses the same key.

Problem-details errors need one handling path here, mapping a stable type code to a message rather than each screen inventing its own.

## Acceptance criteria

- [x] Conditional requests send If-None-Match and a 304 serves the cached body
- [x] Per-thread client caches are keyed on log length and a new length replaces rather than merges
- [x] Problem-details responses map to messages in one place
- [x] A failed request has a retry path that does not leave a screen in a permanent spinner

## Implementation plan

Claim-time plan (2026-09-24). It revises the pre-groom plan after reading the client as it stands at fc7a709. The pre-groom plan had no "Settled" section; its open item is decided in "Decided at claim" below.

### Approach
1. `src/api/problem.ts`
   - `ProblemError {type, status, title, retryAfter}`, and `toProblem(response)`: a problem+json body gives its `type`; any other body maps from the status through the catalogue (404 → not-found, 503 → unavailable, anything else → internal). A thrown fetch becomes the client-only code `network`.
   - `problemMessage(p)` is the one table from code to `{text, signIn}`. It is typed as a `Record` over the code union, so a missing code does not compile.
   - `isProblem(err, type)` is how a screen asks, instead of comparing `.type` itself.
2. `src/api/client.ts`
   - `apiGet<T>(url, {signal})` keeps a bounded LRU (100 entries) of `{etag, body}` per URL. It sends `If-None-Match` when it holds a tag, and a 304 answers with the held body. A 304 with nothing held is an error, never an empty success.
   - `peekGet(url)` returns the held body.
   - `apiSend<T>(method, url, body)` sends JSON with `Content-Type: application/json`.
   - Errors from both are ProblemErrors.
   - `resetApiCache()` is for tests.
3. `src/api/resource.ts`: `useResource<T>(key, fetcher, peek?)` returns `{data, error, loading, retry}`.
   - The answer is keyed on `key`, so a new key never shows another key's answer, not even for one render.
   - It shows `peek()` at once while it revalidates.
   - A superseded request is aborted and ignored.
   - An error ends loading and keeps any data this key already had.
   - `retry()` re-runs the request.
4. `src/components/ErrorPanel.tsx` renders `problemMessage`, the Retry-After wait when there is one, and a Retry button. For `unauthenticated` it adds a sign-in prompt, and for `forbidden` it does not.
5. `src/state/viewer.ts`: the `viewer` signal seeded from `#bootstrap` (moved from `src/state.ts`), a `notifications` computed from it, and `refreshViewer()`, which fetches /me.
6. `src/state/threads.ts`
   - `threadCache`: a signal holding `id → {logLength, header, pages}`.
   - `loadThreadPage(id, query, signal)` stores the response. A `log_length` different from the entry's replaces the whole entry with this one page, never merging it.
   - A `not-found` or `gone` drops the entry.
   - `cachedThread(id)` returns the entry.
   - The page key is the page the response names (number and lens), not the request, because a request without a page lands where the server chooses.
7. `src/screens/ThreadScreen.tsx` stops calling fetch and uses `useResource` over `loadThreadPage`. The guarantees from TKT-01M29CEE52 hold:
   - an id-keyed answer;
   - no stale title or 404 for even one render;
   - a later failure replaces a 404;
   - a late superseded failure is ignored;
   - the title is kept through a failure, now from `cachedThread`;
   - the canonical-slug redirect.
   Failure renders `ErrorPanel` with Retry.
8. `src/api/dto.ts` holds hand-written types that mirror `internal/dto`. `src/api/dto-contract.ts` imports every golden (`resolveJsonModule`) and checks it against its type both ways: the golden must be assignable to the type (a missing or retyped field fails), and a deep no-extra-keys check makes a field the types lack fail too. Plain assignment would not catch that, because TypeScript checks excess properties only on object literals. `tsc --noEmit` runs in `npm run build`, which CI runs.

### Decided at claim
- **Retry-After:** display it; do not retry automatically. The Retry button stays enabled.
  - An automatic retry on 429 turns the client into a load generator against a limiter that exists to shed load, and it has to be cancelled when the screen changes. Showing the wait and leaving the retry to the reader cannot do either.
  - This choice is reversible and client-only.

### Tests (Vitest, stubbed fetch)
- AC1: the first GET stores the ETag, the second sends If-None-Match, and a 304 resolves to the first body. A 304 with nothing held is an error. The LRU evicts.
- AC2: page 1 at log_length 100, then page 2 at 120: page 1 is gone and logLength is 120. At the same length, pages accumulate. A 404 drops the entry.
- AC3:
  - Every code in `internal/problem/testdata/catalogue.json` has non-empty text, and the table has no code the catalogue lacks, except `network`.
  - A status-only error maps through the table.
  - `unauthenticated` offers sign-in and `forbidden` does not.
  - A source scan: no file under `src/screens` or `src/components` compares `.type` or `.status`, or calls `fetch(`, and no file outside `src/api` calls `fetch(`.
- AC4: `useResource` with a network error, then a 200 on `retry()`: `loading` is false after the error and `data` is set after the retry. ThreadScreen's Retry button recovers the screen. The existing view tests stay green, moved to full-shaped bodies.
- The contract: `tsc --noEmit` over `dto-contract.ts`. A Vitest test checks that every golden file is imported there, and a deliberately wrong type is proven to fail compilation, via a throwaway tsc run during development, recorded in a note.

### Files
- `web/client/src/api/{client,problem,resource,dto,dto-contract}.ts`
- `src/state/{viewer,threads}.ts`
- `src/components/ErrorPanel.tsx`
- `src/screens/ThreadScreen.tsx` and `src/screens/Placeholder.tsx` (the import moves)
- `src/testing.tsx` (reset the caches)
- `tsconfig.json` (`resolveJsonModule`)
- the rebuilt `dist`

### Design
docs/07 "Preact client" (State), "Error catalogue", "Thread response shape", "Where the shapes live".

### Interactions
- Carried from the pre-groom plan: this depends on CCC02 and CA99R (/me). Every screen ticket calls `useResource`.
- The later SSE client, CTYA0, calls the thread endpoint with `after_log_length`. That answer is partial: only the new posts and edits. Storing it through `loadThreadPage` would replace a whole page with a fragment, so `loadThreadPage` refuses an `after_log_length` query. CTYA0 adds the merge that applies a fragment to the held page and advances `logLength`.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:53:34Z

### What was built

**`src/api/problem.ts`: the one table from code to words.**
- It is typed as a `Record` over the code union, so leaving out a code fails to compile.
- A body that is not problem+json maps from its status to the code the server would have sent.
- A type the client does not know is not believed.
- `network` is the client's own code, for a request that never reached the server.

**`src/api/client.ts`: `apiGet` and `apiSend`.**
- `apiGet` keeps an LRU of `{etag, body}` per URL, 100 entries, and a 304 counts as a use.
- It sends If-None-Match when it holds a tag, and serves the held body on a 304.
- A 304 with nothing held is an error.
- A 200 without a tag drops what was held.
- `apiSend` always sends JSON.

**`src/api/resource.ts`: `useResource`.**
- Answers are keyed.
- It shows the held copy at once.
- A superseded request is aborted and ignored, whether it succeeds or fails late.
- An error keeps any data the key already had.
- `retry` re-runs the request.

**`src/state/threads.ts`: the thread cache.**
- A signal of `id → {logLength, header, pages}`.
- A new log length replaces the entry; it is never merged.
- A 404, 410, 403 or 401 drops the thread.
- Pages are filed under the page the server answered with, not the one requested.
- `loadThreadPage` takes no `after_log_length`; merging a fragment is TKT-01M29CTYA0's job.

**`src/state/viewer.ts`**: `viewer`, `notifications` (computed from the viewer), and `refreshViewer`.

**`src/components/ErrorPanel.tsx`**: shows the table's words, any Retry-After, a sign-in prompt for `unauthenticated` only, and Retry.

**`src/api/dto.ts` and `dto-contract.ts`**: TypeScript types mirroring internal/dto, checked against all 19 goldens in both directions by `tsc`.

**ThreadScreen** now goes through `useResource` and `loadThreadPage`. All the TKT-01M29CEE52 guarantees still hold; its tests now use full-shaped bodies built from the member golden.

### Decided at claim
Retry-After is displayed and never acted on automatically. The rationale is in the plan and in the ErrorPanel comment.

### Found while testing
The first `useResource` ignored every AbortError. An abort it did not cause itself would have left `loading` true forever, which is exactly the permanent spinner AC4 forbids. Now only the hook's own abort is silent, and a test pins this.

### Gap, not blocking
The sign-in offer is a prompt, not a link. The client has no provider list to link to `/auth/{provider}/start`, and no ticket yet covers a sign-in screen. Whoever builds one should render it where ErrorPanel marks `data-sign-in`.

### Verification
- **The contract fails for real drift.** Eight deliberate drifts of dto.ts, run through `tsc` during development, were all caught:
  - an extra key nested in Author;
  - an extra key in a recursive Forum;
  - an extra key in array elements (revisions, attachments);
  - an extra top-level key;
  - a field the golden lacks;
  - a retyped field;
  - a nullable narrowed to null.
- **Mutation:** 39 mutants over client, problem, resource, threads, ThreadScreen, ErrorPanel and viewer were all killed. The one that survived at first, a late success from a superseded request, got its own test.
- **Suite and build:** Vitest has 261 tests. `tsc --noEmit` is clean. The bundle is 37.5 kB raw, 14.4 kB gzipped, and holds no golden. The rebuilt dist is reproducible, and the Go web and spa tests pass.

## Summary

Landed the client fetch layer: problem.ts (one code-to-words table, typed exhaustive over the catalogue, status fallback, client-only network code), client.ts (apiGet with a 100-entry LRU of ETag/body, If-None-Match, 304 served from the held body; apiSend JSON), resource.ts (useResource: keyed answers, held copy shown at once, superseded requests aborted and ignored, retry), state/threads.ts (thread cache keyed on log_length, replaced on a new length, dropped on 404/410/403/401), state/viewer.ts (viewer, notifications, refreshViewer), ErrorPanel, and dto.ts + dto-contract.ts holding hand-written types to all 19 goldens both ways under tsc. ThreadScreen uses it. Retry-After is shown, not acted on. 39 mutants killed; 261 Vitest tests.
