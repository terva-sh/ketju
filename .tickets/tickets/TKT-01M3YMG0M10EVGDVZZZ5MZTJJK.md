---
schema: 3
id: TKT-01M3YMG0M10EVGDVZZZ5MZTJJK
title: Drive Phase 4 to done autonomously
type: task
status: blocked
status_reason: "Waiting on the maintainer: docs/releases/v1.0-checklist.md part 2 (open items) and part 3 (secrets, pre-release tag, v1.0 tag). After the tag the run ticks TKT-01M3YSVEDJ criteria 1, 2 and 5 and closes this ticket."
priority: high
due_on: null
labels:
  - area:process
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:42:30Z
updated_at: 2026-10-03T19:43:06Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

Tracks the autonomous Phase 4 run that docs/15 describes.

On 2026-10-02 the maintainer said "Let's land our merges, rebase and then start burning this down". Asked what to include, they chose all four options:
- Phase 1 cleanup;
- the small unplaced tickets;
- a Phase 4 run;
- the profile and account drafts.

On the same day they answered the thread-tag questions (TKT-01M3M4KVN).

The run's procedure is docs/12, as docs/13 and docs/14 changed it, with docs/15's changes on top.

## Acceptance criteria

- [ ] Every phase-4 ticket other than the epics and this one is done or archived, or blocked on a question docs/15 says the maintainer must answer
- [ ] Every run decision, recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [ ] docs/11 counts are refreshed when the run ends

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T22:47:44Z

Run log, 2026-10-02 (orchestrator).

### Grooming merged
- #361 tags-archive (0483f8d6), #358 ops (c0997d3d), #360 client-account (e9a30a2e), #362 correctness (f5ed2d89), #359 moving posts (5b1a6da9), #375 exit check (60653e22).
- Maintainer decisions recorded: #363 no unarchive in v1 (2edadd7e); #365 metrics on a separate address, publishing (Forgejo and GitHub releases, scratch image, no signing), no re-auth for handle change or deactivation, avatars (30834b8e); #371 reply as a new thread: optional `continues` on ThreadCreated, the pre-filled quote is the member's own text (b31d0938).
- #364 was closed unmerged: the orchestrator rebased its pushed branch by mistake, and #365 carried it.

### Implementation merged
- TKT-01M3B12ZZ4 tag rules on create, #368 (6d38a742).
- TKT-01M29CEE7S metrics endpoint, #369 (2c7a2af4).
- TKT-01M3YME9DM API layering fence, #367 (cac3ed9d). Ticked TKT-01M29BKBMC criterion 1.
- TKT-01M3V9DWTN shortcut off switch, #370 (d0bb743c); reader 79859 B. Ticked TKT-01M3S3ZRYD criterion 2.
- TKT-01M3VVZE4G unsigned ordinal bounds and a 386 CI step, #366 (75695ad8).
- TKT-01M29CYSCP thread tags, #372 (06db6b6f); one e2e re-run (phone sign-in timeout).
- TKT-01M3YME9KG method-not-allowed, #374 (24f4f3d6); one e2e re-run (phone Unread badge timeout).
- TKT-01M3VRFRFW index entry checks on open and read, #376 (56636cac).

### Filed by lanes
TKT-01M3YSQPZX, TKT-01M3YSQQ33, TKT-01M3YSQQ5X (done), TKT-01M3YSVEDJ, TKT-01M3YSQ4CR, TKT-01M3YT04ST (no milestone), TKT-01M3YTHAZB, TKT-01M3Z50NKE (flaky search catch-up test, no milestone).

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T07:39:16Z

Run log, 2026-10-02 to 2026-10-03 (orchestrator), continuing the note of 2026-10-02.

