---
schema: 3
id: TKT-01M3SAKPK9Y66WMGGMC6CAN3BR
title: Guard a reply post answered after the viewer changed
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T14:13:32Z
updated_at: 2026-09-30T14:13:32Z
created_by:
  id: agent:claude-code/phase2-run-ns16vh
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ns16vh
  name: ""
extensions: {}
---

## Description

Terva review 1591 on PR #235 (head 0a72954) found this in the new-thread composer, and TKT-01M3NS16VH fixed it there. The reply composer has the same shape.

`postReply` in `web/client/src/state/composer.ts` awaits the POST and then calls `drop({kind: 'reply', thread})` by key. Suppose the viewer changes in the same tab while the POST is out, and the next viewer opens the same thread. `composerFor`'s owner check resets the draft keys, and the next viewer's load creates a new entry under that key. The old answer then drops the new entry, which queues a DELETE of the next viewer's reply draft under their session.

Signing in and out loads a page today, which empties the module state, so this only happens if that changes. The fix is the one `createThread` now has (`state/newThread.ts`, `current`): capture the owner before the await, and after it drop nothing and answer null when the owner or the composer is no longer the one that posted.
