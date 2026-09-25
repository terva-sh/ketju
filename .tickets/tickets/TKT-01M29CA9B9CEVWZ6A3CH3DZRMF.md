---
schema: 3
id: TKT-01M29CA9B9CEVWZ6A3CH3DZRMF
title: Implement create thread and reply
type: task
status: in-progress
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C0WD88M1C8EW4QQTX76NJ
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
  - TKT-01M29C5Y2EVNCBADSQYW12F01D
  - TKT-01M29P9RS0YMM3465BSH7AGSNJ
blocks_on: none
references:
  - ref: design:create-post-flow
    path: docs/02-architecture.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/ca9b9-create-reply
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 2ee6b9a7852a87ec279928c965180ed106c3aeb9
  session: null
  claimed_at: 2026-09-25T00:40:33Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-25T00:45:13Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`POST /forums/{slug}/threads` and `POST /threads/{id}/posts`, following the docs/02 create-post flow: rate limit and permission check, thread not locked, slow mode respected, render-validate the markdown, append through the thread actor, publish the event, and return the rendered post so the client can insert it immediately.

Rendering happens before the append and the HTML is discarded. That is deliberate: a post whose markdown cannot be rendered should be refused rather than written to an append-only log forever.

## Acceptance criteria

- [ ] A post is durable before the response returns, and the response carries ordinal, revision, and log length
- [ ] A locked thread, a missing permission, and an active slow mode each refuse the write with their own problem code
- [ ] Source that fails to render is refused before anything is appended
- [ ] The rendered post in the response is identical to what a later read returns

## Implementation plan

Revised at claim (2026-09-25), after reading the code as it stands.

### Approach
1. `internal/service/posts.Writer` is the one seam for thread appends. It holds `Publish func(events.Event)`. `Append(th, a)` calls `th.Actor.Append(a)`, publishes `{Thread, Ordinal, LogLength}`, and returns the `threadlog.Result`. TKT-01M29CA9BP reuses it. `threads.Service` gains `Writer *posts.Writer`, `IDs *threadid.Generator`, `Project func(ctx, id, logLength) error`, and `Now func() time.Time`.
2. `threads.Service.Reply(ctx, v, id, req)` backs `POST /api/v1/threads/{id}/posts {source, reply_to?, attachments?}`, in this order:
   - Require a member (member.Require).
   - Call `access`. Missing read gives not-found, unauthenticated or forbidden, as for every thread endpoint.
   - `!canReply` gives `forbidden`.
   - Under `Threads.With`:
     - An archived thread gives `conflict`, including for a moderator.
     - `row.Locked` gives `conflict` unless the viewer is a moderator (docs/08 "Locking semantics").
     - Slow mode is `settings.SlowModeDefaultSeconds`, off by default, and a moderator bypasses it. The viewer's newest post is found with `Index.ByAuthor`, then `Entry.CreatedMS`. If it is too recent, the answer is `rate-limited` with `Retry-After` set to the remaining wait. The error type is `problem.RetryAfter`, and `WriteError` writes the header.
     - Render with `renderSource(th, source, replyTo)`, the same function preview uses. Empty or whitespace-only source gives validation-failed. An invalid reply_to gives validation-failed (existing mapping). Too large gives payload-too-large.
     - Non-empty attachments give validation-failed until blobs exist (no blob upload is built yet).
     - Append a Post through the Writer with `PostBody{Source, ReplyTo, Mentions}`. `FrameFlagMachineAuthored` is set when the users row has kind `agent`.
   - The response is built by `s.Post(ctx, v, id, ordinal, "")`. That is the same assembly as a later read, which gives AC4.
3. `threads.Service.CreateThread(ctx, v, forumSlug, req)` backs `POST /api/v1/forums/{slug}/threads {title, source, tags}`:
   - A member is required.
   - Look the forum up with the new query `GetForumBySlug`. No such forum gives not-found, and so does Perm Hidden. Read denied gives forbidden. `Can(create_thread)` false gives forbidden. An archived forum gives conflict.
   - Validate the title: trim it, then it must be 1–200 characters with no control characters.
   - Tags must be empty. No tag rules are documented, and whatever a log holds it holds forever, so any tag is refused as validation-failed until a ticket defines them. A draft will be filed.
   - Render-validate the source with no thread (nothing to quote yet). reply_to is not accepted.
   - Then `IDs.New()`, `Threads.Create(id, forum, now)`, and append ThreadCreated `{Title, Slug: Slug(title), ForumID}`, then the Post, both through the Writer.
   - Then call `Project(ctx, id, logLength)` synchronously, so the thread row exists before the response and the client can open the thread at once. The bus delivers the same events too, and projection is idempotent, so a failed synchronous projection is retried there. Here it is returned as an error.
   - The response is `View(ctx, v, id, Query{})`'s header and first post, which is the same assembly as a later read.
4. `access` treats `post_count == 0` as not-found. That covers the crash between the two appends ("Projection and listing hide such threads").
5. `Slug(title)` folds Latin letters with diacritics to ASCII through a small table, then lowercases. Runs of anything else become '-'. It is trimmed, cut to 60 bytes on a separator where possible, and the empty result is "thread". No x/text dependency is added.
6. DTOs with goldens: `CreatedPost{post, ordinal, revision, log_length}` and `CreatedThread{thread, post: CreatedPost}`. They also go in the TS types and the client dto-contract, and in docs/07.

### Tests
- AC1: with SyncAlways, reply and create both return. A fresh threadcache over the same root then reads the post back. ordinal, revision and log_length equal the index's.
- AC2: a locked thread gives conflict, and a moderator's reply passes. A member without reply gives forbidden. Slow mode gives rate-limited with Retry-After, a moderator bypasses it, and the setting at 0 lets the post through.
- AC3: bad reply_to, empty source, oversized source, and a render-time handle-lookup failure each leave the log size and the Index.Len unchanged.
- AC4: the response post equals GET /threads/{id}/posts/{n} and the thread page's post, with a cold render cache.
- A race test: 50 concurrent replies give dense ordinals and one event per append.
- Create thread: title limits, tags refused, the slug, and a thread with no posts being not-found.

### Settled in grooming (2026-09-24)

- Title is at most 200 characters. The slug is an ASCII fold of the title, at most 60 characters.
- ThreadCreated and the first Post are two appends. A crash between them leaves a thread with no posts. Projection and listing hide such threads; do not invent a new record type.
- Slow mode: honour the setting, which is off by default (TKT-01M29P9RS0). Storing it per thread is Phase 2.
