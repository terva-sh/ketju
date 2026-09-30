---
schema: 3
id: TKT-01M29CP2Q36D13MT5DDF48RX9E
title: Build the post history view with word-level diffs
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29CA9C2CNBHGBD9304N0EJE
  - TKT-01M29CKRHSMCRM9T440314D2Q8
  - TKT-01M3R1WPNSG5S4K02V245YZ9MA
blocks_on: none
references:
  - ref: design:post-history
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-30T08:52:37Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Every revision with its actor and time, and a word-level diff computed on demand from the two revisions' sources. Visible to moderators always, and to members when `public_edit_history` is on, which it is by default.

Public history by default is a trust decision rather than a technical one. In a group of people who know each other, visible edits discourage quietly rewriting what you said after somebody replied to it.

The diff is computed rather than stored, which costs nothing because both revisions are already in the log.

### Folded in from TKT-01M39WZY8C (Build the post history screen), archived as a duplicate on 2026-09-28

- **The route** is `/t/{id}/{slug}/{ordinal}/history` (docs/07 "URL scheme"). PR #64 left a placeholder there: the thread's title, "History of post #N", "This screen is not built yet", and a link back to the post. The placeholder keeps the slug correction, and so must the screen that replaces it. The Post footer's "history" link already points at this route.
- **The endpoint** is `GET /api/v1/threads/{id}/posts/{ordinal}/history` (TKT-01M29CA9C2). It answers `{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}`, and the client diffs two revisions' markdown sources (docs/08 "Post history").
- **Refusals** are the endpoint's:
  - an anonymous reader is `unauthenticated`;
  - a member is `forbidden` when `public_edit_history` is off;
  - a moderator can always read the history.

  Render the refusals through ErrorPanel.
- **A deleted post** answers `deleted: true` with no revisions today. The criterion below that lets a moderator see a deleted post's history needs the endpoint changed to serve its revisions to a moderator.

## Acceptance criteria

- [ ] The screen lists every revision with actor, time and reason, and wordDiff marks a one-word change inside a long line as exactly one deleted and one inserted word (wordDiff.test.ts, PostHistory.test.tsx)
- [ ] A grace-edited revision is listed with grace: true and a grace note although the post shows no edited marker (TestHistoryListsGraceRevisions, PostHistory.test.tsx)
- [ ] For a removed post a member gets deleted: true with no revisions and the screen shows the withheld notice, while a moderator gets and can compare every revision (TestTombstoneFromEveryEndpoint, PostHistory.test.tsx)
- [ ] Any two revisions can be chosen and compared (PostHistory.test.tsx)
- [ ] /t/{id}/{slug}/{ordinal}/history renders the screen instead of the PR #64 placeholder, and a stale slug there is corrected to the current one (ThreadPage.test.tsx)
- [ ] A history with redacted: true says 'This post was removed permanently.': as the notice when there are no revisions, and above a moderator's revisions with no comparison offered (PostHistory.test.tsx)
- [ ] The reader bundle stays within the 60000 B gzip budget, the PR records the total against 47415 B, and the dist from go generate ./internal/web/spa is committed
- [ ] A member is refused with forbidden when public_edit_history is off and an anonymous reader with unauthenticated, both rendered through ErrorPanel, while a moderator of the forum, by role or through a group's grant, always reads the history (TestHistory, TestHistoryForAForumModeratorByGrant, PostHistory.test.tsx)

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a. Refreshed 2026-09-30 against main at 5124209.

### Settled in grooming

- **TKT-01M39WZY8C (Build the post history screen) was folded into this ticket** on 2026-09-28 by the maintainer's decision, and archived as a duplicate. Its route, endpoint shape, refusals and placeholder detail are in this ticket's description. Everything below builds on them.
- **The route** is `/t/{id}/{slug}/{ordinal}/history` (docs/07 "URL scheme"). It replaces `PostHistoryPending` in `web/client/src/screens/ThreadScreen.tsx`, the PR #64 placeholder. The slug correction stays: `ThreadScreen` already runs `canonicalThreadUrl` on this route, and the screen stays under it.
- **The endpoint exists**: `GET /api/v1/threads/{id}/posts/{ordinal}/history` → `dto.PostHistory{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}` (`History` in `internal/service/threads/posts.go`, TKT-01M29CA9C2). The client diffs two revisions' sources (docs/08 "Post history"; settled for TKT-01M29CA9C2).
- **Refusals** are the endpoint's (docs/07 "The post endpoints"): an anonymous reader is `unauthenticated`, a member is `forbidden` when `public_edit_history` is off, and a moderator may always read it. They render through `ErrorPanel`. One exception, built by TKT-01M29CKRJ9 (revised for Terva review 1359): the author of a hidden-by-mod post is admitted to that post's history even when `public_edit_history` is off. The screen needs no special case for it, because it renders whatever the endpoint serves.
- **A moderator already receives a deleted post's revisions.** `History` returns early only when the post is removed and the viewer lacks `canModerate`, so the description's "needs the endpoint changed" is out of date on main. A member gets `deleted: true` with no revisions, and that is the refusal the criterion means. TKT-01M29CKRJ9 widens "moderator" to "moderator, or the author of their own hidden post" through one `revealRemoved` rule, which applies only to removed posts and leaves the history of visible posts as it is. That is not this ticket's work.
- **Grace revisions are listed** (docs/08: grace edits are "still stored"). TKT-01M29CKRHS writes `grace: true` into the `Edit` body.
- **The diff is computed on the client, word by word, from the markdown sources.** The HTML is never diffed.

