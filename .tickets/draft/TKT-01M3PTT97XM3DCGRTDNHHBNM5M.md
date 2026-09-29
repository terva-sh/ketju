---
schema: 3
id: TKT-01M3PTT97XM3DCGRTDNHHBNM5M
title: Offer edit and delete only inside the author windows in the client
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CKRHSMCRM9T440314D2Q8
  - TKT-01M3QJYV4ZYJ24B0V9F0WA2W8H
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:59:02Z
updated_at: 2026-09-29T22:07:20Z
created_by:
  id: agent:claude-code/phase2-run-ckrhs
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The client half of TKT-01M29CKRHS (Implement author edit and delete windows with grace edits), split out because that ticket ran in the server lane and the client lane held the committed dist. The server now enforces the windows and answers `edit-window-closed` and `delete-window-closed` (403), and the client already has words for both in `src/api/problem.ts`. What it does not have is a way to know the windows before it offers the actions.

1. **DTO.** `dto.ThreadViewer` gains `author_windows: {edit_seconds, delete_seconds} | null`, null for a viewer who may moderate the forum, since they are bound by neither window. It is per viewer and lives in the viewer block, which is never cached (`internal/service/threads/view.go`). The windows come from `posts.edit_window_seconds` and `posts.delete_window_seconds`. Regenerate the DTO goldens and add the field to `web/client/src/api/dto.ts`, or `dto-contract.ts` fails the build.
2. **Controls.** `Post.tsx` offers Edit and Delete on the viewer's own post only while `created_at` plus the window has not passed, and Restore on their own deleted post under the delete window. A refusal code still shows its message if the client's clock disagrees with the server's.
3. Regenerate `internal/web/spa/dist`.

The windows are open for their first N seconds and closed from N on, so a window of 0 means the author may never act (docs/07 "Edit, delete and restore").

## Acceptance criteria

- [ ] The thread page's viewer block carries author_windows {edit_seconds, delete_seconds} from the two settings for a member, null for a viewer who may moderate the forum or is not a member, outside the cached posts (TestThreadViewerCarriesAuthorWindows, TestAuthorWindowsAreNotCached)
- [ ] The client offers Edit and Delete on the viewer's own visible post and Restore on their own deleted post only inside the windows, Edit only with can_reply, and all three at any age when author_windows is null (postControls.test.ts)
- [ ] The controls disappear when the window passes with no other input (fake timers), and edit, delete and restore send the routes' requests and refetch the page (PostControls.test.tsx)
- [ ] A refusal with edit-window-closed or delete-window-closed shows its sentence beside the control (PostControls.test.tsx)
- [ ] The DTO goldens and dto-contract.ts are updated, the thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476.

**The description assumes controls that do not exist.** On main, `web/client/src/components/thread/Post.tsx` offers quote, reply, link and history, and `Tombstone.tsx` offers nothing. No client code calls `PATCH`, `DELETE` or `POST …/restore` on a post. TKT-01M29CKRHS (Implement author edit and delete windows with grace edits) lists "the client controls" as this ticket's, so this ticket builds Edit, Delete and Restore on the viewer's own posts and gates them by the windows. The effort is M, not S.

An author cannot edit without the post's current source, and `GET …/history`, the only route that carries it, is closed to members when `public_edit_history` is off (docs/08 "Post history"). That route is filed as TKT-01M3QJYV4Z (Serve a post's current source to its author for editing), and this ticket depends on it.

### Settled in grooming

- **The windows** (docs/07 "Edit, delete and restore", docs/08 "Primitives", TKT-01M29CKRHS): measured from the post's creation. An edit is allowed while `posts.edit_window_seconds` has not passed, and a delete or a restore of one's own delete while `posts.delete_window_seconds` has not. A window is open for its first N seconds and closed from the Nth on, so a window of 0 never opens. The defaults are 24 h and 15 min.
- **A viewer who may moderate the forum is bound by neither window** on their own post (docs/07, settled for TKT-01M29CKRHS). `authorize` in `internal/service/threads/mutate.go` checks `a.canModerate`, which is `viewer.can_moderate` on the page.
- **Edits need `reply` in the forum and an unlocked thread** for a member. Deleting and restoring one's own post need neither, and a member may delete or restore in a locked thread (docs/07 "Locking and pinning threads", `authorize`). `viewer.can_reply` is already `canReply && (unlocked || canModerate)` (`internal/service/threads/view.go`), so it is exactly the edit condition apart from the window.
- **A post hidden by a moderator is not its author's to touch**, and only a moderator restores it (decided 2026-09-28, docs/08 "Restoring a moderator's delete", TKT-01M29CRJ5J).
- **The viewer block is never cached.** The response cache holds the posts by viewer class; the viewer block is built per request in `View`. The page's ETag hashes the whole body (`etag` in `view.go`), so a changed window changes the ETag.
- **The refusal words exist**: `edit-window-closed` and `delete-window-closed` in `web/client/src/api/problem.ts`.
- **A button that answers with a refusal is worse than none** (the rule `Post.tsx` follows for quote and reply, TKT-01M29CEE79). The controls are offered only where the server would accept them, and the refusal words cover a clock that disagrees with the server's.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. The thread view stays under `threadViewGzipBytes` (60000) in `web/client/budget.json`.

