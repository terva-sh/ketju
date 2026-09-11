---
schema: 3
id: TKT-01M29BN36WRBJMH049G6V1GNHE
title: Non-JS server-rendered HTML client
type: epic
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:client
  - effort:XL
  - placeholder
assignees: []
milestone: phase-5
parent: null
origin: null
dependencies:
  - TKT-01M29BKBMCNDSYHYF9WXAPDNRN
  - TKT-01M29BN367F3YQZESM63VN26GP
blocks_on: none
references:
  - ref: design:non-js-path
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:08:14Z
updated_at: 2026-09-11T23:32:15Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Placeholder for Phase 5, post-1.0. Not broken down yet, by decision: the shape of this work depends on question 19 in docs/10, which asks whether the HTML client should become the primary shell with Preact as an enhancement layer rather than a second client.

`internal/web/html` with templ views for every URL in docs/07, form-post mutations, page-load read marking, an htmx layer for composer preview and expanders, and crawler plus `<noscript>` routing.

The constraints that keep this cheap are imposed now, in Phase 1, and each one is a ticket in another epic: shared DTOs, every mutation expressible as a form post, lenses and pages as URL state, server-side markdown preview, and `CrossOriginProtection` enabled from day one. If those hold, this epic is handler and template work over services that already exist.

Break this down at the Phase 5 boundary with real usage data on how many members would benefit and how much of the SPA is actually interactive.

## Acceptance criteria

- [ ] Question 19 in docs/10 is decided before this epic is broken down
- [ ] Every URL in the docs/07 scheme is served by the HTML client
- [ ] The HTML client is responsive from its first version, at the same breakpoints as the SPA
- [ ] No service or store code changes are needed to add it
