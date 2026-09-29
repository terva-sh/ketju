---
schema: 3
id: TKT-01M3NRP3W9XSXYMSPNYS7JGYF4
title: Implement handle changes with cooldown, reservation, and redirect
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
  - effort:L
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29CKRE9F0WR2JH8QEMZ7G5B
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
  - TKT-01M29CKRFSDCGKH605X8Y4RT3C
blocks_on: none
references:
  - ref: design:identity-handle-change
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-29T05:02:34Z
updated_at: 2026-09-29T15:28:10Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

docs/05 "Changing a handle" records the policy the maintainer decided on 2026-09-28 (docs/10 item 9, TKT-01M29BSPEN). No ticket builds it yet. This one does.

- A member may change their own handle once every 30 days. The new handle passes registration's checks (`identity.CheckHandle`) and must be free in any case.
- The old handle is reserved for 90 days so nobody can take it to impersonate the member. During that time `/u/{old}` and `GET /api/v1/users/{old}` answer 302 to the new handle with `Cache-Control: no-store`. The redirect is temporary on purpose, because a cached 301 would keep sending visitors to the former owner after the handle is free again. After 90 days the old handle answers `not-found` and is free.
- Mentions, posts and quotes name members by user id, so history does not change. Posts show the current handle.
- An admin may rename any member without the cooldown. That is how grandfathered handles ending in `-` get fixed (TKT-01M3ANX1NF). The old handle is reserved and redirected just the same.
- Every change writes an audit row with the old and the new handle.
- An anonymized account (`deleted-user-<id>`) cannot be renamed.

Filed during Phase 2 grooming (TKT-01M3M9RR26) from the plan for TKT-01M29CKRFS "Implement the account lifecycle: deactivate, suspend, anonymize".

### Notes for the plan

- Registration (`identity.Register`), the auto_approve handle skip (TKT-01M29CKRE9), and rename must share one "is this handle free" check. TKT-01M29CKRE9 extracts it as `identity.handleFree(ctx, q, h)`. This ticket adds the reservation lookup there, so a reserved handle counts as taken on all three paths.
- A reservation table (the old handle `COLLATE NOCASE`, the user it redirects to, and an expiry) needs a migration, plus a `users` column or an audit query for the 30-day cooldown. An anonymized account's old handle is also reserved for 90 days, with no redirect (maintainer decision 2026-09-29, recorded on TKT-01M29CKRFS). The table therefore needs a row with no redirect target, and a reservation must not name the anonymized user. Audit rows from renames keep the old and new handle after anonymization, and docs/05 says so (maintainer decision 2026-09-29).
- A rename changes the author rows, so the response cache's authors digest and the render cache's resolved-mention check both see it (docs/07 "The response cache"). Name tests that prove a cached page is not served with the old handle.
- The member route and the admin route are new API surface. Record them in docs/07 in the same PR.

## Acceptance criteria

- [ ] PUT /api/v1/settings/handle renames the viewer at most once per 30 days: a second rename inside the window is conflict with available_at, while POST /api/v1/admin/users/{handle}/rename is not limited and does not start the member's cooldown (TestRenameCooldown)
- [ ] A new handle passes identity.CheckHandle and identity.HandleFree, and a handle released within 90 days is taken for rename, Register, the auto_approve skip and application approval (TestRenameChecksTheHandle, TestReleasedHandleIsTakenEverywhere)
- [ ] For 90 days a member's GET of /u/{old}, /u/{old}/posts, /api/v1/users/{old} and /api/v1/users/{old}/posts answers 302 with Cache-Control: no-store to the renamed user's current handle, chains included; anonymous gets the shell or 401 with no Location; afterwards the old handle is not-found and can be registered (TestReleasedHandleRedirects, TestShellRedirectsReleasedProfiles)
- [ ] Every rename writes one user.renamed audit row through audit.Append with {from, to} in the transaction that renames, and a failed audit insert leaves the handle, handle_changed_at and handle_releases unchanged (TestRenameWritesOneAuditRow, TestRenameIsOneTransaction)
- [ ] An anonymized account cannot be renamed by its owner or an admin (TestAnonymizedCannotBeRenamed)
- [ ] After a rename neither the render cache nor the response cache serves a page showing the old handle as author or mention (TestOldMentionsFollowTheRename, TestResponseCacheAfterRename)
- [ ] An old post's mention of the old handle links the renamed member by their current handle during and after the reservation, and never a later holder of the old handle, while a mention of a handle registered after the post still links (TestOldMentionsNeverMoveToANewHolder, TestAMentionTypedBeforeRegistrationStillLinks)
- [ ] Anonymizing reserves the old handle for 90 days with a handle_releases row naming no user, and clears the user from that account's earlier release rows, so no old handle redirects to deleted-user-<id> (TestAnonymizeReleasesWithoutRedirect)
- [ ] docs/04 describes handle_releases and users.handle_changed_at, and docs/07 records both rename routes and the member-only 302

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310.

