---
schema: 3
id: TKT-01M29P9RSW21SCES335WERTBSF
title: Implement agent accounts, provisioning, and the agent credential
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:identity
    path: docs/05-identity-and-accounts.md
  - ref: design:api
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-12T02:14:17Z
updated_at: 2026-09-12T02:14:17Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The implementation of the positions settled in TKT-01M29GGG3W. The design is written into docs/04, docs/05, docs/07, and docs/08; this builds it.

An agent is a first-class member: `users.kind = 'agent'`, its own handle, its own standing, its own credential. An admin provisions it and it never self-registers, because it has no browser and no IdP subject. Roles apply unchanged, so an agent can be a member, a moderator, or an admin.

Lands in Phase 2 because provisioning an account and issuing its credential are one admin screen, and the users and permissions screens are already Phase 2. Splitting them across phases means building that screen twice.

The credential belongs to the agent rather than to the human who created it, so revoking it stops one agent and nothing else. It is a hashed bearer token with no idle expiry, because an agent may be quiet for weeks and then work.

Note the one subtlety worth getting right: the machine-authored frame flag bit is set by the server from `users.kind` at append time, and is never accepted from a client. It records what was true when the post was written, which is why it is in the log and not only derived from the account. An account whose kind changes later does not rewrite its history.</description>
<parameter name="implementation_plan">Depends on the admin users screen rather than duplicating it. Read docs/05 "Agent accounts" and docs/07 "Agents on the API" before starting; both were written for this ticket.

## Acceptance criteria

- [ ] users.kind exists with values human and agent, defaulting to human, and is orthogonal to role
- [ ] An admin can create an agent account and issue its credential in one flow, and revoke the credential without touching the account
- [ ] The credential authenticates as Authorization: Bearer, is stored hashed, has no idle expiry, and creates no session row
- [ ] Suspending or deactivating an agent revokes its credential, and the audit log records the actor either way
- [ ] An agent can hold the moderator and admin roles, and nothing in the code special-cases agent-ness as a permission
- [ ] The server sets the machine-authored frame flag bit from users.kind at append time, and rejects or ignores any client-supplied value
- [ ] The index entry carries the mirroring machine-authored bit so a lens can filter without decoding bodies
- [ ] Agent writes are charged against a per-agent token bucket with a site default, and exceeding it is 429 with Retry-After rather than a queue
- [ ] An agent sets its read position only through PUT /threads/{id}/read, with no beacon and no mark-on-render path
- [ ] author.kind and the per-post machine_authored flag appear in the thread response, and a golden JSON test pins both
