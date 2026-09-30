---
schema: 3
id: TKT-01M3QHKD21M1JPCQBZVCQZ4V6K
title: Show moderators' post controls and the edited-by-a-moderator marker
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: TKT-01M29CRJ5J9XMJZ30FMZX4D473
dependencies:
  - TKT-01M29CRJ5J9XMJZ30FMZX4D473
  - TKT-01M3PTT97XM3DCGRTDNHHBNM5M
blocks_on: none
references:
  - ref: pr:251
    path: null
claim: null
archive: null
created_at: 2026-09-29T21:37:14Z
updated_at: 2026-09-30T22:24:39Z
created_by:
  id: agent:claude-code/phase2-run-crj5j
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qhkd21
  name: ""
extensions: {}
---

## Description

The server side of TKT-01M29CRJ5J (Allow moderators to edit and delete any post) lets a viewer with `moderate` in the forum edit, delete and restore any post, and serves `edited_by_moderator` on `dto.Post`. The client does not use either yet, because the Phase 2 run's server lane may not change the committed `internal/web/spa/dist`.

This is step 8 of that ticket's plan:
- `Post.tsx` offers Edit, Delete and Restore on every post to a viewer with `viewer.can_moderate`, not only on the viewer's own posts. A moderator's Restore applies to a post that is deleted or hidden by a moderator.
- A post with `edited_by_moderator: true` shows "(edited by a moderator)" where it would show "(edited)". It does not name the moderator; the post's history does (docs/08 "Post history").
- Regenerate `internal/web/spa/dist` with `go generate ./internal/web/spa`.

The tombstone wording and the author's view of their own hidden post are TKT-01M29CKRJ9, not this ticket.

## Acceptance criteria

