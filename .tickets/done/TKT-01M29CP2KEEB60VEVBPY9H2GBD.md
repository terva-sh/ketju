---
schema: 3
id: TKT-01M29CP2KEEB60VEVBPY9H2GBD
title: Implement the declarative form schema and server-side validation
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
blocks_on: none
references:
  - ref: design:forms
    path: docs/05-identity-and-accounts.md
  - ref: pr:150
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T16:02:50Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-cp2ke
  name: ""
extensions: {}
---

## Description

The field list from docs/05 as data: `text`, `textarea`, `email`, `checkbox`, `select`, and `markdown_notice`, with `required`, `max`, `from_claim`, and `locked`. Validation runs server-side from the stored schema.

The form is data rather than code because three renderers need it: the Preact client, the future `html/template` client, and the validator. Hand-written per-field validation would have to be written three times and would drift twice.

`locked` with `from_claim` means a field is prefilled from the IdP and cannot be edited, so the server must reject a submitted value for it rather than trusting the form to have disabled the input.

## Acceptance criteria

- [x] Answers validate through one per-type table in internal/forms covering all six types, with no code naming a field key; a table-driven test exercises each type's accepted and refused values
- [x] A locked field ignores any submitted value and takes the claim value, tested with a differing value, no value, and no claim (stored empty, not required-checked)
- [x] A missing required field answers 422 validation-failed carrying errors: [{field, code: required}], pinned by a golden problem body whose title stays the catalogue's
- [x] An unknown field type in a stored schema makes loading that version fail with an error-level log naming the form and version and a 500, never a form without the field
- [x] Saving a form version is admin-only (anonymous 401, member and moderator 403), requires base_version to equal the active version (else 409), appends version N+1, moves active_version, and writes a form.version_created audit row in the same transaction
- [x] A schema that breaks the rules is refused with 422 and path-scoped errors and writes nothing, and the preview route renders a draft without writing
- [x] The seeded registration form parses under the rules, and docs/05 and docs/07 describe the properties, caps, error codes and admin routes

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. Nothing parses or stores a form yet. `internal/problem` writes only type, title and status. `render.Render(source)` in `internal/render/render.go` renders markdown with no thread.

### Settled in grooming

