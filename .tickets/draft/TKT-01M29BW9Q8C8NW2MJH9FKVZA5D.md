---
schema: 3
id: TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
title: Build the markdown pipeline with a renderer version
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-11T23:12:10Z
updated_at: 2026-09-11T23:12:10Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

goldmark for markdown and bluemonday for sanitization, wrapped behind one function that every caller uses, carrying an explicit renderer version constant.

The version is not decoration. It is part of every cache key and part of the ETag, so bumping it is how a pipeline upgrade invalidates every piece of cached HTML in the system without rewriting a log or touching the database. Any change to the pipeline that can change output must bump it, and that rule needs writing down next to the constant.

Sanitization runs on output, with an allowlist. No raw HTML passthrough.

## Acceptance criteria

- [ ] One exported function renders source to HTML and every caller in the codebase uses it
- [ ] A golden test file covers the markdown features in scope and fails on any output change
- [ ] Script tags, event handler attributes, and javascript: URLs in source are stripped, with tests for each
- [ ] The renderer version constant is documented as required to change whenever output can change
