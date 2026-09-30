---
schema: 3
id: TKT-01M3NTSCJQ8XSYGTEZ6RXG29FM
title: Drive Phase 2 to its exit gate autonomously
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
dependencies:
  - TKT-01M3NTSCMN7YYA0M8TMMQX6H6E
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:39:18Z
updated_at: 2026-09-30T04:51:17Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

Drive the `phase-2` milestone to its exit gate under docs/13 (The Autonomous Phase 2 Run), which amends docs/12.

### The grant, in the maintainer's words (2026-09-29)

"Let's pickup what you recommend next and continue our work through phase 2 autonomously merging after reviews are complete and issues resolved or disagreements recorded."

docs/13 "The grant" says what this covers. The run merges on the docs/12 gate. It records a disagreement with a finding, rather than stopping for one, except for security findings, which are fixed or block the ticket. It may promote a groomed `phase-2` draft that has no open decision.

### Wave order

1. Foundations: the admin scaffold (TKT-01M3NRYCP5), the applications schema (TKT-01M29CKRDQ), pre-user sessions (TKT-01M29CKRG9), the author windows (TKT-01M29CKRHS), lock and pin (TKT-01M29CKRGS), and the archived-forum permission (TKT-01M3N24RHD).
2. Identity: providers, linking, group mapping, the lifecycle, and handle changes.
3. Applications: the form schema, submit, review, notifications, and screens.
4. Moderation: moderator edit and delete, tombstones, flags, rename and move, history, slow mode, the queue, and suspension.
5. Admin screens and agent accounts.
6. Follow-ups: redaction, fsck, Playwright, drafts, mark-unread, and remote images.
7. The exit gate.

`git ticket ready` and the dependency links decide the actual order. This list is only the intent.

One note per merged PR goes here, written by the orchestrator in batches.

## Acceptance criteria

- [x] The Phase 2 exit ticket is done
- [x] Every recorded disagreement is listed in the summary for the maintainer
- [x] docs/11 counts are refreshed when the gate closes

## Notes

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T07:18:23Z

Run started 2026-09-29 by the orchestrator (agent:claude-code/phase2-run-orchestrator) under docs/13, which merged in PR #141. The 36 groomed Phase 2 tickets with no open decision were promoted to ready. Six more are still being planned on groom/phase2-plans-late: TKT-01M3NRP3W9, 3NRX2NN, 3NS16VH, 3NS1931, 3NTSCMN, and the spike 3NS147C, which needs a maintainer decision. CKRFS, CKRF9, CKRES, CKRGS, CRJ5J and CKRH9 were linked to depend on the admin scaffold TKT-01M3NRYCP5, because they write audit rows through its internal/audit helper. There are two lanes, server and client. Only one open PR at a time may change the committed client dist.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T15:28:04Z

Merges so far, written by the orchestrator in one batch:
- TKT-01M29CKRDQ (Add the applications and forms schema): PR #144 at 2897ac0. Migration 0011 adds forms, form_versions, applications, application_notes, identity blocks and notifications.application_id, and seeds the registration form. Review 1373 had two mediums, both fixed, and run 3efcb1fc was clean.
- TKT-01M3NRYCP5 (Add the admin API gate, audit helper and admin screen scaffold): PR #145 at 889870b. It adds internal/audit, the only writer of audit_log (TestOneWayIntoAuditLog enforces it); the admin, staff and moderates gates with adminRoute; perm.ModeratesAny; and the admin layout. Review 1374 had two mediums, both fixed, and run 881 was clean. **AC4 is unticked:** the reader bundle grew 116 B gzip, to 38287 of 60000, for the viewer export the admin chunk needs. This goes to the maintainer to accept or reword.
- TKT-01M29CKRHS (Implement author edit and delete windows with grace edits): PR #146 at ccc05ad. It adds the windows, grace recorded in the log, and the edit-window-closed and delete-window-closed codes. authorize() takes (a, e, action, now, w). The orchestrator approved the small client change. Review 1378 had one medium, fixed, and run 821923a1 was clean. Draft TKT-01M3PTT97X was filed for the client controls.
- Grooming PR #143 merged at 4f38f14 with the late plans and the title-redaction decision. Two round-5 mediums were carried into TKT-01M3NS1931.
Main was green after each merge.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T15:28:19Z

Promoted to ready after #143 merged: TKT-01M3NRP3W9, 3NRX2NN, 3NS16VH, 3NS1931 and 3NTSCMN. TKT-01M3P0CYR3 (admin rename control) and TKT-01M3PTT97X (client edit and delete windows) were filed during the run and have no groomed plan, so under docs/13 they stay draft until one is written.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T21:00:34Z

