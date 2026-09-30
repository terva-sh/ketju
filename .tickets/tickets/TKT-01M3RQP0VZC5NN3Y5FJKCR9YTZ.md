---
schema: 3
id: TKT-01M3RQP0VZC5NN3Y5FJKCR9YTZ
title: Drive the rest of Phase 2 to done autonomously
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-orchestrator
  branch: groom/phase2-continue
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-438e26c1
  commit: 51242096ed1a113f94494c2d8012213fb5a74071
  session: null
  claimed_at: 2026-09-30T08:43:12Z
  expires_at: null
archive: null
created_at: 2026-09-30T08:42:46Z
updated_at: 2026-09-30T19:35:53Z
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
- [ ] Every recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [ ] docs/11 counts are refreshed when the run ends

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
