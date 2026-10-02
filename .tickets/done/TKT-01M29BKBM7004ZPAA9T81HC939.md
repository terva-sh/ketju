---
schema: 3
id: TKT-01M29BKBM7004ZPAA9T81HC939
title: Markdown render pipeline and attachments
type: epic
status: done
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
updated_at: 2026-10-02T15:41:59Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase4-run-orchestrator
  name: ""
extensions: {}
---

## Description

`internal/render` and `internal/store/blob`: turning post source into HTML, and holding the files a post points at.

The log stores source, never rendered HTML, because storing HTML would double log size and couple the log to the renderer version. Rendering happens on read behind a cache whose key includes the renderer version, which makes a markdown pipeline upgrade free: bump the version and every cached entry falls out without rewriting a single log.

The same pipeline serves the composer preview over `POST /api/v1/render`, so the preview is exact and no client-side markdown parser ships. Rendering also extracts the structured facts a post carries: mentions, quote backlinks, and reply_to.

Attachments stay out of the log entirely. Files land in `blobs/sha256/xx/<hash>`, deduplicated by content hash, referenced from the post body by hash, and reference-counted in the database for garbage collection.

## Acceptance criteria

- [x] Sanitized output is identical between the composer preview endpoint and the stored-post read path for the same source
- [x] Bumping the renderer version invalidates every cached rendered post without touching a log file
- [ ] Mentions, quotes, and reply_to extracted at render time match what the projection writer records
- [x] An attachment uploaded twice by different users is stored once and reference-counted twice
- [x] Uploads outside the MIME allowlist are refused, and the allowlist is checked by sniffing rather than by the declared type

## Notes

**agent:claude-code/phase4-run-orchestrator** at 2026-10-02T15:41:58Z

Verification on 2026-10-02, against origin/main at adeba570, done the same way as the other Phase 1 epics.
1. **Preview equals stored read: holds.** Both go through `render.RenderWith` with the same quote resolver (`preview.go:47,86`, `view.go:715-746`). Tests: `TestPreviewMatchesTheReadPath`, `TestPostedMatchesItsPreview`. Caveat: preview resolves mentions as of now, and the read path as of the revision, so a handle rename between the two could make them differ.
2. **Renderer version bump invalidates the cache: holds.** `render.Version` is in the cache key, the response cache key and the ETag. Tests: `TestBumpingTheRendererVersionMakesEveryEntryUnreachable`, and `TestGoldenOutput`, which refuses a changed golden without a bump.
3. **Mentions, quotes and reply_to match the projection: partly. Left unticked.** Mentions and reply_to are recorded (`create.go:355-363`, `projection/apply.go:95,112`). Quotes are not recorded anywhere, although docs/06 says the server records them as backlinks. Filed as TKT-01M3YME9AZ (Record quotes as backlinks, as docs/06 promises), Phase 4.
4. **Stored once, counted twice: holds under the decided model.** docs/03 (TKT-01M29CXK0) counts a reference when a post attaches the blob, not when it is uploaded. Tests: `TestUploadDedupOneFileOneRow`, `TestTwoPostsTwoRefs`.
5. **MIME allowlist by sniffing: holds.** `sniff` (`blobs.go:372`); the handler never reads the declared type. Tests: `TestUploadSniffsNotClaims`, `TestUploadRefusesOutsideAllowlist`, `TestUploadImageMustDecodeConfig`.

## Summary

Phase 1's render and attachments epic. All 12 children are done. On 2026-10-02, criteria 1, 2, 4 and 5 were verified against the code and its tests and ticked. Criterion 3 is unticked: quotes are never recorded as backlinks. That follow-up is TKT-01M3YME9AZ in Phase 4.