Added in the 2026-09-30 refresh:
- **"Moderator" here means `moderate` in the thread's forum, by role or through a group.** `History` asks `a.canModerate`, which `access` in `internal/service/threads/access.go` fills from `Perm.Can(ctx, v, forumID, domain.PermModerate)`. That matches the maintainer's decision of 2026-09-30 that thread moderation follows the forum grant (docs/08 "Decided 2026-09-30", on PR #208's branch; TKT-01M3RQP0TG). The screen asks nothing of the viewer's role: it renders what the endpoint serves.
- **A hard-redacted post** (TKT-01M29CXJZ9, PR #192, done; docs/03 "Hard redaction"): a moderator is served its revisions with empty `source` and `reason`, and anyone else the tombstone, `deleted: true` with no revisions. TKT-01M3R1WPNS (Expose hard redaction in the post DTO and client) adds `redacted` to `dto.PostHistory`, and this screen reads it, so this ticket depends on that one. Without the flag, a moderator's list of empty revisions would read as a post edited to nothing.
- **The words for a redacted post** follow TKT-01M3R1WPNS's: "This post was removed permanently." A moderator's list adds "Its revisions are listed without their content." and offers no comparison, because every source is empty.
- **The screen is reader code and counts against the 60000 B gzip budget** (docs/07 "Preact client"). `web/client/scripts/bundle.mjs` counts every chunk a reader can load except `src/screens/admin/`, lazy ones included. The reader total on main at 5124209 is 47415 B.

### What changed since the plan was written

Read from main at 5124209:
- The endpoint, `revealRemoved` and the author's admission to a hidden post's history are all on main (TKT-01M29CKRJ9, done). The settled item that calls this "not this ticket's work" still holds.
- `threadlog.Revision` (`internal/store/threadlog/reader.go`) still has no `Grace`, and `Reader.History` does not read `EditBody.Grace`. Step 1 stands.
- The phone post menu `PostMore` (`web/client/src/components/thread/PostMore.tsx`, PR #202) links to `…/history` as the desktop footer does. Neither needs a change.
- `GET …/posts/{ordinal}/source` (PR #205) is for editing, not history. This screen does not use it.
- The test named `TestHistoryOfARemovedPost` in the earlier plan does not exist. The removed-post rule is held by `TestTombstoneFromEveryEndpoint` and `TestHistory` (`internal/web/api/posts_test.go`), `TestHiddenPostsAuthorReadsHistoryWithSettingOff` (`tombstones_test.go`) and `TestRedactedHistoryHasNoContent` (`redact_test.go`).

### Approach

1. **Server.** `threadlog.Revision` gains `Grace bool`, set from `EditBody.Grace` in `Reader.History`. A redacted edit keeps `Grace` (`blankEdit` in `redact.go`), so this holds after a redaction too. `dto.PostRevision` gains `Grace bool` with `json:"grace"`, with no `omitempty` (the `internal/dto` package rule). `History` in `internal/service/threads/posts.go` copies it. Regenerate the goldens (`go test ./internal/dto -update`, `go test ./internal/web/api -update`), then update `web/client/src/api/dto.ts`. This is the only server change.
2. **Word diff** in `web/client/src/history/wordDiff.ts`. Tokenize each source into words and whitespace runs, keeping punctuation attached to its word. Run Myers O(ND) over the two token arrays. The output is a list of `{op: 'same'|'ins'|'del', text}` runs. No dependency is added. The `diff` npm package lost: it would add to the reader bundle and to dependency review, for about 80 lines of code. Sources are capped at 64 KB (docs/07), so N+M is bounded.
3. **Screen** `web/client/src/screens/PostHistory.tsx`, rendered by `ThreadScreen` in place of `PostHistoryPending`.
   - It fetches `GET /api/v1/threads/{id}/posts/{ordinal}/history` with `useResource` and `apiGet`.
   - It lists revisions newest first. Each row shows the revision number, the actor through `Avatar` and `RoleBadge`, the time through `PostTime`, the reason, and a "grace edit" note where `grace` is set.
   - Two radio columns choose the older and the newer revision. The default compares the newest with the one before it.
   - The diff renders as `<ins>` and `<del>` inside a `pre-wrap` block. The colours go in `thread.css`, for both themes.
   - Refusals render through `ErrorPanel`.
   - `deleted: true` with no revisions renders a notice. The words are "This post was removed permanently." when `redacted` is set, and "This post was removed. Its history is not shown." otherwise.
   - A `redacted` history with revisions (a moderator's) lists them without the comparison, under "This post was removed permanently. Its revisions are listed without their content."
   - A link goes back to the post (`permalink`).
   - The screen is a static import in `ThreadScreen`. A lazy chunk lost: `bundle.mjs` counts it either way, and it would add a round trip to a screen reached from a link.
4. Regenerate the dist with `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Run `npm --prefix web/client run budget` and record the reader total in the PR against 47415 B.

### Tests

- `wordDiff.test.ts`: one word changed inside a long line gives exactly one `del` and one `ins` of that word, with everything else `same`. That shows the diff works word by word, not line by line. Also: pure insertion, pure deletion, identical inputs giving one `same` run, whitespace-only changes, and two 64 KB sources that finish under a stated time.
- `PostHistory.test.tsx`:
  - It lists revisions with actor, time and reason.
  - Comparing revision 1 with revision 3 shows their diff.
  - A grace revision carries its note.
  - `unauthenticated` and `forbidden` render through `ErrorPanel`.
  - `deleted: true` with no revisions renders the removed notice.
  - `deleted: true, redacted: true` with no revisions renders "removed permanently".
  - A moderator's `redacted: true` history lists its revisions and offers no comparison.
- `ThreadPage.test.tsx` "the post history route": the placeholder assertions are replaced by the screen's, and `/t/{id}/{old-slug}/27/history` is corrected to the current slug.
- Server: `TestHistoryListsGraceRevisions` (`internal/web/api/posts_test.go`). After a grace edit and a later edit, history has three revisions and only the second has `grace: true`. The page shows the edited marker from the third. A reader-level case in `internal/store/threadlog` checks that `Revision.Grace` is read from the body.
- Server: `TestHistoryForAForumModeratorByGrant` (`internal/web/api/posts_test.go`). A member whose group holds `moderate` in the thread's forum reads the history with `public_edit_history` off. The same member is `forbidden` in a forum where the group holds no grant. `TestHistory` covers moderators by role only.
- The removed-post rule is unchanged, and these stay green: `TestTombstoneFromEveryEndpoint`, `TestHistory`, `TestHiddenPostsAuthorReadsHistoryWithSettingOff` and `TestRedactedHistoryHasNoContent`.
- The reader bundle stays within 60000 B (`npm run build`).

### Out of scope

- Who may read a hidden post's history beyond moderators: TKT-01M29CKRJ9, done.
- Writing grace edits: TKT-01M29CKRHS, done. A moderator's edit shown in history as the moderator's: TKT-01M29CRJ5J, done. History already names each revision's actor.
- The `redacted` field itself: TKT-01M3R1WPNS.
- Who holds `moderate` by grant for the thread routes: TKT-01M3RQP0TG. This screen needs no change for it.
- A server-computed diff for the non-JS client: Phase 5.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:52:37Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) now depends on TKT-01M3R1WPNS (Expose hard redaction in the post DTO and client), because a moderator's history of a hard-redacted post (PR #192) is revisions with empty sources, and the screen needs PostHistory.redacted to say 'removed permanently' instead of offering a diff of empty text. A criterion and tests were added for it. (2) 'Moderator' is moderate in the thread's forum, by role or through a group (access.canModerate), which matches the maintainer's decision of 2026-09-30. TestHistoryForAForumModeratorByGrant is new, because TestHistory covers role moderators only. (3) TestHistoryOfARemovedPost did not exist. The criteria now name TestTombstoneFromEveryEndpoint and TestHistory instead. (4) The screen is reader code. The budget is recorded against 47415 B of 60000 B, and a criterion says so. (5) PostMore (PR #202) links to history, and no change is needed there. threadlog.Revision still has no Grace, so the server step stands. Every criterion now names its test.
