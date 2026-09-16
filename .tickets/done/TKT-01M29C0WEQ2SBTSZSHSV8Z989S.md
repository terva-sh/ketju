---
schema: 3
id: TKT-01M29C0WEQ2SBTSZSHSV8Z989S
title: Add mention syntax, resolution, and autocomplete data
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:M
assignees: []
milestone: phase-1
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
blocks_on: none
references:
  - ref: design:composer
    path: docs/06-thread-views.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-16T19:10:46Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

`@handle` resolved at render time to a user id and rendered as a profile link, with the resolved ids returned so the notifier and the mentions projection can use them.

Mentions are stored by user id rather than by handle, which is what keeps old posts correct after a handle change. The renderer resolves the handle at render time, so the stored source keeps the text the author typed while the link and the notification follow the person.

## Acceptance criteria

- [x] A mention of an existing handle renders as a profile link and returns the resolved user id
- [x] A mention of a handle that does not exist renders as plain text and produces no notification
- [x] Mentions inside a code block or a quote are not resolved and produce no notification
- [ ] An endpoint supplies handle candidates for the composer's autocomplete

## Notes

**agent:claude-code/phase1** at 2026-09-16T19:10:46Z

### Criterion 4 is not done, and cannot be done here

"An endpoint supplies handle candidates for the composer's autocomplete" needs
two things this repository does not have yet: an HTTP layer
(TKT-01M29C5Y3S builds the server and router) and a users table
(TKT-01M29BYYH7 adds the schema). There is nothing to query and nowhere to
serve it from, so the box is left unticked rather than reinterpreted into
something this package could satisfy.

What is here for whoever does build it: HandleResolver is the interface the
endpoint and the renderer should share, so that the handles autocomplete offers
and the handles that resolve are the same set by construction. If the endpoint
grows its own query, the composer will eventually suggest a handle that the
renderer then declines to link, and that inconsistency will be reported as a
rendering bug.

The natural owner is TKT-01M29CEE79 (build the composer with server-rendered
preview), which needs the autocomplete anyway.

### The handle character set is a guess, deliberately a wide one

No document states what a handle may contain -- docs/04 says only "unique,
case-insensitive". The parser accepts letters, digits, underscore, hyphen and
dot, bounded at MaxHandle, and strips trailing dots and hyphens so that
"@alice." at the end of a sentence mentions alice.

The direction matters. This pattern decides only what becomes a *candidate*;
the resolver decides what is a user. A pattern wider than the real rule costs
nothing, because candidates that are not users render as text. A pattern
narrower than the real rule means mentions of some accounts silently never
resolving, which nobody reports as a bug because it looks like the person
simply was not mentioned.

If TKT-01M29BYYH7 or TKT-01M29BSPEN settles on a handle rule that allows a
character not in isHandleByte, this parser has to be widened to match.

### Two decisions that could reasonably have gone the other way

Mentions inside a *plain* blockquote are also suppressed, not only ones inside
an attributed quote. A blockquote without an attribution is still somebody
else's words most of the time, and the cost of being wrong in this direction is
a missing notification rather than an unwanted one.

Without a resolver, mentions render as plain text -- the opposite of quotes,
which render optimistically as links. A quote points at an ordinal in the thread
the reader is already on, so optimism is usually right. A mention would point at
a profile for a person who may not exist, and a preview that tells an author
they have addressed somebody who is not there is worse than one showing text.

### Stale links after a rename

The link uses the canonical handle at render time, so re-rendering fixes it. HTML
already in the rendered-post cache keeps the old link until it is evicted.
Whether a rename should invalidate those entries belongs with TKT-01M29BSPEN,
which owns the handle change policy; render.Cache.InvalidateThread and
InvalidatePost are what it would call.

## Summary

internal/render: @handle resolves at render time to a user id and renders as a profile link using the canonical spelling; RenderWith reports the ids. Mentions in code, in a quotation, or inside a link are text and are not reported. Criteria 1 to 3 are done; criterion 4, the autocomplete endpoint, is left unticked because there is no HTTP layer and no users table yet, and HandleResolver is the interface it should share. Version bumped to 3.
