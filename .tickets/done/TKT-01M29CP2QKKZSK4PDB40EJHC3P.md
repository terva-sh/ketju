---
schema: 3
id: TKT-01M29CP2QKKZSK4PDB40EJHC3P
title: Implement user suspension from the moderator tools
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRFSDCGKH605X8Y4RT3C
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
  - TKT-01M29CXK1X0EF4CPPN11KVGC2Z
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
  - ref: pr:169
    path: null
  - ref: pr:172
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T22:57:41Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2qk
  name: ""
extensions: {}
---

## Description

Suspend and unsuspend with a reason and a duration, from the moderator UI, with the option to hide the user's posts.

docs/08 rules out shadow banning explicitly: a suspension carries a reason the member can read. Deceiving a member of a small community about whether they are still in it is worse than removing them.

## Acceptance criteria

- [x] Suspension revokes sessions immediately and shows the reason at the next login attempt
- [x] Every suspension and unsuspension is in audit_log
- [x] A suspension with an end time is lifted by the SuspensionExpiry job within a minute of it, audited with no actor and via expiry, and a suspension with no end time lasts until lifted (TestSuspensionExpires)
- [x] hide_posts appends a hidden-by-mod Delete carrying the suspension's audit id to each of the user's visible posts, and restoring appends a Restore to exactly those and leaves posts hidden for other reasons hidden (TestHidePostsIsReversible)
- [x] A suspended user cannot obtain a session by any path: IssueUserWith refuses them and the chokepoint's caller lists are unchanged (TestChokepointRefusesSuspended, TestOneDoorIntoSessions)
- [x] Moderators suspend members, admins suspend anyone but themselves, and suspending the last active admin is conflict, tested as a table (TestSuspendAuthority)
- [x] When a suspension expires, the posts it hid stay hidden until a moderator restores them (TestSuspensionExpires)

## Implementation plan

### Settled in grooming

