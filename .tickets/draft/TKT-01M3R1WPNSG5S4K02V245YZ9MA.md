---
schema: 3
id: TKT-01M3R1WPNSG5S4K02V245YZ9MA
title: Expose hard redaction in the post DTO and client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
dependencies:
  - TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T02:21:56Z
updated_at: 2026-09-30T02:22:05Z
created_by:
  id: agent:claude-code/phase2-run-cxjz9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-cxjz9
  name: ""
extensions: {}
---

## Description

TKT-01M29CXJZ9 (Build the offline hard-redaction tool) serves a hard-redacted post as a moderator removal with no reason: `deleted: true`, `removal: "moderator"`. The index knows the difference (EntryRedacted, bit 7), but the post DTO has no field for it, so a moderator cannot tell a redacted post from a hidden one, and any restore the client offers on it is answered with conflict.

Adding `redacted` to `internal/dto` was tried in that lane and reverted: `web/client/src/dto-contract.ts` pins the Go goldens to `dto.ts` exactly, and that lane was barred from touching web/client and the built dist.

### Scope

- `dto.Post` and `dto.HistoryPost` gain `redacted bool` (omitempty), set from `EntryRedacted` in `internal/service/threads/view.go` and `posts.go`.
- Goldens under `internal/dto/testdata` and `internal/web/api/testdata` regenerated; `dto.ts` and the contract test updated.
- The client shows a redacted post as removed permanently and offers no restore or edit on it; `go generate ./internal/web/spa` rebuilds the dist.
