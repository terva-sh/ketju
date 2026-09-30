---
schema: 3
id: TKT-01M3QH3GS23M8VG1JEPCTGSGR1
title: Add agent provisioning controls to the admin users screen
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area:moderation
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies:
  - TKT-01M29P9RSW21SCES335WERTBSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T21:28:33Z
updated_at: 2026-09-30T00:06:33Z
created_by:
  id: agent:claude-code/phase2-run-p9rsw
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qh3gs2
  name: ""
extensions: {}
---

## Description

The admin users screen's controls for agent accounts. The server half is TKT-01M29P9RSW (Implement agent accounts, provisioning, and the agent credential), which built the routes, the DTOs and the client types. It left the screen alone because the client lane held the committed dist at the time.

What the screen needs, from that ticket's plan, step 8:
- A "New agent" form: handle, display name, and "issue a token now". It posts `POST /api/v1/admin/users` with `{kind: "agent", handle, display_name, issue_credential}` and answers `CreatedAgent`.
- After a creation or an issue, a one-time token panel with a copy button and the line "This token is shown once. Store it now." The token is in that response and never again.
- On an agent's detail, from `AdminUserDetail.agent`: when the live credential was issued and last used, "Issue new token" (with a confirmation that it replaces the old one, `POST /admin/users/{handle}/credential`), and "Revoke" (`DELETE` on the same path, 204).
- Limit fields for burst and refill seconds, showing the site default (`default_burst`, `default_refill_seconds`) as the placeholder. An empty field sends null. `PUT /admin/users/{handle}/agent-limits` with `{burst, refill_seconds}` answers the detail.

All of it is admin-only and in the lazy admin chunk. The built client is committed, so run `go generate ./internal/web/spa`, and only one lane may have a PR open that changes the dist.

## Acceptance criteria

- [x] An admin creates an agent from the users screen with and without a token, the request to POST /api/v1/admin/users is exactly {kind: "agent", handle, display_name, issue_credential}, and a 409 says the handle is taken (AgentControls.test.tsx)
- [x] A new token shows once in a panel with a copy button and the line "This token is shown once. Store it now.", and after Done or closing the detail it is in neither the DOM, web storage nor the address (AgentControls.test.tsx)
- [x] An agent's detail shows its live credential's issue and last-use times, Issue new token sends POST .../credential only after confirmation, and Revoke sends DELETE .../credential only after confirmation (AgentControls.test.tsx)
- [x] The burst and refill fields show default_burst and default_refill_seconds as placeholders, and PUT .../agent-limits sends null for an empty field (AgentControls.test.tsx)
- [x] A moderator, and an admin viewing a human, see no agent controls; TestTheAdminBundleIsSeparate and the bundle budget pass, and the dist regenerated with go generate ./internal/web/spa is committed

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at f04a476. The server half is merged: TKT-01M29P9RSW (Implement agent accounts, provisioning, and the agent credential), PR #164.

### Settled in grooming

- **The routes and shapes are fixed by TKT-01M29P9RSW** and documented in docs/07 "Agents on the API". This ticket changes no Go code and no DTO.
  - `POST /api/v1/admin/users` with `{kind: "agent", handle, display_name, issue_credential}` answers 201 `CreatedAgent{user, credential}`. `credential` is null when none was asked for. An invalid or reserved handle is `validation-failed` with no field errors (`identity.CheckHandle` in `users.CreateAgent`), and a taken one is `conflict`.
  - `POST /api/v1/admin/users/{handle}/credential` answers 201 `IssuedCredential{token, created_at}` and revokes the live one. An agent that is not active, or an account that is not an agent, is `conflict`.
  - `DELETE /api/v1/admin/users/{handle}/credential` answers 204, and `not-found` when there is no live credential.
  - `PUT /api/v1/admin/users/{handle}/agent-limits` with `{burst, refill_seconds}` answers `AdminUserDetail`. Both keys are required, and null is the site default. Burst must be at least 1, and refill 1 to 604800 seconds (`settings.AgentRefillMaxSeconds`); outside that is `validation-failed`.
  - Every answer is `no-store`. The types `CreatedAgent`, `IssuedCredential`, `AgentInfo` and `AgentCredential` are already in `web/client/src/api/dto.ts`.
