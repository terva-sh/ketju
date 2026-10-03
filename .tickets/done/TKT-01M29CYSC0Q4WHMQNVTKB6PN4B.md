---
schema: 3
id: TKT-01M29CYSC0Q4WHMQNVTKB6PN4B
title: Implement reply as a new thread
type: task
status: done
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
  - ref: pr:399
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-03T09:51:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-cysc0
  name: ""
extensions: {}
---

## Description

The member-facing version of split: open a composer for a new thread that quotes the source post, and leave a small "continued in <link>" note under it.

This is how drift gets handled before a moderator has to. Someone who notices they are about to take a thread sideways gets a one-click way to do it properly, which is cheaper than any amount of guidance about staying on topic.

## Acceptance criteria

- [x] The source post gains a continued-in note linking to the new thread
- [x] The action is available to members, not only moderators
- [x] The continued-in note and the new thread's reference to its source name the other thread only to viewers who may read it (TestContinuedNoteRevealsOnlyToReaders)
- [x] The flow passes e2e/reply-as-new-thread.spec.ts at 390 and 1440 px with one axe scan per test
- [x] The committed dist is regenerated and the reader stays within the 120000 B gzip budget
- [x] The new thread opens with the source post pre-filled as an ordinary editable blockquote, which is the member's own text and survives a later removal of the source (TestPrefilledQuoteIsMemberText)
- [x] The link is the continues key on the new thread's ThreadCreated, so ketju reindex rebuilds the note from the new thread's log (TestContinuedNoteSurvivesReindex), and the source log is untouched

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

**agent:claude-code/phase4-run-cysc0** at 2026-10-03T04:48:20Z

Phase A is done on run/cysc0-reply-as-thread. The branch is stacked on run/cysba-merge-posts because both change the thread page's notices, movedraw.go and the header read. It is not pushed. It rebases onto main after the split and merge PRs merge, and it waits for the dist after them. The migration is 0027_thread_continuations; it is renumbered at push time if another PR takes 0027 first.

### What landed

- **Record.** `ThreadCreatedBody.Continues *PostRef` (`continues {thread_id, ordinal}`, omitempty).
  - `CheckThreadCreated` refuses thread 0, ordinal 0 and the record's own thread. The actor checks it before writing, and also refuses a ThreadCreated that names a frame ordinal. The projection treats a bad body as a log fault.
  - Title redaction (`blankCreated`) keeps `continues`, because it names a post and not the title. Without this, redacting the new thread's title would have dropped the link.
- **Projection.** Migration 0027 adds `thread_continuations (thread_id PK, source_thread_id, source_ordinal)`, indexed on the source pair. It is listed in ProjectionTables and ClearProjections, and docs/04 has a section for it. No backfill is needed.
- **Service.**
  - `CreateThreadRequest.From`. `continuesFrom` answers validation-failed for a malformed id. It answers not-found for:
    - an unreadable or missing thread;
    - a missing post;
    - a post not shown as itself.
    It runs after the forum, title, tag, source and attachment checks, so a refusal writes nothing. A locked or archived source is accepted, per the 2026-10-02 run decision. The source log is never written.
  - `ThreadViewer.CanCreateThread` is true for a member, in a forum that is not read-only to them, who holds create_thread there.
- **Drawing.**
  - `Post.ContinuedIn []ThreadLink` is always a list. `ThreadHeader.ContinuedFrom *ContinuedFrom`. Both are drawn per viewer through moveLinks and never cached.
  - The header's split source, continued-from and "is anything continued" now come from one statement, `ThreadLinksOf`, which replaces `SplitSourceOf`. So a thread nobody continued runs no extra statement for continued_in, and the lens statement tests stay equal. Paths without a header (single post, replies, user posts, queue) always read `ContinuationsInRange`.
