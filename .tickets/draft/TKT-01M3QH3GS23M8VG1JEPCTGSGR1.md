---
schema: 3
id: TKT-01M3QH3GS23M8VG1JEPCTGSGR1
title: Add agent provisioning controls to the admin users screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:28:33Z
updated_at: 2026-09-29T21:28:37Z
created_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
extensions: {}
---

## Description

The admin users screen's controls for agent accounts. The server half is TKT-01M29P9RSW (Implement agent accounts, provisioning, and the agent credential), which built the routes, the DTOs and the client types. It left the screen alone because the client lane held the committed dist at the time.

What the screen needs, from that ticket's plan, step 8:
- A "New agent" form: handle, display name, and "issue a token now". It posts `POST /api/v1/admin/users` with `{kind: "agent", handle, display_name, issue_credential}` and answers `CreatedAgent`.
- After a creation or an issue, a one-time token panel with a copy button and the line "This token is shown once. Store it now." The token is in that response and never again.
- On an agent's detail, from `AdminUserDetail.agent`: when the live credential was issued and last used, "Issue new token" (with a confirmation that it replaces the old one, `POST /admin/users/{handle}/credential`), and "Revoke" (`DELETE` on the same path, 204).
- Limit fields for burst and refill seconds, showing the site default (`default_burst`, `default_refill_seconds`) as the placeholder. An empty field sends null. `PUT /admin/users/{handle}/agent-limits` with `{burst, refill_seconds}` answers the detail.

All of it is admin-only and in the lazy admin chunk. The built client is committed, so run `go generate ./internal/web/spa`, and only one lane may have a PR open that changes the dist.

## Acceptance criteria

- [ ] An admin can create an agent from the users screen, with or without a token
- [ ] A new token is shown once in a panel with a copy button and the line This token is shown once. Store it now.
- [ ] An agent's detail shows its credential's issue and last-use times, and offers Issue new token (confirmed) and Revoke
- [ ] Burst and refill fields show the site defaults as placeholders and send null when empty
- [ ] go generate ./internal/web/spa is committed and the bundle budget holds
