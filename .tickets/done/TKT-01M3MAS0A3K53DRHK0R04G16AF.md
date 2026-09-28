---
schema: 3
id: TKT-01M3MAS0A3K53DRHK0R04G16AF
title: Add ketju admin setting so a fresh install can open registration
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:40:14Z
updated_at: 2026-09-28T16:18:15Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

A fresh Phase 1 install cannot register anyone. `registration.mode` defaults to `approval` (internal/settings/settings.go), and Phase 1 answers approval mode with a 501 page (TKT-01M29CKRG9 builds it in Phase 2). Settings live only in the database, and nothing in Phase 1 can change one: the admin settings screen is Phase 2 (TKT-01M29CXK2H) and the settings endpoints are Phase 4 (TKT-01M29CCC0X). The first admin cannot register either, so `bootstrap.admin_emails` never applies. The e2e seed never hit this, because it writes users directly. Found deploying the dogfood instance (TKT-01M3M9S7S2).

### Decided by the maintainer on 2026-09-28
"CLI to set a setting": `ketju admin setting` with `set KEY VALUE`, `get KEY` and `list`. The default stays `approval`, so a public install is closed until an operator opens it.

### Shape
- Follow `ketju admin promote` (PR #97): `--root DIR`, the data-directory lock (refuse while serve holds it), and exit codes 1 domain, 2 usage, 4 lock or database.
- Validate every value through the settings registry: unknown key or invalid value is exit 1, naming the key and the allowed values.
- Write an audit row in the same transaction, as Promote does. There is no actor for the CLI, so record it the way `admin promote` does.
- Add a settings writer (`Key.Set`, or a registry-level set by name) in internal/settings, so the CLI does not write the table around the registry.
- Name the command in README's first-run steps and in docs/02 or docs/05.

## Acceptance criteria

- [x] ketju admin setting set registration.mode open opens registration on a stopped instance, and serve then admits a new member
- [x] Every value is validated through the settings registry; an unknown key or bad value exits 1 naming the allowed values
- [x] It refuses while serve holds the data-directory lock and follows admin promote's exit codes
- [x] Each change writes an audit row in the same transaction
- [x] README's first-run steps name the command

## Implementation plan

Read on 2026-09-28 against main 2b1c620.

### What is already there
- `settings.Set(ctx, d, name, json.RawMessage, actor, now)` already validates by the registry and writes the row and a `setting.changed` audit row in one transaction. `TestWritesGoThroughSet` already forbids `PutSetting` outside internal/settings. So the writer exists; what is missing is a way to record `"via": "cli"` in the audit row, a way to turn a command-line word into the key's JSON, and an untyped read for `get` and `list`.
- `ketju admin promote` does not take the data-directory lock (it is settled as safe beside serve). The ticket's criterion asks `setting set` to refuse while serve holds it, so `set` takes `lock.Acquire(root, "admin setting")` as reindex does; exit 4 on refusal, as reindex's.

### internal/settings
- `SetBy(ctx, d, name, value, Change{Actor, Details, At})`; `Set` becomes a call to it. Details go into the audit row beside `from` and `to`, as perm.Promotion's do beside `role`.
- `FromText(name, text) (json.RawMessage, error)`: a text key takes the word as the string itself (no quotes needed); an integer or boolean key takes it as JSON (`30`, `true`). ErrUnknownKey for an unknown name. Validation still happens in Set.
- Each key gets an `Allowed` description (`one of "open", "approval"`; `a whole number, 0 or more`; `true or false`; `text of 1 to 100 characters`), carried by the validators, and ErrInvalidValue messages end with it so the CLI names the allowed values. `Setting` gains `Kind` and `Allowed`.
- `Lookup(ctx, d, name)` and `Current(ctx, d)` return `Value{Setting, JSON, IsDefault}` read in one transaction; a stored value that no longer decodes is an error naming the key, as `Key.Get` is. IsDefault compares the canonical value with the default.

### cmd/ketju/admin.go
- `ketju admin setting set|get|list [--root DIR]`. Same "has no database" refusal as promote; set also takes the lock. Unknown key and invalid value are exit 1 (errNotFound for the unknown key, a new errInvalid for the value; both map to 1) naming the known keys or the allowed values. get prints the value in the form set takes; list prints `key = JSON  # default|set, default D` and the doc on the next line. get/list read without the lock (read-only, safe beside serve) and without migrating.

### Tests
- unit tests in internal/settings for FromText, SetBy details, Lookup/Current, the Allowed strings.
- cmd/ketju table tests for set/get/list and their refusals, including the lock held.
- End to end: the binary (KETJU_AS_MAIN) runs `ketju admin setting set --root DATA registration.mode open` on a stopped instance; serve then starts on that directory and a new identity completes the callback and POST /auth/register, and a users row exists.

### Docs
README gains "First run" steps naming the command; docs/05 (registration policy paragraph) and docs/02 Configuration name it; docs/04 settings section mentions the CLI.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T16:18:12Z

Decisions taken while building, and what lost:

- **How VALUE is read.** By the key's kind, which `define` now records: a text key takes the words as typed (`open`, `'My forum'`), an integer or boolean key takes JSON (`30`, `true`). Considered: JSON for every key, which makes the one command the ticket exists for `registration.mode '"open"'` and fails on the shell's quoting; and guessing the type from the word, which would store `true` as a boolean for a text key. A JSON-quoted `"open"` for a text key is refused, naming what the key takes. Said in `ketju admin setting -h` and in docs/05.
- **The writer.** `settings.Set` already validated through the registry and wrote the row and the audit row in one transaction. Added `settings.SetBy` with a `Change{Actor, Details, At}` so the CLI records `"via": "cli"` with no actor, as `admin promote` does; `Set` calls it, so no caller changed. Details sit beside `from`/`to` and cannot replace them. `TestWritesGoThroughSet` is unchanged and still holds: the CLI never calls `PutSetting`.
- **Naming the allowed values.** Each validator now carries its own description (`one of "open", "approval"`, `a whole number, 1 or more`, `true or false`, `text of 1 to 100 characters`), and every ErrInvalidValue ends `; it takes ...`. ErrUnknownKey lists every key. Considered: printing the key's doc string on failure, which for integers says nothing about the lower bound.
- **The lock.** `admin promote` takes no lock (settled as safe beside serve), but this ticket's criterion asks set to refuse while serve holds the directory, so set takes `lock.Acquire(root, "admin setting")` before opening the database, as reindex does, and exits 4 on refusal. get and list only read: they take no lock and do not migrate, so they may run beside serve and report what it sees.
- **Exit codes.** Unknown key is errNotFound (1); a value the key refuses is a new errInvalidValue (1); usage 2; no database, a held lock, or a database failure 4. `needDatabase`/`openDatabase` were factored out of promote so the three commands refuse a mistyped --root the same way.
- **Output.** set prints `key = JSON` as stored; get prints the value in the form set takes (so it round-trips); list prints `key = JSON  # default` or `# set; the default is D`, then `kind: allowed`, then the doc string. A row that sets a key back to its default counts as the default.
- **End to end.** `TestAdminSettingOpensRegistration` runs the binary (KETJU_AS_MAIN) through README's first-run steps: serve once, get says approval, set refuses beside serve (exit 4), stop, set invite (exit 1), set open (exit 0), start serve, a new identity reaches the handle screen, registers as `newcomer`, and gets a session cookie; the users row exists after shutdown.

Mutations tried: set without the lock (caught by TestAdminSettingWhileServeHoldsTheDirectory and the e2e); set that reports success but writes approval (caught by the e2e: the callback answers 501 instead of the handle screen).

## Summary

ketju admin setting set|get|list [--root DIR] reads and changes runtime settings through the registry. set validates by key kind (text as typed, integer and boolean as JSON), refuses while serve holds the data directory, and writes the row and a setting.changed audit row (no actor, via cli) in one transaction via the new settings.SetBy. Unknown key and bad value exit 1 naming the keys or what the key takes. README gains First run steps; docs/02, 04 and 05 name the command. The default stays approval.
