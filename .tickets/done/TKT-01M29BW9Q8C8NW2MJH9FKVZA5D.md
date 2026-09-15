---
schema: 3
id: TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
title: Build the markdown pipeline with a renderer version
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
dependencies: []
blocks_on: none
references:
  - ref: design:render
    path: docs/06-thread-views.md
  - ref: ticket:TKT-01M2K1JKS7P0T68EFGH9A11VRS
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-15T17:24:45Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/render
  name: ""
extensions: {}
---

## Description

goldmark for markdown and bluemonday for sanitization, wrapped behind one function that every caller uses, carrying an explicit renderer version constant.

The version is not decoration. It is part of every cache key and part of the ETag, so bumping it is how a pipeline upgrade invalidates every piece of cached HTML in the system without rewriting a log or touching the database. Any change to the pipeline that can change output must bump it, and that rule needs writing down next to the constant.

Sanitization runs on output, with an allowlist. No raw HTML passthrough.

## Acceptance criteria

- [x] One exported function renders source to HTML and every caller in the codebase uses it
- [x] A golden test file covers the markdown features in scope and fails on any output change
- [x] Script tags, event handler attributes, and javascript: URLs in source are stripped, with tests for each
- [x] The renderer version constant is documented as required to change whenever output can change

## Implementation plan

internal/render with two files: render.go (Version, Result, Render, the goldmark pipeline) and policy.go (the bluemonday allowlist).

goldmark/v2 v2.1.1 with GFM, footnotes and typographer; bluemonday v1.0.27 on the output. Raw HTML is dropped twice, by goldmark and then by the sanitizer, and a test proves the second one alone is sufficient.

Three decisions that are not obvious from the code.

The allowlist is built from bluemonday.NewPolicy, not UGCPolicy. UGCPolicy is sized for arbitrary user HTML; this pipeline has goldmark's output, which is a short and knowable list. Starting from nothing means the allowlist and the list of things goldmark emits are the same list, so an element goldmark starts emitting after an upgrade is stripped until somebody adds it deliberately. Starting from UGCPolicy would let new output through because it happened to be on somebody else's list.

Table alignment is rendered as an align attribute rather than an inline style. Identical result, and the sanitizer then never has to parse CSS -- a whole parser, and a whole class of sanitizer bypass, that this pipeline simply does not contain.

Version is enforced rather than documented. Per-case output hashes live in testdata/pipeline.sum, and -update refuses to rewrite a case whose output changed unless Version was bumped first. Adding or removing a case is free; only a changed rendering forces a bump, which is exactly the set of changes that leave stale HTML in a cache.

## Notes

**agent:claude-code/render** at 2026-09-15T17:24:14Z

Criteria 1 and 4 are the two that a test normally cannot reach, so both were turned into mechanisms.

Criterion 1, 'every caller uses it', cannot be checked by looking at the callers, because the failure is a caller that does not exist yet. TestNothingElseRendersMarkdown walks every .go file in the repository and fails if anything outside internal/render imports goldmark or bluemonday, which is the only way a second pipeline could come into being. A second pipeline is how the composer preview stops matching the post -- the promise POST /render makes is only true while there is one path. Verified by adding a package that imports bluemonday and watching it fail.

Criterion 4, 'documented as required to change', is written in the package comment and then enforced. testdata/pipeline.sum holds a sha256 per golden case plus the version that produced them, and running the goldens with -update fails if any case's output changed while Version stayed the same. Adding or removing a case costs nothing; only a changed rendering forces a bump, which is exactly the set of changes that leave stale HTML in a cache. Verified by removing the typographer extension and running -update: refused, naming the case.

Criterion 3 is 46 cases over three tests, and the assertion had to be rewritten once. Substring matching was wrong twice in the same way: '<javascript:alert(1)>' and a link containing a quote both come out as escaped text in a paragraph, which is correct and safe, and a grep for 'javascript:' calls both failures. What matters is not whether a dangerous string is present but whether it is anywhere a browser acts on. So assertSafe parses the output with x/net/html and checks three things: every element is on the allowlist, no attribute name begins with 'on', and every URL attribute carries http, https, mailto or no scheme. Verified by disabling both defences: 50 findings across every case.

**agent:claude-code/render** at 2026-09-15T17:24:45Z

Two things left open, both recorded rather than silently decided.

Remote images are permitted from any http or https source, which tells that host who read the thread. Filed as TKT-01M2K1JKS7 against the render epic: the renderer is the wrong place to weigh it, because proxying and refusing only become possible once TKT-01M29CXK0 exists. If that decision lands as proxy or refuse, the renderer version bumps in the same change.

golang.org/x/net is now a direct dependency of the module, used only by the tests for html.Parse in assertSafe. Go does not separate test requirements in go.mod, so it appears in the main require block. It was already in the graph as a bluemonday dependency; nothing ships that did not ship before.

## Summary

internal/render is the one path from post source to HTML: goldmark/v2 with GFM, footnotes and typographer, then a bluemonday allowlist built from NewPolicy rather than UGCPolicy so that the allowlist and the list of things goldmark emits stay the same list.

Version is enforced, not just documented: per-case output hashes in testdata/pipeline.sum, and -update refuses a changed case unless the constant was bumped. 'Every caller uses this function' is enforced too, by a test that walks the repository and fails if anything outside the package imports goldmark or bluemonday.

13 golden cases cover the markdown in scope. 46 sanitization cases cover script tags, event handlers and dangerous URL schemes, asserted by parsing the output and checking elements, attribute names and URL schemes rather than by grepping for strings -- the first version grepped, and called two correct results failures.

Remote images stay permitted; the privacy trade is filed as TKT-01M2K1JKS7 for the attachment work to settle.
