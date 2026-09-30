---
schema: 3
id: TKT-01M3R10VP32VXJ35AK76NBHCWH
title: Prove a suspension's bulk hide cannot race the queue's restore
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29CRJ4E4AZ079KEHVGJ01DS
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:06:43Z
updated_at: 2026-09-30T04:21:39Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The moderator queue (TKT-01M29CRJ4E, PR #184) made the thread page's single-post Delete and Restore take the striped per-post flag lock, which the auto-hide, the moderator restore and ConfirmHide already share (TKT-01M29CP2PJ). A suspension's bulk hide (`/admin/users/{handle}/suspend` with hide_posts, and `/posts/hide`, TKT-01M29CP2QK) still appends its Deletes without taking that lock. The queue re-reads the post before it writes, which narrows the race to the write itself but does not close it.

The race: a moderator's restore from the queue and a suspension's hide of the same post can interleave, so the post ends in whichever state was appended last, and the flag rows may not match it (a restore dismisses the post's open flags before its append).

### What to decide

Whether the bulk hide takes the per-post lock for each post it hides (cost: one lock per post in a loop that may touch many threads), or whether the restore's append precondition already makes the outcome safe and only a test is missing. Read the flag lock in internal/service/flags and the hide loop in the suspension service before choosing.

Reported by the moderator queue lane on 2026-09-30.

## Acceptance criteria

- [ ] HidePostsOf and RestorePostsOf take no flag lock stripe, and the flagLocks, ResolvePost and closeFlags comments say why, naming the tests below
- [ ] TestSuspensionHidePassesOverAnAutoHiddenPost: the real HidePostsOf leaves a post hidden pending review untouched, with its flags open
- [ ] TestSuspensionHideWhileTheQueueRestores: in both orders the post and its flag rows end in a serial outcome, shown with flags dismissed or hidden by the suspension with flags dismissed
- [ ] TestStateChangeIsConflictNotSuccess calls the real HidePostsOf from afterQueueRead while the queue holds the stripe, and gains an unsuspension's-restore case
- [ ] docs/08 Suspension says the hide passes over a post hidden pending review
- [ ] TestSuspensionHideRacesTheQueueRestore: 20 concurrent runs under -race each end in outcome (a) or (b), or untouched when the Keep answered an error; in post 1's log no Delete follows a Delete without an intervening Restore; and the final post state and flag rows agree (shown or suspension-hidden with 0 open flags and one flag.dismissed row, or auto-hidden with every flag open and no row)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. The description points at two wrong places. The lock is `flagLocks` in `internal/service/threads/flags.go`, not in an `internal/service/flags` package. The bulk hide is `HidePostsOf` in `internal/service/threads/suspension.go`.

### Settled in grooming

- **Decision: the bulk hide does not take the lock. Its append precondition already makes the outcome safe, and this ticket adds the tests that prove it** (engineering choice, made after reading both paths). The title changes to match.
  - `HidePostsOf` skips a post that is not `Visible()`. It appends each `Delete` with `Refuse: gone` (deleted, hidden-by-mod, moved, redacted; `mutate.go`) and `AtLength: at`. The actor checks both when the record lands (`threadlog.Actor.frameFor`). A record from anyone else after `at` fails the append with `ErrLogMoved`, and `eachAuthorThread` judges the thread again, up to `threadAttempts`.
  - The queue restores only a post hidden pending review. `queueRestore` in `queue.go` appends a `Restore` with `Require: threadlog.EntryAutoHidden, Refuse: EntryMoved`. An auto-hide sets hidden-by-mod as well (`postFlagEffect` in `threadlog/apply.go`: `deleteAuto` sets `EntryHiddenByMod | EntryAutoHidden`). So the hide can never append on a post the queue could restore until that Restore has landed, and it never clears the auto-hidden bit the Restore requires.
  - So there are two possible end states, and both are serial orders. (a) The hide passed the post before the Restore: the post is shown and its flags are dismissed. (b) The Restore landed first and the hide, retried after `ErrLogMoved`, hid the post: the post is hidden by the suspension and its flags are dismissed. Taking the lock allows exactly these two outcomes, no more and no fewer.
  - The window where the flags are already dismissed and the post is still auto-hidden cannot be seen by the hide, which never reads flags.
  - The auto-hide and the hide both refuse `gone`, so exactly one `Delete` lands. When the hide wins, the post is a moderator's hide with its flags still open. The sweep skips it (`moderation.sql` filters on `deleted = 0`, and `EnforceAutoHide` checks `Visible`).
  - The one place the order can still show is `closeFlags` (dismiss or resolve on a shown or hand-hidden post). It checks the post state inside `Threads.With` and then commits, and `With` is not exclusive, so a hide or an unsuspension's restore can land between the check and the commit. The result equals "close, then hide". The flags and the post agree with that order, and the lock would change only which of two valid orders is recorded.
- **Why taking the lock lost** (recorded so nobody reopens it):
  - `flagLocks`' own rule (`flags.go`) is "A caller holds one stripe at a time and takes it before the thread, never inside Threads.With". The hide decides which posts to hide inside `With`, so taking stripes would mean restructuring it into one `With` and one append per post. That is a new write path for the one operation docs/08 describes as idempotent and resumable.
  - Posts share stripes, so the loop would have to release each stripe before it takes the next, or it would deadlock on itself.
  - A wait on a stripe inside the `AtLength` loop makes `ErrLogMoved` retries likelier against the limit of 16, which turns into `ErrSomeThreadsUnavailable` for the moderator.
  - `lockAccount` (`internal/service/account/suspension.go`) is held across the whole hide. Adding stripes would create an account-then-stripe order for the first time.
  - It would buy no outcome that the preconditions do not already guarantee.
- **An auto-hidden post of a suspended author is left to the queue** (docs/08 "Suspension": "Each of the account's **visible** posts gets a moderator's Delete"). The hide skips it, and a moderator's Keep in the queue may show it while the author is still suspended. That is a moderator's explicit decision about that post, not a race, and the lock would not change it. Converting it to a suspension hide lost: the unsuspension's exact restore would then put back a post that flags had hidden.
- **Suspension rules are the maintainer's** (decisions 2026-09-29, docs/08 "Decided 2026-09-29" and docs/05 "Account lifecycle"). When a suspension expires, the posts it hid stay hidden until a moderator restores them. This ticket changes none of it.

### Approach

1. No change to `HidePostsOf`, `RestorePostsOf` or `flagLocks`' behaviour.
2. Correct the comments that call the bulk hide an unclosed gap.
   - `flagLocks` in `flags.go` gains a paragraph: a suspension's hide and restore walk many posts and do not take the stripe. Their `Refuse`/`Require` preconditions and `AtLength` make each record atomic against the post's state, and neither can touch a post hidden pending review. It names the tests below.
   - The `ResolvePost` comment in `queue.go` ("what can still land outside it is a suspension's hide or restore") and the `closeFlags` comment ("which this check narrows to the write itself") say that the outcomes are serial orders and name the tests.
3. docs/08 "Suspension", in the bullet on hiding, gains one sentence: the hide passes over a post hidden pending review, which stays in the moderator queue.
4. Add the tests below to `internal/service/threads/queue_test.go`, using `newQueueWorld`, `w.hide`, `w.writer.set` (`faultWriter`) and `afterQueueRead`. Each calls the real `HidePostsOf` with a suspension id, not a hand-built `Delete`.

### Tests

- `TestSuspensionHidePassesOverAnAutoHiddenPost`: the author has post 1 auto-hidden (`w.hide(1)`) and post 2 shown. `HidePostsOf(mod2, author, 1)` answers 1. Post 2 carries a `Delete` with `Suspension: 1`. Post 1 is still auto-hidden with its flags open, and it has no second `Delete`.
- `TestSuspensionHideWhileTheQueueRestores`, the ordered cases, deterministic:
  - (a) The queue's Keep holds its `Restore` in `w.writer.set` until `HidePostsOf` has returned. The hide does not wait on the queue, answers 0 for post 1, and writes nothing on it. After the release, post 1 is shown, 0 flags are open, and there is one `flag.dismissed` row.
  - (b) The queue's Keep completes, then `HidePostsOf` runs. Post 1 is hidden-by-mod and not auto-hidden, its `Delete` in force carries suspension 1, 0 flags are open, and there is one `flag.dismissed` row.
- `TestSuspensionHideRacesTheQueueRestore`, under `-race`: 20 iterations, each with a fresh world. A Keep through `ResolvePost` and `HidePostsOf` start together on a released barrier. Every end state must be (a) or (b) above. The one exception is a Keep that answered an error, which must leave the post untouched. For every iteration the test asserts:
  - **The log alternates.** Walking post 1's records in log order, no `Delete` follows another `Delete` without a `Restore` between them. Outcome (b) is auto-hide `Delete`, `Restore`, then the suspension's `Delete`, which passes. Two `Delete`s in a row fail. (Review 1540 on PR #203: a flat "no second Delete" would refuse the valid outcome (b).)
  - **The post and its flag rows agree** (`w.entry(1).Flags`, `w.openFlags(1)`, `w.flagRows()`):
    - shown: 0 open flags, and exactly one `flag.dismissed` row, from the Keep;
    - hidden by the suspension: hidden-by-mod without auto-hidden, the `Delete` in force carrying `Suspension: 1`, 0 open flags, and exactly one `flag.dismissed` row;
    - hidden pending review: every flag still open and no `flag.dismissed` row. This arises only if the Keep failed, and the Keep's answer must then be an error.

    Any other combination fails, with the iteration and the log's record types in the message. Examples: flags open beside a `flag.dismissed` row, auto-hidden with 0 open flags, or shown with flags open. The count stays at 20 because of TKT-01M3R5P3CN (Keep the Go race tests inside their timeout under CI runner load).
- `TestStateChangeIsConflictNotSuccess` (existing): its "a suspension's hide" case calls the real `HidePostsOf` from `afterQueueRead` in place of the hand-built `Delete`. The call returns while the queue still holds the stripe, which proves that the hide takes no stripe and cannot deadlock against the queue. The assertions stay as they are: `conflict`, flags left open, one `Delete`, and the audit row.
- A new case in the same table, "an unsuspension's restore": post 1 is hidden by `HidePostsOf` and then flagged. `RestorePostsOf` runs from `afterQueueRead` during a Dismiss. The answer is `conflict` and the flags are left open, or the order is dismiss then restore. The post is never shown with a `flag.dismissed` row saying "hidden".

### Out of scope

- Serialising `closeFlags` against the bulk operations. The outcome is a valid serial order; see Settled.
- The suspension controls on the admin screen (TKT-01M3QMRE47).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T03:57:59Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Decided: the precondition already makes it safe, so the bulk hide does not take the lock, and the ticket adds the tests that prove it. Retitled from 'Take the per-post flag lock in a suspension's bulk hide'. The lock is flagLocks in internal/service/threads/flags.go, and the hide is HidePostsOf in internal/service/threads/suspension.go; the description named internal/service/flags, which does not exist. The reasoning and the lock's costs are in the plan's Settled section.

**agent:claude-code/phase2-groom** at 2026-09-30T04:21:39Z

Review 1540 on PR #203 (head 22de825), medium, accepted: the race criterion forbade any second Delete, which outcome (b) needs. It now requires that no Delete follows a Delete without an intervening Restore, and that the post state and flag rows agree in every interleaving. The plan's race test says the same.
