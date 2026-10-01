---
schema: 3
id: TKT-01M29CYS9W8GAWNQVHFH7HZ7AR
title: Generate image thumbnails for attachments
type: task
status: done
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
  - ref: pr:288
    path: null
  - ref: pr:302
    path: null
claim: null
archive: null
created_at: 2026-09-11T23:31:00Z
updated_at: 2026-10-01T12:25:53Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase3-cys9w
  name: ""
extensions: {}
---

## Description

Server-side thumbnails for image attachments, cached next to the blob and served with the same immutable headers.

Decoding user-supplied images is the riskiest thing the server does with untrusted bytes, so bounds on dimensions and pixel count matter as much as the file size cap. A 100 KB file can decode to gigabytes.

Pure Go decoders only, since the whole binary is cgo-free and a thumbnail feature is not worth losing that.

## Acceptance criteria

- [x] Side and decode-memory bounds are checked from DecodeConfig before any decode, so a PNG, JPEG or GIF bomb under 1 KB is refused with no decoder call and under 1 MiB allocated (TestThumbnailRefusesBombBeforeDecode, TestThumbnailBudgetCountsDepth)
- [x] A thumbnail is <hash>.thumb beside the blob, fitted inside 160 px, made once and made again only when missing (TestThumbnailFitsAndKeepsAspect, TestThumbnailFormats, TestThumbnailMadeOnceAndCached)
- [x] A thumbnail is served only to the viewers its blob is served to, through the same Authorize (TestThumbnailServedOnlyToReaders, every row of the blob table)
- [x] Decodes run one at a time, and two readers of a new image make one decode (TestThumbnailsDecodeOneAtATime, TestThumbnailMadeOnceAndCached)
- [x] Blob GC and ketju redact remove a blob's thumbnail with it, and GC removes a thumbnail whose blob has no row (TestCollectorRemovesThumbnail, TestCollectorRemovesOrphanThumbnail, TestRedactRemovesThumbnail)
- [x] docs/03 and docs/07 name the thumbnail file and route in the same PR
- [x] Only the standard library's image/png, image/jpeg and image/gif decode untrusted bytes (TestThumbImportsOnlyStdlibImage), and a new CI step builds ./cmd/ketju with CGO_ENABLED=0
- [x] An image that fails to decode, or panics the decoder, leaves the blob served and the post rendered with its attachment, and is not decoded again while it stays in the 4096-entry failure cache (TestThumbnailDecodeFailureLeavesBlobServable, TestDecodePanicIsContained)

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48. Read for this plan:
- docs/03 "Attachments" and "Blob garbage collection", with the run decision on thumbnails (2026-10-01);
- docs/07 "Attachments" (run decision 2026-10-01);
- the plan of TKT-01M29CXK0K (Build the content-addressed blob store and upload path);
- `internal/store/blob/blob.go`, `internal/jobs/blobgc.go` (`collect`, `recoverTrash`, `trashSuffix`) and `cmd/ketju/redact.go` (`deleteBlobs`);
- `go.mod`, which has no image dependency, and `.forgejo/workflows/ci.yml`.

### Settled in grooming

- **Decoders: the standard library only.** The run decided this on 2026-10-01 (docs/07 "Attachments", pending maintainer review), and this is the record of the choice.
  - `image/png`, `image/jpeg` and `image/gif` decode untrusted bytes. Each is called directly, by the type the upload sniffed, never through `image.Decode`'s format registry.
  - The resize is hand-written area averaging into an `*image.NRGBA`, about 60 lines.
  - `golang.org/x/image` lost. It would bring WebP and better filters, but it is a second decoder surface with no fuzzing history in this tree, and a module dependency for a 160 px thumbnail.
  - The binary stays cgo-free, which also rules out libvips and ImageMagick.
  - WebP is not on the upload allowlist for the same reason.
