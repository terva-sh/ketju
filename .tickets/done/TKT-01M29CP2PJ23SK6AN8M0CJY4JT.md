---
schema: 3
id: TKT-01M29CP2PJ23SK6AN8M0CJY4JT
title: Implement flags with the auto-hide threshold
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29CA9CECTE02DFZGBGN2HC2
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
  - TKT-01M29CKRJ9DTBAC2BSBYAT9CQC
blocks_on: none
references:
  - ref: design:flags
    path: docs/08-moderation.md
  - ref: pr:175
    path: null
  - ref: pr:176
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-30T00:06:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2pj
  name: ""
extensions: {}
---

## Description

Any member can flag a post with a reason of spam, abuse, off-topic, or other with text. A post reaching the threshold of open flags, default 3 and a setting, is auto-hidden pending review through a Delete variant marked `auto=true`, which a moderator's Restore clears.

Reporters see "thanks, a moderator will look" and nothing else. Telling a reporter what happened to their report turns moderation into a conversation with the reporter, and in a small community that is how grudges start.

## Acceptance criteria

- [x] When a post's open flags reach flags.auto_hide_threshold, that request appends one Delete with auto: true and frame actor 0, the entry gets hidden-by-mod and auto-hidden, and a post.auto_hidden audit row is written
- [x] A moderator's Restore clears hidden-by-mod and auto-hidden, and the post's open flags become dismissed rows that stay in the flags table
- [x] POST /threads/{id}/flags answers the same body for a new flag, a duplicate and the flag that triggers the hide, and no member-facing response carries a flag count or a reporter
- [x] A member's second open flag on the same post adds no row (partial unique index), so the open count counts distinct reporters
- [x] While auto-hidden, the author and moderators receive the post's html with removal review, everyone else gets a tombstone without html, and the author's restore is forbidden
- [x] A page cached before the auto-hide is not served after it (TestAutoHideMovesTheResponseCacheKey)
- [x] A threshold-crossing flag whose hide append fails stays committed and answers unavailable, and the next AutoHideSweep run hides the post (TestAutoHideRecoversFromAFailedAppend)
- [x] Anyone other than the author and moderators sees an auto-hidden post as a tombstone reading 'Post hidden pending review'
- [x] A restore of an auto-hidden post and AutoHideSweep hold one per-post lock, flags are dismissed before the Restore append and reopened if it fails, and a concurrent sweep never re-hides the restored post (TestSweepCannotReHideDuringRestore)
- [x] Flags dismissed for a restore are reopened only when the Restore append itself fails; after a durable append whose projection fails they stay dismissed and no sweep re-hides the post

## Implementation plan

Revised 2026-09-30 by the Phase 2 run against main 2826208. The Settled section is carried unchanged from grooming.

### Settled in grooming

