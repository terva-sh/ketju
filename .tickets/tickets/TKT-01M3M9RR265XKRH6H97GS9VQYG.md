---
schema: 3
id: TKT-01M3M9RR265XKRH6H97GS9VQYG
title: Groom Phase 2 for an autonomous run
type: chore
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:L
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29BSPE2ZP7X2C736K2KVPBA
  - TKT-01M29BSPENYP54KS604DRKH874
  - TKT-01M29BSPEXA023NH3DRMWKW9HB
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase1-run-438e26c1
  branch: groom/phase2-decision-spikes
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-438e26c1
  commit: 5d0fa1a51b491c476bc977f392e0b847fb6a412a
  session: null
  claimed_at: 2026-09-28T21:32:46Z
  expires_at: null
archive: null
created_at: 2026-09-28T15:22:37Z
updated_at: 2026-09-29T05:39:49Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Phase 1 ran autonomously and cleanly because it was groomed first, on 2026-09-24 (docs/11 "Phase 1 was groomed"). With the user, grooming took every decision the backlog was waiting on and recorded each in docs/10 or its design document. It gave every open ticket an implementation plan with a `### Settled in grooming` section, rewrote vague criteria into checkable ones, filed what no ticket owned, and wrote the run procedure (docs/12) with its merge authority. Phase 2 needs the same before any autonomous run.

Phase 2 (docs/09) is approval-mode registration and forms, multiple providers and account linking, moderation primitives, admin screens, and agent accounts. Its exit criterion: "a club can open registration to strangers safely, and an admin can give an agent an account and a credential". It carries about 33 tickets under the epics TKT-01M29BKBM3 (Approval-mode registration and the forms system) and TKT-01M29BN36J (Moderation primitives and admin surfaces). Several more sit under Phase 1 epics: the OIDC epic TKT-01M29BKBKZ parents the provider, linking, group-mapping and lifecycle tickets.

### Decisions this grooming must take (dependencies of this ticket)
- TKT-01M29BSPE2, Decide hard redaction semantics
- TKT-01M29BSPEN, Decide the handle change policy
- TKT-01M29BSPEX, Decide whether auto_approve providers skip handle selection

