---
schema: 3
id: TKT-01M29BYYJGYDT6KK729749ED25
title: Ship ketju config check
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
dependencies:
  - TKT-01M29BW9QFZ2C7Q24KN1JW5MFW
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
  - ref: pr:106
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:13:37Z
updated_at: 2026-09-28T08:23:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

A subcommand that validates the configuration and prints the effective values with secrets redacted, exiting non-zero on a problem so it works in a deployment script.

This is the command an operator runs before restarting, and the one they paste into a support thread. Redaction has to be by construction rather than a denylist of key names, because the first secret somebody adds with an unexpected name is the one that leaks.

## Acceptance criteria

- [x] Exit code is non-zero on any validation failure and the message names the key
- [x] Effective output shows merged file, environment, and default values, and says which layer each came from
- [x] No secret value appears in the output, including one loaded from a file, tested with a config carrying a known sentinel string

## Implementation plan

Revised at claim (2026-09-28) from the code as it stands after PRs #97, #100, #102 and #103.

### Approach
1. `cmd/ketju/config.go`: `ketju config check [--config PATH] [--json]`. The path is `configPath` from serve.go (--config, then KETJU_CONFIG, then ./config.toml). It calls `config.Load(path, getenv)`, exactly as serve does.
2. Exit codes follow the CLI convention of #97 and #100: 0 valid (warnings allowed), 1 the config has problems (`errConfig`, each problem named by its key), 2 usage, 4 the file is missing or unreadable. `Load` now wraps `fs.ErrNotExist`, and go-toml's fallback error becomes a `(file)` problem, so every "the file is wrong" outcome is a `*config.Error` and exits 1.
3. Provenance in `internal/config/source.go`: `Layer` (default, file, env, env-file, secret-file), `Source{Layer, From}`, `Config.Source(key)`, and `Config.Entries()`, which walks the struct by reflection so a new key is listed without anyone remembering to list it. Providers are keyed `oidc.providers.<key>.<field>`, as problems and overrides already name them. File sources come from the raw decode, env sources from `lookup` (which now returns a Source), secret-file from `resolveSecrets`.
4. Redaction by construction: an Entry's value is the typed field, so a secret is a `config.Secret` and every printer (text via MarshalText, JSON via MarshalJSON, %v via String) redacts it. `cmd/ketju/config.go` never calls `.Value()`, and a test reads the source to hold that.
5. The report prints every key with value and source, then warnings, then the KETJU_* variables this build reads (set or not, never a value), then the runtime settings from `settings.All()`, named as not read from the file.
6. Warnings (legal, costly, printed, exit 0; serve logs them): fsync = never, insecure_skip_issuer_check, auto_approve while approval mode does not exist, and the existing writable secret file. Written into docs/02 "Configuration".
7. Every settings key written into config.toml is refused naming the settings table: movedKeys gains posts, flags and public_edit_history, with a test over `settings.All()`.
8. A syntax error names the key on its line (under its table header) instead of `(file)` where it can.

### Tests
- AC1: `TestConfigCheckFailsNamingKey` (table: bad fsync, missing data_dir, moved and runtime keys, unknown key, response_cache_mb, admin subjects and emails, a bad variable, malformed TOML), text and JSON; `TestConfigCheckExitCodes`; `TestConfigCheckAsTheBinary` through the real main under KETJU_AS_MAIN.
- AC2: `TestConfigCheckShowsLayers` with a golden (`cmd/ketju/testdata/config-check.golden`, `-update`), plus `TestSourcesNameEachLayer` and `TestEntriesListEveryKey` in internal/config.
- AC3: `TestConfigCheckNeverPrintsSecret` with the sentinel inline, in client_secret_file, in KETJU_OIDC_A_CLIENT_SECRET and in its _FILE companion, on success and failure, text and JSON, stdout and stderr; `TestConfigCheckNeverReadsASecret` (static); `TestEntriesCarryASecretAsASecret`.

### Design
docs/02-architecture.md "Configuration"; docs/05 "Provider configuration"; the internal/config package doc.

### Settled in grooming (2026-09-24)

- Read internal/config before deciding the default config path. If nothing is set there: `--config`, then KETJU_CONFIG, then ./config.toml. A relative data_dir resolves against the config file's directory.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T08:22:27Z

Gaps settled under docs/12 "Gaps the design leaves open", written into docs/02 "Configuration" in the same PR:

- Exit codes: 0 valid (with or without warnings), 1 problems, 2 usage, 4 file missing or unreadable. Matches admin promote and reindex. Alternative considered: reuse errUsage (2) for an invalid config, as the pre-groom plan offered; lost because 2 means "you invoked me wrong" everywhere else, and a deploy script needs to tell a bad file from a bad command line.
- Errors versus warnings. docs/02 already makes every refusal an error. Warnings are the configurations that are legal, have a use, and cost something: fsync = "never" (validate.go already said config check and the startup line are for it), insecure_skip_issuer_check (docs/09), auto_approve while approval-mode registration does not exist (PR #102 made it not open registration, so it has no effect; TKT-01M29CKRE9 should remove the warning when it lands), and the existing writable client_secret_file. Considered and not added: a warning for no providers or no bootstrap admins (docs/05 gives `ketju admin promote` as the way in, so neither is a misconfiguration), and for an inline client_secret (the file already allows it; docs/05 shows it as an option).
- No new config keys. The runtime settings are listed by name from settings.All() and refused by name when written into config.toml; posts, flags and public_edit_history joined movedKeys, which only changes the message from "unknown key" to "lives in the settings table".
- Output format: one `key = value  # source` line per key in struct order, rather than a {key: {value, source}} map in JSON; --json emits a list so the order survives.

Mutations tried against the tests, each caught: printing Secret.Value() in formatValue (4 tests fail, including the static one); dropping the env source record (TestConfigCheckShowsLayers, TestSourcesNameEachLayer); dropping warn() from Load (TestConfigCheckExitCodes, TestWarningsNameTheirKeyAndDoNotFail); mapping errConfig to 4 (TestConfigCheckFailsNamingKey, TestConfigCheckAsTheBinary).

## Summary

ketju config check [--config PATH] [--json] loads the config as serve does and prints every key with its value and source layer (file, env, env-file, secret-file, default), the warnings, the KETJU_* variables this build reads, and the runtime settings. Exit 0 valid, 1 problems named by key, 2 usage, 4 missing or unreadable file. Secrets are redacted by the Secret type; the command never calls Value. internal/config gained Source/Entries, key-naming for syntax errors, warnings for fsync=never, insecure_skip_issuer_check and auto_approve, and refuses every settings key by name. The error/warning split is written into docs/02 Configuration.
