---
schema: 3
id: TKT-01M3ANX1NF3XWCJBS3DH9QZNFC
title: Handles ending in '-' can never be mentioned
type: bug
status: blocked
status_reason: "Needs a maintainer decision: forbid a trailing '-' in handles (docs/04 rule, CHECK migration, policy for existing handles) or make the mention parser accept it (renderer version bump, existing posts can render differently). docs/04 and the mention syntax disagree and no document says which wins; see the decision note."
priority: normal
due_on: null
labels: []
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T21:42:17Z
updated_at: 2026-09-28T19:19:43Z
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