### Implementation merged since
- TKT-01M29CXK35 ketju backup, #373 (ee77e54e); five review rounds, all fixed.
- TKT-01M29CXK3S release dry run, #381 (5f50eb27).
- TKT-01M3YSVEDJ release workflow, #385 (800748a0); done with criteria 1, 2 and 5 unticked until the maintainer's tag (v1.0 checklist). Secrets for the maintainer: RELEASE_FORGEJO_TOKEN, RELEASE_GITHUB_TOKEN, CONTAINER_PUSH_REPO_USER, CONTAINER_PUSH_REPO_PASSWORD.
- TKT-01M29CRJ64 archival, #378 (a7e30e7e); criteria 1 and 7 completed by #395 and ticked after close.
- TKT-01M3YME9AZ quote backlinks, #380 (bb6faf74); migration 0025. The clean review said replay safety could not be judged from the diff; the follow-up the lane requested never ran, and it merged on TestCatchUpBackfillsTheBacklinksAfterTheMigration.
- TKT-01M29CXJZZ split and merge records, #387 (c0563a59), carried from #383; migration 0026. Review 2018 was a security finding, fixed. Criterion 6's missing tests (blobs, moderator queue, live events) are written on TKT-01M29CYSAM's branch.
- TKT-01M3V7GE59 / shortcut, #377 (74105a52). TKT-01M3V6XTYG account screen, #382 (88b12b35). TKT-01M3V7CFJ5 admin focus, #389 (5bb0d3b4). TKT-01M3YSQPZX tags client, #391 (1198e2b8). TKT-01M3YTHAZB Quoted by, #394 (5469a729). TKT-01M3YSQQ33 archive client, #395 (ead49125). Reader bundle 83350 B.
- TKT-01M3YME9G0 served goldens, #390, #392, #393 (bd5fdc20): all 105 API routes.
- TKT-01M3ZE2211 Phase 3 exit flakes, #386 (8470d86d): two test waits and one product bug (a watch alerted to posts before it was set). TKT-01M3Z50NKE, #388 (2f2f914d).
- Orchestrator: #379 (log, ticks), #384 (placed the two flakes in Phase 4).

### Ticked after close
TKT-01M29BKBMC criterion 2, TKT-01M29BKBM7 criterion 3, TKT-01M29CRJ64 criteria 1 and 7.

### Process
- One lane pushed with --force-with-lease once (a fast-forward; nothing rewritten). Told plain pushes only.
- Disk reached 91% on 2026-10-02, mostly other projects' scratch; the run removed its finished Phase 3 scratch.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T15:26:00Z

in-progress to blocked: Waiting on the maintainer: docs/releases/v1.0-checklist.md part 2 (open items) and part 3 (secrets, pre-release tag, v1.0 tag). After the tag the run ticks TKT-01M3YSVEDJ criteria 1, 2 and 5 and closes this ticket.

**agent:claude-code/phase4-run-orchestrator** at 2026-10-03T19:43:06Z

Maintainer review of the v1.0 checklist's part 2, 2026-10-03, asked as choices:
- **Groups approved whole:**
  - A, screens and interaction;
  - B, data and storage, including how a move is written, watch timing, tag dedupe and sort, the tightened upload bound, and Phase 3's two.
- **Approved after reading one by one:**
  - C, access-adjacent: the backup control socket, moderator-by-group for reserved tags and retag, and redact on archived threads (the stale unarchive wording is fixed);
  - D, operations: linux amd64 and arm64; a backup omits config.toml and secrets/ (README now names client_secret_file too); the release mechanics; and the internal plan-only choices.
- **The avatar hash** sent to signed-out readers is accepted for v1.0 as a run decision. TKT-01M41MKE81 is filed to remove it.
- **PR #380 backlinks replay:** an independent full review was requested, as review-only PR #406.
- **Rollback past v1.0 with moves:** documented in the README and release notes "Upgrading".
- **Acknowledged:** the out-of-scope items and the one --force-with-lease push. v1.0 ships without the three items.

All 24 "pending maintainer review" markers in docs/02 to docs/08 now read "maintainer approved 2026-10-03" (docs/15 "Decided by the maintainer").

## Summary

The Phase 4 run (docs/15) is finished. Every other `phase-4` ticket is done: 33 of 34. The only work left is the maintainer's. `docs/releases/v1.0-checklist.md` (#404) is the release checklist and the full list of open items. This summary is the report.

### What shipped (2026-10-02 to 2026-10-03)

- **Moving posts.**
  - The split and merge records in the log: #387, migration 0026.
  - Split into a new thread: #397.
  - Merge into an existing thread: #398.
  - Reply as a new thread: #399, migration 0027.
- **Tags and archive.**
  - The tag rules: #368.
  - Thread tags and retag: #372.
  - The tags client: #391.
  - Archival with no unarchive: #378.
  - The archive client: #395.
- **Operations.**
  - Metrics on a separate `metrics_listen`: #369.
  - `ketju backup`: #373.
  - The release dry run: #381.
  - `release.yml`, the Dockerfile and the README install section: #385. These have not been run.
