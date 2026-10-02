---
schema: 3
id: TKT-01M29CYSC0Q4WHMQNVTKB6PN4B
title: Implement reply as a new thread
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-4
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
  - TKT-01M29CEE6V358M5KCBJGHD58HM
blocks_on: none
references:
  - ref: design:split-merge
    path: docs/06-thread-views.md
  - ref: pr:359
    path: null
  - ref: pr:371
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-02T22:47:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

The member-facing version of split: open a composer for a new thread that quotes the source post, and leave a small "continued in <link>" note under it.

This is how drift gets handled before a moderator has to. Someone who notices they are about to take a thread sideways gets a one-click way to do it properly, which is cheaper than any amount of guidance about staying on topic.

## Acceptance criteria

- [ ] The source post gains a continued-in note linking to the new thread
- [ ] The action is available to members, not only moderators
- [ ] The continued-in note and the new thread's reference to its source name the other thread only to viewers who may read it (TestContinuedNoteRevealsOnlyToReaders)
- [ ] The flow passes e2e/reply-as-new-thread.spec.ts at 390 and 1440 px with one axe scan per test
- [ ] The committed dist is regenerated and the reader stays within the 120000 B gzip budget
- [ ] The new thread opens with the source post pre-filled as an ordinary editable blockquote, which is the member's own text and survives a later removal of the source (TestPrefilledQuoteIsMemberText)
- [ ] The link is the continues key on the new thread's ThreadCreated, so ketju reindex rebuilds the note from the new thread's log (TestContinuedNoteSurvivesReindex), and the source log is untouched

## Implementation plan

### Settled in grooming

These hold whichever option is chosen:
- **docs/06 "Moderator flow for drift":** "Reply as new thread" is the member-facing version of the same primitive. It opens a composer for a new thread that quotes the source post and leaves a small "continued in <link>" note under it.
- **Criterion 4:** any member who may create a thread may use it; it is not a moderator action.
- **No post moves**, so the move rule's "moderate in both" does not apply. The new thread is created under `create_thread` in its forum, exactly as `POST /forums/{slug}/threads` is (`internal/web/api/create.go`, `service/threads/create.go` `CreateThread`).
- **docs/15 "Invariants, added"**, read as the move rule reads: the note under the source post names the new thread only to viewers who may read it. The new thread's reference to the source names it only to the source's readers.
- **Decided by the maintainer (2026-10-02)**, recorded in docs/03 (the `ThreadCreated` row and the "`ThreadCreated.continues`" paragraph) and docs/06 ("Reply as a new thread"). The answers were "Optional key on ThreadCreated" and "Member's own text":
  - `ThreadCreated` gains an optional `continues {thread_id, ordinal}`. Only this action writes it, and a split writes none.
  - The composer pre-fills an ordinary, editable blockquote. It shows to every reader of the new thread and does not follow a later removal of the source post. `ketju redact` follows `origin`, not `continues`.
  - The "continued in" note under the source post shows only to readers of the new thread. The "continued from" header shows only to readers of the source. Nobody else sees either.

### Approach

1. **Record** (`internal/store/threadlog/records.go`): add `Continues *PostRef` to `ThreadCreatedBody`, with tag `continues,omitempty`. `PostRef` has the same shape as `PostOrigin`: `thread_id uint64`, `ordinal uint32`. A `continues` with a 0 id or ordinal is malformed. Add the docs/03 golden case for a body with it and without it.
2. **Projection** (`internal/events/projection`). Add a `thread_continuations` table, `(source_thread_id, source_ordinal, thread_id)`, keyed on `thread_id`, with an index on the source pair.
   - It goes in the next free migration when this lands. That is 0025 on main today; TKT-01M29CXJZZ also plans 0025 for `post_moves`, so whichever lands second takes 0026.
   - Add it to `ProjectionTables` so `ketju reindex` clears and rebuilds it.
   - `applyThreadCreated` upserts the row when `continues` is set.
3. **API and service.**
   - `threadRequest` (`internal/web/api/create.go`) gains `from: {thread, ordinal}`, with `thread` as the base32 id.
   - `CreateThread` (`service/threads/create.go`) checks that the viewer may read the source thread and that the post exists and is shown as itself (not deleted, hidden, pending review, redacted or moved). Otherwise it answers `not-found`, so a request never confirms a thread the viewer cannot read.
   - It then writes `continues` into the `ThreadCreated` body. `create_thread` in the target forum is checked as today.
   - The source thread is not touched, so only one log is written.
