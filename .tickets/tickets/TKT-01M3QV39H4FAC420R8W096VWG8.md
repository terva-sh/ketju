---
schema: 3
id: TKT-01M3QV39H4FAC420R8W096VWG8
title: Clear the agent limits form's Saved status when a field is edited
type: bug
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area:moderation
  - area:client
  - effort:XS
assignees: []
milestone: phase-2
parent: TKT-01M29BN36JH4M1W5819D06PGPP
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/phase2-run-qv39h4
  branch: run/qv39h4-limits-saved-status
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ab684c2d5c5936692
  commit: 13709f0d74ab93dbdc040da9e4bd218ed6f0a324
  session: null
  claimed_at: 2026-09-30T18:42:52Z
  expires_at: null
archive: null
created_at: 2026-09-30T00:23:12Z
updated_at: 2026-09-30T18:43:55Z
created_by:
  id: agent:claude-code/phase2-run-qh3gs2
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-qv39h4
  name: ""
extensions: {}
---

## Description

Terva review 1510 on PR #179 (head a064d60), low: after a successful save of an agent's write limits (web/client/src/screens/admin/AgentPanel.tsx, Limits), editing either field leaves 'Saved.' beside values that have not been saved. An edit made while the PUT is in flight is also labelled saved when the answer arrives. Clear the saved status on any input, and on the answer set it only if the fields still hold what was sent. Client only; it regenerates the committed dist, so it takes the client lane.

## Acceptance criteria

- [ ] Editing either limit field after a save removes 'Saved.' (AgentControls.test.tsx)
- [ ] An answer to a save does not show 'Saved.' when a field was edited while the save was in flight (AgentControls.test.tsx)
- [ ] An answer shows 'Saved.' when both fields still hold the text that was sent (AgentControls.test.tsx)
- [ ] The dist from go generate ./internal/web/spa is committed

## Implementation plan

Revised 2026-09-30 on claim (agent:claude-code/phase2-run-qv39h4), against main at 13709f0. The groomed plan (written against 9a8fc76 and confirmed current at 5124209) still fits, so the approach below is unchanged. `AgentPanel.tsx` and `AgentControls.test.tsx` changed since only in tests: #239 (TKT-01M3P0CYR3) added a `?user=` history test and #229 (TKT-01M3QMRE47) changed a moderator's button list. Neither touches `Limits`. What those PRs changed around the panel, checked:
- #239 keys `UserDetail` by the selected handle, so a rename or a `?user=` change remounts `Limits` with fresh state. A "Saved." cannot survive onto another account.
- #229's `SuspensionPanel` refetch replaces the whole detail with `setFresh`. That reaches `Limits` through the existing `given` effect, which rewrites only unedited fields. If a refetch rewrites a field while a save is in flight, the answer's comparison fails and "Saved." stays hidden. That errs towards not claiming a save, which is the rule.
- This is admin-chunk code only. The reader bundle should not change size; the PR reports both figures.

The form is `Limits` in `web/client/src/screens/admin/AgentPanel.tsx`, from TKT-01M29P9RSW's admin half (PR #179). The route is `PUT /api/v1/admin/users/{handle}/agent-limits` with `{burst, refill_seconds}`, each a number or null for the site default. It answers 200 `AdminUserDetail`. Nothing on the server changes.

### Settled in grooming

- **The limits and their bounds are the maintainer's** (decision 2026-09-29, TKT-01M29P9RSW, docs/08 "Rate limits"): a burst of 20 and one token every 30 s by default, a refill of at most 604800 s (`settings.AgentRefillMaxSeconds`, mirrored as `refillMax` in `AgentPanel.tsx`). This ticket changes neither.
- **"Saved." means the fields show what the server holds.** Any edit to either field clears it. An answer shows it only when both fields still hold the text that was sent.
- **The comparison is on the field text, not on an edit counter** (engineering choice). The ticket asks for "the fields still hold what was sent". A counter bumped on every input lost: typing a character and deleting it would hide "Saved." beside values that are, in fact, saved.
- **The comparison reads the fields through a ref.** The `save` closure holds the `burst` and `refill` of the render that submitted, so it cannot see an edit made while the PUT is in flight. A ref that each render sets to the current `{burst, refill}` is what `save` reads when the answer comes back. Moving the check into a `useEffect` on `[burst, refill, saved]` lost: it would show "Saved." for one render before clearing it.
- **The error line is unchanged.** Clearing `failed` on input is a different behaviour that nobody reported, and it would hide a refusal before the admin has read it.
- **The built client is committed** (docs/13 "Running two lanes"). This ticket regenerates `internal/web/spa/dist` with `go generate ./internal/web/spa`, so it takes the client lane.

### Approach

1. `web/client/src/screens/admin/AgentPanel.tsx`, `Limits`:
   - Add `const current = useRef({ burst, refill })`, and set `current.current = { burst, refill }` on every render.
   - Each field's `onInput` sets its value and calls `setSaved(false)`.
   - In `save`, take `const sent = { burst, refill }` before the PUT. On an `ok` answer, call `setFailed(null)` and `onSaved(res.value)` as now, then `setSaved(current.current.burst === sent.burst && current.current.refill === sent.refill)`.
   - The `useEffect` that carries a refetched detail into unedited fields stays as it is. After a save it can only rewrite a field that still shows the value it was given, so it does not make a saved field look edited.
   - Update the comment above `Limits` to say when "Saved." shows.
2. Run `go generate ./internal/web/spa` and commit `internal/web/spa/dist`.

### Tests

In `web/client/src/screens/admin/AgentControls.test.tsx`, a new `describe('the saved status', …)` using the file's `renderAt`, `open`, `type` and `submit` helpers:
- `clears when either field is edited after a save`: save a burst of 7 and see "Saved.". Type into the burst field and "Saved." is gone. Save again, then type into the refill field, and it is gone.
- `does not show for an answer to fields edited in flight`: hold the PUT's answer in a promise, as `changes in flight together` does. Submit with a burst of 7, type 8 into the burst field, then answer 200. "Saved." is absent, the field still reads 8, and no second PUT was sent.
- `shows when the fields still hold what was sent`: the same held answer with no edit shows "Saved.".
- The existing `a limits answer that arrives after a new token does not bring the old token back` still passes. Its fields are unedited during the flight, so it still sees "Saved.".

### Out of scope

- Server changes to the limits route (TKT-01M29P9RSW, done).
- Clearing the error line on input (see Settled).

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T08:58:30Z

Plan refreshed against main at 5124209 on 2026-09-30. The plan is current, so it is not rewritten. Checked: AgentPanel.tsx and AgentControls.test.tsx are unchanged since the plan's base 9a8fc76 (their last commit, d512bd3, is an ancestor of it). Limits still sets 'Saved.' on any ok answer. The helpers the tests use (renderAt, open, type, submit) and the case 'a limits answer that arrives after a new token does not bring the old token back' exist. The only stale detail is the docs/13 section, now titled 'Running two lanes, changed', with the same one-lane rule. TKT-01M29CXK2H's settings screen adopts this ticket's saved-status rule for its own fields.
