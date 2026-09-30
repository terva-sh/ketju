---
schema: 3
id: TKT-01M3NS16VHMVXZW0TG41KVTNSV
title: Build the new-thread screen with draft autosave
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M3K9XBJA8FSJ8ADCEY284SGA
  - TKT-01M3NS19315QTWBKNZN782VRF1
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase2-run-ns16vh
  branch: run/ns16vh-new-thread-screen
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a39b42604a9c0f5bf
  commit: 1ae02c57ee7d2a8b741ff68b2af57e2339f6a77f
  session: null
  claimed_at: 2026-09-30T12:58:03Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:08:37Z
updated_at: 2026-09-30T13:28:07Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-ns16vh
  name: ""
extensions: {}
---

## Description

The route `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:113`), although the forum index (`screens/ForumIndex.tsx:57,88`) and the thread list (`screens/ThreadListScreen.tsx:72`) both link to it. The server has everything the screen needs:
- `POST /api/v1/forums/{slug}/threads` creates the thread (TKT-01M29CA9B9);
- `POST /api/v1/render` previews it;
- `GET/PUT/DELETE /api/v1/forums/{slug}/draft` autosaves it, title included, once TKT-01M3K9XBJA (Autosave new-thread drafts) lands.

No ticket builds the screen. Found while planning TKT-01M3K9XBJA.

### What to build
- A new-thread screen with a title field and the existing composer (toolbar, preview tab, server preview): inline on desktop, and the composer sheet on the phone (docs/06 "Composing").
- The draft is loaded on open and filled in, saved on a debounce while typing (title and body), and deleted after the thread is created.
- A forum the reader cannot create in shows the refusal the API gives. The same goes for an archived forum.
- After creating, go to the new thread.
- Rebuild the dist (`go generate ./internal/web/spa`) and stay within the 60000 B gzip budget.

## Acceptance criteria

- [x] A member can create a thread from /f/{slug}/new at 390 px and 1440 px, and lands on it
- [x] The title and body typed on one device are restored from the server draft on another, and the draft is deleted once the thread is created
- [x] The reply composer and the new-thread screen share composer/ComposerBody.tsx, and Composer.test.tsx and ComposerSheet.test.tsx pass unchanged
- [x] The dist is regenerated with go generate ./internal/web/spa and committed
- [x] An archived forum's notice never disables Post before the API has answered, so an admin can start a thread in an archived forum from the screen (NewThreadScreen.test.tsx)
- [x] The forum draft's autosave is disarmed until its GET answers, so an empty form never deletes a forum draft before it loads (state/newThread.test.ts)
- [x] e2e/new-thread.spec.ts creates a thread in events and restores a title-and-body draft in a second context at 390 px and 1440 px, with axe reporting no violations, and npm run budget stays within 60000 B gzip
- [x] On a phone the screen hides the tab bar through ShellContext.composing and carries Post in the top bar through ShellContext.primary (Shell.test.tsx)
- [x] forbidden, conflict, validation-failed, payload-too-large and rate-limited are said in words and keep the typed text; a draft-save forbidden or conflict only turns autosave off and never disables Post, only the create route's own forbidden or conflict disables it, and an admin's draft saves and Post both succeed in an archived forum (NewThreadScreen.test.tsx)
- [x] Creating the thread removes the forum draft's local copy as well as the server draft, and a local copy is restored only when the server's revision says no other save came between (state/newThread.test.ts, with state/drafts.ts from TKT-01M3NS1931)

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310. Refreshed against main at 5124209 on 2026-09-30: TKT-01M3N24RHD's admin exemption has landed, which changes what a draft save answers an admin; the phone top bar needs a primary action it does not have; and the round-5 requirements on `state/drafts.ts` (TKT-01M3NS1931) are inherited.

