---
schema: 3
id: TKT-01M3N24RM0SK783PPS7E98HF67
title: Render remote markdown images as links rather than broken images
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: phase-2
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:28:37Z
updated_at: 2026-09-28T22:28:37Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

The CSP's `img-src 'self' data:` (docs/07, `internal/web/security.go`) already stops the browser loading a remote image, so a markdown image with an http or https source renders as a broken image. Decided 2026-09-28 in TKT-01M2K1JKS7 (Decide whether remote images are proxied, refused, or left alone): keep the CSP, and have the renderer turn such an image into a plain link to its URL, with the alt text as the link text, or the URL when there is no alt text.

Attachment images, which are same-origin, and `data:` images the sanitizer already allows are unchanged. The rendered output changes, so the renderer version (docs/03 "Renderer version") must be bumped, and cached renders are invalidated.

## Acceptance criteria

- [ ] A markdown image with a remote http or https source renders as a link to that URL, never as an img element
- [ ] Same-origin attachment images still render as images
- [ ] The renderer version is bumped and a render test covers both cases
