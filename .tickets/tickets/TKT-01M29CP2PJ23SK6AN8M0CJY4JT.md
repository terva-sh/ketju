---
schema: 3
id: TKT-01M29CP2PJ23SK6AN8M0CJY4JT
title: Implement flags with the auto-hide threshold
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Any member can flag a post with a reason of spam, abuse, off-topic, or other with text. A post reaching the threshold of open flags, default 3 and a setting, is auto-hidden pending review through a Delete variant marked `auto=true`, which a moderator's Restore clears.

Reporters see "thanks, a moderator will look" and nothing else. Telling a reporter what happened to their report turns moderation into a conversation with the reporter, and in a small community that is how grudges start.

## Acceptance criteria

- [ ] When a post's open flags reach flags.auto_hide_threshold, that request appends one Delete with auto: true and frame actor 0, the entry gets hidden-by-mod and auto-hidden, and a post.auto_hidden audit row is written
- [ ] A moderator's Restore clears hidden-by-mod and auto-hidden, and the post's open flags become dismissed rows that stay in the flags table
- [ ] POST /threads/{id}/flags answers the same body for a new flag, a duplicate and the flag that triggers the hide, and no member-facing response carries a flag count or a reporter
- [ ] A member's second open flag on the same post adds no row (partial unique index), so the open count counts distinct reporters
- [ ] While auto-hidden, the author and moderators receive the post's html with removal review, everyone else gets a tombstone without html, and the author's restore is forbidden
- [ ] A page cached before the auto-hide is not served after it (TestAutoHideMovesTheResponseCacheKey)
- [ ] A threshold-crossing flag whose hide append fails stays committed and answers unavailable, and the next AutoHideSweep run hides the post (TestAutoHideRecoversFromAFailedAppend)
- [ ] Anyone other than the author and moderators sees an auto-hidden post as a tombstone reading 'Post hidden pending review'
- [ ] A restore of an auto-hidden post and AutoHideSweep hold one per-post lock, flags are dismissed before the Restore append and reopened if it fails, and a concurrent sweep never re-hides the restored post (TestSweepCannotReHideDuringRestore)
- [ ] Flags dismissed for a restore are reopened only when the Restore append itself fails; after a durable append whose projection fails they stay dismissed and no sweep re-hides the post

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

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

### Approach

