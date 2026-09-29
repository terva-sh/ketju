---
schema: 3
id: TKT-01M29CXK2HYBSGKYBBRHJ5NA0T
title: Build the admin site settings screen
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M3NRYCP5BSA717S1HANWY1AA
blocks_on: none
references:
  - ref: design:config
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-29T05:12:48Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The runtime settings from the `settings` table: site name, registration mode, edit and delete windows, the flag auto-hide threshold, `public_edit_history`, default page size, and slow-mode defaults.

The screen also has to explain the split. An admin looking for the OIDC client secret here should be told it lives in config.toml and why, rather than concluding the feature is missing.

## Acceptance criteria

- [ ] The screen lists every key in settings.All() and edits each one, and a change applies to the next request without a restart, site_name included (TestSettingsScreenListsEveryKey, TestSettingTakesEffectWithoutRestart)
- [ ] Each change writes setting.changed with the admin, the IP and the old and new values in the same transaction, and the screen shows who last changed a key and when (TestSettingChangeAudited)
- [ ] The screen names each config.toml section with the reason it is not editable here, and shows no values (TestEverySectionHasAReason)
- [ ] An invalid value is validation-failed, and the screen shows the key's allowed range or choices from the registry (TestInvalidSettingRefused)

## Implementation plan

### Settled in grooming

- **The split.** `config.toml` holds what must be readable before the database exists. Everything an admin changes at runtime lives in `settings`, and a runtime key written into `config.toml` is refused at startup (docs/02 "Configuration", docs/05 "Provider configuration").
- The key list is closed and lives in `internal/settings`. `settings.SetBy` refuses an unknown key or an ill-typed or out-of-range value, and writes `setting.changed` with the old and new values in the same transaction. A key with no row reads as its default. docs/04 "settings" lists the keys, and `TestKeysDocumented` holds that list to the code.
- `registration.mode` takes `open` or `approval`. `invite` is refused until its phase (docs/04, docs/05).
- **The default page size is not a runtime setting.** The ticket's description lists it, but the design overrides that: 25 was decided once because it appears in shared URLs, so the API and the client treat it as a constant (docs/10 item 13, docs/07 "Preact client"). The per-user preference of 10 to 50 is a member setting (docs/06), not a site one.
- The flag threshold defaults to 3 (docs/08, decided 2026-09-28, TKT-01M29CP2PJ). The agent bucket keys come with TKT-01M29P9RSW. The screen lists whatever the registry holds, so a key added later appears without a client change.
- The gate, the audit helper and the admin scaffold come from TKT-01M3NRYCP5. The screen is in the lazy admin chunk. The built client is committed: run `go generate ./internal/web/spa`.

Engineering choices made here, each with the alternative that lost:
- **The screen is generated from the registry.** `GET /api/v1/admin/settings` returns each key's name, doc, kind, `allowed` text, default and current value. The client draws a text field, a number field or a checkbox by kind, or a select when `allowed` is a closed list. A hand-built form per key lost because every new key would need a client change, and `TestKeysDocumented` would not catch a key missing from the screen.
- **The range message comes from the registry's `allowed` words** ("a whole number, 1 or more"), shown beside the field and again when the server answers `validation-failed`. Problem titles are fixed text (docs/07 "Error catalogue"), so the server cannot put the range in the error. Adding an `invalid-params` member to every problem lost because it changes the catalogue's shape for one screen.
- **`site_name` is wired through**, because nothing reads it today (`internal/web/api/shell.go` hard-codes "ketju", and the top bars say "Ketju"), and AC1 requires every setting to take effect without a restart. `ShellPage` reads it into `Page.Title` on each request, and `dto.Me` gains `site_name`, which the two wordmarks show.
- **config.toml keys are named, not shown.** The screen lists each `config.toml` section with the reason it lives in the file, and no values. `ketju config check` already shows values with secrets redacted, and a screen that echoes paths and issuer URLs to the browser gains nothing from it.

### Approach