What exists at 5124209:
- `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:119`). `ThreadListScreen.tsx:71` links to it for a signed-in reader. `ForumIndex.tsx:57` and `:88` open `ForumPicker` (`:93`), whose links (`ForumPicker.tsx:71`) lead to it, and `ForumIndex.tsx:195` offers "Start one" in an empty forum. The picker drops archived forums (`ForumPicker.tsx:77-80`) and "Start one" needs `!f.archived` (`ForumIndex.tsx:192`), so an admin reaches an archived forum's screen from its thread list or by address.
- `POST /api/v1/forums/{slug}/threads` takes `{title, source, tags}` (`internal/service/threads/create.go:38-44`) and answers 201 `dto.CreatedThread{thread, post}`. A hidden forum is `not-found`, an archived forum is `conflict` to everyone but an admin (`forumForThread`, `create.go:217-233`), no `create_thread` is `forbidden`, and the title must be 1–200 characters (`MaxTitle`, `create.go:26`) with no control characters (docs/07). Slow mode applies to replies only, so creating a thread has no wait.
- `POST /api/v1/render` renders a preview; without `thread_id`, quotes render optimistically and `reply_to` is refused (docs/07).
- `composer/Composer.tsx` is bound to a thread: `composerFor(thread)`, `usePreview(thread, …)` (`:306-335`) and `PostButton` (`:241-256`) post a reply. The toolbar (`edit.ts`), the mention list (`useMentions`) and the preview panel are inside it.
- `state/shell.ts`: `ShellAction` is `{id, label, run}` (`:8-12`) and `ShellContext` is `{title, back?, actions, reply?}` (`:17-25`). `reply` swaps the tab bar for the reply bar (`Shell.tsx:25`, `:46`). `PhoneTopBar` draws the back link, the title, the moderation queue button when the viewer has a queue, and `ctx.actions` inside `MoreMenu`. Nothing there draws a primary button. On the reply path, Post is in `ComposerSheet`'s `.sheet-head` (`ComposerSheet.tsx:85-90`).
- `/forums` (`dto.Forum.archived`) is usually held by `api/client.ts` from the forum index, readable with `peekGet` (`:74`).
- `api/problem.ts` reads `validation-failed`'s `errors` extension into `fieldErrors` (`:85`, `:107-111`).
- The e2e seed (`cmd/ketju/e2e.go:162-166`) has no archived forum. `thread-list.spec.ts` reads `general`'s page 1 by title and checks that pinned threads come first, and `forum-index.spec.ts` expects `reading` to stay empty. `events` holds one seeded thread and no spec reads its list.
- Two other ready tickets edit the composer: TKT-01M3R0PM0K (Show slow mode in the composer) adds a `slow` prop to `Composer` and `PostButton`, and TKT-01M3PTT97X (Offer edit and delete only inside the author windows) exports `usePreview` for its `PostEditor`.

### Settled in grooming

