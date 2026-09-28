---
schema: 3
id: TKT-01M3B12ZZ403NEPFH3DASEW1D8
title: Define thread tag rules so new threads can carry tags
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T00:57:46Z
updated_at: 2026-09-28T05:02:26Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`POST /forums/{slug}/threads` (TKT-01M29CA9B9) refuses any tag with `validation-failed`. Nothing documents what a tag may be, and a ThreadCreated record keeps its tags in the log forever. docs/03 names tags in ThreadCreated and ThreadTagged, and docs/06 relies on a tag to mark agent threads, but neither says:

- the characters a tag may contain, and its case
- its length, and how many tags a thread may have
- whether tags are free text or chosen from a vocabulary each forum keeps
- who may set tags when a thread is created

Decide these in docs/04 or docs/08. Then accept tags in `CreateThread` (internal/service/threads/create.go) and remove the refusal.

## Acceptance criteria

- [ ] docs name the tag syntax, limits and who may set them
- [ ] POST /forums/{slug}/threads accepts tags that follow the rules and refuses others with validation-failed