More merges, written by the orchestrator in one batch:
- TKT-01M29CKRF9 (Map IdP group claims onto local groups): PR #147 at 21dbd04. Adds identity.Policy and perm.SyncIdPGroups, which writes the group.member_added and group.member_removed audit rows. Every sign-in now requires identity.Service.Perm. Both reviews were clean.
- TKT-01M29CKRG9 (Implement pre-user sessions restricted to the application flow): PR #149 at 10a3a7b. Adds web.PreUserGate at chain position 7 with the positive allowlist web.PreUserRoutes, and makes the API and auth routes walkable tables. Review 1397 had one medium, fixed; the next run was clean.
- TKT-01M29CP2KE (Implement the declarative form schema and server-side validation): PR #151 at 3eab1e9, which superseded #150 after main moved. Adds internal/forms, formsvc, and field errors on validation-failed. Reviews 1404, 1407 and 1412 were all fixed.
- TKT-01M29CXK1X (Build the admin users, groups, and permissions screens): PR #153 at 9d5ef44, which superseded #152. Adds perm.ChangeRole and the ...Then forms, and user.demoted. The last admin cannot be demoted. Every finding was fixed.
- TKT-01M29CP2KZ (Implement application submit, status, and withdraw): PR #156 at 3fd598f, carried from #154, after #155 blocked the ticket on TKT-01M3Q2V25E. The maintainer chose "sign in again" on 2026-09-29. Adds internal/service/applications and web.KeyedLimit.
- TKT-01M29CKRFS (Implement the account lifecycle: deactivate, suspend, anonymize): PR #157 at 31e6812, after five review rounds with every finding fixed. Adds perm.ChangeStatus with status_version, handle_releases, and identity.HandleFree, which TestEveryHandleWriteChecksHandleFree enforces. AC5 waited on TKT-01M3Q7Q1T4; the maintainer chose plain text on 2026-09-29, and AC5 is now reworded and ticked.
- TKT-01M29CP2NG (Build the apply and status screens): PR #158 at f1129ce, after five rounds with every finding fixed. One low was answered in a note. Adds the shared src/forms renderer. The bundle is 45219 of 60000 B.
- TKT-01M29CP2MG (Implement moderator application review): PR #159 at 855ca5d. Review 1470 had a high and a medium, both fixed; the next run was clean. **AC7 is unticked:** a suspended moderator gets 401 rather than 403, because member.Require answers 401 to every inactive account across the site. That goes to the maintainer to accept or reword.
Main was green after each merge.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-29T22:49:17Z

More merges, written by the orchestrator in one batch:
- TKT-01M3NRX2NN (Build the admin applications review screen): PR #162 at fe5cdcb. Five rounds; every finding fixed. Decision.tsx is exported for the queue, the e2e seed adds the moderator mona, and ApplyRoot has a codeSplitting group. The bundle grew 111 B to 45330 B, so the "reader total unchanged" clause holds only approximately; the ticket note explains why.
- TKT-01M29CKRGS (Implement thread lock, unlock, pin, and unpin): PR #161 at a27a721. The projection writer now writes the moderation audit rows by log offset. Review 1471's medium was answered with a test proving the code already correct; the re-review was clean. The client work is TKT-01M3QGH66W.
- TKT-01M29P9RSW (Implement agent accounts, provisioning, and the agent credential): PR #164 at f04a476, replacing #163. Adds internal/auth/agentcred, web.KeyedRateLimit with a 20/30s bucket, lifecycle revocation, and migration 0013. Review 1477: the medium (refill overflow) was fixed; the high was a false positive and was rewritten anyway. The UI work is TKT-01M3QH3GS2; /me for agents is TKT-01M3QH3X58.
- TKT-01M29CRJ5J (Allow moderators to edit and delete any post): PR #165 at 796641c. Adds Delete.by_moderator (hidden-by-mod), Append.RequireAny, and dto.Post.edited_by_moderator. Review 1486's medium was fixed. AC2's client half is in TKT-01M3QHKD21.
- TKT-01M29CKRE9 (Support multiple providers with domain gates and auto-approve): PR #166 at 1dd6ed9. The review was clean with no findings. The domain gate requires email_verified, auto_approve registers under approval mode, and the config warning is removed.
- Grooming PR #167: plans for seven run-filed drafts, all promoted here. The high finding on agent-token handling was fixed. A medium on timestamps was answered as a false positive, since created_at is Millis, and the plan was clarified.
Main was green after each merge.
Open question for the maintainer's report: a member who moderates a forum through a group grant has can_moderate, but the role-based staff gate refuses them lock and pin (docs/07 "by role"). Kept for Phase 2.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T01:44:28Z

