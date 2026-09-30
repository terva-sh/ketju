---
schema: 3
id: TKT-01M29CXK2HYBSGKYBBRHJ5NA0T
title: Build the admin site settings screen
type: task
status: ready
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
updated_at: 2026-09-30T08:56:23Z
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
- [ ] GET and PUT /api/v1/admin/settings are admin-only: a moderator by role and a member who holds moderate through a group are 403 (TestSettingsAreAdminOnly)
- [ ] site_name reaches /me for every viewer, the shell title and the three wordmarks (TopBar, PhoneTopBar, ApplicantShell) at the cost of one /me statement, and TestMeBudget says so (TestShellInlinesMe, TestMeBudget, Shell.test.tsx)
- [ ] The screen lives under src/screens/admin, TestTheAdminBundleIsSeparate passes, the reader total is recorded against 47415 B within 60000 B, and the dist from go generate ./internal/web/spa is committed (SettingsScreen.test.tsx)

## Implementation plan

Groomed 2026-09-29. Refreshed 2026-09-30 against main at 5124209.

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

Added in the 2026-09-30 refresh:
- **The settings screen stays admin-only under the forum-grant decision.** The maintainer decided on 2026-09-30 that thread moderation follows the forum grant, and that "Admin screens and account actions keep their role gates" (docs/08 "Decided 2026-09-30", on PR #208's branch; TKT-01M3RQP0TG).
- **A thread's own slow mode overrides the site default, 0 included** (TKT-01M29CRJ50, PR #188; `settings.SlowModeDefaultSeconds`). The screen's doc line for `slow_mode.default_seconds` comes from the registry and already says it is the default. A test of the default uses a thread with no `thread_slow_mode` row.

Engineering choices made in the refresh, each with the alternative that lost:
- **`site_name` rides on `/me`, and the shell title reads it from there.** `ShellPage` in `internal/web/api/shell.go` already calls `users.Service.Me` on every page, so `Page.Title` takes `me.SiteName` and costs no statement of its own. `/me` pays one more statement, a `settings.SiteName.In` read, and `TestMeBudget` is updated to say so. An in-process cache of `site_name`, cleared by `SetBy`, lost: `ketju admin setting set` runs in another process and would not clear it, so the name would not change without a restart, which AC1 forbids.
- **The PUT's refusal stays a plain `validation-failed`, with no field errors.** docs/07 "Field errors on `validation-failed`" now exists, but it is reserved for forms, schemas, application decisions and handle changes ("No other body has it"). A code such as `out_of_range` would tell the client less than the registry's `allowed` text, which the listing already carries, and one PUT names one key, so there is no field to attribute.

### What changed since the plan was written

Read from main at 5124209:
- **The registry holds twelve keys** (`internal/settings/settings.go`): `site_name`, `registration.mode`, `registration.allow_local_passwords`, `slow_mode.default_seconds`, `posts.edit_window_seconds`, `posts.delete_window_seconds`, `threads.rename_window_seconds` (TKT-01M29CKRH9, PR #193), `posts.grace_edit_seconds`, `public_edit_history`, `flags.auto_hide_threshold`, `agents.write_burst` and `agents.write_refill_seconds` (TKT-01M29P9RSW, with `between(1, AgentRefillMaxSeconds)`). The screen lists whatever the registry holds, so none needs a client change.
- **`settings.SetBy` already takes the IP.** `settings.Change` has `Actor`, `Details`, `IP` and `At`, and `SetBy` writes `setting.changed` with `from` and `to` through `audit.Append` in the same transaction. The earlier step 1's "the audit helper's `By` carries the IP" is already true. This ticket passes `web.ClientIP` as `Change.IP`.
- **`settings.Value` does not carry who last changed a key.** `current` reads the row (`dbq.Setting{Key, ValueJson, UpdatedAt, UpdatedBy}`) and drops `UpdatedAt` and `UpdatedBy`. `Value` gains both.
- **`site_name` is still read by nothing.** `ShellPage` hard-codes `Title: "ketju"`, and `internal/web/shell.go` has the same fallback. The wordmark "Ketju" is hard-coded in `TopBar.tsx`, `PhoneTopBar.tsx` and `ApplicantShell.tsx` (`web/client/src/shell/`). The applicant shell is new since the plan. `Shell.test.tsx` asserts "Ketju" at two places.
- `dto.Me` now carries `queue` (TKT-01M29CRJ4E), and an agent authenticated by its credential gets `/me` too (TKT-01M3QH3X58, PR #189). Both are served by `users.Service.Me`, so `site_name` reaches both with one change.
- `sections.ts` already lists `/admin/settings` for admins, and `routes.tsx` routes it. `AdminRoot`'s `built` map has no entry, so the section says "This screen is not built yet".
- docs/07 "Moderation and admin" is now split into `####` subsections, one per area.

### Approach

1. **`internal/settings`**:
   - `Setting` gains `Choices []string`, set by `oneOf`, so the client can draw a select.
   - `Value` gains `UpdatedAt int64` and `UpdatedBy int64`, zero when the key has no row, filled by `current`.
2. **`internal/config`**: `Sections() []Section{Name, Reason string; Keys []string}`, a fixed table over the top-level sections of `Config`. Suggested reasons:
   - `oidc` holds provider secrets and is needed before anyone can sign in.
   - `bootstrap` decides who is admin before any admin exists.
   - `listen`, `site_url`, `data_dir`, `storage`, `log`, `limits` and `cookie_key_file` are read before the database opens.

   The keys come from the same flattening `Config.Entries()` (`source.go`) uses.
3. **`internal/service/sitesettings`** (new):
   - `Get(ctx, v) (dto.SiteSettings, error)` returns `settings.Current` plus `config.Sections()`, and resolves `UpdatedBy` through `member.AuthorsByID`.
   - `Set(ctx, v, by audit.By, key string, value json.RawMessage) (dto.SiteSetting, error)` calls `settings.SetBy` with `Change{Actor: by.Actor, IP: by.IP, At: now}`. `ErrUnknownKey` is `not-found`, and `ErrInvalidValue` is `validation-failed`.
   - Both are admin-only.
4. **Routes** in `internal/web/api/admin_settings.go`, registered from `registerAdmin`, each through `adminRoute` with `adminGate`: `GET /api/v1/admin/settings`, and `PUT /api/v1/admin/settings/{key}` with `{value}`. The PUT is JSON-only and write-limited. Both answers are `no-store`.
5. **`internal/dto`**:
   - `SiteSettings{Settings []SiteSetting; Config []ConfigSection}`.
   - `SiteSetting{Key, Doc, Kind, Allowed string; Choices []string; Default, Value json.RawMessage; IsDefault bool; UpdatedAt *Millis; UpdatedBy *Author}`, with `UpdatedAt` and `UpdatedBy` null for a key with no row, or one the command line changed.
   - `ConfigSection{Name, Reason string; Keys []string}`.
   - The golden `settings.admin.json` has one key changed by an admin, one at its default, and one changed from the command line, so the nullable fields appear both null and set.
   - `dto.Me` gains `SiteName string` (`site_name`), and the `me.*.json` goldens gain it.
6. **`site_name` wherever it shows**:
   - `users.Service.Me` fills `SiteName` through `settings.SiteName.In` in its read, for every viewer: anonymous, pre-user, member and agent. Update the statement count in the `Me` doc comment, in `TestMeBudget` and `TestMeBudgetWhenTouchIsDue`, and in docs/07's `GET /me` wording.
   - `ShellPage` sets `Page.Title` from `me.SiteName`. The `internal/web/shell.go` fallback stays "ketju", because it runs with no database.
   - `TopBar.tsx`, `PhoneTopBar.tsx` and `ApplicantShell.tsx` render `viewer.value.site_name`, falling back to "Ketju" when it is empty. That is reader code, a few bytes.
7. **`web/client/src/screens/admin/SettingsScreen.tsx`**, registered in `AdminRoot`'s `built` map under `admin-settings`:
   - One field per key, grouped by the key's prefix (`posts.`, `flags.`, `registration.`, `threads.`, `agents.`, …), with the doc line, "default: X", and the `allowed` text.
   - Each field saves itself: Save, then an inline "Saved" or the refusal with the `allowed` text.
   - "Saved" follows the rule settled for the agent limits form (TKT-01M3QV39H4): editing the field clears it, and an answer shows it only when the field still holds what was sent.
   - A reset link restores the default.
   - "Last changed" shows who and when, or "from the command line" when `UpdatedBy` is null and `UpdatedAt` is set.
   - Below the fields, a "Set in config.toml" section lists each section with its reason and the line "Change these in config.toml and restart ketju".
8. **Docs**:
   - A new `####` subsection under docs/07 "Moderation and admin", "Site settings (TKT-01M29CXK2H)", documents both endpoints.
   - docs/07's `GET /me` wording gains `site_name`.
   - docs/04 "settings" notes that the page size default is a constant, not a key.
9. Run `go generate ./internal/web/spa`, commit `internal/web/spa/dist`, and run the budget check. The reader delta is `site_name` in three wordmarks. Record the total against 47415 B.

### Tests

- `TestSettingsScreenListsEveryKey`: the GET lists exactly `settings.All()`, twelve keys on main, with `choices` for `registration.mode`.
- `TestSettingTakesEffectWithoutRestart`: a PUT changes the behaviour of the next request in the same server for each of these keys:
  - `public_edit_history`: history is refused.
  - `registration.mode`: registration switches.
  - `slow_mode.default_seconds`: a post in a thread with no `thread_slow_mode` row is slowed.
  - `site_name`: the shell title and `/me` change.
- `TestSettingChangeAudited`: the row names the admin, the IP and the old and new values. A failing audit insert leaves the setting unchanged. The listing shows the admin and time as last changed.
- `TestInvalidSettingRefused`: an out-of-range integer, an unknown choice, the wrong type, `invite`, and an `agents.write_refill_seconds` over 604800 are each `validation-failed`, and the listing's `allowed` names the range. An unknown key is `not-found`.
- `TestEverySectionHasAReason`: every top-level key in `Config.Entries()` belongs to a section with a reason.
- `TestSettingsAreAdminOnly`: a moderator by role is 403, and so is a member who holds `moderate` through a group.
- `TestMeBudget` and `TestMeBudgetWhenTouchIsDue` count one more statement. `TestShellInlinesMe` sees `site_name` in the inlined `/me` and in the title.
- Goldens. Client: `SettingsScreen.test.tsx` covers rendering by kind, a select for a key with choices, the refusal message, the saved status clearing on edit, and the config section. `Shell.test.tsx` checks that the wordmark shows `site_name`. `TestTheAdminBundleIsSeparate` stays green.

### Out of scope

- New keys. Each arrives with its feature, and the screen shows it without a change.
- Editing `config.toml` from the browser (the split above).
- The member's own page-size preference (`/settings`, docs/06; TKT-01M29CXK4E, phase 4).
- An agent's own limits, which override the `agents.*` defaults: the users screen (TKT-01M29P9RSW, done).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:56:23Z

Plan refreshed against main at 5124209 on 2026-09-30. The Settled items are carried verbatim; new ones follow them. Changes: (1) The registry now holds twelve keys. threads.rename_window_seconds (#193) and agents.write_burst and agents.write_refill_seconds are new, and the screen lists them with no client change. (2) settings.Change already carries the IP, so the old step 1 about the audit helper is resolved. settings.Value gains UpdatedAt and UpdatedBy, which current() used to drop. (3) site_name rides on /me: ShellPage already calls Me, so the title costs nothing more, and /me pays one statement (TestMeBudget). An in-process cache lost because a CLI setting change could not clear it. It reaches agents' /me (#189) too. (4) ApplicantShell has a third hard-coded wordmark. (5) The slow_mode default test uses a thread with no thread_slow_mode row (#188). (6) The field saves itself, and 'Saved' follows TKT-01M3QV39H4's rule. (7) The PUT's refusal stays plain validation-failed, because docs/07 reserves field errors for forms, schemas, applications and handles. (8) The maintainer's decision of 2026-09-30 keeps admin screens on their role gates. Three criteria are added: admin-only, site_name, and the admin chunk and dist.
