---
schema: 3
id: TKT-01M29CRJ64G7PJBPK8Z52SE8NW
title: Implement thread archival
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area:storage
  - effort:S
assignees: []
milestone: phase-4
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: null
dependencies:
  - TKT-01M29BYYGNN34ZP3AMFZ540R2H
  - TKT-01M29C0WE5GNEYS35WWVCRBWMY
blocks_on: none
references:
  - ref: design:locking-semantics
    path: docs/08-moderation.md
  - ref: pr:361
    path: null
claim:
  actor: agent:claude-code/phase4-run-crj64
  branch: run/crj64-archive
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a1933242261dfde8d
  commit: 06db6b6f96a53bc238455a37bf964f05a9c0c905
  session: null
  claimed_at: 2026-10-02T21:51:42Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:27:36Z
updated_at: 2026-10-02T21:51:44Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-crj64
  name: ""
extensions: {}
---

## Description

The `ThreadArchived` record makes a thread read-only for everyone including moderators, and the open-thread cache drops archived threads eagerly and permanently.

Archiving is stronger than locking. A locked thread is a moderation state a moderator can still write into; an archived thread is closed history. Unarchiving to make a change is the deliberate friction.

## Acceptance criteria

- [ ] POST /admin/threads/{id}/archive, admin only with a row in adminRouteGates, appends one ThreadArchived with its reason, writes thread.archived (in audit.Actions and docs/04), and the header and list rows say archived (TestArchiveThread, TestArchiveRefusals)
- [ ] An archived thread refuses every write from everyone, the author, members, moderators by role and by group, admins and agents, and its log does not grow (TestArchivedThreadIsReadOnlyToEveryone)
- [ ] The actor refuses any append after ThreadArchived, so a reply racing an archive never lands after it, and a reopened archived thread is sealed (TestArchiveRacingReplyNeverLandsAfter, TestActorRefusesAfterThreadArchived, TestArchivedThreadSealedOnReopen)
- [ ] Archived threads are evicted from the open-thread cache ahead of live ones once archived through the service (TestArchiveDropsThreadFromCacheFirst)
- [ ] The archived state comes from the log and survives a projection rebuild and a restart (TestArchivedStateSurvivesReindex)
- [ ] Reading, watching and the read position are unchanged, the viewer block offers no reply or upload, the response cache key moves, and ketju redact still reaches the thread (TestArchivedThreadStaysReadable, TestArchivedViewerBlockOffersNothing, TestArchiveMovesTheResponseCacheKey, TestRedactReachesAnArchivedThread)
- [ ] docs/07 documents the archive route, its refusals and the archived field

## Implementation plan

### Settled in grooming

- **The record is fixed.** `ThreadArchived` is type 30 with body `{reason}` (docs/03 "Record types"), and it is in the catalogue (`threadlog.TypeThreadArchived`, `ThreadArchivedBody`). The projection already applies it (`applyArchived` sets `threads.archived = 1`). No format change, so docs/15 stop class 2 does not apply to archiving.
- **Who archives.** An admin, by role: docs/08 "Primitives" ("Archive thread | admin") and docs/07 "Moderation and admin" ("Archiving a thread is admin-only"). The 2026-09-30 forum-grant decision names lock, pin, move and slow mode, not archive, and admin routes keep their role gates. The route is behind the `admin` gate.
- **Read-only for everyone.** After `ThreadArchived`, nothing more is appended for anyone, admins included (docs/03; docs/04 "permissions"; docs/08 "Locking semantics"; docs/15 "Invariants, added"). Reading, watching, muting and read positions are unchanged, as in an archived forum. The thread stays in every list.
- **`ketju redact` is not refused.** The offline redaction still reaches an archived thread (docs/08 "Locking semantics", run decision pending maintainer review).
- **Audit.** docs/08's opening says every moderation action appends to `audit_log`. The projection writes `thread.archived` against the thread with `{"reason"}`, as it writes `thread.locked`. This replaces the "until its own ticket decides" in `auditFor`'s comment.
- **The cache.** `threadcache` already evicts archived threads first (`Options.Archived`, `MarkArchived`, and the tests `TestArchivedThreadIsEvictedBeforeALiveOne` and `TestMarkArchivedMovesAThreadToTheFrontOfTheQueue`). `cmd/ketju/serve.go` `archived(d)` reads the flag from the projection.
- **Unarchiving is split out.** It is not done here. docs/08 says "there is no unarchive record in v1 (03)", and docs/04 says a thread's archive "cannot be undone only because the log has no unarchive record". docs/15 and this ticket's original criterion 4 say a moderator unarchives. An unarchive needs a record type docs/03 does not name, which is docs/15 stop class 2, and the documents disagree on who may do it, which is class 6. The question is TKT-01M3YSQQ5X (Decide whether an archived thread can be unarchived, and by whom), with options and no default. This ticket does not wait on it.
- **The client is a separate ticket**, TKT-01M3YSQQ33 (Show archived threads and offer archiving in the client), so this server work does not queue for the committed dist (docs/15 "Order").

