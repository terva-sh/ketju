---
schema: 3
id: TKT-01M3MAS08RJDT1RRJZVA6TB423
title: Accept an OIDC issuer that ends in a slash, as Authentik's does
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:119
    path: null
claim: null
archive: null
created_at: 2026-09-28T15:40:14Z
updated_at: 2026-09-28T16:14:01Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

`ketju config check`, and so `serve`, refuses an OIDC issuer that ends in a slash (internal/config/validate.go, "providers do not send a trailing slash"). Authentik does send one. Its per-provider issuer is `https://authentik.local.sothr.com/application/o/<slug>/`, and the discovery document's `issuer` field carries that trailing slash. Found deploying the dogfood instance (TKT-01M3M9S7S2), whose provider is `brokkr-ketju`.

With the slash, config is refused. Without it, go-oidc's discovery refuses the mismatch, because it compares the configured issuer with the discovered one exactly, which is the check that matters. So no Authentik provider can be configured at all. `insecure_skip_issuer_check` is not an acceptable workaround, because it weakens verification.

### Fix
Accept the issuer exactly as written. Keep the https and absolute-URL checks. Rely on go-oidc's exact comparison against discovery, and on the `iss` claim check, to catch a mismatch at startup, where the error names both values. Update the config test that pins the old rule. If docs/05 or docs/07 states the slash rule, correct it.

## Acceptance criteria

- [x] An issuer ending in a slash passes ketju config check, and serve reaches discovery with it
- [x] A mismatch between the configured and discovered issuer fails at startup with both values named
- [x] The config test pinning the old rule is replaced by one pinning the new

## Implementation plan

Read on 2026-09-28 against main 2b1c620. No settled-in-grooming items existed in this section.

### What the code does now
- `internal/config/validate.go` `validateIssuer` refuses an issuer whose path ends in `/`. That is the only place the slash is judged.
- `internal/auth/oidc/oidc.go` `discover` passes `pc.Issuer` to `gooidc.NewProvider` untouched. Nothing trims or normalises it. go-oidc v3.21.0 builds the discovery URL as `TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"` and then compares the document's `issuer` with the configured string exactly; a mismatch is `IssuerMismatchError`, whose text names both values. The verifier is built from the discovered issuer and compares `iss` exactly.
- A discovery failure disables the provider and logs `oidc provider disabled: discovery failed` with the key, the configured issuer and the error; with every provider disabled, `New` fails and serve does not start.
- docs/05 and docs/07 do not state the slash rule. docs/11 mentions this ticket only as a bug, which stays true as history.

### Change
1. Drop the trailing-slash rule from `validateIssuer`. Keep https, host, and no query/fragment. Add a comment saying why the slash is not judged here (the exact comparison against discovery is the check).
2. Config test: remove the "issuer with a trailing slash" refusal case; add a test that an Authentik-shaped issuer with a trailing slash loads and reaches `Config` byte for byte.
3. oidc fake IdP: let it be mounted under a path so its issuer can be `https://host/application/o/<slug>/`, with endpoints under that path, as Authentik serves them.
4. oidc tests: (a) an issuer ending in a slash discovers, and a login's ID token with that `iss` verifies through Exchange; (b) configured and discovered issuers that differ only by the slash, in either direction, fail at startup, and the startup log names both values.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T16:13:15Z

Evidence for the criteria, gathered 2026-09-28 on run/mas08r-oidc-issuer-slash from 2b1c620.

- Criterion 1: `ketju config check --config` on a config naming `https://authentik.local.sothr.com/application/o/brokkr-ketju/` exits 0 and prints the issuer unchanged. `TestAnIssuerEndingInASlashWorksEndToEnd` mounts the fake IdP at `/application/o/ketju/`, so the configured issuer, the discovery document's issuer and the token's iss all end in a slash; discovery succeeds and Exchange verifies. A throwaway test (not committed) ran `oidc.New` against the real Authentik discovery document: with the slash the provider was discovered, without it discovery failed. Nothing in internal/auth/oidc trims the issuer; `discover` hands `pc.Issuer` to `gooidc.NewProvider` as written, and go-oidc v3.21.0 compares it with the document exactly (oidc.go line 331).
- Criterion 2: `TestAnIssuerThatDiffersFromDiscoveryFailsAtStartupNamingBoth` covers both directions of a slash mismatch. `New` fails when that is the only provider, and the `oidc provider disabled: discovery failed` log line, decoded as JSON, names the provider and both quoted issuers. The returned error names only the key, as before; the values are in the log line serve writes just before it exits.
- Criterion 3: the "issuer with a trailing slash" refusal case is gone. `TestAnIssuerIsKeptExactlyAsWritten` loads four issuers, two ending in a slash, and checks each reaches Config byte for byte. A new "issuer with a query" refusal case pins the rule next to the one removed.
- docs/05 and docs/07 never stated the slash rule, so no design text changed.

Mutations tried: trimming the slash before `gooidc.NewProvider` failed both oidc tests; always setting `InsecureIssuerURLContext` failed the mismatch test; restoring the slash rule in validateIssuer failed the config test for both slash-ended issuers; dropping the query/fragment rule failed the new query case.

## Summary

config no longer refuses an issuer ending in a slash; validateIssuer keeps the https, host, and no-query/fragment rules. The issuer reaches go-oidc exactly as written, so an Authentik issuer (https://host/application/o/<slug>/) discovers and verifies, and a slash mismatch in either direction disables the provider at startup with both values in the log. Tests: config keeps the issuer byte for byte; the fake IdP can be mounted under a path for a slash-ended issuer end to end; a mismatch test decodes the startup log line. Checked live against the brokkr-ketju Authentik provider.
