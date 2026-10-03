---
schema: 3
id: TKT-01M3V6XVVGSTDA1SH15DKYP7NZ
title: Let a member edit their display name, bio and avatar
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:api
  - area:client
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:360
    path: null
  - ref: pr:400
    path: null
claim: null
archive: null
created_at: 2026-10-01T07:47:40Z
updated_at: 2026-10-03T10:51:13Z
created_by:
  id: agent:claude-code/phase3-groom-pages
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-6xvvg
  name: ""
extensions: {}
---

## Description

The profile shows `bio_html`, a display name and an avatar (docs/07 `GET /users/{handle}`), but no endpoint or screen lets a member set any of them. `users.bio_source` and `display_name` exist (docs/04). An avatar needs the blob store, TKT-01M29CXK0K.

Found while grooming TKT-01M3V3CFMA (Build the member profile screen and their post list). The profile renders whatever is stored, so every bio stays empty until this lands.

This needs a design line first. The open questions are the endpoint, the bio's length limit, whether a display name passes the handle's character rules, and whether a change is audited.

## Acceptance criteria

- [x] GET and PATCH /settings/profile follow docs/05 Display name and bio: 100 characters for the display name and 2000 for the bio, counted as characters; wrong_type, too_long and invalid_text refusals write nothing; an agent by credential and a pre-user are forbidden and anonymous unauthenticated (TestUpdateProfileValidates, TestSettingsRoutesRefuseAgentsAndAnonymous; golden, dto.ts and docs/07 rows)
- [x] An agent's display name and a member's are checked by one function (TestDisplayNameRuleIsShared)
- [x] /settings/profile edits the display name and bio and sends only what changed; the profile then shows both, and the own profile's Account block links to Edit profile
- [x] A display-name change misses the response cache, and the next thread page carries the new name (TestDisplayNameChangeMissesTheResponseCache)
- [x] e2e/settings.spec.ts covers the screen at 390 px and 1440 px, with one axe scan per test, and puts carol's name and bio back after
- [x] The dist from go generate ./internal/web/spa is committed, npm run budget keeps the reader within 120000 B gzip, and the PR states the reader's growth

## Implementation plan

Groomed 2026-10-02 for the Phase 4 run (docs/15), against main at ed82f15.

### Settled in grooming

- **The design line the description asked for** is written. Decided by the run, pending maintainer review, in docs/05 "Display name and bio" (2026-10-02), with the alternatives that lost:
  - **Endpoint.** `GET /settings/profile` answers `{display_name, bio}`, the bio as its Markdown source. `PATCH /settings/profile` takes either key or both, and answers the same shape.
  - **The display name** follows the agent rule (`users.DisplayNameMaxLen`, `internal/service/users/agents.go`): trimmed, valid UTF-8, at most 100 characters, no control character. Empty is stored empty, and author blocks then show the handle. Display names need not be unique.
  - **The bio** is Markdown, at most 2000 characters, and is rendered when it is served, as `threads.Service.Profile` already does (`render.RenderWith`, with no posts to resolve quotes against).
  - **Who.** A member session only. Anonymous is `unauthenticated`, a pre-user `forbidden`, and an agent by its credential `forbidden`. Each `PATCH` is charged to the per-address write limiter.
  - **No audit row**, as `thread.renamed` is not written for an author's own rename (docs/04 "audit_log").
  - **The screen** is `/settings/profile` ("Profile"), first in the settings links.
- **The avatar is split out** to TKT-01M3YSQ4CR (Let a member set an avatar). Who may fetch an avatar's blob is a security question docs/07 "Attachments" does not answer, so that ticket holds it as an open decision. Splitting lets the display name and bio go ahead. The title of this ticket keeps "avatar", because docs/15's scope names it that way. The criteria below leave the avatar out, and this note and the plan say why.
- **Invariants this touches.**
  - The **response cache** key holds a digest of the page's authors' rows (`authorsDigest`, `internal/service/threads/cached.go`). It hashes `dto.Author` as JSON, so a display-name change misses the cache with no code change. A test pins it.
  - An anonymized row cannot change its display name or bio (`users_anonymized_is_final`, docs/04). Such an account is deactivated and has no session, so the route never reaches the trigger. The trigger stays the backstop.
  - The bio is no wider than today. `GET /users/{handle}` keeps its gates (a member session only), and a mention in a bio notifies nobody.

### Approach