- **The token is shown once** (docs/05 "Agent accounts", docs/07): it is in the two 201 answers and nowhere else, ever. The client must not put it anywhere it outlives the panel: no signal, no `localStorage`, no URL, no log. A POST answer is not held by `peekGet`, so the fetch layer keeps no copy.
- **One live credential per agent**, and issuing replaces it (TKT-01M29P9RSW, settled there). "Issue new token" therefore always confirms, and the prompt says the old token stops working at once.
- **Agents are an admin's.** `AdminUserDetail.agent` is null for a moderator, and every route above is behind `adminGate`. A moderator is offered none of these controls.
- **Site defaults**: burst 20, refill 30 s, maintainer decision 2026-09-29 on TKT-01M29P9RSW. The client does not hard-code them; it shows `default_burst` and `default_refill_seconds` from the detail.
- **The admin chunk.** Everything stays under `web/client/src/screens/admin/`, reached only through the lazy import in `routes.tsx` (TKT-01M3NRYCP5), so `TestTheAdminBundleIsSeparate` passes. The built client is committed: run `go generate ./internal/web/spa` and commit `internal/web/spa/dist` (docs/13 "Running two lanes": only one lane may have a PR open that changes the dist).

Engineering choices made here, each with the alternative that lost:
- **New files, not a longer `UsersScreen.tsx`.** `screens/admin/NewAgentForm.tsx` and `screens/admin/AgentPanel.tsx`, with `TokenPanel` inside the second. TKT-01M3P0CYR3 (rename control) also changes the user detail; separate files keep the two PRs from conflicting. Adding them inline in `UserDetail` lost for that reason.
- **Refusal words specific to this form.** `send` (`screens/admin/common/send.ts`) maps `conflict` to "Someone else changed this while you were working", which is wrong for a taken handle. The form maps `conflict` to "That handle is taken." and `validation-failed` to "That is not a valid handle, or it is reserved." and uses `send`'s words for anything else. Extending `send` with a per-call table lost: one caller needs it.
- **The token lives only as long as the agent's detail.** Every token, including the one from creation, is shown by `TokenPanel` inside `AgentPanel`, which is part of `UserDetail`, keyed by the handle. `NewAgentForm` holds no token. When the creation answer arrives, the form passes it up to `UsersTab` through `onCreated`. `UsersTab` selects the new agent and gives `UserDetail` the credential as a one-shot `issued` prop. `AgentPanel` copies it into its own state on mount, and `UsersTab` clears its copy in the same step through `onIssuedTaken`. Done, closing the detail, choosing another account and leaving the screen all unmount or clear it. Terva review 1488 (PR #167, head f5a4206), finding 1, found that the first version kept the creation token in `NewAgentForm`, above the list, where closing the detail would not clear it. Two other designs lost. Keeping the token in the form and clearing it when the selection changes is a second lifetime to keep in step. A module-level signal would survive navigation.

### Revised at claim (2026-09-30, main at 4d793b2)

The code matches the groomed plan: `UsersScreen.tsx` still keeps the selection in `UsersTab` state (TKT-01M3P0CYR3 has not landed, so there is no `?user=`), `UserDetail` has no Close, and the agent routes and DTOs are as settled. Three details the plan left to the build:
- **The handoff lasts one commit.** `CreatedAgent.user` is the new agent's `AdminUserDetail`, so `UsersTab` hands it to the new `UserDetail` as its first detail (`initial`). `AgentPanel` then mounts in the same render as the selection, takes the token in its state initializer, and `UsersTab` clears its copy from `AgentPanel`'s mount effect. Waiting for the detail GET instead would leave the token in `UsersTab` for a round trip, and indefinitely if that GET failed. Close and picking a row also clear the handoff, so a token never waits there for a detail that is not coming.
- **Refusals are read with `isProblem`**, not by comparing `.type` or `.status`: `src/api/one-path.test.ts` refuses that under `screens/`.
- **Focus after Close** returns to the account's row in the list when it is there, so a keyboard user is not left on nothing. The handle and display-name inputs carry `maxLength` 30 and 100 (`domain.HandleMaxLen`, `users.DisplayNameMaxLen`), so the one `validation-failed` the form's words cannot describe, an overlong display name, cannot be sent.

An e2e spec, `e2e/admin-agents.spec.ts`, signs in as the seeded admin `ada` at both widths, creates an agent with a handle unique to the run, checks the token panel, Done, issue and revoke, and scans each state with axe in both themes as `admin-forms.spec.ts` does.

### Approach

1. **`screens/admin/NewAgentForm.tsx`**, rendered above the account list in `UsersTab` when `admin`:
   - A "New agent" button opens the form: `Field`s for handle and display name, and a checkbox "Issue a token now", checked by default.
   - Submit sends `send<CreatedAgent>('POST', '/api/v1/admin/users', {kind: 'agent', handle, display_name, issue_credential})`. On success it clears the form and calls `onCreated(answer)`. It does not render the token. `UsersTab`'s `onCreated` calls the tab's `reload` and selects the new account the way a row click does. It hands `answer.credential` to that account's `UserDetail` as the `issued` prop, and clears it once `AgentPanel` has taken it.
   - Refusals show beside the form with the words above.
