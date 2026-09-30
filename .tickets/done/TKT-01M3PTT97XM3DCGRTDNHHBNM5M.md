---
schema: 3
id: TKT-01M3PTT97XM3DCGRTDNHHBNM5M
title: Offer edit and delete only inside the author windows in the client
type: task
status: done
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
  - TKT-01M3R33SR5DFV94SHH7P5M92QZ
blocks_on: none
references:
  - ref: pr:243
    path: null
claim: null
archive: null
created_at: 2026-09-29T14:59:02Z
updated_at: 2026-09-30T19:18:46Z
created_by:
  id: agent:claude-code/phase2-run-ckrhs
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ptt97x
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

- [x] The thread page's viewer block carries author_windows {edit_seconds, delete_seconds} from the two settings for a member, null for a viewer who may moderate the forum or is not a member, outside the cached posts (TestThreadViewerCarriesAuthorWindows, TestAuthorWindowsAreNotCached)
- [x] The client offers Edit and Delete on the viewer's own visible post and Restore on their own deleted post only inside the windows, Edit only with can_reply, and all three at any age when author_windows is null (postControls.test.ts)
- [x] The controls disappear when the window passes with no other input (fake timers), and edit, delete and restore send the routes' requests and refetch the page (PostControls.test.tsx)
- [x] A refusal with edit-window-closed or delete-window-closed shows its sentence beside the control (PostControls.test.tsx)
- [x] The DTO goldens and dto-contract.ts are updated, the thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
- [x] author_windows carries the seconds the server enforces, int(secondsDuration(n)/time.Second): with both settings at 10000000000 the page carries 9223372036 and a delete 30 days after posting is allowed (TestAuthorWindowsFollowTheClamp)
- [x] useWindowClock sets its timer through delayUntil, never longer than 2147483647 ms, so a 40-day window neither fires at once nor loops (deadline.test.ts, PostControls.test.tsx 'a long window sets no immediate timer')
- [x] Ownership compares handles without case through one isOwn helper that mayFlag also uses, and Restore is drawn on a tombstone for an author and in a removed post's footer for a moderator-author (postControls.test.ts 'ownership ignores case', PostControls.test.tsx)
- [x] On a phone, edit and delete are under PostMore and not in the footer, and closing the delete confirmation returns focus to the More button (PostControls.test.tsx 'on a phone edit and delete are under More')

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476. Refreshed on 2026-09-30 against main at a660ae5 (5124209 plus the run's tracking commits).

**The description assumes controls that do not exist.** On main, `web/client/src/components/thread/Post.tsx` offers quote, reply, link, flag and history. `Tombstone.tsx` offers only the reply expander. No client code calls `PATCH`, `DELETE` or `POST …/restore` on a post. TKT-01M29CKRHS (Implement author edit and delete windows with grace edits, PR #146) lists "the client controls" as this ticket's, so this ticket builds Edit, Delete and Restore on the viewer's own posts and gates them by the windows. The effort is M, not S.

An author cannot edit without the post's current source, and `GET …/history`, the only route that carried it, is closed to members when `public_edit_history` is off (docs/08 "Post history"). TKT-01M3QJYV4Z (Serve a post's current source to its author for editing) has since landed in PR #205: `GET /api/v1/threads/{id}/posts/{ordinal}/source` answers `dto.PostSource` `{ordinal, revision, source}` to the post's author and to a moderator of its forum, `no-store` with no ETag (`threads.Service.Source`, `sourceRefusal` in `internal/service/threads/posts.go`). The client type `PostSource` is in `web/client/src/api/dto.ts`.

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
- **One set of controls for authors and moderators.** One offer rule, the pure function `postControls(post, me, viewer, now)` in `components/thread/postControls.ts`, answers `{edit, delete, restore, closesAt}`. `Post.tsx` and `Tombstone.tsx` both use it, and the tests test it directly. The controls, `PostEditor`, the inline delete confirmation and the page refetch are built here once. TKT-01M3QHKD21 (Show moderators' post controls and the edited-by-a-moderator marker) depends on this ticket. It only widens the rule to every post when `viewer.can_moderate`, and adds the marker. A second set of controls for moderators lost: two components for the same three requests would drift apart.
- **The page refetches after a change**, through the thread resource's `retry`. The answer's `log_length` is longer, so the new page replaces the cache entry (`storeThreadPage` in `state/threads.ts`). Splicing `CreatedPost.post` into the cached page lost: the cache holds pages at one log length, and a spliced page would be a page at a length that never existed.
- **Inline editing** in the post's place, with the composer's preview. `components/thread/PostEditor.tsx` replaces the post body while open. `usePreview` in `composer/Composer.tsx` is exported for it. Loading the post into the reply composer lost: the composer is one per thread and carries a reply in progress, which an edit would overwrite.
- **One timer per page** re-renders when the soonest open window on the page closes, so a control disappears on time without polling. A per-post interval lost: a long page would run one per post.


Added in the refresh of 2026-09-30:
- **The source route is built** (TKT-01M3QJYV4Z, PR #205). It does not check the windows, a lock or an archive, because those refuse the edit and not the reading. So the editor may open on a post whose edit the server will then refuse, and the refusal's words show. `apiGet` holds no body for an answer without an ETag, so the source is never kept on the client. `PATCH` takes no revision, so the editor sends only `{source}`, and `PostSource.revision` is unused.
- **`removal` is on `dto.Post`, and removed posts are drawn by who may see them** (TKT-01M29CKRJ9, PRs #170 and #174). `revealRemoved` in `view.go` serves a moderator every removed post's body except a moved or redacted one. It serves an author their own post while a moderator's removal or a review hide holds it, and not a post they deleted themselves. `ThreadBody` draws a removed post served with its body as `Post`, with the class `removed` and `removedMark`, and any other as `Tombstone` (`isTombstone` in `components/thread/removal.tsx`). So an author's own deleted post reaches them as a `Tombstone`, and Restore goes in the tombstone's footer. A moderator's own deleted post reaches them as a removed `Post`, and Restore goes in that post's footer. Restore is drawn in whichever of the two the post is drawn as (engineering choice). The rule decides whether to offer it. This settles, for both tickets, the placement that TKT-01M3QHKD21's plan left to "whichever lands second".
- **Ownership compares handles without case** (refines "Ownership by handle" above). `mayFlag` in `Post.tsx` (PR #202) compares `me.handle.toLowerCase()` with the author's, "as the server compares them". The rule does the same through one exported helper, `isOwn(post)`, and `mayFlag` calls it. Two comparisons that could drift lost.
- **The windows follow TKT-01M3R33SR5's clamp** (engineering choice, depends on TKT-01M3R33SR5 (Bound the post edit and delete windows before making Durations)). The server enforces a window as `secondsDuration(n)`, so the longest window is `math.MaxInt64` ns, about 292 years. `author_windows` carries `int(secondsDuration(n) / time.Second)`, the seconds the server enforces, never the raw setting. So a setting of 10000000000 reaches the client as 9223372036. The client and the server then agree to the second, and `created_at + s * 1000` stays an exact number, under 2^53. Sending the raw setting lost: past 2^53 a JSON number loses precision, and the client would hold a window the server does not.
- **Long timers** (engineering choice). A browser fires a `setTimeout` longer than 2^31 − 1 ms (about 24.8 days) at once. With a window of 30 days, one timer set to the soonest `closesAt` would fire at once, re-render, and set itself again, without end. `useWindowClock` sets its timer through `delayUntil(at, now)` from `web/client/src/components/thread/deadline.ts`, bounded at `maxTimeout = 2_147_483_647`. When it fires early it finds the next `closesAt` and sets itself again. Whichever of this ticket and TKT-01M3QXV399 (Add rename and move to the thread screen) lands first adds `deadline.ts`, and the second imports it.
- **On a phone, edit and delete go under `PostMore`** (engineering choice, following PR #202). docs/06 "Mobile" collapses the footer at 390 px, and `PostMore` already holds link, flag and history. It gains edit and delete when the post hands them over, after flag. Choosing one closes the list, and the delete confirmation or the editor then opens in the post. When either closes, focus returns to the post's More button, as `FlagDialog` returns it (Terva review 1525 on PR #181). Restore on a tombstone stays inline, since a tombstone has no other controls.
- **An archived forum is not on the page** (a known limit, engineering choice). In an archived forum `authorize` refuses a non-admin's edit, delete and restore of their own post with `conflict` (`readOnlyForum`), a moderator's own post included. The page knows the edit case, because `can_reply` is false there. It does not know the delete and restore cases, since `dto.ForumRef` carries no archived flag. The client therefore offers them, and the `conflict` answer's words show beside the control. Adding `archived` to `ThreadHeader.forum` lost: it changes every thread golden for a state that archives rarely enter.
- **The reader bundle is 47415 B of 60000 B gzip** on main after PR #202. The editor with its preview is the largest client addition planned for Phase 2, so the PR records the new figure.

### Approach

1. **DTO** (`internal/dto/dto.go`): `ThreadViewer` gains `AuthorWindows *AuthorWindows json:"author_windows"`, with `type AuthorWindows struct { EditSeconds int json:"edit_seconds"; DeleteSeconds int json:"delete_seconds" }`. The doc comment says when it is null, and that the seconds are the ones the server enforces. Regenerate the goldens with `go test ./internal/dto -update` and `go test ./internal/web/api -update`. In `internal/web/api/testdata/golden`, `thread.member.json` and `thread.author.json` gain the defaults, and `thread.moderator.json` and `thread.anon.json` gain null. In `internal/dto/fixtures_test.go` the `tMember` viewer gains the defaults, and `tAnon` and `tModerator` gain null, which regenerates `thread.member.json`, `thread-locked.member.json`, `thread.moderator.json` and `thread.anon.json` in `internal/dto/testdata/golden`. Add both types to `web/client/src/api/dto.ts`; `dto-contract.ts` fails the type check otherwise.
2. **Server** (`internal/service/threads/view.go`, `View`): inside the existing `s.DB.Read`, when `v.Member() && !canModerate`, read `settings.EditWindowSeconds` and `settings.DeleteWindowSeconds`, and set `viewer.AuthorWindows` to `int(secondsDuration(n) / time.Second)` of each. Nothing else in the view changes, and the posts are built as before.
3. **Docs**: docs/07 "Thread response shape" gains `author_windows` in the viewer block, when it is null, and that a window past what a `time.Duration` holds is sent as the longest one. "Edit, delete and restore" says the client reads the windows there.
4. **Offer rule** (`components/thread/postControls.ts`): for a post `p`, the page's `viewer` and `now` in ms from `Date.now()`:
   - `created = p.created_at`. It is `Millis`, a JSON number of milliseconds since the epoch (`web/client/src/api/dto.ts`, `dto.Millis` in Go, for example `1757500300000` in `created-post.member.json`), not a date string. The rule still guards it: when a window applies, a `created` that is not a finite number offers nothing rather than comparing `NaN`. Terva review 1488 (PR #167, head f5a4206), finding 2, read it as a string. The shape says otherwise, and the guard and its test make the type part of the rule. `created` is used for both `open` and `closesAt`;
   - own: `isOwn(p)`, the viewer's handle equal to the author's without case;
   - `open(s) = windows === null || (s > 0 && now - created < s * 1000)`;
   - edit: own, not deleted, `moved_to` null, `viewer.can_reply`, and `open(edit_seconds)`;
   - delete: own, not deleted, `moved_to` null, and `open(delete_seconds)`;
   - restore: own, deleted, `moved_to` null, `removal` null or `"author"`, and `open(delete_seconds)`;
   - `closesAt`: the soonest time at which one of these goes from true to false, or null.
5. **Controls**:
   - `Post.tsx` gains an optional `controls` prop, beside `onFlag`. `ThreadBody` passes it; the reply expander and the popover do not. On desktop the footer shows "edit" and "delete" buttons after flag, with `data-post-action="edit"` and `"delete"`. On a phone they go under `PostMore`, whose props gain `controls`. On a removed `Post` (a moderator-author's own deleted post) the footer shows "restore" when the rule offers it.
   - Delete asks inline, "Delete this post? You can restore it until {time}.", with Delete and Cancel. It then sends `apiSend<CreatedPost>('DELETE', `/api/v1/threads/${id}/posts/${ordinal}`)` with no body. The time is the post's creation plus the delete window, and the second sentence is left out when `author_windows` is null.
   - `Tombstone.tsx` takes a `restore` control. Its footer shows "restore" (`data-post-action="restore"`), which sends `POST …/restore` with no body. The footer is drawn when the expander or restore is shown.
   - Edit opens `PostEditor`. It loads `apiGet<PostSource>(`/api/v1/threads/${id}/posts/${ordinal}/source`)`, and shows a textarea with the source, the preview from `usePreview` (exported from `composer/Composer.tsx` for it), and Save and Cancel. Save sends `PATCH …` with `{source}`. The editor is not saved as a draft. A source that fails to load shows `problemMessage` with a Retry.
   - Every refusal shows beside the control that sent it, in `problemMessage`'s words, which include the two window codes. After a success, `ThreadBody` calls the `onChanged` it receives from `ThreadScreen`, which is `res.retry`.
6. **The clock** (`components/thread/useWindowClock.ts`): given the page's posts, it finds the smallest `closesAt` after now and sets one `setTimeout` with `delayUntil(closesAt, Date.now())` to re-render then. It is cleared on unmount and reset when the page changes.
7. **CSS** in `components/thread/thread.css` for the editor and the inline confirmation, following the composer's styles.
8. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Record the change from 47415 B in the PR.

### Tests

- Go, `internal/web/api/windows_test.go` (exists for TKT-01M29CKRHS, with `TestAuthorWindows`):
  - `TestThreadViewerCarriesAuthorWindows`: a member's page has `author_windows` equal to the two settings. After an admin changes `posts.edit_window_seconds`, the next page carries the new value. A moderator of the forum, by role or through a group, gets null, and so does an anonymous viewer. A member who moderates a different forum through a group gets the windows.
  - `TestAuthorWindowsFollowTheClamp`: with both settings at 10000000000, the page carries 9223372036 for each, and the post's delete is still allowed 30 days after posting.
  - `TestAuthorWindowsAreNotCached` (response cache invariant): two members' requests share one cached posts entry (the second is a hit on the posts), and each answer's viewer block is built for that request. A settings change moves the ETag although the log length did not change.
- Goldens: the thread goldens in `internal/web/api` and `internal/dto` named in Approach step 1 are regenerated, and `dto-contract.ts` passes.
- Client, `components/thread/postControls.test.ts`:
  - `offers edit and delete on one's own post inside the windows`, and nothing on another member's post.
  - `ownership ignores case`: a post by `Alice` is the own post of a viewer whose handle is `alice`.
  - `edit closes at edit_seconds and delete at delete_seconds`: at `created_at + s*1000 - 1` it is offered and at `created_at + s*1000` it is not. A window of 0 is never open.
  - `null windows bind nothing`: with `author_windows` null, edit, delete and restore are offered on one's own post at any age.
  - `no edit without can_reply`: in a locked thread a member is offered delete and not edit.
  - `restore only one's own delete`: offered on one's own deleted post inside the delete window, and not when `removal` is `"moderator"` or `"review"`.
  - `created_at is Millis` (review 1488, finding 2): a post whose `created_at` is the number `now - 1000` is inside a 15-minute window. One whose `created_at` is not a finite number is offered nothing a window bounds. `closesAt` is `created_at + s * 1000` as a number.
- Client, `components/thread/deadline.test.ts` (whichever ticket adds it): `delayUntil` returns `at - now` below the bound, the bound for a deadline 30 days ahead, and 0 for one already passed.
- Client, `components/thread/PostControls.test.tsx` (fake timers):
  - `the controls disappear when the window passes`: a post 14 minutes old shows delete. After `vi.advanceTimersByTime` past 15 minutes, with no other input, it does not.
  - `a long window sets no immediate timer`: with `edit_seconds` of 40 days, advancing 2147483647 ms leaves edit shown and runs the timer once, not in a loop.
  - `edit loads the source and sends the new one`: the editor shows the source route's answer. Save sends `PATCH` with `{source}`, and then the page is requested again.
  - `delete asks first` and `restore sends POST …/restore`, the latter from a tombstone for an author and from a removed post for a moderator-author.
  - `on a phone edit and delete are under More`: with the phone layout, the footer has no edit or delete button, and the More list does. Closing the delete confirmation returns focus to the More button.
  - `a window refusal shows its words`: a 403 `edit-window-closed` shows "The time for editing this post has passed.", and `delete-window-closed` shows its sentence.
- The thread-view budget (`npm run build`) passes.

### Out of scope

- The source route: TKT-01M3QJYV4Z, done in PR #205.
- The `secondsDuration` helper and the server's clamps: TKT-01M3R33SR5.
- A moderator's Edit, Delete and Restore on other members' posts, and "(edited by a moderator)": TKT-01M3QHKD21, which builds on these controls. The server side is TKT-01M29CRJ5J, merged in PR #165.
- Tombstone wording and the author's view of a post a moderator hid: TKT-01M29CKRJ9, done.
- The post history screen.
- An edit reason field. The route takes `reason`, but docs/08 asks the author for none, and an empty reason is valid.
- Saying on the page that a forum is archived, as above.

### Revised at claim, 2026-09-30, against main at 13709f0

Everything above stands. What the code on main changed, and what was built differently, with the reason for each:

- **`deadline.ts` exists** (TKT-01M3QXV399, PR #231). `useWindowClock` imports `delayUntil` from it, and `deadline.test.ts` is already there, so this ticket adds no copy.
- **Redacted posts are offered nothing** (PRs #228 and #237). A hard-redacted post is deleted, has no body, and can never be edited or restored, so the rule returns no controls for `redacted` before anything else, as it does for `moved_to`.
- **Restore needs `removal === "author"`**, not "null or author". This server always sets `removal` on a deleted post, and the client ships in the same binary. A deleted post with a null removal is one whose remover the page does not know, and offering Restore there could offer what the server refuses. The stricter test loses nothing.
- **The editor is the shared `ComposerBody`** (PRs #227 and #235 made it the one composer every writing screen uses), not a bare textarea with `usePreview`. It gains an `editing` prop: its tab and panel ids are `edit-N-…`, so it can sit beside the reply composer without duplicate ids, and it is marked `data-editing` rather than `data-composer`, which names the reply composer. Its fields are local signals, never watched by `state/drafts.ts`, so nothing of an edit is saved as a draft, and `checkOwner` has nothing to forget. Save is off for unchanged or empty source.
- **The editor closes when the viewer changes.** It is drawn only while the post is the viewer's own (`isOwn`), so a sign-in as someone else in the same tab unmounts it and drops the source it held. The source route answers without an ETag, so `apiGet` never holds it.
- **Component module `OwnControls.tsx`**, not `PostControls.tsx`: the rule is `postControls.ts`, and two modules whose names differ only by case resolve wrongly on a case-insensitive filesystem. It holds `useOwn` (one post's state), `PostEditor`, `DeleteConfirm`, `Restore` and `ownFailure`. The tests keep the names the criteria give.
- **Refusals.** Every refusal is said beside the control that sent it. `edit-window-closed` and `delete-window-closed` also withdraw what they name from that post while it is on screen, whatever the client's clock says: the server's clock is the one that counts, and a control that is only refused again is worse than none (as rename does after Terva review 1589). `conflict`, `forbidden`, `not-found` and `gone` ask for the page again, so the offers follow the server. After a success the post offers nothing until a page at least as long as the answer's `log_length` arrives, so a second delete is never offered while the refetch is in flight.
- **Focus.** The delete confirmation takes focus on Cancel, so a second Enter does not delete; Escape cancels. When the editor or the confirmation closes, focus goes to the control that opened it, on a phone the post's More button, and to the post when that control is gone. While the source loads, focus is on the editor's Cancel.
- **Server.** `readWindows` in `mutate.go` reads the windows for both `authorize` and the page, so the keys and `secondsDuration` cannot drift apart; `viewerWindows` converts to whole seconds. The locked-thread DTO fixture carries the windows too, since a lock leaves delete and restore open.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:20Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. The description assumes Edit and Delete controls that main does not have: Post.tsx offers quote, reply, link and history, and nothing in the client calls PATCH, DELETE or restore on a post. TKT-01M29CKRHS names this ticket as the owner of the client controls, so the plan builds them and gates them by the windows; effort is now M. Editing needs the post's source, which only history carries and history is closed to members when public_edit_history is off, so the route was filed as TKT-01M3QJYV4Z (Serve a post's current source to its author for editing) and linked as a dependency. author_windows is null for non-members as well as for forum moderators; the plan says why.

**agent:claude-code/phase2-groom** at 2026-09-29T22:25:28Z

Terva review 1488 on PR #167, head f5a4206, finding 2 (medium): answered, and the plan is clarified. created_at is Millis, a JSON number of milliseconds (web/client/src/api/dto.ts, dto.Millis; 1757500300000 in the goldens), not a date string, so the subtraction did not produce NaN. The plan now says the type, guards a non-finite value when a window applies, and names the test 'created_at is Millis'. Also, per the coordinator on 2026-09-30, TKT-01M3QHKD21 (Show moderators' post controls and the edited-by-a-moderator marker) now depends on this ticket and widens the same controls. This ticket builds the only set.

**agent:claude-code/phase2-groom** at 2026-09-30T09:21:43Z

Plan refreshed against main at a660ae5 on 2026-09-30. What changed: (1) The source route landed in PR #205. The editor reads GET .../posts/{ordinal}/source (dto.PostSource), which apiGet never holds, and sends {source} only. (2) Honouring TKT-01M3R33SR5's clamp, now a dependency: author_windows carries int(secondsDuration(n)/time.Second), so a setting past a Duration arrives as 9223372036 and client and server agree. useWindowClock bounds its timer with delayUntil (components/thread/deadline.ts, added by this ticket or TKT-01M3QXV399, whichever lands first), because a setTimeout over 2^31-1 ms fires at once. New criteria 6 and 7. (3) TKT-01M29CKRJ9 landed, so removal exists, and a moderator sees removed posts as a removed Post. Restore is drawn on a tombstone for an author and in a removed post's footer for a moderator-author, which settles the placement TKT-01M3QHKD21 left open. (4) After #202, ownership compares handles without case through isOwn, which mayFlag shares, and on a phone edit and delete go under PostMore with focus returned to its button. New criteria 8 and 9. (5) Known limit recorded: in an archived forum the page cannot tell delete and restore will be refused, so conflict's words show. (6) Goldens corrected: thread.author.json is an api golden, and thread-locked.member.json a dto one. Bundle 47415 of 60000 B.

**agent:claude-code/phase2-run-ptt97x** at 2026-09-30T19:18:42Z

Terva review 1610 on PR #243, head be48840, base 13709f0 (run 1586). Finding 1 (high), keep the editor open while a save is in flight: fixed. The editor's Cancel is off while the PATCH is out, and so are the delete confirmation's Cancel and Escape while the DELETE is out, since both show their refusal in place. Tests: 'a save in flight keeps the editor open', 'a delete in flight keeps the confirmation open'. Finding 2 (medium), refetch the page on a stale source-load refusal: fixed. A conflict, forbidden, not-found or gone answer from GET .../source now asks for the page again, as a refused save does, and says it in ownFailure's words. Test: 'a stale refusal of the source asks for the page again'. Reader bundle now 56988 B.

## Summary

The thread page now carries `viewer.author_windows` and the client offers Edit, Delete and Restore on the viewer's own posts only while the server would accept them.

Server: `dto.ThreadViewer.AuthorWindows` (`{edit_seconds, delete_seconds}`), read in `View`'s existing read transaction for a member who does not moderate the forum, through `readWindows` and `secondsDuration`, the same path `authorize` uses. It is null for a forum moderator and for anyone who is not a member. It is in the viewer block, so the response cache is untouched and the ETag moves with a setting. docs/07 records it. Tests: TestThreadViewerCarriesAuthorWindows, TestAuthorWindowsFollowTheClamp, TestAuthorWindowsAreNotCached; the thread goldens in internal/dto and internal/web/api are regenerated.

Client: `postControls.ts` is the one offer rule, and `isOwn` is its ownership check, shared with `mayFlag`. `OwnControls.tsx` holds the per-post state (`useOwn`), the inline editor on the shared `ComposerBody` (which gains an `editing` prop for its own ids), the delete confirmation and Restore. `useWindowClock` sets one page timer through `delayUntil`. On desktop, edit and delete follow flag in the footer. On a phone they are under PostMore, and focus returns to its button. Restore is drawn on a tombstone for an author and in a removed post's footer for a moderator-author. Nothing is offered on a moved or redacted post, or on a post a moderator removed. Window refusals withdraw the control locally, stale refusals refetch the page, and after a change nothing is offered until the page shows it. The reader bundle went from 54924 B to 56988 B of 70000 B.

Follow-on: TKT-01M3QHKD21 widens `postControls` and `useOwn` to a moderator's reach over every post; the placement of Restore is settled here.
