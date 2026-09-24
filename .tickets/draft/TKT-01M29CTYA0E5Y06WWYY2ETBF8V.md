---
schema: 3
id: TKT-01M29CTYA0E5Y06WWYY2ETBF8V
title: Add the client SSE subscription with the new-posts pill
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:realtime
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies:
  - TKT-01M29CRJ3XXH2CHG4GKC4CGXAS
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:live-updates
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:28:54Z
updated_at: 2026-09-24T12:53:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

While a thread page is open the client holds an SSE subscription. On a hint it fetches the new posts and shows a "3 new posts, show" pill at the bottom rather than inserting them unasked, because inserting them moves the page under the reader. If the reader is already at the very bottom, posts stream in directly.

Polling every 30 seconds is the fallback when SSE is unavailable, which covers proxies that buffer streams.

## Acceptance criteria

- [ ] New posts never shift content under a reader who is not at the bottom
- [ ] A reader at the bottom receives posts inline with no pill
- [ ] Polling takes over when the stream fails and stops when it recovers
- [ ] The subscription closes on navigation away and does not leak connections

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T12:53:34Z

From TKT-01M29CEE5H (fetch layer). `state/threads.ts`'s loadThreadPage refuses nothing at runtime, but it is only for full pages: storing an after_log_length answer through it would replace a page with a fragment, because a new log_length replaces the whole entry. Add a merge that applies the fragment to the held page (threadCache entry, pages keyed by pageKey) and advances logLength, and route SSE refetches through it. A delete/restore moves log_length with no post to account for it (docs/07); refetch the full page through loadThreadPage then.