### Revised at claim

The approach below stands as groomed, except where this section says
otherwise:

- **The `archived` field is not added here.** Step 5's
  `ThreadHeader.archived` and `ThreadRow.archived` change the DTO goldens,
  and the client's `dto-contract.ts` holds `dto.ts` to those goldens with no
  extra keys. So the field cannot land without a change under `web/client`,
  which this server lane may not make (docs/15 "Order": one open PR changes
  the dist). It moves to TKT-01M3YSQQ33 (Show archived threads and offer
  archiving in the client), which adds it to the Go DTO and `dto.ts`
  together. Criterion 1's "the header and list rows say archived" stays
  unticked here. docs/07 says the field is not served yet. The viewer block
  still says what a client needs to offer: `can_reply` and `can_upload` are
  false.
- The actor's refusal also covers a batch. `frameFor` refuses when the actor
  is sealed or a `ThreadArchived` is already staged in the batch, and
  `commit` seals the actor once the batch holding it is written.
- `threadcache` reports a thread archived when its actor is sealed as well
  as when it was marked. So a reply that checks `th.Archived()` after an
  archive answers before doing any work.
- `ErrArchived` maps to `conflict` in `internal/problem`. The flag sweep's
  auto-hide treats it as nothing to hide.
- Test fixtures that append random records through an actor stop a thread
  at its `ThreadArchived`: `cmd/ketju` `writeThreads` and the threadlog
  property tests. `TestArchivedThreadIsEvictedBeforeALiveOne` writes its
  threads before it reopens them as archived.
- `TestArchivedStateSurvivesReindex` drives the real serve: an admin
  archives through the API, then `ketju reindex` runs and serve restarts.
  `TestRedactReachesAnArchivedThread` uses the redact fixture's world.

### Approach

1. **The actor refuses after an archive.** This closes the one gap the current checks leave. `Reply` (internal/service/threads/create.go) reads `th.Archived()` and then appends without `AtLength`, so a reply racing an archive could land after `ThreadArchived`. Make the refusal the actor's:
   - Add `Actor.Seal()` and a sentinel `threadlog.ErrArchived` (internal/store/threadlog/actor.go). Once sealed, every `Append` fails with `ErrArchived` and writes nothing. The check runs where the actor checks `Require`/`Refuse`, against queued records too, so a batch holding a `ThreadArchived` refuses every record after it in the batch.
   - The actor seals itself when it writes a `TypeThreadArchived` record.
   - `threadcache` seals a thread's actor when it opens one that `Options.Archived` reports archived, and in `MarkArchived`.
   - Map `ErrArchived` to `problem.CodeConflict` ("threads: ... is archived") where the service turns append errors into problems, so every write path answers as the existing `th.Archived()` checks do. Keep those checks: they answer before any work is done.
   - The offline `ketju redact` writes the log without an actor (internal/store/threadlog/redact.go) and is unaffected, as decided.