What exists there:
- `identity.CheckHandle` and `NormalizeHandle` (`internal/service/identity/handle.go`). `Register` reads "free in any case" inline with `q.GetUserByHandle` inside `IssueUserWith`'s create func (`register.go`). TKT-01M29CKRE9 extracts that read as `handleFree(ctx, q, h)`.
- `member.Account` looks a profile up by handle (`internal/service/member/account.go`); `GET /api/v1/users/{handle}` and `/posts` are `userProfile` and `userPosts` in `internal/web/api/users.go`, both `no-store`.
- `/u/{handle}` and `/u/{handle}/posts` are SPA routes: the catch-all in `internal/web/server.go` hands them to `web.Shell`, and `routes.tsx` renders a placeholder.
- A mention is resolved by the text typed, at every render: `threads.Service.handle` reads `GetUserByHandle` (`internal/service/threads/view.go`), through `handleMemo`, for the read path (`htmlDeps`) and the write path (`renderSource`, preview and create). The `mentions` projection (`thread_id, ordinal, mentioned_user_id`) holds the ids each post mentions, kept current across edits.

### Settled in grooming

- **The policy** (decided 2026-09-28, docs/05 "Changing a handle", docs/10 item 9, TKT-01M29BSPEN). A member may change their own handle once every 30 days. The new handle passes registration's checks: valid, not reserved, free in any case. The old handle is reserved for 90 days. During that time `/u/{old}` and `GET /users/{old}` answer 302 to the new handle with `Cache-Control: no-store`; the redirect is temporary so that no cached 301 outlives the reservation. Afterwards the old handle is `not-found` and free. Mentions, posts and quotes name the member by user id, so history does not change, and posts show the current handle. An admin may rename any member without the cooldown, with the same reservation and redirect. Every change is audited with the old and new handle. An anonymized account cannot be renamed.
- **A released handle is not free for the auto_approve handle skip** (decided 2026-09-28, docs/05 "mode = approval", docs/10 item 10). Registration, the skip, approval and rename share one check.
- **Anonymization** (maintainer decisions 2026-09-29, recorded on TKT-01M29CKRFS and in docs/05 "Account lifecycle"): an anonymized account's old handle is reserved for 90 days with no redirect, and the reservation does not name the user. Audit rows keep old handles, rename rows included; anonymization does not reach `audit_log`.
- **Profiles are for members** (decided 2026-09-28, TKT-01M3KJJNDC, docs/07 `GET /users/{handle}`): anonymous is `unauthenticated`, a pre-user `forbidden`, a pending account `not-found` except to staff.
- **Audit rows go through `internal/audit`** (`audit.Append`, `audit.User(id)`), and every `/api/v1/admin/*` route registers through `adminRoute` with a gate (TKT-01M3NRYCP5). `TestOneWayIntoAuditLog` and `TestAdminRoutesAreGated` cover this ticket's writes and route.
- **The cache invariant** (docs/07 "The response cache"): the authors digest sees a rename of an author, and the handle lookups a rendering made are checked on every hit of the render and response caches.

