---
schema: 3
id: TKT-01M3ANX1NF3XWCJBS3DH9QZNFC
title: Handles ending in '-' can never be mentioned
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T21:42:17Z
updated_at: 2026-09-28T18:06:55Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The handle rule (docs/04, the users CHECK, and domain.ValidateHandle) accepts a handle that ends in `-`, such as `bob-`. The mention parser in internal/render/mention.go strips a trailing `-` as sentence punctuation, the same way it strips `.`. So `@bob-` is read as `@bob`:

- It mentions a different user, `bob`, when one exists.
- It mentions nobody when one does not.

A user whose handle ends in `-` can therefore never be mentioned, and nothing reports it.

TKT-01M38XJJKM (Serve handle autocomplete for mentions) works around this by leaving such handles out of the suggestions, because suggesting one would insert a mention of somebody else. That is a workaround, not a fix.

### Options
- Forbid a trailing `-` in the handle rule: in docs/04, the CHECK (a migration), `domain.ValidateHandle` and its shared cases. Existing handles would need a rename policy (TKT-01M29BSPEN).
- Make the parser keep a trailing `-` when the handle with it resolves. That makes parsing depend on resolution, and changes what existing posts render (Version bump).

This is a format and product decision, so it is filed as a draft.