- **Client and account.**
  - The shortcut off switch: #370.
  - `/` opens search: #377.
  - Layout-effect focus on the admin screens: #389.
  - The account screen for handle and deactivation: #382.
  - The profile screen: #400.
  - The upload bound: #401, migration 0028.
  - Avatars: #402, migration 0029.
- **Correctness.**
  - Unsigned ordinal bounds, with a 386 CI step: #366.
  - Index entry checks: #376.
  - Quote backlinks: #380 and #394, migration 0025.
  - The API layering fence: #367.
  - A 405 for a wrong verb: #374.
  - Served goldens for all 105 routes: #390, #392 and #393.
- **CI health.**
  - The Phase 3 exit-spec flakes: #386. This found and fixed a product bug: a watch alerted to posts written before it was set.
  - The search catch-up test: #388.
  - The exit check, with the removed-post contrast fix: #404.
- **Grooming and decisions.**
  - Grooming: #358, #359, #360, #361, #362, #371 and #375.
  - Maintainer decisions: #363, #365 and #371.
  - Run logs: #379, #384 and #396.

**Measures.** The reader bundle went from 79213 B to 92266 B of the 120000 B budget. `cmd/ketju` under `-race` took 257 to 465 s across the last CI runs, against the 900 s bound. Every PR merged on a clean full review of its exact head, or on a carry of one. The longest took six rounds (#383 to #387, sixth round granted narrow). Security findings were fixed, never argued: review 2018 (an unchecked target thread on a move) and review 2063 (the upload bound bypassed by clearing an avatar).

### Decided by the maintainer during the run

- **Tag rules:** lowercase with folding, at most 24 characters and 8 tags, free text, and reserved tags.
- **Archive:** no unarchive in v1.
- **Metrics:** a separate, unauthenticated listener, off by default.
- **Publishing:** Forgejo and GitHub releases plus a `scratch` image, unsigned.
- **Account:** no recent-sign-in check for a handle change or deactivation.
- **Avatars:** visible to signed-in members only; PNG or JPEG up to 1 MB, re-encoded to 256 px.
- **Reply as a new thread:** an optional `continues` key on `ThreadCreated`, and the quote is the member's own text.
- **Bundle budget (2026-10-03):** the budget measures the wrong thing. This was filed as TKT-01M40YC2Z6 (#403), unscheduled.

### Waiting on the maintainer

All of it is in the checklist. In short:
1. **Settle part 2.** The run decisions pending review cover the backup socket, release platforms, watch timing, tag order and moderator scope, the settings screens, the shortcuts, the chips, the "Quoted by" line, the selection UI, how a move is written, the tightened upload bound, redact on archived threads, and `from`. Three Phase 3 decisions from 2026-10-01 are also still marked pending. No disagreements were recorded and no requirements were carried.
2. **The backlinks review gap.** #380 merged without the follow-up review it asked for on replay safety. It rests on `TestCatchUpBackfillsTheBacklinksAfterTheMigration`.
3. **Process.** One lane pushed with `--force-with-lease` once. It was a fast-forward and rewrote nothing.
4. **The rollback limit.** Do not downgrade past v1.0 with logs that hold a move. Records 20 and 21 are advisory, so an older binary's reindex would show cross-forum moved posts in the source.
5. **The release.** Create the four secrets (`RELEASE_FORGEJO_TOKEN`, `RELEASE_GITHUB_TOKEN`, `CONTAINER_PUSH_REPO_USER`, `CONTAINER_PUSH_REPO_PASSWORD`). Push a pre-release tag and check it, then push v1.0. After that the run ticks TKT-01M3YSVEDJ's criteria 1, 2 and 5 and closes this ticket.

### Left as found

- The runner record TKT-01M3VDYENN. CI also showed scattered one-off e2e timeouts under load (`flag.spec` "Loading…", the admin token panel, a serve start that stalled for 19 s), each passing on its one re-run.
- Disk on this machine reached 91% on 2026-10-02. Most of it is other projects' scratch (kuvabooru 32 GB, terva 17 GB) and the shared Go build cache (17 GB). The run removed only its own finished scratch.
- The remote branch `run/cxjzz-split-merge` (from #383, closed and carried to #387) is left for the maintainer to delete.
