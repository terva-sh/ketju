---
schema: 3
id: TKT-01M3KFFFJRNQD0F5N6XN966V8T
title: Add the mark-unread control to the thread page
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T07:43:10Z
updated_at: 2026-09-28T15:01:09Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The thread page has no way to mark a thread, or the posts after a given post, unread. The server side landed with TKT-01M29CA9CE (PR #99). The client's read-tracking beacon landed with TKT-01M29CGBWN (PR #101) and does not include this control. An implementation of the client-side ordering, `markUnread` in `web/client/src/state/readTracker.ts`, was written on PR #101 and removed before merge because no criterion there covered it. It is at commit dd761e95ee631b5e07583cbd230e3eedbef3798d on the `run/cgbwn-read-beacon` branch, with its tests in `readTracker.test.ts`, and can be reused.

### Server contract (docs/07 "Read position")

- `PUT /api/v1/threads/{id}/read {ordinal}` moves the position forward only. The server keeps the larger of the stored position and the one sent.
- `DELETE /api/v1/threads/{id}/read`, with an optional `{ordinal}` body, is the only way the position goes down. With no body the whole thread is unread. It never raises the position. Both answer `{last_read_ordinal}`, the position as stored.
- A PUT sent before a DELETE and arriving after it moves the position forward again, because the server cannot tell it from a new read.

### Requirements on the control

- Flush the beacon's pending position and wait for every PUT in flight for the thread before sending the DELETE. Send no beacon for the thread until the DELETE has answered.
- After the DELETE answers, lower the beacon's floor for the thread to the answer. Nothing in `readTracker.ts` lowers it today. Held page bodies of the thread report the old position, so forget them. A page requested before the DELETE answered must not raise the floor again. A page requested after it must.
- Two marks of one thread must take effect in call order, because two DELETEs in flight can land in either order.
- Posts read while a DELETE is pending must be kept and judged against the DELETE's answer, not the old floor. Review 1149 on PR #101 found a medium defect in the removed implementation: when the DELETE fails, the reads kept while it was pending were checked against the unchanged, higher floor and dropped. A retried mark that then succeeded never sent them, and their dwell timers had already fired. Reads made while a DELETE is pending must survive a failed DELETE, so that a retried mark still sends them.

### Design

docs/06 "Read tracking", docs/07 "Read position".

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:01:09Z

This note supersedes the pointer to dd761e9 for the removed markUnread implementation. PR #101 was rebase-merged, which rewrote that commit, so dd761e9 is not in main's history and will not stay reachable. The same code is on main at 74c4e8f ("record review 1148's fixes"): `git diff dd761e9 74c4e8f -- web/client/src` is empty. Use `git show 74c4e8f:web/client/src/state/readTracker.ts`.
