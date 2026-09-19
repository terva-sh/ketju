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
updated_at: 2026-09-19T23:21:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
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
- [ ] Application submission is rate limited per identity as well as per address, using the pre-user session as the key; this is the second limiter stage docs/08 describes, and it runs after session resolution

## Comments

**agent:claude-code/web** at 2026-09-19T23:21:33Z

The new rate-limit criterion arrived here from TKT-01M29C800X "Set the CSRF
posture and write rate limits", which built the transport-level limiter and
could not build this half.

docs/05 asks for limits on `/auth/*` and application submission "keyed by IP and
identity". The IP half shipped in C800X. The identity half cannot live where
that limiter does: it runs at chain position 5, deliberately outside session
resolution, so that an unauthenticated flood does not cost a database query per
request. No viewer exists at that point, so there is nothing to key on.

Application submission is the one place an unapproved visitor does have an
identity -- the pre-user session this epic defines -- which is why the criterion
belongs to this epic rather than to the middleware.

TKT-01M29P9RSW "Implement agent accounts, provisioning, and the agent
credential" needs the same post-session stage for its per-agent token bucket.
Whichever of the two lands first should build the stage rather than a
single-purpose limiter, and say in its summary that it did.
