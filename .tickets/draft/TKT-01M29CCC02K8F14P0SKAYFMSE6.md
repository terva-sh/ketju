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
updated_at: 2026-09-16T20:52:03Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
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