1. **Server: query.** Add `UpdateUserProfile` to `internal/store/db/queries/identity.sql`, beside the users queries. It is `UPDATE users SET display_name = coalesce(?, display_name), bio_source = coalesce(?, bio_source), updated_at = ? WHERE id = ? RETURNING display_name, bio_source`. Regenerate with sqlc.
2. **Server: service.** In `internal/service/users`, a new `profile.go` with `Profile(ctx, v)` and `UpdateProfile(ctx, v, ProfilePatch)`.
   - `ProfilePatch` keeps each key as `json.RawMessage`, as `SettingsPatch` does, so a wrong type is a field error.
   - Codes, from `internal/forms`:
     - `wrong_type`: null or not a string;
     - `too_long`: a display name over 100 characters, or a bio over 2000;
     - `invalid_text`: a control character in the display name, or one other than tab, line feed and carriage return in the bio.
   - A refusal writes nothing. The display name is trimmed. The bio is stored as sent.
   - Share the display-name check with `CreateAgent`: move it into one function both call, so the two rules cannot drift.
   - Both methods need a member who is not an agent by credential. Use `ownSession`'s test (`internal/web/api/settings.go`), or the same check in the handler.
3. **Server: routes.** `GET` and `PATCH /api/v1/settings/profile` in `api.go`'s table, handled in `settings.go`. They answer `dto.ProfileSettings {display_name, bio}`, `no-store`. Add docs/07 rows, `internal/dto` and `api/dto.ts` types, and a golden.
4. **Client: `screens/account/ProfileSettingsScreen.tsx`** at `/settings/profile`, exported from `AccountRoot.tsx`, routed in `routes.tsx`, and first in `pages` in `settings.tsx`.
   - A form with **Display name**: an input with `maxlength=100` and the hint "Shown on your posts in place of your handle. Leave it empty to show your handle."
   - **Bio**: a textarea with the hint "Markdown, as in a post. At most 2000 characters.", and a character count in an `aria-live="polite"` region that speaks only near the limit.
   - **Save** sends only the keys that changed. On 200 it takes the answer, calls `refreshViewer()` so the top bar's avatar initial and `/me` follow, and shows "Saved." in a status region with a "View your profile" link.
   - A field error shows its words beside its field, and focus goes to it in a `useLayoutEffect`.
   - Leaving with unsaved edits needs no guard. Nothing else on the settings screens has one.
5. **Client: the own profile.** `AccountBlock` (`screens/account/profile.tsx`) gains "Edit profile", to `/settings/profile`, and its sentence names the profile.
6. **Build.** Run `go generate ./internal/web/spa` and `npm run budget`.

**Reader bundle.** The screen is in the reader's chunk, with the other settings screens (`AccountRoot.tsx`). Estimated at +1000 B gzip. Measure it and state it in the PR.

### Tests

- **Go (`internal/service/users`).**
  - `TestUpdateProfileValidates`: each code, for each field, at the boundary. 100 and 2000 characters pass, and 101 and 2001 do not, counted in characters, not bytes (a test with multi-byte text). A refusal writes nothing.
  - `TestUpdateProfileKeepsTheOtherField`: a display-name write keeps the bio, and the other way round.
  - `TestDisplayNameRuleIsShared`: an agent's creation and a member's update refuse the same inputs.
- **Go (`internal/web/api`).**
  - `TestSettingsRoutesRefuseAgentsAndAnonymous` gains both routes in its `ownOnly` list: an agent is `forbidden`, anonymous `unauthenticated`, and a pre-user `forbidden`, each before the body is read, writing nothing.
  - `TestProfileSettingsThroughTheAPI`: a `PATCH` answers the stored shape. `GET /users/{handle}` then serves the new display name in `user` and the rendered bio in `bio_html`. A mention in the bio writes no notification row.
  - `TestDisplayNameChangeMissesTheResponseCache`: a thread page served from the cache to a member, then a display-name change by one of its authors, then the next request misses and carries the new name. This is the **response-cache key** invariant.
  - Golden: `ProfileSettings`.
- **Vitest** (`screens/account/ProfileSettingsScreen.test.tsx`, through `renderAt('/settings/profile')`):
  - "fills from GET and sends only the changed keys".
  - "a refused field shows its words and takes focus".
  - "the count speaks near the limit".
  - "Saved refreshes the viewer".
  - `ProfileScreen.test.tsx`: the own profile's Account block links to Edit profile.
  - `routes.test.tsx` gains `['/settings/profile', 'settings-profile']`.
- **e2e** (`e2e/settings.spec.ts`, in the carol `preferences` describe, or a new describe on her stored session):
  - At 390 px and 1440 px, carol sets a display name and a bio with a link and a mention. Her profile then shows both, and a thread page she posted in shows the new name.
  - `afterEach` puts back "Carol Ruiz" and an empty bio, as the preferences test puts back her settings.
  - One axe scan per test. No `/auth/*` call, so no row in `e2e/session.ts`.

### Out of scope

- The avatar: TKT-01M3YSQ4CR (Let a member set an avatar), filed in this grooming with its open decision.
- Letting staff reset a member's display name or bio: TKT-01M3YT04ST (Let staff reset a member's display name and bio), filed in this grooming as a draft with no milestone, because no Phase 4 deliverable needs it.
- A bio preview. The member sees the bio on their profile, one link away.

## Notes

**agent:claude-code/phase4-groom-client-account** at 2026-10-02T17:21:26Z

