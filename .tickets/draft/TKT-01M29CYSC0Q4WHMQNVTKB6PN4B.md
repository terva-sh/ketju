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
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-02T17:21:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-groom-moving-posts
  name: ""
extensions: {}
---

## Description

The member-facing version of split: open a composer for a new thread that quotes the source post, and leave a small "continued in <link>" note under it.

This is how drift gets handled before a moderator has to. Someone who notices they are about to take a thread sideways gets a one-click way to do it properly, which is cheaper than any amount of guidance about staying on topic.

## Acceptance criteria

- [ ] The new thread opens with the source post quoted and attributed
- [ ] The source post gains a continued-in note linking to the new thread
- [ ] The note is a thread event in the log so it survives a rebuild
- [ ] The action is available to members, not only moderators
- [ ] The continued-in note and the new thread's reference to its source name the other thread only to viewers who may read it (TestContinuedNoteRevealsOnlyToReaders)
- [ ] The flow passes e2e/reply-as-new-thread.spec.ts at 390 and 1440 px with one axe scan per test
- [ ] The committed dist is regenerated and the reader stays within the 120000 B gzip budget

## Implementation plan

### Open decision

This is docs/15 stop class 2, with a class 1 question inside it. The ticket stays in draft until the maintainer answers.

Criterion 3 says the "continued in" note is "a thread event in the log". docs/03 has no record or field that can carry it:
- `ThreadCreated` is `title, slug, forum_id, tags[]`;
- `Post` is `source, reply_to, mentions[], attachments[], origin`, and its `origin` means a copy made by split or merge.

docs/15 class 2 stops a new field on a log record. Its example is quotes on `Post`. Every way of making the note survive a rebuild therefore either changes the log or rebuilds the note from post text.

There is a second gap. The opening post "quotes the source post", which lives in another thread. The quote syntax `[quote=#N]` (docs/06 "Composer"; `internal/render/quote.go` accepts only `#N`) can only name a post in the same thread. Any option that quotes across threads needs a rule for who sees the quoted text: a reader of the new thread who cannot read the source forum, or one reading after the source post was removed. docs/05, 07 and 08 give no such rule, which makes it stop class 1.

The options (no default chosen):

1. **`ThreadCreated` gains an optional `continues {thread_id, ordinal}`.** It is written in the new thread's log.
   - A projection that reindex rebuilds turns it into the source post's "continued in" note, shown only to readers of the new thread.
   - The new thread shows a "continued from" header, shown only to readers of the source. That header is the attribution.
   - The opening post's quote stays the member's own blockquote, so it would outlive a later removal of the source post, unless option 4 is also taken.
   - Cost: one optional key on a frozen record.
2. **A new advisory record type** (for example 17, `PostContinued {ordinal, thread_id}`), claimed by docs/03's procedure.
   - It is written in the source log when the member creates the new thread. It is the only option in which the note is literally an event in the source thread's log.
   - Cost: a new type number, and a member's action writing into a thread they only replied to. Two logs are written, so it needs the ordering and recovery TKT-01M29CXJZZ settles for moves.
3. **No log change.**
   - The opening post quotes with a cross-thread attribution `[quote=<thread>#N]`, which is a renderer change.
   - The note is rebuilt from quote backlinks once TKT-01M3YME9AZ records them by re-rendering in the projection writer.
   - Criterion 3 is reworded to "rebuilt by `ketju reindex` from the logs".
   - Cost: the format question disappears, but a syntax is stored in post text for good, and option 4's question must be answered.
4. **The rule for a quotation of a post in another thread** (needed by 3, and optional with 1). Two choices:
   - the text shows only to a viewer who may read the quoted post, which is a tombstone otherwise and once it is removed, resolved at draw time as `moved_to` is; or
   - the text is the quoting member's and shows to every reader of the new thread, as a pasted blockquote does.

### Settled in grooming

These hold whichever option is chosen:
- **docs/06 "Moderator flow for drift":** "Reply as new thread" is the member-facing version of the same primitive. It opens a composer for a new thread that quotes the source post and leaves a small "continued in <link>" note under it.
- **Criterion 4:** any member who may create a thread may use it; it is not a moderator action.
- **No post moves**, so the move rule's "moderate in both" does not apply. The new thread is created under `create_thread` in its forum, exactly as `POST /forums/{slug}/threads` is (`internal/web/api/create.go`, `service/threads/create.go` `CreateThread`).
- **docs/15 "Invariants, added"**, read as the move rule reads: the note under the source post names the new thread only to viewers who may read it. The new thread's reference to the source names it only to the source's readers.

### Approach

This is provisional; it is rewritten once the decision is made.

1. Add a "Reply as new thread" action to `Post`'s footer: under `PostMore` on a phone and among the actions on a desktop. It is offered when the viewer may create threads in the source forum, which needs a `can_create_thread` boolean in the thread page's viewer block, beside `can_reply`.
2. The action opens `NewThreadScreen` at `/f/{forum}/new?from={thread}&ordinal={n}`, with the composer prefilled. The quote form depends on the decision.
3. `createThread` (`state/newThread.ts`) sends `from: {thread, ordinal}`. The server checks that the viewer may read the source post and that it is shown as itself, then records the link as the decision says.
4. The note and the header are drawn per viewer at draw time, as TKT-01M29CYSAM draws `moved_to`.
5. Client UI goes in the reader bundle: about 0.8 KB gzip for the action, the prefill and the note.

### Tests

- `TestReplyAsNewThreadNeedsCreateThread`: a member with `create_thread` succeeds, a member without it is refused, and a pre-user is refused.
- `TestReplyAsNewThreadRefusesUnreadableOrRemovedSource`.
- `TestContinuedNoteRevealsOnlyToReaders`: three viewers, one reading both forums, one only the source and one only the new thread.
- `TestContinuedNoteSurvivesReindex`.
- Option 4 adds `TestCrossThreadQuoteRule`.
- Vitest: `PostFooter` (the action), `NewThreadScreen` prefill, and the note.
- e2e: `e2e/reply-as-new-thread.spec.ts` on a member's stored session, at 390 and 1440 px with one axe scan per test.

### Out of scope

- Recording quote backlinks: TKT-01M3YME9AZ.
- Moving posts: TKT-01M29CYSAM and TKT-01M29CYSBA.

## Notes

**agent:claude-code/phase4-groom-moving-posts** at 2026-10-02T17:21:12Z

open decision (docs/15 stop class 2, with class 1 inside it). Criterion 3 wants the continued-in note to be a log event, and no docs/03 record or field can carry it. Quoting a post in another thread has no syntax, and no rule says who sees the quoted text. The plan's '### Open decision' lists the options. The ticket stays in draft until the maintainer answers.
