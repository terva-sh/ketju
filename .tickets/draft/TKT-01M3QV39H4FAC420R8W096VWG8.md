---
schema: 3
id: TKT-01M3QV39H4FAC420R8W096VWG8
title: Clear the agent limits form's Saved status when a field is edited
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:XS
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T00:23:12Z
updated_at: 2026-09-30T00:23:12Z
created_by:
  id: agent:claude-code/phase2-run-qh3gs2
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qh3gs2
  name: ""
extensions: {}
---

## Description

Terva review 1510 on PR #179 (head a064d60), low: after a successful save of an agent's write limits (web/client/src/screens/admin/AgentPanel.tsx, Limits), editing either field leaves 'Saved.' beside values that have not been saved. An edit made while the PUT is in flight is also labelled saved when the answer arrives. Clear the saved status on any input, and on the answer set it only if the fields still hold what was sent. Client only; it regenerates the committed dist, so it takes the client lane.

## Acceptance criteria

- [ ] Editing either limit field after a save removes 'Saved.' (AgentControls.test.tsx)
- [ ] An answer to a save does not show 'Saved.' when a field was edited while the save was in flight (AgentControls.test.tsx)
