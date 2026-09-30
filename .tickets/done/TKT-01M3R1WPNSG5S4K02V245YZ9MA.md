---
schema: 3
id: TKT-01M3R1WPNSG5S4K02V245YZ9MA
title: Expose hard redaction in the post DTO and client
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKP8C66GXXQK4HPEZ35
origin: TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
dependencies:
  - TKT-01M29CXJZ9MVXGY1FC0ESYNG8F
blocks_on: none
references:
  - ref: pr:228
    path: null
  - ref: pr:237
    path: null
claim: null
archive: null
created_at: 2026-09-30T02:21:56Z
updated_at: 2026-09-30T15:19:08Z
created_by:
  id: agent:claude-code/phase2-run-cxjz9
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-r1wpns
  name: ""
extensions: {}
---

## Description

TKT-01M29CXJZ9 (Build the offline hard-redaction tool) serves a hard-redacted post as a moderator removal with no reason: `deleted: true`, `removal: "moderator"`. The index knows the difference (EntryRedacted, bit 7), but the post DTO has no field for it, so a moderator cannot tell a redacted post from a hidden one, and any restore the client offers on it is answered with conflict.

Adding `redacted` to `internal/dto` was tried in that lane and reverted: `web/client/src/dto-contract.ts` pins the Go goldens to `dto.ts` exactly, and that lane was barred from touching web/client and the built dist.

### Scope

- `dto.Post` and `dto.HistoryPost` gain `redacted bool` (omitempty), set from `EntryRedacted` in `internal/service/threads/view.go` and `posts.go`.
- Goldens under `internal/dto/testdata` and `internal/web/api/testdata` regenerated; `dto.ts` and the contract test updated.
- The client shows a redacted post as removed permanently and offers no restore or edit on it; `go generate ./internal/web/spa` rebuilds the dist.

## Acceptance criteria

- [x] dto.Post and dto.PostHistory carry redacted (bool, no omitempty), set from threadlog.EntryRedacted in postsDeps and History; the dto and api goldens, dto.ts and the fixtures follow, and contract.test.ts passes
- [x] removedText and removedMark say 'Post removed permanently' and 'removed permanently' for a redacted post whatever its removal (removal.test.tsx)
- [x] The moderator queue shows a redacted post as 'Removed permanently' with no Remove and no Keep, and Dismiss still sends dismiss (QueueScreen.test.tsx)
- [x] docs/07 Removed posts and the thread response shape name the redacted field
- [x] The thread-view bundle stays within the 60000 B gzip budget and the dist from go generate ./internal/web/spa is committed
- [x] A redacted post answers redacted: true to every reader admitted to each route (anon, member, author, moderator): the page, the single post, /users/{handle}/posts, the moderator queue and history; a visible or moderator-hidden post answers false (redact_test.go)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 9a8fc76. The server half is TKT-01M29CXJZ9 (Build the offline hard-redaction tool), PR #192 at 5c2ba25. Its summary says: "The API serves a redacted post as a moderator removal with no reason to everyone and refuses edit, delete and restore with conflict." Its note 6, gap 1, records that a `redacted` DTO field was tried and reverted because `web/client/src/dto-contract.ts` pins the goldens to `dto.ts` and that lane could not touch the client.

The description needs three corrections, read from main:
- The history DTO is `dto.PostHistory` (`internal/dto/dto.go`), not `dto.HistoryPost`.
- The flag is `threadlog.EntryRedacted` (bit 7) in `internal/store/threadlog/flags.go`.
- `internal/dto` forbids `omitempty` (the package comment: "no field here carries omitempty"), so the field is a plain `bool` present on every post.

### Settled in grooming

