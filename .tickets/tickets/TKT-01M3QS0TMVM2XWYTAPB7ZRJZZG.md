---
schema: 3
id: TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
title: Link to the link-a-sign-in page from the client
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: TKT-01M29CKRES18SZSCQRV8XSNBT2
dependencies:
  - TKT-01M29CKRES18SZSCQRV8XSNBT2
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase2-run-qs0tmv
  branch: run/qs0tmv-link-sign-in
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-a5586a3f3215d1594
  commit: 13709f0d74ab93dbdc040da9e4bd218ed6f0a324
  session: null
  claimed_at: 2026-09-30T18:43:10Z
  expires_at: null
archive: null
created_at: 2026-09-29T23:46:54Z
updated_at: 2026-09-30T18:45:00Z
created_by:
  id: agent:claude-code/phase2-run-KRES
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qs0tmv
  name: ""
extensions: {}
---

## Description

Account linking (TKT-01M29CKRES) landed server-side: `GET /auth/link` is the "link a sign-in" page, and its forms post to `POST /auth/{provider}/link` (docs/05 "Login flow" step 3, docs/07 "URL scheme"). docs/05 says the action lives "in settings", but the client has no settings screen until TKT-01M29CXK4 (Build the user settings screens, phase 4), so a member can reach the page today only by typing its address.

This ticket adds a way to reach it from the client before then: a "Link a sign-in" entry in the signed-in member's account menu (or wherever the shell keeps account actions), as a full page load to `/auth/link?return_to=<current path>`, since `/auth/*` is outside the client's routes. It is hidden for anyone who is not an active member, and for a pre-user.

### Details

- The server page already sends a signed-out reader to `/auth/signin` and back, so the client needs no state of its own.
- When TKT-01M29CXK4 builds `/settings/identities`, its "add" button is the same link, and this entry can move there.
- The committed client dist changes, so only one lane may hold it at a time (docs/13 "Running two lanes").

## Acceptance criteria

- [ ] The entry is absent for a signed-out reader and for a pre-user (user null, pre_user set) (LinkSignIn or Shell test)
- [ ] The entry is a plain anchor to linkSignInHref(url) = /auth/link?return_to= plus the encoded current path and query, and /auth/link is outside clientScope (nav and routes.test.tsx)
- [ ] The dist from go generate ./internal/web/spa is committed and the thread-view bundle stays under threadViewGzipBytes in budget.json
- [ ] At 390 px and 1440 px a signed-in member presses their handle or the Me tab, then "Link a sign-in" in the Account block of their own profile page, and lands on the server's /auth/link page; another member's profile has no entry (e2e/link-sign-in.spec.ts)

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. The server half is merged: TKT-01M29CKRES (Implement account linking), PR #178, which built `GET /auth/link` (`internal/web/auth/link.go`, `linkPage`). Refreshed against main at 5124209 on 2026-09-30. Revised at claim on 2026-09-30 against main at 13709f0, by agent:claude-code/phase2-run-qs0tmv: `/u/:handle` is still `placeholder('profile', 'Profile')`, `linkPage` and its template are as the plan says, and the only change since the refresh that touches this plan is the `/auth/*` budget below. The budget is now 70000 B (`budget.json`).

### Settled in grooming

