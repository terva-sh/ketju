---
schema: 3
id: TKT-01M29CP2P1R6TTJHY28FP7SWPG
title: Build the admin form editor
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CKRDQHB528ZW1FFBN4SA1
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
  - TKT-01M29CP2KEEB60VEVBPY9H2GBD
  - TKT-01M29CP2NGWH32C6M718QHB30X
blocks_on: none
references:
  - ref: design:forms
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:26:15Z
updated_at: 2026-09-29T07:18:12Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
extensions: {}
---

## Description

A small admin UI for editing the registration form, writing a new version on each save while the old version stays for applications already submitted against it.

A preview of the applicant's view belongs here. An admin writing a screening question should see what a stranger sees before a stranger does.

## Acceptance criteria

- [ ] Saving posts the draft with base_version; on 201 the editor shows the new version as active and lists the earlier ones unchanged; on 409 it keeps the draft and says a newer version exists
- [ ] The editor previews the draft through POST /api/v1/admin/forms/{key}/preview, drawn by the same src/forms renderer the apply screen uses
- [ ] A draft the server refuses cannot be saved: its path-scoped errors attach to the editor's fields and the save control stays disabled until a preview passes
- [ ] Any earlier version opens read-only in the same renderer (rendering applications against their version is TKT-01M29CP2MG and TKT-01M3NRX2NN)
- [ ] The section is shown to admins only, the editor lives under src/screens/admin so TestTheAdminBundleIsSeparate passes, the dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip

## Implementation plan

Groomed 2026-09-29 for the Phase 2 autonomous run (TKT-01M3M9RR26), against main at 7af2c5a. `/admin/forms` renders the placeholder in `web/client/src/screens/admin/AdminRoot.tsx`, inside the lazy admin chunk that `TestTheAdminBundleIsSeparate` (`internal/web/spa/spa_test.go`) and `bundle.mjs` keep out of the reader's bundle.

### Settled in grooming

- **Admins edit the form in a small UI, and each save writes a new version** that leaves the old one for the applications answered on it (docs/05 "Forms", docs/04 "forms, form_versions").
- **Only admins edit forms** ("Admins edit the form", docs/05). The server enforces it (TKT-01M29CP2KE's routes answer 403 to anyone else), and the client shows the section to admins only.
- **A preview of the applicant's view belongs here** (this ticket's description).
- **The server is the authority on schema rules** (TKT-01M29CP2KE's `forms.Parse`). The editor never keeps a second copy of the rules.
- **The built client dist is committed.** Run `go generate ./internal/web/spa` after any client change. The admin chunk is outside the 60000 B budget, but the shared `src/forms` renderer it imports is counted once through the apply screen.

Engineering decisions taken here, with the alternative that lost:
- **The editor is a structured field list**: add a field by type, move it up or down, delete it, and edit its properties, with title and intro above. A raw JSON textarea lost: the admins of a club are not assumed to write JSON, and a server error pointing at `fields.3.options` means nothing in a textarea.
- **The preview is the server's rendering**, from `POST /api/v1/admin/forms/{key}/preview`, drawn by the same `src/forms/FormView` the apply screen uses, in `preview` mode. The notices and intro are markdown rendered by `internal/render`, which the client cannot reproduce. Rendering the preview locally would show something the applicant never sees.
- **Save sends `base_version`.** A 409 keeps the draft and says a newer version exists, so two admins cannot silently overwrite each other.
- **Save is disabled until the latest preview has passed.** Previews are debounced by 500 ms after an edit. A 422 from either call attaches its paths to the fields.
- **The criterion that old applications render against their version moves out of this ticket.** The editor shows versions, and applications are shown on the review screen (TKT-01M3NRX2NN) from TKT-01M29CP2MG's API, which carries each application's own version. Here, any earlier version opens read-only.

### Approach

1. `src/screens/admin/forms/FormEditor.tsx`, routed from `AdminRoot` for `/admin/forms`:
   - It loads `GET /api/v1/admin/forms/registration` (`FormAdmin`) and seeds the draft from its `schema`.
   - `draft.ts` holds the editor model as a signal, with pure functions to add, remove, move and update a field, and `toSchema()`.
   - `FieldEditor.tsx` edits one field's properties. A property input appears only for the types TKT-01M29CP2KE allows it on. The list of which properties go with which type is presentational; the server still refuses a wrong combination.
2. The preview pane calls `POST …/preview` on a debounced change and renders `FormView mode="preview"` with no submit. A 390 px frame toggle shows the phone width. Errors map onto the fields: a path such as `fields.2.options` names the third field's options input.
3. Save calls `POST …/versions` with `{base_version, schema}`:
   - 201 reloads `FormAdmin` and shows the new version as active.
   - 409 shows "Someone saved a newer version" with a reload control, and keeps the draft.
   - 422 attaches the errors.
4. The versions list shows each version from `FormAdmin.versions`, with its date and author. Opening one calls `GET …/versions/{n}` and renders it read-only.
5. The admin section index shows `/admin/forms` to role admin only. Direct navigation by anyone else shows the server's 403 through `ErrorPanel`.
6. Build: `npm run build`, then `go generate ./internal/web/spa`, then commit `internal/web/spa/dist`. `npm run budget` must pass, which confirms the editor stayed in the admin chunk.

### Tests

- `src/screens/admin/forms/draft.test.ts`: adding, removing and reordering fields produces the expected schema JSON.
- `src/screens/admin/forms/FormEditor.test.tsx`:
  - `save posts base_version and shows the new active version`.
  - `a 409 keeps the draft`.
  - `a 422 attaches errors and save stays disabled`.
  - `the preview renders the server's form through FormView`.
  - `an earlier version opens read-only`.
  - `the section is hidden from moderators`.
- `internal/web/spa` `TestTheAdminBundleIsSeparate` still passes, because the editor lives under `src/screens/admin`.
- `npm run budget` passes.

### Out of scope

- The admin form API and the schema rules: TKT-01M29CP2KE.
- The shared renderer: TKT-01M29CP2NG.
- Rendering applications against their version: TKT-01M29CP2MG (server) and TKT-01M3NRX2NN (screen).
- Forms other than `registration`.
