---
schema: 3
id: TKT-01M3KJJNDC77K3VH47TKGN9VDH
title: Decide who may see a user profile and what its post count counts
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - effort:S
  - decision
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T08:37:20Z
updated_at: 2026-09-28T08:37:20Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

TKT-01M29CCBYG (Implement the user profile endpoints) is blocked on three questions about who may see what. docs/04, docs/05, docs/07 and the ticket's settled items leave them open, and docs/12 "Gaps the design leaves open" says a question about who can see what goes to a person.

Settled already, and not reopened here: the profile's fields are handle, display name, avatar, bio, joined date and post count. "Moderators" in the deleted-posts criterion means the site role. An anonymized profile shows the placeholder handle and no prior identity.

### 1. May an anonymous viewer read a profile and its post history?

docs/07 lists `GET /users/{handle}` and `/users/{handle}/posts` as "profiles" and names no session requirement. The endpoints beside it state one when they have one. The only other `/users` read, `GET /users?prefix=`, requires a member session, which suggests the design treats listing accounts as a members' matter. Author blocks (handle, display name, avatar, role, kind) are already public on public-forum threads. A profile would add bio, joined date and post count. It would also confirm that a handle exists for a member who has only ever posted in members-only forums.

Options:
- (a) Profiles and histories are open to everyone. The history filters by forum readability, so an anonymous viewer sees public-forum posts only.
- (b) A member session is required, as for `GET /users?prefix=`: anonymous is `unauthenticated` and pre-user is `forbidden`.
- (c) The profile is open, but bio and post count are for members only.

### 2. What does "post count" count?

Candidates:
- (a) Every post the user wrote. This shows activity in restricted forums to viewers who cannot read them.
- (b) The posts this viewer's history would list. It varies by viewer and gives no restricted-forum signal.
- (c) Non-deleted posts in forums the viewer can read.

Also decide whether deleted posts count, and whether a moderator's count differs from a member's.

### 3. Is the profile of a non-active account served?

Status is `pending`, `suspended` or `deactivated`. docs/05 says a deactivated user's posts are "kept with handle shown" and a suspended user's posts are "optionally hidden" (Phase 2). Neither says whether the profile page is served, or to whom. docs/04 evaluates a non-active viewer as anonymous, but that is about viewers, not the account being viewed.

Options:
- (a) Served like any other account.
- (b) `not-found` to everyone but moderators and admins.
- (c) Served, with the status shown.

### Smaller point to confirm with the above

Criterion 3 says deleted posts are omitted from "a member's view". Does that include the author viewing their own history?

### Done when

The answers are written into docs/07 beside the `/users/{handle}` row (and docs/05 for question 3), and TKT-01M29CCBYG's settled items carry them.

## Acceptance criteria

- [ ] Anonymous access to /users/{handle} and /users/{handle}/posts is decided and written into docs/07
- [ ] What the profile's post count counts is decided and written into docs/07
- [ ] Whether a pending, suspended or deactivated account's profile is served, and to whom, is decided and written into docs/05 or docs/07
- [ ] TKT-01M29CCBYG's settled items carry the answers
