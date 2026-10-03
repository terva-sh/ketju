---
schema: 3
id: TKT-01M3YSQ4CRYBDMEM1MY51D736K
title: Let a member set an avatar
type: task
status: in-progress
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
references:
  - ref: pr:360
    path: null
claim:
  actor: agent:claude-code/phase4-run-ysq4cr
  branch: run/ysq4cr-avatar
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ae4f1c07204ca9588
  commit: ac8e3bf9270c8398536257b3791c2f216191f97c
  session: null
  claimed_at: 2026-10-03T04:20:55Z
  expires_at: null
archive: null
created_at: 2026-10-02T17:13:46Z
updated_at: 2026-10-03T04:51:34Z
created_by:
  id: agent:claude-code/phase4-groom-client-account
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-ysq4cr
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

- [x] The maintainer has answered the open decision, and docs/05 or docs/07 records the avatar rule
- [x] A blob named only as an avatar is served to exactly the viewers the rule names, and attachment serving is unchanged (a test)
- [x] A member sets and clears an avatar on /settings/profile under that rule
- [x] A blob an avatar_blob names does not count against limits.unattached_upload_mb
- [x] An e2e test covers it at 390 px and 1440 px, with one axe scan per test
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

### The decision, as grooming asked it

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

### Decided (maintainer, 2026-10-02; docs/05 "Display name and bio")

- **Who sees it.** Any signed-in member may fetch any member's avatar. Signed-out readers and pre-users see the initials placeholder. This is an exception to docs/07 "Attachments", so it has its own serving route, and a test holds that a signed-out request is refused. Any finding about who can fetch an avatar is treated as security.
- **What it may be.** PNG or JPEG, at most 1 MB. The server re-encodes it to a 256 px square, which strips its metadata, and serves only the re-encoded image.
- **Who sets it.** A member sets or clears their own. A moderator may clear one.

### Approach

Stacked on TKT-01M3V6XVVG's and TKT-01M3V73KM1's code commits (both unmerged at claim), from origin/main 2f2f914d.

1. **Image** (`internal/store/blob/avatar.go`). `blob.Avatar(f, mime)` reads the header first and checks the same bounds as `Thumbnail` (`MaxSide`, `MaxDecodeBytes`). It takes only `image/png` and `image/jpeg`. It decodes with the same panic-safe `decode`, crops the centred square, and scales it to `AvatarEdge` (256) by the thumbnail's area averaging. The scaler is generalised from `fit` to a source rectangle and a target size; the thumbnail's output is unchanged. A PNG is encoded as a PNG and a JPEG as a JPEG at quality 85. `AvatarMaxBytes = 1 << 20`.
2. **Storage** (`blobs.Service.SetAvatar`, `ClearAvatar`).
   - `SetAvatar` reads at most `AvatarMaxBytes`+1 bytes; over that is `payload-too-large`. It sniffs the type: anything but PNG or JPEG is `validation-failed` `{file, type_not_allowed}`. It re-encodes under the thumbnail semaphore, so one decode runs at a time in the process. A header over the bounds is `{file, image_too_large}`.
   - Then one write does all of this. It creates the blob row for the re-encoded bytes, or finds it; a quarantined one is `{file, blocked}`. It sets `users.avatar_blob`. It refuses `{file, upload_budget}` when the bytes are new and the account's unattached bytes after the change would be over the bound. Last, it moves the file into place, as `Upload` does.
   - No `blob_uploads` row is written, so an avatar is never attachable as the member's upload.
   - `ClearAvatar` sets the column to null. Clearing is never refused for the bound.
   - Both answer `ProfileSettings`, which gains `avatar` (the hash, or empty).
3. **Routes.**
   - `PUT /api/v1/settings/avatar` (multipart, one part `file`) and `DELETE /api/v1/settings/avatar`, behind `ownSession`, as the profile routes are. Each is charged to the write limiter. `web.Limits` measures `PUT /settings/avatar` against `AvatarMaxBytes` plus `UploadOverhead`, not the 64 KB body limit.
   - `DELETE /api/v1/admin/users/{handle}/avatar` answers 204, behind `staffGate`, on `mayActOn`'s authority: a moderator acts on members, and an admin on anyone but themselves. It writes `user.avatar_cleared` (audit, docs/04 table). A member's own set or clear is not audited, as profile edits are not.
4. **Serving.** `GET /avatars/{hash}` is a new prefix, served by the blobs web group (`internal/web/blobs`), with the attachment's headers: private, no-cache, `Vary: Cookie`, nosniff, sandbox CSP and CORP same-origin. `blobs.Service.AuthorizeAvatar` answers only when both hold:
   - `v.Member()`;
   - the hash is the `avatar_blob` of an account that is not anonymized, and the blob's status is `ok`.

   Everyone else gets not-found, in the same bytes as for a hash nothing has. So a replaced avatar stops being served at once, whoever kept its hash. `/blobs/{hash}` is unchanged and never serves an avatar that no post names.