- **Name, size and format** are the docs/03 run decision: `<hash>.thumb` beside the blob, fitted inside 160 × 160 px, JPEG for a JPEG blob and PNG for a PNG or GIF blob. It is a cache, made on first request and made again whenever it is missing.
- **Route and access.** `GET /blobs/{hash}/thumb` (docs/07 "Attachments"), authorized by the same `blobs.Service.Authorize` as the blob. A thumbnail therefore reaches exactly the viewers the blob reaches (docs/14 "Invariants, added").
- **Bounds come before decoding** (the ticket's description: "a 100 KB file can decode to gigabytes"). The checks use the type's own `DecodeConfig`, which reads only the header:
  - each side at most 16384 px;
  - width × height × bytes per pixel at most `MaxDecodeBytes`, which is 256 MiB.

  Bytes per pixel is a deliberate overestimate of the decoder's peak:
  - 8 for a 16-bit PNG and 4 for any other PNG;
  - 4 for a GIF, whose first frame is bounded by the logical screen;
  - 16 for a JPEG, because a progressive JPEG keeps an int32 coefficient for every sample of every component.

  So a JPEG thumbnail is made up to about 16.7 megapixels. A larger photo is still served, as a card with no thumbnail, which is the failure case the ticket already requires.
- **One decode at a time.** A semaphore of one bounds memory to one `MaxDecodeBytes` budget, and a per-hash singleflight stops two readers of a new post from decoding the same file twice.
- **Failures are remembered in memory, not on disk, on a best-effort basis.** An LRU of 4096 hashes that failed to decode stops a reload from paying for the decode again, while the hash stays in it. It is forgotten on restart, and an entry evicted by 4096 newer failures may be decoded once more. That is bounded by the one-at-a-time semaphore and the size bounds, so a retried failure costs time, never memory (Terva review 1712) (docs/03 decision: "a cache entry that means 'never' outlives the decoder that failed").

### Approach

1. **`internal/store/blob/thumb.go`** (new):
   - `ThumbPath(root, hash)`, which is `Path(root, hash) + ".thumb"`;
   - the constants `ThumbEdge = 160`, `MaxSide = 16384` and `MaxDecodeBytes = 256 << 20`;
   - the errors `ErrTooLarge`, `ErrDecode` and `ErrNotImage`;
   - `Thumbnail(f io.ReadSeeker, mime string) (data []byte, contentType string, err error)`:
     1. `DecodeConfig` with the type's package, then the bounds. The check returns `ErrTooLarge` before any decode.
     2. Rewind, then `Decode` with the same package inside a `defer recover()`. A panic or an error is `ErrDecode`.
     3. Area-average the result into the fitted size.
     4. Encode with `jpeg.Encode` (quality 82) or `png.Encode`.

     A package-level `decodeHook` lets the tests count calls into the decoders.
2. **`internal/service/blobs`** (from TKT-01M29CXK0K):
   - `Thumb(ctx, v, hash) (path, contentType string, err error)` first calls `Authorize`. A type outside png, jpeg and gif is `not-found`.
   - An existing `ThumbPath` is served as it is.
   - Otherwise the method enters the singleflight for the hash and the semaphore, waiting with `ctx`, and calls `Thumbnail` on the blob file. It writes the result through `blob.Stage` and renames it to `ThumbPath`, with no `fsync` because the file is a cache, and serves it.
   - `ErrTooLarge` and `ErrDecode` enter the failure LRU and answer `not-found`, and the blob itself stays servable.
   - Errors go to the log at warn, with the hash and never the bytes.
3. **`internal/web/blobs`**: `GET` and `HEAD /blobs/{hash}/thumb` set the blob's headers: `private, no-cache`, `inline` disposition, and `ETag: "<hash>.thumb"`. `Authorize` runs before any 304, as for the blob (TKT-01M29CXK0K, after Terva review 1696), and the file is served through `http.ServeContent`.
4. **Removal with the blob.**
   - `BlobGC.collect` removes `ThumbPath` after the row's deletion commits. A failed removal is logged and left for the next pass.
   - A new step in `Run`, beside `recoverTrash`, globs `blobs/sha256/*/*.thumb` and removes each one whose `BlobExists` is false, inside a `db.Write` so that no upload interleaves.
   - `redactor.deleteBlobs` adds `ThumbPath` to the files it removes after the commit.
   - docs/03's sentence "Thumbnails are not named yet" is replaced by a pointer to the decision.
5. **docs/07** gains a row for `GET /blobs/{hash}/thumb`.
6. **CI.** `.forgejo/workflows/ci.yml` gains the step `CGO_ENABLED=0 go build ./cmd/ketju`.

### Tests

- `TestThumbImportsOnlyStdlibImage` (a layering test in `internal/store/blob`): the package imports no module outside the standard library, and imports no image package other than `image`, `image/color`, `image/png`, `image/jpeg` and `image/gif`. CI's build and race steps run with cgo, and only the sqlc step sets `CGO_ENABLED=0`. So this ticket adds a CI step, `CGO_ENABLED=0 go build ./cmd/ketju`, which holds the "no cgo" half of the criterion for the whole binary.
- `TestThumbnailRefusesBombBeforeDecode`: a PNG whose IHDR says 100000 × 100000, a JPEG whose SOF says 65000 × 65000, and a GIF whose screen is 65535 × 65535, each under 1 KB. Each returns `ErrTooLarge` with zero calls through `decodeHook`, and `runtime.MemStats.TotalAlloc` grows by less than 1 MiB.
- `TestThumbnailBudgetCountsDepth`: a 16-bit PNG over the budget is refused where an 8-bit one of the same size passes.
- `TestThumbnailFitsAndKeepsAspect`: 1200 × 640 becomes 160 × 85, and 100 × 50 is never enlarged.
- `TestThumbnailFormats`: JPEG gives JPEG, PNG and GIF give PNG, and alpha is kept.
- `TestDecodePanicIsContained`: a hook that panics gives `ErrDecode`, and the process carries on.
- `TestThumbnailMadeOnceAndCached`: two requests make one decode. Deleting the file makes it again on the next request.
- `TestThumbnailDecodeFailureLeavesBlobServable`: a PNG that is truncated after a valid header gives 404 on `/thumb` and 200 on `/blobs/{hash}`. The thread page renders the post with its attachment, and a second `/thumb` request makes no decode call.
- `TestThumbnailServedOnlyToReaders`: the same table as `TestBlobServedOnlyToReaders` (TKT-01M29CXK0K), run against `/thumb` through a shared fixture. `TestRevalidationRechecksAccess` also runs against `/thumb`.
- `TestThumbnailNotForNonImages`: a PDF's `/thumb` is 404.
- `TestThumbnailsDecodeOneAtATime`: under `testing/synctest`, three concurrent requests for different hashes never overlap inside `Thumbnail`.
- `TestCollectorRemovesThumbnail`, `TestCollectorRemovesOrphanThumbnail` and `TestRedactRemovesThumbnail`.

### Out of scope

- Uploading, attaching, serving the blob, and `Authorize`: TKT-01M29CXK0K.
- Showing the thumbnail on the post card: TKT-01M3V73J8K (Attach files in the composer and show them on posts).
- WebP, HEIC and more sizes: decided against in docs/03 and docs/07 for Phase 3.
### At implementation (2026-10-01, against main at 4990dea)

The plan above stands, and Settled in grooming is unchanged. What the code changed:
- **The decode estimate is stricter in two cases** that the header reveals and `DecodeConfig` does not report. An Adam7-interlaced PNG counts double, read from the IHDR's interlace byte, because the decoder allocates a full image as well as each pass's image. A four-component (CMYK) JPEG counts 1.5×, because it keeps four coefficient planes and four image planes. Both only refuse more, which keeps the settled estimate the overestimate it is meant to be.
- **The service seam is `Service.Thumbnailer`** (nil is `blob.Thumbnail`), beside `decodeHook` in `internal/store/blob`. The HTTP tests in `internal/web/api` count decodes through it, which `decodeHook` cannot do from another package.
- **`Thumb` returns the opened file**, not a path, so a thumbnail the collector removes between the check and the open is made again rather than answering 404.
- **The orphan-thumbnail sweep runs after the hold check**, beside the orphan-blob sweep, not beside `recoverTrash`, so a held pass deletes nothing, as docs/03 says of it. It reads which thumbnails have rows in one read, then checks again and removes each orphan inside its own `db.Write`.
- **`TestThumbnailMadeOnceAndCached` is in `internal/service/blobs`**, under `testing/synctest` with the decode paced, and `TestThumbnailCachedOverHTTP` covers the same through the route. `TestThumbnailWaiterTakesOverFromCancelledMaker` and `TestThumbnailFailureRemembered` were added.
- **The CI step is `go build -o /dev/null ./cmd/ketju`** with `CGO_ENABLED=0`, placed after `build`.

## Notes

**agent:claude-code/phase3-groom-live** at 2026-10-01T07:59:48Z

run decision: pending maintainer review. Recorded in docs/03 (2026-10-01): one thumbnail, <hash>.thumb beside the blob, fitted inside 160 px. JPEG for JPEG and PNG for PNG and GIF. It is a cache made on first request, and GC and redact remove it. Decoder choice, also in docs/07 'Attachments': stdlib image/png, image/jpeg and image/gif only, each called directly by sniffed type, with a hand-written area-average resize. golang.org/x/image lost: a second decoder surface and a new module for a 160 px thumbnail. Bounds before decode: 16384 px a side and 256 MiB estimated decode memory, so JPEG thumbnails stop near 16.7 MP and a larger photo is a card with no thumbnail.

**agent:claude-code/phase3-cys9w** at 2026-10-01T11:10:27Z

run decision: pending maintainer review. The decode-memory estimate is stricter than the groomed numbers in two cases the header reveals: an Adam7-interlaced PNG counts double (the decoder holds a full image and each pass's image), and a four-component JPEG counts 1.5x (four coefficient and four image planes). Recorded in docs/07 'Attachments', Thumbnails bullet. Effect a member sees: an interlaced RGBA PNG over about 33 MP, or a CMYK JPEG over about 11 MP, shows as a card with no thumbnail; the blob is still served. Alternative that lost: the groomed 4 and 16 bytes a pixel as written, which understate those decoders' peak and so would let one decode reach about 512 MiB against a 256 MiB budget. Mutations tried against the tests, each caught: Thumb authorizing as an admin instead of the viewer, no side bound, no memory budget, 16-bit PNG at 4 bytes, interlace ignored, no recover, no failure memory, no singleflight (caught once concurrent failing readers were added), semaphore of 8, no takeover from a cancelled maker, GC keeping the thumbnail, GC ignoring orphan thumbnails, redact keeping the thumbnail, the thumbnail's ETag set to the blob's, nearest-pixel instead of area averaging, and a thumbnail made for a PDF.

**agent:claude-code/phase3-cys9w** at 2026-10-01T11:44:43Z

Terva review 1748 (head 0338bef5134306f0aebf539fe39150d37e69ac36, base 6d10fe06d4b183e7cae48783855f30f1cc3bf261; run 261d5f27-45a2-4a11-83f8-8920c94ccbbe, Actions run 1924), medium 'Use fractional source coverage when area-averaging thumbnails': accepted and fixed. fit now weights each source pixel by the exact share of its area that falls in each thumbnail pixel, with integer weights in units of 1/dst of a source pixel. Sums stay within uint64: sw x sh is at most 2^28. TestThumbnailAreaAverageAtAFractionalRatio compares every pixel at 403x251, 161x3 and 400x1 with a floating-point area average, to within 1. It fails against the old whole-pixel ranges, at 187 against 171 for the first pixel of 400x1.

**agent:claude-code/phase3-cys9w** at 2026-10-01T12:25:53Z

PR #288 (last head 9fb25ee581b616fec805dd32951e733a7d261b88, CI success in run 1971, clean Terva review run c9dc8f48-7bd0-4334-8784-865851673578 against base 0da0411c) was carried onto main as PR #302 after main moved under it. The only conflict was cmd/ketju/redact_test.go against TKT-01M3V73DP6's TestRedactKeepsDraftAttachedBlob, and both tests are kept. #288 is closed unmerged.

## Summary

GET /blobs/{hash}/thumb serves an image attachment's 160 px thumbnail, kept as <hash>.thumb beside the blob and made on first request. internal/store/blob/thumb.go holds ThumbPath, ThumbType and Thumbnail: header bounds from the type's own DecodeConfig (16384 px a side, 256 MiB estimated decode), then the stdlib png, jpeg or gif decoder called directly inside a recover, an area-average resize, and JPEG or PNG out. internal/service/blobs/thumb.go adds Service.Thumb, which calls Authorize first, so thumbnails follow the blob's access rule (TestThumbnailServedOnlyToReaders runs every row of the blob table). It also adds the one-decode semaphore, a per-hash singleflight, and a 4096-entry in-memory failure LRU, with Service.Thumbnailer as the test seam. Blob GC removes a collected blob's thumbnail after the commit and sweeps thumbnails whose blob has no row (not in a held pass). ketju redact removes it with the blob. docs/03 and docs/07 name the file and route, and CI gains a CGO_ENABLED=0 build of ./cmd/ketju. No migration. All eight criteria are ticked.
