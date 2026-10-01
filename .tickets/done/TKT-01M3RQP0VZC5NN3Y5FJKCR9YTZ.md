---
schema: 3
id: TKT-01M3RQP0VZC5NN3Y5FJKCR9YTZ
title: Drive the rest of Phase 2 to done autonomously
type: task
status: done
status_reason: null
priority: high
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:241
    path: null
  - ref: pr:244
    path: null
  - ref: pr:252
    path: null
  - ref: pr:263
    path: null
claim: null
archive: null
created_at: 2026-09-30T08:42:46Z
updated_at: 2026-10-01T05:14:18Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

The maintainer extended the Phase 2 run on 2026-09-30, after its exit gate closed (TKT-01M3NTSCJQ, TKT-01M3NTSCMN): "Let's start grooming and promoting the phase 2 tickets so we can continue autonomously". Asked how far the run should go, the maintainer chose "All Phase 2": the terms of 2026-09-29 continue until every `phase-2` ticket is done, including tickets the run files and grooms along the way.

docs/13 carries the extension. This ticket tracks the run: the orchestrator logs merges here in batches and writes the maintainer's report in its summary when the run ends.

## Acceptance criteria

- [ ] Every phase-2 ticket other than the two epics and this one is done or archived
- [ ] The two Phase 2 epics are closed once their children are
- [x] Every recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [x] docs/11 counts are refreshed when the run ends

## Notes

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T16:41:45Z

Merges of the extended run, written by the orchestrator in one batch. Every PR merged on the gate, with a clean review on its exact head.

**Grooming**
- #208: extends the run to all of Phase 2, per docs/13 "The extension of 2026-09-30". Files TKT-01M3RQP0TG and this ticket.
- #209, #210 and #211: refreshed the plans for 25 tickets against main.
- #225: plans for TKT-01M3S1TVPD and TKT-01M3RZXGBA.
- #234: raises the reader budget to 70000 B, per the maintainer's decision of 2026-09-30.

**Server**
- TKT-01M3RQP0TG (Let forum moderators by grant lock, pin, move and set slow mode): #214.
- TKT-01M3K9XBJA (Autosave new-thread drafts with their title): #215, migration 0018.
- TKT-01M3R5P3CN (Keep the Go race tests inside their timeout under CI runner load): #216. `internal/perm` under `-race` dropped from 113 s to 34 s.
- TKT-01M3R10VP3 (Prove a suspension's bulk hide cannot race the queue's restore): #217. The race test found no race.
- TKT-01M3R33SR5 (Bound the post edit and delete windows before making Durations): #218.
- TKT-01M3RBMVMJ (Stop the seal round-trip test failing when ciphertext contains abc): #212.

