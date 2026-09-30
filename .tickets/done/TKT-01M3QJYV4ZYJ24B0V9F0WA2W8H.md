---
schema: 3
id: TKT-01M3QJYV4ZYJ24B0V9F0WA2W8H
title: Serve a post's current source to its author for editing
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:197
    path: null
  - ref: pr:199
    path: null
  - ref: pr:201
    path: null
  - ref: pr:204
    path: null
  - ref: pr:205
    path: null
claim: null
archive: null
created_at: 2026-09-29T22:00:57Z
updated_at: 2026-09-30T04:23:19Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qjyv4z
  name: ""
extensions: {}
---

## Description

An author cannot edit a post in the client without its current source, and no route gives it to them in every configuration. `GET /threads/{id}/posts/{ordinal}/history` carries each revision's source, but docs/08 "Post history" makes history visible to members only while `public_edit_history` is on. With it off, an author can still `PATCH` their post inside the edit window and has no way to read what they are editing.

Found while planning TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client) in Phase 2 grooming (TKT-01M3M9RR26). That ticket builds the client's Edit control and depends on this one.

### What to build

- `GET /api/v1/threads/{id}/posts/{ordinal}/source` answers `dto.PostSource{ordinal, revision, source}`, the current revision's source, `no-store`.
- Who may read it is who may edit it now or later: the post's author, and a viewer who may moderate the thread's forum. Anyone else is `forbidden`. Access to the thread is the thread page's (`not-found` for a thread hidden from the viewer, `unauthenticated` for anonymous, `forbidden` for a pre-user).
- A post hidden by a moderator is `forbidden` to its author, as `authorize` refuses the author's edit of it. A deleted or moved post is `conflict` to its author: there is nothing to edit.
- The windows are not checked here. Reading one's own words after the window closes gives nothing away, and the edit itself is refused by `PATCH`.
- Add the route to docs/07's table and the "Edit, delete and restore" section, the DTO golden, and the type to `web/client/src/api/dto.ts` and `dto-contract.ts`. The client types are type-only, so the dist does not change.

## Acceptance criteria

- [x] GET /api/v1/threads/{id}/posts/{ordinal}/source answers the author and a forum moderator the current revision's source as dto.PostSource, no-store, with public_edit_history on and off (TestPostSourceReaders)
- [x] Anyone else is forbidden, a post hidden by a moderator is forbidden to its author, and a deleted or moved post is conflict to its author (TestPostSourceRefusals)
- [x] The route is in docs/07, dto.PostSource has a golden, and web/client/src/api/dto.ts carries the type with dto-contract.ts passing

## Implementation plan

Filed and planned 2026-09-30 in Phase 2 grooming (TKT-01M3M9RR26), against main at f04a476, for TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client), which depends on it.

### Settled in grooming

- **History visibility is not changed.** docs/08 "Post history" shows history to moderators always and to members while `public_edit_history` is on. `threads.Service.History` (`internal/service/threads/posts.go`) enforces it, and this ticket leaves it as it is.
- **Who may edit** is `authorize` in `internal/service/threads/mutate.go`: the post's author, never on a post a moderator hid, and a viewer who may moderate the forum is bound by neither author window. TKT-01M29CRJ5J widens it to moderators acting on any post.
- **Access to the thread** is the thread page's, through `s.access`: `not-found` for a thread hidden from the viewer. A member session is required, as for every write it serves (anonymous `unauthenticated`, pre-user `forbidden`).

Engineering choices made here, each with the alternative that lost:
- **A route of its own**, `GET /api/v1/threads/{id}/posts/{ordinal}/source`. Letting the author read their own post's history when `public_edit_history` is off lost: it changes a documented visibility rule for a need that only the current revision meets, and it would serve every earlier revision too.
- **Readers are the author and a forum moderator**, whether or not the windows are open. Reading one's own words gives nothing away, and a moderator can already read every revision through history. Tying the read to the windows lost: the client would have a second rule to mirror, and a closed window already refuses the `PATCH`.
- **`no-store`, and no ETag.** The source is read for one edit and should never be served stale from a cache. `CreatedPost`, the edit's answer, carries the new revision.
- **The source comes from the post's history as the reader already decodes it**: the last entry of `th.Reader.History(ordinal, logLength)` is the current revision. A new reader method that decodes only the last frame lost for now: an edit is rare, and history is bounded by a post's revisions. If profiling says otherwise, it can be added behind the same service method.

### Approach

Revised at claim, 2026-09-30, against main at e425323. The grooming approach holds; what changed is written into it.

