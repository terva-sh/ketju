---
schema: 3
id: TKT-01M29CCBYGNF7T7R9MFNHW5X60
title: Implement the user profile endpoints
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C801M9WBEAVCCC6AXNZ4Z
blocks_on: none
references:
  - ref: design:account-lifecycle
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:21:20Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

`GET /users/{handle}` and `GET /users/{handle}/posts`, the latter served from the `post_meta` index on (author_id, created_at), which is one of the two reasons that projection table exists.

An anonymized user is the case to get right. Their posts remain, their attribution is suppressed at render time by the `users.anonymized` flag, and their profile must not resurrect the handle or display name that was cleared.

## Acceptance criteria

- [ ] A post history spans forums and excludes posts in forums the viewer cannot read
- [ ] An anonymized user's profile shows the placeholder handle and no prior identity
- [ ] Deleted posts are omitted from a member's view of a history and shown to moderators
- [ ] Paging is cursor-based on created_at

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. `GET /api/v1/users/{handle}` → a new `dto.Profile{user dto.Author, bio_html, created_at, post_count?}`, looked up case-insensitively by handle with a sqlc query. The shape is not pinned in docs/07. Pin the minimal field set there in the same PR, because internal/dto refuses shapes no document fixes.
2. Anonymized users: anonymization rewrites `handle` to `deleted-user-<id>` and clears display_name and avatar (docs/05), so the profile reads straight from the row. When the anonymized flag is set, also blank `bio_source` and suppress any field that could carry prior identity. A lookup by the old handle is `not-found`. If handle-change redirects arrive (TKT-01M29BSPEN), anonymization must not leave a redirect behind.
3. `GET /api/v1/users/{handle}/posts?cursor=` reads `post_meta` through `INDEX(author_id, created_at)`, joined to `threads.forum_id`, and filtered to the viewer's readable forums. That is a set from TKT-01M29C5Y2E, bound into the query as an IN list or temp table. The query is hand-written, since it is cursor-paged.
4. Cursor: opaque `(created_at, thread_id, ordinal)` descending, so equal timestamps do not collide.
5. Deleted posts: excluded (`post_meta.deleted = 0`) unless the viewer can moderate that forum, in which case they are included and marked. Bodies are rendered through the shared post assembler (TKT-01M29CA9AX), which opens each thread involved. Page size is bounded, so the number of opened threads is too.

### Files
internal/service/users/{profile.go,posts.go}, internal/web/api/users.go, internal/dto (`Profile`, `UserPostList` + goldens anon/member/moderator/anonymized).

### Tests
- AC1: seed posts in a public forum, a members forum, and a restricted one. An anon viewer, a member, and a member with a restricted grant each see exactly their readable subset, in time order across forums.
- AC2: anonymize a user by running the lifecycle update or seeding the columns. The old handle is 404. The profile at `deleted-user-<id>` has an empty display_name and avatar, and the raw response contains neither the old handle nor the old name, checked with a substring assertion.
- AC3: a deleted post is absent for a member and present with `deleted:true` for a moderator of that forum.
- AC4: with 3 posts sharing one `created_at`, pages of size 2 return all 3 exactly once. A post inserted mid-pagination causes no duplicate.

### Design
Reread docs/04 "users" and "post_meta", docs/05 "Account lifecycle" (anonymize), docs/07 "Resources" (/users) and "Where the shapes live".

### Interactions
Consumes TKT-01M29BYYJ8 and TKT-01M29C2WW5 (post_meta), TKT-01M29C5Y2E (the readable-forum set), TKT-01M29CKRFS (the anonymize flag), and the TKT-01M29CA9AX assembler.
Open: the `users.anonymized` column appears in docs/05 but not in the docs/04 users column list.
Open: "shown to moderators" does not say whether that means site role or per-forum `moderate`. The plan uses per-forum.
Open: the profile field set is undocumented.

### Settled in grooming (2026-09-24)

- Profile fields: handle, display name, avatar, bio, joined date, post count. 'Moderators' means the site role.
- Add `users.anonymized` to docs/04 if the ticket needs it.
