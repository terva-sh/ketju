---
schema: 3
id: TKT-01M3ANX1NF3XWCJBS3DH9QZNFC
title: Handles ending in '-' can never be mentioned
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:129
    path: null
  - ref: pr:130
    path: null
claim: null
archive: null
created_at: 2026-09-24T21:42:17Z
updated_at: 2026-09-28T19:45:07Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The handle rule (docs/04, the users CHECK, and domain.ValidateHandle) accepts a handle that ends in `-`, such as `bob-`. The mention parser in internal/render/mention.go strips a trailing `-` as sentence punctuation, the same way it strips `.`. So `@bob-` is read as `@bob`:

- It mentions a different user, `bob`, when one exists.
- It mentions nobody when one does not.

A user whose handle ends in `-` can therefore never be mentioned, and nothing reports it.

TKT-01M38XJJKM (Serve handle autocomplete for mentions) works around this by leaving such handles out of the suggestions, because suggesting one would insert a mention of somebody else. That is a workaround, not a fix.

### Options
- Forbid a trailing `-` in the handle rule: in docs/04, the CHECK (a migration), `domain.ValidateHandle` and its shared cases. Existing handles would need a rename policy (TKT-01M29BSPEN).
- Make the parser keep a trailing `-` when the handle with it resolves. That makes parsing depend on resolution, and changes what existing posts render (Version bump).

This is a format and product decision, so it is filed as a draft.

## Implementation plan

### Settled in grooming
- Maintainer decision, 2026-09-28, in the maintainer's words: "Forbid for new handles".
- Registration and handle validation refuse a trailing '-' from now on: `CheckHandle`, `ValidateHandle`, the /auth/register form's error, and the registration prefill, which already trims one.
- docs/04's handle grammar says so.
- Existing handles stay valid. No database constraint change and no migration. Renaming them waits for TKT-01M29BSPEN (Decide the handle change policy).
- The mention parser and the renderer stay as they are, with no renderer version bump.
- The autocomplete workaround (`UsersByHandlePrefix` leaving out handles that end in '-') stays, and docs/07 says why.

### Approach
1. `domain.ValidateHandle` refuses a handle whose last character is '-', with its own message. Its comment then describes the rule for a new handle, not every stored one.
2. The shared table `domain.HandleCases` gains a `Grandfathered` field. It marks a handle that the users CHECK still stores but a new handle may not be. `TestHandleRuleMatchesTheSchema` expects the CHECK to accept `Valid || Grandfathered`, so the two rules may differ only in the rows that say so. New rows cover `bob-`, `a--`, `a---...` (dashes only after the first character, 3 and 30 long), and `a-b`/`bob_` staying valid.
3. `TestCheckHandle` adds table rows for a trailing '-' after normalizing (`Bob-`, ` bob- `).
4. /auth/register: `handleRule` in internal/web/auth/page.go says a handle may not end in '-'. A register test shows that `bob-` gets the form again with the reason and no account is created.
5. `identity.suggest`'s comment is updated: the trim is now the rule.
6. docs/04 grammar, docs/07 `GET /users` note, and the `mention.go` comment that says the rule is not written down anywhere.

The migrations, reindex, and the mention parser are left alone.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:19:40Z

Decision needed from the maintainer (hardening sweep, 2026-09-28). Blocked under docs/12 "Gaps the design leaves open": either fix changes what a member can do or what a member sees.

