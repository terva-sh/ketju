---
schema: 3
id: TKT-01M29BN370YYWN3WEFJ7DPDTWW
title: Deferred features from the Later and Maybe list
type: epic
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - effort:XL
  - placeholder
assignees: []
milestone: later
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:roadmap-later
    path: docs/09-roadmap.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-11T23:08:14Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Placeholder holding the docs/09 "Later / maybe" list so it stays visible without cluttering the queue. Nothing here is broken down, and nothing here is committed to.

Timeline scrubber backed by the since lens. Digest lens, which needs question 16 decided and real reply and quote volume. Bookmarks with reminder times. Invite registration mode. Local passwords with SMTP, email verification, and password reset. Email notification digests, which need question 12 decided and a mail library chosen. Reactions as an append-only table that never reorders anything. Personal API tokens on a Bearer path that is never issued to browser code. ActivityPub read-only export. Built-in ACME TLS. Multi-language stemming through Go FTS5 tokenizers. Web Push, which needs VAPID keys and a subscription table. Offline reading through a service worker, which is plausible because thread pages are immutable per log length. Sub-topic dividers for drift regions.

Native apps are out of scope. The installable web app is the mobile story.

Pull an item out of here into its own epic when there is a reason, and say what the reason was.

## Acceptance criteria

- [ ] Nothing in this epic is worked without first being promoted into an epic of its own with a stated reason
- [ ] Reactions, if they ever arrive, never affect ordering anywhere
