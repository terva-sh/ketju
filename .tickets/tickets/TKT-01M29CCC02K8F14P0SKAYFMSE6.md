---
schema: 3
id: TKT-01M29CCC02K8F14P0SKAYFMSE6
title: Scaffold the Preact client and its embedding
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase1-run-415af6aa
  branch: run/ccc02-client-scaffold
  worktree: /home/sothr/.t3/worktrees/ketju/t3code-415af6aa
  commit: 70b6caf55d9697d86df5ccfabf8ac4a1cae50196
  session: null
  claimed_at: 2026-09-24T10:27:10Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T10:28:35Z
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

- [ ] `go build` produces a binary serving the client with no separate build step when the client is unchanged
- [ ] The dev proxy serves the client against a live Go server with hot reload
- [ ] index.html renders as a Go template injecting the CSP nonce and the bootstrap /me payload
- [ ] Preact is pinned at 10.x and the pin is documented with the reason

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
