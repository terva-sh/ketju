---
schema: 3
id: TKT-01M3QS0TMVM2XWYTAPB7ZRJZZG
title: Link to the link-a-sign-in page from the client
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-29T23:46:54Z
updated_at: 2026-09-30T00:29:01Z
created_by:
  id: agent:claude-code/phase2-run-KRES
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
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

- [ ] At 390 px and 1440 px a signed-in member follows the "Link a sign-in" entry, placed where the open decision says, with one press from that place and lands on the server's /auth/link page (e2e/link-sign-in.spec.ts)
- [ ] The entry is absent for a signed-out reader and for a pre-user (user null, pre_user set) (LinkSignIn or Shell test)
- [ ] The entry is a plain anchor to linkSignInHref(url) = /auth/link?return_to= plus the encoded current path and query, and /auth/link is outside clientScope (nav and routes.test.tsx)
- [ ] The dist from go generate ./internal/web/spa is committed and the thread-view bundle stays under threadViewGzipBytes in budget.json

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 43a844d. The server half is merged: TKT-01M29CKRES (Implement account linking), PR #178, which built `GET /auth/link` (`internal/web/auth/link.go`, `linkPage`).

### Settled in grooming

- **Linking happens only through the explicit action** (maintainer decision 2026-09-29, docs/05 "Login flow" step 3, TKT-01M29CKRES). An identity nobody has, signed in from a browser that is already a member's, is attached only when the sign-in began from "link a sign-in". Otherwise it is refused. This ticket only makes that action reachable; it changes nothing about when a link happens.
- **The page is the server's** (docs/07 "URL scheme"). `GET /auth/link` lists each enabled provider as a one-button form posting to `/auth/{key}/link` with the page's `return_to`. Anyone who is not an active member signed in with a session gets 303 to `/auth/signin`, with `return_to` bringing them back. The page is `no-store`. `return_to` passes `auth.SafeReturnPath`, so only a path on this site survives. The client needs no state of its own and no new route.
- **`/auth/*` is outside the client's routes** (`clientScope` in `routes.tsx`, held by `routes.test.tsx`), so a plain anchor to `/auth/link` is a full page load that reaches the server. This is how `meHref` in `shell/nav.ts` sends a signed-out reader to `/auth/signin?return_to=…` (TKT-01M3MFS6KM).
- **A link issues no session** (`internal/web/auth/link.go`). The identity is used later to sign in through the callback, and so through `session.IssueUserWith`'s status check.
- **The action's final home is settings** (docs/05 says "in settings"). `/settings/identities` is a placeholder route until TKT-01M29CXK4 (Build the user settings screens, phase-4), whose "add" button is this same link. This ticket's entry moves there then.
- **The built client is committed.** Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. Only one lane may have a PR open that changes the dist (docs/13 "Running two lanes").

Engineering choices made here, each with the alternative that lost:
- **One helper builds the address.** `linkSignInHref(url)` in `shell/nav.ts`, beside `meHref`, answers `/auth/link?return_to=${encodeURIComponent(url)}`. `url` is preact-iso's `useLocation().url`, the path and query the reader is at. A second hand-built string where the entry is drawn lost, because the two would drift. The fragment is not sent, as `meHref` does not send it: the server would carry it only as part of `return_to`, and `useLocation().url` does not include it.
- **The entry is shown only for `viewer.value.user !== null`.** Only an active member's session gives a user (the chokepoint refuses any other status). A pre-user has `user: null` and is drawn by `ApplicantShell`, which has no forum navigation, and a signed-out reader has `user: null`. No new server field is needed. A check of the account's status in the client lost, because `/me` does not answer it and the session already implies it.

### Open decision

Where does the entry go? There is no account menu. The shell's only account control is the top bar's handle on desktop and the Me tab on a phone, and both go to `/u/{handle}`, the profile. That is a placeholder screen (`routes.tsx`, `placeholder('profile', 'Profile')`), and no ticket builds it.
- (a) **The member's own profile.** `/u/{handle}` gets a small screen in place of the generic placeholder. It says the profile is not built yet and, when the handle is the viewer's, offers "Link a sign-in". It is one press from the handle or the Me tab, which every screen has, and needs no change to the shell or to docs/06.
- (b) **An account menu.** The desktop handle and the phone Me tab open a menu holding "Your profile" and "Link a sign-in". The link is then one menu away from every screen, and the menu would also be the natural home for Sign out, which the member's shell lacks today (only `ApplicantShell` has one). But it changes docs/06 "Mobile": the Me tab stops being a plain tab, and docs/06 chose tabs over a menu (docs/10 Q25).
- (c) **A minimal `/settings/identities` screen**, holding only the link, ahead of TKT-01M29CXK4. It is where the action will finally live, but nothing in the shell leads to `/settings`, so it needs (a) or (b) as well.