2. **`screens/admin/AgentPanel.tsx`**, rendered by `UserDetail` when `admin && d.agent !== null`, below the facts. It owns the only `TokenPanel` state. On mount it copies an `issued` prop into that state and calls `onIssuedTaken`.
   - A "Token" block. With a live credential: "Issued {when(created_at)}", "Last used {when(last_used_at)}" or "never", "Issue new token" as a `ConfirmButton` whose prompt says the current token stops working at once, and "Revoke" as a `danger` `ConfirmButton`. Without one: "No live token" and "Issue token", which needs no confirmation because nothing is replaced.
   - Issue sends `POST …/credential`, shows `TokenPanel` with the answer, and refetches the detail. Revoke sends `DELETE …/credential` and refetches it. A 404 on revoke means another admin revoked it first, so the panel refetches and says "This agent has no live token."
   - A "Write limits" block: two number `Field`s, burst and refill seconds, whose placeholders are `default_burst` and `default_refill_seconds`, and whose values are `burst` and `refill_seconds` or empty. The hint says an empty field uses the site default. Save sends `PUT …/agent-limits` with `{burst: n or null, refill_seconds: n or null}` and replaces the detail with the answer. `min=1` on both inputs, and `max=604800` on refill, mirror the server's bounds; the server's `validation-failed` is still shown.
3. **`TokenPanel`**: the token in a read-only `<input>` (selectable, `autocomplete="off"`, `spellcheck=false`), a "Copy" button using `navigator.clipboard.writeText` that says "Copied" or, when the clipboard is refused, "Select the token and copy it", the line "This token is shown once. Store it now.", and "Done", which clears it. It has `role="status"` so a screen reader announces it, and focus moves to the token when it opens.
4. **`UsersScreen.tsx`**: `UserDetail` passes `d`, `url`, its detail setter, `issued` and `onIssuedTaken` to `AgentPanel`. `UsersTab` renders `NewAgentForm` and holds the one-shot handoff described above. On main, the detail has no way to close. It gains a "Close" button that clears the selection and so unmounts the detail. If TKT-01M3P0CYR3 has moved the selection into `?user=`, Close routes to `/admin/users`. Nothing else in the file changes.
5. **CSS** in `screens/admin/admin.css`, using the existing `admin-*` classes. The token is set in a monospace font so that similar characters can be told apart.
6. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`. `npm run build` runs the budget; the admin chunk is outside `threadViewGzipBytes`.

### Tests

- `screens/admin/AgentControls.test.tsx`, with `fetch` stubbed as `UsersScreen.test.tsx` does:
  - `creates an agent with a token`: the request body is exactly `{kind: 'agent', handle, display_name, issue_credential: true}`, the panel shows the token and the line "This token is shown once. Store it now.", and the new account is selected.
  - `creates an agent without a token`: `issue_credential: false`, and no panel.
  - `says a taken handle is taken`: a 409 shows "That handle is taken." and not the generic conflict sentence. A 422 shows the invalid-or-reserved words.
  - `the token is gone after Done`: after Done, the token string appears nowhere in `document.body.innerHTML`, and not in `localStorage`, `sessionStorage` or `location.href`.
  - `closing the detail clears the creation token` (review 1488, finding 1): create an agent with a token, then close the detail and, separately, pick another account. Each time the token string is absent from `document.body.innerHTML`, and it does not come back when the new agent is selected again.
  - `the form holds no token`: after creation, `NewAgentForm`'s subtree does not contain the token.
  - `copies the token`: `navigator.clipboard.writeText` is called with the token. When it rejects, the fallback words show.
  - `issue new token asks first`: nothing is sent until Confirm, then one `POST …/credential`, and the new token shows.
  - `revoke`: one `DELETE …/credential` after Confirm, and the detail refetches. A 404 shows "This agent has no live token."
  - `limits show the defaults and send null`: the placeholders are the detail's defaults. With both fields empty the body is `{"burst":null,"refill_seconds":null}`, and with values it sends numbers.
  - `a moderator sees none of it`: with `role: 'moderator'` and `agent: null`, there is no New agent button and no agent panel.
  - `a human's detail has no agent panel`: `agent: null` for an admin viewing a human.
