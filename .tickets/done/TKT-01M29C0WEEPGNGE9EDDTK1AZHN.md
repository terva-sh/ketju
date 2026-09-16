---
schema: 3
id: TKT-01M29C0WEEPGNGE9EDDTK1AZHN
title: Add quote syntax, linked blockquotes, and quote backlinks
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
updated_at: 2026-09-16T19:35:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

A goldmark extension for the `[quote=#37]` attribution line, rendering it as a collapsible blockquote linked to post 37, and recording the quote as a backlink so the quoted post can show who quoted it.

Quoting is how a linear thread refers backwards without indenting, so it carries weight the syntax does not suggest. A quote of a post that was later deleted must render as a quote of a tombstone rather than leaking the removed text.

## Acceptance criteria

- [x] A quote block renders with a link to the quoted ordinal and is collapsible in the client
- [x] Quoting a deleted post renders the tombstone rather than the original text
- [x] Backlinks are emitted from the render call so the projection writer can record them
- [x] A quote of an ordinal that does not exist renders as a plain blockquote rather than failing the post

## Notes

**agent:claude-code/phase1** at 2026-09-16T19:04:28Z

### The tombstone is not what the criterion sounds like

"Quoting a deleted post renders the tombstone rather than the original text"
reads like a lookup problem. It is not. The quoted text is never fetched from
the quoted post: the composer copies it into the quoting post's own source when
somebody clicks "quote", so deleting post 37 does nothing whatsoever to the copy
sitting in post 50.

Dropping the quotation's children at render time is therefore the only thing
that makes a delete effective anywhere other than the original. Without it, a
moderator removing a post leaves the text visible in every post that quoted it,
and the delete looks like it worked.

The backlink is still emitted, with Deleted set, so the tombstone can show how
far the removed text had spread. That is the number a moderator needs after
removing something.

### Syntax decision

The attribution is the first line inside the blockquote rather than a line above
it. Any renderer that does not know this extension still shows the quoted text
as a quotation with one unfamiliar line at the top, instead of an orphan line
followed by an unattributed quote. That covers plain markdown viewers, mail
notifications and any future export.

### What the renderer does when it cannot resolve

- Ordinal does not exist: demoted to a plain blockquote, content kept, no
  backlink. It must not fail the post and must not link to an anchor that is
  not on the page.
- No resolver at all (composer preview, rendered before the post exists):
  linked optimistically and the backlink reported. Declining to link would make
  the preview disagree with the posted result, and that agreement is the reason
  this package is the only renderer in the system.

### Two things the next reader should know

`Version` is bumped to 2. No existing golden case changed -- a plain blockquote
still goes through goldmark's own renderer -- but the pipeline can now produce
different output, and the rule is about what can change rather than what did.

The sanitizer allowlist gained `<details>` and `<summary>`, which is a real
widening and is named in the test that previously asserted they were stripped.
They are what makes a quote collapse without script, which the non-JS client
needs as much as the Preact one. Neither takes a URL or an event handler, and
the only attributes allowed are a fixed class and an empty `open`.

### A goldmark v2 trap

goldmark v2's BaseNode keeps a reference to the concrete node it is embedded in,
and AppendChild uses it to set each child's parent. A custom node that does not
call Init in its constructor silently orphans every child moved into it. The
symptom here was a nested quotation that parsed correctly and then rendered as a
plain blockquote. Anyone adding a node type for mentions or anything else needs
the same Init call.

### Where the validation actually is

The ordinal pattern is digits-only, but that is not the guarantee. Loosening it
to accept any text changes nothing: ParseUint refuses non-numbers, the range
check refuses what is out of range, and the value reaching the href is written
with %d from a uint32, so no string from the source is ever formatted into an
attribute. This was checked by loosening the pattern and watching the injection
tests keep passing, and the comment says so rather than claiming a defence the
pattern does not provide.

## Summary

internal/render: [quote=#N] as the first line of a blockquote becomes a collapsible <details> quotation linked to the ordinal, and RenderWith reports Quote backlinks. A quote of a deleted post drops the copied text and renders a tombstone, which is the only thing that makes a delete effective in the posts that quoted it. An unresolvable ordinal degrades to a plain blockquote. Version bumped to 2.
