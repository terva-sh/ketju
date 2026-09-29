---
schema: 3
id: TKT-01M3NRX2NN2RB8C37HP4WBTEG5
title: Build the admin applications review screen
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:client
  - area:moderation
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM38VTYY4Y91018GTH6
origin: null
dependencies:
  - TKT-01M29CP2MGC6FD35PJHR2Y2WNA
  - TKT-01M29CP2NGWH32C6M718QHB30X
  - TKT-01M29CEE4KNQBFP49FVSCRNZA4
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:06:22Z
updated_at: 2026-09-29T05:08:42Z
created_by:
  id: agent:claude-code/phase2-groom
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The client screen at `/admin/applications` (docs/07 URL scheme), which no ticket owned. TKT-01M29CP2MG builds the review API and TKT-01M29CRJ4 builds the unified queue. Without this screen, a moderator has no page to open an application, read its answers, and approve or deny it.

This is what makes the epic's exit criterion usable: a club can open registration to strangers only if a moderator can review them in the browser.

It lives under `web/client/src/screens/admin/`, so it stays in the lazy admin chunk that `TestTheAdminBundleIsSeparate` and `bundle.mjs` keep out of the reader's bundle. The answers render through the read-only mode of the shared form renderer in `web/client/src/forms/`, which TKT-01M29CP2NG builds. The same component the applicant filled in therefore shows the moderator what was answered, against the version it was answered on.

docs/10 keeps SMTP out of v1, so the screen states that notifications are in-app only and that applicants learn the outcome on their status page (TKT-01M29CP2N0's description asks for this to be stated in the admin UI rather than discovered).

Remember: the built client dist is committed. Run `go generate ./internal/web/spa` after any client change, and keep `npm run budget` within 60000 B gzip.

## Acceptance criteria

- [ ] /admin/applications lists applications newest first from GET /api/v1/admin/applications, pending by default with a status filter, and pages with next_cursor
- [ ] Opening an application shows its answers through the shared src/forms renderer in read-only mode, against the form version it was answered on, with the identity's provider, email and verified flag, its prior applications and the moderators' notes
- [ ] A moderator can add a note, approve with the suggested handle prefilled and editable, and deny with a required note and a 'block this subject' checkbox
- [ ] A 409 from approve or deny re-fetches the application and says who decided it; a handle error from the errors extension attaches to the handle field
- [ ] The screen states that notifications are in-app only and that applicants learn the outcome on their status page
- [ ] The screen lives under src/screens/admin so TestTheAdminBundleIsSeparate passes, works at 390 px and 1440 px, the dist is regenerated with go generate ./internal/web/spa and committed, and npm run budget stays within 60000 B gzip