- **The route** is `/f/{forum-slug}/new`, "new thread composer" (docs/07 URL scheme).
- **A new-thread draft saves its title as well as its body** (maintainer decision 2026-09-28, docs/04 `drafts`, TKT-01M3K9XBJA). The routes are `GET/PUT/DELETE /api/v1/forums/{slug}/draft`, `PUT {title?, source}` replacing the whole draft, 204 when there is none, `create_thread` needed to save, an archived forum `conflict` (TKT-01M3K9XBJA's plan, a dependency).
- **The composer is the same everywhere**: toolbar, preview tab, and the server-rendered preview (docs/06 "Composer"). On a phone, composing is a full-screen sheet with the same toolbar and preview (docs/06 "Mobile"), and list screens have a floating "new thread" button.
- **Archived forums are read-only except for admins** (decided 2026-09-28, docs/04; docs/08 "Decided 2026-09-29", TKT-01M3N24RHD). The client shows the API's answer and decides nothing itself.
- **The built client is committed.** Run `go generate ./internal/web/spa` after any client change, and `npm run budget` stays within 60000 B gzip.

Engineering decisions taken here, with the alternative that lost:
- **The composer body is extracted, not copied.** `composer/ComposerBody.tsx` holds the Write and Preview tabs, the toolbar, the textarea with mentions, and the preview panel, over any state with `source` and `caret`, and an optional `thread` for the preview. The reply `Composer` keeps its header, chip and footer around it; the new-thread screen puts a title field and its own footer around it. Lost: a `mode` prop on `Composer`, which doubles every branch; and a second composer, which drifts.
- **Autosave through `state/drafts.ts`** from TKT-01M3NS1931 (Autosave reply drafts from the composer), with the key `new:<forum>` and the snapshot `{title, source}`. This ticket depends on it. Its rules hold here too: a 1500 ms debounce, a loaded draft never overwrites typed text, blank title and body mean `DELETE`, and requests for one draft keep their order.
- **On a phone the screen is the sheet.** The page itself is the full-screen column the sheet uses (`.composer.in-sheet`), with Post in the top bar. `ShellContext` gains `composing: true`, which hides the tab bar the way `reply` does. Lost: opening `ComposerSheet` as a dialog over an empty page, since there is no page under it to return to; Back leaves the route.
- **Not lazy.** The screen is small and reuses the composer already in the main chunk; the budget counts a lazy chunk anyway (`bundle.mjs`).
- **The API decides who may post; the client only warns.** There is no `can_create_thread` flag to read beforehand. When `/forums` is held (`peekGet`) and the forum is archived, the screen shows a notice before anything is typed: "This forum is archived. Only admins can start threads here." It does **not** disable Post on that basis, because admins may post in an archived forum (docs/04, docs/08) and the rule belongs to the server. A refusal of a **draft save** never touches Post. The draft route answers `conflict` in an archived forum to everyone (TKT-01M3K9XBJA), admins included until TKT-01M3N24RHD says otherwise, while the create route admits an admin there. So a draft-save `forbidden` or `conflict` turns autosave off and shows the quiet status "Draft not saved", and Post stays enabled. Only the **create** route's own `forbidden` or `conflict` disables Post. Then the screen shows the refusal and keeps the text so it can be copied. Terva review 1375 (PR #143, head 17f3b02) found that the previous revision let a draft-save conflict disable Post for an admin. Lost: retrying the draft save as an admin, which the draft route would still refuse. Terva review 1372 (PR #143, head 6d6aeea) found that the first version disabled Post for an admin too. Two alternatives lost: disabling up front for every viewer whose role is not admin, which copies a server rule into the client and goes stale when TKT-01M3N24RHD changes it; and filing a capability flag on the thread list, which changes a cached projection answer to save one refusal.
- **The client deletes the draft after creating the thread**, as for replies (docs/07 "Drafts": `DELETE` is safe to retry). Lost: deleting it inside the create transaction, which changes TKT-01M29CA9B9's create path for no gain the retry does not give.


Settled in the refresh of 2026-09-30:
- **TKT-01M3N24RHD has landed, so an admin's draft saves succeed in an archived forum.** The draft route saves through `forumForThread` (TKT-01M3K9XBJA), which admits an admin there (`create.go:222`). The sentence above that the draft route answers `conflict` "admins included until TKT-01M3N24RHD says otherwise" no longer holds. The rule it supports stands: a draft-save refusal only turns autosave off, and only the create route's own `forbidden` or `conflict` disables Post. Draft and create now give the same answer in the same forum, so the rule guards against a future divergence rather than a present one, and its test uses a mocked answer.
- **The round-5 requirements hold here too** (TKT-01M3NS1931's note of 2026-09-29, and its plan). The forum draft's local copy is ordered by the server's `revision`, never by the client's clock, and creating the thread removes the local copy as well as the server draft. Both come with `state/drafts.ts` through `drop`, so this screen only calls `drop` on success. The same four cases decide a forum draft's copy on the next load, 204s included (review 1559 on PR #210): an unacknowledged first draft comes back, and one posted or discarded on another device does not.
- **Post on a phone is a primary action in the top bar** (engineering choice). `ShellContext` gains `primary?: ShellAction & {disabled?: boolean}`, and `PhoneTopBar` draws it as a labelled button at the right end, before `MoreMenu`, with the queue button kept. `composing: true` hides the tab bar as `reply` does. The desktop layout ignores both, since the screen's own footer carries Post there. Two alternatives lost: a `.sheet-head` inside the screen, which would put two bars at the top of a phone screen; and a Post entry in `actions`, which would bury the one action the screen exists for inside a "more" menu.
- **`PostButton` stays outside `ComposerBody`** (engineering choice). The reply footer keeps it, where TKT-01M3R0PM0K adds `slow`, and the new-thread screen has its own button, since creating a thread has no slow mode. `usePreview` stays an exported hook taking an optional thread, which TKT-01M3PTT97X's `PostEditor` also uses. Whichever of these tickets lands second takes the other's changes to `Composer.tsx`.
- **The e2e spec starts threads in `events`** (engineering choice). `thread-list.spec.ts` reads `general`'s first page by title and the pinned-first order, which each new thread in `general` would shift. `events` is public, holds one seeded thread, and no spec reads its list. Lost: `general`, as planned before.
- **A `validation-failed` answer marks its field** when `fieldErrors` names one (`title` or `source`), beside the sentence the screen already shows.

### Approach

1. `composer/ComposerBody.tsx`: move the tabs, toolbar, textarea, mention list and preview panel out of `Composer.tsx`. Props: `state` (`source`, `caret`, `summoned`, `posting`), `thread?`, `replyTo?`, `label` ("Reply text" or "Thread text"), `sheet`. `usePreview` sends `thread_id` and `reply_to` only when a thread is given, and stays exported. The mention list's id comes from a `useId`-style counter rather than the thread id. `Composer.tsx` uses it and keeps its header, chip, footer and `PostButton`, and its tests pass unchanged.
2. `state/newThread.ts`: `newThreadFor(forum)` with `title`, `source`, `caret`, `summoned`, `posting`, `failure`, and the same owner rule as `composerFor`. It starts `watch({kind: 'new', forum}, () => ({title, source}))`. `loadNewThreadDraft(forum)` fills an empty form from `apiGetOptional<ForumDraft>`'s body after `restoreLocal`, which reads the revision from the body or, on a 204, from `Draft-Revision`, then arms the key. The key is created disarmed, as TKT-01M3NS1931 settles, so an empty form never sends `DELETE` for a forum draft before it has loaded, and a failed load never deletes one. `createThread(forum)`: one at a time, `POST /api/v1/forums/{slug}/threads` with `{title, source}`, then `drop` the draft, clear the state, and answer the `CreatedThread`.
3. `screens/NewThreadScreen.tsx` for `/f/:forum/new`:
   - A signed-out reader gets the sign-in prompt the other screens use; a pre-user is sent away by TKT-01M29CKRG9's gate.
   - The forum's name from `peekGet('/api/v1/forums')`, or from the thread list's `forum`; `not-found` from the draft load shows "No such forum".
   - A labelled title input (`maxlength` 200), `ComposerBody`, and a footer with the draft status line, Discard (confirm when there is text; it drops the draft), and "Post thread", off while the title or body is blank or a post is in flight.
   - Refusals in words: `forbidden` "You may not start threads in this forum."; `conflict` "This forum is archived."; `validation-failed` "The title must be 1 to 200 characters with no control characters, and the post cannot be empty.", with the named field marked; `payload-too-large`; `rate-limited` with `Retry-After`.
   - On success, `route('/t/{id}/{slug}')`, where the thread page shows post 1.
   - `useShellContext({title: 'New thread', back: {href: '/f/{slug}', label: forum name}, actions: [], composing: true, primary: {id: 'post-thread', label: 'Post', run, disabled}})`.
4. `state/shell.ts`, `shell/Shell.tsx` and `shell/PhoneTopBar.tsx`: `composing` hides the tab bar, and `primary` is drawn in the phone top bar, as settled.
5. `routes.tsx`: the route renders `NewThreadScreen`. `routes.test.tsx` still diffs the table against docs/07.
6. Build: `npm run build`, `go generate ./internal/web/spa`, commit the dist, and record the thread view's gzip figure in a note. It was 47415 B of the 60000 B `threadViewGzipBytes` budget after PR #202, and the screen is not lazy: `scripts/bundle.mjs` exempts only the admin screens' chunk (`:7-9`, `:22`).

### Tests

- `composer/ComposerBody.test.tsx`: the preview sends `thread_id` only when given. `Composer.test.tsx` and `ComposerSheet.test.tsx` pass unchanged.
- `state/newThread.test.ts`: a loaded draft fills an empty form with title and body and not a typed one; a load slower than the debounce sends no `DELETE` before it answers; `createThread` posts once on a double click, drops the draft and its local copy, and clears the form; a refusal keeps the text.
- `screens/NewThreadScreen.test.tsx`:
  - Post is off with a blank title or body;
  - each refusal's words, and a `validation-failed` naming `title` marks the title field;
  - an archived forum held in `/forums` shows its notice before typing and leaves Post enabled, for a member and for an admin;
  - an admin's draft saves and Post both succeed in an archived forum, and the 201 routes to the thread;
  - a `conflict` from the create route disables Post for a member;
  - a `forbidden` or `conflict` draft save, mocked, turns autosave off, shows "Draft not saved", and leaves Post enabled;
  - success routes to the thread;
  - the title input and the textarea are labelled.
- `shell/Shell.test.tsx`: `composing` hides the tab bar at 390 px, and `primary` is drawn as a button in the phone top bar, disabled when it says so.
- `web/client/e2e/new-thread.spec.ts` at 390 px and 1440 px: alice opens `/f/events/new` from the thread list, types a title and body, and waits for "Draft saved"; a second context from `stateFile('alice')` finds both; posting lands on `/t/{id}/{slug}` with post 1 shown, and `GET /api/v1/forums/events/draft` is 204. Axe reports no violations on the screen, and the page has no horizontal scroll. The spec deletes the draft before it starts.
- `npm run budget` passes.

### Out of scope

- The draft routes, the `title` column and `revision`: TKT-01M3K9XBJA.
- The autosave module: TKT-01M3NS1931.
- Tags on a new thread: the API refuses non-empty `tags` until tag rules exist.
- "Reply as new thread" and split, which open this composer with content: Phase 4 (docs/06 "Moderator flow for drift").
- A capability flag saying up front whether the reader may create a thread here: not filed; the refusal is shown when it comes.
- Slow mode in the reply composer: TKT-01M3R0PM0K.


### Revised at claim (2026-09-30, main at 6b5dc9a)

Everything above stands, `### Settled in grooming` and the refresh's items included. Built against main at 6b5dc9a, where TKT-01M3K9XBJA (#215) and TKT-01M3NS1931 (#227) have both landed. What the build does differently from the approach, each with the alternative that lost:
- **The route is eager, measured both ways.** Eager, the reader bundle is 52903 B gzip (+1689 B over 51214 B). Lazy, it is 55381 B: Vite splits the modules the screen shares with the thread page into a third chunk, and `bundle.mjs` counts every reader chunk. Lost: the lazy route, which costs 2478 B more.
- **`ComposerBody` takes `head`, `notice` and `foot` slots** and draws the section, the tabs in the head, the toolbar, the textarea and the preview. `Composer` puts the h2 and chip in `head`, the refusal in `notice` and its footer in `foot`, so its markup is unchanged and its tests pass unchanged. `focus` is split from `sheet`: the reply sheet takes focus when it opens, while the new-thread screen leaves focus on the heading the route focuser moves it to. The mention list's id comes from `useId`. Lost: a `mode` prop, as the grooming settled.
- **The forum comes from `useResource('/api/v1/forums')`,** which shows a held tree at once and revalidates it, rather than from `peekGet` alone. A forum absent from the viewer's tree is `NotFound`. Lost: reading `not-found` from the draft load, because `load` in `state/drafts.ts` turns a refused load into autosave off and does not return it. Changing that would change the shared engine for one screen.
- **`drafts.ts` gains `draftStatus(key)`**, a read of the status it already keeps, so `newThreadNote` can say "Draft not saved" when a refused save turned autosave off. `draftNote` says nothing then, and the reply composer keeps that. No rule of the engine changes.
- **The owner check is shared.** `composer.ts` exports `checkOwner()`, which `newThreadFor` calls before its own map's check, so whichever composer is asked for first after a viewer change resets the draft keys and neither resets the other's fresh ones.
- **On a phone, Discard is in the top bar's more menu** (`ShellContext.actions`) and the draft line is in the composer's head, since the sheet layout hides the footer. On a desktop both are in the footer.
- **The title field and the refusal are inside the composer card** (`notice`), above the toolbar, so the phone's column reads title, text, toolbar.
- **Creating the thread lands on `threadHref` (`/t/{id}/{slug}?page=1`)** and forgets the held forum tree and the forum's list pages, so going back shows the new thread at once.
- **The e2e spec has two tests per width**, the draft-and-post flow and one axe scan of the screen, after TKT-01M3MTQG5A's one scan per test. Both start from alice's stored session.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T09:01:39Z

Plan refreshed against main at 5124209 on 2026-09-30. Changed: TKT-01M3N24RHD is done and forumForThread admits an admin in an archived forum, so an admin's forum-draft saves succeed there; the rule that a draft-save refusal never disables Post stands and is now tested with a mocked answer, and the admin case asserts that draft saves and Post both succeed (criterion 9, replacing the old 8). The phone top bar draws no primary action today (ShellAction is {id, label, run}; PhoneTopBar puts actions in MoreMenu), so ShellContext gains primary beside composing (criterion 8, replacing the old 5). The round-5 requirements on state/drafts.ts from TKT-01M3NS1931's note are inherited: the local copy is ordered by the server revision and creating the thread removes it (new criterion 10). Settled as engineering choices, with the alternatives that lost in the plan: PostButton stays outside ComposerBody so TKT-01M3R0PM0K's slow prop stays on the reply footer and usePreview stays exported for TKT-01M3PTT97X; the e2e spec starts threads in events rather than general, whose first page thread-list.spec.ts reads (criterion 7, replacing the old 3); fieldErrors marks the named field. Corrected: routes.tsx:119, the entry points (ThreadListScreen.tsx:71, ForumPicker, ForumIndex 'Start one'), which already hide archived forums; dto.ts's ForumDraft now comes from TKT-01M3K9XBJA, a dependency, so that step is gone; the budget reference is 47415 of 60000 B. The Settled items are carried verbatim; the refresh adds its own items after them.

**agent:claude-code/phase2-groom** at 2026-09-30T09:23:50Z

Follows Terva review 1559 on PR #210: the forum draft's local copy is decided by the same four cases as a reply draft's, 204s included, reading Draft-Revision on a 204 (TKT-01M3K9XBJA, TKT-01M3NS1931). The plan's refresh item and step 2 say so; criterion 10 is unchanged.

**agent:claude-code/phase2-run-ns16vh** at 2026-09-30T13:28:07Z

Built on a local branch against main at 6b5dc9a, and held unpushed while the client dist belongs to another lane. The plan's "Revised at claim" subsection says what the build does beyond the approach.

Checks run here: gofmt, go build, go vet, go test -race ./... all pass; tsc --noEmit and vitest (1699 tests) pass; the full Playwright suite passes on port 8796 (103 passed, 11 skipped by design), new-thread.spec.ts included at both widths.

Bundle: eager 52903 B gzip of 60000 B, up 1689 B from 51214 B. Lazy measured at 55381 B, since Vite splits a shared chunk off and bundle.mjs counts every reader chunk, so the route stays eager.

One unit test, ApplicationDetail.test.tsx 'shows the answers through the form renderer, against the version they answered', failed twice in six full vitest runs here, at about 1.4 s, and passes alone and in the other runs. It is the lazy admin chunk's transform under a parallel run, which this change does not touch. Recorded here in case CI shows it.