1. `internal/settings`: `Setting` gains `Choices []string`, set by `oneOf`, so the client can draw a select. The audit helper's `By` carries the IP (TKT-01M3NRYCP5).
2. `internal/config`: `Sections() []Section{Name, Reason string; Keys []string}`, a fixed table over the top-level sections. Suggested reasons: `oidc` holds provider secrets and is needed before anyone can sign in. `bootstrap` decides who is admin before any admin exists. `listen`, `site_url`, `data_dir`, `storage`, `log`, `limits` and `cookie_key_file` are read before the database opens. The keys come from the same flattening `Entries()` uses.
3. `internal/service/sitesettings` (new): `Get(ctx, v) (dto.SiteSettings, error)` returns `settings.Current` plus `config.Sections()`. `Set(ctx, v, by, key, value json.RawMessage) (dto.SiteSetting, error)` calls `settings.SetBy`: `ErrUnknownKey` is `not-found` and `ErrInvalidValue` is `validation-failed`. Both are admin-gated.
4. Routes in `internal/web/api/admin_settings.go`: `GET /api/v1/admin/settings`, and `PUT /api/v1/admin/settings/{key}` with `{value}`. The PUT is JSON-only and write-limited. Both are `no-store`.
5. `internal/dto`: `SiteSettings{Settings []SiteSetting; Config []ConfigSection}`, with `SiteSetting{Key, Doc, Kind, Allowed string; Choices []string; Default, Value json.RawMessage; IsDefault bool; UpdatedAt int64; UpdatedBy *Author}` and `ConfigSection{Name, Reason string; Keys []string}`. Read `updated_at` and `updated_by` from the row. Golden `settings.admin.json`, with one key changed and one at its default. `me.*.json` gain `site_name`.
6. `site_name` everywhere it shows: `api.ShellPage` reads `settings.SiteName.Get` for `Page.Title`. `users.Service.Me` fills `SiteName`, one more read on the settings table, so update the statement count in docs/07's GET /me row. `TopBar.tsx` and `PhoneTopBar.tsx` render `viewer.value.site_name`. That is reader code, a few bytes.
7. `web/client/src/screens/admin/SettingsScreen.tsx`: one field per key, grouped by the key's prefix (`posts.`, `flags.`, `registration.`, …), with the doc line, "default: X", and the `allowed` text. A field saves itself: Save, then an inline "Saved" or the refusal with the `allowed` text. A reset link restores the default. Last changed shows by whom and when. Below, a "Set in config.toml" section lists each section with its reason and the line "Change these in config.toml and restart ketju".
8. Docs: docs/07 "Moderation and admin" documents both endpoints. The GET /me row gains `site_name`. docs/04 "settings" notes that the page size default is a constant, not a key.
9. Run `go generate ./internal/web/spa`, commit `dist`, and run the budget check. The reader delta is `site_name` in two wordmarks. Record the total.

### Tests

- `TestSettingsScreenListsEveryKey`: the GET lists exactly `settings.All()`.
- `TestSettingTakesEffectWithoutRestart`: for `public_edit_history`, `registration.mode`, `slow_mode.default_seconds` and `site_name`, a PUT changes the behaviour of the next request in the same server: history is refused, registration switches, a post is slowed, and the shell title and `/me` change.
- `TestSettingChangeAudited`: the row names the admin, the IP and the old and new values. A failing audit insert leaves the setting unchanged.
- `TestInvalidSettingRefused`: an out-of-range integer, an unknown choice, the wrong type and `invite` are each `validation-failed`, and the listing's `allowed` names the range. An unknown key is `not-found`.
- `TestEverySectionHasAReason`: every top-level key in `Config.Entries()` belongs to a section with a reason.
- `TestSettingsAreAdminOnly`: a moderator is 403.
- Goldens. Client: `SettingsScreen.test.tsx` covers rendering by kind, the refusal message and the config section. `Shell.test.tsx` checks the wordmark shows `site_name`. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- New keys. Each arrives with its feature, and the screen shows it without a change.
- Editing `config.toml` from the browser (the split above).
- The member's own page-size preference (`/settings`, docs/06).
