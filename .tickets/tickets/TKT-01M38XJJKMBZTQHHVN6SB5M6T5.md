---
schema: 3
id: TKT-01M38XJJKMBZTQHHVN6SB5M6T5
title: Serve handle autocomplete for mentions
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29BYYH73ZBC9WTS7HN03PKJ
  - TKT-01M29C5Y3E5SKHFR2SHJ8Y60TP
blocks_on: none
references:
  - ref: design:api
    path: docs/07-api-and-clients.md
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/38xjj-handle-autocomplete
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 6627c5516be2d23f9caf0efad02d0798e65ae24e
  session: null
  claimed_at: 2026-09-24T21:38:34Z
  expires_at: null
archive: null
created_at: 2026-09-24T05:17:53Z
updated_at: 2026-09-24T21:38:34Z
created_by:
  id: agent:claude/phase1-groom
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

The composer's mention autocomplete (TKT-01M29CEE79, Build the composer with
server-rendered preview) needs an endpoint that turns a typed handle prefix
into candidate members. No ticket owned it. TKT-01M29C0WEQ (Add mention
syntax, resolution, and autocomplete data) closed with that criterion
unticked, because at the time there was no HTTP layer and no users table.
docs/07 does not list the route either.

Proposed shape, to be written into docs/07 in the same PR:
`GET /api/v1/users?prefix=ab&limit=8`. It returns handle, display name, and
avatar, drawn from the same resolution rules the renderer applies to
`@handle` (TKT-01M29C0WEQ), so what autocomplete offers is what renders as a
mention. Handles are ASCII (docs/04), so matching a prefix is a plain
case-insensitive index range scan.

## Acceptance criteria

- [ ] A signed-in member gets up to limit active users whose handle starts with the prefix, ordered by handle, and an anonymous viewer gets 401
- [ ] Suggested handles are exactly those the renderer would resolve as mentions, shown by a test that renders each suggestion
- [ ] The route and response shape are documented in docs/07 with a golden test in internal/dto
- [ ] The query uses the handle index, confirmed with EXPLAIN QUERY PLAN in a test

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach

1. Write a sqlc query `UsersByHandlePrefix(prefix, limit)` over `users`, filtered to status = 'active', ordered by handle. Handles are ASCII and lowercased (docs/04), so `handle >= ? AND handle < ?||'{'` is a plain index range scan.
2. Add `service/users.Suggest(ctx, viewer, prefix, limit)`: lowercase the prefix, reject anything outside the handle alphabet with an empty result, and clamp limit to 1..20 (default 8).
3. Add the handler `GET /api/v1/users?prefix=` in internal/web/api. A member session is required; anonymous callers get 401 from internal/problem.
4. Add `dto.UserSuggestion{handle, display_name, avatar_url}` plus a golden file, and write the route into docs/07's resources list.

### Files

internal/store/db/queries/users.sql, internal/service/users, internal/web/api/users.go, internal/dto (new type and golden), docs/07-api-and-clients.md.

### Tests

- AC1: handler test for member, anonymous and suspended users.
- AC2: render `@handle` for each suggestion through internal/render's mention resolver and assert every one resolves.
- AC3: golden JSON.
- AC4: EXPLAIN QUERY PLAN shows the handle index in use.

### Design

docs/07 (JSON API, Resources), docs/04 (users, handle rule), and the resolution rules in TKT-01M29C0WEQ.

### Interactions

Consumes BYYH7 (users schema) and C5Y3E (session and viewer). Consumed by CEE79 (composer autocomplete).
