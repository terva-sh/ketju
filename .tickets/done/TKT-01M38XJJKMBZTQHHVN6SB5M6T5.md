---
schema: 3
id: TKT-01M38XJJKMBZTQHHVN6SB5M6T5
title: Serve handle autocomplete for mentions
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-24T05:17:53Z
updated_at: 2026-09-24T21:50:04Z
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

- [x] A signed-in member gets up to limit active users whose handle starts with the prefix, ordered by handle, and an anonymous viewer gets 401
- [x] Suggested handles are exactly those the renderer would resolve as mentions, shown by a test that renders each suggestion
- [x] The route and response shape are documented in docs/07 with a golden test in internal/dto
- [x] The query uses the handle index, confirmed with EXPLAIN QUERY PLAN in a test

## Implementation plan

Revised at claim (2026-09-25), after reading the users schema, internal/render/mention.go, internal/service/users and internal/web/api.

### Approach
1. sqlc query `UsersByHandlePrefix(lo, hi, lim)` in queries/identity.sql:
   - `handle >= lo AND handle < hi`, with `hi = prefix + "{"` computed in Go. `{` sorts after every handle character.
   - Filtered to `status = 'active'`, ordered by handle, limited.
   - The range compares under the column's NOCASE collation, the same collation as the UNIQUE index, so it is a range search on that index.
2. `users.Service.Suggest(ctx, viewer, prefix, limit)`:
   - It needs a member: anonymous and inactive get `unauthenticated`, and a pre-user gets `forbidden`, the same rule as POST /render.
   - The prefix is lowercased. A prefix that no handle can start with gets an empty list, not an error, because the composer asks while the author is still typing. That covers anything outside `a-z0-9_-`, a first character outside `a-z0-9`, and anything longer than 30.
   - `limit` defaults to 8 and is capped at 20; below 1 or not an integer is `bad-request`. A missing `prefix` is `bad-request`.
3. **The renderer's rule, not the schema's:** a handle ending in `-` is valid in the schema, but the mention parser strips a trailing `-` as sentence punctuation. So `@bob-` mentions `bob`, and `bob-` can never be mentioned. Suggesting it would insert a mention that resolves to somebody else, or to nobody, so the query leaves such handles out. This is exactly AC2's rule. I'm filing a draft for the underlying mismatch, which is a handle-rule/format decision rather than this ticket's.
4. `GET /api/v1/users?prefix=&limit=` returns `dto.UserSuggestions{users: [Author]}`. It reuses `dto.Author`, so the composer can draw a suggestion with the same avatar and name component as a post byline, and the golden is `users.member.json`, also added to the client's dto-contract. docs/07 gets a row.

### Tests
- AC1: members, anonymous, pre-user and inactive viewers; the prefix match itself (case-insensitive, `_` and `-` inside, ordered by handle); the limit, its default and its cap; suspended and pending users excluded.
- AC2: each suggestion for a set of prefixes is inserted as `@handle ` into source and rendered through the same handle lookup the read path uses (the threads service's `renderSource`). Every one must resolve to that exact handle, and a trailing-hyphen user exists in the fixture to prove the exclusion.
- AC3: `users.member.json` golden in internal/dto, plus the TS type and the contract.
- AC4: `EXPLAIN QUERY PLAN` shows `SEARCH users USING INDEX sqlite_autoindex_users_1 (handle>? AND handle<?)`, not a SCAN.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T21:50:04Z

Built as the revised plan says, with two simplifications found by mutation testing.

### Simplifications
- **No Go-side prefix validation.** Every handle character sorts below `{`, so the range `[p, p+"{")` holds exactly the handles that start with `p`, for any `p`. The alphabet, length and first-character checks were equivalent mutants, and are gone.
- **No Go-side lowercasing.** The range compares under the column's NOCASE collation, which folds ASCII only. `strings.ToLower` would have been wrong as well as redundant: it folds the Kelvin sign U+212A to `k`. A test pins this, and the ToLower mutant is killed.

### Trailing-`-` handles
A handle ending in `-` is left out of the suggestions. The schema allows it, but the mention parser reads a trailing `-` as punctuation, so `@bob-` mentions `bob`. Filed draft TKT-01M3ANX1NF3XWCJBS3DH9QZNFC (Handles ending in '-' can never be mentioned) for the underlying mismatch, which is a handle-rule/format decision. The AC2 test proves `@al-` resolves to nobody, so that exclusion fails loudly if the parser changes.

### AC2 scope
Every suggestion resolves as a mention of exactly that user; the test round-trips each one through POST /render. The converse is deliberately not true. A suspended or pending user still resolves as a mention (GetUserByHandle has no status filter), but AC1 limits suggestions to active users.

### Tests and mutants
- Mutants: 17 hand mutants across suggest.go, users.go, the generated query, the query source (collation and `lower()` rewrites, killed by the plan test) and the route. All are killed except removing `ORDER BY handle`, which is equivalent while the plan is an index search in handle order. It is kept because SQLite promises no order without it.
- `UserSuggestions` reuses `dto.Author`. The golden is `users.member.json`, and the client has the type and contract entry. The full client suite ran: 530 tests.

## Summary

GET /api/v1/users?prefix=&limit= returns dto.UserSuggestions{users: [Author]}: up to limit (default 8, max 20) active members by case-insensitive handle prefix, in handle order, excluding handles ending in '-' (unmentionable; draft TKT-01M3ANX1NF filed). Member session required. It is a NOCASE range search on the handle index, proven by EXPLAIN QUERY PLAN. Every suggestion is round-tripped through POST /render as a mention. docs/07, golden and client contract updated.
