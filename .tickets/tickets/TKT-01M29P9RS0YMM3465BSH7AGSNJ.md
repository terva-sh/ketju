---
schema: 3
id: TKT-01M29P9RS0YMM3465BSH7AGSNJ
title: Fix seven document inconsistencies found by the design survey
type: chore
status: ready
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
updated_at: 2026-09-24T05:21:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
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

- [ ] The bootstrap admin config keys agree between the prose in docs/05 and the TOML example, match what internal/config reads, and every key named in prose appears in an example
- [ ] Archiving authority is stated once and consistently across docs/03, docs/07, and docs/08
- [ ] docs/08 states that slow mode is off by default
- [ ] docs/10 item numbers ascend in file order, with any renumbered item's old number noted so existing citations still resolve
- [ ] The groups claim key is spelled the same in docs/05 and internal/config

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

The ticket file is malformed and must be repaired at claim. It has no `## Acceptance criteria` section. The four criteria sit inside the Description, after a stray `</description>` and a `<parameter name="acceptance_criteria">` fragment, which look like an agent's tool-call text pasted into the body. Before anything else, move them into a real section with `git ticket update --ac …` (or `check --fix`, if it detects this) and delete the stray lines.

### Approach
1. Bootstrap keys: `internal/config/config.go` already reads `bootstrap.admin_emails` (plural) and `bootstrap.admin_subjects`, and `env.go` exposes `KETJU_BOOTSTRAP_ADMIN_EMAILS` and `KETJU_BOOTSTRAP_ADMIN_SUBJECTS`. The code is the fixed point. Edit docs/05 "Principles" to say `bootstrap.admin_emails`, and add `admin_subjects = ["google:1234…"]` to the docs/05 TOML example.
2. Also in that example, and outside the ticket's list: docs/05 writes `claims.groups = "groups"`, but the loader's key is `groups_claim` (config.go `Provider.GroupsClaim`). `DisallowUnknownFields` would refuse the documented spelling. Fix it in the same edit, and mention it in the commit as an eighth instance.
3. Archiving authority: docs/08 "Locking semantics" says admin. Make docs/03 "Record types" row 30 name the actor, and split docs/07 "Moderation and admin" so archive is listed as admin-only, apart from the moderator actions. The line references in the ticket are stale (docs/03 row 30 is now around line 285, and the docs/07 list is around line 113), so locate these by grep rather than by line number. docs/08's "Primitives (v1)" table has no archive row at all. Add one ("Archive thread | admin | `ThreadArchived` | no") so authority is stated once, in the table.
4. Slow mode default: add a default to the docs/08 "Primitives" slow-mode row. No doc proposes a number, so this is a product decision, not a consistency fix. Ask the human for it rather than inventing one. Also note that `settings` stores slow mode "per thread", while `internal/config/problem.go` `movedKeys` says "changed per forum in the admin UI". Reconcile that wording in the same edit.
5. docs/10 numbering: move the "Operations" section (22–24) before "Mobile" (25–29), so numbers ascend in file order without renumbering, because other documents cite these numbers ("item 24"). The working tree currently has uncommitted edits to docs/10 from another session. Do not start this item until those are committed, and rebase over them.

### Files and packages
Docs only: docs/03, docs/05, docs/07, docs/08, docs/10, and the ticket file itself. Optionally a comment fix in `internal/config/problem.go` (per forum or per thread).

### Tests
- AC1: `grep -n "admin_email\b\|admin_subject\b" docs/` returns nothing. Every bootstrap key in the docs/05 prose appears in its TOML example. A throwaway check: paste the docs/05 TOML example into a temp `config.toml` with a `data_dir`, and run it through `config.Load` in a scratch test (not committed) to prove the loader accepts the documented keys.
- AC2: `grep -n -i archiv docs/0[378]*.md` reads consistently: admin everywhere, and stated once in the docs/08 table.
- AC3: the docs/08 slow-mode row carries a default in the same form as the other defaults ("default N s").
- AC4: `grep -n '^[0-9]\+\. \*\*' docs/10-open-questions.md` yields ascending numbers.

### Design
docs/05-identity-and-accounts.md "Principles" and "Provider configuration". docs/08-moderation.md "Primitives (v1)" and "Locking semantics". docs/07-api-and-clients.md "Moderation and admin". docs/03-thread-storage.md "Record types". docs/10-open-questions.md (whole file ordering).

### Interactions
It is independent of the code tickets. The slow-mode default feeds the settings key list in BYYJ0. Archive authority feeds TKT-01M29CKRGS and the admin API.

### Settled in grooming (2026-09-24)

- The description and criteria were repaired in grooming. The slow-mode default is off. The eighth item is the groups_claim spelling, and internal/config is the reference for it.

## Notes

**agent:claude/phase1-groom** at 2026-09-24T05:05:28Z

Pulled into Phase 1 during grooming (2026-09-24). Two of the seven inconsistencies land on Phase 1 tickets: the bootstrap admin config key is spelled three ways, which TKT-01M29C800G (Bootstrap the first admin) must pick one of, and the slow-mode default is missing, which a criterion of TKT-01M29CA9B9 (Implement create thread and reply) depends on. Both now depend on this ticket, so each is settled in the docs before code reads them.