run decision: pending maintainer review. A member edits their display name and bio on /settings/profile through GET and PATCH /settings/profile. The display name follows the agent rule (100 characters, no control character, empty allowed). The bio is Markdown of at most 2000 characters. It is a member session only, with no audit row. Recorded with the alternatives in docs/05 'Display name and bio' (2026-10-02). The avatar is split out to TKT-01M3YSQ4CR (Let a member set an avatar), because who may fetch an avatar's blob is a class 1 question docs/07 'Attachments' does not answer. The title keeps 'avatar' because docs/15's scope names this ticket that way. Staff reset of a display name or bio is filed as TKT-01M3YT04ST (Let staff reset a member's display name and bio), with no milestone.

**agent:claude-code/phase4-run-6xvvg** at 2026-10-03T04:06:45Z

Implemented (Phase A, before the dist), as planned, with these readings:

- `users.Service.Profile` and `UpdateProfile` (`internal/service/users/profile.go`) serve the routes through the service, and the handlers in `internal/web/api/settings.go` keep to the layering test. The handlers refuse an agent by credential and a stranger with `ownSession` before the body is read, as the session and identity routes do. The write is one statement, `UpdateUserProfile` in `queries/identity.sql`, which keeps a field sent as null.
- **One display-name rule.** `users.CheckDisplayName` answers the trimmed name or a forms code (`too_long`, `invalid_text`), and `CreateAgent` now calls it. Its refusals keep their validation-failed shape with no field list, as before. TestDisplayNameRuleIsShared holds the two callers together.
- **The bio** rejects any control character but tab, LF and CR (`invalid_text`), and counts characters as runes. JSON decoding already replaces invalid UTF-8 with U+FFFD, so the UTF-8 check guards other callers only, and the test has no case for it.
- **A mention in a bio now links.** docs/05 "Display name and bio" says "A mention in it links and notifies nobody". `threads.Service.Profile` rendered the bio with no handle resolver, so mentions were plain text. It now resolves them with the rule a post's mentions follow (`handleMemo.resolveNow`), quotes still pointing nowhere (`noPosts`). Nothing notifies: only a post's create path notifies, and TestProfileSettingsThroughTheAPI pins that no notification row is written. docs/07's `GET /users/{handle}` row says so.
- **The response-cache invariant** is pinned by TestDisplayNameChangeMissesTheResponseCache (respcache_test.go). The change goes through `users.Service.UpdateProfile` directly, because the thread world has no session store. The next page is a fresh draw (Inserts+1, no hit) carrying the new name.
- **Client.** `screens/account/ProfileSettingsScreen.tsx` sits at `/settings/profile`, first in the settings links. Save sends only changed keys. The name is compared trimmed, so whitespace alone is not a change. A bio over 2000 characters is refused locally, without sending. The count region (`aria-live="polite"`) is empty until 200 characters are left. `refreshViewer` runs only when the name changed. The textarea's class is `settings-bio`, because `.profile-bio` is the profile page's rendered bio. The own profile's Account block gains "Edit profile" above "Settings".
- **e2e:** a `profile` describe in `e2e/settings.spec.ts` on carol's stored session, with `afterEach` putting back "Carol Ruiz" and an empty bio. It passed at 390 px and 1440 px locally.
- **Reader bundle:** 81492 B on main at 88b12b3, 82499 B with this change (+1007 B; the plan estimated +1000).
- **Local runs:** `go test -race -p 4 ./...` timed out once in `cmd/ketju` at the default 10 minutes under a load average of 21 to 26. Run alone it passed in 331 s. Every other package passed.

## Summary

A member edits their own display name and bio on /settings/profile, as docs/05 "Display name and bio" describes.

### Server
- `GET` and `PATCH /settings/profile` call `users.Service.Profile` and `UpdateProfile`.
- The display name follows the rule `CheckDisplayName` holds, which agents share.
- The bio is Markdown, at most 2000 characters. Its mentions link but notify nobody.
- Both routes are for a member session only: an agent by its credential and a pre-user get `forbidden`, and an anonymous caller gets `unauthenticated`.
- A display-name change misses the response cache.
- The DTO is `dto.ProfileSettings`, with a golden.
- Served goldens cover `GET` and `PATCH /settings/profile`, as the #390 guard requires.

### Client
- `ProfileSettingsScreen` sits at /settings/profile and is first in the settings links.
- Save sends only the fields that changed, then fetches /me again.
- The Account block on a member's own profile links to it as "Edit profile".

### Built client
The reader is 91416 B gzip against main's 90416 B, so it grows by 1000 B. The budget is 120000 B.

### Tests
- `go test -race -p 4 ./internal/... ./tools/...` is green.
- `cmd/ketju` passes with `-race` when run on its own.
- vitest is green: 2810 tests.
- `e2e/settings.spec.ts` and `e2e/account.spec.ts` pass at 390 px and 1440 px: 14 tests.

The avatar is TKT-01M3YSQ4CR's.
