---
schema: 3
id: TKT-01M29CXK0KHC2QZQ286AG0KKGD
title: Build the content-addressed blob store and upload path
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
  - effort:L
assignees: []
milestone: phase-3
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29BYYJ0WJ3C7RRZQMDRP5ZB
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:storage-attachments
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:30:21Z
updated_at: 2026-09-11T23:30:21Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`POST /api/v1/blobs` as multipart, writing to `blobs/sha256/xx/<hash>`, deduplicated by content hash, with a `blobs` row carrying size, MIME, uploader, refcount, and status. Serving is at `/blobs/{hash}` outside the API with immutable cache headers.

Attachments are referenced from the post body by hash, filename, size, and MIME type. They never enter the log itself.

MIME is decided by sniffing against an allowlist, not by what the upload claims. A file named `.png` that is something else is the oldest trick in the book.

## Acceptance criteria

- [ ] The same bytes uploaded twice produce one file and two refcounts
- [ ] MIME type is determined by sniffing and a type outside the allowlist is refused
- [ ] Blobs serve with immutable cache headers from the hash-addressed URL
- [ ] The size cap is enforced during the read rather than after buffering the whole upload
- [ ] A quarantined blob is never served