Engineering choices made here, each with the alternative that lost:
- **`author_windows` is null for a viewer who may moderate the forum and for a viewer who is not a member.** An anonymous viewer or a pre-user has no posts to act on, so windows would never be read, and leaving them out spares the two settings reads on the anonymous page, the most common one. Sending windows to every viewer who does not moderate lost for that cost. The doc comment and docs/07 say both cases.
- **The settings are read in `View`'s existing `s.DB.Read`**, beside the read position and watch level, only when the viewer is a member who does not moderate the forum. They go through the same keys `authorWindows` uses (`settings.EditWindowSeconds`, `settings.DeleteWindowSeconds`). A second transaction lost: one read transaction already runs.
- **Ownership by handle.** A post is the viewer's own when `post.author.handle` equals `viewer.value.user.handle`. `dto.Author` has no id, and both handles are current (a rename changes both). Adding a user id to `Author` lost: it changes every golden to answer one comparison.
- **One offer rule, as a pure function**, `postControls(post, me, viewer, now)` in `components/thread/postControls.ts`, answering `{edit, delete, restore, closesAt}`. `Post.tsx` and `Tombstone.tsx` both use it, and the tests test it directly. TKT-01M29CRJ5J's client step later widens it to every post for a moderator.
- **The page refetches after a change**, through the thread resource's `retry`. The answer's `log_length` is longer, so the new page replaces the cache entry (`storeThreadPage` in `state/threads.ts`). Splicing `CreatedPost.post` into the cached page lost: the cache holds pages at one log length, and a spliced page would be a page at a length that never existed.
- **Inline editing** in the post's place, with the composer's preview. `components/thread/PostEditor.tsx` replaces the post body while open. `usePreview` in `composer/Composer.tsx` is exported for it. Loading the post into the reply composer lost: the composer is one per thread and carries a reply in progress, which an edit would overwrite.
- **One timer per page** re-renders when the soonest open window on the page closes, so a control disappears on time without polling. A per-post interval lost: a long page would run one per post.

### Approach

1. **DTO** (`internal/dto/dto.go`): `ThreadViewer` gains `AuthorWindows *AuthorWindows json:"author_windows"`, with `type AuthorWindows struct { EditSeconds int json:"edit_seconds"; DeleteSeconds int json:"delete_seconds" }`. The doc comment says when it is null. Regenerate the goldens (`thread.member.json` gains the defaults, `thread.moderator.json` and `thread.anon.json` gain null). Add both types to `web/client/src/api/dto.ts`; `dto-contract.ts` fails the type check otherwise.
2. **Server** (`internal/service/threads/view.go`, `View`): inside the existing `s.DB.Read`, when `v.Member() && !canModerate`, read the two settings and set `viewer.AuthorWindows`. Nothing else in the view changes, and the posts are built as before.
3. **Docs**: docs/07 "Thread response shape" gains `author_windows` in the viewer block and when it is null; "Edit, delete and restore" says the client reads the windows there.
4. **Offer rule** (`components/thread/postControls.ts`): for a post `p`, the viewer's handle `me`, the page's `viewer` and `now` in ms:
   - own: `me !== null && p.author.handle === me`;
   - `open(s) = windows === null || (s > 0 && now - p.created_at < s * 1000)`;
   - edit: own, not deleted, `moved_to` null, `viewer.can_reply`, and `open(edit_seconds)`;
   - delete: own, not deleted, `moved_to` null, and `open(delete_seconds)`;
   - restore: own, deleted, `removal` absent or `"author"` (TKT-01M29CKRJ9 adds `removal`), and `open(delete_seconds)`;
   - `closesAt`: the soonest time at which one of these goes from true to false, or null.
