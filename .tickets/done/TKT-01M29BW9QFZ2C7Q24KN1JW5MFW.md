---
schema: 3
id: TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
title: Load and validate config.toml with env overrides and file secrets
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-15T17:12:30Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/config
  name: ""
extensions: {}
---

## Description

`internal/config`: TOML loading, validation with errors that name the offending key, `KETJU_*` environment overrides for container deployments, and `client_secret_file` style indirection so a secret never has to sit in the config file.

The split is the thing to get right. `config.toml` holds what must be readable before the database exists: bootstrap admins, OIDC providers, paths, listen address, fsync policy, and cache sizes. Everything an admin changes at runtime belongs in the `settings` table instead, and putting a key in the wrong half is the mistake this ticket exists to prevent.

## Acceptance criteria

- [x] An invalid config fails at startup naming the key and the reason, never with a zero value silently substituted
- [x] Environment overrides are applied and their precedence over file values is tested
- [x] A secret loaded from a file never appears in a log line or in the effective-config output
- [ ] Every configured OIDC provider is validated for a reachable issuer at startup, and a failing one is disabled with a loud log line rather than aborting the server

## Implementation plan

internal/config with four files: config.go (schema, Load, defaults), secret.go (the Secret type and file indirection), env.go (the KETJU_* override table), validate.go (every check, each naming its key).

TOML by github.com/pelletier/go-toml/v2. Chosen over BurntSushi/toml for DisallowUnknownFields plus StrictMissingError, which carries one entry per offending key rather than one error for the file. The first acceptance criterion is about naming the offending key, so a library that already knows the key is the difference between a good message and a parser written here.

Defaults are driven by which keys the file mentions, not by zero values. Decoding cannot tell an absent rendered_cache_mb from one written as 0, and the two mean opposite things; conflating them is the silent substitution the criterion forbids. So Load also decodes into a map and flattens it into a set of dotted key paths.

Environment overrides are an explicit table rather than derived from struct tags. Derivation is less code and a worse contract: the variable name has to be guessed by whoever writes the compose file, and it changes silently when a field is renamed or nested deeper. Overrides() returns the table, which is what config check prints.

One file rule covering every override: KETJU_X_FILE supplies the value of KETJU_X. Setting both is an error, not a precedence rule, because precedence means a deployment that sets both gets one silently and it is the one nobody checked.

Secret is a struct, not a string, so the value has one named exit. It implements Stringer, GoStringer, json.Marshaler, TextMarshaler and slog.LogValuer, all returning [redacted].

## Notes

**agent:claude-code/config** at 2026-09-15T17:12:05Z

The split had a contradiction in the docs and this ticket had to settle it.

docs/05 shows a [registration] block in config.toml with mode and allow_local_passwords. docs/04 says registration mode is a runtime setting in the settings table. Both cannot be true, and this ticket's whole reason for existing is that putting a key in the wrong half is the mistake to prevent.

Settled in favour of docs/04: registration policy is something an admin changes while the server runs, so it belongs in settings, where the change takes effect without a restart and leaves an audit row. A runtime setting that also exists in a file works right up to the day an admin changes it in the UI and the next restart silently puts it back -- a bug that presents as the admin UI not working, days after the edit.

So the loader does not ignore these keys and does not quietly accept them. registration, registration.mode, registration.allow_local_passwords, site_name and slow_mode are refused by name, with the settings table named in the same message. 'Unknown key' would have been worse than useless: it invites the reader to conclude the key was renamed and go hunting for a spelling that does not exist.

docs/05 and docs/02 updated to match. The movedKeys table in problem.go is where the next one gets added.

**agent:claude-code/config** at 2026-09-15T17:12:05Z

Secret is a struct rather than a named string type, and that is the whole defence.

A `type Secret string` is printed by anything that prints a string, and 'anything' includes %v on a struct that merely contains one, a slog attribute, JSON encoding of a config dump, and the error text of a library that quotes what it was given. Each is a plausible route from a config file to a log aggregator, and closing them one at a time as they are noticed is how a credential ends up in one.

So Secret holds an unexported field and implements the four interfaces printing goes through -- fmt.Stringer, fmt.GoStringer, json.Marshaler, slog.LogValuer -- plus TextMarshaler for the TOML dump. Value() is the single exit.

TestASecretSurvivesNoRouteToOutput sweeps every route rather than asserting once, and TestASecretInsideAStructIsStillRedacted covers the case that actually happens: nobody logs a Secret, they log the Config that contains one. Verified by mutation -- replacing the four methods with the raw value fails 9 assertions in the first test and 8 in the second, including Effective(), slog on the whole config, and %#v.

TestALoadFailureDoesNotQuoteASecret covers the remaining route: an error message is a log line, and the failure happens after the secret has been read.

**agent:claude-code/config** at 2026-09-15T17:12:17Z

Criterion 4 is left unticked on purpose, and it is not a gap in the work.

'Every configured OIDC provider is validated for a reachable issuer at startup, and a failing one is disabled with a loud log line rather than aborting the server' is discovery, not configuration. It needs an HTTP client, a discovery document parser, a JWKS cache and a refresh policy, and TKT-01M29C5Y34 -- the provider registry -- already owns all four and carries the same criterion almost word for word: 'A provider failing discovery is disabled and logged, and the server still starts and serves the others.' That ticket already depends on this one.

Implementing it here would mean the config loader making network calls, which puts a DNS timeout in the path of `ketju config check` and makes a unit test of a TOML parser need a fake issuer.

What this ticket does own and has done is the half that is knowable without the network: the issuer must be present, absolute, https, without a trailing slash and without a query or fragment. https is not style -- the ID token's signing keys are fetched from that origin, so plaintext means anyone on the path decides who may log in. The trailing-slash check exists because the issuer has to match the iss claim exactly and providers do not send one.

Also present but not acted on: insecure_skip_issuer_check, off by default. The key has to exist for the registry to read it; the behaviour and the loud log line are the registry's.

**agent:claude-code/config** at 2026-09-15T17:12:30Z

Two known gaps, both small, neither worth a ticket of its own yet.

A KETJU_ variable that no override claims is not reported. The loader takes a getenv function, which can answer a lookup but cannot enumerate, so a typo like KETJU_DATA_DIRR is simply never read. Fixing it means also taking an environ function, and the natural owner is TKT-01M29BYYJG ketju config check, which is where an operator would look for the answer. Overrides() already returns the complete list to check against.

data_dir is validated for existing-and-not-a-directory but not for writability. Writability is a property of the moment the server writes, not of startup, and a check here would be a TOCTOU with a comforting message. TKT-01M29CCBZ8 healthz already says the data directory must be writable, which is the right place: it is asked repeatedly.

## Summary

internal/config loads config.toml, overlays KETJU_* overrides, resolves file-held secrets, and refuses to start on anything it cannot make sense of -- reporting every problem in one pass, each naming its key.

Settled the docs/04 versus docs/05 contradiction over registration policy in favour of the settings table, and made the loader refuse runtime settings by name with the settings table named. docs/02 and docs/05 updated.

Defaults are presence-driven, so a key written as 0 keeps the meaning the operator gave it. Overrides are an explicit table rather than derived from field names, and KETJU_X_FILE supplies the value of KETJU_X for all of them. Secret has one named exit and is redacted through Stringer, GoStringer, JSON, text and slog, verified by mutation.

Criterion 4 (issuer reachability) is left unticked: it is discovery, and TKT-01M29C5Y34 owns it and carries the same criterion. The offline half of issuer validation is done here.
