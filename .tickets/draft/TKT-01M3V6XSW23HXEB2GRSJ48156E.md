---
schema: 3
id: TKT-01M3V6XSW23HXEB2GRSJ48156E
title: Decide whether unlink, link and sign-out-everywhere need recent sign-in
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - area:security
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:47:38Z
updated_at: 2026-10-01T07:51:25Z
created_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
extensions: {}
---

## Description

A question for the maintainer, found while grooming TKT-01M29CCC0X (Implement the settings endpoints and session management). It is not a blocker. The ticket is planned as docs/05 and docs/07 describe it, with no step-up check.

With linking (#178) and unlinking (TKT-01M29CCC0X), a stolen session cookie becomes a permanent takeover. Whoever holds the cookie can:
1. link an identity they control through `/auth/link`;
2. remove the owner's identities through `DELETE /settings/identities/{id}`, because the last-identity rule counts the attacker's new identity;
3. end the owner's sessions through `DELETE /settings/sessions`.

The owner then has no way back in, and an admin is the only remedy.

docs/05 asks for no recent sign-in before any of these, and adding one would change how sessions work, which is docs/14 class 1. Options:
- require a sign-in within N minutes for unlinking, linking and "everywhere";
- notify the account in-app when an identity is linked or removed, which needs the notification generator, TKT-01M29CRJ2T;
- leave it, on the grounds that a stolen cookie is already the whole account.