**Client**
- TKT-01M29CXK19 (Build the admin forum management screens): #221, carried from #213 and #219.
- TKT-01M29CP2R4 (Build the audit log query and admin view): #220, then #222. **Criterion 6 is unticked:** the reader grew 11 B for the route line.
- TKT-01M3QGH66W (Add lock, unlock, pin and unpin to the thread screen): #224.
- TKT-01M3NS1931 (Autosave reply drafts from the composer): #227. **Disagreement pending the maintainer:** review 1585.
- TKT-01M3QMRE47 (Add the suspension controls to the admin users screen): #229.
- TKT-01M3QXV399 (Add rename and move to the thread screen): #231.
- TKT-01M3NS16VH (Build the new-thread screen with draft autosave): #235. **Disagreement pending the maintainer:** review 1591, finding 3.
- TKT-01M3R0PM0K (Show slow mode in the composer and add the moderator's slow mode dialog): #236.
- TKT-01M3R1WPNS (Expose hard redaction in the post DTO and client): #228, then #237. Also fixes the queue, which could not close flags on a redacted post.
- TKT-01M29CXK2H (Build the admin site settings screen): #230, then #238.
- TKT-01M3P0CYR3 (Add a rename control to the admin users screen): #239.

**Tests**
- TKT-01M3MTQG5A (Stop Playwright checks timing out under CI runner contention): #223.
- TKT-01M39WBN4Q (Commit Playwright screenshots and the safe-area check for the mockups): #226. Filed nine visual-divergence drafts, with no milestone, under TKT-01M29BN367.
- TKT-01M3RZXGBA (Bound the axe scans in the admin forms and agents Playwright tests): #232.
- TKT-01M3S1TVPD (Stop the full Playwright run spending more /auth calls than it refills): #233.

Main's push CI was green after each merge I watched.

Two security fixes came from lanes beyond their plans:
- TKT-01M3P0CYR3: an admin who renamed themselves would have been offered Suspend on their own account.
- TKT-01M3NS16VH: a create answered after the viewer changed could delete the next viewer's draft. The same race in the reply composer is TKT-01M3SAKPK9, filed as a draft.

The landing stopped at the maintainer's request on 2026-09-30.

Open, groomed and ready:
- TKT-01M3PTT97X
- TKT-01M3QHKD21
- TKT-01M29CP2Q3
- TKT-01M3KFFFJR
- TKT-01M3QS0TMV
- TKT-01M3QVWKAG
- TKT-01M3QV39H4

Phase-2 drafts filed during the run, to groom next:
- TKT-01M3SAKPK9 (reply post after a viewer change)
- TKT-01M3S8Y2NA (ApplicationDetail flake)
- TKT-01M3SK2HFE (admin-forms restore)

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T18:45:03Z

2026-10-01: maintainer answered the five open questions. Accepted the reply-autosave disagreement (review 1585, TKT-01M3NS1931) and the new-thread e2e disagreement (review 1591 finding 3, TKT-01M3NS16VH); accepted TKT-01M29CP2R4 AC6 (reader +11 B), now ticked; decided absolute post times (spike TKT-01M3S3ZF48, done, docs/06); and resumed the run for the seven ready tickets and the three run-filed drafts. Recorded on branch run/phase2-decisions-1001 (docs/06, docs/08 "Decided 2026-10-01", docs/11). Lanes started: TKT-01M3PTT97X holds the dist; KFFFJR, CP2Q3, QS0TMV and QV39H4 are preparing locally; the three drafts are being groomed.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T19:35:43Z

2026-10-01 log. #241 merged at b37c46a (the maintainer's decisions; reviews 1606/1607 low finding fixed, clean on 1eb0160; CI run 1578). TKT-01M3PTT97X Offer edit and delete only inside the author windows in the client: PR #243 merged at cd485ef (review 1610 high and medium fixed, then clean on cd485ef; CI run 1587). Adds viewer.author_windows, postControls/isOwn, OwnControls and ComposerBody's editing prop; reader bundle 56988 B of 70000 B. #242 merged at 46c1081: plans for TKT-01M3SAKPK9, TKT-01M3S8Y2NA and TKT-01M3SK2HFE (review 1608 medium fixed: afterAll closes in finally; review 1609 medium fixed by bounding the change-and-back case, with the no-lookup case pinned by a test and the generation-counter alternative recorded as lost; clean on dbbc108; CI run 1582). The three are promoted to ready here. Dist queue: QS0TMV (holding), QV39H4, KFFFJR, CP2Q3, QHKD21, QVWKAG, SAKPK9; S8Y2NA and SK2HFE change tests only and run beside it.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T22:53:26Z

2026-10-01 log (orchestrator), merges since #244:
- TKT-01M3QS0TMV Link to the link-a-sign-in page from the client: #245 at d28aead (review 1599 clean on 1726076; CI run 1598). "Link a sign-in" in an Account block on the member's own profile; the spec is paced with paceAuth(1); /auth worst case 20/20; reader 57204 B.
- TKT-01M3SK2HFE Restore the registration form after a failed admin-forms e2e test: #246 (clean review run 1605 on ba2f282, carried to 9a7dd6c; CI runs 1604, 1611). The afterAll restores through the admin API, with the context closed in finally; the deliberate break failed only that test in each project; dist unchanged.
- TKT-01M3QV39H4 Clear the agent limits form's Saved status when a field is edited: #247 at c9909b0 (clean on c9909b0, run 1609; CI run 1608). Reader unchanged at 57203 B.
- TKT-01M3KFFFJR Add the mark-unread control to the thread page: #248 at 4471ea6 (reviews 1619, 1622, 1623 and 1624, then clean on 4471ea6 in run 1626; CI run 1625). One disagreement pending maintainer: review 1623, on awaiting refreshViewer() before navigating. Review coverage gap: the clean review did not judge the tail of threads.ts. The Terva follow-up was refused because the PR had merged (run 1629), so the orchestrator read the diff and found nothing. The ticket note has the details. Reader 58151 B.
- TKT-01M29CP2Q3 Build the post history view with word-level diffs: #249 at 466e82e (review 1625 medium fixed, a crash on huge diffs; clean on 466e82e, run 1637; CI run 1636). AC6's quoted text differs; the screen says "Post removed permanently", as redacted posts do everywhere else. Reader 59923 B.
- TKT-01M3S8Y2NA Stop ApplicationDetail.test.tsx failing in full vitest runs: #250 at 5cf26ce (clean on 8591cca, run 1640; CI run 1639). Cause: the lazy admin chunk's first import used up settle's bound under load. renderAt now loads the chunk first. 7/20 red on main, 20/20 green on the fix.
- TKT-01M3QHKD21 Show moderators' post controls and the edited-by-a-moderator marker: #251 at 0f8efbe (review 1626 medium and low fixed; clean on 0cae5e7, run 1648; CI run 1647). Adds the viewer field forum_read_only, which closes the archived-forum gap #243 recorded. Reader 60162 B.
- Filed draft TKT-01M3SZ3GCE Stop admin-applications.spec.ts timing out under CI load, from main's push run #1592. It is not promoted.
- The run brief now says a review reporting a truncated diff is not a full review of the untruncated part, and requires a follow-up while the PR is still open.
Remaining: TKT-01M3QVWKAG (sign out, holds the dist) and TKT-01M3SAKPK9 (reply guard, built locally, queued).

**agent:claude-code/phase2-run-orchestrator** at 2026-10-01T05:13:08Z

2026-10-01 log (orchestrator), merges since #252:
- TKT-01M3QVWKAG Give signed-in members a Sign out control: #253 at f3ba70d. Review 1627's high finding was fixed: two applicants who signed in through the same provider looked like the same viewer. The fix adds the opaque `pre_user.tag` to `/me`, a 12-byte domain-separated hash of the stored session hash that can sign nothing in. The review was then clean on 0c0bc91 (run daf5e09b); CI #1659. The new `state/viewerGuard.ts` reloads other tabs and bfcache-restored pages when the viewer changes. The /auth worst case passed 20/20. Reader 60395 B.
- TKT-01M3SAKPK9 Guard a reply post answered after the viewer changed: #254 at 8532682 (clean on 8532682, run 1665; CI 1664). All 8 criteria are ticked. Reader 60438 B.
- TKT-01M3SZ3GCE Stop admin-applications.spec.ts timing out under CI load: plan #255 (8a849ca) and investigation #256 (c3de983), both clean on their heads. It did not reproduce in 42 CI-image runs under load, 11 host runs or 300 probe repeats, so the ticket is blocked waiting for a recurrence. Its block reason was refreshed here after TKT-01M3TNBKNQ landed.
- TKT-01M3TMRCMJ Close the shortcuts overlay on an Escape pressed before it takes focus: plan #257 (af9cd54) and fix #259 at 61201be (clean on 61201be, run 1693; CI 1692). The race was confirmed by tests that failed before the fix. 20 loaded repeats gave 84 passed and 0 failed. Reader +2 B.
- TKT-01M3TNBKNQ Print Playwright's error context into the CI log when a test fails: plan #258 (d643707) and implementation #261 at d2f80f6. Reviews 1636 (two medium) and 1637 (one low) were all fixed. The review was clean on c37c38d and carried to 81763a8, the later commits touching only the ticket; CI #1714. A passing run's log is unchanged.
- TKT-01M3TS0ZV9 Take Escape in the flag, moderate and forum-picker dialogs before focus: plan #260 (b3cf1f4) and fix #262 at aad50a4 (clean on aad50a4, run 1712; CI 1711). Adds `useModal` in `components/focus.ts`. 404/404 loaded e2e runs passed. Reader 60465 B.
- Epic TKT-01M29BKBM3 is closed in this PR, with AC1 unticked (see its note).
- Drafts filed with no milestone, after the orchestrator bounded scope on 2026-10-01: TKT-01M3TV4HBF (focus and key listeners in menus and popovers) and TKT-01M3TSZHSW (slow-mode e2e test failing on a phone under CI load).
- Housekeeping, as the maintainer asked on 2026-10-01:
  - Removed 114 agent worktrees and deleted 322 local branches whose work is in main, a merged PR, or a carried and merged successor.
  - Deleted 55 superseded remote branches. The remote now holds only main and groom/phase-1-autonomous-run, which another T3 thread has checked out.
  - Before deletion, every branch whose commits were not literally in main was saved as a git bundle under ~/.cache/agent-scratch/ketju/housekeeping.*/, which is swept after a week.
  - The maintainer's own checkout and the other T3 thread worktrees were left alone.

## Summary

The extended Phase 2 run ended on 2026-10-01. This summary is its report to the maintainer.

### Outcome
- **Merges.** Since the maintainer resumed the run on 2026-10-01, 22 PRs merged on the gate, #241 through #262, and the close-out PR merges after them. Before the pause the run had merged 30 of #208–#239. Every merge had green CI on its exact head, and the orchestrator read the review body or the clean comment on that head before merging.
- **Phase 2's 82 tickets.** 80 are done, counting this one. Two remain:
  - The epic TKT-01M29BN36J (Moderation primitives and admin surfaces) cannot close under docs/13's rule. Four of its children are Phase 4 drafts: split, merge, reply as a new thread, and thread tags.
  - TKT-01M3SZ3GCE (Stop admin-applications.spec.ts timing out under CI load) is blocked. The failure did not reproduce under any load tried, and the plan forbids fixing a cause nobody has seen. CI now prints a failing test's error context and server log (TKT-01M3TNBKNQ, #261), so the next recurrence will carry the evidence.
- **Reader bundle.** It finished at 60465 B of the 70000 B budget.

### Recorded disagreements
- **Pending maintainer.** Review 1623 on TKT-01M3KFFFJR (#248) asked that mark-unread await the viewer refresh before navigating. The refresh was left unawaited so the reader is not held on a page that reads its posts back up. This is not a security finding.
- **Accepted by the maintainer on 2026-10-01.** Review 1585 on TKT-01M3NS1931, and review 1591 finding 3 on TKT-01M3NS16VH.
- **None else.** No other disagreement was recorded in this run.

### Criteria merged unticked, or ticked by exception
- **TKT-01M29BKBM3, criterion 1.** A pre-user reaches the docs/05 allowlist: the form, the status page and logout, plus `/me`, the sign-in routes and assets. That is intended, and it is pinned by TestPreUserRoutesAreExactlyTheAllowlist. It is wider than the criterion's wording, so please accept or reword.
- **This ticket, criteria 1 and 2.** They stay unticked for the two tickets above.
- **TKT-01M3SZ3GCE.** All criteria are unticked, because it is blocked.
- **Ticked with a note.**
  - TKT-01M29CP2Q3 criterion 6: the screen says "Post removed permanently", as redacted posts do everywhere else, not the criterion's quoted text.
  - TKT-01M29CP2R4 criterion 6: the 11 B growth was accepted by the maintainer.
- **From the first run.** The unticked criteria on TKT-01M29CP2MG, TKT-01M29CRJ50, TKT-01M29CRJ5J, TKT-01M3M9RR26 and TKT-01M3NRYCP5 are as that run's report (TKT-01M3NTSCJQ) listed them.

### Review coverage
- **#248's clean review did not judge part of the diff.** Its diff ended partway through web/client/src/state/threads.ts. The Terva follow-up was refused because the PR had already merged. The orchestrator read the 21 lines and found nothing, and the note on TKT-01M3KFFFJR says this was not a Terva review.
- **The run brief now requires a follow-up while the PR is still open.**

### Questions for the maintainer
1. **The moderation epic TKT-01M29BN36J.** Should its four Phase 4 children move to another epic so it can close, or should it stay open until Phase 4?
2. **Drafts with no milestone, to place.**
   - Filed by the run after scope was bounded on 2026-10-01: TKT-01M3TV4HBF, the focus race in menus and popovers, and TKT-01M3TSZHSW, the slow-mode e2e test failing on a phone under CI load.
   - Earlier: eight open visual-divergence drafts under TKT-01M29BN367.
3. **Sign out (TKT-01M3QVWKAG, #253) does two things its plan did not spell out, for your information.**
   - Other open tabs reload when the viewer changes.
   - `/me` now gives a pre-user an opaque session tag, which a high finding needed.

### Housekeeping
- **Removed** 114 agent worktrees and 322 local branches, plus 55 remote branches, each with its work in main or in a merged PR.
- **Backups.** Unmerged-but-superseded commits are bundled under ~/.cache/agent-scratch/ketju/housekeeping.*/ for a week.
- **Left as found.** The maintainer's checkout at ~/workspace/git.local.sothr.com/terva-sh/ketju is on main at ba749bb, 700+ commits behind, with only `.claude/` untracked. The worktrees of two other T3 threads were also left alone.