More merges, written by the orchestrator in one batch:
- TKT-01M29CP2P1 (Build the admin form editor): PR #168 at 3655731. Admin-only /admin/forms in the lazy admin chunk, with the server's preview gating Save. The reader total grew 6 B of export names, to 45336 B.
- TKT-01M29CKRJ9 (Render tombstones for deleted and moved posts): the server half is PR #170 at 2826208 (dto.Post.removal and threads.revealRemoved, with the cache class author:<id>), and the client half is PR #174 at 4d793b2.
- TKT-01M29CP2QK (Implement user suspension from the moderator tools): PR #172 at 5c9da6d, carried from #169 after main moved. Adds hide_posts and restore_posts with the maintainer's authority rule (mayActOn), plus threadlog.Append.AtLength. The UI is TKT-01M3QMRE47.
- TKT-01M29CP2N0 (Notify moderators and applicants about applications): PR #173 at 2e2fe7c. Adds internal/service/notify; notification rows are written in the transaction of the act that causes them.
- TKT-01M29CP2PJ (Implement flags with the auto-hide threshold): PR #176 at 3e1ff35, carried from #175. Adds migration 0014, index bit 6 EntryAutoHidden, removal 'review', and jobs.AutoHideSweep. The client item is TKT-01M3QQWA5Z.
- TKT-01M29CKRES (Implement account linking): PR #178 at 43a844d, carried from #177. Linking happens only through the explicit action. identity.Link calls no issuer, so the chokepoint lists are unchanged. The client entry point is TKT-01M3QS0TMV.
- TKT-01M3QH3GS2 (Add agent provisioning controls to the admin users screen): PR #179 at d512bd3. One-time tokens live only in AgentPanel state. The reader bundle is 45575 of 60000 B.
- TKT-01M3N24RHD (Make archived forums read-only in permission evaluation): PR #182 at b41b6db. AC7's move half is left unticked for TKT-01M29CKRH9, which owns the move route; that agent reports the move rule built.
- TKT-01M29CRJ4E (Build the moderator queue), server half: PR #184 at 034053c, carried from #183. The ticket stays in progress for the client half.
- Grooming PR #181 at 2fc352f: plans for TKT-01M3QMRE47, 3QQWA5Z, 3QS0TMV and 3QVWKAG. It ran the full five review rounds, and every finding was fixed; none was disagreed with. The last run was clean.
Main was green after each merge. PR #180 (TKT-01M3NRP3W9) was closed in favour of a carried branch that is still in flight.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T01:44:28Z

Promoted to ready after #181 merged: TKT-01M3QMRE47 (Add the suspension controls to the admin users screen), 3QQWA5Z (Add the flag item and dialog to the post menu), 3QS0TMV (Link to the link-a-sign-in page from the client), and 3QVWKAG (Give signed-in members a Sign out control), which waits on 3QS0TMV.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T03:34:51Z

More merges, recorded by the orchestrator in one batch:
- TKT-01M29CRJ4E (Build the moderator queue): server half in PR #184 at 034053c, carried from #183; queue screen and badge in PR #185 at ffddc6b. Reviews 1522 and 1526 were fixed; the 1524 finding was a misreading and was answered with an explicit precondition and a test. All 13 criteria are ticked. Thread-page Delete and Restore now take the per-post flag lock. The suspension bulk hide still does not; draft TKT-01M3R10VP3 covers that.
- TKT-01M3NRP3W9 (Implement handle changes with cooldown, reservation, and redirect): PR #186 at c24635c, carried from #180. Review 1521's medium was fixed, so mentions now resolve from handle_releases rows only. Migration 0016.
- TKT-01M3QH3X58 (Answer GET /me for an agent authenticated by its credential): PR #189 at c158b5f. The review was clean and the chokepoint lists are unchanged.
- TKT-01M29CRJ50 (Implement per-thread slow mode): server half in PR #188 at a827aef, reviewed clean. Migration 0017. dto.ts changed types only, and the dist is byte-identical. **Criterion 3's composer half is unticked** and goes to draft TKT-01M3R0PM0K.
- TKT-01M29CXJZ9 (Build the offline hard-redaction tool): PR #192 at 5c2ba25, carried from #191. Reviewed clean, and all 18 criteria are ticked. The log format gains record 0x80 Redacted and index bit 7. The DTO and client field is draft TKT-01M3R1WPNS.
- TKT-01M29CKRH9 (Implement thread rename and move): PR #193 at 052db1d, carried from #190. Review 1535's two mediums were fixed. All 8 criteria are ticked, which completes TKT-01M3N24RHD's criterion 7. The client controls are draft TKT-01M3QXV399, and the post-window overflow is draft TKT-01M3R33SR5.
- TKT-01M3N24RM0 (Render remote markdown images as links rather than broken images): PR #195 at e425323. Reviewed clean. render.Version is 5.
None of these recorded a disagreement. CI runs 1240 and 1242 timed out in internal/perm under runner load and passed on re-run; draft TKT-01M3R5P3CN covers that.

