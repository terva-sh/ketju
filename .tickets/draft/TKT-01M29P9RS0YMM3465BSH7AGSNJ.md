---
schema: 3
id: TKT-01M29P9RS0YMM3465BSH7AGSNJ
title: Fix seven document inconsistencies found by the design survey
type: chore
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:docs
  - effort:S
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-12T02:14:17Z
updated_at: 2026-09-12T02:14:17Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
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

The index entry's 4 reserved bytes were described both as committed to segment rollover and as generic slack; docs/03 and docs/09 now say they are spoken for. The frame's 2-byte Flags field was declared and never explained; docs/03 now has a Frame flags section.</description>
<parameter name="acceptance_criteria">["The bootstrap admin config keys agree between the prose at docs/05:12 and the TOML example, and every key named in prose appears in an example", "Archiving authority is stated once and consistently across docs/03, docs/07, and docs/08", "Slow mode has a stated default interval in docs/08, like every other parameterised primitive there", "docs/10 item numbers ascend in file order"]
