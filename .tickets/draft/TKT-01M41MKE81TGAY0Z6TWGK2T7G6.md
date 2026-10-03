---
schema: 3
id: TKT-01M41MKE81TGAY0Z6TWGK2T7G6
title: Leave the avatar hash out of author blocks sent to signed-out readers
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:api
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-03T19:42:05Z
updated_at: 2026-10-03T19:42:05Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

The maintainer decided on 2026-10-02 that only signed-in members see avatars (docs/05 "Display name and bio", TKT-01M3YSQ4CR). `GET /avatars/{hash}` enforces this: a signed-out reader or a pre-user gets the same 404 as for a hash that does not exist.

The author block itself still sends the hash. `member.Author` (`internal/service/member/account.go`) builds the block without knowing who will read it, and every author block a signed-out reader receives carries `avatar`. That includes thread pages, lists, profiles and the forum tree. Fetching the hash gets them nothing. It does still tell them whether a member has an avatar, and when it changes.

On 2026-10-03, reviewing Phase 4's open items, the maintainer accepted this for v1.0 as a recorded run decision and asked for this ticket to remove it.

### What to settle in the plan

- **Where the strip happens.** It can be done per response, after any cache read, as the moved-post pointers are (TKT-01M29CYSAM `movedraw.go`), or by passing a viewer class to `member.Author`. The thread page's response cache serves members and signed-out readers. Check whether its key already separates them, or whether stripping after the cache read is the only safe place.
- **Every path that sends an author block to a signed-out reader.** At least threads (view, lists, user posts), forums (tree), groups, forms and the audit log. Most of these already require a member session; list which do not.
- **The goldens.** A served golden for a signed-out GET would change.

## Acceptance criteria

- [ ] No response sent to a signed-out reader or a pre-user carries a non-empty avatar in any author block, and a member's responses are unchanged
- [ ] A test walks every route a signed-out reader may call and fails if any author block in it carries an avatar
- [ ] The response cache never serves a member's author blocks to a signed-out reader, or the reverse, and a test shows it
- [ ] docs/05's avatar paragraph drops the recorded exception
