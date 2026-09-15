---
schema: 3
id: TKT-01M2HTEXRV33P6HT37QG4FKX3R
title: Refresh the MVP critical path document against the store
type: chore
status: done
status_reason: Document rewritten and committed alongside this ticket.
priority: normal
due_on: null
labels:
  - area:docs
  - effort:S
assignees: []
milestone: phase-0
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: commit:cc98160
    path: null
claim: null
archive: null
created_at: 2026-09-15T06:00:56Z
updated_at: 2026-09-15T06:14:38Z
created_by:
  id: agent:claude-code/docs
  name: ""
updated_by:
  id: agent:claude-code/docs
  name: ""
extensions: {}
---

## Description

`docs/11-mvp-critical-path.md`: bring the document's numbers and claims back
into agreement with the store.

The document is a hand-written reading of `.tickets/`, so nothing validates it
and it rots without anyone noticing. It had drifted a whole phase: it claimed
155 tickets against 162, 18 in Phase 0 against 21, 63 off-path against 65, and
"every ticket is in draft" when 13 were done and 4 ready.

The substantive error was not a count. The document argued that Phase 0 is
less serial than it looks because "only five Phase 1 tickets actually depend
on it". Counted against the store it is seven directly and 21 transitively,
because the database epic carries much of the client behind it. The argument
survives — 51 of 72 Phase 1 tickets still never reach storage — but it is a
third weaker than stated, and staffing decisions were resting on it.

Also recorded: the route the document describes is prose, not a query. The
recorded `depends-on` graph has a longest chain of eight tickets, so reading
`git ticket deps` alone understates what has to happen first.

## Acceptance criteria

- [x] Ticket totals, per-phase distribution and status counts match git ticket list --all
- [x] The dependency claim is corrected to the measured figure, with the direct and transitive counts distinguished
- [x] The startable list reflects what is actually ready, and names the entries that are now done
- [x] The document says when its numbers were last read and how to re-read them, so the next drift is visible

## Notes

**agent:claude-code/docs** at 2026-09-15T06:01:25Z

draft to done: Document rewritten and committed alongside this ticket.

## Summary

docs/11-mvp-critical-path.md rewritten against git ticket list --all: 162 tickets not 155, Phase 0 at 21 not 18, 65 off-path not 63, and a Where it stands section replacing the claim that every ticket is in draft. The dependency claim went from five Phase 1 tickets to seven direct and 21 transitive, with 51 of 72 still independent of storage. Added the date the numbers were read, the command to re-read them, and a caveat that the recorded depends-on graph has a longest chain of eight against the twenty-ticket prose route.
