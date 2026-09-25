---
schema: 3
id: TKT-01M3BFSBPJY0DA4RYA7ASXJKKX
title: Stop nesting the quote source link inside its summary
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T05:14:39Z
updated_at: 2026-09-25T05:14:39Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
extensions: {}
---

## Description

A rendered quote is `<details class="quote" open><summary><a href="#post-N" class="quote-source">Post #N</a></summary><blockquote>…` (internal/render/quote.go). The link sits inside the `<summary>`, which is itself a control, the toggle that collapses the quote. axe reports this as `nested-interactive` (WCAG 4.1.2, serious). Assistive technology announces the summary as a button, and the link inside it may not be reachable or announced as a link.

TKT-01M29CGBXK's scan (web/client/e2e/a11y.spec.ts) sets that one rule aside for quote summaries only, and names this ticket. The markup is a design decision: `<details>` was chosen so a quote collapses without script, for the non-JS client (docs/07).

### Options
- Keep `<details>`, put plain text in the summary (for example "Quote from #N"), and move the link into the blockquote as its first line.
- Drop the collapse: render `<blockquote class="quote">` with a header line holding the link. This loses collapsing without script.
- Keep the markup and accept the finding.

### Things to settle with it
- Rendered HTML already in the thread cache and projection keeps the old shape unless posts are re-rendered.
- The client's quote handling (`a.quote-source` in ThreadScreen's `follow`) and its tests.
- The render goldens.
