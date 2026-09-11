---
schema: 3
id: TKT-01M29BKBM7004ZPAA9T81HC939
title: Markdown render pipeline and attachments
type: epic
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:render
  - effort:L
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies: []
blocks_on: children
references:
  - ref: design:thread-views-composer
    path: docs/06-thread-views.md
  - ref: design:storage-attachments
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:07:17Z
updated_at: 2026-09-11T23:31:54Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

`internal/render` and `internal/store/blob`: turning post source into HTML, and holding the files a post points at.

The log stores source, never rendered HTML, because storing HTML would double log size and couple the log to the renderer version. Rendering happens on read behind a cache whose key includes the renderer version, which makes a markdown pipeline upgrade free: bump the version and every cached entry falls out without rewriting a single log.

The same pipeline serves the composer preview over `POST /api/v1/render`, so the preview is exact and no client-side markdown parser ships. Rendering also extracts the structured facts a post carries: mentions, quote backlinks, and reply_to.

Attachments stay out of the log entirely. Files land in `blobs/sha256/xx/<hash>`, deduplicated by content hash, referenced from the post body by hash, and reference-counted in the database for garbage collection.

## Acceptance criteria

- [ ] Sanitized output is identical between the composer preview endpoint and the stored-post read path for the same source
- [ ] Bumping the renderer version invalidates every cached rendered post without touching a log file
- [ ] Mentions, quotes, and reply_to extracted at render time match what the projection writer records
- [ ] An attachment uploaded twice by different users is stored once and reference-counted twice
- [ ] Uploads outside the MIME allowlist are refused, and the allowlist is checked by sniffing rather than by the declared type
