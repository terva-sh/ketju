---
schema: 3
id: TKT-01M29CA9D4R0YMPNSDZK9XENM6
title: Implement POST /render for the composer preview
type: task
status: draft
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
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`POST /api/v1/render` takes source and returns html, mentions, reply_to, and quotes. This is why no client-side markdown parser ships and why the preview is exact rather than approximate.

The same endpoint serves the future HTML client, which posts a form and gets a preview page back. Rate limiting matters here more than on most reads, because it renders arbitrary input for anyone with a session.

## Acceptance criteria

- [ ] Output is identical to what the post read path returns for the same source
- [ ] A member session is required, so it is not an open rendering service
- [ ] The response shape is the same structured output the create path uses
- [ ] The endpoint sits behind the per-address write limiter like every other mutating route, and its body limit matches the post limit

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `POST /api/v1/render {source}` → the structured render result DTO that TKT-01M29C2WWG defines (`{html, mentions, reply_to, quotes}`; internal/dto/dto.go already defers this shape to that ticket). This ticket only serves it.
2. Require `viewer.Member()`: anon → `unauthenticated`, pre_user → `forbidden`. The check sits in the service, not the handler, per docs/07 "Neither contains authorization".
3. Call the same function the create path uses (`posts.RenderForWrite(ctx, viewer, threadCtx, source)` from TKT-01M29CA9B9) so preview and stored output cannot drift. The mentions resolver is users-backed. With no thread context, the quote resolver is nil, which `render.Context` documents as optimistic.
4. Body limit: nothing new. `web.MaxPostBody` (64 KB) already applies to every non-upload route in `limitBody`, and `render.MaxSource` is the backstop.
5. Rate limit: `POST /api/v1/render` is a mutating API path, so the existing per-address write bucket (`writeRate`, 60 burst, 1 per 0.5 s, in internal/web/ratelimit.go) already covers it. See the open item on "per session".

### Files
internal/web/api/render.go, internal/service/posts/render.go (shared with TKT-01M29CA9B9). DTO and golden from TKT-01M29C2WWG.

### Tests
- AC1: for a corpus of sources (the internal/render/testdata inputs, plus mentions, quotes, and a reply_to), POST /render; then create the post and GET it. The `html` is byte-equal, and the structured fields equal what the create path recorded.
- AC2: a 64 KB + 1 body is `payload-too-large`, and a Content-Length lie is caught by MaxBytesReader. Driving the write bucket to empty returns `rate-limited` with `Retry-After`.
- AC3: anon gets 401 and pre_user gets 403, as handler tests through the full `web.New` chain.
- AC4: a golden test that the render response and the create response's render block marshal to the same JSON shape (same type).

### Design
Reread docs/06 "Composer", docs/07 "Resources" (/render) and "Request limits", and docs/08 "The transport-level ceiling".

### Interactions
Consumes TKT-01M29C2WWG (result type), TKT-01M29C5Y3S (chain, done), TKT-01M29C5Y3E (Viewer), and the shared render function from TKT-01M29CA9B9. Consumed by TKT-01M29CEE79 (composer).
Open: the AC's "rate limited per session" contradicts docs/05 and docs/08, both of which settle that members get no per-session limit. The per-address ceiling already applies.
Open: the request carries no thread id, so a quote of a deleted or nonexistent post, and reply_to validation, can differ from the stored result. Exactness as the AC states it needs an optional `thread_id` in the request.

### Settled in grooming (2026-09-24)

- Accept optional thread_id and reply_to so quote and reply_to rendering matches the create path.
- Rate limiting is the per-address write limiter (criterion rewritten in grooming).

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:17:38Z

Criterion rewritten during Phase 1 grooming (2026-09-24). It moves to the end of the list. It said 'rate limited per session', which contradicts docs/05 and docs/08: members have no per-session write limit, and the per-address ceiling is the mechanism.