2. **Service: `Archive`.** Add `func (s *Service) Archive(ctx, v, id, reason string) (dto.ThreadHeader, error)` in `internal/service/threads/moderate.go`, through `appendDecided`:
   - `MayWrite`.
   - `forbidden` unless `v.Role == domain.RoleAdmin`. The gate checks this too, and the service checks it again so that no caller relies on the route alone.
   - `conflict` when `th.Archived()` or `a.row.Archived != 0`.
   - `validation-failed` for a reason `checkReason` refuses (over `MaxReason`).
   - Append `{Type: TypeThreadArchived, Body: &ThreadArchivedBody{Reason: reason}}`. After `appendDecided` returns, call `s.Threads.MarkArchived(id)`, so the cache drops the thread ahead of live ones. The actor sealed itself when it wrote the record, so this call orders eviction only.
3. **Route.** `POST /api/v1/admin/threads/{id}/archive` with `{reason?}`, or no body, through `h.adminRoute(..., adminGate, h.archiveThread)` in `internal/web/api/moderate.go`:
   - The body is decoded strictly and bounded at 4 KiB.
   - It answers 200 with the `ThreadHeader`, `no-store`, and is charged to the write limiter.
   - Add `"POST /api/v1/admin/threads/{id}/archive": "admin"` to `adminRouteGates`. `TestThreadModerationRoutesUseTheModeratesGate` must still pass, since this route is not on `moderates`.
4. **Audit.**
   - Add `case *threadlog.ThreadArchivedBody` to `auditFor` (internal/events/projection/audit.go): `ActionThreadArchived` = `"thread.archived"`, target the thread, `{"reason": b.Reason}`. A reason is admin-written, as a lock's is.
   - Update the "never" comment.
   - Add the action to `audit.Actions` and a row to docs/04's audit table.
5. **What the API says.**
   - Add `Archived bool json:"archived"` to `dto.ThreadHeader` and `dto.ThreadRow`, read from `threads.archived`. For the header, read it in `threadHeader`/`view.go`. For the row, add `t.archived` to `views.selectRows` and `ThreadRow`. The cross-forum lists share the row.
   - In the viewer block (internal/service/threads/view.go), `CanReply` and `CanUpload` are false on an archived thread. `RenameUntil` is already nil there.
   - The header is in the cached response, and archiving moves the log length and so the cache key. Nothing per viewer changes.
6. **Docs.**
   - Add an "Archiving threads" table and refusal list to docs/07 under "Renaming and moving threads". Its refusal order is `unauthenticated`/`forbidden` from the gate, `bad-request`, the thread read, `forbidden` to a non-admin, `conflict` when already archived, and `validation-failed` for the reason.
   - Add `archived` to the `ThreadHeader` and `ThreadRow` descriptions.
   - State in docs/07's reply, edit, moderation and flag rows that an archived thread is `conflict` to everyone. Most rows say so already, so check each against the built behaviour.

### Tests

- `TestArchiveThread` (internal/web/api, new `archive_test.go`, `dbtest.Open` fixtures):
  - An admin archives with a reason. The answer's header has `archived: true`.
  - The log ends in one `ThreadArchived` with the reason.
  - One `thread.archived` audit row has the admin as actor.
  - The thread list row has `archived: true`.
- `TestArchiveRefusals`, each writing nothing to the log or `audit_log`:
  - anonymous is `unauthenticated`;
  - a member, a moderator by role, and a member holding `moderate` through a group are `forbidden`, at the gate and again at the service;
  - a non-JSON body is `bad-request`;
  - an unknown thread is `not-found`;
  - an already archived thread is `conflict`;
  - a 501-character reason is `validation-failed`.
