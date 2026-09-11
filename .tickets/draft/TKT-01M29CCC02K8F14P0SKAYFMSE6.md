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
updated_at: 2026-09-11T23:20:57Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
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
