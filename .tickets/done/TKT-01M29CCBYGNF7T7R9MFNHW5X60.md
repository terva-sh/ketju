---
schema: 3
id: TKT-01M29CCBYGNF7T7R9MFNHW5X60
title: Implement the user profile endpoints
type: task
status: done
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
  - TKT-01M3KJJNDC77K3VH47TKGN9VDH
blocks_on: none
references:
  - ref: design:account-lifecycle
    path: docs/05-identity-and-accounts.md
  - ref: pr:108
    path: null
  - ref: pr:113
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-28T14:21:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`GET /users/{handle}` and `GET /users/{handle}/posts`, the latter served from the `post_meta` index on (author_id, created_at), which is one of the two reasons that projection table exists.

An anonymized user is the case to get right. Their posts remain, their attribution is suppressed at render time by the `users.anonymized` flag, and their profile must not resurrect the handle or display name that was cleared.

## Acceptance criteria

- [x] A post history spans forums and excludes posts in forums the viewer cannot read
- [x] An anonymized user's profile shows the placeholder handle and no prior identity
- [x] Deleted posts are omitted from a member's view of a history and shown to moderators
- [x] Paging is cursor-based on created_at

## Implementation plan

Revised at claim (2026-09-28), from the code on main at a784672.

### Approach
1. `GET /api/v1/users/{handle}` → `dto.Profile{user, bio_html, joined_at, post_count, status}` from `threads.Service.Profile` (moved from the users service after review 1162: the count is the history's and needs the thread cache). `GET /api/v1/users/{handle}/posts?cursor=` → `dto.UserPosts{posts: [{thread, forum, post}], next_cursor}` from `threads.Service.UserPosts`, which owns the post assembler and the thread cache. Both shapes pinned in docs/07's `/users/{handle}` row and in goldens.
2. Who is served which account is one rule both services call, `member.Account`: `member.Require` first (anonymous unauthenticated, pre-user forbidden, before anything is looked up), then `GetUserByHandle` (NOCASE), then `domain.Viewer.MaySeeAccount`, which hides a pending account from everyone but staff (`domain.Viewer.Staff`: active moderator or admin, the site role). Unknown and hidden pending are the same not-found.
3. Anonymized: no `users.anonymized` column (anonymize is Phase 2, TKT-01M29CKRFS), so `domain.Anonymized` reads the reserved `deleted-user` prefix, which registration refuses to anyone else; identity's reserved prefix now names the same constant. `member.Author` blanks display name and avatar for one and the profile withholds the bio, so a row caught halfway cannot bring any of them back. docs/04 is not changed: the ticket does not need the column.
4. Readable forums: `perm.ReadableForums`, changed to ask `CanEach` once rather than `Can` per forum. Hand-written view queries in `views/userposts.go` bind the set as one json_each parameter, both `INDEXED BY post_meta_author`: `UserPosts` (a keyset batch of candidates, deleted or not) and `UserThreadPosts` (per thread: projected length, non-deleted count, ordinals).
5. The log's index, not post_meta, says whether a post is deleted (review 1162). History: candidates from post_meta, grouped by thread, each thread opened once, `historyShows` on the index entry; further batches are read until the page holds a page and one more or the history ends, and the cursor is the last post shown. Rendered by the thread page's assembler, `moderator` = staff, so staff see a deleted post's body and its author sees the tombstone their thread page shows them. Hidden-by-mod is a deleted post here, as on the thread page. Moved posts are left out. Quarantined threads are skipped. Count: for each thread, `threadcache.LogSize` (a stat) against `log_length_projected`; a caught-up thread counts post_meta's non-deleted rows, a trailing one is counted from its index over the same candidates, and a quarantined one counts nothing; so the count is exactly a member's history.
6. Cursor: base64url of `at|thread|ordinal`, canonical spelling only, no account in it (no response exposes a user id). Page size 25; `Service.UserPostsPage` lets tests page by two.
7. Both answers `no-store`; neither uses the response cache, so its authors digest is untouched.

### Tests
- api `profile_test.go`: every viewer class (anon, pre-user, inactive, author, member, grantee of a restricted forum, moderator, admin) against active, suspended, deactivated, pending and unknown accounts on both endpoints; count per viewer; history per viewer with deleted/visible marks and bodies; log-over-projection; full member pages; anonymized raw-body search; keyset paging with three tied posts and a post written mid-paging; cursor refusals.
- threads `userposts_test.go`: `historyShows` over every flag state, cursor round trip.
- dto goldens: profile.member/moderator, profile-anonymized.member, user-posts.member/moderator, user-posts-own.member.

### Settled in grooming (2026-09-24)

- Profile fields: handle, display name, avatar, bio, joined date, post count. 'Moderators' means the site role.
- Add `users.anonymized` to docs/04 if the ticket needs it.

### Settled in grooming (maintainer's decision, 2026-09-28, TKT-01M3KJJNDC)

- Anonymous access, "Members only": a member session is required for GET /users/{handle} and /users/{handle}/posts, as for GET /users?prefix=: anonymous → unauthenticated, pre-user → forbidden.
- Post count, "Visible, non-deleted posts": non-deleted posts in forums this viewer can read, exactly what their history view lists. It varies by viewer and leaks nothing about restricted forums.
- Read with the next item (review 1161 on PR #112): a deleted post never counts, for any viewer. The count equals what a member's history lists; an author's, moderator's or admin's history also shows deleted posts, marked deleted, which the count leaves out.
- Non-active accounts, "Hide pending; serve the rest": pending accounts are not-found except to moderators and admins. Suspended and deactivated profiles are served, since docs/05 keeps their posts with the handle shown; their status is shown only to moderators and admins.
- Author's own deleted posts, "Yes, marked deleted": an author sees their own deleted posts, marked as deleted, in their own history (PR #95 lets only the author restore a post). Other members do not; moderators and admins follow criterion 3.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:37:32Z

Blocked at planning, before any code, on who-can-see-what questions the design leaves open. TKT-01M3KJJNDC (Decide who may see a user profile and what its post count counts) lists them with options. In short:

1. docs/07 names no session requirement for `/users/{handle}` or its `/posts`. The sibling `GET /users?prefix=` is members-only. Serving a profile to an anonymous viewer shows bio, joined date and post count, and confirms that a handle exists, which public threads do not already show.
2. "Post count" does not say whether it counts all of the user's posts (activity in restricted forums shows through) or only the posts this viewer may see, and does not say whether deleted posts count.
3. Nothing says whether a pending, suspended or deactivated account's profile is served, or to whom.

docs/12 "Gaps the design leaves open" puts each of these with a person. The run brief says the same for a field whose visibility to a class of viewer is not stated.

Facts found while reading, for whoever picks this up:
- `users.anonymized` does not exist in the schema, and anonymize is TKT-01M29CKRFS, a Phase 2 draft. Registration reserves the `deleted-user` prefix (docs/04, identity.CheckHandle), so only anonymization can produce that handle, and a profile can recognize an anonymized account from its handle without a migration. The anonymized profile should also withhold `bio_source`: docs/05 does not say anonymize clears it, and criterion 2 requires "no prior identity".
- The endpoints are read-only, and profile editing is not in this ticket, so this work does not affect the response cache. `threads.authorsDigest` hashes the whole `dto.Author` (handle, display name, avatar, role, kind), so a later edit or anonymization is already reflected on a cached thread page. Bio does not appear on thread pages.
- "Moderators" means the site role (settled). docs/04 gives `role = 'moderator'` `moderate` on every forum it can see, and an admin passes every check, so the site role and the per-forum check agree for those two roles.
- `post_meta` has `INDEX (author_id, created_at)`. The history query is keyset on `(created_at, thread_id, ordinal)` descending, joined to `threads.forum_id` and filtered to the viewer's readable forums (internal/perm).

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:37:37Z

in-progress to blocked: Who may see a profile (anonymous viewers, non-active accounts) and what post count counts are undecided; see TKT-01M3KJJNDC

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:48:46Z

Terva review 1154 (run be15eb74-0959-4884-8c1d-bb0ae24d29a1, Actions run #609), head a042bd20e46a35494737820affa50347640f1421, base de904cb55ab88426e91eb9c84c66c9a49bbb6ee1: one low finding, 'Transition note records the wrong previous status'. Answered, not changed. The PR's net diff shows ready to blocked, but on the branch the ticket went ready, claimed, in-progress, blocked, released, each as a separate git ticket write in commit f7a0081. git ticket generated the in-progress-to-blocked note from the status the ticket actually had at that moment. The claim was set and then released inside the same commit, which is why base and head both read claim: null. The history is accurate.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:09:07Z

Built to the maintainer's 2026-09-28 decision. Evidence and choices:

- AC1: TestHistoryByViewer and TestPostCountByViewer. alice has posts in a public, a members-only and a restricted forum; a member, the author, a moderator (no row for the restricted forum, so it is hidden from him), a grantee and an admin each see exactly their readable subset, newest first across forums.
- AC2: TestAnonymizedProfile. The anonymized row still holds an old display name, avatar and bio; for four viewer classes the raw profile and history bodies contain none of them, and the author block is the placeholder handle alone. The old handle is not-found because no account has it (TestProfileWhoMaySee covers an unknown handle).
- AC3: TestHistoryByViewer. A deleted post is absent for another member, marked deleted without a body for its author, and marked deleted with its body for a moderator and an admin. "Moderators" is the site role, as settled.
- AC4: TestHistoryPaging. Three posts share one created_at across two threads; pages of two return each once, in (thread, ordinal) descending order, and a post written between pages does not reappear.

Choices settled here as internals, written into docs/07: the history cursor names no account, since no response exposes a user id; both answers are no-store; a bio's quote links nowhere; a moved post is left out; a quarantined thread's posts are skipped rather than failing the whole history. One visibility question came up and was answered from existing rules rather than decided: a post hidden by a moderator is treated as deleted in the history, as the thread page and dto.Post.Deleted already treat it (docs/08: hiding is a Delete variant). Nothing sets that flag in this build.

Mutations tried against the new tests (20), each reverted: pending served to all, pending hidden from staff, status shown to all, member not required, count including deleted, count ignoring forums, history ignoring forums, no index filter, author not recognised, staff without bodies, author given bodies, no anonymized scrub, anonymized bio shown, inclusive cursor, cursor ignoring ties, non-canonical cursor accepted, bio quotes linked, ReadableForums ignoring perm: all caught. "Members' query includes deleted rows" survived at first (the index filter masks it) and is now caught by TestMemberPagesAreFull. "ORDER BY created_at only" survives: under INDEXED BY post_meta_author the index already carries (thread_id, ordinal), so the order is the same and no test can tell.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:18:24Z

Terva review 1162 (run 3dc9bae0-5d30-4c2f-b591-155d0fd295d7, Actions run #651), head a7ec5581c31f5d11a1a8141d74644fff86cf7f05, base 03a0ea63f76ea95c39e90f34f90bff41bfddc867: three medium findings. All three were fixed by changing the flagged behaviour. None was answered by argument.

1. "Restored posts can be excluded before the log is checked". Fixed: the history query no longer filters on post_meta.deleted for anyone. Every candidate's index entry decides. TestRestoreBeforeTheProjection restores a post in the log without projecting it, and a member sees and counts it.
2. "Profile count can disagree with the visible history". Fixed: the count uses the same candidates and the same source as the history. For each thread, a stat of the log is compared with threads.log_length_projected. A caught-up thread counts post_meta's non-deleted rows. A trailing thread is counted from its index, and a quarantined thread counts nothing, as the history shows nothing from it. The count moved to threads.Service.Profile because it needs the thread cache. TestHistoryTrustsTheLogOverTheProjection and TestRestoreBeforeTheProjection now check the count in both directions. TestCountTakesTheProjectionWhenCaughtUp holds the fast path's premise that a projected thread's log size equals log_length_projected.
3. "Filtering after pagination can return an empty page". Fixed: further batches are read until the page holds a full page and one more post, or the history ends. The cursor is the last post shown, so a page is short only at the end, and a history that ends has no cursor. TestPagesFillPastHiddenPosts runs the reviewer's example: a page of one whose first candidate is deleted in the log only.

Mutations rerun against the reworked code: 23 in all. 22 are caught, and "count trusts a stale projection" is now caught too. "Count every thread from its index" survives: it changes only the cost, not the answer, and the premise test guards the fast path.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T14:21:54Z

CI run 656 on efa1444 failed at 'client tests': web/client's contract test requires every golden in internal/dto/testdata/golden to be imported by web/client/src/api/dto-contract.ts and held to a type there. Added Profile, UserPosts and UserPost to web/client/src/api/dto.ts and the six goldens to the contract. Type-only, so go generate ./internal/web/spa leaves dist unchanged; tsc, 875 client tests and the budget pass locally. Profile.status is string | null, as Author.role is string, because a JSON import widens its literals.

## Summary

GET /users/{handle} (dto.Profile) and GET /users/{handle}/posts (dto.UserPosts) are served to members only, by the rule in member.Account; pending accounts are not-found except to moderators and admins, status is shown to staff only, the count is the viewer's visible non-deleted posts, and the history is keyset-paged from post_meta with the log's index deciding what each viewer sees. Anonymized accounts are recognised by the reserved deleted-user prefix. Shapes and rules in docs/07; all four criteria met.
