---
schema: 3
id: TKT-01M29CP2Q36D13MT5DDF48RX9E
title: Build the post history view with word-level diffs
type: task
status: draft
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
blocks_on: none
references:
  - ref: design:post-history
    path: docs/08-moderation.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T06:18:29Z
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

- [ ] The screen lists every revision with actor, time and reason, and wordDiff marks a one-word change inside a long line as exactly one deleted and one inserted word
- [ ] A member is refused with forbidden when public_edit_history is off and an anonymous reader with unauthenticated, both rendered through ErrorPanel, while a moderator always reads the history
- [ ] A grace-edited revision is listed with grace: true and a grace note although the post shows no edited marker
- [ ] For a removed post a member gets deleted: true with no revisions and the screen shows the withheld notice, while a moderator gets and can compare every revision
- [ ] Any two revisions can be chosen and compared
- [ ] /t/{id}/{slug}/{ordinal}/history renders the screen instead of the PR #64 placeholder, and a stale slug there is corrected to the current one

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a.

### Settled in grooming

- **TKT-01M39WZY8C (Build the post history screen) was folded into this ticket** on 2026-09-28 by the maintainer's decision, and archived as a duplicate. Its route, endpoint shape, refusals and placeholder detail are in this ticket's description. Everything below builds on them.
- **The route** is `/t/{id}/{slug}/{ordinal}/history` (docs/07 "URL scheme"). It replaces `PostHistoryPending` in `web/client/src/screens/ThreadScreen.tsx`, the PR #64 placeholder. The slug correction stays: `ThreadScreen` already runs `canonicalThreadUrl` on this route, and the screen stays under it.
- **The endpoint exists**: `GET /api/v1/threads/{id}/posts/{ordinal}/history` → `dto.PostHistory{ordinal, deleted, revisions: [{revision, actor, at, reason, source}]}` (`History` in `internal/service/threads/posts.go`, TKT-01M29CA9C2). The client diffs two revisions' sources (docs/08 "Post history"; settled for TKT-01M29CA9C2).
- **Refusals** are the endpoint's (docs/07 "The post endpoints"): an anonymous reader is `unauthenticated`, a member is `forbidden` when `public_edit_history` is off, and a moderator may always read it. They render through `ErrorPanel`. One exception, built by TKT-01M29CKRJ9 (revised for Terva review 1359): the author of a hidden-by-mod post is admitted to that post's history even when `public_edit_history` is off. The screen needs no special case for it, because it renders whatever the endpoint serves.
- **A moderator already receives a deleted post's revisions.** `History` returns early only when the post is removed and the viewer lacks `canModerate`, so the description's "needs the endpoint changed" is out of date on main. A member gets `deleted: true` with no revisions, and that is the refusal the criterion means. TKT-01M29CKRJ9 widens "moderator" to "moderator, or the author of their own hidden post" through one `revealRemoved` rule, which applies only to removed posts and leaves the history of visible posts as it is. That is not this ticket's work.
- **Grace revisions are listed** (docs/08: grace edits are "still stored"). TKT-01M29CKRHS writes `grace: true` into the `Edit` body.
- **The diff is computed on the client, word by word, from the markdown sources.** The HTML is never diffed.

### Approach

1. **Server**: `threadlog.Revision` gains `Grace bool`, read from `EditBody.Grace` in `Reader.History`. `dto.PostRevision` gains `grace bool`. Regenerate the dto golden and `web/client/src/api/dto.ts`. This is the only server change.
2. **Word diff** in `web/client/src/history/wordDiff.ts`: tokenize each source into words and whitespace runs, keeping punctuation attached to its word, and run Myers O(ND) over the token arrays. The output is a list of `{op: 'same'|'ins'|'del', text}` runs. There is no new dependency. The `diff` npm package lost: it would add to the committed bundle and its size budget (docs/07), and to dependency review, for about 80 lines. Sources are capped at 64 KB (docs/07), so N+M is bounded.
3. **Screen** `web/client/src/screens/PostHistory.tsx`, rendered by `ThreadScreen` in place of `PostHistoryPending`. It fetches with `useResource` and lists revisions newest first: revision number, actor through `Avatar` and `RoleBadge`, time through `PostTime`, the reason, and a "grace edit" note where `grace` is set. Two radio columns choose the older and the newer revision. The default is the newest against the one before it. The diff renders as `<ins>` and `<del>` inside a `pre-wrap` block, with the colours in `thread.css` for both themes. Refusals render through `ErrorPanel`. `deleted: true` with no revisions renders a notice: "This post was removed. Its history is not shown." A link goes back to the post (`permalink`).
4. The thread page's post footer already links here, so no change is needed there. Regenerate `dist/` with `go generate ./internal/web/spa`.

### Tests

- `wordDiff.test.ts`: one word changed inside a long line gives exactly one `del` and one `ins` of that word, with everything else `same`. That shows the diff works word by word rather than line by line. Also: pure insertion, pure deletion, identical inputs giving one `same` run, whitespace-only changes, and two 64 KB sources that finish under a stated time.
- `PostHistory.test.tsx`: lists revisions with actor, time and reason; comparing revision 1 against 3 shows their diff; a grace revision carries its note.
- Refusals: `unauthenticated` and `forbidden` render through `ErrorPanel`, and `deleted: true` renders the notice.
- `ThreadPage.test.tsx` "the post history route": the placeholder assertions are replaced by the screen's, and `/t/{id}/{old-slug}/27/history` is corrected to the current slug.
- Server: `TestHistoryListsGraceRevisions`. After a grace edit and a later edit, history has three revisions and only the second has `grace: true`. The page shows the edited marker from the third.
- `TestHistoryOfARemovedPost` keeps the endpoint's rule: a member gets `deleted: true` with no revisions, and a moderator gets them all. The author of a hidden-by-mod post gets them too, with the setting off as well as on; that case is TKT-01M29CKRJ9's `TestHiddenPostsAuthorReadsHistoryWithSettingOff`, and this screen renders it like any other history.

### Out of scope

- Who may read a hidden post's history beyond moderators: TKT-01M29CKRJ9.
- Writing grace edits: TKT-01M29CKRHS. A moderator's edit shown in history as the moderator's: TKT-01M29CRJ5J, and history already names each revision's actor.
- A server-computed diff for the non-JS client: Phase 5.
