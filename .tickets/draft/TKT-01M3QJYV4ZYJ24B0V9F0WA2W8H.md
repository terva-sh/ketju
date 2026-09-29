---
schema: 3
id: TKT-01M3QJYV4ZYJ24B0V9F0WA2W8H
title: Serve a post's current source to its author for editing
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-09-29T22:00:57Z
updated_at: 2026-09-29T22:07:02Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] GET /api/v1/threads/{id}/posts/{ordinal}/source answers the author and a forum moderator the current revision's source as dto.PostSource, no-store, with public_edit_history on and off (TestPostSourceReaders)
- [ ] Anyone else is forbidden, a post hidden by a moderator is forbidden to its author, and a deleted or moved post is conflict to its author (TestPostSourceRefusals)
- [ ] The route is in docs/07, dto.PostSource has a golden, and web/client/src/api/dto.ts carries the type with dto-contract.ts passing

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

1. **DTO** (`internal/dto`): `PostSource{Ordinal int json:"ordinal"; Revision int json:"revision"; Source string json:"source"}` and a golden `post-source.member.json`. Add the type to `web/client/src/api/dto.ts` and the golden to `dto-contract.ts`.
2. **Service** (`internal/service/threads/posts.go`): `(*Service).Source(ctx, v, id, ordinal) (dto.PostSource, error)`.
   - `v.Member()` or `unauthenticated`. A pre-user never reaches the handler: the route is not in `web.PreUserRoutes` (`internal/web/preuser.go`), whose gate refuses it.
   - `s.access(ctx, v, id)`, then one `s.stable` snapshot of the ordinal (`not-found` for an ordinal past the end).
   - With `a.canModerate`, any post that is not moved may be read.
   - Otherwise the entry's `Author` must be `v.UserID`, or `forbidden`. A post with `EntryHiddenByMod` is `forbidden`. A deleted or moved post is `conflict`.
   - The source and revision are the last element of `th.Reader.History(ordinal, snap.logLength)`.
3. **Route** (`internal/web/api/api.go` `routes()`): `{Method: get, Path: "/api/v1/threads/{id}/posts/{ordinal}/source", Handler: h.postSource}`, answering with `Cache-Control: no-store`. It is not in the pre-user allowlist, so `TestPreUserRoutesAreExactlyTheAllowlist` needs no change. Being a GET, it is not charged to an agent's bucket.
4. **Docs**: docs/07's route table and "Edit, delete and restore" gain the route, who may read it, and its refusals.

### Tests

- `internal/web/api/posts_test.go`:
  - `TestPostSourceReaders`: the author reads their post's current source and revision after two edits, with `public_edit_history` on and off. A moderator of the forum reads a member's post. The answer is `no-store`.
  - `TestPostSourceRefusals`: anonymous is 401, a pre-user 403, another member 403, a thread hidden from the viewer 404, a post a moderator hid 403 to its author, and a deleted or moved post 409 to its author.
- The golden `post-source.member.json`, and `dto-contract.ts` passes.
- `TestEveryRouteIsInTheTable` passes.

### Out of scope

- The client's Edit control: TKT-01M3PTT97X.
- Any change to who may read history.
- A moderator editing another member's post: TKT-01M29CRJ5J.