1. **Migration** (the next free number): `CREATE UNIQUE INDEX flags_one_open_per_reporter ON flags (thread_id, ordinal, reporter_id) WHERE status = 'open'`. A member's second open flag on a post then adds no row, and the open count is a count of distinct reporters. Re-flagging after a dismissal is allowed. `CreateFlag` becomes `INSERT … ON CONFLICT (thread_id, ordinal, reporter_id) WHERE status = 'open' DO NOTHING`. `INSERT OR IGNORE` lost because it would also swallow the CHECK on `other`. New queries: `DismissOpenFlagsForPost` and `ResolveOpenFlagsForPost`, which set `status`, `resolved_by`, `resolved_at` and `resolution_note` on the post's open rows. Regenerate dbq.
2. **Format** (`internal/store/threadlog`): `DeleteBody` gains `Auto bool`, with cbor and json tags `auto,omitempty`. Add `EntryAutoHidden EntryFlags = 1 << 6` in `flags.go`, and update `EntryFlagsKnown`, the reserved set and the comments. In `postFlagEffect`, a Delete with `Auto` sets `EntryHiddenByMod|EntryAutoHidden`. A Delete with `ByModerator` (TKT-01M29CRJ5J) sets `EntryHiddenByMod` and clears `EntryAutoHidden`. A Restore clears all three bits. `EntryAutoHidden` means something only together with `EntryHiddenByMod`.
3. **Service**, new `internal/service/threads/flags.go`:
   - `Flag(ctx, v, id, ordinal, reason, note)`. Requires a member session (`MayWrite`) and read access through `access`. Refusals: a `reason` outside the four, `other` without a note, or a note over 1000 characters is `validation-failed`; an ordinal past the end is `not-found`; a post already removed or moved is `conflict`.
   - In one `db.Write`, insert the flag. After it commits, call `EnforceAutoHide(ctx, id, ordinal)`.
   - **`EnforceAutoHide` is idempotent and reads only durable state** (revised for Terva review 1343 on PR #138, finding 1). It reads `OpenFlagCount` and `settings.FlagAutoHideThreshold`. If the count is at or over the threshold and the post's entry is visible, it appends `Delete{Reason: "hidden pending review", Auto: true}` with `Author: 0` and `Refuse: gone`, through TKT-01M29CKRGS's `appendEvent`, which projects synchronously. The projection writer writes the audit row from the record (TKT-01M29CKRGS step 1, revised for Terva review 1352): action `post.auto_hidden`, `actor_id` NULL, target `audit.Post(id, ordinal)`, details `{thread, log_offset}`. The flag count stays out of the log and the audit row; moderators read it from `flags`. `ErrPostState` means the post is already gone, and nothing more is done. The audit log is staff-only (TKT-01M29CP2R4). It runs on every flag at or over the threshold, not only the one that crosses it, so a retried or later flag also retries the hide. It holds the post's lock from step 4 from reading the count through the append.
   - **A failed hide is recovered without waiting for another report.** The committed rows in `flags` are the durable record that a hide is owed, so no outbox is needed. The new job `internal/jobs.AutoHideSweep`, registered with the existing `jobs.Scheduler` with `Every: time.Minute` and run once when `serve` starts, reads `PostsAtFlagThreshold`. That query takes open flags grouped by `(thread_id, ordinal)` having `count(*) >= threshold`, joined to `post_meta` where `deleted = 0` to skip posts already down, through the `flags_post` index. The job calls `EnforceAutoHide` for each. If the append fails, the flag request answers `unavailable`, and the flag stays committed, so the post is hidden by the sweep within a minute at worst. Two other orders lost. Appending before the commit could hide a post on a flag whose insert then fails. An outbox table would duplicate what `flags` already says. A lowered threshold is enforced by the same sweep, which is docs/08's rule: a post with at least N open flags is hidden.
   - The answer is the same for a new flag, a duplicate and a flag that caused the hide.
4. **A moderator's review, serialized with the hide** (revised for Terva review 1352 on PR #138, finding 3). A restore, a confirm and `EnforceAutoHide` on the same post must not interleave, or the sweep could read the still-open flags after a Restore lands and hide the post again before they are dismissed. So all three hold one in-process lock per post, `s.flagLocks.Lock(id, ordinal)`: a striped mutex keyed on the thread and ordinal. One lock covers every writer, because `ketju serve` is the only writer process and the data-directory lock guarantees there is one (docs/03 "Write path"). Under it:
   - **Restore of an auto-hidden post** (TKT-01M29CRJ5J's moderator path) first runs `DismissOpenFlagsForPost` in one `db.Write` and keeps the ids it closed. Then it appends the `Restore` by calling `s.Writer.Append` directly, not `appendEvent`, so that the append's outcome is known on its own (revised for Terva review 1365 on PR #138). Only when `s.Writer.Append` itself returns an error, meaning nothing reached the log, does it reopen exactly those ids (`ReopenFlags`: `status = 'open'`, resolution fields cleared, `WHERE id IN (…) AND status = 'dismissed'`) and answer the error. Once the append has returned a `Result`, the Restore is durable and has happened. The service then projects with `s.Project`, and if that fails it answers `internal` and does not reopen anything: projection catch-up applies the Restore later, and the flags stay dismissed beside a post the index already shows as visible. The flags therefore never sit open beside a durable Restore, so the sweep has nothing to act on. `EnforceAutoHide` reads visibility from the index, which the actor updates at append, not from the projection. If the reopen also fails, the post stays hidden with its flags dismissed. That fails closed, and the moderator, who got an error, retries the restore. The later flag counts from zero.
   - **ConfirmHide(ctx, v, id, ordinal, reason)** is a moderator's `Delete{ByModerator: true}` with `Require: EntryAutoHidden`, `Refuse: EntryDeleted|EntryMoved`. It turns the pending hide into a moderator's hide and then runs `ResolveOpenFlagsForPost`. The sweep acts only on visible posts, so a failure between the two leaves nothing to re-hide.
   - Two alternatives lost. Dismissing after the append, without the lock, is the interleaving review 1352 found. Stamping each auto-hide with the newest flag id it counted, so that the sweep ignores flags older than a later Restore, would put flag ids into the log and still need a read of the log to compare against.
   - The moderator queue (TKT-01M29CRJ4E) gives restore and confirm a UI.
5. **Route** in `internal/web/api/viewerstate.go`: `POST /api/v1/threads/{id}/flags`, answering 200 `dto.FlagReceipt{Received: true}` (`{"received": true}`) with `no-store`, charged to the write limiter. Add the `dto` type and its golden.
6. **Client**: a "Flag" item in the post menu (`components/thread/Post.tsx`) for a signed-in member, on a post that is not removed. A dialog offers the four reasons and a note, required for `other`. On success it says "Thanks, a moderator will look." and nothing else. Once TKT-01M29CKRJ9's `removal` field exists, `removal: "review"` renders "Hidden pending review" to the author and to moderators, and the tombstone for everyone else reads "Post hidden pending review" (settled above). Regenerate `dist/`.
7. **Docs**: docs/03 record table: the `Delete` body gains `auto`, and the index entry flags row gains `auto-hidden (6)` with reserved bits 7 to 15. docs/07 gets the flag route row filled in. docs/04 "flags" notes the partial unique index.

### Tests

- `TestAutoHideRecoversFromAFailedAppend`: with the writer failing the append once, the threshold-crossing flag answers `unavailable` and its row is committed. One `AutoHideSweep` run then hides the post, and a second run appends nothing. A threshold lowered to 2 is enforced by the next sweep on a post with 2 open flags.
- `TestAutoHideAtThreshold`: with the threshold at 3, three members flag post 4. The third request appends one `Delete` with `auto: true` and frame actor 0. The entry has hidden-by-mod and auto-hidden. One `post.auto_hidden` audit row is written. A fourth flag appends nothing.
- `TestDuplicateFlagDoesNotCount`: one member flags twice. There is one row, the open count is 1, and both answers are byte-identical.
- `TestReporterSeesOnlyTheReceipt`: the answers to the first, the duplicate and the threshold-crossing flag are identical. No endpoint a member can call reports flag counts or reporters.
- `TestAutoHiddenVisibility`: the author gets `html` with `removal: "review"`, a moderator gets `html`, and another member and an anonymous viewer get a tombstone with no `html`. The author's restore is `forbidden`.
- `TestRestoreClearsAutoHideAndKeepsFlags`: a moderator's `Restore` clears both bits. The three rows are `dismissed` with `resolved_by` set, and a new flag counts from 1.
- `TestSweepCannotReHideDuringRestore`: with the Restore append held at a barrier inside the lock, run `AutoHideSweep` concurrently. It waits for the lock, then finds no open flags and appends nothing, and the post ends visible. With the Restore append made to fail, the dismissed flags are reopened and the post stays hidden. With the append succeeding and the synchronous projection made to fail, the call answers `internal`, the flags stay dismissed, the index shows the post visible, the next sweep appends nothing, and catch-up projects the Restore. Run under `-race`.
- `TestConfirmHide`: the entry keeps hidden-by-mod and loses auto-hidden, the rows become `resolved`, and the author still sees `removal: "moderator"`.
- `TestPostFlagEffectIsWhatApplyDoes` and a reindex test cover `auto`, and a full index rebuild reproduces bit 6.
- **Response cache invariant**, `TestAutoHideMovesTheResponseCacheKey`: a member's cached page is a miss after the hide, and the post is then a tombstone without content.
- Migration: the partial unique index refuses a second open flag from one reporter and allows one after dismissal.

### Out of scope

- The moderator queue, flag counts for moderators, and resolve and dismiss without a restore: TKT-01M29CRJ4E.
- Moderator deletes (`by_moderator`) and `RequireAny` for Restore: TKT-01M29CRJ5J. Tombstone rendering and the author viewer class: TKT-01M29CKRJ9.
- The non-JS form route `POST /t/{id}/posts/{ordinal}/flag`: Phase 5 (docs/07 "Path to a non-JS client").
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