- **Maintainer decision 2026-09-29, who may suspend whom:** moderators suspend members, and admins suspend anyone except themselves. The last active admin cannot be suspended, deactivated, anonymized or demoted. This supersedes option (a) of the grooming's open decision, which would have stopped admins suspending admins, and it matches the lifecycle ticket TKT-01M29CKRFS. A note on this ticket records it.
- **Maintainer decision 2026-09-29, expiry:** when a suspension expires, the posts it hid stay hidden until a moderator restores them. Hiding was a separate judgement about the posts, and the expiry job should not undo it unreviewed. A note on this ticket records it.
- A suspension carries a reason the member can read. There is no shadow banning (docs/08 "What is intentionally missing").
- Suspend and unsuspend are moderator actions, recorded in `audit_log` and `users.status` (docs/08 "Primitives").
- **The lifecycle is TKT-01M29CKRFS**, a dependency planned separately. It owns the `users` columns for the reason and end time, the transitions, revoking sessions (and, through TKT-01M29P9RSW, an agent's credential) in the transaction that changes the status, and the "account unavailable" page that shows the reason and the end date (docs/05 "Login flow", "Account lifecycle"). This ticket calls those transitions and does not re-implement any of them.
- The end date is optional. docs/05 says the page shows the reason "and any end date", so a suspension with none lasts until a moderator lifts it.
- Posts are optionally hidden (docs/05 "Account lifecycle").
- A suspended account's profile and posts are still served, with its status shown only to staff (docs/05, decided 2026-09-28, TKT-01M3KJJNDC).
- A moderator's hide is `hidden-by-mod`, and only a moderator restores it (docs/08, decided 2026-09-28, TKT-01M29CRJ5J).
- A body map may gain an optional key without a format bump. An older reader ignores it (docs/03 "Record types", the `Edit.mentions` precedent, docs/10 Q31). No record type is claimed.
- **The session chokepoint.** `session.IssueUserWith` is the only way a session is issued, and it refuses any status but `active` inside the inserting transaction (`internal/auth/session/issue.go`). A suspended user therefore cannot sign in by any path, and this ticket adds no path that issues a session.
- The gate, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The suspension controls sit in the users screen (TKT-01M29CXK1X), which moderators may open read-only.

Engineering choices made here, each with the alternative that lost:
- **Hiding posts writes the log.** Each of the user's visible posts gets a moderator `Delete` (`hidden-by-mod`) whose body carries a new optional key, `suspension`: the id of the suspension's audit row. Everything that already understands a moderator's hide then applies unchanged: tombstones, the author's view, forum totals, profile counts, and the response cache, whose key moves with the log length. The hide also survives a database rebuild, as the epic requires of thread-scoped actions. A users-level "posts hidden" flag lost. Every counting and rendering path would need to learn it, docs/07's forum totals and profile counts would disagree with the thread page, and the response cache's authors digest hashes `dto.Author`, which would not carry it.
- **Restore is exact.** Unsuspending with `restore_posts`, or the standalone restore, appends a `Restore` only to posts whose latest `Delete` carries this user's suspension id, so a post a moderator hid for its own sake stays hidden.
- **Status first, then the log.** The status transaction (TKT-01M29CKRFS) commits first: sessions end and the audit row is written. Then the hides are appended thread by thread. A database transaction cannot span the logs. A failure part-way returns an error naming how many posts were hidden, and `POST …/posts/hide` is idempotent and finishes the job, because already-hidden posts are refused by the actor's flag precondition and skipped.
- **Expiry is a job, not a lazy check.** `jobs.SuspensionExpiry` runs every minute and lifts suspensions whose end time has passed, through the same unsuspend transition, with `audit.System("expiry")`, which writes a NULL actor and `"via": "expiry"` (TKT-01M3NRYCP5's helper, which gained the system actor after Terva review 1349 on PR #140). A lazy check lost because every status read would then have to consult the end time: the chokepoint, `Session.Viewer`, `MaySeeAccount` and perm. `users.status` would also stop being the truth.

### Approach (revised at implementation, 2026-09-29, against main at 796641c)

TKT-01M29CKRFS landed the lifecycle: `perm.ChangeStatus`, `account.Service.Suspend`/`Unsuspend` with `mayActOn` (the maintainer's rule, asked in the transaction), the last-admin check, the suspended sign-in page, and the `/suspend` and `/unsuspend` routes. Reading the code left this for the ticket:

1. **`perm.ChangeStatusID`** returns the audit row's id; `ChangeStatus` wraps it. `account.Service.Suspend` uses it.
2. **The log.** `threadlog.DeleteBody.Suspension uint64` (`suspension`, omitempty), recorded in docs/03 beside `by_moderator`. `threadlog.Append.AtLength int64` is a new in-memory precondition, not a format change: the record must start at that log length, or the append fails with `ErrLogMoved` and writes nothing. `Reader.Removals(from, upTo, fn)` walks the Delete and Restore frames.
3. **`internal/service/threads/suspension.go`**: `HidePostsOf` and `RestorePostsOf`. The threads are the author's, from `post_meta` and `threads.author_id`. Each thread is admitted through `access`, as any moderator action is, and skipped unless the viewer may moderate its forum. So a moderator hides nothing in a forum they cannot moderate or see, and the count reveals nothing about one. Archived threads are skipped, because they are read-only.
   - A hide appends `Delete{Reason: "suspension", ByModerator: true, Suspension: id}` with `Refuse: gone` to each of the author's visible posts. It then projects the thread, so the projection's `post.hidden` rows exist. Those rows gain `"suspension"`.
   - A restore walks the log up to a length L. It appends `Restore{Reason: "unsuspended"}` with `AtLength: L`, `Require: hidden-by-mod` and `Refuse: moved` to each post whose Delete in force carries one of the account's suspension ids. If another record lands first, it walks the new frames and decides again. A post a moderator hid or restored in the meantime is therefore judged on its latest record.
4. **`account.Service`**:
   - `Suspend(..., hidePosts)` and `Unsuspend(..., restorePosts)`.
   - `HidePosts` needs a suspended account and uses the current suspension, the newest `user.suspended` row. `RestorePosts` takes the ids of every `user.suspended` row about the account.
   - All of these are on `mayActOn`. They write `user.posts_hidden {suspension, posts}` or `user.posts_restored {posts}` when a post changed.
   - `ExpireSuspensions` lifts every suspension whose `status_until` has passed, through `perm.ChangeStatus` with `audit.System("expiry")`. Its `Allow` checks again, in the transaction, that the suspension is still due, so a new suspension made in between is not lifted.
5. **`jobs.SuspensionExpiry`** runs every minute and is added to `serve.go`'s background list.
6. **Routes.**
   - `/suspend` takes `hide_posts`, and `/unsuspend` takes an optional `{restore_posts}`.
   - `POST /admin/users/{handle}/posts/hide` and `/posts/restore` are new, behind the staff gate.
   - All of them answer 204 `no-store`, as the lifecycle routes already do. The count goes in the audit row, so there is no new DTO and the client lane's dist is untouched.
7. **Queries**: `ThreadsWithPostsBy`, `SuspensionAuditIDs` and `ExpiredSuspensions`.
8. **Docs**: docs/03 (the key), docs/04 (the audit actions), docs/07 (the routes) and docs/08 (a Suspension paragraph).
9. **The client, step 7 of the groomed plan,** is not in this lane, because the client lane holds the dist. The UI controls are filed as a draft.

### Tests

- `threadlog`: `TestDeleteSuspensionKeyRoundTrips`, `TestAppendAtLength` and `TestRemovals`.
- `account`: `TestSuspendAuthority` gains two table rows: the last active admin, reached through a stale admin viewer, is `conflict`, and so is an account that is already suspended. It also gains the hide and restore authority.
- `session`: `TestChokepointRefusesSuspended`, for IssueUser and IssueUserWith. `TestOneDoorIntoSessions` passes with its lists unchanged.
- `api`: `TestSuspendEndsSessionsAtOnce`, `TestHidePostsIsReversible`, `TestHidePostsIsIdempotent`, `TestSuspensionHideMovesTheCacheKey`, `TestSuspensionAudited`, `TestSuspensionExpires` and `TestHideSkipsForumsTheModeratorCannotModerate`.

### Out of scope

- The lifecycle columns, transitions and sign-in page (TKT-01M29CKRFS).
- Deactivation and anonymization.
- Revoking an agent's credential on suspension (TKT-01M29P9RSW adds it to the lifecycle transition).
- Suspending from the queue, which links here instead.
- Editing the reason or end time of a current suspension: lift it and suspend again.
- The suspension controls in the admin users screen (client lane; filed as a draft).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Two open decisions for the maintainer (grooming, 2026-09-29). 1. Who may suspend whom: (a) only a role strictly below the actor's, never oneself; (b) any staff member may suspend anyone but themselves; (c) as (a), and admins may also suspend admins. Recommended: (a). 2. On expiry, do the posts hidden with the suspension come back: (a) no, a moderator restores them; (b) yes. Recommended: (a). The plan builds both recommendations. See the plan's Open decision.

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Maintainer decisions 2026-09-29, superseding the open decisions in the earlier note. (1) Moderators suspend members, and admins suspend anyone except themselves. The last active admin cannot be suspended, deactivated, anonymized or demoted. This supersedes option (a) and matches TKT-01M29CKRFS. (2) When a suspension expires, the posts it hid stay hidden until a moderator restores them.

**agent:claude-code/phase2-run-cp2qk** at 2026-09-29T22:33:46Z

Implementation decisions (2026-09-29), with the alternatives that lost:

- **Routes answer 204.** The groomed plan had the routes answer `dto.AdminUserDetail`. They answer 204 `no-store`, as TKT-01M29CKRFS's `/suspend` and `/unsuspend` already did, and the counts go in `user.posts_hidden` and `user.posts_restored`. A new response type would have meant a dto.ts change in the server lane while the client lane holds the dist. The UI draft TKT-01M3QMRE47 (Add the suspension controls to the admin users screen) can add a count or the reason and end to `AdminUserDetail` if the screen needs them.
- **A hide or restore touches only threads the caller may moderate.** This is the moderate permission, checked per thread through `threads.access`, as a moderator's own delete of the post would be. A moderator's hide passes over a restricted forum they cannot see, and the count excludes it, so the answer discloses nothing about that forum. An admin reaches every forum. Hiding everywhere lost, because it would act in a forum the moderator has no grant in and leak a post count from it.
- **The restore is exact under concurrency.** `threadlog.Append.AtLength` is a new in-memory precondition, not a format change: the Restore is written only at the log length the walk read to. Without it, a moderator's restore and re-hide landing between the walk and the append could be undone. Reading the audit rows' `suspension` detail instead of the log lost, because the log is the truth and survives a database rebuild.
- **Archived threads are passed over.** Archived is read-only to everyone (docs/08).
- **A quarantined thread is passed over.** Every other thread is finished, and the answer is `unavailable`, so running it again finishes the job.
- **A reply still in flight is not caught.** The hide finds the author's threads in `post_meta` and `threads.author_id`, which the projection writes. A reply the member sent in the milliseconds before the suspension, and not yet projected when the hide reads, is not found. `POST …/posts/hide` catches it. The hide is idempotent, and docs/07 says to run it again.
- **Expiry reads the end again in the transaction** (`Allow`), so a suspension lifted and replaced between the list and the change stands.
- **docs/11 counts** were not refreshed for the new draft, as earlier Phase 2 run PRs also left them. docs/12 refreshes them when the gate closes.

**agent:claude-code/phase2-run-cp2qk** at 2026-09-29T22:54:11Z

Terva review 1495 (run 15741, Actions #1066), head 141ee0a3581adaf3145aaa042cf8b3e4904f6936, base 1dd6ed90c253e04f8906a966943dde764b48e0c6: two medium findings, both fixed. (1) 'Continue past quarantined threads returned by With': a thread that fails to open now counts as unreached like a quarantined one, every other thread is finished, and the answer is unavailable. Pinned by TestHideGoesPastAThreadItCannotOpen. (2) 'Recheck forum authority when applying the thread change' (security: access to posts): the moderator is now admitted under the thread. The projection is brought up to the log's length L first, so an unprojected ThreadMoved is in the forum judged. Every record is appended at the length the previous one left, starting from L (AtLength), so a record landing in between, a move included, sends the thread back to be decided again. Pinned by TestHideJudgesTheForumTheLogSays and TestRestoreDecidesAgainWhenTheLogMoves. Mutations were tried: without the projection before admitting, the forum test fails; with an unopened thread stopping the walk, the open test fails; without AtLength on the restore, the interleaving test fails.

## Summary

Suspension is complete on the server; the UI is draft TKT-01M3QMRE47 (Add the suspension controls to the admin users screen). POST /admin/users/{handle}/suspend takes hide_posts, and /unsuspend takes an optional {restore_posts}. The new /posts/hide and /posts/restore routes sit behind the staff gate. All four answer 204 and use the maintainer's authority rule (mayActOn). A hide gives each of the account's visible posts a moderator's Delete carrying suspension, a new optional key (docs/03) holding the id of the suspension's user.suspended row. The hide is idempotent, touches only threads the caller may moderate, and passes over archived threads. The restore walks each thread's log and restores only posts whose Delete in force carries one of the account's suspensions. It appends at the length it read to, through a new in-memory threadlog.Append.AtLength precondition, so it is exact under concurrent moderation. jobs.SuspensionExpiry runs every minute and lifts due suspensions through perm.ChangeStatus with a null actor and via expiry. It rechecks the end in the transaction and leaves hidden posts hidden. The audit log gains user.posts_hidden and user.posts_restored, and post.hidden rows name the suspension. perm.ChangeStatusID returns a transition's audit id, and there are three new queries. The session chokepoint is unchanged. docs/02, 03, 04, 07 and 08 are updated.