- **Linking happens only through the explicit action** (maintainer decision 2026-09-29, docs/05 "Login flow" step 3, TKT-01M29CKRES). An identity nobody has, signed in from a browser that is already a member's, is attached only when the sign-in began from "link a sign-in". Otherwise it is refused. This ticket only makes that action reachable; it changes nothing about when a link happens.
- **The page is the server's** (docs/07 "URL scheme"). `GET /auth/link` lists each enabled provider as a one-button form posting to `/auth/{key}/link` with the page's `return_to`. Anyone who is not an active member signed in with a session gets 303 to `/auth/signin`, with `return_to` bringing them back. The page is `no-store`. `return_to` passes `auth.SafeReturnPath`, so only a path on this site survives. The client needs no state of its own and no new route.
- **`/auth/*` is outside the client's routes** (`clientScope` in `routes.tsx`, held by `routes.test.tsx`), so a plain anchor to `/auth/link` is a full page load that reaches the server. This is how `meHref` in `shell/nav.ts` sends a signed-out reader to `/auth/signin?return_to=…` (TKT-01M3MFS6KM).
- **A link issues no session** (`internal/web/auth/link.go`). The identity is used later to sign in through the callback, and so through `session.IssueUser`'s status check (`internal/service/identity/login.go:170`; the comment at `link.go:24-26` says the same).
- **The entry goes on the member's own profile page** (maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181). There is no account menu: the desktop top bar's handle and the phone's Me tab both go to `/u/{handle}`, which is a placeholder (`placeholder('profile', 'Profile')` in `routes.tsx`) that no ticket builds. That page, for the viewer's own handle, gets "Link a sign-in". It is one press from a control every screen has, and neither the shell nor docs/06 changes. Two options lost: an account menu on the handle and the Me tab, which would change docs/06 "Mobile", and a minimal `/settings/identities`, which nothing leads to. TKT-01M3QVWKAG (Give signed-in members a Sign out control) puts Sign out beside it on the same page.
- **The action's final home is settings** (docs/05 says "in settings"). `/settings/identities` is a placeholder route until TKT-01M29CXK4 (Build the user settings screens, phase-4), whose "add" button is this same link. This ticket's entry moves there then.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes, changed", `docs/13-phase2-run.md:104`).

Engineering choices made here, each with the alternative that lost:
- **One helper builds the address.** `linkSignInHref(url)` in `shell/nav.ts`, beside `meHref`, answers `/auth/link?return_to=${encodeURIComponent(url)}`. `url` is preact-iso's `useLocation().url`, the path and query the reader is at. The entry is on the profile page, so that is `/u/{handle}`: after linking, the member comes back to the profile they left from. Carrying the page they were on before the profile (a thread, say) through the profile navigation lost (Terva review 1516 on PR #181, finding 1). It would need state threaded through the handle link and the Me tab, and the member chose to go to their profile. A second hand-built string where the entry is drawn lost, because the two would drift. The fragment is not sent, as `meHref` does not send it: the server would carry it only as part of `return_to`, and `useLocation().url` does not include it.
- **The entry is shown only for `viewer.value.user !== null`.** Only an active member's session gives a user (the chokepoint refuses any other status). A pre-user has `user: null` and is drawn by `ApplicantShell`, which has no forum navigation, and a signed-out reader has `user: null`. No new server field is needed. A check of the account's status in the client lost, because `/me` does not answer it and the session already implies it.

- **Handles are compared ASCII-case-insensitively** (settled in the refresh of 2026-09-30). Handles are stored `COLLATE NOCASE`, and `NormalizeHandle` (`internal/service/identity/handle.go:57`) folds ASCII only, avoiding `strings.ToLower` because of the Kelvin sign. The profile page folds both handles with `s.replace(/[A-Z]/g, (c) => c.toLowerCase())`. `toLowerCase()` on the whole string lost, because it folds more than the server does.
- **An agent is not a browser viewer.** `/me` for an agent has a non-null `user` with `kind: "agent"` (`internal/dto/testdata/golden/me.agent.json`), but an agent authenticates only by bearer credential (`agentcred.Middleware`), which a browser does not send, and `/auth/link` refuses an agent (`linker`, `link.go:35-41`). So `user !== null` stays the only check. A `kind` check in the client lost as a rule the server already enforces.
- **The e2e specs wait out the `/auth/*` limiter** (settled in the refresh of 2026-09-30). `/auth/*` allows a burst of 10 per address, refilling one every 6 s (`authRate`, `internal/web/ratelimit.go:34`). `session.setup.ts` spends 8 of them, and each `GET /auth/link` spends one more. `phase2-exit.spec.ts:49-67` already waits out a 429 as its `Retry-After` says, in a local `through()` helper. This ticket moves `through()` into `e2e/session.ts` as an exported helper, unchanged, and `phase2-exit.spec.ts` imports it from there. TKT-01M3QVWKAG uses it too. A second copy in the new spec lost, because the two would drift. Raising the limit for e2e lost, because the limiter is under test like the rest.


### Revised at claim, 2026-09-30