### Why the design does not settle it
- docs/04 "users" states the handle grammar: 3 to 30 characters from a-z, 0-9, `_` and `-`, starting with a letter or digit. It says nothing about the last character, so `bob-` is valid. `domain.ValidateHandle`, the users CHECK and `identity.CheckHandle` (PR #102) all accept it.
- `internal/render/mention.go` strips any trailing `.` and `-` as sentence punctuation, so `@bob-` is read as `@bob`.
- docs/07 `GET /users` documents that behaviour of the mention syntax and the autocomplete workaround that follows from it: a handle ending in `-` is not offered (`UsersByHandlePrefix`, `internal/service/users/suggest.go`).
- No document says that the mention grammar must accept every valid handle. The two grammars are each written down and they disagree. Choosing between them is a product decision.
- `identity.Prefill` already trims a trailing `-` from the suggested handle, so a member only gets such a handle by typing it.

### Option (a): forbid a trailing `-` in handles
- Change docs/04, `domain.ValidateHandle` and its shared cases, and the users CHECK through a new migration. `TestHandleRuleMatchesTheSchema` keeps the code rule and the CHECK in step.
- What a member can do changes: registration refuses `bob-` with a reason.
- A handle that already ends in `-` needs a policy before the migration can tighten the CHECK. The choices are to rename it, to grandfather it (the CHECK stays loose and only registration refuses), or to refuse to migrate. This depends on the handle change policy, TKT-01M29BSPEN (Decide the handle change policy, phase-2 draft).
- The renderer is unchanged, with no version bump and no change to existing posts. The autocomplete filter could then be removed.
- Existing handles with a trailing `-` on the dogfood instance (`~/.local/state/ketju/data`, read-only query on 2026-09-28): 0 of 1 user.

### Option (b): make the mention parser accept a trailing `-`
- (b1) Keep the `-` unconditionally. Ordinary prose such as `@bob- see above` or `ask @bob-- he knows` then reads `bob-` instead of `bob`. When nobody is called `bob-`, it mentions nobody, where today it mentions bob. This changes what existing posts render and whom they notify on edit.
- (b2) Keep the `-` only when the handle with it resolves (the ticket's option). `@bob- see above` still mentions bob unless a `bob-` exists, and then it silently mentions `bob-`. Parsing then depends on who is registered, so registering `bob-` later changes how old posts render. The mention rows recorded at write time are by user id and would still name bob, so the rendered link and the recorded mention can disagree. The render cache and page cache, which today re-check only the handles a render resolved (docs/07), would also have to re-check the `-` variant.
- Either way the renderer version is bumped from 4 to 5, the goldens change, and the autocomplete filter is removed.
- What a member sees in existing posts can change, which is the docs/12 line this crosses.

### What unblocks it
The maintainer picks (a), (b1) or (b2), and for (a) says what happens to an existing trailing-dash handle. Record the choice in a dated note on this ticket and move it back to ready.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:19:42Z

in-progress to blocked: Needs a maintainer decision: forbid a trailing '-' in handles (docs/04 rule, CHECK migration, policy for existing handles) or make the mention parser accept it (renderer version bump, existing posts can render differently). docs/04 and the mention syntax disagree and no document says which wins; see the decision note.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:41:51Z

Maintainer decision, 2026-09-28, relayed by the sweep's orchestrator, in the maintainer's words: "Forbid for new handles".

This is option (a) from the decision note, limited to new handles:
- Registration and handle validation refuse a trailing '-' from now on: `identity.CheckHandle`, `domain.ValidateHandle`, the /auth/register form's error, and the registration prefill, which already trims one.
- docs/04's handle grammar says so.
- Existing handles stay valid. There is no database constraint change and no migration, so a database holding such a handle keeps working. Renaming them waits for the handle change policy, TKT-01M29BSPEN (Decide the handle change policy).
- The mention parser and the renderer stay as they are, so the renderer version is not bumped.
- The autocomplete workaround (`UsersByHandlePrefix` leaving out handles that end in '-') stays, because such handles can still exist. docs/07 says so.

This unblocks the ticket.

## Summary

Built to the maintainer's 2026-09-28 decision, "Forbid for new handles". domain.ValidateHandle, and so identity.CheckHandle and the /auth/register form, refuse a new handle ending in '-', and the form's rule text says so. The users CHECK is unchanged and there is no migration, so an existing handle ending in '-' stays valid and stored. The shared HandleCases table marks such rows Grandfathered, and TestHandleRuleMatchesTheSchema expects the CHECK to accept them. Tests cover the grammar, including handles that are only dashes after the first character at 3, 30 and 31 characters. The renderer, the mention parser and the autocomplete filter are unchanged, with no version bump. docs/04 records the rule and the decision, and docs/07 says why the filter stays. Renaming existing trailing-dash handles waits for TKT-01M29BSPEN.
