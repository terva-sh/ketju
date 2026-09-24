---
schema: 3
id: TKT-01M29CCC02K8F14P0SKAYFMSE6
title: Scaffold the Preact client and its embedding
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN367F3YQZESM63VN26GP
origin: null
dependencies:
  - TKT-01M29BW9NWKJGM3ZQ2DJH8WPWH
blocks_on: none
references:
  - ref: design:preact-client
    path: docs/07-api-and-clients.md
  - ref: pr:59
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T10:35:47Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

`web/client` as a Vite 8, Preact 10.x, TypeScript project, built into `internal/web/spa` and embedded with `//go:embed all:dist`.

`index.html` is a Go template so the server can inject per-thread meta tags for link unfurls, the CSP nonce, and a bootstrap JSON block carrying `/me`. In development, Vite's dev server proxies to Go on 8080. In production, Go serves the embedded build.

`go generate` runs the client build so that `go build` alone produces a complete binary when the client is unchanged. Pin Preact at 10.x through v1.0, per docs/10.

## Acceptance criteria

- [x] `go build` produces a binary serving the client with no separate build step when the client is unchanged
- [x] The dev proxy serves the client against a live Go server with hot reload
- [x] index.html renders as a Go template injecting the CSP nonce and the bootstrap /me payload
- [x] Preact is pinned at 10.x and the pin is documented with the reason

## Implementation plan

Revised at claim, 2026-09-24, after reading internal/web (shell.go, server.go, security.go and its tests) as they stand.

1. **`web/client/`:** Vite 8.3, `@preact/preset-vite`, Preact pinned exactly at 10.29.8, `@preact/signals`, `preact-iso`, and TypeScript 5.9, with a committed `package-lock.json`.
   - TypeScript 7 (the native port) was rejected for now. The scaffold wants the mature toolchain, and nothing depends on the TS version yet.
   - `npm run build` is `tsc --noEmit && vite build`.
   - `README.md` records the Preact pin with the reason (docs/10 Q20).
2. **`vite.config.ts`:**
   - `build.outDir` is `../../internal/web/spa/dist`, with `emptyOutDir`.
   - `html.cspNonce` is `{{.Nonce}}`, so every tag Vite emits carries the Go template placeholder.
   - `build.modulePreload.polyfill: false`, so no inline script is emitted.
   - `build.manifest: true`, for CGBYH.
   - `server.proxy` sends `/api`, `/auth`, `/events` and `/blobs` to `http://localhost:8080`.
3. **`index.html`** is the Go template source: `<title>{{.Title}}</title>`, a description meta, the viewport meta with `viewport-fit=cover`, a `<noscript>`, and `<script type="application/json" id="bootstrap" nonce="{{.Nonce}}">{{.Bootstrap}}</script>`. html/template treats application/json as a JS context, so a `dto.Me` value is marshalled and escaped there; a test confirms the escaping. In dev, Vite serves the raw template, so the client falls back to an anonymous viewer when the block does not parse.
4. **`internal/web/spa`:**
   - `//go:embed all:dist`, with `//go:generate` running `npm ci && npm run build`.
   - `Shell()` returns `dist/index.html`.
   - `Assets()` serves `/assets/*` with `Cache-Control: public, max-age=31536000, immutable` and no directory listings.
   - `dist/` is committed (settled in grooming).
5. **`internal/web`:**
   - `ShellData` gains `Title`, `Description` and `Bootstrap dto.Me`, and its doc comment cites docs/07.
   - `Shell.SetPage(fn)` lets the server supply them per request. The default is the site name "ketju" and an anonymous `dto.Me`, until CA99R and CCBYX land.
   - `DefaultShell` is removed; the security tests point at `spa.Shell()`.
   - `TestTheShellHasNoInlineScriptOrStyle` exempts only `type="application/json"` data blocks.
6. **CI:** new steps in Build and Test: `apk add nodejs npm`, then `go generate ./internal/web/spa` with `git diff --exit-code` and no untracked files under `dist/`. Adding steps only, per docs/12 "Changing CI".

### Tests
- AC1: a Go test that `dist/index.html` and a hashed JS asset are embedded and served, with CI's clean `go build`.
- AC2: a manual check, recorded in a note. `ketju serve` (CCBYX) has not landed, so the live Go server is a scratch program serving `web.New` with the real shell, assets and API. Run `npm run dev`, load the page through the proxy, edit a component, and watch HMR.
- AC3: the shell tests against the built template, a test that the nonce and the bootstrap JSON render (with escaping), and a browser load with the CSP enforcing and a clean console.
- AC4: the README and the exact version in package.json.

## Notes

**agent:claude-code/phase1-run-415af6aa** at 2026-09-24T10:34:16Z

Built and verified.