- `TestArchivedThreadIsReadOnlyToEveryone`, the docs/15 invariant. Table-driven over the actors {the thread's author, a member, a moderator by role, a moderator by group grant, an admin, an agent account} and the actions:
  - reply;
  - an author's own edit, delete and restore;
  - a moderator's edit, hide and restore of another's post;
  - lock and unlock;
  - pin and unpin;
  - move;
  - rename;
  - slow mode;
  - a flag;
  - a queue resolve;
  - archive again.

  Each pair the actor could do on a live thread is `conflict` on the archived one (`forbidden` where it already was). The log's length is unchanged after every one. Add the retag row when TKT-01M29CYSCP lands; whichever of the two lands second adds it.
- `TestArchiveRacingReplyNeverLandsAfter`: 20 goroutines reply while an admin archives. A walk of the log afterwards finds no record after `ThreadArchived`. Every reply either landed before it or answered `conflict`.
- `TestActorRefusesAfterThreadArchived` (internal/store/threadlog):
  - An actor that wrote `ThreadArchived` refuses a `Post`, an `Edit` and a `ThreadLocked` with `ErrArchived`, and the log length is unchanged.
  - A batch queued as `[ThreadArchived, Post]` writes the first and refuses the second.
  - `Seal()` on a fresh actor refuses at once.
- `TestArchivedThreadSealedOnReopen` (internal/store/threadcache): a cache whose `Options.Archived` reports the thread archived opens it sealed, and an append is `ErrArchived`.
- `TestArchivedStateSurvivesReindex` (cmd/ketju, beside reindex_test.go):
  - Archive through the API, then clear the projection and rebuild it from the logs. `threads.archived` is 1.
  - After a restart on the rebuilt database (a new cache with `archived(d)`), a reply is `conflict` and the log is unchanged.
- `TestArchiveDropsThreadFromCacheFirst`: after `Archive`, filling the cache to its bound evicts the archived thread before any live one. This is the service-level counterpart of the existing cache tests.
- `TestArchivedViewerBlockOffersNothing`: on an archived thread, `can_reply` and `can_upload` are false and `rename_until` is null for the author, a moderator and an admin. A member may still watch, mute and set the read position (`POST /threads/{id}/watch`, `PUT /threads/{id}/read`).
- `TestArchiveMovesTheResponseCacheKey`: a page cached before the archive is not served after it.
- `TestArchivedThreadStaysReadable`: a member who could read the thread still reads it, its posts and its history, and finds it in the forum's list and `/participated`. Search still finds it for a reader.
- `TestRedactReachesAnArchivedThread` (cmd/ketju): `ketju redact` on an archived thread blanks the post, and the thread stays archived.
- Invariants:
  - The session chokepoint is unchanged (`TestOneDoorIntoSessions`).
  - `adminRouteGates` has the new row (`TestAdminRoutesAreGated`).
  - No role or status change, so nothing new goes through `perm`.
  - The log format is unchanged (`TestEveryRecordTypeHasADecision` as is).
  - `TestActionsDocumented` covers `thread.archived`.

### Out of scope

- Unarchiving, the original criterion 4: TKT-01M3YSQQ5X (Decide whether an archived thread can be unarchived, and by whom), an open decision for the maintainer.
- The archive action, the archived notice and the list flag in the client: TKT-01M3YSQQ33 (Show archived threads and offer archiving in the client).
- Archiving many threads at once, or archiving old threads automatically: no ticket, and not a docs/09 deliverable.
- Refusing reply drafts in an archived thread. A draft is the viewer's own row, not the thread's (04 `drafts`), and a locked thread keeps them too. The client offers no composer there.

## Notes

**agent:claude-code/phase4-groom-tags-archive** at 2026-10-02T17:20:28Z

run decision: pending maintainer review. The offline `ketju redact` still redacts an archived thread (docs/08 "Locking semantics", on branch groom/p4-tags-archive).

Phase 4 grooming (tags and archive lane) checked docs/03: `ThreadArchived` is fixed as type 30 with body `{reason}`, so archiving needs no format change. The original criterion "Unarchiving is possible and recorded" did not stay here. docs/08 says v1 has no unarchive record, and docs/04 agrees. docs/15 says a moderator unarchives. An unarchive needs a record type docs/03 does not name (docs/15 stop class 2), and the documents disagree on who may do it (class 6). The question is filed as TKT-01M3YSQQ5X (Decide whether an archived thread can be unarchived, and by whom), with options and no default, for the maintainer. This ticket does not wait on it.

The client half is split into TKT-01M3YSQQ33 (Show archived threads and offer archiving in the client).