### Decisions already known to be coming
Each has been raised by a merged PR or a note:
- Whether an author may restore a post a moderator deleted (TKT-01M29CRJ5J; raised on PR #95, whose `authorize()` in internal/service/threads is author-only).
- The flag auto-hide threshold, and who sees a hidden post (TKT-01M29CP2PJ).
- What an archived forum does to permissions (TKT-01M39788AT, no milestone).
- Whether the phase-1 epics' Phase 2 children are regrouped under the Phase 2 epics before the Phase 1 epics close (the user kept the epics open on 2026-09-28).
- Where the unmilestoned drafts go: the hardening sweep ticket lists the ones it left out.
- Removing the `auto_approve` "has no effect" warning in `ketju config check` (validate.go `warn()`) when TKT-01M29CKRE9 lands (PR #106).

### Security focus
Phase 2 is where strangers get in. Grooming should name, for each ticket that creates a session or changes a role, the test that proves:
- a pending or suspended user cannot reach member endpoints (docs/09's Gitea-bug risk; see TKT-01M3AFHGJD in the hardening sweep);
- role changes go through perm.Evaluator (PR #97);
- the response cache (PR #103) cannot serve one viewer class's page to another.

### Run procedure
If Phase 2 is to run autonomously, grooming writes its procedure as docs/12 was written, or generalises docs/12. It also records the user's merge authority on a new tracking ticket. docs/12 is scoped to the Phase 1 run and says so. Lessons from the Phase 1 runs belong in it:
- a medium-or-higher finding answered by argument stops for a person, even if a re-review clears it;
- a cap on review rounds per PR;
- two lanes, with the orchestrator batching tracking notes;
- rebase merges rewrite commit IDs, so cite commits as they landed on main.

The handoff for this grooming is in `~/.local/state/agent-handoffs/ketju/`, outside the repository.

## Acceptance criteria

- [x] The three Phase 2 decision spikes are decided and recorded in docs/10 or their design documents
- [ ] Every open Phase 2 ticket has an implementation plan with a Settled in grooming section and checkable criteria
- [ ] Every session-creating or role-changing ticket names the test that proves a pending or suspended user cannot reach member endpoints
- [ ] A run procedure and a tracking ticket with the user's merge authority exist, or the user chose not to run Phase 2 autonomously
- [ ] docs/11 is refreshed with the groomed counts

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:32:46Z

Grooming started 2026-09-28 at the maintainer's direction ("Let's start phase 2 grooming"). The three decision spikes were decided first, and are closed in the first grooming PR: BSPE2 redaction, BSPEN handle changes, BSPEX auto_approve handles and guests. The decisions are in docs/03, 05 and 10.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:42:54Z

Maintainer, 2026-09-28: grooming PRs for this ticket may be merged on the docs/12 gate (the required CI is success on the exact head, terva-review/code is success, every finding is fixed or answered). A medium-or-higher finding answered by argument still goes to the maintainer before merging. Terva review 1255 (Actions run #774) on PR #133, head 14fbb6a, base 5d0fa1a, had two findings, both fixed rather than argued. High, 'Do not permanently redirect a handle that can be reassigned': docs/05 and docs/10 now say a temporary 302 with no-store. Medium, 'Specify durable writes before claiming power-cut safety': the docs/03 hard-redaction swap now fsyncs the new log, renames it, then fsyncs the directory.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T21:50:43Z

Terva review 1259 (Actions run for request ready-review-88ce15f) on PR #133, head 88ce15f, base 5d0fa1a: one medium finding, 'Correct the handle-policy ticket's redirect summary'. Fixed: the TKT-01M29BSPEN summary now says 302 with no-store.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:01:03Z

Terva review 1265 (Actions run #777) on PR #133, head 8444533, base 5d0fa1a: previous finding resolved. One high finding, 'Redact post copies created by split or merge'. Fixed, not argued: docs/03 hard redaction now follows origin links both ways and redacts every copy.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:09:30Z

Terva review 1271 on PR #133, head d40f8a7, base 5d0fa1a: previous finding resolved. One high finding, 'Make redaction recoverable across all logs containing a post'. Fixed, not argued: docs/03 adds a fsynced redaction journal that blocks serve and is resumed by rerunning the command.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:18:20Z

Terva review 1275 on PR #133, head 449cfac, reported the journal as absent, though it was in the diff at docs/03:334. It was treated as a misread and the review run once more (request ready-review-449cfac-r2). The re-run, review 1278, found the journal but raised two new findings. High, 'Make the redaction journal's directory entry durable before swapping logs', and medium, 'Specify how resumption avoids appending another Redacted record'. Both fixed, not argued.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:24Z

Grooming decisions, batch 2, maintainer 2026-09-28. CRJ5J: only moderators restore a moderator's delete. CP2PJ: auto-hide at 3 (a setting), visible to author and moderators. 39788AT: archived forum is read-only except for admins; spike closed; TKT-01M3N24RHD filed. 2K1JKS7: refuse remote images cleanly (the CSP already blocks them); spike closed; TKT-01M3N24RM0 filed. K9XBJA: the new-thread draft saves the title. 39WZY8C folded into CP2Q3 and archived. Moved into phase-2: KFFFJR, K9XBJA, A054TR, 973KG, MTQG5A, WBN4Q. MSFX3S stays unscheduled. 973KG still needs a decision: whether docs/03 or the code is right about the reserved split/merge types.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:39:20Z

Terva review 1280 on PR #134, head d7c1473, base 02db608, had three findings, all fixed rather than argued. Medium, 'Do not mark the renderer-version criterion complete before the build': TKT-01M2K1JKS7's criteria 2 (not applicable) and 3 (moved to TKT-01M3N24RM0) are unticked, with a note. Low, 'Clarify that a remote-image link can contact its host': docs/07 now says viewing sends nothing, while following the link does. Medium, 'Reconcile the author-visible label with the flag-confidentiality rationale': the docs/08 rationale is corrected, since the author learning of the review is intended and who flagged stays confidential, and a superseding note is on CP2PJ.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-29T04:50:59Z

Maintainer, 2026-09-28: 'pick up what you recommend next and continue our work through phase 2 autonomously, merging after reviews are complete and issues resolved or disagreements recorded.' This grants the docs/12 merge gate to the rest of Phase 2 grooming and to the Phase 2 implementation run. The run's tracking ticket and procedure will name the grant. A disagreement with a review finding is recorded on the ticket and in the PR, and does not stop the merge. This supersedes docs/12's rule that a medium-or-higher finding answered by argument stops for the maintainer, for Phase 2 only. The recorded disagreements go to the maintainer in the run's report.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-29T05:01:23Z

Terva review 1337 on PR #135, head 5cd773d, base 7af2c5a: one low finding, 'Date the refreshed ticket counts after the ticket transition'. Fixed: docs/11 now says the counts were read on 2026-09-29.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-29T05:39:49Z

Plan PRs #136–#140 were written by five parallel planning agents from grooming-brief.md, one per group: identity, forms, moderation, admin, and follow-ups. The first reviews and their fixes, all fixed and none argued:
- #136, review 1342, head ba5e6ff: high 'unmapped-provider sign-in can retain revoked IdP permissions' (CKRF9: every sign-in now replaces the IdP-granted groups with what that provider maps, and an unmapped provider grants none); medium 'linking records an identity login that did not occur' (CKRES: a link sets no last-login and no claims).
- #137, review 1344, head c60fa40: high 'approval updates status before user_id exists' (CP2MG: the user is created and linked first, then status and user_id are set in one UPDATE); high 'refuse new applications from linked identities' (CP2KZ: 409, checked again inside the transaction); medium 'FK fallback does not validate new forms' (CKRDQ: active_version starts NULL and can only be set to a version that exists).
- #138, review 1343, head 53f8b54: high 'a threshold-crossing flag can leave the post visible' (CP2PJ: a hide on every flag at or over the threshold, plus an AutoHideSweep job); high 'archive invalidation misses another evaluator's cache' (RHD: forums.archived is read on every write request); medium 'audit helper conflicts with unaudited author actions' (appendEvent for author actions, moderate plus internal/audit for moderator actions).
- #139, review 1345, head e2ce3eb: three highs on the redaction plan (CXJZ9): attachment hashes are journalled before any rewrite, blob-reference drops are made idempotent using the audit row as the resume marker, and the origin search runs to a fixed point.
- #140, review 1349, head 90d2f7c: see the next note.
The maintainer accepted all 15 open recommendations on 2026-09-29. They are written into docs/05, 06 and 08 on the run-procedure PR, and into each ticket's Settled section on the plan PRs.
