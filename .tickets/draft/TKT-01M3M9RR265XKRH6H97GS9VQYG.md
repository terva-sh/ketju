---
schema: 3
id: TKT-01M3M9RR265XKRH6H97GS9VQYG
title: Groom Phase 2 for an autonomous run
type: chore
status: draft
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
claim: null
archive: null
created_at: 2026-09-28T15:22:37Z
updated_at: 2026-09-28T15:22:53Z
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

- [ ] The three Phase 2 decision spikes are decided and recorded in docs/10 or their design documents
- [ ] Every open Phase 2 ticket has an implementation plan with a Settled in grooming section and checkable criteria
- [ ] Every session-creating or role-changing ticket names the test that proves a pending or suspended user cannot reach member endpoints
- [ ] A run procedure and a tracking ticket with the user's merge authority exist, or the user chose not to run Phase 2 autonomously
- [ ] docs/11 is refreshed with the groomed counts
