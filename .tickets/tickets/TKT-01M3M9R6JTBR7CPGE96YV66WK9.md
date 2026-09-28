---
schema: 3
id: TKT-01M3M9R6JTBR7CPGE96YV66WK9
title: Run the pre-Phase-2 hardening sweep of Phase 1 follow-ups
type: chore
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3A8V7XCHBCPEH6HYHPVYM66
  - TKT-01M3AFHGJDQTMQ67CB8832F4AQ
  - TKT-01M39VNEC1YB0P357W0MCWV1HA
  - TKT-01M393S6P7NK3PVXM76TVHJ16M
  - TKT-01M3ANX1NF3XWCJBS3DH9QZNFC
  - TKT-01M3BFSBPJY0DA4RYA7ASXJKKX
  - TKT-01M3KB7FXCKRBV62FR6EZXCKP3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:22:19Z
updated_at: 2026-09-28T18:06:56Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The Phase 1 runs left a set of small bugs and gaps as drafts with no milestone. This ticket collects the ones that should land before Phase 2 opens registration to strangers, so they are worked as one sweep, not left to drift. It tracks the sweep; the work happens in each linked ticket, one PR per ticket, as in docs/12's loop.

### In scope (each is a dependency of this ticket)
- TKT-01M3A8V7XC (Publish quarantine before an interrupted live sweep returns). A high finding deferred from PR #67. A cancelled live sweep can leave a corrupt thread neither held nor quarantined, so its endpoints serve a projection longer than its log. The ticket describes the fix and its test.
- TKT-01M3AFHGJD (Test the OIDC callback against a pending user). This pins docs/09's named risk, "approval bypass via a code path that creates sessions (Gitea's bug)", before approval mode adds pending users in bulk.
- TKT-01M39VNEC1 (Stop returning a thread page the cache refused to its caller). A medium finding deferred from PR #63. Content can show after access was withdrawn once a second caller loads the same thread, and read tracking plus the reply expander make that more likely.
- TKT-01M393S6P7 (Order the group-commit fsync test so its first append is in flight). A test that can pass by winning a race.
- TKT-01M3ANX1NF (Handles ending in '-' can never be mentioned). Open registration (PR #102) accepts such handles today.
- TKT-01M3BFSBPJ (Stop nesting the quote source link inside its summary). This removes the one axe exception in e2e/a11y.spec.ts. **It needs a person's decision first:** the markup options are in the ticket.
- TKT-01M3KB7FXC (Stop recovered databases reusing member ids that old posts name). After a restore, new members can be given ids that old posts name. **It needs a person's decision first:** it is a product and security choice, and the options are in the ticket.

### Also to settle in the sweep
- TKT-01M3AZGKBP (Keep client PRs reviewable: exclude the built bundle from review) is probably obsolete. The v0.5.0 reviewer (PR #93) sends up to 1 MiB, and every client PR in the continuation run was reviewed directly. Confirm it, then archive it: it carries notes, so it is archived, not removed.

### Considered and left out
Other drafts with no milestone are feature work or decisions that belong with a later phase:
- TKT-01M3A054TR, fsck drift report;
- TKT-01M39973KG, reserved record types doc;
- TKT-01M39WBN4Q, mockup screenshots;
- TKT-01M39WZY8C, post history screen, which may duplicate phase-2 TKT-01M29CP2Q3;
- TKT-01M3K9XBJA, new-thread drafts;
- TKT-01M3KFFFJR, the mark-unread control;
- TKT-01M2K1JKS7, remote images;
- TKT-01M39788AT, archived forum permissions.

Phase 2 grooming (see its ticket) should place them.

### How to run it
None of the linked tickets is promoted yet; each is `draft`. A person promotes them, taking the two decisions above first. They can then be worked by hand or under a docs/12-style run if a person grants that authority for the sweep. The handoff for this sweep is in `~/.local/state/agent-handoffs/ketju/`, outside the repository.

## Acceptance criteria

- [ ] Every in-scope ticket is done, or blocked with a reason and a filed follow-up
- [ ] The two decision tickets (TKT-01M3BFSBPJ, TKT-01M3KB7FXC) carry a person's recorded decision before implementation
- [ ] TKT-01M3AZGKBP is archived or kept, with the reason in a note
- [ ] Main's CI is green after the last merge

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:06:54Z

### Authority for the sweep, granted 2026-09-28

When asked, the maintainer chose "Merge on the docs/12 gate". The sweep has the same standing merge authority and exclusions as the Phase 1 runs, applied to this ticket's dependencies and nothing else. A PR merges only when all of these hold on its exact head:
- CI `ci / Build and Test (pull_request)` is success;
- `terva-review/code` is success;
- every low finding is fixed or answered.

A medium-or-higher finding answered by argument, not by changing the code, stops for the maintainer, even if a re-review clears it. The rest of docs/12 applies as written: "Changing CI", "Gaps the design leaves open", "When a ticket cannot proceed" and "Never". No change to AGENTS.md or docs/12 is covered. The orchestrator redeploys the dogfood instance after the sweep.

Also decided, in the maintainer's words: "Promote all 7, archive AZGKBP". The seven dependencies and this ticket move to ready. TKT-01M3AZGKBP is archived, not removed, because it carries notes.
