---
schema: 3
id: TKT-01M3YSQQ33K01M960MGMSZ90P2
title: Show archived threads and offer archiving in the client
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CRJ64G7PJBPK8Z52SE8NW
blocks_on: none
references:
  - ref: pr:361
    path: null
claim:
  actor: agent:claude-code/phase4-run-SQQ33
  branch: run/sqq33-thread-archive
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-adf21bb4c458f1a36
  commit: efca9c6d0f15c237a56c8e9f7d40065a6d30a7ed
  session: null
  claimed_at: 2026-10-03T00:53:48Z
  expires_at: null
archive: null
created_at: 2026-10-02T17:14:05Z
updated_at: 2026-10-03T00:53:51Z
created_by:
  id: agent:claude-code/phase4-groom-tags-archive
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-SQQ33
  name: ""
extensions: {}
---

## Description

The client half of thread archival, split out of TKT-01M29CRJ64 (Implement thread archival) in Phase 4 grooming so the server work can land while client PRs queue for the committed dist (docs/15 "Order").

What it covers:
- An admin's "Archive thread" action, with its confirmation.
- What an archived thread shows: a notice on the thread page, a flag in list rows, and no control the server would refuse (docs/06 "The thread head", decided by the run 2026-10-02).

## Acceptance criteria

- [ ] An archived thread shows "This thread is archived." and offers no composer, reply, edit, delete, restore, flag or moderation control to a member, a moderator or an admin, while watch and Mark unread stay (ThreadScreen.test.tsx, threadActions.test.ts)
- [ ] An archived row in any thread list carries an "Archived" flag with that accessible name (ThreadListScreen.test.tsx)
- [ ] An admin, and nobody else, is offered Archive on a live thread; the confirmation says it cannot be undone, sends the reason, and the page then shows the archived state (threadActions.test.ts, thread-moderation.spec.ts)
- [ ] The archived thread page and the archive panel pass axe at 390 and 1440 px, one scan per test
- [ ] The committed dist is regenerated with go generate ./internal/web/spa and the reader is within the 120000 B gzip budget (npm run budget), its growth stated in the PR

## Implementation plan

### Settled in grooming

- **What an archived thread shows.** The thread page says "This thread is archived." where a locked thread says it is locked (web/client/src/screens/ThreadScreen.tsx, the `data-thread-state="locked"` notice). It offers nobody, admins included, a reply, an edit, a delete, a restore, a flag or a moderation control, because the server refuses every one. Its list row carries an "Archived" flag where a locked row carries the lock. The thread is listed and read as before (docs/06 "The thread head", run decision pending maintainer review; docs/08 "Locking semantics").
- **Who archives.** An admin, by role (docs/08 "Primitives"). The action is offered to a viewer whose role is admin, not to one who only moderates. The server route is TKT-01M29CRJ64's `POST /api/v1/admin/threads/{id}/archive` with `{reason?}`, and the header's `archived` field comes from the same ticket.
- **No unarchive control.** v1 has no unarchive (docs/08, docs/03), and whether it gets one is TKT-01M3YSQQ5X's open decision. The confirmation says so, because the action cannot be undone: "Archiving makes this thread read-only for everyone, admins included. It cannot be undone."
- **Watching and reading stay.** Watch, mute, Mark unread and First unread are still offered, since the server allows them (TKT-01M29CRJ64).
- **Bundle.** In the reader bundle, because ThreadScreen, ThreadRow and ModerateDialog are there and the budget counts every chunk but admin. The estimate is about 0.5 to 0.7 KB gzip:
  - the notice and the row flag: 0.15 KB;
  - the `archived` gating of the controls: 0.15 KB;
  - the archive panel kind with its confirmation text: 0.3 KB.

### Approach

