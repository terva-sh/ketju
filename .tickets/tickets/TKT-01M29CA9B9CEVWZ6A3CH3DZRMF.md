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
updated_at: 2026-09-25T00:40:33Z
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

Depends on TKT-01M29P9RS0, which states the slow-mode default in docs/08. Plan against that text.

### Approach
1. `service/posts.Writer` becomes the one seam for every thread append. It wraps `Actor.Append`, then `events.Bus.Publish(events.Event{Thread, Ordinal, LogLength})`, then returns `threadlog.Result`. TKT-01M29CA9BP and later moderation writes reuse it, and nothing else publishes.
2. `POST /api/v1/threads/{id}/posts {source, reply_to?, attachments?}`, in order:
   - Require `viewer.Member()` (401 anon, 403 pre_user). `Can(reply)` → `forbidden`; `not-found` when `read` is missing.
   - Thread archived (`threadcache.Thread.Archived()`) → `conflict`. Locked and the viewer is not a moderator (docs/08) → `conflict`.
   - Slow mode active and the viewer is not a moderator → `rate-limited` with `Retry-After` set to the remaining wait. The viewer's last post time in this thread comes from the in-memory index (`Index.ByAuthor` → last entry `CreatedMS`), and the interval from TKT-01M29CRJ50's storage.
3. Render-validate with `render.RenderWith(source, ctx)`, using the same resolvers as the read path. It must return reply_to validation from TKT-01M29C2WWG. A render error or an invalid reply_to → `validation-failed`. `render.ErrTooLarge` already maps to `payload-too-large`. Empty source → `validation-failed`. Nothing is appended before this point.
4. Append `threadlog.PostBody{Source, ReplyTo, Mentions: user ids from the result, Attachments}` with `FrameFlagMachineAuthored` when `viewer.User.Kind=="agent"`. Then `render.Cache.Put(Key{thread, ordinal, 1, render.Version}, html)`. Build the response post with the shared assembler from TKT-01M29CA9AX, so the create response and a later read produce identical HTML.
5. `POST /api/v1/forums/{slug}/threads {title, source, tags}`: `Can(create_thread)`, validate the title, `threadid.Generator.New()`, `threadcache.Cache.Create(id, forumID, now)`, append `ThreadCreatedBody{Title, Slug, ForumID, Tags}`, then the first Post through steps 2–4.
6. Response DTOs, new in internal/dto with goldens: `CreatedPost{post dto.Post, ordinal, revision, log_length}` and `CreatedThread{thread dto.ThreadHeader, post CreatedPost}`. Add the shapes to docs/07 in the same PR, because internal/dto only pins documented shapes.

### Files
internal/service/posts/{writer.go,create.go}, internal/service/threads/create.go, internal/web/api/posts_create.go, internal/dto/dto.go (+fixtures and goldens).

### Tests
- AC1: with fsync `always`, the handler returns; then close the threadcache, reopen from disk with a fresh Cache, and `Reader.One(ordinal)` returns the post. Response fields equal the `threadlog.Result`.
- AC2: three handler tests assert `conflict`, `forbidden`, and `rate-limited` plus `Retry-After`. The slow-mode test uses a fake clock, and a moderator bypasses both lock and slow mode.
- AC3: invalid reply_to and a render failure. After each, `Index.Len()` and the log size on disk are unchanged.
- AC4: create, then GET the thread page; the post's `html` is byte-equal. Run once with a cold render cache so a re-render is compared, not just the cached value. Also a `-race` test with 50 concurrent replies checks for dense ordinals and one event per append.

### Design
Reread docs/02 "Create a post", docs/03 "Write path" steps 1–6, docs/08 "Locking semantics" and slow mode, and docs/07 "Resources" and "Error catalogue".

### Interactions
Consumes TKT-01M29C0WD8 (actor), TKT-01M29C2WWG (structured render), TKT-01M29C5Y2E, TKT-01M29C2WW5 (subscriber to the events published here), and TKT-01M29CRJ50 (slow-mode storage, which is not a recorded dependency). Defines `posts.Writer` for TKT-01M29CA9BP.
Open: slow mode has no storage defined in docs/04; `settings` is global key-value. Also open: `locked` is read from the async `threads` projection, so a reply racing a lock can land. Also open: the title limit and slug derivation are undocumented. Also open: ThreadCreated plus the first Post are two appends; a crash between them leaves a thread with no posts.

### Settled in grooming (2026-09-24)

- Title is at most 200 characters. The slug is an ASCII fold of the title, at most 60 characters.
- ThreadCreated and the first Post are two appends. A crash between them leaves a thread with no posts. Projection and listing hide such threads; do not invent a new record type.
- Slow mode: honour the setting, which is off by default (TKT-01M29P9RS0). Storing it per thread is Phase 2.
