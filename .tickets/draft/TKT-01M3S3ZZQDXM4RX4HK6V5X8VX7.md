---
schema: 3
id: TKT-01M3S3ZZQDXM4RX4HK6V5X8VX7
title: Seed a moderator post, a tombstone and an edit in the e2e thread
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T12:17:55Z
updated_at: 2026-10-01T06:45:52Z
created_by:
  id: agent:claude-code/phase2-run-39wbn4q
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

Found comparing the thread screenshots with `docs/mockups/` (TKT-01M39WBN4Q, divergences T11 and P8).

The mockups' thread page shows a moderator's post with its role badge, a tombstone ("Post removed by its author."), an "edited" mark, a quote, an attachment and a tag chip. The e2e seed's main thread (`cmd/ketju/e2e.go`) has only members' posts, removes nothing, edits nothing, attaches nothing, and quotes only on page 1. So the committed screenshots cannot show those parts, and `e2e/thread.spec.ts` cannot assert the badge or the tombstone row.

### What to do
Seed, on page 3 of the main thread, a post by the seeded moderator (mona), a post removed by its author, and an edited post, keeping every ordinal and reply the suite already names. Then assert the role badge and the tombstone row in `e2e/thread.spec.ts` and refresh the thread screenshots. Attachments wait for TKT-01M29CXK0 (the blob store).

## Acceptance criteria

- [ ] The e2e seed's main thread has, on page 3, a moderator's post, a post removed by its author and an edited post, and every existing spec still passes
- [ ] e2e/thread.spec.ts asserts the role badge and the tombstone row, and the thread screenshots are refreshed
