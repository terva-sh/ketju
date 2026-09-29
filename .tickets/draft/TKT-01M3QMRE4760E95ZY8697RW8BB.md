---
schema: 3
id: TKT-01M3QMRE4760E95ZY8697RW8BB
title: Add the suspension controls to the admin users screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CP2QKKZSK4PDB40EJHC3P
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T22:32:24Z
updated_at: 2026-09-29T22:32:27Z
created_by:
  id: agent:claude-code/phase2-run-cp2qk
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-cp2qk
  name: ""
extensions: {}
---

## Description

Put the suspension controls into the admin users screen's detail panel (web/client, admin chunk). The server side is TKT-01M29CP2QK (Implement user suspension from the moderator tools). It shipped without UI because the client lane held the committed dist at the time (the docs/13 lane rule).

The routes are in docs/07 "Users, groups and permissions". Each answers 204 `no-store`:
- `POST /api/v1/admin/users/{handle}/suspend` `{reason, until?, hide_posts?}`
- `POST /api/v1/admin/users/{handle}/unsuspend` `{restore_posts?}`
- `POST /api/v1/admin/users/{handle}/posts/hide`
- `POST /api/v1/admin/users/{handle}/posts/restore`

The groomed plan for TKT-01M29CP2QK (step 7) described the screen:
- **Suspend** opens a dialog with a required reason (1 to 500 characters) and an end: 1 day, 7 days, 30 days, until lifted, or a date. It has a "Hide their posts" checkbox, explained as "Members see 'removed by a moderator'; you can restore them later".
- **Unsuspend** has a "Restore posts hidden with this suspension" checkbox.
- A standalone "Restore hidden posts" action works on any account whose posts a suspension hid, which is how a moderator restores the posts of a suspension that expired.
- The status badge shows the reason and the end time.
- A moderator sees the controls only on members, and an admin on anyone but themselves. The server refuses anything else, and answers `conflict` for the last active admin.
- Moderators open the users screen read-only apart from these controls.

`GET /admin/users/{handle}` does not return the suspension's reason or end yet. Adding them to `AdminUserDetail` is a server change for whoever takes this ticket.

Tests: the dialog's validation and the badge text. `TestTheAdminBundleIsSeparate` stays green, and the bundle stays within the 60000 B gzip budget.
