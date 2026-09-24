---
schema: 3
id: TKT-01M29CCC02K8F14P0SKAYFMSE6
title: Scaffold the Preact client and its embedding
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:20:57Z
updated_at: 2026-09-24T05:21:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
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

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Create `web/client/` with Vite 8, Preact 10.x, `@preact/signals`, `preact-iso`, TypeScript, and a `package-lock.json`. Pin `preact` to `~10.x` or an exact 10 version. Record the reason (docs/10 Q20: pin 10.x through v1.0) in `web/client/README.md` and in a comment next to the dependency in package.json, if the chosen format allows one.
2. `vite.config.ts`: `build.outDir: '../../internal/web/spa/dist'` with `emptyOutDir`; `html.cspNonce: '{{.Nonce}}'` so every emitted script and style tag carries the Go template placeholder; `build.modulePreload.polyfill: false` so no inline script is emitted; `build.manifest: true` (CGBYH reads it); `server.proxy` for `/api`, `/auth`, `/events` and `/blobs` pointing at `http://localhost:8080`.
3. `web/client/index.html` is the Go template source. It holds a `<meta>` block for title and description, `<script type="application/json" id="bootstrap">{{.Bootstrap}}</script>`, a `<noscript>`, and the viewport meta with `viewport-fit=cover`.
4. `internal/web/spa/spa.go`: `//go:embed all:dist`, `//go:generate sh -c "cd ../../../web/client && npm ci && npm run build"`, `ShellTemplate()`, and `Assets()` serving `/assets/*` with immutable caching for hashed files.
5. Extend `ShellData` with `Bootstrap` (a `dto.Me` marshalled by html/template in a JSON-script context; confirm the escaping at claim) and `Meta`. Wire `web.NewShell(spa.ShellTemplate())` where the server is assembled (CCBYX `ketju serve`, if it has landed). Rewrite the ShellData doc comment, which currently argues against state in the shell, to cite docs/07.
6. Commit `dist/`, so a clean `go build` works. Add a CI step that runs `go generate ./internal/web/spa` and then `git diff --exit-code internal/web/spa/dist`. CI needs Node: add `nodejs npm` to the existing `apk add` in `.forgejo/workflows/ci.yml`, same job, so the required context `ci / Build and Test (pull_request)` covers it. Vite 8 needs Node 20.19+ or 22.12+; check which version the golang:1.27-alpine base ships.

### Files
`web/client/**` (new); `internal/web/spa/spa.go` and `dist/` (new); `internal/web/shell.go` and `security_test.go` (touched); `.forgejo/workflows/ci.yml` (touched).

### Tests
- AC1: CI runs `go build ./...` on a clean checkout. A Go test in `internal/web/spa` asserts that `dist/index.html` and at least one hashed asset are embedded.
- AC2: manual check, recorded in a ticket note. Start Go on :8080 and `npm run dev`, edit a component, and watch hot reload.
- AC3: point `TestTheShellHasNoInlineScriptOrStyle` at `spa.ShellTemplate()`, exempting only `type="application/json"` data blocks, since the JSON bootstrap has an inline body by design. Add a test that the rendered nonce and bootstrap appear. Close the inherited TKT-01M29C8020 AC3 in the browser: load the built client with the CSP enforcing and check the console is clean (Playwright or by hand).
- AC4: README text plus the package.json range.

### Design
docs/07 "Preact client" (Stack, Embedding, Dev) and "Security headers"; docs/02 module layout (`internal/web/spa`, `web/client`); docs/10 Q20; the comment on this ticket.

### Interactions
Blocks every client ticket. Needs CA99R for a real `/me`; until then the bootstrap is the anonymous `dto.Me`. CCBYX (serve) mounts the handlers. CGBYH reads the Vite manifest.
Open in docs: whether `dist/` is committed. The AC's "no separate build step" implies it is.

### Settled in grooming (2026-09-24)

- The bootstrap /me block is `<script type="application/json" nonce>`. Exempt that type in TestTheShellHasNoInlineScriptOrStyle, because JSON data is never executed.
- Commit the built dist/. CI rebuilds it and fails on a diff, so `go build` alone stays complete.
- Node joins the existing Build and Test job (docs/12, Changing CI).

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
