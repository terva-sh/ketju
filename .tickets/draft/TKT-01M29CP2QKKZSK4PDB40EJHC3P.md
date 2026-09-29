---
schema: 3
id: TKT-01M29CP2QKKZSK4PDB40EJHC3P
title: Implement user suspension from the moderator tools
type: task
status: draft
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
blocks_on: none
references:
  - ref: design:moderation-primitives
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T05:41:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Suspend and unsuspend with a reason and a duration, from the moderator UI, with the option to hide the user's posts.

docs/08 rules out shadow banning explicitly: a suspension carries a reason the member can read. Deceiving a member of a small community about whether they are still in it is worse than removing them.

## Acceptance criteria

- [ ] Suspension revokes sessions immediately and shows the reason at the next login attempt
- [ ] Every suspension and unsuspension is in audit_log
- [ ] A suspension with an end time is lifted by the SuspensionExpiry job within a minute of it, audited with no actor and via expiry, and a suspension with no end time lasts until lifted (TestSuspensionExpires)
- [ ] hide_posts appends a hidden-by-mod Delete carrying the suspension's audit id to each of the user's visible posts, and restoring appends a Restore to exactly those and leaves posts hidden for other reasons hidden (TestHidePostsIsReversible)
- [ ] A suspended user cannot obtain a session by any path: IssueUserWith refuses them and the chokepoint's caller lists are unchanged (TestChokepointRefusesSuspended, TestOneDoorIntoSessions)
- [ ] Moderators suspend members, admins suspend anyone but themselves, and suspending the last active admin is conflict, tested as a table (TestSuspendAuthority)
- [ ] When a suspension expires, the posts it hid stay hidden until a moderator restores them (TestSuspensionExpires)

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

### Approach

1. **The contract with TKT-01M29CKRFS.** It provides, under whatever names it lands with, `Suspend(ctx, by, userID, reason, until *time.Time) (auditID int64, err)` and `Unsuspend(ctx, by, userID, via string) error`. If `Suspend` does not return the audit row's id, this ticket adds that.
2. **Authority.** `moderation.MaySuspend(v, target dbq.User) error` applies the maintainer's rule. The actor is active, never the target. A moderator by role may suspend a `member`. An admin may suspend any account. Refuse, as `conflict`, a suspension that would leave no active admin: count the active admins inside the transition's transaction, which TKT-01M29CKRFS holds for all its transitions. A forum-scoped moderator, a member who holds `moderate` through a group, may not suspend, because suspension is not forum-scoped. Suspending an already-suspended account is `conflict`. The reason is required, 1 to 1000 characters. `until`, if given, must be in the future.
3. **The Delete key.** In `internal/store/threadlog`, the `Delete` body gains `Suspension uint64` with `cbor:"suspension,omitempty"` (and the matching JSON tag). docs/03's table row for type 4 and a paragraph like `Edit.mentions` record it. The threads service's moderator delete path (TKT-01M29CRJ5J) takes an optional suspension id.
4. **`internal/service/moderation/suspend.go`**:
   - `Suspend(ctx, v, by, handle, SuspendRequest{Reason; Until *int64; HidePosts bool})` calls the lifecycle transition, then `hidePosts` when asked.
   - `hidePosts` takes the user's threads from `post_meta` (`author_id` index), then each thread's ordinals by this author from its index, as the index is the log. Each thread gets one `threadlog.Append` batch of `Delete{Reason: "suspension", Suspension: auditID}` with `Refuse: deleted|hidden|moved`, actor = the moderator. It then appends one `user.posts_hidden {suspension, posts: n}` audit row.
   - `Unsuspend(ctx, v, by, handle, {RestorePosts bool})`, and `RestorePosts(ctx, v, by, handle)`, which finds `hidden-by-mod` posts by this author whose latest `Delete` frame carries a suspension id recorded for this user (read with `ScanFrames` for the thread; this is a moderator action, not a page), appends `Restore{Reason: "unsuspended"}` to each, and writes `user.posts_restored`.