### What landed
- **`web/client`:**
  - Vite 8.3.0, `@preact/preset-vite` 2.10.6, Preact 10.29.8, `@preact/signals` 2.11.2, `preact-iso` 2.12.2, TypeScript 5.9.3. All exact pins, with the lockfile committed.
  - A router with `/` and a not-found route, a viewer signal read from the bootstrap block, and one small stylesheet (the CSS system is CCC0F).
  - The build is 10.4 KB gzipped of JS.
- **`internal/web/spa`:** embeds `dist/`. `Shell()` returns the built template; `Assets()` serves `/assets/*` with `immutable` and 404s everything else, including listings and `.vite/`.
- **`internal/web`:**
  - `ShellData` gains Title, Description and `Bootstrap dto.Me`, and `Shell.SetPage` supplies them per request. A page error is a 500.
  - `DefaultShell` is gone; every shell test runs against the built `index.html`.
- **CI:** two added steps, "Install Node" and "built client is current" (`go generate ./internal/web/spa`, then `git diff --exit-code` including added files). Existing steps are unchanged, per docs/12.

### Decisions (alternatives considered)
- **Exact pins.** Rejected: `~10.29`. CI diffs the build, so any unpinned resolution is a spurious failure waiting to happen, and the lockfile alone does not stop a `npm install` from moving it.
- **TypeScript 5.9.** Rejected: TypeScript 7, the native port, which is new; nothing here needs it.
- **The README gives the Preact pin's reason.** docs/10 Q20 states the pin but not why. The reason given is that the router, signals and preset are built against 10, and a major bump mid-build would turn every client ticket into a migration. It is labelled as recorded here, not quoted from docs/10.
- **The bootstrap is a real `dto.Me` value in a `type="application/json"` block.** html/template treats that as a JS context and emits escaped JSON, so a display name of `</script><script>alert(1)</script>` round-trips as data, which the test checks. Rejected: `template.JS` of pre-marshalled bytes, which bypasses the escaper.
- **The default page** is the title "ketju" and an anonymous viewer, until CCBYX (serve, site_name) and CA99R (the real /me) wire it.
- **Reproducible builds.** Two builds from a clean `npm ci` were byte-identical, which is what the CI check relies on.

### AC2: the manual check, done 2026-09-24
- `ketju serve` has not landed (CCBYX), so the live Go server was a scratch `main` under `cmd/`, never committed and deleted afterwards. It served `web.New` with the real shell, `spa.Assets()` and a stub API.
- With `npm run dev` on :5173, driven by headless Chromium through Playwright:
  - The page rendered.
  - `fetch('/api/v1/ping')` returned the Go server's JSON through the proxy.
  - Editing `src/app.tsx` updated the heading in place. A `window` marker set before the edit survived, so it was HMR, not a reload, and Vite logged `hmr update /src/app.tsx`.
  - The edit was reverted.

### AC3: the browser half
- The built client loaded from the Go server with the CSP enforcing. Both routes rendered, the CSS applied, and the console was clean.
- A negative control (an injected inline script) was blocked and reported, so the check can see a violation.
- This also closes TKT-01M29C8020's AC3, ticked there with a note.

### Mutation
8 mutants over the shell, assets and the inline check. One survivor weakened the inline check itself, invisible because the built shell has nothing for it to miss. It became `TestTheInlineCheckIsNarrow`, which pins the single exemption. All are killed.

## Comments

**agent:claude-code/web** at 2026-09-16T20:52:03Z

Inherited check from TKT-01M29C8020 (security headers, CSP nonces).

That ticket's AC3 -- "No inline script or style is needed by the client,
verified with the CSP in report-only off" -- could not be met there, because
the client it refers to is the one this ticket scaffolds. It was left unticked
rather than claimed.

What already exists for you to build against:

- `web.DefaultShell` is a Go template rendered per response with `.Nonce`. The
  mechanism is what has to survive being replaced by the built `index.html`:
  a template, rendered per response, with the nonce on every script and style
  tag. Swapping the markup is fine; dropping the per-response render is not,
  because the nonce would go stale.
- The policy is strict and carries no `'unsafe-inline'`, so any inline script,
  inline style, or `on*` attribute in the built client will be blocked by the
  browser rather than warned about.
- `TestTheShellHasNoInlineScriptOrStyle` in `internal/web` already fails on an
  inline body, a script without a nonce, or an `on*` attribute. Pointing it at
  the real built shell is the cheapest way to close AC3.

The remaining half is the browser one: load the real client with the CSP
enforcing (not report-only) and confirm the console is clean. Worth doing
before the client grows, because retrofitting means finding every inline
handler written in the meantime.

## Summary

web/client is a Vite 8 + Preact 10.29.8 (exact pins, documented) TypeScript project that builds into internal/web/spa/dist, which is committed and embedded. index.html is a Go template rendered per response with the CSP nonce, title, description and a bootstrap /me JSON block (web.Shell.SetPage). CI installs Node and fails if the committed build differs from a fresh one. The dev proxy with HMR and the built client under an enforcing CSP were both verified in headless Chromium.
