---
schema: 3
id: TKT-01M3Q7Q1T4AD039RJKEQ73P6K5
title: Decide how a mention of an anonymized account renders
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:identity
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T18:44:28Z
updated_at: 2026-09-29T18:44:28Z
created_by:
  id: agent:claude-code/phase2-run-ckrfs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ckrfs
  name: ""
extensions: {}
---

## Description

TKT-01M29CKRFS (Implement the account lifecycle: deactivate, suspend, anonymize) has a criterion that says a mention of an anonymized account shows `deleted-user-<id>`. As built, it does not, and this draft asks a person to decide which behaviour is right.

### What happens now

A mention is resolved by the handle typed in the post body, at every render (`threads.Service.handle`, `internal/service/threads/view.go`). After anonymization the account's handle is `deleted-user-<id>`, so Bob's `@alice` no longer resolves. It renders as the plain text Bob typed, `@alice`, linking nobody, and it names no display name or avatar. The render cache and the response cache see the change at once. `TestAnonymizedAttributionEverywhere` pins this.

### The options

- **A. Plain text (as built).** The post body is not edited (docs/05), so the typed `@alice` stays visible as text. Nothing on the page ties `alice` to `deleted-user-<id>`, which follows the 2026-09-29 decision that an anonymized account's old handle gets no redirect, because a redirect would tie the two. TKT-01M3NRP3W9's read-time mention rule gives the same result: its `handle_releases` row for an anonymized account names no user.
- **B. `@deleted-user-<id>`, linked.** Resolve the mention by the user id the post recorded (the `mentions` projection) and draw the current handle. The page would then show that Bob's `@alice` meant the account now called `deleted-user-<id>`, which is the link the no-redirect decision avoided. It also needs the id-pinned resolution that TKT-01M3NRP3W9 builds.

### Recommendation

A. Then TKT-01M29CKRFS's fifth criterion should read "mentions of the account no longer link it", and it can be ticked.
