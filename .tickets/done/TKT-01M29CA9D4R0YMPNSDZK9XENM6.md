---
schema: 3
id: TKT-01M29CA9D4R0YMPNSDZK9XENM6
title: Implement POST /render for the composer preview
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29C2WWGF4ERSR2AM2QNEH8C
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-render
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:19:48Z
updated_at: 2026-09-24T21:33:29Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`POST /api/v1/render` takes source and returns html, mentions, reply_to, and quotes. This is why no client-side markdown parser ships and why the preview is exact rather than approximate.

The same endpoint serves the future HTML client, which posts a form and gets a preview page back. Rate limiting matters here more than on most reads, because it renders arbitrary input for anyone with a session.

## Acceptance criteria

- [x] Output is identical to what the post read path returns for the same source
- [x] A member session is required, so it is not an open rendering service
- [x] The response shape is the same structured output the create path uses
- [x] The endpoint sits behind the per-address write limiter like every other mutating route, and its body limit matches the post limit

## Implementation plan

Revised at claim (2026-09-25), after reading internal/web/api, internal/service/threads and internal/web as they stand.

### Approach
1. `POST /api/v1/render {source, thread_id?, reply_to?}` returns `dto.RenderResult` through `threads.RenderDTO` (TKT-01M29C2WWG), with `Cache-Control: no-store`.
2. `threads.Service.Preview(ctx, viewer, PreviewRequest)` holds the rules; the handler parses and writes. The rules:
   - A member is required. A pre_user gets `forbidden`, and anyone else who is not a member gets `unauthenticated`. An inactive account counts as anonymous, the same as `access`.
   - With `thread_id`, the viewer must be able to read the thread, through the same `access` the read endpoints use: hidden gives 404 and so on. Read is enough, because a preview reveals nothing a reader of the thread cannot see; the reply permission is the create path's job.
   - `reply_to` without `thread_id` is `bad-request`. `render.ErrInvalidReplyTo` maps to `validation-failed` in internal/problem's table.
3. One render function serves both the read path and the preview:
   - `postResolver(th)` is the quote resolver `posts()` already builds from the index, factored out.
   - Mentions go through the same `handleMemo`/`handle` lookup, and a failed lookup is an error, not an unresolved mention.
   - `s.renderSource(ctx, th, source, replyTo)` is what TKT-01M29CA9B9's create path should call. CA9B9 is not built yet, so this ticket defines the shared function rather than consuming it.
4. Body limit: nothing new. `web.MaxPostBody` applies to every non-upload route. Parsing is strict: unknown fields, trailing data and a malformed thread id are `bad-request`, and an oversized body is `payload-too-large` via `MaxBytesError`.
5. Rate limit: this is a mutating API path, so the per-address write bucket already covers it. It is proven through the full chain.

### Files
- internal/service/threads/preview.go
- view.go, to factor out the resolver
- internal/problem/domain.go, for the mapping
- internal/web/api/api.go and render.go
- render_test.go in api, which runs through `web.New` with the real rate limit and CSRF middleware
- docs/07 resources table

### Tests
- AC1: a corpus is posted to a thread, then previewed with that `thread_id`, and the `html` is byte-equal to the page's.
  - The corpus includes a known mention, an unknown mention, a quote of a live post, a quote of a deleted post, a quote of a missing ordinal, and markdown.
  - The structured fields equal `RenderDTO(render.RenderWith(...))` with the same context.
- AC2: through `web.New`, anonymous gets 401, pre_user 403, inactive 401 and member 200.
- AC3: the response decodes strictly into `dto.RenderResult`, the type the create path answers with.
- AC4, all through `web.New`:
  - A 64 KiB + 1 Content-Length gets 413.
  - A chunked body over the limit gets 413.
  - The 61st request in a burst gets 429 with `Retry-After`.
  - Form content gets 415.

### Settled in grooming (2026-09-24)

- Accept optional thread_id and reply_to so quote and reply_to rendering matches the create path.
- Rate limiting is the per-address write limiter (criterion rewritten in grooming).

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. It said 'rate limited per session', which contradicts docs/05 and docs/08: members have no per-session write limit, and the per-address ceiling is the mechanism.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T21:33:29Z

Built as the revised plan says.

### How it works
- `threads.Service.Preview` holds the rules.
- `renderSource(ctx, th, source, replyTo)` is the one render context for a post's source: `postResolver(th)`, factored out of the read path, plus the memoized handle lookup, with a failed lookup as an error.
  - **TKT-01M29CA9B9 (create path): render through `renderSource` with the thread you are appending to.** That is what makes AC1 hold for the stored post too. It currently lives in the threads (read) service; move it if the create path gets its own service, but keep one function.
- `render.ErrInvalidReplyTo` maps to `validation-failed` in internal/problem's table.

### Tests
- The AC tests run through `web.New` with the real body limit, the real CSRF middleware, and the real rate limiter on a frozen clock.
- Mutants: 18 hand mutants across preview.go, render.go, api.go, view.go and problem/domain.go, all killed; none of them only by a compile error.
- The GET-to-POST-route mutant first survived because the test only asserted "not 200". It now asserts not-found or method-not-allowed.

### Decisions for the maintainer
- **Read is enough to preview in a thread.** Reply permission is not checked, because a preview reveals nothing a reader cannot see, and posting is the create path's check.
- **A reply to a deleted post is accepted**, inherited from render.Context (see TKT-01M29C2WWG's notes).
- **`GET /api/v1/render` answers 404**, like every wrong-method request under /api/v1 today, because of the API's catch-all. It does not answer 405. This was not changed here.

## Summary

POST /api/v1/render serves dto.RenderResult through threads.Preview. It needs a member (401 anon/inactive, 403 pre-user), takes optional thread_id (read access, quotes resolved, reply_to checked) and reply_to, and parses strictly. It renders through renderSource, the same context the read path uses, and the create path should use it too. Body limit and per-address write limiter proven through web.New. docs/07 updated.
