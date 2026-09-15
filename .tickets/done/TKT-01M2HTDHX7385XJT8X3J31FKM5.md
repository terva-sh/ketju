---
schema: 3
id: TKT-01M2HTDHX7385XJT8X3J31FKM5
title: Pin CI to git-ticket v0.18.0 for the demoted-heading check
type: chore
status: done
status_reason: Merged as 19b68a8 on main; CI run 27 installed v0.18.0 and the store check reported no problems.
priority: normal
due_on: null
labels:
  - area:ops
  - effort:S
assignees: []
milestone: phase-0
parent: TKT-01M29BN36Q9D32WQGYPFS336J0
origin: null
dependencies: []
blocks_on: none
references:
  - ref: commit:19b68a8
    path: null
claim: null
archive: null
created_at: 2026-09-15T06:00:11Z
updated_at: 2026-09-15T06:14:38Z
created_by:
  id: agent:claude-code/ci
  name: ""
updated_by:
  id: agent:claude-code/ci
  name: ""
extensions: {}
---

## Description

`.forgejo/workflows/ci.yml`: move `GIT_TICKET_VERSION` from v0.17.1 to
v0.18.0, so the store check in CI can see acceptance criteria written as a
sub-heading.

v0.18.0 carries the fix for TKT-01M2HEEX, filed against terva-sh/git-ticket
from this repository after `ac --check` failed on TKT-01M2BXKF8F. The criteria
there had been written as `### Acceptance criteria` inside the Description,
where they are prose no mutation can reach, and `check --strict` passed the
ticket anyway. `check` now reports that shape as `section_heading_demoted`.

The bump is safe because the gate it adds has nothing behind it: both this
store and git-ticket's own report "No problems found" unchanged under v0.18.0.

## Acceptance criteria

- [x] CI installs git-ticket v0.18.0 and the store check runs under it, confirmed from the step log rather than the run summary
- [x] A ticket writing a format-owned section as a sub-heading fails check --strict; ### Scope and ### Trigger stay silent
- [x] Both the ketju and git-ticket stores still pass unchanged, so the gate ships without a backlog of repairs

## Notes

**agent:claude-code/ci** at 2026-09-15T06:01:24Z

draft to done: Merged as 19b68a8 on main; CI run 27 installed v0.18.0 and the store check reported no problems.

## Summary

GIT_TICKET_VERSION moved v0.17.1 to v0.18.0 in .forgejo/workflows/ci.yml. Verified in a scratch store before bumping: v0.17.1 reported 'No problems found' exit 0 on a ticket carrying ### Acceptance criteria, v0.18.0 reported section_heading_demoted and exit 1 under --strict, and ### Scope and ### Trigger stayed silent under both. CI run 27 step log confirms it downloaded v0.18.0 and the store check passed under it. Merged as 19b68a8.
