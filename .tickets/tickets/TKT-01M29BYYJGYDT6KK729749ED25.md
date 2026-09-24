---
schema: 3
id: TKT-01M29BYYJGYDT6KK729749ED25
title: Ship ketju config check
type: task
status: ready
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
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-24T05:21:19Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude/phase1-groom
  name: ""
extensions: {}
---

## Description

A subcommand that validates the configuration and prints the effective values with secrets redacted, exiting non-zero on a problem so it works in a deployment script.

This is the command an operator runs before restarting, and the one they paste into a support thread. Redaction has to be by construction rather than a denylist of key names, because the first secret somebody adds with an unexpected name is the one that leaks.

## Acceptance criteria

- [ ] Exit code is non-zero on any validation failure and the message names the key
- [ ] Effective output shows merged file, environment, and default values, and says which layer each came from
- [ ] No secret value appears in the output, including one loaded from a file, tested with a config carrying a known sentinel string

## Implementation plan

Pre-groom plan (2026-09-24). Revise at claim, after reading the code as it stands then.

### Approach
1. Add `cmd/ketju/config.go` with `ketju config check [--config PATH] [--json]`. The path default is decided at claim: docs/03's layout puts `config.toml` inside `data/`, but `data_dir` is itself a config key, so the default is circular. The candidate is `./config.toml`, overridable with `KETJU_CONFIG`. The subcommand calls `config.Load(path, nil)`.
2. On a load error, print each `config.Problem`. `*config.Error` already carries `Key` and `Msg`, and `Keys()` lists them. Exit non-zero through a new `errConfig`, or reuse `errUsage` (2), in `cmd/ketju/exit.go`. Choose the code at claim and document it in the command's usage text, as fsck does. Warnings (the second return of Load) print but do not fail.
3. Provenance: `Load` currently discards what it knows. Add `Config.Sources() map[string]Source`, where Source is one of `file`, `env`, `env-file` (from `KETJU_X_FILE`), `secret-file` (from `client_secret_file`), or `default`. Record it in `applyEnv`, `applyDefaults`, and `resolveSecrets`. The `present` map already separates file from default, so this adds bookkeeping, not logic.
4. Output: walk the effective config as dotted keys in a stable order, one line each: `key = value  # source`. Base it on `Config.Effective()`, which marshals through TOML. `Secret` already implements `MarshalText`, `MarshalJSON`, `String`, `GoString`, and `LogValue` as redacted. Redaction is therefore by type, not by key name, which is the "by construction" the ticket asks for. The printer must never call `Secret.Value()`. A test enforces that. `--json` emits `{key: {value, source}}`.
5. Also print the env override table from `Config.Overrides()`, marking which ones are set, so an operator sees the variables this build reads. Secret overrides show `set` or `unset` only.

### Files and packages
New: `cmd/ketju/config.go` and `cmd/ketju/config_test.go`. Touched: `cmd/ketju/main.go` (dispatch and usage), `cmd/ketju/exit.go`, `internal/config/config.go` and `env.go` (Sources), and `internal/config/config_test.go`. Reuses `config.Load`, `Effective`, `Overrides`, `Secret`, `Error.Keys`, and `testdata/full.toml`.

### Tests
- AC1: `TestConfigCheckFailsNamingKey` uses a table of bad configs (bad fsync, missing data_dir, a moved key such as `site_name`). It asserts a non-zero exit via `exitCodeFor` and that the output contains the key.
- AC2: `TestConfigCheckShowsLayers` uses a file setting `listen`, a getenv setting `KETJU_LOG_LEVEL`, a `KETJU_SITE_URL_FILE`, and the remaining defaults. It asserts each key's source label. Add a golden test of the full text output with `-update`, following the repo's golden habit.
- AC3: `TestConfigCheckNeverPrintsSecret` puts a sentinel `ketju-sentinel-7f3a` in an inline `client_secret`, in a `client_secret_file`, and in `KETJU_OIDC_<KEY>_CLIENT_SECRET_FILE`. It asserts the sentinel appears in neither text nor JSON output. Also a static test that `cmd/ketju/config.go` never references `.Value()`. Mutation check for the commit body: temporarily printing `Value()` must fail the test.

### Design
docs/02-architecture.md "Configuration". The `internal/config` package doc ("What belongs here", "Failing rather than defaulting"). The comments on `internal/config/env.go` `FileSuffix`.

### Interactions
Standalone. It depends on the done config loader (TKT-01M29BW9QF). The `Sources` addition is reusable by serve's startup log line (CCBYX), which could print the effective sources at debug level.

### Settled in grooming (2026-09-24)

- Read internal/config before deciding the default config path. If nothing is set there: `--config`, then KETJU_CONFIG, then ./config.toml. A relative data_dir resolves against the config file's directory.