- **The e2e spec paces itself with `paceAuth`; `through()` stays where it is.** This supersedes the "moves `through()` into `e2e/session.ts`" sentence of the last settled item above, and keeps its decision that the limiter is not raised for e2e. After the refresh, TKT-01M3S1TVPD (PR #233) gave the run an `/auth/*` budget in `e2e/session.ts`: a spec that spends `/auth/*` calls `paceAuth(n)` with its count at the start of the test, before any request, and adds its row to the table above `stateFile`. The orchestrator's instructions for this ticket say the same, and add: do not change the shared `signIn` helper, and do not retry around a 429. So `through()` stays local to `phase2-exit.spec.ts`, and `link-sign-in.spec.ts` starts from alice's stored session (`stateFile('alice')`) and calls `paceAuth(1)`, since each project spends one `GET /auth/link`. TKT-01M3QVWKAG paces itself the same way.
- **The phone flow starts on the forum index, not a thread page.** A phone thread page has no tab bar (it shows the reply bar; `smoke.spec.ts`), so the Me tab is pressed from `/`. At 1440 px the top bar's handle is pressed from the same page. Both are `[data-nav=me]`.
- **The handle is compared as preact-iso gives it.** `useRoute().params.handle` is already percent-decoded (`exec` in `preact-iso/src/router.js`), and `viewer.value.user.handle` is the stored form, so both are folded with the ASCII fold settled above and compared.

### Approach

1. **`shell/nav.ts`**: `export function linkSignInHref(url: string): string` answers `/auth/link?return_to=${encodeURIComponent(url)}`, with a comment naming TKT-01M29CKRES and the move to `/settings/identities` with TKT-01M29CXK4. Beside it, `sameHandle(a, b)`, the ASCII-only fold.
2. **`shell/LinkSignIn.tsx`**: `LinkSignIn()` returns `null` unless `viewer.value.user`. Otherwise a `<p>` with "Add another way to sign in to this account." and `<a class="hit" href={linkSignInHref(useLocation().url)} data-nav="link-sign-in">Link a sign-in</a>`. A plain anchor with no `onClick`; `/auth/*` is outside `clientScope`, so it is a full page load.
3. **`screens/ProfilePlaceholder.tsx`** serves `/u/:handle` in `routes.tsx` in place of `placeholder('profile', 'Profile')`. It renders what `Placeholder` renders, keeping `<main class="scaffold" data-screen="profile">`. When the viewer is a member and `sameHandle(params.handle, viewer.value.user.handle)`, it adds `<section data-account aria-labelledby=…>` with an `<h2>Account</h2>` holding `<LinkSignIn />`. TKT-01M3QVWKAG adds Sign out to the same section. It is in the reader bundle, and small.
4. **CSS**: none unless the page needs it; the anchor takes `.hit` (`styles/base.css`), which pads it to `var(--target)` on the phone.
5. **e2e**: new `e2e/link-sign-in.spec.ts`, and its row in the budget table in `e2e/session.ts`.
6. `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and record the reader bundle's figure against 70000 B (54924 B before) in a note.

### Tests

- **`shell/LinkSignIn.test.tsx`**, with viewers from the `/me` goldens:
  - `builds the link address`: `linkSignInHref('/unread?x=1&y=a%20b')` is `'/auth/link?return_to=' + encodeURIComponent('/unread?x=1&y=a%20b')`.
  - `sameHandle` folds ASCII only: `Alice`/`alice` match; `Ken`/`ken` (Kelvin sign) do not.
  - `a member sees the entry on their own profile`: on `/u/alice` and `/u/ALICE`, the anchor's `href` is `linkSignInHref` of that address, inside `[data-account]`. On `/u/carol` there is no entry and no Account block.
  - `nobody else does`: absent for a signed-out viewer and for a pre-user (`user: null`, `pre_user` set) on `/u/alice`.
- **`routes.test.tsx`**: `/auth/link` and `/auth/link?return_to=%2F` out of `clientScope`, in the `clientScope %s` table; `/u/alice` still renders `profile`.
- **e2e `link-sign-in.spec.ts`**, both projects, alice's stored session, `paceAuth(1)` first: from `/`, press `[data-nav=me]`, land on `/u/alice`, press "Link a sign-in", land on the server's `/auth/link` with `return_to=/u/alice`, whose page offers "Link Fake". Then `/u/carol` has no entry, and a signed-out context on `/u/alice` has none. Neither of those spends `/auth/*`.
- **Budget checks**: the full suite, and the worst case, the `/auth/*` spenders alone back to back: `npx playwright test e2e/apply.spec.ts e2e/phase2-exit.spec.ts e2e/sign-in.spec.ts e2e/smoke.spec.ts e2e/link-sign-in.spec.ts`.
- **Invariants held by the server's tests, which stay green:** `TestLinkRefusesAnInactiveMember`, `TestLinkNeedsTheMembersSession`, `TestTheLinkStartNeedsAMember`, `TestNoLinkWithoutIntent`, and `TestOneDoorIntoSessions`. This ticket adds no server code, no session path and no cached DTO.

### Out of scope

- Listing and unlinking identities, and the settings screens: TKT-01M29CXK4.
- Any change to `/auth/link`, `/auth/{provider}/link` or the callback: TKT-01M29CKRES, done.
- The Sign out control on the same page: TKT-01M3QVWKAG (Give signed-in members a Sign out control), which depends on this ticket.
- A real profile screen. This ticket keeps the placeholder and adds only the Account block.
- Any change to `signIn`, `through()` or the limiter.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T00:29:01Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, against main at 43a844d. Open decision, not taken here: where the entry goes. The shell has no account menu: the desktop handle and the phone Me tab both go to /u/{handle}, a placeholder that no ticket builds. Options: (a) the member's own profile placeholder; (b) a new account menu on the handle and the Me tab, which changes docs/06 'Mobile'; (c) a minimal /settings/identities, which still needs (a) or (b) to be reached. The grooming recommends (a). Found while planning: the member's shell has no Sign out control; only ApplicantShell has one. Option (b) would host it. It is reported, not filed here.

**agent:claude-code/phase2-groom** at 2026-09-30T00:37:39Z

Maintainer decision 2026-09-29, relayed by the Phase 2 coordinator on PR #181. It supersedes the open decision in the previous note: the 'Link a sign-in' entry goes on the placeholder profile page, in an Account block shown only on the viewer's own profile (screens/ProfilePlaceholder.tsx). The open decision is removed from the plan, and the placement criterion is rewritten. The missing Sign out control is now TKT-01M3QVWKAG (Give signed-in members a Sign out control), which depends on this ticket and adds its form to the same block.

**agent:claude-code/phase2-groom** at 2026-09-30T00:52:13Z

Terva review 1516 on PR #181 (head 9fc6307), finding 1 (medium), accepted. The entry is on the profile page, so return_to is the profile's own path, /u/{handle}, and not the thread the member came from. Carrying the earlier page through the profile navigation lost: the member chose to go to their profile. The plan and the e2e expectation are updated.

**agent:claude-code/phase2-groom** at 2026-09-30T08:53:28Z

Plan refreshed against main at 5124209 on 2026-09-30. Nothing it builds has landed, and /u/:handle is still the placeholder (routes.tsx:129); PR #186's handle redirects are server-side (api.ProfileRedirects) and do not touch the client route. Corrected in the Settled items, with their decisions unchanged: a linked identity signs in through session.IssueUser's status check, not IssueUserWith (login.go:170); the docs/13 heading is now 'Running two lanes, changed'. Corrected elsewhere: the routes.test.tsx table is the clientScope one at :203-217, which holds //ketju.test/auth/logout; the e2e provider is the fake one; .hit lives in styles/base.css. Settled as engineering choices, with the alternatives that lost in the plan: the own-profile check folds ASCII only, as NormalizeHandle does; user !== null stays the only viewer check, since an agent never has a browser session and /auth/link refuses agents; and the e2e spec waits out the /auth/* limiter (burst 10, setup spends 8) through phase2-exit.spec.ts's through() helper, which this ticket moves into e2e/session.ts for TKT-01M3QVWKAG to share. Component tests build viewers from the /me goldens, since Me gained queue. Budget reference: 47415 of 60000 B. The criteria are unchanged and still checkable.
