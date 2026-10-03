---
schema: 3
id: TKT-01M3ZE2211GDC23K09PZ1GJ8BQ
title: Stop the Phase 3 exit Unread surface test failing under CI load
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:ci
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:386
    path: null
claim: null
archive: null
created_at: 2026-10-02T23:09:15Z
updated_at: 2026-10-03T01:18:18Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-ze2211
  name: ""
extensions: {}
---

## Description

`e2e/phase3-exit.spec.ts:151` ("a member finds a thread with new posts on the Unread surface…") failed in CI on two unrelated Phase 4 PRs on 2026-10-02, once at each width. Each lane used its one allowed re-run, and the re-run passed.

- **Run 2491** (PR #374, method-not-allowed; phone): `[data-unread]` read "1 new" where the test expects "2 new", until the 5 s timeout.
- **Run 2522** (PR #373, backup; desktop): the thread's row on /unread had no `[data-unread]` at all within 5 s.

Neither PR touches the unread surface, lists, or read positions.

The test starts a thread as carol, sets alice's read position to 1, has carol post two replies, then opens /unread straight away. Both failures look like the list being read before every reply is reflected in what /unread serves: one reply counted, then none. That points at a lag between a reply's acknowledgement and the list's projection or unread count under CI load, or at the read-position write racing the replies.

Find which, and fix the cause:
- If the product can serve /unread a stale count after a reply is acknowledged, that is a product bug and the fix goes there, with docs/06's promise in mind.
- If the lag is allowed by the design, the test must wait for the state it asserts (for example, poll the API until the thread's post count is 3) rather than assume it.

Do not raise the timeout without knowing which.

## Acceptance criteria

- [x] The cause of runs 2491 and 2522 is named in a note: a product lag, or a test that asserts before the state exists
- [x] The fix addresses that cause, and the test passes 20 times in a row under go test -race load locally

## Implementation plan

### Causes

Each failure was reproduced locally by delaying the projection's and the notification generator's bus deliveries by 400 ms (a local patch, not committed): 18 of 20 runs of the two tests failed, with exactly CI's messages.

- **`:151` (runs 2491, 2522, 2573): the test asserts before the state exists.** A plain reply is answered once its log record is durable; the database projection follows the event bus (docs/03 "Write path", step 5, "asynchronous"), and only a new thread or a reply with attachments is projected before the answer (`threads.Service.Reply`, `CreateThread`). /unread reads the projection, so straight after carol's two 201s it can count one reply ("1 new") or none (no row, since the read position is 1). The screen fetches the list once. The design allows the lag, so the product does not change.
- **`:186` (run 2571): a product race in the notification generator.** The generator reads watch levels when it processes a post, not as they stood when the post was written. alice watched the thread a moment after carol started it; when the generator processed #1 after the watch, alice got `thread_you_watch` for #1, and the one-unread-watch rule (docs/04) then kept that row on #1 when #2 arrived. The badge still counted 2, so the test reached the link and landed on /1. Under a backlog the same race alerts a member to posts written before they watched.
- **`:446` (run 2558, filed as TKT-01M3ZGWX60, folded in here by the orchestrator): the test asserts before the state exists.** `page.goto(...?page=2)` returns at the load event; the thread page draws its posts when its API answer arrives after that, so reading the ordinals once found none (`undefined`).

### Changes

1. `web/client/e2e/phase3-exit.spec.ts`:
   - `:151` polls alice's `GET /api/v1/unread` until the thread's row counts 2 unread (15 s, as the alerts test already polls generation), then opens the screen.
   - `:446` polls the first ordinal on page 2 instead of reading it once.
   - `:186` is unchanged; the product fix makes it deterministic.
2. Product, a run decision (member-visible, default from the meaning of "a thread you watch"): a watch alerts only to posts written at or after the member chose it.
   - `ThreadWatchers` also selects `updated_at`; `generate` offers `thread_you_watch` only when the record's timestamp is at or after the `updated_at` of the row that decides the level (the thread's own, else the forum's).
   - `SetThreadWatch` and `SetForumWatch` move `updated_at` only when the level changes, so setting the same level again does not restart the watch. Nothing else reads `updated_at`.
   - docs/04 "notifications" and "watches" record the decision and its alternatives. No migration, no log format change, and nobody can read more than before: the rule only removes alerts.
3. Tests: `TestAWatchAlertsFromWhenItWasSet` (notifier), `TestWatchTimeIsWhenTheLevelWasChosen` (db).

### Not changed

- No timeout was raised. The 15 s poll waits on a stated state, not a guess.
- Reply is not projected before its answer: that would put the SQLite write path into every reply's latency, against docs/03's choice.

### Verification

- Before: with the 400 ms injected lag, 18 of 20 runs failed (`:151` "2 new" not found or "1 new", `:186` on /1).
- After: with the same injection, 40 of 40 passed; without it, 40 of 40 passed beside `go test -race -p 4 ./...`.

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T00:30:42Z

Placed in Phase 4 by the run (orchestrator, 2026-10-02; the maintainer may move it back out). docs/15 keeps found work out of the milestone unless it blocks a Phase 4 exit criterion. This one does: TKT-01M3YMG0NX needs green CI on the exit tests, and the Phase 3 exit spec now fails under load often enough to stop that. docs/14's run placed a CI flake (TKT-01M3TSZHSW) the same way.

More evidence since filing, all in `e2e/phase3-exit.spec.ts`, each on a change that does not touch the surface:
- **Run 2571** (PR #382, account settings; phone): `:186` (mention and watched-thread alerts). After Back to the alerts list, the watched-thread alert landed on `/1` instead of `/2`, then timed out on `toHaveURL`.
- **Run 2573** (main push of 4936e55c, ticket-only): `:151` on phone again, "1 new" where "2 new" was expected.

So the scope is the Phase 3 exit spec under CI load, not only the Unread test. `:151` has now failed three times and `:186` once. Name the cause of each before changing a timeout.

**agent:claude-code/phase4-run-ze2211** at 2026-10-03T01:17:27Z

Causes named (criterion 1), each reproduced locally before it was fixed.

- **Runs 2491, 2522 and 2573 (`:151`): the test asserted before the state existed.** carol's replies are answered once their log records are durable. The database projection that /unread reads follows the event bus, which docs/03 "Write path" step 5 makes asynchronous, and only a new thread or a reply with attachments is projected before its answer (`threads.Service.CreateThread`, `Reply`). In 2491 and 2573, /unread was asked for 0.9 s and 0.2 s after the second 201, while the search indexer was still behind on the replies (its "behind" and "caught up" lines bracket the request), and it counted one reply (2491: "1 new") or none (2573: no row, since alice's position was 1). 2522's evidence kept no serve log; its symptom is 2573's. The screen fetches the list once. The design allows this lag, so the fix is in the test: it polls alice's `GET /api/v1/unread` until the row counts 2.
- **Run 2571 (`:186`): a product race in the notification generator.** The generator read watch levels when it processed a post, not as they stood when the post was written. alice watched the thread 160 ms after carol started it; the generator processed #1 after the watch and alerted her to it, and the one-unread-watch rule then kept the row on #1 when #2 arrived. The badge still counted 2, so the test went on and landed on /1. The fix is in the product (run decision in docs/04): a watch alerts only to posts written at or after the member chose it.
- **Run 2558 (`:446`, filed as TKT-01M3ZGWX60 and folded in here): the test asserted before the state existed.** `page.goto` returns at the load event and the thread page draws its posts when its API answer arrives afterwards; `ordinals` reads the DOM once. The fix polls it. `lenses.spec.ts`'s callers of the same helper each wait for the lens header or count first, which comes from the same answer, so they are not exposed.

Reproduction: a local patch (not committed) delayed the projection's and the generator's bus deliveries by 400 ms. Before the fix, 18 of 20 runs of `:151` and `:186` failed with CI's exact messages ("2 new" not found, or the alert on /1). For `:446`, delaying `/api/v1/threads/*` by 800 ms through `page.route` failed 10 of 10 before and passed 10 of 10 after. CPU contention alone (two cores shared with three busy loops) and `go test -race -p 4 ./...` alongside did not reproduce either failure in 72 runs.

**agent:claude-code/phase4-run-ze2211** at 2026-10-03T01:17:33Z

run decision: pending maintainer review (2026-10-02). A watch alerts only to posts written at or after the member chose it.

The `:186` failure was not the test's: the generator read watch levels when it processed a post, so whether a member was alerted to a post written just before they watched depended on how far behind the generator was. The rule follows from what "a thread you watch" means, and it only removes alerts. It changes no format, adds no migration, and widens nobody's access, so it is not one of docs/15's stop classes.

- The member's moment is the `updated_at` of the `watches` row that decides their level: the thread's own row when it has one, else the forum's. The post's moment is its record's timestamp, the same service clock the watch routes stamp with.
- `SetThreadWatch` and `SetForumWatch` now move `updated_at` only when the level changes, so setting the same level again does not restart the watch. Nothing else read `updated_at`.
- Alternatives that lost: the level as it stands when processed (the old behaviour; alerts depend on generator lag, including after a restart's backlog); skipping posts at or below the read position (a member who never opened the thread is still alerted to #1); the earlier of the thread's and the forum's rows (a thread row that is `watch` now may have been `mute`, which the row does not record).

Recorded in docs/04 "notifications" and "watches", in the same PR.

## Summary

Fixed the cause of every Phase 3 exit spec failure seen under CI load. No timeout was raised.

- `:151` (runs 2491, 2522, 2573): the test asserted before the state existed. A reply's 201 comes before the asynchronous projection that /unread reads (docs/03 "Write path" step 5). The test now polls alice's `GET /api/v1/unread` until the thread counts 2 unread.
- `:186` (run 2571): this was a product race, and the product is fixed. A watch alerts only to posts written at or after the member chose it (run decision, docs/04, pending maintainer review). The generator compares each post's timestamp with the `updated_at` of the deciding `watches` row. The watch upserts move `updated_at` only when the level changes.
- `:446` (run 2558, TKT-01M3ZGWX60, archived into this ticket): the test asserted before the state existed. It now polls page 2's first ordinal.

Evidence: with a local 400 ms delay on the projection's and the generator's deliveries, 18 of 20 runs failed before and 40 of 40 passed after. Delaying `/api/v1/threads/*` by 800 ms made `:446` fail 10 of 10 before and pass 10 of 10 after. With no delay, 40 of 40 passed beside `go test -race -p 4 ./...`, which passed too.

Later tickets should know that `watches.updated_at` now means "when the member chose this level", and that `ThreadWatchers` returns it.
