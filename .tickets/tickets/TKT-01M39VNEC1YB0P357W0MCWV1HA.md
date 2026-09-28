---
schema: 3
id: TKT-01M39VNEC1YB0P357W0MCWV1HA
title: Stop returning a thread page the cache refused to its caller
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T14:03:44Z
updated_at: 2026-09-28T18:06:55Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Review 420 on PR #63 (TKT-01M29CEE5H, Build the fetch layer and signals state) reported one medium finding, left unfixed. The PR was merged at 6cf16d8 by the decision of the person running the session: fix review 417's two findings, re-review once, then merge anyway and file a ticket for any further medium ordering edges.

### The finding
In `web/client/src/state/threads.ts`, `loadThreadPage` sometimes refuses to store a page:
- because a newer load for the thread was already refused (401, 403, 404 or 410); or
- because `storeThreadPage` rejected the page for its shorter log length.

In both cases it still resolves with that page. `useResource` treats any resolved value as current data, so a caller could show a page the cache has just refused. In the refusal case, that is content shown after access was withdrawn.

### Why it was deferred
It needs two overlapping loads of one thread from different callers. The only caller today is ThreadScreen, which aborts a superseded request before starting the next, and an aborted answer is never shown. No screen yet loads two pages of one thread at once. That changes with TKT-01M29CEE6V (the thread page) and TKT-01M29CTYA0 (live updates), so this should be settled before either lands a second caller.

### Likely fix
Make a rejected load reject rather than resolve.
- A load overtaken by a refusal throws that refusal. It is a `ProblemError`, so the screen shows the access error.
- A load rejected for its shorter length is replaced by the entry's current page when one is cached under the same key. Otherwise it throws a retryable error, or triggers a refetch.

Add tests in `threads.test.ts` that settle the loads by hand in both orders. The four earlier review rounds on this code (411 to 417) each found the next ordering case, so the tests should enumerate the orderings, not just the reported one.

## Acceptance criteria

- [ ] A load overtaken by a newer applied refusal rejects with that refusal rather than resolving with its page
- [ ] A load rejected for a shorter log length never resolves with that page
- [ ] Tests settle overlapping loads by hand in every order of success and refusal