4. **DTO, resolved per viewer at draw time** (as TKT-01M29CYSAM resolves `moved_to`; never in the cached posts):
   - `Post.ContinuedIn []ThreadRef` lists the threads continuing this post that the viewer may read, from `thread_continuations` and one `Perm.Can(..., PermRead)` per forum, memoised for the request. It is an empty list otherwise.
   - `ThreadHeader.ContinuedFrom *ContinuedFrom` (`{thread: ThreadRef, ordinal}`) is null unless the viewer may read the source.
   - Update docs/07's thread-create row and post example.
5. **Client, in the reader bundle** (about 0.8 KB gzip):
   - A "Reply as new thread" action in `Post`'s footer, under `PostMore` on a phone. It is offered when the viewer block's new `can_create_thread` is true.
   - The action opens `NewThreadScreen` at `/f/{forum}/new?from={thread}&ordinal={n}`.
   - `state/newThread.ts` pre-fills the source as a plain `> ` blockquote of the post's text, with a first line "@handle wrote:". It does not use `[quote=#N]`, which would name a post of the new thread. The member may edit it all.
   - `createThread` sends `from`.
   - The "continued in <link>" note goes under the post. The "continued from <link>" header goes on the thread.
6. Run `go generate ./internal/web/spa` and commit the dist. This PR queues for the dist.

### Tests

- `TestThreadCreatedContinuesRoundTrip`, and `TestThreadCreatedContinuesMalformed`.
- `TestReplyAsNewThreadNeedsCreateThread`: a member with `create_thread` succeeds, a member without it is refused, and a pre-user is refused.
- `TestReplyAsNewThreadRefusesUnreadableOrRemovedSource`: an unreadable, deleted, hidden, moved or redacted source is `not-found`, and nothing is written.
- `TestReplyAsNewThreadWritesOneLog`: the source log is byte-identical afterwards.
- `TestContinuedNoteRevealsOnlyToReaders`: three viewers, one reading both forums, one only the source and one only the new thread. The note and the header each appear only to the reader allowed to see it.
- `TestContinuedNotInCachedPosts`: two members in one viewer class who differ in reading the new thread are served different bodies and different ETags.
- `TestContinuedNoteSurvivesReindex`.
- `TestPrefilledQuoteIsMemberText`: after the source post is removed, the new thread's opening post still renders its blockquote. The new thread has no `[quote=#N]`.
- `TestRedactDoesNotFollowContinues`.
- Vitest: `PostFooter` (the action and when it is offered), the `NewThreadScreen` pre-fill, the note and the header.
- e2e: `e2e/reply-as-new-thread.spec.ts` on a member's stored session, at 390 and 1440 px with one axe scan per test.

### Out of scope

- Recording quote backlinks: TKT-01M3YME9AZ.
- Moving posts: TKT-01M29CYSAM and TKT-01M29CYSBA.

## Notes

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T17:21:12Z

open decision (docs/15 stop class 2, with class 1 inside it). Criterion 3 wants the continued-in note to be a log event, and no docs/03 record or field can carry it. Quoting a post in another thread has no syntax, and no rule says who sees the quoted text. The plan's '### Open decision' lists the options. The ticket stays in draft until the maintainer answers.

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T19:54:44Z

Maintainer decision, 2026-10-02. The orchestrator asked as choices, and the answers were 'Optional key on ThreadCreated' and 'Member's own text'. ThreadCreated gains an optional continues {thread_id, ordinal}, written only by reply as a new thread. The composer pre-fills an ordinary editable blockquote, which shows to every reader of the new thread and does not follow a later removal of the source post. The continued-in note shows only to readers of the new thread, and the continued-from header only to readers of the source. Recorded in docs/03 and docs/06. This supersedes the open decision in the earlier plan, which is rewritten.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:33Z

Run decision relayed by the orchestrator (pending maintainer review), 2026-10-02. The exit-check grooming (PR #375, TKT-01M3YMG0NX) decided in docs/06 "Moderator flow for drift" that "Reply as new thread" stays available under a post of a locked or archived thread: it writes only the new thread's log, so the archived thread stays read-only. Hiding the action in the client and refusing it on the server both lost. Build to it.
