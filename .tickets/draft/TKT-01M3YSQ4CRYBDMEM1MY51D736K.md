---
schema: 3
id: TKT-01M3YSQ4CRYBDMEM1MY51D736K
title: Let a member set an avatar
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:api
  - area:client
  - area:security
assignees: []
milestone: phase-4
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3V6XVVGSTDA1SH15DKYP7NZ
  - TKT-01M3V73KM1VJ5XTC53MFCEJ0XW
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T17:13:46Z
updated_at: 2026-10-02T17:21:02Z
created_by:
  id: agent:claude-code/phase4-groom-client-account
  name: ""
updated_by:
  id: agent:claude-code/phase4-groom-client-account
  name: ""
extensions: {}
---

## Description

Split out of TKT-01M3V6XVVG (Let a member edit their display name, bio and avatar) in Phase 4 grooming on 2026-10-02, against main at ed82f15. The display name and bio need no security decision and go ahead there. The avatar does, so it waits here.

`users.avatar_blob` exists (docs/04), every author block carries `avatar` (`member.Author`), and `components/thread/Avatar.tsx` already draws `/blobs/{avatar}` when it is set. Nothing writes the column. Anonymizing clears it, and blob GC keeps a blob any `avatar_blob` names (docs/03 "Blob garbage collection").

docs/07 "Attachments" serves `GET /blobs/{hash}` only to a viewer who may read a post naming the hash in its `attachments`, and answers everyone else `not-found`. An avatar is named by no post. So under today's rule nobody can fetch one, and the client draws a broken image wherever an avatar is set. Uploads need `upload` in a forum (docs/07 "Attachments"), and an avatar belongs to no forum.

### What is needed
A rule for avatars, which docs/05, 07 and 08 do not give. That is docs/14 stop class 1 as docs/15 extends it, so the maintainer decides it. The ticket's plan holds the open decision.

## Acceptance criteria

- [ ] The maintainer has answered the open decision, and docs/05 or docs/07 records the avatar rule
- [ ] A blob named only as an avatar is served to exactly the viewers the rule names, and attachment serving is unchanged (a test)
- [ ] A member sets and clears an avatar on /settings/profile under that rule
- [ ] A blob an avatar_blob names does not count against limits.unattached_upload_mb
- [ ] An e2e test covers it at 390 px and 1440 px, with one axe scan per test
- [ ] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15. Split out of TKT-01M3V6XVVG (Let a member edit their display name, bio and avatar).

### Settled in grooming

- **What exists.**
  - The column `users.avatar_blob` (docs/04).
  - The author block's `avatar` (`member.Author`, withheld for an anonymized account).
  - `components/thread/Avatar.tsx`, which draws `/blobs/{avatar}` when the field is set and the handle's initial otherwise.
  - Blob GC keeps a blob any `avatar_blob` names (docs/03 "Blob garbage collection").
  - Anonymizing clears the column (docs/05 "Account lifecycle").
- **What the screen would be.** An avatar control on `/settings/profile` (TKT-01M3V6XVVG), so this ticket depends on that one.
- **Why it waits.** docs/07 "Attachments" (maintainer approved) serves `GET /blobs/{hash}` only to a viewer who may read a post naming the hash in its `attachments`. Everyone else gets `not-found`. An avatar is named by no post, so under that rule nobody may fetch one. Uploads need `upload` in a forum, and an avatar has no forum. docs/05, 07 and 08 give no rule for either. Widening who may fetch a blob is the maintainer's call: docs/07 "Attachments" says so in its alternatives, citing docs/14 class 1.

### Open decision

**Class 1, security posture beyond the design (docs/14 "The stop rule, changed", docs/15).** What are the rules for an avatar's blob? No default is chosen. The questions, with the options grooming found:

1. **Who may fetch an avatar.** Avatars appear wherever an author block does:
   - thread pages, which anonymous readers see in public forums;
   - profiles and the mention lookup, which need a member session;
   - the top bar, which shows the viewer's own.

   Options:
   - (a) anyone who may see an author block naming the account, which in practice is anyone, an anonymous reader included;
   - (b) members only, with anonymous readers drawing the initial;
   - (c) a separate avatar store with its own URL (`/avatars/{user}`), served to anyone, decoupled from `/blobs/` so the attachment rule stays as it is.
2. **Who may set one.** Options:
   - (a) every member;
   - (b) only accounts an admin grants `upload` somewhere;
   - (c) a new global permission.
3. **What an avatar may be.**
   - Images only, from the attachment allowlist: PNG, JPEG, GIF.
   - Whether the server re-encodes it to a fixed size. The thumbnail path (`blobs/thumb.go`) can produce one, and re-encoding drops metadata such as EXIF location.
   - A smaller size cap than `limits.attachment_mb`.
4. **Whether a member's old avatar stays fetchable** after they replace it, until GC removes it, by anyone who saved its hash.

### Approach

To be written once the decision is made. What any answer needs:
1. A route that sets and clears `avatar_blob`, gated as the decision says, writing in one transaction with the blob's checks.
2. A serve rule for avatar blobs in `blobs.Service.Authorize`, or a new route, as the decision says. A test holds that a blob named only as an avatar is fetchable by exactly the viewers the decision names, and that the attachment rule is unchanged for everything else.
3. The upload bound (TKT-01M3V73KM1, Bound how much one member may upload) must not count a blob an `avatar_blob` names.
4. An avatar control on `/settings/profile`.

### Tests

To be named with the approach. The invariants are fixed already. Uploads are content-addressed, and a blob that is only an attachment is served only to readers of a post that references it (docs/14). This ticket must leave that true for attachments, whatever it decides for avatars.

### Out of scope

- The display name and the bio, which are TKT-01M3V6XVVG's.