1. **DTO** (`internal/dto`): `PostSource{Ordinal int json:"ordinal"; Revision int json:"revision"; Source string json:"source"}` and a golden `post-source.member.json`. Add the type to `web/client/src/api/dto.ts` and the golden to `dto-contract.ts`. Type-only, so `internal/web/spa/dist` stays byte-identical.
2. **Service** (`internal/service/threads/posts.go`): `(*Service).Source(ctx, v, id, ordinal) (dto.PostSource, error)`.
   - `s.access(ctx, v, id)` first, as History does, so a thread hidden from the viewer is `not-found` to everyone; then `a.viewer.Member()` or `unauthenticated` (access serves an inactive viewer as anonymous). A pre-user never reaches the handler: the route is not in `web.PreUserRoutes`.
   - One `s.stable` snapshot of the ordinal (`not-found` for an ordinal past the end). The decision is a pure function, `sourceRefusal(a, e)`, so it can be table-tested over every flag combination, a moved post included, which no record can produce yet:
     - a moderator of the forum: served when the post is visible or `revealRemoved` serves it to them, so a moved post is `conflict`. So will a redacted one be once TKT-01M29CXJZ9 lands, since its `revealRemoved` refuses a redacted post to everyone and its `Visible` counts the redacted bit;
     - anyone else who is not the author: `forbidden`, checked before any removal so the answer says nothing about the post's state;
     - the author: `forbidden` for a post carrying `EntryHiddenByMod` (a moderator's hide and a hide pending review alike), then `conflict` for any other post that is not visible (deleted or moved).
     - So a removed post's source reaches only a viewer `revealRemoved` would give its content to, and not all of them: the author of a hidden post is refused although `revealRemoved` shows them its body, because the ticket settles that as `authorize` refuses their edit.
   - The source and revision are the last element of `th.Reader.History(ordinal, snap.logLength)`. Its revision must equal the entry's, or the read is an internal error rather than a stale source.
   - No response cache: the service never touches `s.Responses`, and the answer carries no ETag.
3. **Route** (`internal/web/api/api.go` `routes()`): `{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}/source", Handler: h.postSource}`, answering with `Cache-Control: no-store`. It is not in the pre-user allowlist, so `TestPreUserRoutesAreExactlyTheAllowlist` needs no change. Being a GET, it is not charged to an agent's bucket.
4. **Archived forums and threads** are not checked here, as the windows are not: the ticket's readers are "who may edit it now or later", a thread stays readable in an archived forum (docs/04), and `PATCH` refuses the edit itself with `conflict`. Reading one's own words gives nothing away.
5. **Docs**: docs/07's route table, "The post endpoints" and "Edit, delete and restore" gain the route, who may read it, its refusals and `no-store`.

### Tests

- `internal/service/threads/source_test.go`: `TestSourceRefusal`, the rule over every combination of the deleted, moved, hidden-by-mod and auto-hidden bits for the author, another member and a moderator, with the property that nothing is served for a removed entry unless `revealRemoved` serves it to that viewer.
- `internal/web/api/source_test.go`:
  - `TestPostSourceReaders`: the author reads their post's current source and revision after two edits, one by a moderator, with `public_edit_history` on and off. A moderator of the forum reads a member's post and a removed one. The answer is `no-store` with no ETag, and the response cache's counters do not move. An archived forum still serves the author.
  - `TestPostSourceRefusals`: anonymous is 401, another member 403, a thread hidden from the viewer 404, an unknown ordinal 404, a post a moderator hid 403 to its author and to another member, a post hidden pending review 403 to its author, and a deleted post 409 to its author.
- The golden `post-source.member.json`, and `dto-contract.ts` passes.
- `TestEveryRouteIsInTheTable` passes; `TestAPreUserNeverReachesTheResponseCache` also asks for the source, so a pre-user is 403 through the real session middleware.

### Out of scope

- The client's Edit control: TKT-01M3PTT97X.
- Any change to who may read history.
- A moderator editing another member's post: TKT-01M29CRJ5J.

## Notes

**agent:claude-code/phase2-run-qjyv4z** at 2026-09-30T03:14:27Z

Decisions taken while building, 2026-09-30, against main at e425323. Each follows the ticket's settled items. None changes who may see what beyond them.

### How the rule relates to revealRemoved (TKT-01M29CKRJ9)

A removed post's source reaches only a viewer that `revealRemoved` gives the post's content to, and of those only a moderator. The author of a post a moderator hid, or of one hidden pending review, is refused (`forbidden`), although `revealRemoved` shows them its body on the page. The ticket settles this refusal, on the grounds that `authorize` refuses their edit. `TestSourceNeverOutrunsRevealRemoved` holds the rule over every combination of the deleted, moved, hidden-by-mod and auto-hidden bits, for five kinds of reader.

Another alternative lost: serving whatever `revealRemoved` serves. That would give the author of a hidden post a source they cannot use, against the ticket's settled refusal.

### Redaction (TKT-01M29CXJZ9, PR #192, not on main yet)

Nothing here needs to change when redaction lands. PR #192 makes `revealRemoved` false for a redacted post, for everyone including moderators, and counts the redacted bit in `Visible()`. The source route asks only those two for a removed post. A redacted post is therefore `conflict` to a moderator and to its author, `forbidden` to anyone else, and never served. The log's revisions are also blank after a redaction.

### Archived forums and threads

These are not checked, the same as the windows and a lock. The ticket's readers are "who may edit it now or later". A thread in an archived forum stays readable (docs/04). `PATCH` answers the archive's `conflict` itself. `TestPostSourceReaders` covers an author reading in an archived forum. The alternative lost: refusing with `conflict` here. That would add a second rule for the client to mirror, and it would hide words the author can already read on the page.

### Order of checks

`s.access` runs first, as History does, then the member check. A thread hidden from the viewer is therefore `not-found` to everyone, including an anonymous viewer and the author, rather than `unauthenticated` first. The grooming approach listed the member check first. The ticket's access rule is "the thread page's", and the page answers this way.

### Response cache

The service never calls `s.Responses`, and the handler answers `no-store` with no ETag, which the ticket prefers. `TestPostSourceReaders` asserts that the cache's counters do not move. `TestAPreUserNeverReachesTheResponseCache` now also asks for the source.

**agent:claude-code/phase2-run-qjyv4z** at 2026-09-30T03:35:28Z

PR #197 (head c5bda82, base e425323) passed CI run 1262 and reviewed clean (run f3f867e1, Actions run 1263, no findings). Main then moved to 052db1d when #193 landed, so the final tree was carried onto it unchanged as one commit in PR #199, and #197 was closed unmerged.

**agent:claude-code/phase2-run-qjyv4z** at 2026-09-30T03:48:03Z

Redaction (TKT-01M29CXJZ9, PR #192) landed on main at 5c2ba25 while PR #199 was open, so the work was carried onto that main as PR #199's replacement. Redaction also needed tests here: the prompt requires that a redacted entry never serves its source. The rule needed no change, as the note of 2026-09-30 predicted. `revealRemoved` refuses a redacted post to everyone, and `Visible` counts the bit. A redacted post is `conflict` to its author and to a moderator, and `forbidden` to anyone else. `TestSourceRefusal` has redacted cases, `TestSourceNeverOutrunsRevealRemoved` includes the redacted bit and asserts that a redacted entry is never served, and `TestRedactedPostServesNoSource` exercises a real offline redaction. docs/07 says the same. Mutation checked: replacing the moderator guard with the grooming's literal 'any post that is not moved' fails all three tests. PR #199 (head a706b7e, base 052db1d) had a clean review, run on issuecomment 19751. That review does not cover the new tests, so a fresh review is requested on the new head.

**agent:claude-code/phase2-run-qjyv4z** at 2026-09-30T04:05:35Z

Main moved under PR #201 (head 2305c26, base 5c2ba25) when the Phase 2 exit (#194, TKT-01M3NTSCMN) landed. That change touched cmd/ketju e2e tests, web/client/e2e and ci.yml, and none of this ticket's files. Per brief item 10, the tree was carried unchanged onto main at 790b6dc as one commit in the PR that replaces #201. #201's review on 499b000 was clean (issuecomment 19789). Its CI run 1288 stopped at gofmt, which 2305c26 fixed.

**agent:claude-code/phase2-run-qjyv4z** at 2026-09-30T04:23:02Z

Main moved under PR #204 (head cedfdbe, base 790b6dc) when ketju fsck drift reporting (#200, TKT-01M3A054TR) landed. That change touched cmd/ketju, internal/events/projection and internal/store/db, and none of this ticket's files. #204 had passed CI run 1303 and reviewed clean (issuecomment 19829, no findings). Per brief item 10, the tree was carried unchanged onto main at 23f4cc8 as one commit in the PR that replaces #204.

## Summary

`GET /api/v1/threads/{id}/posts/{ordinal}/source` answers `dto.PostSource{ordinal, revision, source}` to a post's author and to a moderator of its forum, with `public_edit_history` on or off. The answer is `no-store` with no ETag and never touches the response cache.

- The service is `threads.Service.Source`. The rule is `sourceRefusal` in `internal/service/threads/posts.go`. A removed post's source reaches only a moderator whom `revealRemoved` serves, so redaction (TKT-01M29CXJZ9) narrows the rule with no change here.
- Tests:
  - `TestSourceRefusal` and `TestSourceNeverOutrunsRevealRemoved` (service) cover the rule. They include the moved post for criterion 2, because no record can move a post yet and the API test cannot reach one.
  - `TestPostSourceReaders` and `TestPostSourceRefusals` (API) cover the route.
  - `TestAPreUserNeverReachesTheResponseCache` now also asks for the source.
- docs/07 has the route in its table, and a paragraph under "Edit, delete and restore".
- The client has the `PostSource` type and its golden in `dto-contract.ts`. The dist is byte-identical.
- The dated note of 2026-09-30 records four decisions: archived forums are not checked, the access check runs before the member check, how the rule relates to `revealRemoved`, and how it relates to redaction.
- Next: TKT-01M3PTT97X (Offer edit and delete only inside the author windows in the client) can build on the route.
