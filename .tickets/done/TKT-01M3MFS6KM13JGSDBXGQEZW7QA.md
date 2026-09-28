---
schema: 3
id: TKT-01M3MFS6KM13JGSDBXGQEZW7QA
title: Give a signed-out reader a way to sign in
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - area:client
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:121
    path: null
claim: null
archive: null
created_at: 2026-09-28T17:07:43Z
updated_at: 2026-09-28T17:22:06Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

- **Found:** deploying the dogfood instance at https://ketju.brokkr.local.sothr.com (TKT-01M3M9S7S2, Deploy a local dogfooding instance of ketju on this machine).
- **Bug:** the client's "Sign in" (top bar, TopBar.tsx; phone Me tab, shell/nav.ts `me`) sends a signed-out reader to /settings. The code comment says "where a signed-out reader signs in", but /settings is an unbuilt placeholder (Phase 4). Nothing in Phase 1 links to /auth/{provider}/start, and the e2e suite signs in through /auth/fake/start directly, so this went unnoticed.
- **Design:** docs/05 line ~77, "the login page only shows configured providers".
- **Maintainer's decision, 2026-09-28:** "Server page, skip if one provider".

## Acceptance criteria

- [x] GET /auth/signin lists each enabled provider by name, linking to /auth/{key}/start with return_to carried
- [x] With exactly one usable provider, /auth/signin answers 302 to that provider's start
- [x] With no usable provider, /auth/signin shows a plain 'sign-in is not available' page, not an error
- [x] A signed-in reader is redirected to return_to, or /
- [x] return_to is validated with SafeReturnPath; a hostile return_to never leaves the site
- [x] The client's Sign in (top bar and phone Me tab) is a full navigation to /auth/signin?return_to=<current path>; signed-in behaviour is unchanged
- [ ] A Playwright check clicks Sign in while signed out, reaches /auth/signin and lands at the fake provider's start
- [x] docs/07's route list and docs/05 name /auth/signin

## Implementation plan

### Server: GET /auth/signin (internal/web/auth)

- `Providers` gains `Enabled() []*oidc.Provider`, which `*oidc.Registry` already has. A nil `Providers` (none configured) and an empty `Enabled()` (every provider disabled by discovery) are the same case.
- Order of checks: return_to through `SafeReturnPath` first; a signed-in viewer gets 302 to it; no enabled provider gets a plain 200 "Sign-in is not available" page; exactly one gets 302 to `/auth/{key}/start?return_to=...`; several get a 200 page listing each by `Name()` as a link to its start URL, in config order.
- The start URL is built with `url.Values`, the key path-escaped, and return_to added only when it is not "/", so the redirect and each link carry the validated value and nothing else. `start` validates it again anyway.
- The page is an html/template in page.go beside the others, so escaping, the CSP (no inline script or style needed) and the security headers come from the existing chain. `Cache-Control: no-store`, because what it shows depends on the session and on provider state.
- Links rather than a form: start is a GET, and a link is what the existing flow and the e2e already use.

### Client

- nav.ts: the Me destination becomes a function of the current URL. Signed in, the profile, unchanged; signed out, `/auth/signin?return_to=<path+search>`. clientScope already puts /auth/* out of the router's reach, so the anchor is a full page load. TopBar and TabBar read the URL from useLocation, which they already call.
- Shell.test.tsx: the signed-out expectation changes from /settings to the sign-in URL, and return_to follows the path.
- Regenerate internal/web/spa/dist and check the bundle budget.

### Tests

- Go (auth_test.go): one provider redirects; several list (a second provider on the same in-process IdP); none configured and all disabled show the unavailable page; a signed-in viewer redirects to return_to; hostile return_to values collapse to "/" in the redirect and the links; the page escapes a provider name.
- e2e: a new sign-in spec, signed out, at both widths: click Sign in, see /auth/signin then /auth/fake/start with return_to, pick alice at the fake IdP, land back on the page it started from. Existing specs keep /auth/fake/start. Cost: three /auth requests per width against the burst of ten refilling every 6 s.

### Docs

- docs/07 URL scheme block gains `/auth/signin`, and the paragraph after it describes it.
- docs/05 gains a sentence beside "the login page only shows configured providers".

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T17:21:29Z

Criterion 7 is left unticked. e2e/sign-in.spec.ts clicks Sign in while signed out, at 390 px and 1440 px. It checks that the browser loads /auth/signin?return_to=<the page> as a full navigation, and that the server answers 302 to /auth/fake/start with the same return_to. It reads that answer with route.fetch and stops the browser there, so the browser never actually loads the fake provider's start.

Why it stops there:
- /auth/* allows a burst of ten requests per address, refilling one every 6 s (docs/08, internal/web/ratelimit.go). The setup project spends four and the smoke spec two per width.
- A version that followed the whole flow (login page, start, callback: three per width) passed the full suite but hit 429 at the fake provider in a filtered run (`npx playwright test sign-in smoke`), so it depended on how long the earlier specs took.
- Following only to the start would cost two per width, which is still over the burst in a filtered run.
- page.route cannot answer the start in the browser instead, because it never sees a request that a redirect made.

What covers the rest: the setup project's sign-in exercises start → provider → callback, and TestTheLoginPageSkipsToTheOnlyProvider follows return_to from /auth/signin through the callback. To tick criterion 7 as written, raise the e2e build's auth burst (cmd/ketju/e2e.go), or accept the server's 302 as landing.

Settled here: "signed in" means `ViewerFrom(ctx).UserID != 0`, so a suspended user's live session is sent back rather than shown the providers. A pre-user (approval-mode applicant) session has UserID 0 and sees the page. None usable only happens with none configured in practice: startup aborts when every provider fails discovery, and Refresh never disables a working provider. The empty-Enabled branch is a guard, tested with a stub.

## Summary

GET /auth/signin (internal/web/auth/signin.go) is the login page. It validates return_to with SafeReturnPath and sends a signed-in reader back to it. With the enabled providers it shows a plain 200 'Sign-in is not available' for none, redirects 302 to /auth/{key}/start?return_to=... for one, and lists them by name for several. The client's signed-out Sign in (TopBar, phone Me tab) is a full navigation to /auth/signin?return_to=<path and query>; nav.ts meHref and tabs take the current URL. dist is regenerated (38031 B of 60000). Go tests cover the one, several and no-provider cases, the signed-in redirect and hostile return_to. e2e/sign-in.spec.ts checks the client-to-login-page navigation and the server's 302 to the fake start at both widths. Criterion 7 is unticked: the browser is stopped before it loads the start (see note). docs/05 and docs/07 describe the route.
