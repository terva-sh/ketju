---
schema: 3
id: TKT-01M3XP6ZRZQ968B8QPZ1RJBEGM
title: Show a thread as read on Unread after going back from reading it
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN36D34YVKVRPNQD3PHDV
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T06:53:16Z
updated_at: 2026-10-02T06:53:16Z
created_by:
  id: agent:claude-code/phase3-exit-check
  name: ""
updated_by:
  id: agent:claude-code/phase3-exit-check
  name: ""
extensions: {}
---

## Description

Found by the Phase 3 exit spec (TKT-01M3V3CFRE, `e2e/phase3-exit.spec.ts`, the Unread surface test) at 1440 px, in the full suite run. The test passed twice when run alone, which fits a race.

### What happens
1. alice opens /unread and sees carol's thread at "2 new". She presses the badge, which lands on #2.
2. #2 and #3 are both on screen past the one-second dwell.
3. She presses Back to /unread.
4. The stored position becomes 3, so the server has it. The list on screen still shows the thread with "2 new", and it stays that way: nothing asks for the list again.

Snapshot from the failure:

    link "Unread surface desktop 1790923939446" … link "2 new"

### Why
Leaving the thread page flushes the read beacon (`state/readTracker.ts`, a keepalive PUT). The list screen revalidates its held answer (`api/client.ts` conditionalGet) at the same moment. When the list GET reaches the server before the PUT, the answer is the old one. Nothing drops or refetches the held list once the PUT answers: `forgetLists` runs after a mark all read and a mark-unread, but not after a read beacon lands.

### Expected
A member who reads a thread and goes back to Unread sees it gone, or at least its badge cleared, without reloading. That is docs/14's "daily-driver quality". One fix is to have the list wait for an in-flight flush before it revalidates. Another is to forget the held lists, and refetch the one on screen, when a beacon answers with a higher position.

### Owners
TKT-01M29CTY7K (Build the cross-forum unread, new, participated, and watching lists) and the read tracker (`state/readTracker.ts`).

### Why phase-3
It fails Phase 3's exit criterion 1 (TKT-01M3V3CFRE). The exit spec is not changed to avoid it: no reload is added and no wait on the beacon.