5. **Controls**:
   - `Post.tsx` gains an optional `controls` prop. The page passes it; the reply expander and the popover do not. The footer shows "edit" and "delete" buttons after reply, with `data-post-action="edit"` and `"delete"`.
   - Delete asks inline, "Delete this post? You can restore it until {time}.", with Delete and Cancel, then sends `DELETE /api/v1/threads/{id}/posts/{ordinal}` with no body. The time is the post's creation plus the delete window, and the second sentence is left out when `author_windows` is null.
   - `Tombstone.tsx` takes the post and a `restore` control. It shows "restore" (`data-post-action="restore"`), which sends `POST …/restore` with no body.
   - Edit opens `PostEditor`: `GET /api/v1/threads/{id}/posts/{ordinal}/source` (TKT-01M3QJYV4Z), a textarea with the source, the preview from `usePreview`, and Save and Cancel. Save sends `PATCH …` with `{source}`. The editor is not saved as a draft.
   - Every refusal shows beside the control that sent it, in `problemMessage`'s words, which include the two window codes. After a success, `ThreadBody` calls the `onChanged` it receives from `ThreadScreen`, which is `res.retry`.
6. **The clock** (`components/thread/useWindowClock.ts`): given the page's posts, finds the smallest `closesAt` after now and sets one `setTimeout` to re-render then. It is cleared on unmount and reset when the page changes.
7. **CSS** in `components/thread/thread.css` for the editor and the inline confirmation, following the composer's styles.
8. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- Go, `internal/web/api/windows_test.go` (exists for TKT-01M29CKRHS):
  - `TestThreadViewerCarriesAuthorWindows`: a member's page has `author_windows` equal to the two settings. After an admin changes `posts.edit_window_seconds`, the next page carries the new value. A moderator of the forum and an anonymous viewer get null. A member who moderates a different forum gets the windows.
  - `TestAuthorWindowsAreNotCached` (response cache invariant): two members' requests share one cached posts entry (the second is a hit on the posts), and each answer's viewer block is built for that request. A settings change moves the ETag although the log length did not change.
- Goldens: `thread.member.json`, `thread.moderator.json` and `thread.anon.json` are regenerated, and `dto-contract.ts` passes.
- Client, `components/thread/postControls.test.ts`:
  - `offers edit and delete on one's own post inside the windows`, and nothing on another member's post.
  - `edit closes at edit_seconds and delete at delete_seconds`: at `created_at + s*1000 - 1` it is offered and at `created_at + s*1000` it is not. A window of 0 is never open.
  - `null windows bind nothing`: with `author_windows` null, edit, delete and restore are offered on one's own post at any age.
  - `no edit without can_reply`: in a locked thread a member is offered delete and not edit.
  - `restore only one's own delete`: offered on one's own deleted post inside the delete window, and not when `removal` is `"moderator"`.
- Client, `components/thread/PostControls.test.tsx` (fake timers):
  - `the controls disappear when the window passes`: a post 14 minutes old shows delete. After `vi.advanceTimersByTime` past 15 minutes, with no other input, it does not.
  - `edit loads the source and sends the new one`: the editor shows the source route's answer. Save sends `PATCH` with `{source}` and then the page is requested again.
  - `delete asks first` and `restore sends POST …/restore`.
  - `a window refusal shows its words`: a 403 `edit-window-closed` shows "The time for editing this post has passed.", and `delete-window-closed` shows its sentence.
- The thread-view budget (`npm run build`) passes.

### Out of scope

- The source route: TKT-01M3QJYV4Z.
- A moderator's Edit, Delete and Restore on other members' posts, and "(edited by a moderator)": TKT-01M29CRJ5J.
- Tombstone wording and the author's view of a post a moderator hid: TKT-01M29CKRJ9.
- The post history screen.
- An edit reason field. The route takes `reason`, but docs/08 asks the author for none, and an empty reason is valid.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. The description assumes Edit and Delete controls that main does not have: Post.tsx offers quote, reply, link and history, and nothing in the client calls PATCH, DELETE or restore on a post. TKT-01M29CKRHS names this ticket as the owner of the client controls, so the plan builds them and gates them by the windows; effort is now M. Editing needs the post's source, which only history carries and history is closed to members when public_edit_history is off, so the route was filed as TKT-01M3QJYV4Z (Serve a post's current source to its author for editing) and linked as a dependency. author_windows is null for non-members as well as for forum moderators; the plan says why.
