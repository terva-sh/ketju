---
schema: 3
id: TKT-01M39VNEC1YB0P357W0MCWV1HA
title: Stop returning a thread page the cache refused to its caller
type: bug
status: done
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
references:
  - ref: pr:125
    path: null
claim: null
archive: null
created_at: 2026-09-24T14:03:44Z
updated_at: 2026-09-28T18:40:07Z
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

- [x] A load overtaken by a newer applied refusal rejects with that refusal rather than resolving with its page
- [x] A load rejected for a shorter log length never resolves with that page
- [x] Tests settle overlapping loads by hand in every order of success and refusal

## Implementation plan

### Callers today (read 2026-09-28 at 55e876a)
- `loadThreadPage` still has one caller, ThreadScreen, through `useResource`. It aborts a superseded request before starting the next one, and a retry does the same.
- ReplyExpander (PR #96) and PostPopover `apiGet` `/threads/{id}/posts/...` themselves. They never call `loadThreadPage` or store into the cache. Their held bodies fall under `isThreadUrl`, so a refusal forgets them.
- Read tracking (PR #101) calls only `forgetLandings`, which drops held landing bodies. It loads nothing. No `forgetHeldPages` function exists.
- The keyboard shortcuts (PR #98) only `route()`, and that remounts ThreadScreen's resource with the abort.
- So nothing loads a second page of one thread concurrently yet. The fix is still needed before TKT-01M29CTYA0 (live updates) adds a second caller.

### Approach (web/client/src/state/threads.ts)
1. Keep the applied refusal's error beside its load number (`refused: Map<id, {n, err}>`).
2. A successful load older than the applied refusal drops its held body (unchanged). Then:
   - while the refusal still stands (no load newer than it has stored a page since), it rejects with that refusal, a ProblemError, so `useResource` clears the data and shows the access error;
   - if a newer load has stored a page since, the refusal is superseded and the page is only stale: take step 3.
3. A page that `storeThreadPage` refuses for its shorter length, or a stale page as in step 2, never resolves as itself:
   - it resolves with the entry's current page under the same `pageKey` when one is cached (same page and lens, at the longer length);
   - otherwise it refetches the same URL once, as a new load, with the same signal;
   - if the refetch is refused again, it rejects with `ProblemError('conflict')`, which the screen shows with a retry.
4. An aborted load still resolves with its page and changes nothing (unchanged; `useResource` ignores it).

### Tests (threads.test.ts)
Two loads, older A and newer B, settled by hand. Table-driven over every pair of outcomes: success at 100, success at 120, 403. Each pair runs in both settle orders, for different pages and for the same page. Each row states what each promise gives and what the cache holds afterwards. There are also separate cases for:
- a superseded refusal;
- the refetch being refused again;
- a refusal during the refetch.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:33:29Z

### Decisions taken while building (2026-09-28)
- **Shorter length:** of the options the ticket gives, I took "the cached page under the same key, else refetch once, else a retryable error".
  - The cached page is the same page and lens at the longer length, so it is the right thing to show.
  - A refetch is invisible to the reader. An immediate error would have shown "Something changed..." for a harmless race.
  - The refetch is bounded to one asking. A second refusal rejects with `ProblemError('conflict', 0)`, which ErrorPanel shows with a retry.
  - This settles only internals: what the reader sees is the right page or an existing error message.
- **Superseded refusal:** an older page landing after a refusal rejects with that refusal only while it still stands. Once a request started after the refusal has stored a page, the refusal no longer stands, and the older page is handled like a shorter-length one, not reported as a refusal that has already lifted. The ticket did not name this case. I settled it because the alternative would show an access error beside a page the viewer can see.
- **Extra fix in `storeThreadPage`:** the ordering table found a held-body gap in the one-page ordering "A 120, B 100, B lands first". The newer request's shorter body stayed held under the URL after the older answer moved the cache to 120, because the length-move forget exempted the stored URL and `apiGet` holds only the newest request's answer. That URL is now kept only when it holds this very page. This is the same class as reviews 412 and 417.

### Callers (checked at 55e876a)
`loadThreadPage` has one caller, ThreadScreen, which aborts before it reloads. The other code the task named does not load thread pages:
- ReplyExpander (PR #96) and PostPopover `apiGet` their own `/posts/...` URLs;
- readTracker (PR #101) only calls `forgetLandings`, and no `forgetHeldPages` exists;
- the shortcuts (PR #98) only route.

So no concurrent second loader exists yet.

### Residual, for TKT-01M29CTYA0 (live updates)
Once a second caller exists, there is a gap this ticket's criteria do not cover. A refusal applied by caller X forgets the cache, but caller Y's `useResource` still holds the page it resolved earlier in component state. The live-update client should re-read the cache, or observe refusals, when it lands.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:40:07Z

### CI run 728 on head 3fb6537 (base 55e876a): one e2e timeout outside this change
- **What failed:** only `[phone] e2e/a11y.spec.ts:69 the forum index passes the scan in both themes`. The axe `analyze()` call passed its 30 s test timeout. It was the first test after session setup. The other 52 tests passed, including the phone thread-page scan and the desktop forum-index scan. Every step before Playwright was green: the built client is current, the budget holds, and the client tests pass.
- **Why I judge it unrelated:**
  - the forum index does not call `loadThreadPage` or read the thread cache;
  - the same suite passed locally on port 8794 (53 passed, 9 skipped);
  - run 724, the other lane's PR #124, was running on the runner at the same time, so the cold first scan was competing for CPU.
- **Terva review:** clean on 3fb6537 (issuecomment-17204).
- **Next step:** re-trigger CI with this `.tickets/`-only commit and carry the review. If the same test fails again, it is not load and I will look into it.

## Summary

loadThreadPage now resolves only with a page the cache holds. A load overtaken by a refusal that still stands rejects with that ProblemError. A page refused for its shorter length, or from before a superseded refusal, resolves with the cached page under the same key, or else is asked for once more, and a second refusal rejects with a retryable conflict. The ordering table found a held-body gap in storeThreadPage, fixed in the same change. threads.test.ts settles two loads by hand for every pair of {100, 120, 403} in both orders, on two pages and on one; 8 mutations tried, all caught.
