---
schema: 3
id: TKT-01M29CYS9W8GAWNQVHFH7HZ7AR
title: Generate image thumbnails for attachments
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area:render
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29CXK0KHC2QZQ286AG0KKGD
blocks_on: none
references:
  - ref: design:storage-attachments
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-09-11T23:31:00Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

Server-side thumbnails for image attachments, cached next to the blob and served with the same immutable headers.

Decoding user-supplied images is the riskiest thing the server does with untrusted bytes, so bounds on dimensions and pixel count matter as much as the file size cap. A 100 KB file can decode to gigabytes.

Pure Go decoders only, since the whole binary is cgo-free and a thumbnail feature is not worth losing that.

## Acceptance criteria

- [ ] Thumbnails are generated with pure Go decoders and no cgo
- [ ] Dimension and pixel-count limits are enforced before decoding, refusing a decompression bomb
- [ ] Thumbnails are cached beside the blob and regenerated only when missing
- [ ] An image that fails to decode leaves the attachment usable as a download rather than failing the post
