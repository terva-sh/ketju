---
schema: 3
id: TKT-01M3M9R6JTBR7CPGE96YV66WK9
title: Run the pre-Phase-2 hardening sweep of Phase 1 follow-ups
type: chore
status: done
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
updated_at: 2026-09-28T20:19:02Z
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

- [x] Every in-scope ticket is done, or blocked with a reason and a filed follow-up
- [x] The two decision tickets (TKT-01M3BFSBPJ, TKT-01M3KB7FXC) carry a person's recorded decision before implementation
- [x] TKT-01M3AZGKBP is archived or kept, with the reason in a note
- [x] Main's CI is green after the last merge

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:06:54Z

### Authority for the sweep, granted 2026-09-28

When asked, the maintainer chose "Merge on the docs/12 gate". The sweep has the same standing merge authority and exclusions as the Phase 1 runs, applied to this ticket's dependencies and nothing else. A PR merges only when all of these hold on its exact head:
- CI `ci / Build and Test (pull_request)` is success;
- `terva-review/code` is success;
- every low finding is fixed or answered.

A medium-or-higher finding answered by argument, not by changing the code, stops for the maintainer, even if a re-review clears it. The rest of docs/12 applies as written: "Changing CI", "Gaps the design leaves open", "When a ticket cannot proceed" and "Never". No change to AGENTS.md or docs/12 is covered. The orchestrator redeploys the dogfood instance after the sweep.

Also decided, in the maintainer's words: "Promote all 7, archive AZGKBP". The seven dependencies and this ticket move to ready. TKT-01M3AZGKBP is archived, not removed, because it carries notes.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T20:18:51Z

### Sweep log, 2026-09-28 (one entry per merge, written by the orchestrator in a batch, because two lanes ran at once)

- **PR #123** (55e876a): set up the sweep. It records the grant, the decisions on BFSBPJ and KB7FXC, promotes the seven tickets, and archives TKT-01M3AZGKBP.
- **TKT-01M393S6P7, PR #124** (7927806).
  - Test-only change: the group-commit fsync test waits for the first append to reach Sync, using `synctest.Wait`, before queuing the other 16.
  - Under `-race -cpu 1,2,12` it now fails 0 of 600 runs, down from 12 of 600. This was the flake behind main's red push runs 492, 606, 635 and 712.
  - Review #725 clean, CI #724.
- **TKT-01M39VNEC1, PR #125** (20ee947).
  - A refused thread page now either rejects or is replaced by the cached page; it is never returned to the caller.
  - An ordering table of 36 cases found and fixed an older bug: a stale held copy of a page stayed in `storeThreadPage`.
  - Review ab44506a clean, carried; CI 731. Run 728 was a phone a11y timeout while another run held the runner.
- **TKT-01M3A8V7XC, PR #126** (86e1e2e).
  - CatchUp now publishes each quarantine as it is found, and replaces the whole set only when a scan completes.
  - This fixes review 468's deferred high. Before, an interrupted sweep could hide a corrupt thread from /healthz and from blob GC, and GC could then delete blobs that only the short log named.
  - Review 14e11e60 clean, CI 735.
- **TKT-01M3BFSBPJ, PR #127** (4302ad1).
  - The quote summary is plain text, and the source link is the first line of the blockquote. The renderer version is now 4.
  - The axe nested-interactive exception is removed.
  - No migration or reindex is needed: HTML is held only in memory caches.
  - The phone attribution line has a margin, and a new edge-press targets check covers it.
  - Review 0c63d1c6 clean, CI 741.
- **TKT-01M3AFHGJD, PR #128** (2f09845).
  - Test-only change. End-to-end tests pin docs/09's approval-bypass risk across the callback, /auth/register, /auth/signin, and sessions that outlive a status change. **No bypass was found.**
  - Review 1231's low finding was fixed. The re-review was clean and carried. CI 755; run 751 was a Playwright timeout.
- **TKT-01M3ANX1NF, PR #129** (9b960cf, blocked for a decision), then **PR #130** (9c415ec).
  - The maintainer's decision: "Forbid for new handles".
  - `ValidateHandle`, `CheckHandle` and /auth/register refuse a trailing '-'.
  - The database constraint is unchanged and no migration was needed; `HandleCases.Grandfathered` covers existing handles.
  - Review 7bcd74bb clean, CI 760.
- **TKT-01M3KB7FXC, PR #131** (e36fd8c).
  - Migration 0010 makes `users.id` AUTOINCREMENT, and the migration runner now runs with foreign keys off and runs `foreign_key_check` before commit.
  - Reindex raises the member-id floor above every id the logs name, and a missing author renders as `deleted-user-<id>`.
  - Draft TKT-01M3MSFX3S filed (reclaiming posts under a lost id).
  - Review #766 clean, CI #765.

Main's push CI was green after the last merge, on e36fd8c. Red push runs caused by cancellation were ignored: Forgejo writes a cancelled run as failure, marked "Has been cancelled".

The dogfood instance was redeployed at e36fd8c. The data directory was backed up first to `~/.local/state/ketju/backups/2026-09-28-before-e36fd8c`. Migration 0010 applied: users is AUTOINCREMENT, the sequence is at 1, `foreign_key_check` is empty, /healthz is ok, and /auth/signin still redirects to Authentik.

## Summary

The sweep is complete. All seven Phase 1 follow-ups merged under the docs/12 gate on 2026-09-28, in PRs #124-#131. No approval bypass was found. The maintainer decided the two open questions (quote markup; the member-id floor), and a third that arose (new handles may not end in '-'). TKT-01M3AZGKBP is archived. Main is green at e36fd8c, and the dogfood instance runs it after a pre-migration backup. Filed: TKT-01M3MSFX3S (reclaiming posts under a lost member id) and a draft for Playwright timeouts under runner contention.
