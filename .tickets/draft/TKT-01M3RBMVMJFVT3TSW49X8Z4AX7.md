---
schema: 3
id: TKT-01M3RBMVMJFVT3TSW49X8Z4AX7
title: Stop the seal round-trip test failing when ciphertext contains abc
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T05:12:24Z
updated_at: 2026-09-30T08:42:51Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

`TestRoundTrip` in `internal/auth/seal/seal_test.go` fails whenever the sealed value happens to contain the substring `abc`, which is the plaintext it checks for (line 37: `strings.Contains(v, "abc")`). The sealed value is random base64url, so any three-character window matches `abc` with probability 64^-3. Over a value about 90 characters long, that is roughly 3 failures in 10,000 runs.

It happened on CI run 1325, for PR #206 at head 2a5ab87, a ticket-only change. The value printed was `...5c6abckU...`.

### Fix

Check for a plaintext that cannot be mistaken for chance. Either seal a long, distinctive value and check for that, or decode the sealed value and check that the raw bytes of the encoded plaintext are absent. Do not weaken the check to nothing: its point is that a seal hides its contents.

Reported by the Phase 2 run orchestrator on 2026-09-30.
