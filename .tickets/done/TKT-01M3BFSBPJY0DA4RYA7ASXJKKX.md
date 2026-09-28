---
schema: 3
id: TKT-01M3BFSBPJY0DA4RYA7ASXJKKX
title: Stop nesting the quote source link inside its summary
type: bug
status: done
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
references:
  - ref: pr:127
    path: null
claim: null
archive: null
created_at: 2026-09-25T05:14:39Z
updated_at: 2026-09-28T19:03:31Z
created_by:
  id: agent:claude-code/phase1-run-415af6aa
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
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

## Implementation plan

### Settled in grooming

Maintainer decision, 2026-09-28, "Keep collapse, link in body" (note on this ticket):

- Keep `<details class="quote" open>`, so a quote collapses without script (the non-JS client, docs/07).
- The `<summary>` becomes plain text "Quote from #N", with no link.
- The source link moves into the blockquote as its first line.
- Bump the renderer version, so cached posts re-render.
- Update the client's quote handling (`a.quote-source` in ThreadScreen's `follow`), its tests, and the render goldens (`-update`).
- Remove the `nested-interactive` exception from web/client/e2e/a11y.spec.ts. The axe scan must pass without it.

### Markup

```html
<details class="quote" open>
<summary>Quote from #N</summary>
<blockquote>
<p class="quote-attribution"><a href="#post-N" class="quote-source">Post #N</a></p>
<p>quoted text</p>
</blockquote>
</details>
```

The link keeps its class and text, so `follow` in ThreadScreen still finds it with `a.quote-source`. A tombstone keeps the link as the first line, then "This post was deleted.", as before. The first-line paragraph takes a class, `quote-attribution`, so the stylesheet can space it from the summary on the phone layout, where the link's 44 px target (base.css) would otherwise overlap the summary's.

### Steps

1. internal/render/quote.go: write the new markup. policy.go: allow `quote-attribution` on `p`, beside `quote-deleted`, exact match. Bump `Version` 3 -> 4. Tests: quote_test.go asserts the summary holds no link, the link is inside the blockquote, and the tombstone keeps the link; a policy test that the new class survives sanitizing. Regenerate goldens with `-update` (quotes, mentions, pipeline.sum).
2. internal/dto fixture and golden (render.member.json): update the example HTML to the new shape; it is a hand-written fixture of the renderer's output.
3. Client: ThreadScreen `follow` comment; tests (ThreadPage.test.tsx, quote.test.ts, Composer.test.tsx) to the new markup; CSS in thread.css (attribution line, phone-layout summary comment) and base.css comment; e2e: drop the nested-interactive exception in a11y.spec.ts, and move targets.spec.ts selectors from `summary .quote-source` to `details.quote .quote-source`.
4. `go generate ./internal/web/spa`, commit dist; budget; tsc, vitest, playwright on port 8794; the Go CI steps.

### Cache and disk

Rendered HTML lives only in memory: internal/render Cache (key carries `Version`) and internal/service/respcache (key carries `Renderer: render.Version`); the thread ETag carries `render.Version` (internal/service/threads/view.go). A bump retires all three at the next start. The logs hold source (docs/03 "Storing rendered HTML alongside source" lost), the projection holds quote ordinals from `Result.Quotes`, not HTML, and no SQL migration has an HTML column. No migration, no `ketju reindex`, no change to the frozen log format.

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T18:06:55Z

### Maintainer decision, 2026-09-28: "Keep collapse, link in body"

- Keep `<details class="quote" open>`, so a quote still collapses without script (the non-JS client, docs/07).
- The `<summary>` becomes plain text, "Quote from #N", with no link.
- The source link moves into the blockquote as its first line.

This removes the `nested-interactive` exception from web/client/e2e/a11y.spec.ts. Bump the renderer version, so cached posts re-render with the new shape. Update the client's quote handling (`a.quote-source` in ThreadScreen's `follow`), its tests, and the render goldens.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T19:02:49Z

### Built, and what re-rendering touches

Markup as the plan says. Two internal choices, settled here (docs/12 "Gaps"): the attribution paragraph takes the class `quote-attribution`, allowed by the sanitizer on `<p>` by exact match beside `quote-deleted`; and on the phone layout it takes a 16 px top margin. Without the margin the link's 44 px target (base.css `::after`, 13.5 px above its line) covers the summary's bottom edge. targets.spec.ts did not catch that, because it presses the summary at its centre and the summary is wider than the link, so the thread-page test now presses the summary's edge directly above the link. Setting the margin to 0 fails that check.

Sanitizer: the goldens show `class="quote-attribution"` and `class="quote-source"` surviving bluemonday; policy_test covers `quote-attribution x` and an arbitrary class being dropped.

Cache and disk: rendered HTML is held only in memory, in the render.Cache (key has `Version`, TKT-01M29C0WF0) and in respcache (key has `Renderer: render.Version`). The thread ETag also carries `render.Version` (internal/service/threads/view.go). Version 3 -> 4 retires all three when the new binary starts, and clients revalidate against the new ETag. Logs store source (docs/03), the projection stores quote ordinals from `Result.Quotes`, not HTML, and no migration has an HTML column. No migration, no `ketju reindex`, no log format change.

Checks run locally: gofmt, go build, go vet, go test -race ./..., tsc --noEmit, Vitest 920 passed, Playwright 53 passed / 9 skipped (port 8794), budget 38171 B of 60000 B.

## Summary

A quote renders as <details class="quote" open> with a plain-text <summary> ("Quote from #N") and the source link as the blockquote's first line, <p class="quote-attribution"><a class="quote-source">. Renderer Version 3 -> 4, which retires the in-memory render and response caches and the ETags; nothing on disk holds HTML, so no migration or reindex. The axe scan passes with no exceptions, and targets.spec.ts now checks the header's edge above the link.