1. **Types.** Add `archived: boolean` to `ThreadHeader` and `ThreadRow` in web/client/src/api/dto.ts.
2. **Row flag.** In components/ThreadRow.tsx, beside the `locked` flag, render `<span class="thread-flag archived"><Icon name="archive" size={14} /><span class="visually-hidden">Archived</span></span>` when `t.archived`, with `title="Archived"`. Add an `archive` glyph to components/Icon.tsx, or reuse an existing one if the icon set has a fitting glyph. Check the contrast of the flag on the pinned tint, as `.row-pinned .thread-row-meta` does.
3. **Thread page.** In screens/ThreadScreen.tsx:
   - When `head.archived`, render `<p class="locked" data-thread-state="archived">This thread is archived.</p>` in place of the locked notice. An archived thread that is also locked shows only the archived notice.
   - Pass `archived` into the decisions about what the page offers:
     - the composer and the sticky Reply are not rendered (`viewer.can_reply` is already false from the server);
     - `PostControls` and `OwnControls` offer no edit, delete or restore;
     - the flag action is not offered;
     - `moderate` in `ThreadActionOptions` is unset, so no Lock, Pin, Rename, Move, Slow mode or Edit tags is offered.

     Keep the gating in one helper, `readOnlyThread(head)` beside `postControls.ts`, so each control reads one rule.
4. **Archive action.**
   - In shell/threadActions.ts, add an `archive` option offered when the viewer is an admin by role (`staff` in ThreadScreen.tsx is moderator or admin, from `isStaff(viewer.value.user?.role)`, so pass `admin` alongside it from the same role) and the thread is not archived.
   - Add `'archive'` to `PanelKind`.
   - In components/thread/ModerateDialog.tsx, the panel is titled "Archive this thread?" with an optional reason (`maxModerateReason`) and the confirmation text above, and its button reads "Archive". It uses `useModal`, as the other panels do.
   - The 200 header goes through `storeThreadHeader`.
   - A `conflict` says "This thread is already archived." A `forbidden` says `moderateForbidden`.
5. **Built client.** Run `go generate ./internal/web/spa`, commit the dist, and run `npm run budget`.

### Tests

- Vitest:
  - `ThreadScreen.test.tsx`: an archived header shows "This thread is archived.", no composer, no sticky Reply, no post edit, delete, restore or flag, and no moderation menu entries, for a member, a moderator and an admin fixture. A locked and archived thread shows only the archived notice.
  - `ThreadRow` (ThreadListScreen.test.tsx): an archived row has the flag with an accessible name "Archived".
  - `threadActions.test.ts`: archive is offered to an admin on a live thread, never to a moderator or member, and never on an archived thread. Watch and Mark unread are still offered on an archived thread.
  - `ModerateDialog`: archiving sends the POST with the reason and updates the head to archived. A conflict shows the "already archived" message.
- e2e:
  - `thread-moderation.spec.ts`, an admin session: archive a thread with a reason; the page shows the notice and no composer; the list row shows the flag. Run at 390 and 1440 px, with one axe scan per test.
  - A member session on the same thread sees the notice and no controls.
  - Stored sessions; no `/auth/*` call, so `paceAuth` and `e2e/session.ts` are unchanged.

### Out of scope

- The server route, the `archived` field and every refusal: TKT-01M29CRJ64 (Implement thread archival).
- An unarchive control: TKT-01M3YSQQ5X (Decide whether an archived thread can be unarchived, and by whom).

## Notes

**agent:claude-code/phase4-groom-tags-archive** at 2026-10-02T17:22:37Z

run decision: pending maintainer review. Phase 4 grooming (tags and archive lane) settled what an archived thread shows, in docs/06 "The thread head" on branch groom/p4-tags-archive. It says "This thread is archived." where a locked thread says it is locked, offers nobody a control the server refuses, and its list row carries an "Archived" flag. It stays listed and readable. If the maintainer answers TKT-01M3YSQQ5X (Decide whether an archived thread can be unarchived, and by whom) with an unarchive, the confirmation's "It cannot be undone" changes with it.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:33Z

Run decision relayed by the orchestrator (pending maintainer review), 2026-10-02. The exit-check grooming (PR #375, TKT-01M3YMG0NX) decided in docs/06 "Moderator flow for drift" that "Reply as new thread" stays available under a post of a locked or archived thread: it writes only the new thread's log, so the archived thread stays read-only. Hiding the action in the client and refusing it on the server both lost. Build to it.
