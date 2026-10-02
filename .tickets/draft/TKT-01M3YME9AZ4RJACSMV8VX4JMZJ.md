---
schema: 3
id: TKT-01M3YME9AZ4RJACSMV8VX4JMZJ
title: Record quotes as backlinks, as docs/06 promises
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T15:41:33Z
updated_at: 2026-10-02T15:41:33Z
created_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

docs/06 says, for quotes: "the server records the quote as a backlink". `internal/render/render.go:82-84` says the render's quote list is "for the projection writer to record as backlinks". Nothing does this:
- The thread log's `Post` record carries `reply_to` and `mentions`, but no quotes (docs/03 record types).
- `internal/events/projection/apply.go` writes `post_meta.reply_to` and `mentions`, and stores no quotes. No migration has a quote or backlink table.
- The notifier finds quotes by rendering the source again, with an empty `render.Context{}` rather than the create-time resolver (`internal/events/notifier/notifier.go:655-672`): "Quotes are not in the log, so the source is rendered to find them".

Found on 2026-10-02 by the check of TKT-01M29BKBM7's criterion 3 ("Mentions, quotes, and reply_to extracted at render time match what the projection writer records").

### What to do
Decide where backlinks come from. Two options:
- Re-render the source in the projection writer. The log stays as it is, and reindex can rebuild the result.
- Add a quotes field to the `Post` record. That changes the frozen format, which is docs/15 stop class 2, so it needs the maintainer.

Prefer the first option. Then:
- Record the backlinks.
- Have the notifier read them, instead of rendering again with an empty context.
- Show them wherever docs/06 draws them.