- **The field types are those of docs/05 "Forms"**: `text`, `textarea`, `email`, `checkbox`, `select` (with `options` and `multiple`), and `markdown_notice` (display only). The properties are `required`, `max`, `from_claim` and `locked`. The schema sits in `form_versions.schema_json` as `{title, intro_markdown, fields}` (docs/05).
- **The property is `from_claim`, singular.** docs/05 step 2 writes `from_claims`, but the schema example and this ticket write `from_claim`. The step 2 wording is corrected in docs/05 by this ticket.
- **Validation runs server-side from the schema** (docs/05). The Preact client renders the same schema, and a future `html/template` client must be able to render it with no new work (docs/05, docs/07 "One service layer, two presentation layers").
- **A locked field is refused at the server**, which ignores any submitted value and stores the claim (this ticket's description).
- **Admins edit the form, and each save writes a new version** that leaves the old one intact (docs/05, docs/07 "Moderation and admin" lists form versions under the admin routes). The admin UI is TKT-01M29CP2P1. The server half it calls is built here.
- **Remote images are rendered as links** (docs/07, decided 2026-09-28). `intro_markdown` and a `markdown_notice` go through `internal/render`, so they get that rule and the sanitizer without extra work.
- **No error body carries request text, and titles are fixed** (docs/07 "Error catalogue").

Engineering decisions taken here, with the alternative that lost:
- **A pure package, `internal/forms`,** holds the schema types, `Parse` and `Validate`. It has no database and no HTTP, so the validator, the JSON API and a future HTML client share one copy. `internal/domain` lost because a validator with its own error vocabulary is larger than the small rules kept there. `internal/service/forms` lost because the HTML renderer would then drag in the store to read a struct.
- **Validation is one table of field types**: `types map[string]fieldType`, where each entry has a schema check and an answer check. `Validate` walks the schema and dispatches through the table. No code anywhere names a field key. A switch in `Validate` lost because adding a type would mean editing the loop, which is the per-field code the criterion forbids.
- **Field-scoped errors are an RFC 9457 extension member.** A `validation-failed` body gains `errors: [{field, code}]`. `field` is a schema key, or a path such as `fields.2.options` for a schema error, and `code` is from a closed list. It never interpolates request text. A new problem type per field lost, because codes are API surface and every new code is a breaking change.
- **The strict decode refuses unknown properties** (`DisallowUnknownFields`) and trailing data. A schema written by a newer build then fails loudly in an older one rather than losing a rule.
- **Stored schemas and submitted schemas fail differently.** A stored schema that fails to parse is `ErrCorruptSchema`: a 500 with an error-level log, because the data is broken. A schema submitted by an admin is `ErrInvalidSchema`: a 422, because the request is at fault.
- **Saving checks `base_version`.** It must equal the active version, or the save is `conflict`. Without the check, two admins editing at once would silently overwrite each other's change as a newer version.

### Approach

1. `internal/forms/schema.go`:
   - Types: `Schema{Title, IntroMarkdown string; Fields []Field}` and `Field{Key, Type, Label string; Required bool; Max int; Options []string; Multiple bool; FromClaim string; Locked bool; Markdown string}`. The JSON tags are `key`, `type`, `label`, `required`, `max`, `options`, `multiple`, `from_claim`, `locked` and `markdown`. `markdown` is the body of a `markdown_notice`, which docs/05 does not name.
   - `Parse(b []byte) (Schema, error)` decodes strictly and then runs the rules. On failure it returns `*SchemaError{Fields []FieldError}`, which wraps `ErrInvalidSchema`.
   - The rules:
     - `title` is 1–200 characters, and `intro_markdown` is at most 10000.
     - There are 1–50 fields.
     - A key matches `^[a-z][a-z0-9_]{0,39}$` and is unique.
     - The type is known.
     - A label is 1–500 characters. A `markdown_notice` may omit it, and requires `markdown` of at most 10000.
     - `options` appear only on `select`: 1–100 of them, each 1–200 characters and unique. `multiple` also appears only on `select`.
     - `max` appears only on `text`, `textarea` and `email`, from 1 to the type's cap: 200, 5000 and 254.
     - `from_claim` appears only on `text` and `email`, and is one of `name`, `email` or `preferred_username`.
     - `locked` requires `from_claim`.
     - `required` is not allowed on `markdown_notice`.
2. `internal/forms/validate.go`:
   - `Validate(s Schema, answers map[string]json.RawMessage, claims Claims) (Answers, error)`. `Claims` is `map[string]string`, and `Answers` is `map[string]any`, normalized: strings trimmed, and locked fields replaced by their claim value.
   - The answer types, per field type:
     - `text` and `email` take a string with no control characters. An email must equal `net/mail.ParseAddress(v).Address`.
     - `textarea` takes a string. It allows `\n` and `\t`.
     - `checkbox` takes a bool. Required means it must be true.
     - A single `select` takes a string from `options`. A multiple one takes a unique string array, all from `options`, and required means at least one.
     - `markdown_notice` must be absent.
     - `max` counts runes.
   - A locked field ignores what was submitted and takes `claims[from_claim]`. When the claim is absent it stores an empty value and skips `required`, because the applicant cannot supply a value the IdP withheld.
   - A key in `answers` that no field names is `ErrUnknownAnswer`, mapped to bad-request: it is a client bug, since the client drew its inputs from the schema.
   - Field failures return `*AnswerError{Fields []FieldError}`, which wraps `ErrInvalidAnswers`.
   - The codes are `required`, `too_long`, `invalid_email`, `invalid_text`, `not_an_option` and `wrong_type`, listed in `forms.Codes`. Schema errors add `invalid_key`, `duplicate_key`, `unknown_type`, `not_allowed`, `out_of_range`, `too_many` and `unknown_property`.
3. `internal/problem`:
   - `FieldError{Field, Code string}`, and `Problem.Errors []FieldError` with the tag `json:"errors,omitempty"`.
   - `WriteError` includes the errors when `errors.As` finds an error with `FieldErrors() []FieldError`. `internal/forms` stays free of an import of `problem`, because it only implements the method.
   - `domain.go` maps `forms.ErrInvalidAnswers` and `forms.ErrInvalidSchema` to validation-failed, `forms.ErrUnknownAnswer` to bad-request, `formsvc.ErrStaleVersion` to conflict, and `formsvc.ErrNoForm` to not-found. `formsvc.ErrCorruptSchema` stays unmapped, so it is internal and logged at error level. It must not wrap `ErrInvalidSchema`, or it would map first.
4. `internal/store/db/queries/forms.sql`: `GetFormByKey`, `GetFormVersion`, `ListFormVersions` (with the creator's handle), `MaxFormVersion`, `InsertFormVersion` and `SetActiveFormVersion`. Regenerate dbq.
5. `internal/service/forms` (package `formsvc`, so it does not collide with `internal/forms`):
   - `Service{DB *db.DB; Now func() time.Time}`.
   - `Active(ctx, key) (Version, error)` and `Version(ctx, key or formID, n) (Version, error)` parse on load, so a corrupt row is `ErrCorruptSchema` wrapping the parse error, with the form key and version in the message.
   - `Versions(ctx, key)`.
   - `Create(ctx, viewer, key, baseVersion int, schemaJSON []byte) (Version, error)` runs in one `DB.Write`. It requires viewer.Member() with role admin, since authority lives in the service (docs/07 "Principles"). It re-reads the active version and returns `ErrStaleVersion` when base differs. Then it runs `forms.Parse`, re-marshals the parsed schema so storage is canonical, inserts version max+1 with created_by, updates `forms.active_version` and `updated_at`, and appends audit `form.version_created` `{key, version}` with the viewer as actor.
   - `Preview(ctx, viewer, schemaJSON)` parses and renders with no write, and is admin-only.
   - `ToDTO(v Version, prefill map[string]string) dto.Form` renders `intro_markdown` and each `markdown_notice` with `render.Render`.
6. `internal/dto`:
   - `Form{key, version, title, intro_html, fields []FormField}`.
   - `FormField{key, type, label, required, max, options, multiple, locked, from_claim, html}`, where `html` is set for a `markdown_notice` only.
   - `FormAdmin{key, title, active_version, versions []FormVersionInfo, form Form, schema json.RawMessage}`. The raw schema is what the editor edits; the rendered form is what it previews.
   - `FormVersionInfo{version, created_at, created_by *Author}`.
   - Goldens go in `internal/dto/testdata/golden/`, with nullable fields both null and set as the harness requires. Add the types to `web/client/src/api/dto.ts` and `dto-contract.ts`, or `contract.test.ts` fails. The additions are types only; run `go generate ./internal/web/spa` anyway and commit any diff.
7. `internal/web/api/forms.go`, wired in `api.New` with a `Forms *formsvc.Service` dependency. Each route answers `no-store`:
   - `GET /api/v1/admin/forms/{key}` returns `FormAdmin`.
   - `GET /api/v1/admin/forms/{key}/versions/{version}` returns `FormAdmin` with that version as `form` and `schema`.
   - `POST /api/v1/admin/forms/{key}/versions` takes `{base_version, schema}` and returns 201 `FormAdmin`.
   - `POST /api/v1/admin/forms/{key}/preview` takes `{schema}` and returns 200 `Form`, or 422 with errors.
   - Anonymous is `unauthenticated`. A member or moderator is `forbidden`. A pre-user session is refused before this by TKT-01M29CKRG9's allowlist.
8. `cmd/ketju/serve.go`: after `d.Migrate`, load `Active("registration")`. On an error, log at error level with the key and version, and keep serving: members can still use the forum, and the fault is stated at the moment the operator is looking.
9. Docs:
   - docs/05 "Forms": the property list, the caps, `markdown`, the locked-without-claim rule, and the `from_claims` wording fix.
   - docs/07: the admin form routes, and the `errors` extension on `validation-failed` with its code list.

### Tests

- `internal/forms`:
  - `TestEveryTypeValidatesFromTheTable` is table-driven over all six types. For each one it tests accepted values, and refused ones with the exact code: wrong JSON type, too long, a bad email, a control character, an option not listed, a duplicate in a multiple select, an unchecked required checkbox, and a value sent for a notice.
  - `TestANewSchemaNeedsNoCode` generates schemas with arbitrary keys over all six types and validates answers through `Validate` unchanged.
  - `TestLockedFieldTakesTheClaim` submits a differing value, no value, and a value while no claim exists. The stored value is the claim, the claim, and empty with no `required` error.
  - `TestRequiredMissingIsFieldScoped` expects `FieldError{Field: "who", Code: "required"}`.
  - `TestUnknownAnswerIsABadRequest`.
  - `TestSchemaRules` is a table: a duplicate key, a bad key, options on text, locked without from_claim, from_claim on a checkbox, max over the cap, an unknown property, no fields, trailing JSON, and an unknown type, each with its path and code.
  - `TestSeededRegistrationFormParses` reads version 1 from a migrated test database and parses it.
- `internal/problem`: a golden body for a field-error problem, and a test that the title stays the catalogue's.
- `internal/service/forms`:
  - `TestCorruptStoredSchemaFailsLoudly`: a version inserted by SQL with type `date` makes `Active` return `ErrCorruptSchema`. `GET /api/v1/admin/forms/registration` answers 500 and logs at error level, naming the key and version.
  - `TestCreateVersionKeepsOldVersions`: version 1 is byte-identical after version 2 is saved, and active moves to 2.
  - `TestCreateVersionIsOneTransaction`: a test trigger that fails the audit insert leaves no version and an unchanged active version.
  - `TestStaleBaseVersionConflicts`.
- `internal/web/api/forms_test.go`:
  - `TestFormAdminRoutesAreAdminOnly`: anonymous is 401, member and moderator 403, admin 200 or 201.
  - `TestInvalidSchemaIsNotSaved`: 422 with `errors`, and no row and no audit row written.
  - `TestPreviewWritesNothing`.
  - Every route answers `Cache-Control: no-store`.
- The goldens for Form and FormAdmin, and the client contract test.

### Out of scope

- The migration and the seed: TKT-01M29CKRDQ.
- The applicant's routes, which call `formsvc.Active` and `forms.Validate`: TKT-01M29CP2KZ.
- Rendering an application's answers against its version: TKT-01M29CP2MG.
- The editor UI: TKT-01M29CP2P1. The apply screen, and reading `errors` in `web/client/src/api/problem.ts`: TKT-01M29CP2NG.
- Forms other than `registration` (reports, polls). The key column allows them, and nothing here builds them.

### Revised at claim (2026-09-29, against main at 21dbd04)

Migration 0011 exists and seeds `registration` v1. `internal/audit` is the one writer of audit_log. Admin routes register through `h.adminRoute(mux, pattern, adminGate, handler)`, whose gate already answers 401 to anonymous and 403 to a pre-user and a non-admin member. Services raise client errors with `problem.Coded`, and `internal/problem` imports the domain packages, never the reverse. The approach above holds, with these changes:

1. `internal/forms` stays pure: no database, no HTTP, and no import of `internal/problem`.
   - The JSON tags carry `omitempty`, so the canonical stored form holds only what was set.
   - `Parse` decodes one raw object at a time. An unknown property, a wrong JSON type and trailing data each get a path such as `fields.2.options`, rather than one decoder message. Paths name indices, never a submitted key, so no error body carries request text.
   - One table, `types map[string]fieldType`. Each entry lists the optional properties its type allows, its `max` cap, and its answer check. A property the entry does not list is `not_allowed`, so the schema rules are table-driven as well as the answers.
   - One error type, `*forms.Invalid{Kind error; Fields []FieldError}`, whose `Kind` is `ErrInvalidSchema` or `ErrInvalidAnswers`, rather than two types that differ only in the sentinel.
   - Added rules: a schema over 64 KiB is refused (the API body limit is 64 KiB already). `title`, labels and options refuse control characters (`invalid_text`). `locked` is allowed only where `from_claim` is (`text`, `email`). New codes: `duplicate_option`, `unknown_claim`, and `not_allowed`, which is also the answer code for a value sent to a `markdown_notice`.
   - A `textarea` answer turns `\r\n` into `\n` before its checks. A locked field stores the claim trimmed, without type checks: it is the IdP's value, not the applicant's.
2. `internal/problem` gains `Problem.Errors []FieldError` (`errors,omitempty`). `domain.go` maps `forms.ErrInvalidAnswers` and `forms.ErrInvalidSchema` to validation-failed and `forms.ErrUnknownAnswer` to bad-request, and reads the field errors out of a `*forms.Invalid`. Only a validation-failed body carries `errors`.
3. `formsvc` raises `ErrStaleVersion` as `conflict` and `ErrNoForm` as `not-found` through `problem.Coded`, as the other services do, rather than through the mapping table. `internal/problem` importing a service would invert the dependency. `ErrCorruptSchema` stays unmapped. Version creators are drawn with `UsersByIDs` and `member.Author`.
4. `internal/audit` gains the target type `form`, whose id is the form's key. docs/04 lists it.
5. The preview answers `Form` with `version` 0, meaning unsaved. A key that names no form is `not-found`.
6. `dto.FormField.max` is the effective limit: the field's `max`, or its type's cap. It is 0 for a type without one. No dto field carries `omitempty`.
7. The client change is types only, in `web/client/src/api/dto.ts` and `dto-contract.ts`. The orchestrator approved it on 2026-09-29, because the contract test fails for any golden that `dto-contract.ts` does not import. `internal/web/spa/dist` must not change.
8. `api.Deps.Forms` is required, like the other services.

## Notes

**agent:claude-code/phase2-run-cp2ke** at 2026-09-29T16:01:37Z

Built at d446037 on run/cp2ke-form-schema (base 21dbd04).

### Why this PR touches web/client

The admin routes need two response shapes, `dto.Form` and `dto.FormAdmin`, and the dto harness requires every type to have a golden. `web/client/src/api/contract.test.ts` fails the CI "client tests" step for any golden that `dto-contract.ts` does not import. So the PR adds types only, to `web/client/src/api/dto.ts` and `dto-contract.ts`. The orchestrator approved this on 2026-09-29 (option 1: types only, no dist change), while the client lane holds the dist for TKT-01M29CKRG9. `go generate ./internal/web/spa` leaves `internal/web/spa/dist` unchanged, as checked with `git status` before the push. `npx tsc --noEmit -p .` and `npm test` (969 tests) pass.

### Gaps settled here (docs/12 "Gaps the design leaves open"), each written into the docs

- **Three codes beyond the groomed list.** `duplicate_option` is an option or a multiple-select answer given twice. `unknown_claim` is a `from_claim` outside name, email and preferred_username. `malformed` is a body that is not JSON, or trailing JSON. The alternative was to fold them into `not_an_option`, `not_allowed` and `wrong_type`, which lost because a client could then not tell "pick another option" from "this is a bug". All 16 codes are in docs/07 "Field errors on validation-failed", held there by `TestCodesAndTypesAreDocumented`.
- **The audit target type `form`, keyed by the form's key.** docs/04 lists it, and `TestTargetTypesDocumented` holds the list. It lost to nothing: no existing type names a form, and `setting` is the precedent for a key-shaped id.
- **`locked` is allowed only where `from_claim` is (text, email).** The grooming allowed `from_claim` on text and email only, and `locked` requires `from_claim`.
- **Field errors appear only on a validation-failed body.** A `*forms.Invalid` wrapped in another code (forbidden, internal) writes no `errors`, so no other body says what a form held.
- **`formsvc` raises not-found and conflict through `problem.Coded`**, not through the mapping table the plan named. Mapping `formsvc` errors in `internal/problem` would make problem import a service, an inverted dependency. `ErrCorruptSchema` stays unmapped, as planned.
- **Create takes the request IP** for the audit row, read as `RemoteAddr`, as `web.ClientIP` reads it.

### A settled item not verifiable here

"Remote images are rendered as links" depends on TKT-01M3N24RM (Render remote markdown images as links rather than broken images), which is `ready` and not built. Today `render.Render` emits `<img>` for a remote image, in a notice exactly as in a post. `intro_markdown` and notices go through `render.Render`, so they take that rule when TKT-01M3N24RM lands, with no change here. `TestCreateVersionKeepsOldVersions` checks the render call and not the image rule.

## Summary

internal/forms parses a form schema and validates answers through one table of six field types, with no code that names a field key. internal/service/forms (formsvc) loads, saves and previews versions. It exposes Active, Version(formID, n), Admin, Create, Preview and ToDTO, which TKT-01M29CP2KZ and TKT-01M29CP2MG call. The four admin form routes sit behind adminGate and answer no-store. A validation-failed body now carries errors [{field, code}] from a closed list of 16 codes, documented in docs/07. A save checks base_version, appends N+1, moves active_version, and audits form.version_created against the new target type form, all in one transaction. A corrupt stored schema is a 500 with an error-level log, and serve logs it at startup. dto.Form and dto.FormAdmin have goldens. The client change is types only (dto.ts, dto-contract.ts), and the dist is unchanged. All seven criteria were met.
