---
schema: 3
id: TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
title: Link to the link-a-sign-in page from the client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: TKT-01M29CKRES18SZSCQRV8XSNBT2
dependencies:
  - TKT-01M29CKRES18SZSCQRV8XSNBT2
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T23:46:54Z
updated_at: 2026-09-29T23:49:26Z
created_by:
  id: agent:claude-code/phase2-run-KRES
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-KRES
  name: ""
extensions: {}
---

## Description

Account linking (TKT-01M29CKRES) landed server-side: `GET /auth/link` is the "link a sign-in" page, and its forms post to `POST /auth/{provider}/link` (docs/05 "Login flow" step 3, docs/07 "URL scheme"). docs/05 says the action lives "in settings", but the client has no settings screen until TKT-01M29CXK4 (Build the user settings screens, phase 4), so a member can reach the page today only by typing its address.

This ticket adds a way to reach it from the client before then: a "Link a sign-in" entry in the signed-in member's account menu (or wherever the shell keeps account actions), as a full page load to `/auth/link?return_to=<current path>`, since `/auth/*` is outside the client's routes. It is hidden for anyone who is not an active member, and for a pre-user.

### Details

- The server page already sends a signed-out reader to `/auth/signin` and back, so the client needs no state of its own.
- When TKT-01M29CXK4 builds `/settings/identities`, its "add" button is the same link, and this entry can move there.
- The committed client dist changes, so only one lane may hold it at a time (docs/13 "Running two lanes").

## Acceptance criteria

- [ ] An active member can reach /auth/link from the client in one step, at 390 px and 1440 px
- [ ] The entry is absent for a signed-out reader and a pre-user
- [ ] The link carries the current path as return_to
