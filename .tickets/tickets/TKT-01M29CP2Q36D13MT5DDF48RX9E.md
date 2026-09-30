---
schema: 3
id: TKT-01M29CP2Q36D13MT5DDF48RX9E
title: Build the post history view with word-level diffs
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-cp2q3
  branch: run/cp2q3-post-history
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ac9f0ed705612d47b
  commit: 13709f0d74ab93dbdc040da9e4bd218ed6f0a324
  session: null
  claimed_at: 2026-09-30T18:42:44Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-30T19:07:40Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2q3
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

Groomed 2026-09-29 against main at 7af2c5a. Refreshed 2026-09-30 against main at 5124209. Revised at claim on 2026-09-30 against main at 13709f0.

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

### Revised at claim, 2026-09-30, against main at 13709f0

Everything under "Settled in grooming" above holds and is built to. What changed since the 2026-09-30 refresh, and how it is taken:

- **TKT-01M3R1WPNS is done** (#228, #237). Its words for a redacted post are `removedText`'s in `components/thread/removal.tsx`, "Post removed permanently", on the tombstone, the popover and the moderator queue. The settled item says the words "follow TKT-01M3R1WPNS's", so the screen calls `removedText` rather than repeating the refresh's draft sentence "This post was removed permanently.": a redacted post reads the same everywhere. A moderator's list reads "Post removed permanently. Its revisions are listed without their content." The orchestrator's brief for this run says the same.
- **Post times are absolute** (maintainer decision of 2026-10-01): revision times use `PostTime`.
- **The budget is 70000 B**, raised by the maintainer on 2026-09-30 (docs/08 "Decided 2026-09-30"). The reader total on main at 13709f0 is 54924 B. The criterion's 47415 B is the refresh's number; the PR records the total against both.
- **History access for a removed post follows `revealRemoved`** (TKT-01M29CKRJ9), unchanged. `TestHistoryForAForumModeratorByGrant` is added, as planned.

### Approach

1. **Server.** `threadlog.Revision.Grace`, read from `EditBody.Grace` in `Reader.History`. `dto.PostRevision.Grace` as `"grace"`, no `omitempty`, copied in `History`. The dto fixture gains the author's grace revision, because `TestEveryFieldIsPopulatedSomewhere` refuses a field no fixture sets. `dto.ts` follows. Nothing else on the server changes.
2. **`web/client/src/history/wordDiff.ts`.** Tokens are whitespace runs and non-whitespace runs, so punctuation stays with its word. The common head and tail are set aside, then Myers O(ND) runs with its trace kept per edit count. Whitespace kept between a replacement and the next change is folded into both sides, so a rewritten phrase reads as one deletion and one insertion. The search stops at 1000 edits: past that the middle shows as one deletion and one insertion, which is still a true account. That bounds memory at about a million ints and time well under the 64 KB test's half second. No dependency is added.
3. **`web/client/src/screens/PostHistory.tsx`**, rendered by `ThreadScreen` in place of `PostHistoryPending`.
   - Revisions are listed newest first, each with a From and a To radio. The default is the newest against the one before it. Any two can be chosen, and the same one twice says to choose two.
   - Each row shows Avatar, name, RoleBadge, `PostTime`, the reason, and a grace note.
   - The diff renders as `<del>` and `<ins>` in a pre-wrap block, with a key sentence. Sources are text, never markup.
   - Refusals go through ErrorPanel.
   - A withheld history says "Post removed. Its history is not shown.", or `removedText`'s words when redacted.
   - A moderator's removed-post history is compared as usual, under a note that the post is removed.
   - The CSS goes in `thread.css` and reuses the themed soft tokens, so no token is added outside the mockups. The chosen row takes an accent edge, not a fill, so the muted role badge keeps its contrast. On a phone the radios' labels reach 44 px.
4. **Static, not lazy.** Measured both ways with `bundle.mjs`. Static: 56686 B, +1762 B over main. Lazy, with its CSS split out: 57551 B. `bundle.mjs` counts reader lazy chunks, and two gzip streams compress worse than one.
5. **Regenerate the dist** after replaying onto the main of the day. This lane waits for the dist.

### Tests

- `wordDiff.test.ts`:
  - one word in a 60-word line gives exactly one `del` and one `ins`;
  - punctuation stays with its word;
  - pure insertion and pure deletion;
  - identical and empty inputs;
  - a whitespace-only change;
  - a rewritten phrase folds into one deletion and one insertion;
  - empty sides;
  - shortest edit script against an LCS table, 300 random cases;
  - the 1000-edit cap;
  - two 64 KB sources, scattered edits and a rewrite, each under 500 ms.
- `PostHistory.test.tsx`:
  - newest-first list with actor, role, absolute time and reason;
  - the default diff;
  - any two compared;
  - a grace note on the grace revision only;
  - a single revision offers no comparison;
  - `unauthenticated` and `forbidden` through ErrorPanel;
  - a withheld removed post;
  - a redacted post with none;
  - a moderator's redacted list with no comparison;
  - a moderator's removed post compared;
  - sources shown as text.
- `ThreadPage.test.tsx`: the route renders the screen, not the placeholder, and `/t/{id}/old-slug/27/history` is corrected. `view.test.tsx`'s stale-slug stub now answers the history endpoint.
- `e2e/post-history.spec.ts`, at 390 px and 1440 px on alice's stored session. A thread in events is edited twice. A stale slug lands on the history. The default and chosen diffs render, and the link goes back to the post. One axe scan per test, in the light and dark themes.
- Server:
  - `TestHistoryListsGraceRevisions`: a grace edit, then an edit past the window, give three revisions with only the second marked, and `edited_at` is the third's;
  - `TestHistoryForAForumModeratorByGrant`;
  - the reader-level grace case in `TestHistoryIsBoundedAndOrdered`;
  - these stay green: `TestTombstoneFromEveryEndpoint`, `TestHistory`, `TestHiddenPostsAuthorReadsHistoryWithSettingOff` and `TestRedactedHistoryHasNoContent`.

### Out of scope

Unchanged from the refresh:
- who may read a hidden post's history (TKT-01M29CKRJ9, done);
- writing grace edits (TKT-01M29CKRHS, done);
- moderator edits in history (TKT-01M29CRJ5J, done);
- the `redacted` field (TKT-01M3R1WPNS, done);
- grant-based thread moderation (TKT-01M3RQP0TG);
- a server-computed diff for the non-JS client (Phase 5).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:52:37Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) now depends on TKT-01M3R1WPNS (Expose hard redaction in the post DTO and client), because a moderator's history of a hard-redacted post (PR #192) is revisions with empty sources, and the screen needs PostHistory.redacted to say 'removed permanently' instead of offering a diff of empty text. A criterion and tests were added for it. (2) 'Moderator' is moderate in the thread's forum, by role or through a group (access.canModerate), which matches the maintainer's decision of 2026-09-30. TestHistoryForAForumModeratorByGrant is new, because TestHistory covers role moderators only. (3) TestHistoryOfARemovedPost did not exist. The criteria now name TestTombstoneFromEveryEndpoint and TestHistory instead. (4) The screen is reader code. The budget is recorded against 47415 B of 60000 B, and a criterion says so. (5) PostMore (PR #202) links to history, and no change is needed there. threadlog.Revision still has no Grace, so the server step stands. Every criterion now names its test.
