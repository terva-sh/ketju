---
schema: 3
id: TKT-01M3YSQQ33K01M960MGMSZ90P2
title: Show archived threads and offer archiving in the client
type: task
status: done
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
  - ref: pr:395
    path: null
claim: null
archive: null
created_at: 2026-10-02T17:14:05Z
updated_at: 2026-10-03T06:50:35Z
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

- [x] An archived thread shows "This thread is archived." and offers no composer, reply, edit, delete, restore, flag or moderation control to a member, a moderator or an admin, while watch and Mark unread stay (ThreadScreen.test.tsx, threadActions.test.ts)
- [x] An archived row in any thread list carries an "Archived" flag with that accessible name (ThreadListScreen.test.tsx)
- [x] An admin, and nobody else, is offered Archive on a live thread; the confirmation says it cannot be undone, sends the reason, and the page then shows the archived state (threadActions.test.ts, thread-moderation.spec.ts)
- [x] The archived thread page and the archive panel pass axe at 390 and 1440 px, one scan per test
- [x] The committed dist is regenerated with go generate ./internal/web/spa and the reader is within the 120000 B gzip budget (npm run budget), its growth stated in the PR

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

**agent:claude-code/phase4-run-SQQ33** at 2026-10-03T01:30:40Z

Phase A is built on run/sqq33-thread-archive. The branch is stacked on run/sqpzx-thread-tags, as the orchestrator asked, because both change the thread head and the rows. It is rebased onto main once the tags PR merges. The dist is not committed yet: it waits for its place in the dist queue (docs/15 "Order").

### Server: the archived field

- `archived` is added to the Go `dto.ThreadHeader` and `dto.ThreadRow`, to `dto.ts`, and to the goldens, all in one commit. `dto-contract.ts` holds `dto.ts` to the goldens.
  - The header reads the projection row (`header()` in `service/threads/view.go`).
  - The rows read `t.archived`, which is added to `views.rowColumns` and scanned in `scanRows`. That covers a forum's list and every cross-forum list (`list.go`, `crosslist.go`).
  - A new golden, `thread-archived-thread.member`, is an archived thread in a live forum. `thread-archived.member` is the archived forum's.
  - One cross-forum fixture row is archived, so `TestEveryFieldIsPopulatedSomewhere` holds the field.
- Tests:
  - `TestArchiveThread` now checks that the archive's answer says archived.
  - `TestArchivedThreadStaysReadable` checks the page header and the rows in the forum's list and in /participated, for an archived thread and for a live one beside it.
  - Removing each of the four assignments alone fails one of those tests.
- docs/07's "Archiving threads" no longer says the field is missing. It names where the field is.
- This is the rest of TKT-01M29CRJ64's criteria 1 and 7. That ticket is not mine to tick; the report to the orchestrator says so.

### Client

- **The notice.** "This thread is archived." takes the place of the locked notice (`data-thread-state="archived"`). A thread that is both locked and archived shows only the archived notice.
- **What the page offers.**
  - `archived` is read from the newest header, so the state shows as soon as an archive answers, a moment before the page is answered again. A test holds the refetch to prove it.
  - On an archived thread, `canReply` and `mayModerate` are false. That removes the composer, the phone's reply bar, Reply, Rename, Lock, Pin, Move, Slow mode and Edit tags.
  - In `ThreadBody`, `readOnlyViewer` (in `postControls.ts`) gives the block no reply, no upload, no moderation, and a forum read-only to the viewer, so `postControls` offers nothing on any post. The author's own post inside the windows is covered too, and so is flag.
  - Watch, First unread, Mark unread, Copy link, and "unread from here" on a post stay.
  - The LensMenu still reads the server's `can_moderate` for the Removed posts lens, because reading is allowed on an archived thread.
- **The row flag.** An Archived flag takes the place of the lock flag: an `archive` icon plus a visually hidden "Archived", with a title. This follows the locked flag, as the plan settled.
- **Archive.**
  - Offered as "Archive…" to an admin by role on a thread that is not archived, whatever the page's `can_moderate` says.
  - Panel title: "Archive this thread?". It shows the warning "Archiving makes this thread read-only for everyone, admins included. It cannot be undone." The reason textarea is focused and is described by the warning.
  - It sends `POST /admin/threads/{id}/archive` with `{reason}`, or `{}` when no reason is given. The answer's header is stored.
  - A conflict says "This thread is already archived." Once the page is answered again, the panel keeps its words and offers Close.
- **TKT-01M3YSQPZX criterion 3.** Edit tags is not offered on an archived thread. The archived test checks that the `tags` action is absent for a member, a moderator and an admin. That ticket's criterion is ticked in this PR.
- **"Reply as new thread"** (the relayed run decision). The client has no such action yet, so nothing hides it. Whoever builds it should keep it available under a post of an archived thread.

### Measured

- The reader bundle grows from 81345 B on the tags branch to 81668 B, +323 B. Against main at 79983 B the growth is +1685 B with the tags change, and this ticket's share is within the plan's 0.5 to 0.7 KB.
- Vitest: 2618 tests pass. Mutation checks: each gate was reverted alone, and each one fails a test.
- e2e: `thread-moderation.spec.ts` has two new tests, each with one axe scan:
  - ada archives carol's new thread from the page and scans the panel. The page shows the notice and no composer, and the list row's flag is named "Archived".
  - carol reads a thread ada archived and is offered nothing that writes. The whole page is scanned.
- `go test -race -p 4 ./...` passes. The first run failed only on `internal/web/api`'s page and lock goldens, which each lacked the new `"archived": false` line. They are regenerated with `-update` in their own commit, and the package then passes under race.
- The full e2e suite at both widths passed 300 of 301. The one failure was the Unread surface test, with the "1 new" symptom that TKT-01M3ZE2211 (Stop the Phase 3 exit Unread surface test failing under CI load) records. That spec passes on its own rerun.

## Summary

Built on run/sqq33-thread-archive, rebased onto main at 5469a729.

### Server
- `archived` is added to `dto.ThreadHeader` and `dto.ThreadRow`.
  - The header reads it from the projection row.
  - The rows read `t.archived` through `views.rowColumns`, which covers a forum's list and every cross-forum list.
- The field is added in one change to `dto.ts`, the dto goldens (a new one for an archived thread) and the api and served goldens.
- docs/07 "Archiving threads" names the field.
- `TestArchiveThread` and `TestArchivedThreadStaysReadable` check it on the answer, the page header, and the rows of an archived thread and of a live thread beside it.
- This is the rest of TKT-01M29CRJ64's criteria 1 and 7.

### Client
- An archived thread says "This thread is archived." in place of the lock notice. It offers nobody a reply, the composer, a post edit, delete or restore, a flag, or a moderation control. Edit tags is included, which meets TKT-01M3YSQPZX's criterion 3.
- The page reads the newest header, so all of this applies as soon as an archive answers (`readOnlyViewer`).
- Watch, First unread, Mark unread, and "unread from here" on a post stay.
- Rows carry an Archived flag in place of the lock.
- An admin by role is offered "Archive…". The panel says the archive cannot be undone, takes an optional reason, and sends `POST …/archive`. A conflict says "This thread is already archived."

### Results
- The reader bundle grows from 83030 B to 83345 B, +315 B. index.js goes from 65031 B to 65345 B, and ApplyRoot.js from 5254 B to 5255 B.
- Vitest passes (2684 tests), and so does `go test -race -p 4 ./...`.
- The thread-moderation, thread-list, thread, targets, a11y and unread-lists e2e specs pass at 390 and 1440 px.