- **What the reader shows is the maintainer's** (decided 2026-09-28, docs/10 item 4 and docs/03 "Hard redaction"): "A redacted post renders as a tombstone saying it was removed, to every reader, moderators included, since nothing of it is left; its edit history lists the revisions with no content, to a moderator." The thread actor refuses any `Edit`, `Delete` or `Restore` of it, and the API answers `conflict` (docs/07 "Removed posts": "No response field yet tells it from a moderator's hide", which this ticket closes).
- **`redacted` is served to every reader** (engineering choice, made in the maintainer's absence because the docs answer it). docs/03 gives every reader, moderators included, the same tombstone. The flag comes from the index, so it is the same in every viewer class, and it reveals nothing that any reader can see beyond "this cannot come back". Serving it to moderators only lost: the words would differ by class for a post whose content is gone for every class, and the class split would buy nothing.
- **`redacted` sits beside `removal`, and `removal` is unchanged** (engineering choice). A redacted post still reads `deleted: true` with `removal` from its other flags, which is `"moderator"`, or `"author"` if its author had deleted it before the redaction (`removal` in `internal/service/threads/view.go`). A fourth `removal` value `"redacted"` lost for two reasons. `removal` says who took the post down, and a redaction is a separate fact about whether it can come back. And every client that switches on `removal` would read an unknown value as a plain "Post removed", which loses nothing but also says nothing.
- **The field goes on `dto.PostHistory` as well.** A moderator's history of a redacted post is its revisions with empty `source` and `reason`. Without the flag the history screen (TKT-01M29CP2Q3) cannot tell "removed permanently" from "edited to nothing".
- **The response cache needs no new key.** `redacted` is the same for every viewer class, and a redaction appends a `Redacted` marker, which moves the log length in `respcache.Key`. `ketju redact` also runs with serve stopped, and both caches are in memory.
- **The words** (engineering choice). A tombstone reads "Post removed permanently", and a shown-removed mark reads "removed permanently". The ticket already asks for "removed permanently", and it is the one thing the reader needs to know: nobody can restore it. The redaction's reason is never served; `Redacted.reason` is in the log for the operator.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").

### Approach

1. **DTO** (`internal/dto/dto.go`):
   - `Post` gains `Redacted bool` with `json:"redacted"`, after `Removal`. Its comment says: set for a hard-redacted post (docs/03 "Hard redaction"), which is also `deleted` and is never restorable.
   - `PostHistory` gains `Redacted bool` with `json:"redacted"`.
2. **Service** (`internal/service/threads`):
   - `postsDeps` in `view.go`: set `d.Redacted = p.Flags.Has(threadlog.EntryRedacted)`. Every post DTO is built here: the page, the single post, replies, `/users/{handle}/posts` (`userposts.go`) and the moderator queue (`QueueFlag.Post`). So all of them carry it with no other change. Update the comment above the `removal` call.
   - `History` in `posts.go`: set `Redacted: redacted` on `res.Body` for every reader, the tombstone case included.
3. **Goldens.**
   - `internal/dto/fixtures_test.go`: one post fixture (in the `thread.moderator` case) becomes a redacted post: `deleted: true`, `removal: "moderator"`, `redacted: true`, `html: ""`, no attachments. One `PostHistory` fixture sets `redacted: true`. This satisfies the rule that every field is set non-zero by some case.
   - Regenerate with `go test ./internal/dto -update` and `go test ./internal/web/api -update`, and check each diff is only the new key.
4. **Client types**: `web/client/src/api/dto.ts` gains `redacted: boolean` on `Post` and on `PostHistory`. `dto-contract.ts` needs no new import, since it already matches those goldens. Test fixtures that build a `Post` by hand gain `redacted: false`, which `tsc` will list.
5. **Client words** (`web/client/src/components/thread/removal.tsx`):
   - `removedText` and `removedMark` take `Pick<PostDTO, 'removal' | 'redacted'>`. `redacted` is checked first and answers "Post removed permanently" or "removed permanently".
   - `TombstoneText` passes it through.
   - `Tombstone.tsx`, `Post.tsx`, `ReplyExpander.tsx`, `PostPopover.tsx` and the queue draw from here, so every screen that draws a removed post through them follows with no change of its own.
6. **Moderator queue** (`web/client/src/screens/admin/queue/QueueScreen.tsx`):
   - `postState` gains `'redacted'`, checked before `removal`, with `stateWords.redacted` "Removed permanently".
   - For a redacted post the item offers neither Remove nor Keep, and shows "This post was removed permanently and cannot be shown again." in place of `data-keep-refused`. Dismiss stays, so its flags can be closed.
   - This is the only place in the client that offers a restore today. The thread page has no post edit, delete or restore controls on main.
7. **docs/07** "Removed posts": replace "No response field yet tells it from a moderator's hide" with the `redacted` field, and add `redacted` to the post in "Thread response shape".
8. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

- `TestRedactedPostIsATombstoneToEveryone` (`internal/web/api/redact_test.go`) also asserts `redacted: true` on #5 for anon, member, author and moderator, on the page and the single post. It asserts `redacted: false` on a visible post (#6) and on a post a moderator hid in the same world.
- `TestRedactedHistoryHasNoContent` asserts `redacted: true` in the moderator's history and in the author's and member's tombstone history.
- `TestRedactedIsFlaggedEverywhere` (new, `redact_test.go`): `redacted: true` on the post in `/api/v1/users/alice/posts` for the author and a moderator, and in the moderator queue's `QueueFlag.Post` for a post flagged before its redaction.
- `internal/dto` golden and fixture tests pass with the regenerated goldens. `web/client/src/contract.test.ts` and `tsc` pass with the new fields.
- `components/thread/removal.test.tsx` (new): `removedText` and `removedMark` say "permanently" for `redacted: true` whatever `removal` is, and are unchanged for `redacted: false`.
- `screens/admin/queue/QueueScreen.test.tsx`: a redacted flagged post shows "Removed permanently", no Remove and no Keep button, and a Dismiss that sends `dismiss`.
- The thread-view bundle stays within the 60000 B gzip budget (`npm run build`).

### Out of scope

- The history screen's handling of a redacted post (TKT-01M29CP2Q3, Build the post history view with word-level diffs), which reads the new `PostHistory.redacted`.
- Post edit, delete and restore controls on the thread page, which no client ticket has built yet.
- Title text in older `thread.renamed` audit rows (TKT-01M29CKRH9, done).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T04:04:40Z

Groomed 2026-09-30 (TKT-01M3M9RR26). Settled as engineering choices in the maintainer's absence, because docs/03 'Hard redaction' answers them: redacted is served to every reader (the tombstone is the same for everyone, moderators included), removal is unchanged beside it, and the words are 'Post removed permanently'. Corrections to the description: the history DTO is dto.PostHistory, not dto.HistoryPost, and internal/dto forbids omitempty, so the field is a plain bool on every post. The only restore the client offers today is the moderator queue's Keep, which the plan withdraws for a redacted post.

**agent:claude-code/phase2-groom** at 2026-09-30T08:58:52Z

Plan refreshed against main at 5124209 on 2026-09-30. The plan is current, so it is not rewritten. Checked on main: dto.Post and dto.PostHistory still have no redacted field. docs/07 'Removed posts' still says no response field tells a redaction from a hide. Every post DTO is still built in one place, postsDeps in view.go (the page, the single post, replies, /users/{handle}/posts through userposts.go, and the queue through posts.go). removal.tsx, QueueScreen's postState and stateWords, and data-keep-refused are as the plan names them. Landed since, with no change to the plan: the flag menu (PR #202). mayFlag already offers no flag on a deleted post, and a redacted post is deleted, so neither PostMore nor FlagDialog changes. The post source route (PR #205) already answers a redacted post with conflict, held by internal/service/threads/source_test.go. The thread page still has no edit, delete or restore controls. Members who moderate through a group see the queue (moderatesGate) and get the same words. TKT-01M29CP2Q3 (post history view) now depends on this ticket for PostHistory.redacted. The docs/13 section the Settled item cites is now titled 'Running two lanes, changed', with the same rule.

**agent:claude-code/phase2-run-r1wpns** at 2026-09-30T12:46:34Z

Claimed for the extended Phase 2 run (docs/13 "The extension of 2026-09-30"). The orchestrator split this ticket into two PRs, because another lane holds the client dist:
1. Server half (this PR): `redacted` on dto.Post and dto.PostHistory for every reader, the goldens, docs/07 and redact_test.go. dto.ts takes a types-only edit, which the goldens force. `go generate ./internal/web/spa` leaves internal/web/spa/dist byte-identical. Criteria 1, 4 and 6 are ticked. The ticket stays in-progress after it merges.
2. Client half (a later PR, once the dist lane is free): the "Post removed permanently" words in removal.tsx, the moderator queue offering no Remove and no Keep on a redacted post, the regenerated dist, and criteria 2, 3 and 5. That PR closes the ticket.

The plan stands as written. Details settled while building, all internal:
- The redacted fixture post (#65) goes on the moderator's thread page only, as the plan says. web/client tests import thread.member.json and one counts its posts (ThreadScreen.test.tsx), so adding the post to every class would have forced a client test edit in the server half. The member history golden's tombstone is now the redacted post's (ordinal 65, redacted: true).
- dto-contract.ts needed no change. No client test fixture needed `redacted` either: tsc --noEmit passes over src with the new required field.
- Security check, as the orchestrator asked: the post source route (PR #205) refuses a redacted post to everyone. redact_test.go TestRedactedIsFlaggedEverywhere asserts 401 for an anonymous reader, 403 for another member, and 409 for the author and a moderator, and that no answer carries the text. The same test asserts that no route (page, single post, parent's replies, history, source, /users/alice/posts, moderator queue) serves the redacted text to anon, member, author or moderator. Every route but the source serves `redacted: true` to each reader it admits.

**agent:claude-code/phase2-run-r1wpns** at 2026-09-30T15:18:29Z

Client half, on main at 3fb7d5a, after the orchestrator released the dist lane (the server half is PR #228, merged at 6b5dc9a).

Built as the plan says: removedText and removedMark answer "Post removed permanently" and "removed permanently" for a redacted post whatever its removal. TombstoneText passes `redacted` through, so the thread page, the reply expander, the popover and the queue follow. The queue's postState gains 'redacted', checked before `deleted`, reading "Removed permanently", with no Remove and no Keep, and "This post was removed permanently and cannot be shown again." in place of the Keep.

A server defect found while building, fixed in this PR because the criterion's Dismiss depends on it. The queue's stateOf (internal/service/threads/queue.go) took a redacted post by its other bits. A post hidden pending review and then redacted read as stateAutoHidden, so dismiss and Keep each tried a Restore. The log refuses every record naming a redacted post, so both answered conflict, and the flags could never close. A shown post that was then redacted read as stateVisible, so Remove answered conflict too. stateOf now counts EntryRedacted as stateGone. Every action then only closes the flags, answering "already hidden", and writes nothing to the log. TestRedactedPostLeavesTheQueue holds the six cases (shown or hidden pending review, times dismiss, Keep or remove). docs/08 "Flags" and docs/07's queue screen say so. Nothing here reveals content: the actor refused the writes before and still does.

Scope, stated so nobody looks for it here:
- History: the client has no post history screen yet; TKT-01M29CP2Q3 (Build the post history view with word-level diffs) builds it and reads PostHistory.redacted, which the server half serves.
- A quote of a redacted post: the server renders "This post was deleted." for a quote of any removed post (internal/render/quote.go). That text has never said who removed a post, and it carries none of the post's text. Saying "permanently" there would need the quote resolver to know about redaction, plus a renderer version bump. That is out of this ticket's scope and leaks nothing.

Checks run on this head: gofmt; go build and go vet; go test -race ./...; the Phase 2 exit tests; go generate ./internal/web/spa with the dist committed (thread view 54912 B of 70000 B); tsc; vitest (1790 passed); and the full Playwright suite on port 8931 (161 passed; 11 skipped by the specs themselves, such as the safe-area check on desktop).

## Summary

A hard-redacted post is now marked as one in the API and reads as one in the client. It landed in two PRs.

- Server (PR #228, merged at 6b5dc9a): dto.Post and dto.PostHistory carry `redacted`, set from threadlog.EntryRedacted, and served to every reader alike. The goldens, dto.ts and docs/07 follow. redact_test.go holds that no route (page, single post, replies, history, source, /users/{handle}/posts, moderator queue) serves a redacted post's text to anon, member, author or moderator. It also holds that the source route refuses a redacted post to everyone.
- Client (this PR): a redacted post reads "Post removed permanently" as a tombstone and "removed permanently" as a mark, wherever removal.tsx draws a removed post. The moderator queue names it "Removed permanently" and offers only Dismiss flags.
- Server fix found on the way: the queue's stateOf now counts a redacted post as already down. Before this, a post hidden pending review and then redacted kept its flags open for good, because every action on it tried a Restore that the log refuses.

Left to others: the post history screen, which reads PostHistory.redacted (TKT-01M29CP2Q3). A quote of a redacted post still renders the server's generic "This post was deleted.", as a quote of any removed post does.