- **Threshold, visibility and restore.** Maintainer decision, 2026-09-28, in note 1 on this ticket and docs/08 "Flags (reports)". The threshold defaults to 3 and is a site setting. `flags.auto_hide_threshold` already exists in `internal/settings/settings.go`, `atLeast(1)`. While a post is auto-hidden, its author and moderators see it marked "hidden pending review", and everyone else sees a tombstone. The auto-hide is hidden-by-mod, so the author cannot restore it; only a moderator can. Queue-only lost because spam would stay up until a moderator is awake.
- **What stays confidential** (note 2, 2026-09-28, which supersedes note 1's rationale but not its decision; docs/08 corrected to match). The marker does tell the author that flags put the post under review, and that is intended. Who flagged it and how many did stays with moderators. Hiding the post from its author too lost, because a tombstone on their own post tells them the same thing.
- **Everyone else reads "Post hidden pending review".** Maintainer decision 2026-09-29, accepting the grooming recommendation. Anyone who is neither the author nor a moderator sees an auto-hidden post as a tombstone reading "Post hidden pending review". It is true, and it reveals no count and no reporter. "Post removed by a moderator" lost because no moderator has acted yet. A plain "Post removed" lost because it says less than the truth for no gain.
- **Reporters see "thanks, a moderator will look" and nothing else** (docs/08, ticket description).
- **The flag route** is `POST /api/v1/threads/{id}/flags` with `{ordinal, reason, note}` (docs/07 resources table). The form-post row naming this ticket is already in docs/07 "Form posts for viewer state". TKT-01M29CA9CE left the route to this ticket.
- **The `flags` table exists** (migration 0006: reasons, a CHECK that `other` carries a note, never deleted or replaced), with `CreateFlag`, `CloseFlag` and `OpenFlagCount` in `queries/moderation.sql`.
- **The hide is a `Delete` variant with `auto=true`** (docs/08 "Flags"). No new record type: `auto` is an optional key in the `Delete` body, which the docs/03 freeze permits.
- **Pending review is an index bit**, `EntryAutoHidden`, bit 6 of the entry flags. docs/03 names the ten unused flag bits (6 to 15) as the index's spare capacity. The index is derived, and the bit is set only from the log's `auto` key, so a rebuild reproduces it. An older build does not know bit 6 but still sees `EntryHiddenByMod`, and shows a tombstone, which fails closed. Deriving "pending review" from open rows in `flags` lost: that would put database state into the cached posts payload outside `respcache.Key`, and a rebuild from the log could not reproduce it.
- **The auto-hide's frame actor is 0**, the system. Naming the reporter whose flag crossed the threshold would put a reporter into the thread log, which any `ketju dump` shows.


Restated by the orchestrator for the run, 2026-09-30, and binding: the threshold is 3 and a setting; the hide runs on every flag at or over the threshold, from committed flags only, and AutoHideSweep runs every minute and at startup; restore, confirm and auto-hide share one per-post lock; a restore reopens flags only if the Restore append itself failed; the auto-hide is a Delete with auto=true, its audit actor is the system (audit.System, actor 0 written with via); a member cannot flag the same post twice; a reporter sees only the acknowledgement. TKT-01M29CRJ4E's recovery-marker rule (an explicit pending marker, never a NULL audit_id) binds any audit recovery. This ticket writes no flag audit row and needs no marker, because the open rows in `flags` are the record that a hide is owed.

### Approach

What main has now: `dto.RemovalReview` is declared and unset; `revealRemoved` already serves a hidden-by-mod post to its author and moderators, and `pageClass` gives the author the `author:<id>` class; `Delete{ByModerator}` and `Append.RequireAny` exist; the projection writer writes moderation audit rows from the record and names actor 0 as `audit.System("thread-log")`. The last migration is 0013. The suspension lane adds none, so this takes 0014.

1. **Migration 0014** `flags_one_open_per_reporter`: the partial unique index. Nothing wrote `flags` before this ticket, so no duplicates can exist. `CreateFlag` becomes `INSERT … ON CONFLICT (thread_id, ordinal, reporter_id) WHERE status = 'open' DO NOTHING` as `:execrows`: the id was never needed, and `RETURNING` on a skipped row is no row. New queries: `DismissOpenFlagsForPost` and `ResolveOpenFlagsForPost` (`:many`, `RETURNING id`), `ReopenFlags` (`sqlc.slice`, only `dismissed` rows), and `PostsAtFlagThreshold` (open flags grouped by post, `HAVING count(*) >= ?`, joined to `post_meta` where `deleted = 0`).
2. **Format**: `DeleteBody.Auto` (`auto,omitempty`), `EntryAutoHidden = 1 << 6`, and `EntryFlagsKnown` and the reserved set updated. In `postFlagEffect`, an auto Delete sets hidden-by-mod and auto-hidden, a `by_moderator` Delete sets hidden-by-mod and clears auto-hidden, and a Restore clears all three.
3. **Reading it**: `removal()` answers `"review"` for hidden-by-mod with auto-hidden. `revealRemoved` already gives every removal other than the author's own to the author and moderators, so no rule changes there.
4. **Audit**: `auditFor` maps an auto Delete to `post.auto_hidden`. The existing code writes actor 0 as `audit.System("thread-log")`.
5. **Service** (`internal/service/threads/flags.go`): `Flag`, `EnforceAutoHide`, `SweepAutoHide`, `ConfirmHide`, and a striped per-post mutex `flagLocks`: 256 stripes by thread and ordinal, ready at its zero value, never two held at once, always taken before the thread. `Flag` requires a member session and read access. It refuses a bad reason or note (`validation-failed`), a missing ordinal (`not-found`), and an archived thread or a removed or moved post (`conflict`). It inserts the flag, then runs `EnforceAutoHide`, and a failure there answers `unavailable` with the flag committed. `EnforceAutoHide` holds the post's lock from the count through the append, and appends `Delete{Reason: "hidden pending review", Auto: true}` with actor 0 and `Refuse: gone`, projected synchronously. A quarantined or archived thread, a post already down, and `ErrPostState` do nothing. `Service.Writer` becomes a one-method interface so that tests can fail or hold an append; `*posts.Writer` satisfies it.
6. **Restore**: a moderator's restore takes the post's flag lock, dismisses the post's open flags in one `db.Write`, then appends through `changePost`. Only when nothing was appended, because the append was refused or failed, does it reopen exactly those ids. After a durable append, a failed read-back or projection leaves them dismissed. This applies to every moderator restore, not only of an auto-hidden post: a restored post with open flags at the threshold would otherwise be hidden again by the next sweep, undoing the moderator. An author's restore of their own delete dismisses nothing.
7. **ConfirmHide** is a moderator's `Delete{ByModerator: true}` with `Require: EntryAutoHidden`, then `ResolveOpenFlagsForPost`, under the lock. It has no route; the queue (TKT-01M29CRJ4E) gives it one.
8. **Job**: `jobs.AutoHideSweep(svc)` runs every minute, and `serve` runs it once at startup before the scheduler starts, logging a failure without stopping. The threads service is built before the jobs step so that the job can hold it.
9. **Route**: `POST /api/v1/threads/{id}/flags` with `{ordinal, reason, note}` answers 200 `dto.FlagReceipt{received: true}`, `no-store`, charged like every write. The golden is `flag-receipt.member.json`. `dto.ts` and `dto-contract.ts` get the type only, and `internal/web/spa/dist` is unchanged.
10. **Docs**: docs/03 (Delete `auto`, entry bit 6), docs/04 (the flags index), docs/07 (the route row and `removal: "review"`), docs/08 (the lock, the sweep, a restore dismissing flags).
11. **Client**: not in this PR. A draft carries the post-menu Flag item and dialog. The tombstone text is TKT-01M29CKRJ9's client half.

### Tests

- threadlog: `TestPostFlagEffectIsWhatApplyDoes` covers `auto`, a rebuild reproduces bit 6, and flags_test's reserved set is bits 7 to 15.
- service (`internal/service/threads/flags_test.go`): `TestAutoHideAtThreshold`, `TestAutoHideRecoversFromAFailedAppend`, `TestSweepCannotReHideDuringRestore` (the Restore held inside the lock, a sweep paused between its count and its append, a failed Restore append, a failed projection), `TestRestoreClearsAutoHideAndKeepsFlags`, `TestConfirmHide`, `TestDuplicateFlagDoesNotCount`.
- API: `TestReporterSeesOnlyTheReceipt`, `TestAutoHiddenVisibility`, `TestAutoHideMovesTheResponseCacheKey`, and the validation refusals.
- db: the partial unique index refuses a second open flag and allows one after dismissal.

### Out of scope

- The moderator queue, flag counts for moderators, and resolve and dismiss without a restore: TKT-01M29CRJ4E.
- Tombstone rendering in the client: TKT-01M29CKRJ9's client half.
- The flag UI: a draft filed by this ticket.
- The non-JS form route `POST /t/{id}/posts/{ordinal}/flag`: Phase 5.
- Notifying moderators of new flags: TKT-01M29CRJ2T (phase 3).

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:23Z

Maintainer decision, 2026-09-28: the threshold defaults to 3 and is a site setting. While a post is auto-hidden, its author and moderators see it marked 'hidden pending review', and everyone else sees a tombstone. The auto-hide is hidden-by-mod, so the author cannot restore it; only a moderator can. Hiding it from the author too lost because it tells them they were reported. Queue-only lost because spam would stay up. Written into docs/08 'Flags'. Carry this into the plan's Settled section.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:39:20Z

Supersedes the rationale in the 2026-09-28 decision note, not the decision. Showing the author 'hidden pending review' does tell them flags put the post under review, and that is intended. What stays confidential is who flagged it and how many did. Hiding the post from the author too lost because a tombstone tells them the same thing. docs/08 is corrected to match. From Terva review 1280 on PR #134.

**agent:claude-code/phase2-groom** at 2026-09-29T05:07:51Z

Open decision for the maintainer, from Phase 2 grooming on 2026-09-29: the tombstone text that everyone other than the author and moderators reads for an auto-hidden post. docs/08 Tombstones has two texts, and the 2026-09-28 decision says only 'a tombstone'. Options: 'Post hidden pending review' (recommended: true, and it reveals no count or reporter); 'Post removed by a moderator' (not quite true, since no moderator has acted); a plain 'Post removed'. The plan's Open decision section carries the same.

**agent:claude-code/phase2-groom** at 2026-09-29T05:39:01Z

Maintainer decision, 2026-09-29: anyone other than the author and moderators sees an auto-hidden post as a tombstone reading 'Post hidden pending review', accepting the grooming recommendation. Supersedes the open decision in the earlier note. Moved into the plan's Settled section.

**agent:claude-code/phase2-run-cp2pj** at 2026-09-29T23:00:28Z

Started by the Phase 2 run, server lane, on branch run/cp2pj-flags from main 2826208. git ticket ready hides this ticket because its dependency TKT-01M29CKRJ9 (Render tombstones for deleted and moved posts) is still open. The orchestrator authorized claiming it anyway: CKRJ9's server half, which is all this ticket needs (dto.Post.removal, dto.RemovalReview, revealRemoved, pageClass and the author:<id> cache class), merged in PR #170 (main 2826208). CKRJ9's client half, which renders the 'Post hidden pending review' tombstone text, is a separate PR still pending. This PR does not touch internal/web/spa/dist; the flag UI (post menu item and dialog) becomes a draft.

**agent:claude-code/phase2-run-cp2pj** at 2026-09-29T23:37:51Z

Work note, 2026-09-30, Phase 2 run.

AC8 is left unticked. The server serves an auto-hidden post to anyone but its author and moderators as a tombstone with `removal: "review"` and no html (TestAutoHiddenVisibility). The words "Post hidden pending review" are drawn by the client, in TKT-01M29CKRJ9's client half (`components/thread/removal.tsx` on its branch), which is a separate PR still pending. This PR may not change `internal/web/spa/dist`. The flag menu item and dialog are filed as draft TKT-01M3QQWA5Z (Add the flag item and dialog to the post menu).

Choices made while building, each within the plan's Settled items:
- A moderator's restore dismisses the post's open flags whatever hid it, not only an auto-hide. A post a moderator hid by hand, with open flags at the threshold, would otherwise be hidden again by the next sweep after the moderator restored it (TestModeratorRestoreDismissesFlagsOfAnyHide). An author's restore of their own delete dismisses nothing. The alternative, dismissing only for an auto-hidden post, lost because it lets the sweep undo a moderator.
- `CreateFlag` stays `:one ... RETURNING id`, with `ON CONFLICT ... DO NOTHING`, and a repeat answers `sql.ErrNoRows`, which `Flag` treats as success. The plan said `:execrows`; keeping `:one` kept the existing schema tests' shape and gives the queue an id.
- `ReopenFlags` skips a dismissed flag whose reporter has flagged the post again since. The new flag stands for them, and reopening the old one would break the partial unique index and fail the whole reopen. A flag's insert does not wait on the post's lock, so this case can happen (TestOneOpenFlagPerReporter).
- A flag on an archived thread is `conflict`: an archived thread takes no records, so no hide could follow. A flag on a removed or moved post is `conflict`.
- `Service.Writer` is now the one-method interface `threads.Appender`, which `*posts.Writer` satisfies, so that tests can fail or hold an append. `Service.afterFlagCount` is an unexported test hook, as `trace` is.
- serve builds the threads service at the jobs step instead of the listener step, because the job holds it. It runs `SweepAutoHide` once before the scheduler starts and logs a failure without stopping.
- `revealRemoved` now names "moderator" and "review" explicitly, and anything else (the author's own delete, or a removal this build does not name) goes to moderators only, which fails closed.
- TKT-01M29CRJ4E's recovery-marker rule does not bind anything here: this ticket writes no flag audit row. The auto-hide's recovery reads the open rows in `flags`, which are the durable record of the hide that is owed.

Mutations tried against the tests. Each was caught:
- no lock in EnforceAutoHide: caught by TestSweepCannotReHideDuringRestore (both held subtests);
- no lock in the restore: caught by the same;
- reopen always: caught by TestRestoreClearsAutoHideAndKeepsFlags and the projection-fails subtest;
- reopen never: caught by the restore-append-fails subtest;
- no dismissal on a moderator's restore: caught by three tests;
- threshold off by one: caught by four tests, TestReporterSeesOnlyTheReceipt among them;
- an auto Delete not setting auto-hidden: caught by four;
- a Restore keeping auto-hidden: caught by TestRestoreClearsAutoHideAndKeepsFlags and TestAutoDeleteSetsAutoHidden.

The first run of the two reopen mutations did not compile (an unused variable), so they were rerun in a form that compiles. Both are caught.

Checks run locally at the head before the PR: gofmt, go build, go vet, `go test -race ./...`, the property test at 5000 cases, sqlc regeneration, `go generate ./internal/web/spa` (dist unchanged; thread-view bundle 45330 B of 60000 B), `tsc --noEmit`, and vitest (1306 passed).

**agent:claude-code/phase2-run-cp2pj** at 2026-09-29T23:46:47Z

PR #175 (head 56551e5, base 2e2fe7c) was replaced by PR #176. The suspension PR #172 landed on main under it, and its Terva run 0ce7fc44 ended in error, 'Review superseded: the pull request moved during the run', so it is not a review. The same work is carried onto main at 5c9da6d as one commit, ea61688 on run/cp2pj-flags-2. The carry merges Delete.suspension beside Delete.auto (records.go, the projection audit writer, docs/03 and docs/04), keeps both background jobs in serve, and has TestRestoreDecidesAgainWhenTheLogMoves reach the Publish hook through *posts.Writer, because Service.Writer is now an interface. Checks after the carry: gofmt, vet, go test -race ./..., sqlc current, and dist unchanged (45334 B of 60000 B).

**agent:claude-code/phase2-run-cp2pj** at 2026-09-29T23:48:29Z

AC8 ticked, superseding the work note's reason for leaving it open. TKT-01M29CKRJ9's client half landed on main in PR #174 (main 4d793b2). Its removedText in components/thread/removal.tsx draws a tombstone with removal 'review' as 'Post hidden pending review', and ThreadPage.test.tsx asserts those words. This PR serves an auto-hidden post to anyone but its author and moderators as a tombstone with removal 'review' and no html (TestAutoHiddenVisibility), so the two together meet the criterion. The summary's line about AC8 is superseded by this note.

**agent:claude-code/phase2-run-cp2pj** at 2026-09-29T23:55:43Z

Terva review 1504 on PR #176 (head 1a463d7, base 4d793b2, run 3b328c27, Actions run 1114): two findings, both accepted and fixed. (1) high, 'Serialize flag insertion with restore dismissal'. Flag now takes the post's flag lock before it checks the post, and holds it through the insert and the hide. So a flag checked before a moderator's restore cannot be inserted after the restore dismissed the others. A flag that waits for the restore checks the restored post and counts from zero. Test: TestFlagHoldsThePostLock, with a flag during a held restore and a restore during a held flag. Removing the lock from Flag fails both subtests. (2) medium, 'Provide recovery when confirmation cannot resolve flags'. ConfirmHide on a post that is already a moderator's hide, no longer pending review, with open flags now resolves them and appends nothing, so asking again completes a confirmation whose resolve failed. The same post with no open flag is conflict. Test: TestConfirmHideRecoversAFailedResolve, which injects the resolve failure with a temporary trigger. Disabling the branch fails it. docs/08 is updated for both.

**agent:claude-code/phase2-run-cp2pj** at 2026-09-30T00:06:40Z

Terva review 1506 on PR #176 (head 607e96a, base 4d793b2, run 4f03568f, Actions run 1124): it marks both findings of 1504 resolved and raises three, all accepted and fixed. (1) high, 'Return the receipt for a reporter's repeat after auto-hide'. Under the lock, a flag on a post that is not shown now asks HasOpenFlag. A reporter whose flag is still open gets the same receipt, so a repeat does not reveal the hide. Anyone else still gets conflict. Tests: TestAutoHideAtThreshold and TestReporterSeesOnlyTheReceipt, where a repeat after the hide is byte-identical. (2) medium, 'Use a recoverable context when reopening flags after a failed restore'. The reopen runs on context.WithoutCancel(ctx) with a 10 s timeout. Test: the subtest 'restore cancelled at its append' of TestSweepCannotReHideDuringRestore. (3) medium, 'Respect archival when resolving an already-confirmed hide'. ConfirmHide checks archival before either branch. Test: TestConfirmHideRefusesAnArchivedThread. Mutations: reverting each fix fails the named test. docs/07 and docs/08 are updated.

## Summary

Flags and the auto-hide are built on the server (PR #176). POST /api/v1/threads/{id}/flags answers one receipt for every accepted report; migration 0014 keeps one open flag per reporter per post. At flags.auto_hide_threshold the system appends a Delete with auto and actor 0, which sets hidden-by-mod and the new index bit 6 (EntryAutoHidden), serves removal 'review' (html to the author and moderators, a tombstone to everyone else), and writes a post.auto_hidden audit row as the system. A failed hide answers unavailable with the flag kept; jobs.AutoHideSweep, every minute and at startup, recovers it and enforces a lowered threshold. A moderator's restore dismisses the post's open flags before its append and reopens them only if nothing was appended; the hide, the restore and ConfirmHide share one striped per-post lock. ConfirmHide has no route until the queue (TKT-01M29CRJ4E). Every criterion is met; AC8's words are drawn by TKT-01M29CKRJ9's client half (PR #174). The flag UI is draft TKT-01M3QQWA5Z.
