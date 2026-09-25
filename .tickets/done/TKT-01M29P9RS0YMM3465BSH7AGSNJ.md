---
schema: 3
id: TKT-01M29P9RS0YMM3465BSH7AGSNJ
title: Fix seven document inconsistencies found by the design survey
type: chore
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area:docs
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-12T02:14:17Z
updated_at: 2026-09-25T00:31:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

A survey of the design documents during TKT-01M29GGG3W turned up seven internal inconsistencies. Two were fixed in passing by that ticket's own edits, so four remain plus one cosmetic. None is urgent and all are cheap.

### The bootstrap admin config key has three spellings for two settings

`docs/05:12` names them `bootstrap.admin_subjects` and `bootstrap.admin_email`, singular. The TOML example at `docs/05:43` writes `admin_emails`, plural. And `admin_subjects` appears in no config example anywhere. Somebody following the prose will write a key the parser does not read.

### Archiving authority differs across three documents

`docs/03:117` lists `ThreadArchived` with no actor named. `docs/08` says "Archiving (admin) is a stronger state". `docs/07:75` lists "thread lock/pin/move/rename/tag/archive" together under routes described as role-gated, without separating the admin-only archive from the moderator actions. A reader cannot tell whether a moderator can archive.

### Slow mode is the only parameterised primitive with no default

Edit window (24 h), self-delete window (15 min), grace edits (5 min), and the flag auto-hide threshold (3) all state defaults. Slow mode at `docs/08:14` gives only "min seconds between posts per user". Whoever implements it will invent a number.

### docs/10 numbering runs out of order

Items 1-21, then the Mobile section opens at 25-29, then Operations returns to 22-24. Cosmetic, but it makes citing an item by number unreliable.

### Already fixed, listed so nobody re-files them

The index entry's 4 reserved bytes were described both as committed to segment rollover and as generic slack; docs/03 and docs/09 now say they are spoken for. The frame's 2-byte Flags field was declared and never explained; docs/03 now has a Frame flags section.


### Added in Phase 1 grooming (2026-09-24)

An eighth mismatch, found while planning: docs/05 writes the IdP groups claim
as `claims.groups`, but the config loader in `internal/config` reads
`groups_claim`. The code is the reference, because it shipped and has tests.

The slow-mode default is off. Slow mode itself is Phase 2 work
(TKT-01M29CRJ50, Implement per-thread slow mode). Phase 1 only needs the create
path to honour a setting that is absent unless a moderator sets it. The
interval a moderator picks is a question for that ticket.

## Acceptance criteria

- [x] The bootstrap admin config keys agree between the prose in docs/05 and the TOML example, match what internal/config reads, and every key named in prose appears in an example
- [x] Archiving authority is stated once and consistently across docs/03, docs/07, and docs/08
- [x] docs/08 states that slow mode is off by default
- [x] docs/10 item numbers ascend in file order, with any renumbered item's old number noted so existing citations still resolve
- [x] The groups claim key is spelled the same in docs/05 and internal/config

## Implementation plan

Revised at claim (2026-09-25). This is docs only, plus one config error message. The ticket body was already repaired in grooming, and docs/10 has no uncommitted edits now.

1. **Bootstrap keys.** internal/config is the reference: `bootstrap.admin_emails` and `bootstrap.admin_subjects`, the latter written as `"provider_key:subject"`.
   - docs/05's prose now names both, spelled as the loader reads them.
   - Its TOML example gains an `admin_subjects` line.
2. **Groups claim.** docs/05's example `claims.groups` becomes `groups_claim`, which is what the loader reads.
3. **Archive authority** is stated once, in docs/08's Primitives table: a new row, Archive thread | admin | `ThreadArchived` | not reversible, since there is no unarchive record.
   - docs/03's row 30 says it is an admin action (08) and read-only for moderators too.
   - docs/07's admin route list no longer lumps archive in with the moderator thread actions, and points at docs/08's table as the one statement of authority.
4. **Slow mode.** The docs/08 row says it is off by default.
   - `internal/config/problem.go`'s moved-key message for `slow_mode` said it was "changed per forum in the admin UI", which contradicted docs/08's per-thread moderator setting. It now says off by default, set per thread by a moderator.
5. **docs/10.** The Operations section (22–24) moves before Mobile (25–29), so numbers ascend in file order with no renumbering and every existing citation still resolves.

Verification:
- A scratch test, not committed, loaded docs/05's TOML example through `config.Load`: no errors or warnings, both bootstrap lists read, and `groups_claim` read.
- A grep for the old spellings finds nothing.
- docs/10's item numbers read 1–34 in order.

### Settled in grooming (2026-09-24)

- The description and criteria were repaired in grooming. The slow-mode default is off. The eighth item is the groups_claim spelling, and internal/config is the reference for it.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:28Z

Pulled into Phase 1 during grooming (2026-09-24). Two of the seven inconsistencies land on Phase 1 tickets: the bootstrap admin config key is spelled three ways, which TKT-01M29C800G (Bootstrap the first admin) must pick one of, and the slow-mode default is missing, which a criterion of TKT-01M29CA9B9 (Implement create thread and reply) depends on. Both now depend on this ticket, so each is settled in the docs before code reads them.

**agent:claude-code/phase1-run-415af6aa** at 2026-09-25T00:31:48Z

Done as the plan says. The docs/05 example was verified by loading it through config.Load in a throwaway test, which was deleted and not committed: no errors, no warnings, admin_emails and admin_subjects both read, and groups_claim read on the authelia provider. A grep for admin_email, admin_subject and claims.groups in docs/ finds nothing. docs/10's numbered items run 1–34 in file order. The only code change is the wording of config's moved-key message for slow_mode; config tests pass.

## Summary

Docs agree now.
- docs/05 names bootstrap.admin_emails and admin_subjects as internal/config reads them, and its example has both; claims.groups is now groups_claim.
- Archive authority is stated once, as an admin row in docs/08's Primitives table; docs/03 and docs/07 point there.
- Slow mode is off by default, in docs/08 and in config's moved-key message.
- docs/10's Operations section moved ahead of Mobile, so items ascend without renumbering.