**agent:claude-code/phase2-run-orchestrator** at 2026-09-30T04:49:52Z

Final merges, written by the orchestrator in one batch:
- TKT-01M3NTSCMN (Prove Phase 2's exit: approval registration and agent credentials e2e): PR #194 at 790b6dc. The review was clean; CI run 1270 passed on 6381d4d. The step-log lines for TestPhase2Exit* and phase2-exit.spec.ts are quoted in PR comments 19728 and 19796. All six criteria are ticked, and the tests found no product failure. The PR adds one CI step, "Phase 2 exit tests, by name", which docs/12 "Changing CI" allows. **The Phase 2 exit gate is closed.**
- TKT-01M3A054TR (Report projection drift and quarantined threads in ketju fsck): PR #200 at 23f4cc8. It was carried from #196, whose test helper clashed with #192. Reviews 1536 and 1537 each raised a medium, and both were fixed. The final run was clean, and all six criteria are ticked. Adds db.OpenReadOnly and projection.Classify.
- TKT-01M3QJYV4Z (Serve a post's current source to its author for editing): PR #205 at 7580e73, carried from #197, #199, #201 and #204. The review was clean and all three criteria are ticked. GET .../posts/{ordinal}/source is no-store. Main moved during CI by ticket files only (#203), and I agree with merging without a further carry.
- TKT-01M3QQWA5Z (Add the flag item and dialog to the post menu): PR #202 at 8037b2e. Review 1538's medium and 1544's two mediums were fixed; the self-flag one was treated as security. The third run was clean, and all nine criteria are ticked. The thread view grew 1691 B, to 47415 of 60000 B gzip. **Behaviour change from TKT-01M29CP2PJ:** a kept flag whose auto-hide fails now answers the 200 receipt and logs ErrHideDeferred; `unavailable` now means nothing was kept. docs/07 and 08 say so.
- Grooming PR #203 at a08a152: plans for seven run-filed drafts. Review 1540's two mediums were fixed, and the second run was clean. Six were promoted here: TKT-01M3QV39H4, 3QXV399, 3R0PM0K, 3R10VP3, 3R1WPNS and 3R33SR5. TKT-01M3R5P3CN (Keep the Go race tests inside their timeout under CI runner load) stays in draft on one open decision for the maintainer: whether the race step gets an explicit `-timeout`. The recommendation is no, because docs/12 does not let a run edit an existing step, and the planned template database removes the cause.
- TKT-01M3N24RHD criterion 7 is ticked here, now that #193 delivered the move half.
Main was green after each merge.

## Summary

The Phase 2 run closed its exit gate on 2026-09-30. TKT-01M3NTSCMN merged in PR #194, proving approval registration, denial, suspension and agent credentials end to end through the real serve, plus Playwright at 390 and 1440 px with axe. The run merged every ticket the gate depends on, the server work it picked up next, and its grooming, each on the docs/12 gate. It stopped at the gate as docs/13 says, leaving 23 Phase 2 tickets ready, mostly client screens. The merge log is in this ticket's notes.

**Recorded disagreements: none.** A note on TKT-01M29CP2KZ from #154's branch once marked a finding "disagreement: pending maintainer". It was superseded when the maintainer decided TKT-01M3Q2V25E (an approved applicant signs in again).

**Merged with a criterion unticked or met only approximately, for the maintainer to accept or reword:**
- TKT-01M29CP2MG criterion 7: a suspended moderator gets 401, not 403, because member.Require answers 401 to every inactive account.
- TKT-01M29CRJ50 criterion 3: the composer half is left to TKT-01M3R0PM0K, which is now ready.
- TKT-01M29CRJ5J criterion 2: the client half is left to TKT-01M3QHKD21.
- TKT-01M3NRYCP5 criterion 4: the reader bundle grew 116 B. TKT-01M3NRX2NN (+111 B) and TKT-01M29CP2P1 (+6 B) similarly meet "reader total unchanged" only approximately.
- TKT-01M3M9RR26 criterion 3: TKT-01M29CXK1X names no test of its own for a pending or suspended user.

**Open questions for the maintainer:**
- A member who moderates a forum through a group grant has can_moderate, but the role-based staff gate refuses them lock, pin, move and slow mode (docs/07 "by role").
- TKT-01M3R5P3CN: should the CI race step get an explicit -timeout? It stays in draft on this; the recommendation is no.
- The flag route's answer changed in #202: a kept flag whose auto-hide fails now returns the 200 receipt rather than unavailable.

**For the maintainer to delete:** remote branches of PRs closed without merging that are still on the server, for example run/crj4e-moderator-queue (#183). The run deletes a branch only after its PR merged.