Engineering decisions taken here, with the alternative that lost:
- **One table, `handle_releases`, kept forever.** A row records that a handle left a user: the handle, the user it redirects to (NULL for an anonymized account), `released_at` and `reserved_until` (released_at + 90 days). A handle is reserved while some row has `reserved_until > now`; nothing sweeps rows. Mentions need the history after the reservation ends (below). Lost: deleting rows at expiry with a job, which forgets who held a handle and so lets an old mention move to the next holder.
- **The redirect names the user id, not the new handle.** A chain alice → bob → carol sends both old handles to carol with no rewrite. Lost: storing the new handle, which is stale after a second rename.
- **The cooldown is a column, `users.handle_changed_at`**, written only by a member's own rename. Lost: reading `audit_log` details (couples behaviour to an operational record's JSON), and `MAX(released_at)` (counts admin renames and loses rows to anonymization).
- **Readings of the settled text, not new rules.** "A member may change their own handle once every 30 days" counts the member's own changes, so an admin rename neither waits for nor starts the member's cooldown. "Not reserved" applies to everyone, so a member cannot take back their own old handle inside the 90 days, and neither can an admin for them. A note on this ticket flags the second for the maintainer; it is not an open decision, because the literal rule is buildable and safe.
- **A released handle answers "taken", not "reserved"**, from `HandleFree` (`ErrHandleTaken`). A registrant cannot probe which handles were released recently, and the auto_approve reason line says "taken". The field codes therefore have one meaning each, on the rename routes, in registration and in every control that shows them (TKT-01M3P0CYR3):
  - `invalid`: `domain.ValidateHandle` refuses it.
  - `reserved`: `CheckHandle` refuses it as a route name (`admin`, `api`, …) or as the `deleted-user-` prefix (`ErrHandleReserved`). It never means a released handle.
  - `taken`: `HandleFree` refuses it, because a user holds it in any case or a release still reserves it.
  - `same`: it is the account's current handle.

  Terva review 1372 (PR #143, head 6d6aeea) found the first version implied a released handle could answer `reserved`. Lost: a separate `released` code, which would tell a stranger which handles were given up in the last 90 days.
- **Only a member is redirected.** Anonymous `GET /api/v1/users/{old}` stays `unauthenticated`, and anonymous `/u/{old}` gets the shell, as for any profile. A redirect to a pending account is `not-found` except to staff. Following the profile rule keeps a stranger from learning who renamed to what.
- **The redirect covers `/posts` too**, keeping the query string (the cursor names no account): `/u/{old}/posts` and `/api/v1/users/{old}/posts`. docs/07 lists the two paths together.
- **Rename lives in `internal/service/identity/rename.go`**, beside `CheckHandle` and `HandleFree`, because identity owns the handle rules. Lost: `perm` (a rename changes no permission decision) and TKT-01M29CKRFS's `internal/service/account` (status transitions; the handle rules would be split across two packages).
- **`handleFree` is exported as `identity.HandleFree`**, so the application review in `internal/service/applications` (TKT-01M29CP2MG) calls the same check.
- **The cooldown refusal is `conflict` with an `available_at` extension** (`dto.Millis`). Lost: `rate-limited` with a 30-day `Retry-After`, which clients treat as "retry soon".
- **Mentions are pinned by id.** docs/05 says a mention names the member by user id, but the read path resolves the typed text on every render, so today a rename turns every old `@alice` into plain text, and after 90 days into a link to whoever registers `alice`. This ticket makes the design true. At read time, for a post whose `mentions` projection rows are M and whose current revision was written at T:
  1. the current holder H of the typed handle, when H is in M;
  2. else a `handle_releases` row for the typed handle whose user is in M: that user, shown by their current handle;
  3. else H, when no release of the handle is later than T (a handle registered after the post was written: docs/07's "a user registering a mentioned handle is seen at once");
  4. else plain text.
  At write time (preview and create) a typed handle resolves to its holder, else to the user an active reservation redirects to, so a new `@alice` during the 90 days mentions the renamed member and records their id. Lost: resolving through the reservation alone, which works for 90 days and then hands old mentions to the next holder; and reading M from the log record, because an `Edit` carries mentions only when it changed them and the reader reads only the current revision's frame.

### Approach

1. **Migration** `internal/store/db/migrations/00NN_handle_releases.sql`, the next free number when implemented:
   - `ALTER TABLE users ADD COLUMN handle_changed_at INTEGER` (Unix ms, NULL until the member's first change).
   - `CREATE TABLE handle_releases (id INTEGER PRIMARY KEY, handle TEXT NOT NULL COLLATE NOCASE CHECK (…the users.handle CHECK…), user_id INTEGER REFERENCES users (id), released_at INTEGER NOT NULL, reserved_until INTEGER NOT NULL CHECK (reserved_until > released_at))`. Authoritative.
   - `CREATE INDEX handle_releases_handle ON handle_releases (handle, reserved_until)` and `handle_releases_user ON handle_releases (user_id)`.
   - docs/04: the table under "Authoritative tables", the column under "users", and the reason rows are kept.
2. **Queries** (`internal/store/db/queries/identity.sql`; regenerate dbq):
   - `HandleReserved :one` (EXISTS a row with `handle = ? AND reserved_until > ?`).
   - `ActiveRelease :one`, the newest row for the handle with `reserved_until > ?` and its `user_id`.
   - `HandleReleases :many`, every row for one handle (`user_id`, `released_at`), for the resolver.
   - `InsertHandleRelease :exec`.
   - `ForgetReleasesOf :exec`: `UPDATE handle_releases SET user_id = NULL WHERE user_id = ?`.
   - `RenameUser :execrows`: `UPDATE users SET handle = ?, handle_changed_at = COALESCE(?, handle_changed_at), updated_at = ? WHERE id = ? AND handle = ?` plus `AND anonymized = 0` once TKT-01M29CKRFS's column exists (it is a dependency). The old handle in the WHERE makes two concurrent renames produce one.
3. **One check** (`internal/service/identity/handle.go`): `HandleFree(ctx, q, h, now) error` returns `ErrHandleTaken` when a user holds `h` in any case or `HandleReserved` is true. `Register`, `AutoRegister` (TKT-01M29CKRE9) and `Rename` call it. If TKT-01M29CP2MG has landed, its `Approve` step 4 switches from `GetUserByHandle` to `HandleFree` in this PR; if not, its plan already requires the reservation check and it calls `HandleFree` when it lands.
4. **Rename** (`internal/service/identity/rename.go`): `(*Service).Rename(ctx, v domain.Viewer, target string, to string, byAdmin bool) (dto.HandleChange, error)`. `v` must be a member (`member.Require`); a self-rename targets `v`'s own row, an admin rename the row `target` names (not-found otherwise). In one `db.Write`:
   1. `CheckHandle(to)`: an invalid handle, or a route name or the `deleted-user-` prefix, is a field error (`validation-failed`, `errors[handle]` `invalid` or `reserved`, TKT-01M29CP2KE's extension).
   2. `GetUser`. Anonymized (the column; `domain.Anonymized(handle)` until it exists) is `conflict`. The same handle is `validation-failed` `errors[handle] = same`.
   3. For a self-rename, `handle_changed_at + 30 days > now` is `conflict` with `available_at`.
   4. `HandleFree(to)`: taken is `conflict`, `errors[handle] = taken`.
   5. `RenameUser`; zero rows is `conflict`. A self-rename passes `now` for `handle_changed_at`, an admin rename NULL.
   6. `InsertHandleRelease(old, user id, now, now + 90 days)`.
   7. `audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, "user.renamed", audit.User(id), {"from": old, "to": new})`.
   The durations are constants `RenameCooldown` and `HandleReservation` in `identity`. No session is touched, and no cache needs invalidating: the session reads the users row on every request.
5. **Anonymization** (`internal/perm/mutate.go`, `Anonymize` from TKT-01M29CKRFS): in its transaction, after `AnonymizeUser`, call `ForgetReleasesOf(user)` and `InsertHandleRelease(old handle, NULL, now, now + 90 days)`. The first stops earlier renames redirecting to `deleted-user-<id>`, which the 2026-09-29 decision exists to prevent. Say so in the summary, as TKT-01M29CKRFS's plan asks.
6. **Routes** (`internal/web/api/handle.go`, recorded in docs/07):
   - `PUT /api/v1/settings/handle` with `{handle}`: a member's own rename. 200 `dto.HandleChange{handle, previous, next_change_at}`, `no-store`, JSON only, charged to the write limiter.
   - `POST /api/v1/admin/users/{handle}/rename` with `{handle}`, registered through `adminRoute(mux, …, admin, …)`. Same answer.
   - `dto.HandleChange` with goldens and the client contract entry in `dto.ts` and `dto-contract.ts`. No screen calls it; if the dist changes, run `go generate ./internal/web/spa`.
7. **The redirect in the API.** `member.Account` returns `*member.Moved{UserID, Handle}` when no user holds the handle, `ActiveRelease` names a user, and `v.MaySeeAccount(that user's status)` holds. `userProfile` and `userPosts` answer it with 302, `Location: /api/v1/users/{new}` (plus `/posts` and the query), and `Cache-Control: no-store`. Every other case is unchanged: `not-found`.
8. **The redirect for the SPA path.** `api.ProfileRedirects(users *users.Service, next http.Handler) http.Handler` wraps the shell in `cmd/ketju/serve.go` (`SPA: api.ProfileRedirects(userSvc, shell)`). For `GET`/`HEAD` of `/u/{h}` or `/u/{h}/posts` with a member viewer, it asks the same lookup and answers 302 `no-store` to `/u/{new}…`; otherwise it serves `next`. The shell's own `no-store` is unchanged.
9. **Mention resolution** (`internal/service/threads/view.go`, `preview.go`):
   - `handleMemo` caches, per typed handle, the holder and its `HandleReleases` rows: one read per distinct handle per request, as now.
   - `resolveAt(typed, mentions []int64, since int64)` applies the read-time rule above; `resolveNow(typed)` applies the write-time rule. `htmlDeps` reads M for the page's ordinals from the `mentions` projection in one query (`MentionsOfPosts`, new) and passes `p.RevisionMS` as `since`. `renderSource` uses `resolveNow`.
   - `render.HandleLookup` gains `Context string`, a digest of the mention ids and `since` the answer was given for, so `handleMemo.current` asks the same question again on a hit. The render cache and the response cache both check through it.
   - The `render/mention.go` comment that says rename invalidation "belongs with TKT-01M29BSPEN" is replaced by a pointer to this rule.
10. **Docs.** docs/07: both routes, the 302 for `/users/{old}` and `/posts` and for `/u/{old}`, member-only. docs/04: the table and column. docs/06 "Composer" already says a mention is recorded by user id; add one sentence on the reservation at write time.

### Tests

- `internal/service/identity/rename_test.go`:
  - `TestRenameCooldown`: a second self-rename inside 30 days is `conflict` with `available_at` = first + 30 days; at that instant it succeeds. An admin rename inside the window succeeds and leaves `handle_changed_at` unchanged.
  - `TestRenameChecksTheHandle`: invalid is `invalid`; `admin` and `deleted-user-9` are `reserved`; a handle held in another case is `taken`; a handle released within 90 days is `taken` and never `reserved`; the current handle is `same`.
  - `TestReleasedHandleIsTakenEverywhere`: after a rename, `Register`, `AutoRegister` (TKT-01M29CKRE9) and `Rename` by another member all get `ErrHandleTaken` for the old handle, and `Approve` too when TKT-01M29CP2MG has landed. With the clock 90 days and 1 ms later, `Register` succeeds.
  - `TestRenameIsOneTransaction`: a `BEFORE INSERT ON audit_log` trigger that fails leaves the handle, `handle_changed_at` and `handle_releases` as they were.
  - `TestRenameWritesOneAuditRow`: `user.renamed`, `target_type` user, the actor (member or admin), and `{from, to}`.
  - `TestConcurrentRenamesProduceOne`: two renames of one user race; one succeeds, one is `conflict`, one release row.
  - `TestAnonymizedCannotBeRenamed`: self and admin are `conflict`, and TKT-01M29CKRFS's trigger refuses a direct UPDATE.
- `internal/perm` `TestAnonymizeReleasesWithoutRedirect`: after a rename alice → alicia and then anonymization, the rows for `alice` and `alicia` both have `user_id` NULL, `/u/alice` and `/u/alicia` answer the shell and `GET /api/v1/users/alice` is `not-found` (no 302), and both handles are taken for 90 days.
- `internal/web/api`:
  - `TestRenameRoutes`: the member route is 401 anonymous, 403 pre-user, 200 member; the admin route is 403 to a moderator and a member and 200 to an admin (and in `TestAdminRoutesAreGated`'s walk).
  - `TestReleasedHandleRedirects`: `/api/v1/users/alice` and `/api/v1/users/alice/posts?cursor=x` answer 302 to alicia's with `no-store`; a chain alice → bob → carol sends both to carol; after 90 days both are `not-found`; anonymous is 401 with no `Location`; a pending target is `not-found` to a member and 302 to a moderator.
  - `TestShellRedirectsReleasedProfiles`: `/u/alice` and `/u/alice/posts` answer a member 302 `no-store`, and anonymous 200 with the shell.
  - `TestResponseCacheAfterRename`, following `TestResponseCacheAfterPromotion`: a page cached with Alice as author and as a mention is not served after the rename; the next answer shows `alicia` in the header and the mention.
- `internal/service/threads`:
  - `TestOldMentionsFollowTheRename`: Bob's cached rendering of `@alice` is not served after the rename; the new one links `/u/alicia` and reads `@alicia`.
  - `TestOldMentionsNeverMoveToANewHolder`: 90 days later Vic registers `alice`. Bob's old post still links alicia. A post written after Vic registered links Vic.
  - `TestAMentionTypedBeforeRegistrationStillLinks`: a post mentioning an unknown `@dora` links Dora once she registers (docs/07), with no release rows for `dora`.
  - `TestAMentionOfAReleasedHandleFollowsTheRedirect`: during the 90 days, a new post's `@alice` records alicia's id and renders `@alicia`.
  - `TestAnAnonymizedMentionIsText`: after alicia is anonymized and Vic takes `alice`, the old post's `@alice` is plain text, never Vic.
- `TestOnlyIdentityRenames`: a source scan like `TestMutationsGoThroughPerm`, failing on a call to `RenameUser` outside `internal/service/identity` and `internal/store/db`.
- Chokepoint: a rename issues no session, and `TestOneDoorIntoSessions` passes with its caller lists unchanged.

### Out of scope

- A member's rename control: TKT-01M29CXK4E (Build the user settings screens, phase-4). Until then the member route has no screen.
- The admin rename control: filed as a new phase-2 ticket, "Add a rename control to the admin users screen".
- A profile screen: `/u/:handle` is a placeholder and no ticket builds it. Whoever does must replace the URL with the handle the API answered after a redirect.
- The anonymization itself and the `anonymized` column: TKT-01M29CKRFS, a dependency.
- Fixing grandfathered handles ending in `-`: TKT-01M3ANX1NF, which uses the admin route.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T05:38:53Z

Maintainer decisions 2026-09-29, from TKT-01M29CKRFS: an anonymized account's old handle is reserved for 90 days with no redirect, and audit rows keep old handles after anonymization, which docs/05 will document. The description and a new criterion reflect the first.

**agent:claude-code/phase2-groom** at 2026-09-29T07:17:37Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-29. Three things for whoever claims this, and for the maintainer:

1. **docs/05 and the code disagree about mentions.** docs/05 says mentions name the member by user id, so history does not change. The read path resolves the typed `@handle` on every render (`threads.Service.handle`), so today a rename turns old mentions into plain text, and once the handle is free they link whoever registers it. The plan makes the doc true: old mentions are pinned through the `mentions` projection and the kept `handle_releases` rows. That is why the effort is now L.
2. **A mention of an anonymized account.** TKT-01M29CKRFS's plan expects Bob's old `@alice` to render as `deleted-user-<id>` after Alice is anonymized. Under the 2026-09-29 decision the reservation names no user, and this plan also clears the user from Alice's earlier release rows, so the old mention renders as plain text `@alice`. It never renders as a link to someone else. Recommended: keep plain text, which ties nothing to the deleted-user id, and change TKT-01M29CKRFS's test to match when either ticket is claimed.
3. **Readings of the settled text, not new decisions.** An admin rename neither waits for nor starts the member's 30-day cooldown. A member cannot take back their own old handle during its 90 days, because the rule says "not reserved" without exception. If the maintainer wants the former owner exempt, it is one condition in `HandleFree`.

Filed: TKT-01M3P0CYR3 (Add a rename control to the admin users screen). Linked: this ticket now depends on TKT-01M3NRYCP5 (audit helper and admin gate) and TKT-01M29CKRFS (the anonymized column, and `perm.Anonymize`, where the reservation write goes).
