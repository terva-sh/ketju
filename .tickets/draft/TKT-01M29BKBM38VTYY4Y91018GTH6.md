---
schema: 3
id: TKT-01M29BKBM38VTYY4Y91018GTH6
title: Approval-mode registration and the forms system
type: epic
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:L
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M29BKBKZW26HNHFM1YFFGVZC
blocks_on: children
references:
  - ref: design:identity-approval
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-09-11T23:31:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Registration gated behind a moderator decision, and the small declarative form system that carries it.

In approval mode a visitor who authenticates but has no user gets an identity row with user_id NULL and a pre-user session bound to that identity. A pre-user session reaches the application form, the status page, and logout, and nothing else. Only on approval does a users row exist.

The form is data rather than code: a JSON field list stored in form_versions, validated server-side from the schema, rendered by the Preact client from the same schema, and renderable by a future html/template client with no new work. Versioning means old applications stay readable after an admin edits the form.

This epic is the exit criterion for Phase 2: a club can open registration to strangers safely.

## Acceptance criteria

- [ ] A pre-user session is rejected by every endpoint except the application form, the status page, and logout
- [ ] Approving an application creates the users row, links the identity, and writes an audit_log entry
- [ ] Denying with 'block this subject' keeps the identity with user_id NULL and refuses re-application
- [ ] Editing a form creates a new version and applications submitted against the old version still render
- [ ] Server-side validation is driven by the stored schema, not by hand-written per-field code