- **API.** `POST /forums/{slug}/threads` takes `from {thread, ordinal}`; both fields are required (bad-request otherwise). docs/07 has the route row, a "Reply as a new thread" section, and the post and viewer example.
- **Client.**
  - The "reply as new thread" link is in the Post footer, and under PostMore on a phone, when `can_create_thread` is true and the post is shown as itself.
  - `ContinuedIn` draws a note under the post, and `ContinuedFromNote` draws the header line.
  - NewThreadScreen reads `?from=&ordinal=`, fetches the post's page, and shows "Continuing <title>, post #N." Once the draft has loaded it pre-fills `@handle wrote:` and a plain blockquote (`continueQuote`). The quote goes after any draft text, and only once per post. A post that cannot be continued gets a notice, and the thread is sent without `from`.

### Deviations from the plan

- `ThreadRef` in the plan is `ThreadLink` in the code, as for the move links.
- The quote is read from the post's rendered HTML (`bodyText` on a DOMParser document), because a member cannot read another member's markdown source. Links and formatting come through as text.
- There is no separate `PostFooter` component. The action lives in Post and PostMore, and its tests are in ThreadPage.test.tsx and PostMore.test.tsx.
- TestReplyAsNewThreadRefusesRedactedSource and TestRedactDoesNotFollowContinues are in internal/web/api, because only that package can rewrite a log offline. The pre-user refusal is in the route test there.
- e2e/targets.spec.ts now expects "reply as new thread" in alice's phone More list.

### Tests

- **Go:**
  - threadlog: TestThreadCreatedContinuesRoundTrip (CBOR and JSON goldens, with and without the key), TestThreadCreatedContinuesMalformed. The redact title test now holds `continues`.
  - service/threads continue_test.go: TestReplyAsNewThreadNeedsCreateThread, TestReplyAsNewThreadRefusesUnreadableOrRemovedSource, TestReplyAsNewThreadWritesOneLog (open, locked and archived sources), TestContinuedNoteRevealsOnlyToReaders, TestContinuedNotInCachedPosts, TestContinuedNoteSurvivesReindex, TestPrefilledQuoteIsMemberText.
  - web/api continue_test.go: TestReplyAsNewThreadRoute, TestReplyAsNewThreadRefusesRedactedSource, TestRedactDoesNotFollowContinues.
- **Vitest:** NewThreadScreen (pre-fill, draft kept, one quote per post, unreadable or removed source, no-post addresses, a not-found refusal), ThreadPage (action offered, note, header), PostMore, quote (`continueQuote`).
- **e2e:** reply-as-new-thread.spec.ts, on carol's session at 390 and 1440 px, with one axe scan per test.

The thread-view bundle was measured with go generate and not committed: 85230 B gzip of 120000, against 84344 B on the merge branch.

## Summary

Landed in one PR on main, after the split (#397) and the merge (#398).

- **Record.** `ThreadCreated` gains an optional `continues {thread_id, ordinal}`.
  - It is checked before it is written: thread 0, ordinal 0 and the thread itself are malformed.
  - Title redaction keeps it.
- **Projection.** Migration 0027 projects it into `thread_continuations`, which ketju reindex clears and rebuilds.
- **Route.** `POST /forums/{slug}/threads` takes `from {thread, ordinal}`.
  - A source the member cannot read, or a post not shown as itself, is not-found, and nothing is written.
  - A locked or archived source is accepted, by the run decision. Only the new thread's log is written.
  - The action is a member's: anyone with create_thread in the forum may use it.
- **Drawing.** `continued_in` on the source post and `continued_from` on the new thread's header are drawn per viewer at draw time.
  - Each names the other thread only to its readers, and the cached posts never hold them.
  - One header statement, ThreadLinksOf, says whether anything is continued, so a thread nobody continued runs no extra statement.
- **Client.**
  - "Reply as new thread" sits in the post footer, and under More on a phone.
  - The new thread screen pre-fills "@handle wrote:" and a plain blockquote of the post. It is the member's own, editable text.
  - The source post shows "Continued in" and the new thread shows "Continued from".
- **Bundle.** The thread view is 90045 B gzip against the 120000 B budget.