- [x] A moderator's delete of another member's post says that members will see it removed by a moderator and only a moderator can restore it, and delete and restore call the existing endpoints (PostControls.test.tsx)
- [x] A post with edited_by_moderator true shows (edited by a moderator) in place of (edited), naming nobody (ThreadPage.test.tsx)
- [x] The thread-view budget holds, and the dist regenerated with go generate ./internal/web/spa is committed
- [x] With viewer.can_moderate, by role or through a group, the shared postControls rule from TKT-01M3PTT97X offers Edit and Delete on every visible post with moved_to null, and Restore on every removed post served with its body, at any age; nothing on a moved post, no Restore on a redacted post (deleted with empty html), and a member still gets only their own (postControls.test.ts)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 1dd6ed9. Refreshed on 2026-09-30 against main at a660ae5 (5124209 plus the run's tracking commits). The server half is merged: TKT-01M29CRJ5J (Allow moderators to edit and delete any post), PR #165. This ticket is its plan step 8.

**It shares one set of controls with TKT-01M3PTT97X** (Offer edit and delete only inside the author windows in the client). On main no client code offers Edit, Delete or Restore on any post. That ticket builds them: the offer rule `postControls` in `components/thread/postControls.ts`, the footer buttons in `Post.tsx` and under `PostMore` on a phone, Restore on `Tombstone.tsx` and on a removed `Post`, `PostEditor.tsx` on the source route (PR #205), and the refetch after a change. This ticket depends on it, widens the rule to moderators, and adds the marker.

### Settled in grooming

- **What a moderator may do** is `authorize` in `internal/service/threads/mutate.go` as merged: a viewer with `moderate` in the thread's forum (`access.canModerate`, which is `viewer.can_moderate` on the page) may edit, delete or restore any post, is bound by no window, and needs no `reply`. An archived thread is `conflict` to everyone, and a lock refuses only members' edits (docs/07 "Edit, delete and restore", docs/08).
- **The post states each action takes** (TKT-01M29CRJ5J): Edit and Delete refuse a post that is gone (deleted, hidden by a moderator, or moved). Restore takes a post that is deleted or hidden by a moderator (`RequireAny: EntryDeleted|EntryHiddenByMod`) and refuses a moved one.
- **Only a moderator restores a moderator's delete** (decided 2026-09-28, docs/08 "Restoring a moderator's delete"). A moderator's delete of someone else's post writes `Delete{by_moderator: true}`, which hides the post (hidden-by-mod) rather than deleting it.
- **The marker names nobody.** `dto.Post.edited_by_moderator` is true when the post shows as edited and its current revision's actor is not its author. The post shows "(edited by a moderator)" where it would show "(edited)". The moderator's handle is in the post's history, which is public by default (docs/08 "Post history"). The editor is not in the response cache's authors digest, which is why the page does not name them (TKT-01M29CRJ5J, settled there).
- **Moderators see a removed post's content inline** (docs/08 "Tombstones"): the page serves a moderator the post with its `html` and `deleted: true` (`HiddenEntry` in `view.go`). The wording and styling of that view, and of the tombstones, are TKT-01M29CKRJ9, which is being built now.
- **No viewer class or cache key changes.** `edited_by_moderator` is read from the frame, which the log length already keys. `TestModeratorHideMovesTheResponseCacheKey` (TKT-01M29CRJ5J) holds the cache invariant for the hide.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. The thread view stays under `threadViewGzipBytes` (60000).

Engineering choices made here, each with the alternative that lost:
- **Widen the shared rule; do not add a second one.** `postControls` gains one branch: when `viewer.can_moderate`, the viewer's own-post condition is dropped and no window applies (`author_windows` is null for such a viewer anyway). Edit and Delete are offered on a post that is not deleted and has `moved_to` null, and Restore on a post that is `deleted` and has `moved_to` null, whoever removed it. Building a moderator menu beside the author controls lost: it would duplicate the editor, the confirmation and the refetch.
- **Where a moderator's controls go on a removed post.** The page draws a moderator's copy of a removed post with its content. Restore goes in that post's footer. Until TKT-01M29CKRJ9 lands its moderator inline view, `ThreadScreen` draws every `deleted` post as `Tombstone`, so Restore stays on the tombstone as TKT-01M3PTT97X builds it. Whichever of the two tickets lands second puts Restore on the moderator's inline view.
- **The delete prompt says who will see what.** For a moderator deleting someone else's post: "Remove this post? Members will see that a moderator removed it. Only a moderator can restore it." For one's own post, TKT-01M3PTT97X's words stay. These sentences follow docs/08 and decide nothing new.
- **No reason field.** The routes take an optional `reason`, and docs/08 does not require one for a moderator's edit or delete. A required reason would be a product rule. The history shows the actor either way.


Added in the refresh of 2026-09-30:
- **TKT-01M29CKRJ9 has landed** (Render tombstones for deleted and moved posts, PRs #170 and #174), so the question "Where a moderator's controls go on a removed post" is settled. This supersedes that item's placement. `ThreadBody` draws a removed post served with its body as `Post`, with the class `removed` and `removedMark`, and any other as `Tombstone` (`isTombstone` in `components/thread/removal.tsx`). After its refresh of 2026-09-30, TKT-01M3PTT97X draws Restore in whichever of the two a post is drawn as. So this ticket places nothing and only widens the rule.
- **A moderator's Restore is offered on a removed post served with its body** (engineering choice, refining "Widen the shared rule" above). `revealRemoved` in `internal/service/threads/view.go` serves a moderator the body of every removed post except a moved or a redacted one. `restoreRecord(true, …)` in `mutate.go` takes a post that is deleted or hidden, and refuses exactly those two: `Refuse: EntryMoved|EntryRedacted`. So for a moderator the rule offers restore when `p.deleted && !isTombstone(p)`. A redacted post is `deleted`, and its `moved_to` is null. Offering restore on "deleted and `moved_to` null" alone would offer it there, and the answer would be `conflict`.
- **Group moderators are included, with no dependency on TKT-01M3RQP0TG.** The post routes are thread routes, not admin routes, and `authorize` checks `a.canModerate`, which a group grant sets. So `viewer.can_moderate` is the whole test, as it is on the server.
- **The thread screen as it stands now.** PR #202 (TKT-01M3QQWA5Z) put flag in the footer and `PostMore` on a phone. A moderator's controls reach the phone through TKT-01M3PTT97X's `PostMore` `controls`, with nothing added here. The header still renders `(edited)` as `<span class="edited">` when `post.edited_at !== null`. The reader bundle is 47415 B of 60000 B gzip on main.

### Approach

1. **Rule** (`components/thread/postControls.ts`), the moderator branch: when `viewer.can_moderate`, the own-post condition is dropped and no window applies (`author_windows` is null for such a viewer). Edit and delete are offered on a post that is not `deleted` and has `moved_to` null. Restore is offered on a post that is `deleted` and not a tombstone (`!isTombstone(p)`), whoever removed it. It still answers `closesAt: null` for a moderator, so `useWindowClock` sets no timer.
2. **Marker** (`components/thread/Post.tsx`): where the header renders `(edited)` when `post.edited_at !== null`, render `(edited by a moderator)` when `post.edited_by_moderator` is also true. Keep the class `edited` and add `data-edited-by="moderator"`.
3. **Delete prompt** in the shared inline confirmation: the moderator's words when `!isOwn(p)`. They match `removedText`'s "Post removed by a moderator", which is what members will then see.
4. **Restore on a removed post**: offered by the rule and drawn where TKT-01M3PTT97X draws it, in the removed `Post`'s footer. It is sent as `POST …/restore` with no body, as TKT-01M3PTT97X sends it.
5. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Record the change from main's figure in the PR.

### Tests

- `components/thread/postControls.test.ts`:
  - `a moderator may act on every post`: with `can_moderate: true` and `author_windows: null`, edit and delete are offered on another member's visible post at any age. Restore is offered on another member's removed post served with its body, whether its `removal` is `author`, `moderator` or `review`.
  - `nothing on a moved or redacted post`: no control on a post with `moved_to` set, for a moderator or an author. No restore for a moderator on a deleted post with empty `html`, which is how a redacted post is served.
  - `a member still gets only their own`: with `can_moderate: false`, another member's post offers nothing (TKT-01M3PTT97X's case, kept).
- `components/thread/PostControls.test.tsx`:
  - `a moderator's delete of another's post says who sees what`, and sends `DELETE` with no body.
  - `a moderator restores a hidden post`: from the removed post's footer, `POST …/restore`, then the page is requested again.
  - `a group moderator gets the same controls`: role `member` with `can_moderate: true`.
- `screens/ThreadPage.test.tsx`, `the edited marker`: `edited_by_moderator: true` with `edited_at` set shows "(edited by a moderator)" and no handle other than the author's. `edited_by_moderator: false` shows "(edited)".
- The thread-view budget passes.

### Out of scope

- The author's controls, the editor, the window rule, where the controls are drawn, and the phone layout: TKT-01M3PTT97X.
- The post source route that the editor reads: TKT-01M3QJYV4Z, done in PR #205.
- Tombstone wording, `removal`, the author's view of their own hidden post, and the moderator's inline view: TKT-01M29CKRJ9, done.
- Naming the moderator on the page, which the response cache design rules out (TKT-01M29CRJ5J).
- Suspension's bulk hide: TKT-01M29CP2QK.
- Who may use the thread moderation routes: TKT-01M3RQP0TG. This ticket uses none of them.

### Revised at claim, 2026-10-01, against main at cd485ef

Everything above stands, `### Settled in grooming` included. What the code on main changed since the refresh, and what is built differently, with the reason for each:

- **TKT-01M3PTT97X has landed** (PR #243). The rule is `postControls` in `components/thread/postControls.ts`, and its ownership check `isOwn` is shared with `mayFlag`. The per-post state is `useOwn` in `OwnControls.tsx` (not `PostControls.tsx`: two module names differing only by case), with `PostEditor`, `DeleteConfirm` and `Restore`. `ThreadBody` hands an `Own` only for `isOwn(p)`. It already draws Restore in a removed `Post`'s footer and on a `Tombstone`, so this ticket places nothing.
- **The moderator branch** (engineering choice, the server's `authorize` and `restoreRecord` as merged). When `viewer.can_moderate`, the rule drops `isOwn`, applies no window, and does not ask for `can_reply`, on any post: Edit and Delete on a post that is not `deleted`, with `moved_to` null and not `redacted`; Restore on a post that is `deleted`, not `redacted`, with `moved_to` null, and served with its body (`!isTombstone(p)`), whatever its `removal`. The redacted and moved guards come first, as #243 built them. A moderator is offered Edit on their own post without `reply`, because `authorize` returns before the reply check for a moderator (a forum where members may only read, and a group that grants `moderate` without `reply`, both reach it). `ThreadBody` hands every post an `Own` when the page says `can_moderate` and someone is signed in.
- **The page says when the forum is read-only to this viewer** (fixing a control the server would refuse). In an archived forum `authorize` refuses a non-admin's edit, delete and restore of their own post, a moderator's own post included, with `conflict` (`readOnlyForum`), and leaves a moderator's action on somebody else's post open. #243 recorded that the page could not tell, and relied on `can_reply` for the author's edit. The moderator branch cannot: `can_reply` false does not tell an archived forum from a moderator without `reply`, and guessing either way offers a refused control or hides an allowed one. So `dto.ThreadViewer` gains `forum_read_only`, which is `readOnlyForum(a) != nil`: true in an archived forum for everyone but an admin. It is in the viewer block, which is never cached, so no cache key changes, and the ETag moves with it. The rule offers nothing on the viewer's own post when it is true, which also closes #243's known limit for an author's delete and restore. Adding `archived` to `ThreadHeader.forum` lost again, for #243's reason: it is cached and changes the header for every viewer, while only the viewer's own relation to it matters here.
- **The delete prompt.** For a post that is not the viewer's own: "Remove this post? Members will see that a moderator removed it. Only a moderator can restore it." For one's own post, #243's words stay.
- **The editor and confirmation close when the viewer changes.** #243 closed them when the post stopped being the viewer's own. A moderator's `Own` depends on the page's `can_moderate`, which a sign-in as someone else in the tab does not change until the page is fetched again, so `useOwn` also closes them when the signed-in handle changes.
- **The marker** as planned: `(edited by a moderator)` in place of `(edited)`, class `edited`, `data-edited-by="moderator"`.
- **Bundle.** The reader bundle is 56988 B of 70000 B on main. The additions are a branch in the rule, one sentence and one attribute, far smaller than a lazy chunk's loader, so nothing moves to a lazy chunk. The PR records the delta.

### Tests, revised

- `postControls.test.ts`: `a moderator may act on every post` (another member's visible post at any age; Restore on a removed post served with its body for `removal` author, moderator and review; no `can_reply` needed); `nothing on a moved or redacted post` for a moderator; `a member still gets only their own`; `nothing on one's own post in a read-only forum`, for a member and a moderator, while a moderator keeps every control on another's post there.
- `PostControls.test.tsx`: `a moderator's delete of another's post says who sees what` (`DELETE` with no body); `a moderator restores a hidden post` (`POST …/restore` from the removed post's footer, then the page again); `a group moderator gets the same controls` (role `member` with `can_moderate: true`); the editor closes when the signed-in viewer changes.
- `ThreadPage.test.tsx`, `the edited marker`: `edited_by_moderator: true` shows `(edited by a moderator)` and names nobody; false shows `(edited)`.
- Go: `TestThreadViewerSaysTheForumIsReadOnly` in `internal/web/api` (archived forum: member, moderator by role and by group get true, admin false; open forum false), and the thread goldens regenerated.
- The thread-view budget passes, and the dist is regenerated with `go generate ./internal/web/spa`.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:25:28Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, on main 1dd6ed9 after PR #165. It depends on TKT-01M3PTT97X, which builds the shared post controls (none exist on main). This ticket widens the offer rule for viewer.can_moderate and adds the marker. Restore on a moderator's inline view of a removed post is placed by whichever of this ticket and TKT-01M29CKRJ9 lands second.

**agent:claude-code/phase2-groom** at 2026-09-30T09:22:29Z

Plan refreshed against main at a660ae5 on 2026-09-30. What changed: (1) TKT-01M29CKRJ9 landed first (PRs #170, #174), so a moderator sees removed posts as a removed Post. TKT-01M3PTT97X's refresh now draws Restore wherever the post is drawn, and this ticket places nothing. The earlier placement item is carried verbatim and marked superseded. (2) Engineering choice: a moderator's Restore is offered on a removed post served with its body (!isTombstone). revealRemoved withholds exactly the moved and redacted posts, and restoreRecord refuses exactly those, so a redacted post (deleted, moved_to null) no longer gets a Restore that would answer conflict. Criterion 1 was replaced by criterion 4. (3) Group moderators get the controls through can_moderate. The post routes are not admin routes, so there is no dependency on TKT-01M3RQP0TG. (4) The phone layout comes from TKT-01M3PTT97X's PostMore controls after #202, and the bundle figure is 47415 of 60000 B.

**agent:claude-code/phase2-run-qhkd21** at 2026-09-30T19:48:58Z

Built and checked locally on 2026-10-01 against main at cd485ef, not yet pushed: the client dist is held by other lanes, and the dist commit is dropped and regenerated at replay.

What is built: the moderator branch in `postControls`; an `Own` for every post to a signed-in viewer whose page says `can_moderate`; the moderator's delete prompt through `Own.hides`; `useOwn` closing the editor and the confirmation when the signed-in handle changes; the `(edited by a moderator)` marker; and `ThreadViewer.forum_read_only` on the server, with a new golden `thread-archived.member.json` so that `TestEveryFieldIsPopulatedSomewhere` sees the field set.

Mutations tried against the tests, each caught:
- the moderator branch disabled: 6 tests failed across `postControls.test.ts` and `PostControls.test.tsx`;
- Restore without `!isTombstone(p)`: 'nothing on a moved or redacted post' failed;
- `forum_read_only` ignored: "nothing on one's own post in a read-only forum" failed;
- no `Own` for a moderator in `ThreadBody`: 4 component tests failed;
- `useOwn` keyed without the handle: 'the editor goes when the signed-in viewer changes' failed;
- `hides` always false: the delete-prompt test and the group moderator test failed;
- the marker always `(edited)`: 'the edited marker' failed;
- `ForumReadOnly` always false in `view.go`: `TestThreadViewerSaysTheForumIsReadOnly` failed for carol, bob and gina.

Checks run: gofmt, `go build`, `go vet`, `go test -race ./...` (exit 0), `tsc --noEmit`, vitest (63 files, 1889 tests), and Playwright on port 18931 (175 passed, 11 skipped). The reader bundle went from 56988 B to 57153 B of 70000 B, +165 B. There is no lazy chunk: the moderator-only code is one branch in the rule and one sentence, which is smaller than a chunk's loader.

## Summary

A viewer whose thread page says `can_moderate`, whether by role or through a group grant, is now offered Edit and Delete on every post that is not removed, moved or redacted, and Restore on every removed post served with its body. No window applies and no `reply` is needed, which is exactly what `authorize` and `restoreRecord` accept. A moderator's edit shows as "(edited by a moderator)" and names nobody.

Client:
- `postControls` gains the moderator branch.
- `ThreadBody` hands every post an `Own` for a signed-in viewer whose page says `can_moderate`.
- `Own.hides` gives a moderator's delete of someone else's post its own prompt: "Remove this post? Members will see that a moderator removed it. Only a moderator can restore it."
- `useOwn` closes the editor and the confirmation when the signed-in handle changes.
- `Post` shows `(edited by a moderator)` with `data-edited-by="moderator"`.

Server: `dto.ThreadViewer.forum_read_only` is `readOnlyForum(a) != nil`. In an archived forum it is true for everyone but an admin, and there the server refuses an edit, delete or restore of the viewer's own post. The rule offers nothing on the viewer's own post while the field is true. That covers a moderator's own post, and it closes the known limit TKT-01M3PTT97X recorded for an author's delete and restore. The field is in the viewer block, which is never cached. The thread goldens gain the field, and the new golden `thread-archived.member.json` sets it. docs/07 records the field and the moderator's offers.

Tests:
- `TestThreadViewerSaysTheForumIsReadOnly`: a member, a moderator by role, a moderator by group and an admin, each checked against the server's answer.
- `postControls.test.ts`: 'postControls for a moderator'.
- `PostControls.test.tsx`: "a moderator's controls", including a group moderator's edit and a change of viewer.
- `ThreadPage.test.tsx`: 'the edited marker'.
- Eight mutations were tried against the tests, and every one was caught.

The reader bundle went from 59923 B to 60080 B of 70000 B (+157 B), with no lazy chunk.
