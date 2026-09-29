---
schema: 3
id: TKT-01M29CP2P1R6TTJHY28FP7SWPG
title: Build the admin form editor
type: task
status: done
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
updated_at: 2026-09-29T22:14:25Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-p2p1
  name: ""
extensions: {}
---

## Description

A small admin UI for editing the registration form, writing a new version on each save while the old version stays for applications already submitted against it.

A preview of the applicant's view belongs here. An admin writing a screening question should see what a stranger sees before a stranger does.

## Acceptance criteria

- [x] Saving posts the draft with base_version; on 201 the editor shows the new version as active and lists the earlier ones unchanged; on 409 it keeps the draft and says a newer version exists
- [x] The editor previews the draft through POST /api/v1/admin/forms/{key}/preview, drawn by the same src/forms renderer the apply screen uses
- [x] A draft the server refuses cannot be saved: its path-scoped errors attach to the editor's fields and the save control stays disabled until a preview passes
- [x] Any earlier version opens read-only in the same renderer (rendering applications against their version is TKT-01M29CP2MG and TKT-01M3NRX2NN)
- [x] The section is shown to admins only, the editor lives under src/screens/admin so TestTheAdminBundleIsSeparate passes, the dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip

## Implementation plan

Revised 2026-09-29 on claim, against main at f04a476 (after PR #164). The grooming plan (written against 7af2c5a) holds. What changed is what main now provides: `src/forms` exports `FormView` (fill, preview, read), `kinds`/`kindOf` and `fieldMessage` with the schema codes; `screens/admin/common` has `Field`, `Table`, `ConfirmButton` (with `disabled`) and `send`; `sections.ts` already offers `/admin/forms` to admins only; vite's `ApplyRoot` group keeps `src/forms` in the apply chunk, which the admin chunk imports from.

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

1. `src/screens/admin/forms/draft.ts` holds the editor model and its pure functions. A draft is `{title, intro, fields}`. Each field has a local id that survives reordering, the schema field as loaded (so a property the editor does not draw is carried, not dropped), and a select's options as the text of a one-per-line textarea. The functions are `fromSchema`, `toSchema` (drops defaults, as the canonical form does), `addField`, `removeField`, `moveField` and `updateField`.
   - `propsFor(type)` is the presentational list of which property controls a type gets. text and email: required, max, from_claim, locked. textarea: required, max. checkbox: required. select: required, options, multiple. A type whose kind takes no answer: markdown. The server still refuses a wrong combination.
   - The types offered for a new field come from `src/forms/fields.tsx`'s `kinds`. The display-only type is found by `answers === false`, so no file outside fields.tsx names it (FormView.test.tsx "the one type map").
   - `attach(errors, sent)` maps each 422 path onto a field id and a property control, and returns the rest as unplaced.
2. `FormEditor.tsx` is the `admin-forms` screen in AdminRoot's `built` map. It loads `GET /api/v1/admin/forms/registration`, and draws title and intro above a structured field list. `FieldEditor.tsx` edits one field: label, key, the type's property controls, move up, move down, remove. Focus stays on the control after a move, and goes to a sensible place after a remove or an add.
3. Preview: 500 ms after an edit, `POST …/preview` with `{schema}`, aborting the previous one. A 200 draws `FormView mode="preview"`; a 422 attaches errors. A "Phone width" toggle (aria-pressed) frames the preview at 390 px.
4. Save is enabled only when the draft has changed and the preview of this exact revision passed. `POST …/versions` with `{base_version, schema}`:
   - 201 takes the answer as the new state (active version, versions list, draft reseeded) and focuses a notice.
   - 409 keeps the draft, says a newer version exists, and offers a ConfirmButton to discard it and load the newest.
   - 422 attaches errors and leaves Save disabled.
5. Errors: a path with a control (`title`, `intro_markdown`, `fields.N.<prop>`, `fields.N.options.M`) shows on that control through `Field` and `fieldMessage`. Every other path (`""`, `fields`, `fields.N`, `fields.N.type`, a property with no control) is said in the editor's own lead line, which names where: "The form as a whole", "The list of fields", "Field 3 (who)".
6. Versions: a `Table` of `FormAdmin.versions` with date, author (or "Seeded at install"), which is active, and a View button. View loads `GET …/versions/{n}` and draws it in `FormView mode="preview"` inside a disabled fieldset, read-only, without leaving the draft.
7. e2e: seed an admin (`ada`) in cmd/ketju e2e.go and session.setup.ts. `e2e/admin-forms.spec.ts`, at both widths, edits the form, sees a refused key attach to its field with Save disabled, saves a new version, opens version 1 read-only, and saves the original fields back so the apply spec's form is unchanged. Axe runs in both themes, with a horizontal overflow check.
8. Build: `npm run build`, `go generate ./internal/web/spa`, commit the dist. `npm run budget` stays within 60000 B; the reader total may grow only by the export names the admin chunk newly imports from the apply chunk.

### Tests

- `draft.test.ts`: add, remove and reorder produce the expected schema JSON; defaults are dropped; options text round-trips; unknown properties are carried; error paths attach to the right field after a reorder, and unplaced ones are returned.
- `FormEditor.test.tsx`: save posts base_version and shows the new active version; a 409 keeps the draft; a 422 attaches errors and save stays disabled; the preview renders the server's form through FormView; an earlier version opens read-only; an error with no control is said in the lead line.
- `AdminRoot.test.tsx`: the forms section is hidden from moderators and refused at its address.
- `TestTheAdminBundleIsSeparate` and `npm run budget` pass.

### Out of scope

- The admin form API and the schema rules: TKT-01M29CP2KE.
- The shared renderer: TKT-01M29CP2NG.
- Rendering applications against their version: TKT-01M29CP2MG (server) and TKT-01M3NRX2NN (screen).
- Forms other than `registration`.

## Notes

**agent:claude-code/phase2-run-p2p1** at 2026-09-29T22:14:10Z

Gaps settled in building (internals and presentation only; none changes what the server allows, who sees what, or what is stored):

- **The type/property table is presentational, in draft.ts `propsFor`.** It decides which controls a type gets and follows internal/forms's types table. The server still refuses a wrong combination with not_allowed. Putting the table in src/forms/fields.tsx lost: that file is in the apply chunk and counts toward the reader budget.
- **The notice type is found by `kinds[t].answers === false`, never by name.** FormView.test.tsx "the one type map" forbids the name in any file but fields.tsx and dto.ts.
- **from_claim is a choice of name, email and preferred_username** (docs/05's list), rather than free text. The server still refuses any other with unknown_claim.
- **A select's options are one per line in a textarea**, held as typed and split on send, with blank lines left out. A controlled array lost: a newline typed at the end would vanish before the next option could be typed.
- **"Load the newest version" after a 409 discards the draft, behind a ConfirmButton.** Keeping the draft on the new base lost: it would let an admin save over the other admin's change without seeing it, which is what base_version exists to stop.
- **Save stays disabled until the draft has changed** as well as passed its preview, so an unchanged draft cannot write a duplicate version.
- **The reader total grows by 6 B** (45330 to 45336 B gzip). The admin chunk now imports `kinds` and `fieldMessage` from the apply chunk, which adds two names to its export list. No editor code is in a reader chunk. The alternative, copying the words table into the admin chunk, would be a second copy of messages.ts.
- **e2e seed: an admin, `ada`** (cmd/ketju e2e.go, e2e/session.setup.ts). admin-forms.spec.ts saves the starting fields back at the end of each run, so apply.spec.ts finds the form's content unchanged, at a higher version number.

## Summary

The admin form editor is at /admin/forms (web/client/src/screens/admin/forms), for admins only, in the lazy admin chunk. It is a structured field list beside the server's preview, drawn by src/forms FormView. Previews run 500 ms after each edit. Save sends base_version and is enabled only once this exact revision's preview has passed; 201 shows the new active version, 409 keeps the draft and offers the newest, and 422 marks each error on its control. An error with no control goes in the editor's own lead line. Any version opens read-only. The e2e seed gains an admin, ada, and admin-forms.spec.ts covers the editor at 390 and 1440 px with axe. The reader total grows by 6 B of export names (45336 of 60000 B).
