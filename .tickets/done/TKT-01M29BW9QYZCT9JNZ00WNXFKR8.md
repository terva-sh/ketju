---
schema: 3
id: TKT-01M29BW9QYZCT9JNZ00WNXFKR8
title: Add structured logging with slog
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:observability
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-16T19:54:47Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

`slog` throughout, JSON in production and text in development, with the level configurable. Request logging in middleware carrying method, path, status, duration, and the viewer id when there is one.

The rule that matters more than the format: no post source, no session id, no token, and no secret ever reaches a log line. A forum's logs end up in someone's aggregator, and a body that was soft-deleted in the log is not deleted if it is also in a log line.

## Acceptance criteria

- [x] Production output is JSON and development output is text, selected by config
- [x] Request logs carry method, path, status, duration, and viewer id
- [x] A test asserts that session ids and post source never appear in log output

## Notes

**agent:claude-code/phase1** at 2026-09-16T19:54:47Z

### The query string is the live hazard, not the attribute denylist

`GET /auth/{provider}/callback?code=...&state=...` is how every login completes.
Logging `r.URL.RequestURI()` would put an authorization code into every log
aggregator the deployment feeds, and one is enough to complete somebody else's
login. The middleware records `r.URL.Path` only. Verified by changing it back
and watching the test fail.

This is worth knowing for TKT-01M29C5Y3S: anything else that logs a URL --
an error handler, a rate limiter, a proxy access log -- has the same problem,
and the path/URI distinction is the whole of the fix.

### Three mechanisms, and which one to reach for

- The redacting handler catches attribute *names*. It is a denylist and
  denylists are never complete; it is there because it costs nothing and
  catches the common mistake.
- `slog.LogValuer` on the type is the one that survives being given an
  innocuous key. `config.Secret` already does this. **When TKT-01M29BSPEF adds
  a session type, give it a LogValue that redacts.** That is the durable fix
  for session ids; the "session_id" key entry is only a backstop for whoever
  logs a raw string.
- The middleware never logs the query.

### A claim that turned out to be false

The `Resolve` call in the redactor looks like what protects `config.Secret`. It
is not: slog resolves a LogValuer itself before formatting, so removing the
line does not make that test fail. What it actually buys is walking a *group*
returned by a LogValuer -- unresolved, its kind is LogValuer rather than Group,
the descent skips it, and the inner handler then expands it with whatever
sensitive keys it holds. There is now a test that fails when the line is
removed, and the comment says which layer does what.

### Two denylist entries deliberately left out

A bare `auth` and a bare `key`. Both are names an operator needs to read -- an
OIDC provider key is the thing a "provider disabled" line exists to name, and
`auth` is a natural name for a *group* holding a provider, a subject and a
token. Redacting either wholesale would throw away useful fields to hide
something the compound entries catch anyway.

Conversely `code`, `state` and `nonce` are in, and will occasionally redact
something harmless. In this system those three names belong to the OIDC flow. A
status code should be logged as `status` and an error code as `error_code`.

### For whoever wires the server

`Middleware` takes the logger and returns the usual `func(http.Handler)
http.Handler`. `ContextWithViewer` is how the session middleware puts the
viewer id where the request log can find it; until sessions exist, every line
logs without a viewer rather than with a zero, because "user zero" and "nobody"
must not look the same.

The response recorder implements `Flush` and `Unwrap`. Both are needed: SSE
handlers written against `http.Flusher` break on a wrapper without it, and
`http.ResponseController` needs `Unwrap`. Without them the `/events` endpoint
buffers until the handler returns, which presents as "live updates are broken".

## Summary

internal/logging: slog logger with a [log] level and format in config (auto = text to a terminal, JSON otherwise), plus request-logging middleware carrying method, path, status, bytes, duration and viewer id. Nothing sensitive reaches a log line: a redacting handler by attribute name, slog.LogValuer on secret-bearing types, and a request log that records the path and never the query, because the OIDC callback carries an authorization code in it.