5. **`internal/jobs/suspension.go`**: `SuspensionExpiry(lifecycle)` returns a `jobs.Job` that runs every minute over `ExpiredSuspensions(now)` (`status = 'suspended' AND <until column> <= ?`). Add it to the `background` list in `cmd/ketju/serve.go`. Posts hidden with the suspension stay hidden on expiry (maintainer decision above).
6. **Routes** in `internal/web/api/admin_users.go` (staff, plus `MaySuspend`): `POST /api/v1/admin/users/{handle}/suspend` with `{reason, until?, hide_posts}`, `POST …/unsuspend` with `{restore_posts}`, `POST …/posts/hide`, and `POST …/posts/restore`. Each answers `dto.AdminUserDetail`. They are JSON-only, write-limited and `no-store`.
7. **Client** (admin chunk), in the users screen's detail panel:
   - Suspend opens a dialog with a required reason, the end time (1 day, 7 days, 30 days, until lifted, or a date), and "Hide their posts" explained as "Members see 'removed by a moderator'; you can restore them later".
   - Unsuspend has a "Restore posts hidden with this suspension" checkbox.
   - The status badge shows the reason and the end time.
   - The moderator queue links an author here (TKT-01M29CRJ4E).
8. **Docs**: docs/08 gains a "Suspension" paragraph (authority, hiding through the log, exact restore, expiry). docs/07 documents the routes. docs/03 documents the `Delete` key.

### Tests

- `TestSuspendEndsSessionsAtOnce`: after the API call, the member's old cookie resolves to no session (the next request is anonymous and the cookie is cleared). A sign-in attempt meets `NotActiveError` from `IssueUserWith`, and the page shows the reason and end time (the page itself is TKT-01M29CKRFS's).
- `TestSuspendAuditedInOneTransaction`: a test trigger aborting the `user.suspended` insert leaves the status active and the sessions in place.
- `TestChokepointRefusesSuspended`: `IssueUser` and `IssueUserWith` refuse a suspended user. `TestOneDoorIntoSessions` passes with its caller lists unchanged.
- `TestSuspensionExpires`: with a fake clock and one job run, the status is active, the audit row has a null actor and `via: expiry`, a sign-in succeeds, and the posts hidden with the suspension are still hidden.
- `TestHidePostsIsReversible`: hiding makes a member's thread page show "removed by a moderator", and lowers the profile post count and the forum's `post_count`. Restoring brings all three back. A post a moderator hid separately stays hidden.
- `TestHidePostsIsIdempotent`: running hide twice appends nothing the second time.
- `TestSuspensionHideMovesTheCacheKey`: a thread page cached for a member before the hide serves tombstones after it.
- `TestSuspendAuthority`: a table covering a moderator suspending a member (allowed) and a moderator or admin (refused), an admin suspending a member, a moderator and another admin (allowed), anyone suspending themselves (refused), suspending the last active admin (`conflict`), a forum-scoped moderator (refused), and an already-suspended account (`conflict`).
- `TestSuspensionAudited`: suspend, unsuspend, hide and restore rows name the actor and target.
- `TestDeleteSuspensionKeyRoundTrips` in `threadlog`: CBOR and JSON round-trip, and a body without the key decodes as zero.
- Client: the suspend dialog's validation, the badge text. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- The lifecycle columns, transitions and sign-in page (TKT-01M29CKRFS).
- Deactivation and anonymization.
- Revoking an agent's credential on suspension (TKT-01M29P9RSW adds it to the lifecycle transition).
- Suspending from the queue, which links here instead.
- Editing the reason or end time of a current suspension: lift it and suspend again.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:13:04Z

Two open decisions for the maintainer (grooming, 2026-09-29). 1. Who may suspend whom: (a) only a role strictly below the actor's, never oneself; (b) any staff member may suspend anyone but themselves; (c) as (a), and admins may also suspend admins. Recommended: (a). 2. On expiry, do the posts hidden with the suspension come back: (a) no, a moderator restores them; (b) yes. Recommended: (a). The plan builds both recommendations. See the plan's Open decision.

**agent:claude-code/phase2-groom** at 2026-09-29T05:41:12Z

Maintainer decisions 2026-09-29, superseding the open decisions in the earlier note. (1) Moderators suspend members, and admins suspend anyone except themselves. The last active admin cannot be suspended, deactivated, anonymized or demoted. This supersedes option (a) and matches TKT-01M29CKRFS. (2) When a suspension expires, the posts it hid stay hidden until a moderator restores them.