Recommendation: (a). It is the smallest change that meets "one step" from a control on every screen. It leaves docs/06 alone, and it gives way cleanly when TKT-01M29CXK4 builds settings. The missing Sign out is a separate gap and is reported, not solved here.

### Approach

1. **`shell/nav.ts`**: `export function linkSignInHref(url: string): string`, as above, with a comment naming TKT-01M29CKRES and the move to `/settings/identities` with TKT-01M29CXK4.
2. **`shell/LinkSignIn.tsx`**: `LinkSignIn()` returns `null` unless `viewer.value.user`. Otherwise it returns `<a class="hit" href={linkSignInHref(useLocation().url)} data-nav="link-sign-in">Link a sign-in</a>`, with the line "Add another way to sign in to this account." It is a plain anchor with no `onClick`, so the router does not take it.
3. **Place it** where the open decision says:
   - (a) A new `screens/ProfilePlaceholder.tsx` for `/u/:handle` in `routes.tsx`. It renders what `placeholder('profile', 'Profile')` renders, plus `<LinkSignIn />` when `params.handle` equals `viewer.value.user.handle`, compared case-insensitively as handles are. `data-screen="profile"` is kept, so the route tests hold.
   - (b) A `shell/AccountMenu.tsx` used by `TopBar` for the handle and by `TabBar` for Me, with the item list above, following `shell/MoreMenu.tsx`'s focus and Escape rules. docs/06 "Mobile" is updated in the same PR.
   - (c) As (a), with the placeholder for `/settings/identities` also rendering `<LinkSignIn />`.
4. **CSS** in `shell/shell.css`, or the screen's stylesheet, with targets of at least `var(--target)`.
5. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and check that the budget line stays under 60000 B.

### Tests

- **`shell/Shell.test.tsx`**, or a new `shell/LinkSignIn.test.tsx`:
  - `builds the link address`: `linkSignInHref('/unread?x=1&y=a%20b')` is `'/auth/link?return_to=' + encodeURIComponent('/unread?x=1&y=a%20b')`.
  - `a member sees the entry`: with a signed-in viewer, the anchor has that `href` for the current location.
  - `nobody else does`: absent for a signed-out viewer, and for a pre-user (`user: null`, `pre_user` set).
- **`routes.test.tsx`**: `/auth/link` and `/auth/link?return_to=%2F` are outside `clientScope`, in the table that already holds `/auth/logout`.
- **e2e, new `e2e/link-sign-in.spec.ts`**, run in both Playwright projects (390 px and 1440 px):
  - A signed-in member on a thread page reaches the entry and follows it with one press.
  - The browser lands on `/auth/link` with `return_to` naming the thread's path, and the server's page offers the dev provider.
  - A signed-out reader finds no entry.
- **Invariants held by the server's tests, which stay green:**
  - A link cannot mint a member session for a pending, suspended or deactivated user: `TestLinkRefusesAnInactiveMember`, `TestLinkNeedsTheMembersSession`, `TestTheLinkStartNeedsAMember` and `TestNoLinkWithoutIntent`.
  - Every session still comes through the chokepoint: `TestOneDoorIntoSessions`, whose allowed-caller list this ticket does not change.
  - This ticket adds no server code, no session path and no cached DTO.

### Out of scope

- Listing and unlinking identities, and the settings screens: TKT-01M29CXK4.
- Any change to `/auth/link`, `/auth/{provider}/link` or the callback: TKT-01M29CKRES, done.
- A Sign out control in the member's shell, which does not exist today. The grooming report raises it; option (b) above would host it.
- A real profile screen. Option (a) keeps the placeholder and adds only the entry.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T00:29:01Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30, against main at 43a844d. Open decision, not taken here: where the entry goes. The shell has no account menu: the desktop handle and the phone Me tab both go to /u/{handle}, a placeholder that no ticket builds. Options: (a) the member's own profile placeholder; (b) a new account menu on the handle and the Me tab, which changes docs/06 'Mobile'; (c) a minimal /settings/identities, which still needs (a) or (b) to be reached. The grooming recommends (a). Found while planning: the member's shell has no Sign out control; only ApplicantShell has one. Option (b) would host it. It is reported, not filed here.
