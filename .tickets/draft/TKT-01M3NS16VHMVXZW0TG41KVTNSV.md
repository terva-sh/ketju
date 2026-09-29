---
schema: 3
id: TKT-01M3NS16VHMVXZW0TG41KVTNSV
title: Build the new-thread screen with draft autosave
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-29T05:08:37Z
updated_at: 2026-09-29T07:20:28Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] A member can create a thread from /f/{slug}/new at 390 px and 1440 px, and lands on it
- [ ] The title and body typed on one device are restored from the server draft on another, and the draft is deleted once the thread is created
- [ ] e2e/new-thread.spec.ts creates a thread and restores a title-and-body draft in a second context at 390 px and 1440 px, with axe reporting no violations, and npm run budget stays within 60000 B gzip
- [ ] The reply composer and the new-thread screen share composer/ComposerBody.tsx, and Composer.test.tsx and ComposerSheet.test.tsx pass unchanged
- [ ] forbidden, conflict (archived), validation-failed, payload-too-large and rate-limited from the draft save or the create are said in words, keep the typed text, and disable Post where retrying cannot succeed (NewThreadScreen.test.tsx)
- [ ] On a phone the screen hides the tab bar through ShellContext.composing and carries Post in the top bar (Shell.test.tsx)
- [ ] The dist is regenerated with go generate ./internal/web/spa and committed

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 9425310.

What exists there:
- `/f/:forum/new` is a placeholder (`web/client/src/routes.tsx:113`). `ThreadListScreen.tsx:72` and `ForumIndex.tsx` link to it.
- `POST /api/v1/forums/{slug}/threads` takes `{title, source, tags}` and answers 201 `dto.CreatedThread{thread, post}`; a hidden forum is `not-found`, no `create_thread` is `forbidden`, an archived forum is `conflict`, and the title must be 1–200 characters (`threads.MaxTitle`) with no control characters (docs/07).
- `POST /api/v1/render` renders a preview; without `thread_id`, quotes render optimistically and `reply_to` is refused (docs/07).
- `composer/Composer.tsx` is bound to a thread: `composerFor(thread)`, `usePreview(thread, …)` and `PostButton` post a reply. The toolbar (`edit.ts`), the mention list (`useMentions`) and the preview panel are inside it.
- `state/shell.ts`'s `ShellContext` gives the phone top bar its title, back link and actions, and `reply` swaps the tab bar for the reply bar.
- `/forums` (`dto.Forum.archived`) is usually held by `api/client.ts` from the forum index, readable with `peekGet`.

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
- **A refusal is shown when the API gives it.** There is no `can_create_thread` flag to read beforehand. An archived forum is known up front when `/forums` is held (`peekGet`), and the screen says so before anything is typed. Otherwise the first draft save or the Post answers `forbidden` or `conflict`, the screen shows it, turns autosave off and disables Post, and keeps the text so it can be copied. Filing a capability flag on the thread list lost for now: it changes a cached projection answer to save one refusal.
- **The client deletes the draft after creating the thread**, as for replies (docs/07 "Drafts": `DELETE` is safe to retry). Lost: deleting it inside the create transaction, which changes TKT-01M29CA9B9's create path for no gain the retry does not give.

### Approach

1. `composer/ComposerBody.tsx`: move the tabs, toolbar, textarea, mention list and preview panel out of `Composer.tsx`. Props: `state` (`source`, `caret`, `summoned`, `posting`), `thread?`, `replyTo?`, `label` ("Reply text" or "Thread text"), `sheet`. `usePreview` sends `thread_id` and `reply_to` only when a thread is given. The mention list's id comes from a `useId`-style counter rather than the thread id. `Composer.tsx` uses it, and its tests pass unchanged.
2. `state/newThread.ts`: `newThreadFor(forum)` with `title`, `source`, `caret`, `summoned`, `posting`, `failure`, and the same owner rule as `composerFor`. It starts `watch({kind: 'new', forum}, () => ({title, source}))`. `loadNewThreadDraft(forum)` fills an empty form from `apiGetOptional<ForumDraft>`. `createThread(forum)`: one at a time, `POST /api/v1/forums/{slug}/threads` with `{title, source}`, then `drop` the draft, clear the state, and answer the `CreatedThread`.
3. `screens/NewThreadScreen.tsx` for `/f/:forum/new`:
   - A signed-out reader gets the sign-in prompt the other screens use; a pre-user is sent away by TKT-01M29CKRG9's gate.
   - The forum's name from `peekGet('/api/v1/forums')`, or from the thread list's `forum`; `not-found` from the draft load shows "No such forum".
   - A labelled title input (`maxlength` 200), `ComposerBody`, and a footer with the draft status line, Discard (confirm when there is text; it drops the draft), and "Post thread", off while the title or body is blank or a post is in flight.
   - Refusals in words: `forbidden` "You may not start threads in this forum."; `conflict` "This forum is archived."; `validation-failed` "The title must be 1 to 200 characters with no control characters, and the post cannot be empty."; `payload-too-large`; `rate-limited` with `Retry-After`.
   - On success, `route('/t/{id}/{slug}')`, where the thread page shows post 1.
   - `useShellContext({title: 'New thread', back: {href: '/f/{slug}', label: forum name}, actions: [], composing: true})`.
4. `state/shell.ts` and `shell/Shell.tsx`: `composing` hides the tab bar, and on a phone the top bar carries the screen's Post action.
5. `routes.tsx`: the route renders `NewThreadScreen`. `routes.test.tsx` still diffs the table against docs/07.
6. `dto.ts`: `ForumDraft` if TKT-01M3K9XBJA has not added it.
7. Build: `npm run build`, `go generate ./internal/web/spa`, commit the dist, and record `npm run budget`'s figure in a note.

### Tests

- `composer/ComposerBody.test.tsx`: the preview sends `thread_id` only when given. `Composer.test.tsx` and `ComposerSheet.test.tsx` pass unchanged.
- `state/newThread.test.ts`: a loaded draft fills an empty form with title and body and not a typed one; `createThread` posts once on a double click, drops the draft, and clears the form; a refusal keeps the text.
- `screens/NewThreadScreen.test.tsx`: Post is off with a blank title or body; each refusal's words; an archived forum held in `/forums` shows its notice before typing and disables Post; a `forbidden` draft save turns autosave off and disables Post; success routes to the thread; the title input and the textarea are labelled.
- `shell/Shell.test.tsx`: `composing` hides the tab bar at 390 px.
- `e2e/new-thread.spec.ts` at 390 px and 1440 px: alice opens `/f/general/new` from the thread list, types a title and body, waits for "Draft saved"; a second context from `stateFile('alice')` finds both; posting lands on `/t/{id}/{slug}` with post 1 shown, and `GET /api/v1/forums/general/draft` is 204. Axe reports no violations on the screen, and the page has no horizontal scroll. The spec deletes the draft before it starts.
- `npm run budget` passes.

### Out of scope

- The draft routes and the `title` column: TKT-01M3K9XBJA.
- The autosave module: TKT-01M3NS1931.
- Tags on a new thread: the API refuses non-empty `tags` until tag rules exist.
- "Reply as new thread" and split, which open this composer with content: Phase 4 (docs/06 "Moderator flow for drift").
- A capability flag saying up front whether the reader may create a thread here: not filed; the refusal is shown when it comes.
