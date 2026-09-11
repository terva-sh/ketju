---
schema: 3
id: TKT-01M29C801M9WBEAVCCC6AXNZ4Z
title: Define the shared DTOs and the golden JSON test harness
type: task
status: draft
status_reason: null
priority: urgent
due_on: null
labels:
  - area:api
  - area:test
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-principles
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-11T23:18:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The structs the JSON API serializes are the structs the future HTML templates will render. One definition, two presentation layers, which is the concrete mechanism behind the whole non-JS story.

Golden JSON files per endpoint are the contract. They catch an accidental field rename, and they are also the document a client developer reads instead of asking. docs/09 names Go 1.27 json/v2 strictness as a risk, and goldens from Phase 1 are the mitigation: a duplicate key or a UTF-8 problem shows up as a failing golden rather than as a runtime surprise later.

## Acceptance criteria

- [ ] DTOs live in one package that neither store code nor handler code duplicates
- [ ] A golden file exists per endpoint and a field change fails the test with a readable diff
- [ ] Regenerating goldens is one documented command, so updating them is deliberate rather than reflexive
- [ ] Goldens cover the anon, member, and moderator viewer classes where the shape differs