5. **The bound.** `UnattachedBytes` leaves out a blob that any `users.avatar_blob` names. Migration 0028 adds `users_avatar_blob ON users (avatar_blob) WHERE avatar_blob IS NOT NULL`, so the exclusion and the serving check seek rather than scan.
6. **Client.**
   - `Avatar.tsx` draws `/avatars/{hash}` only for a signed-in viewer (`viewer.value.user`), and the initials otherwise. An image that fails to load falls back to the initials.
   - `/settings/profile` gains an Avatar section: the current avatar, a file chooser (`accept="image/png,image/jpeg"`), and a Remove button when one is set. A file over 1 MB is refused before it is sent. Each refusal code has its own words. After a change it asks `/me` again, so the top bar follows.
   - `apiUpload` takes a method.
   - The admin user screen offers "Remove avatar" to staff when the account has one.
7. **Docs.**
   - docs/07: the routes, the `/avatars/` serving rule and the codes.
   - docs/02: the prefix.
   - docs/04: the audit action and the index.
   - docs/05: points at the implementation.
   - docs/03: GC already keeps a blob an `avatar_blob` names.

### Tests

- `internal/store/blob`:
  - a PNG and a JPEG come out 256×256, in their own type, with metadata gone (a JPEG's EXIF/APP1 is not in the output);
  - a wide image is centre-cropped;
  - a GIF, a PDF and text are refused;
  - a header over the bounds is `ErrTooLarge`, with no decode.
- `internal/service/blobs`:
  - **TestAvatarServedOnlyToMembers** (security). A blob named only as an avatar is served to an active member, the owner, a moderator and an agent. It is not found for an anonymous viewer, a pre-user, or an inactive (suspended) viewer. A hash that is an attachment only, or a replaced avatar, is not found at `/avatars/`. An avatar is never served by `Authorize` (`/blobs/`) unless a post names it.
  - Set, replace and clear, with the row and file checks.
  - The budget: an avatar does not count, a replaced one does, a new avatar over the bound is refused, and a clear is never refused.
  - `TestUnattachedBytesUsesItsIndex` gains the users index.
- `internal/web/api`:
  - `TestAvatarOverHTTP`: the routes, codes, 1 MB body limit, agent and pre-user refusals, and the response headers.
  - `TestAvatarFetchOverHTTP`: signed out and pre-user get 404, a member gets 200.
  - The moderator clear: authority, audit row, and a member refused.
- dto goldens: `profile-settings`.
- Vitest:
  - `Avatar.test.tsx`: signed-out draws initials, and a member gets `/avatars/`;
  - `ProfileSettingsScreen.test.tsx`: set, remove, refusals, and the size check;
  - the admin user screen's clear.
- e2e: `e2e/settings.spec.ts` profile: carol sets an avatar, the top bar draws it, a signed-out request is 404, and she removes it. At 390 px and 1440 px, one axe scan per test.
- The invariant: `TestBlobServedOnlyToReaders` and `TestRevalidationRechecksAccess` stay green.

### Out of scope

- The display name and the bio, which are TKT-01M3V6XVVG's.
- Cropping in the browser. The server takes the centred square.
- Withholding the `avatar` hash from signed-out answers. `member.Author` is built in eight places without a viewer, and the hash grants nothing: `/avatars/` refuses a signed-out reader. The client draws the initials for them.

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T18:30:52Z

Maintainer decision, 2026-10-02, on the open decision. Asked as choices, the maintainer picked:
- who may see an avatar: 'Signed-in members (Recommended)', over 'anyone who sees the name' and over deferring avatars;
- what an avatar may be: 'PNG/JPEG, 1 MB, square crop (Recommended)'. The server re-encodes it to a 256 px square, which strips its metadata. The member sets or clears their own, and a moderator may clear one.

This is written into docs/05 'Display name and bio'. Signed-out readers and pre-users see the initials placeholder. An avatar is an explicit exception to docs/07 'Attachments' serving, so the plan must give it its own serving route and test that a signed-out request is refused. The open decision is settled, and the ticket can be promoted once its plan says this.

**agent:claude-code/phase4-run-ysq4cr** at 2026-10-03T04:50:41Z

Phase A done on run/ysq4cr-avatar at 635409b (source only; the dist is not committed, so criterion 6 waits for Phase B).

### Stacking

The branch is origin/main 2f2f914d with two commits cherry-picked onto it:
- c177fe35, TKT-01M3V6XVVG (Let a member edit their display name, bio and avatar), as a9b0433d;
- 4e64f390, TKT-01M3V73KM1 (Bound how much one member may upload), as ac8e3bf9.

Both of those are still unmerged. I took the code commits only, not their ticket commits. Once both land, rebase this branch onto main and the cherry-picks drop out. If either one gets review fix-ups first, expect conflicts in profile.go, ProfileSettingsScreen.tsx, blobs.sql or budget_test.go.

Migration numbers: 0027 belongs to V73KM1 and 0028 to this ticket. Check both again at push.

### Decisions made at implementation, with the alternatives that lost

- **Serving.** Avatars get their own prefix, `/avatars/{hash}`, which the blobs web group serves.
  - `/blobs/avatar/{hash}` lost because it conflicts with `GET /blobs/{hash}/thumb` in ServeMux: `/blobs/avatar/thumb` matches both patterns.
  - Widening `Authorize` lost because it would change the attachment rule itself.
  - `AuthorizeAvatar` serves a hash only when it is the current `avatar_blob` of an account that is not anonymized. A replaced avatar therefore stops being served at once. This answers grooming question 4 in the strict direction.
- **Signed-out readers.** The client draws `/avatars/` only when `viewer.user` is set. The server still puts the `avatar` hash in author blocks sent to a signed-out reader.
  - Withholding the hash on the server lost. `member.Author` is built without a viewer in eight places, and the hash grants nothing because `/avatars/` refuses that reader.
  - A Terva finding on this point would be a security finding, and docs/05 states it openly so a reviewer can weigh it.
- **Pre-users.** `web.PreUserGate` answers a pre-user's request for `/avatars/` with 303 to `/apply/status` before the route runs. `AuthorizeAvatar` also refuses them, and the service test covers that.
- **Format.** A PNG stays a PNG, so transparency is kept, and a JPEG stays a JPEG at quality 85. This matches `ThumbType`. A single output format lost: PNG makes photos large, and JPEG drops transparency.
- **Decoding.** Avatars use the thumbnail path's header bounds (`MaxSide`, `MaxDecodeBytes`) and the same one-decode semaphore, so the process-wide bound still holds. The scaler is `fit` generalised to `scale(img, rect, w, h)`, and the thumbnail output is unchanged.
- **Upload bound.** `UnattachedBytes` leaves out any blob a `users.avatar_blob` names, through the new partial index `users_avatar_blob`.
  - A new avatar is refused with `upload_budget` only when its bytes are new and the avatar it replaces takes the account's unattached total up and over the bound. This bounds avatar churn the same way uploads are bounded.
  - Bytes already stored are never refused.
  - Clearing an avatar is never refused.
- **No `blob_uploads` row.** An avatar has no upload row, so the member cannot attach it. `Check` answers `not_yours`, and `TestSetAvatar` covers that.
- **Staff clear.** `DELETE /admin/users/{handle}/avatar` goes through `staffGate` and uses `mayActOn`, the suspension authority: a moderator may act on members, and an admin on anyone but themselves. It writes `user.avatar_cleared`, which is added to `audit.Actions` and the docs/04 table.
  - An account with no avatar is `conflict`, as unsuspending an active account is.
  - An idempotent 204 lost because it would write an audit row for nothing.
- **CSRF.** `needsJSON` exempts `PUT /settings/avatar` (multipart), for PUT only. CrossOriginProtection still refuses a cross-site PUT, which `TestCrossSiteAvatarPutIsRefused` covers. `web.Limits` measures that path against `AvatarMaxBytes` plus `UploadOverhead`.
- **Error code.** `image_too_large` is a new field code, for an image whose header declares more than the decode bounds.

### Tests run

- **Go.** `go vet ./...` is clean. `go test -p 4 ./internal/... ./tools/...` is green. `go test ./cmd/ketju` run alone is green (51 s).
- **Go, under -race.** `internal/service/blobs` is green.
- **Security mutation check.** Removing `v.Member()` from `AuthorizeAvatar` fails both `TestAvatarServedOnlyToMembers` and `TestAvatarFetchOverHTTP`: a signed-out request then got 200 and a 304. The check is restored.
- **Client.** `tsc --noEmit` passes, and vitest is green at 93 files and 2640 tests.
- **e2e.** `e2e/settings.spec.ts` and `e2e/attachments.spec.ts` pass at phone and desktop, 20 of 20 including setup. The new avatar test has one axe scan.

### Bundle

`npm run budget` on the reader:

| Build | Reader (B gzip) |
|---|---|
| main 2f2f914d | 81492 |
| stack base: V6XVVG and V73KM1 client source, built with vite without tsc, because the old dto contract does not type the new golden | 82571 |
| this branch | 83315 |

This ticket alone adds 744 B. The whole stack adds 1823 B over main. The budget is 120000 B. The generated dist was restored and is not committed.

**agent:claude-code/phase4-run-ysq4cr** at 2026-10-03T04:51:34Z

Phase B to-do: main gained TestEveryRouteHasAServedGolden in #390 (f5696cd5). After rebasing, add a servedCases entry in internal/web/api/served_test.go for each of PUT /api/v1/settings/avatar, DELETE /api/v1/settings/avatar and DELETE /api/v1/admin/users/{handle}/avatar. Run go test ./internal/web/api -run TestServedGoldens -update and commit the new files under testdata/golden/served/. Never add a route to pending. TKT-01M3V6XVVG's GET and PATCH /settings/profile need the same in its own Phase B. TKT-01M3V73KM1 adds no route.