- `TestTheAdminBundleIsSeparate` passes: none of the new files is imported from outside `screens/admin`.
- The server's own tests stay as they are: `TestAgentCreationAuditedInOneTransaction` covers the audit row and transaction of creation, and `TestAgentRolesUnchanged` covers role changes through `perm.ChangeRole`. This ticket adds no path that changes a role, writes an audit row, or issues a session.

### Out of scope

- Any server change. A field error on the handle (`errors[handle]`) for `CreateAgent` would let the form attach it to the field; that is not needed for a form with one handle field, and would be a change to TKT-01M29P9RSW's route.
- Renaming an agent: TKT-01M3P0CYR3.
- Suspending or deactivating an agent, which revokes its credential: TKT-01M29CP2QK and TKT-01M29CKRFS.
- Personal API tokens for humans (a later phase, docs/07).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-29T22:07:21Z

Planned in Phase 2 grooming (TKT-01M3M9RR26), 2026-09-30. Client only; the routes and types are merged with PR #164. The controls go in new files under screens/admin (NewAgentForm.tsx, AgentPanel.tsx) so this ticket and TKT-01M3P0CYR3, which also changes the user detail, do not conflict. Only one lane may change the committed dist at a time.

**agent:claude-code/phase2-groom** at 2026-09-29T22:25:28Z

Terva review 1488 on PR #167, head f5a4206, finding 1 (high, credential handling): accepted and fixed in the plan. The first plan kept the creation token in NewAgentForm, above the list, so closing the detail would not clear it. Now every token, the creation token included, lives only in AgentPanel inside the agent's UserDetail. NewAgentForm passes the answer up and holds no token, and the detail gains a Close button. New tests: 'closing the detail clears the creation token' and 'the form holds no token'.

**agent:claude-code/phase2-run-qh3gs2** at 2026-09-30T00:06:22Z

Built on main at 4d793b2. Three choices differ from the plan's letter, and one shared file changed:

- **The limit fields are text with `inputMode="numeric"`, not `type=number` with min and max.** A number input reports what it cannot parse ("1e", "12-") as an empty value, and this form sends empty as null. A typo would then reset the agent's limit to the site default without anyone asking. The client holds each field to the server's bounds (burst at least 1, refill 1 to 604800) before sending, and shows the server's `validation-failed` if it still refuses. Test: `sends nothing the server would refuse`.
- **Issue and revoke show the change at once, then refetch.** The answer says exactly what changed: a new credential with `last_used_at` null, or none. The refetch is ordered by a counter, so a slow GET cannot overwrite a newer limits answer, and a failed GET leaves the local update in place.
- **The handover lasts one render.** `CreatedAgent.user` seeds the new `UserDetail` (`initial`), so `AgentPanel` mounts in the same render as the selection and clears `UsersTab`'s copy from its mount effect. `choose()` (a row, or Close) also clears it. Mutations: dropping either clear alone is covered by the other. Dropping both brings the creation token back when the agent is chosen again, and `is gone when the detail is closed…` and `…another account is chosen…` fail.
- **`common/ConfirmButton.tsx`**: on close it now refocuses its button only when focus went to the body with the question. Playwright found that after "Issue new token", Confirm put focus back on the button and took it from the token panel. New test: `leaves focus where the action moved it`, which fails without the change.

Other mutations tried: removing `setToken(null)` from Done and from revoke fails three tests. Removing the confirmation step fails the issue and revoke tests.

e2e: `e2e/admin-agents.spec.ts` runs as ada at 390 and 1440 px. It creates an agent with a token, checks the token is accepted as a bearer (200) and gone from the page after Done, saves a limit, issues a new token (the old one is then 401), revokes (the new one is then 401), and closes. axe finds no violations in either theme at five states. The probe reads only the status, because GET /me answers an agent as anonymous; TKT-01M3QH3X (Answer GET /me for an agent authenticated by its credential) owns that. Full suite: 65 passed, 9 skipped, on port 5181. Reader bundle 45575 of 60000 B.

## Summary

The admin users screen creates agents and manages their tokens and write limits. NewAgentForm (screens/admin) posts {kind, handle, display_name, issue_credential} and holds no token. AgentPanel, inside the agent's UserDetail, holds every one-time token in its own state only, so Done, Close, choosing another account and leaving the screen each drop it. The detail gained a Close button, and ConfirmButton no longer takes back focus an action moved. Unit tests are in AgentControls.test.tsx, and e2e/admin-agents.spec.ts runs with axe at 390 and 1440 px. The dist is regenerated, and the reader bundle is 45575 of 60000 B.
