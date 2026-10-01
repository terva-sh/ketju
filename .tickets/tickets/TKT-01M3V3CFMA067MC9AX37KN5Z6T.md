---
schema: 3
id: TKT-01M3V3CFMA067MC9AX37KN5Z6T
title: Build the member profile screen and their post list
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29CCBYGNF7T7R9MFNHW5X60
blocks_on: none
references:
  - ref: design:profiles
    path: docs/07-api-and-clients.md
  - ref: design:whose-profile
    path: docs/05-identity-and-accounts.md
claim:
  actor: agent:claude-code/phase3-v3cfma
  branch: run/v3cfma-profile-screen
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a7484665c6fa856f2
  commit: 86752c422f9d6152ce015e2c1b52cdd721f33910
  session: null
  claimed_at: 2026-10-01T18:09:28Z
  expires_at: null
archive: null
created_at: 2026-10-01T06:45:45Z
updated_at: 2026-10-01T18:40:40Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-v3cfma
  name: ""
extensions: {}
---

## Description

`/u/{handle}` and `/u/{handle}/posts` route to placeholders. TKT-01M3QS0TMV (#245) put an Account block on the viewer's own profile, with "Link a sign-in", and TKT-01M3QVWKAG (#253) added Sign out there. The profile itself is still empty, although its endpoints are done (TKT-01M29CCBYG).

docs/07 defines both answers. `GET /users/{handle}` returns `{user, bio_html, joined_at, post_count, status}`, where `status` reaches only moderators and admins. `GET /users/{handle}/posts?cursor=` returns 25 posts to a page, newest first, each rendered as its thread page renders it, with a link to its thread and forum. Who may see a profile follows docs/05 "Whose profile is served" (maintainer decision 2026-09-28, TKT-01M3KJJNDC). A renamed handle redirects (docs/07, TKT-01M3NRP3W9).

Found when the maintainer used the dogfood instance on 2026-10-01 and asked how far the UI was from working. The Phase 3 run filed it under docs/14.

## Acceptance criteria

- [x] The posts list at /u/{handle}/posts pages newest first by the API's cursor and links each post to its permalink and forum
- [x] The viewer's own Account block keeps Link a sign-in and Sign out
- [x] The profile at /u/{handle} shows the member's handle, role badge, bio, join date and post count, and shows status only when the API sends it (moderators and admins); e2e: a moderator sees a suspended member's status and a member does not
- [x] A not-found profile (unknown, or pending to a member) shows not found, a signed-out reader is offered sign in, and a renamed handle's address is replaced with the current one
- [x] Author names in post headers link to the author's profile, with the phone's 44 px hit area
- [x] Tested at 390 and 1440 px with one axe scan per test
- [ ] The committed dist is regenerated (go generate ./internal/web/spa) and npm run budget stays within 70000 B, with the before and after figures in the PR, the account chunk measured lazy and inlined

## Implementation plan

### Settled in grooming

- The endpoints are done and decide everything about access (TKT-01M29CCBYG, docs/07 `GET /users/{handle}` and `/users/{handle}/posts`):
  - A pending account is `not-found` except to moderators and admins.
  - A suspended or deactivated account is served to members, and `status` is null except to moderators and admins.
  - Posts in forums the viewer cannot read are left out, and deleted or hidden posts are left out for other members.
  - The author sees their own removed posts marked, and moderators see them marked with the body.

  This follows docs/05 "Whose profile is served" (maintainer decision 2026-09-28, TKT-01M3KJJNDC). The client draws what the API sends. It never decides visibility, never shows a status the API left null, and never shows a body the API left out.
- A member session is required for both endpoints. A signed-out reader gets `unauthenticated` and is offered sign in, through `ErrorPanel`'s existing handling. A pre-user never reaches `/u/*`, because `ApplicantShell` takes every address but `/apply`.
- A renamed handle redirects (docs/05 "Changing a handle", TKT-01M3NRP3W9). A full load is redirected by the server before the shell (`api.ProfileRedirects`). A client-side navigation's `fetch` follows the API's 302, so the screen sees `user.handle` differ from the address and replaces the address with the current handle (`route(..., true)`), keeping `/posts` and the query.
- The post list pages by the API's cursor carried in the address (`/u/{handle}/posts?cursor=`), with an "Older posts" link and a "Newest" link back. That is how `ThreadListScreen` pages a cursor list, so a page of history can be linked and the browser restores it.
- Times use `PostTime` (docs/06, decided 2026-10-01). Joined is a date.
- Author names in post headers become links to `/u/{handle}`. docs/06 "Targets and gestures" lists author names among the inline links, and every mockup draws the name as a link. This is what makes a profile reachable from a thread. They take `.hit` on the phone.
- The own profile's Account block stays: "Link a sign-in" (#245) and "Sign out" (#253), decided 2026-09-29 (TKT-01M3QS0TMV). The settings screens' ticket, TKT-01M29CXK4E, adds a "Settings" link to it.
- Bundle: the profile screens go in a new lazy chunk, `src/screens/account/AccountRoot.tsx`, loaded by `/u/*`. TKT-01M29CXK4E adds the settings screens to the same chunk. `scripts/bundle.mjs` counts every non-admin lazy chunk, so the chunk counts toward the reader budget all the same. It is lazy only so that a thread page's first load does not fetch it. Estimate: 1.6 to 2.2 KB gzip for JS and CSS, plus about 60 B in the reader for the author links. With the reader at 60465 B, it fits. Measure the profile both in the chunk and inlined, and keep whichever counts fewer bytes in total; docs/07 records that a split can add a shared chunk.

### Approach

1. Add `src/screens/account/AccountRoot.tsx`, exporting `ProfileScreen` and `ProfilePostsScreen`, the way `ApplyRoot` does. Route `/u/:handle` and `/u/:handle/posts` to them with `lazy()` in `routes.tsx`. Delete `ProfilePlaceholder.tsx` and move its Account block into `ProfileScreen`. Add a row to `testing.tsx`'s `lazyChunks`: `[/^\/u\//, () => import('./screens/account/AccountRoot')]`.
2. **`ProfileScreen`** fetches `GET /api/v1/users/{handle}` with `useResource`. It shows:
   - the avatar (`Avatar`), handle, display name and role badge (`RoleBadge`, which carries kind, so an agent is marked);
   - `bio_html` in a `.post-body`-styled block, since the server renders and sanitizes it as a post (`internal/render`), and nothing when empty;
   - "Joined" with the date, and the post count, with a link "Posts" to `/u/{handle}/posts`;
   - `status` as a badge ("Suspended", "Deactivated", "Pending"), drawn only when non-null and not `active`;
   - the Account block on the viewer's own profile (`sameHandle`).

   An anonymized account renders as the API sends it: placeholder handle, no display name, no bio. A `not-found` shows `NotFound`'s text inside the screen.
3. **`ProfilePostsScreen`** fetches `GET /api/v1/users/{handle}/posts?cursor=` and lists each `UserPost`:
   - the forum (link to `/f/{slug}`) and the thread title (link to the thread), above the post;
   - the post's ordinal as a link to its permalink (`permalink(thread, ordinal)`), with `PostTime`, and `Edited`;
   - the post body, or the existing `removal.tsx` rendering when `removal` is set (the author's own deleted post has no body; a moderator-hidden one does).

   It is read-only: no reply, quote, flag or expander, because a history is not a thread page. An empty first page says the member has no posts the viewer can see.
4. **Author links.** In `components/thread/Post.tsx`, wrap the `.author` span in `<a class="author hit" href={`/u/${encodeURIComponent(handle)}`}>`, in the header and wherever a post's author is drawn (`ReplyExpander` copies included). Keep the visual style. Check `thread.css` for `.author` selectors that assume a span.
5. **Styles** go in `screens/account/account.css`, imported by the chunk. Use the existing tokens only.
6. Run `go generate ./internal/web/spa` and commit the dist. Run `npm run budget` and quote the before and after numbers in the PR.

### Tests

Vitest (`screens/account/ProfileScreen.test.tsx`, `ProfilePostsScreen.test.tsx`, with `renderAt` and the lazy-chunk row):
- `shows handle, role badge, bio, joined date and post count`.
- `shows status only when the API sends it` (profile visibility): with `status: null` no status is drawn for a suspended account's profile; with `status: "suspended"` the badge is drawn. The client adds no rule of its own.
- `not-found shows the not-found text`: the pending-to-a-member case and an unknown handle.
- `unauthenticated offers sign in`.
- `replaces a renamed handle's address`: the response handle differs, so the URL becomes `/u/{new}` and the history entry is replaced.
- `own profile keeps Link a sign-in and Sign out`, and another member's profile has no Account block.
- `posts list links each post to its permalink and its forum`, and `Older posts carries next_cursor in the address`; there is no "Older" link at the end.
- `an author's own deleted post is marked without a body`, and `a moderator-hidden post is marked with its body`: rendering what the API sends.
- `post author names link to their profile` (`Post.test.tsx`).

e2e (`e2e/profile.spec.ts`, stored sessions, one axe scan per test, both projects):
- `a member opens another member's profile from a post's author link`, then the posts list, then a permalink, at 390 and 1440 px.
- `a moderator sees a suspended member's status and a member does not` (profile visibility): suspend a seeded member through the admin API as `ada`, open the profile as `mona` and then as `alice`, and unsuspend after. If the run's other specs need that member active, seed a dedicated one in `cmd/ketju/e2e.go`.
- `the own profile keeps the Account block`.

No new `/auth/*` spend, so no `paceAuth` row is needed.

### Out of scope

- Editing a display name, bio or avatar: TKT-01M3V6XVVG (Let a member edit their display name, bio and avatar).
- The settings link in the Account block, and the settings screens: TKT-01M29CXK4E.
- Profile visibility rules, which are server-side and done in TKT-01M29CCBYG.

## Notes

**agent:claude-code/phase3-v3cfma** at 2026-10-01T18:40:28Z

Implementation, prepared on run/v3cfma-profile-screen (2026-10-01). The dist is not committed yet: the orchestrator hands it out (docs/14 "Amended during the run").

### Where the plan's approach landed

- **Bundle: inlined, not lazy.** The plan asked for the profile to be measured both in its own chunk and inlined, keeping whichever counts fewer bytes. The lazy split counted 69818 B in all, because Rollup moved what the account screens share with the reader into a new shared chunk (13395 B, `shell-*.js`) beside a 1897 B JS and 876 B CSS account chunk. Inlined it counted 67636 B. So `routes.tsx` imports `screens/account/AccountRoot.tsx` directly, and `testing.tsx` has no lazy row for `/u/`. The reader went from 66189 B to 67509 B (+1320 B). docs/07 "Preact client" already records that a split adds a shared chunk, so no doc change. `AccountRoot.tsx` stays the module the settings screens (TKT-01M29CXK4E) join.
- **Reusable for settings.** `screens/account/account.css` holds the page column (`.account`), the card (`.account-card`), the heading (`.account-head`, `.account-up`) and the paging (`.account-pages`). `screens/account/profile.tsx` holds `AccountBlock` (Link a sign-in, Sign out), where TKT-01M29CXK4E adds its Settings link, and `useProfile`, `ProfileHead` and `Joined`.
- **The posts screen fetches the profile too.** It needs it for its heading ("Posts by {name}") and so that a renamed handle's address is replaced there as well, `/posts` and the query kept. A post's author handle would not do that for an empty history. The profile key does not change with the cursor, so paging does not fetch it again.
- **Author links.** `AuthorLink` in `components/thread/Post.tsx` is used by the post header, the reply expander's copies and the reply-to popover. On the phone `.post-header .ordinal + .author` takes 6 px more room, because the ordinal's 44 px target would otherwise run under the name. `e2e/targets.spec.ts` checks the thread page and now the profile and its posts.
- **The posts list** draws each post without an avatar or author (it is all theirs), and with no element ids, since two threads can share an ordinal. On the phone the forum and thread links are full 44 px targets, with 14 px below them for the ordinal's.
- **e2e status check.** As in `admin-suspension.spec.ts`, each project suspends an agent that ada makes without a token. A seeded human reader would have its stored session ended by the suspension, and other specs sign in as those readers. The server applies the status rule to every account, whatever its kind.

### Tests

- Vitest: `screens/account/ProfileScreen.test.tsx`, `ProfilePostsScreen.test.tsx`, and "the author link" in `components/thread/Post.test.tsx`. The full suite has 80 files and 2283 tests, all passing.
- Playwright: `e2e/profile.spec.ts` (three tests, one axe scan each) and `e2e/targets.spec.ts` "a profile and its posts", at 390 and 1440 px. The full suite, run locally against the built dist: 221 passed, 13 skipped, none failed.
- `go vet`, `gofmt` and `go test -race ./...` are clean.
- I compared screenshots of the profile, the posts list and a thread page, at both widths and in both themes, by eye. There is no profile mockup. The author name in a thread still looks as the mockups draw it.

Acceptance criterion 7 (the committed dist) stays unticked until the dist is handed to this lane.
