---
schema: 3
id: TKT-01M29C0WF04HEBYMB9DXCB9WCB
title: Implement the rendered-post cache
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
dependencies:
  - TKT-01M29BW9Q8C8NW2MJH9FKVZA5D
blocks_on: none
references:
  - ref: design:storage-caching
    path: docs/03-thread-storage.md
claim: null
archive: null
created_at: 2026-09-11T23:14:40Z
updated_at: 2026-09-16T18:55:33Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase1
  name: ""
extensions: {}
---

## Description

An LRU bounded by bytes, default 256 MB, keyed on thread, ordinal, revision, and renderer version. HTML lives here and nowhere else: not in the log, not in the database.

The revision in the key is why an edit invalidates exactly one entry, and the renderer version is why a pipeline upgrade invalidates everything at once without a rewrite. Bounding by bytes rather than entry count matters because post sizes vary by orders of magnitude and a count-based bound would either waste memory or blow past it.

## Acceptance criteria

- [x] The cache is bounded by total bytes and stays within the configured limit under a mixed workload of large and small posts
- [x] Editing a post invalidates only that post's entry
- [x] Bumping the renderer version makes every prior entry unreachable
- [x] Hit ratio is exposed as a metric

## Notes

**agent:claude-code/phase1** at 2026-09-16T18:55:33Z

### What criterion 4 means here, precisely

Stats() exposes Hits, Misses and HitRatio(), plus the counters that separate the
reasons a lookup missed. This package does not publish them over HTTP: the
`/metrics` endpoint is TKT-01M29CEE7S, and docs/02 already lists cache hit
ratios among what it serves. The criterion is ticked because the number exists
and is named; if the reading is that it must be scrapeable, that half is the
other ticket's and it has everything it needs.

The counters are deliberately more than one. A miss because the bound is below
the working set and a miss because a post is larger than the entire cache call
for opposite responses -- raise the bound, or stop trying to cache that post --
and a single misses counter cannot distinguish them.

### The version in the key is for a cache that does not exist yet

The cache is in memory, so a renderer version bump implies a new binary, which
implies an empty cache. Criterion 3 is therefore free today. It is held down by
a test anyway because the stated reason this cache is not persisted (docs/04) is
exactly the renderer-version coupling, and "not persisted" is a decision someone
will revisit. Getting the key wrong then means serving HTML from the old
pipeline under an ETag claiming the new one, which is indistinguishable from
correct until a reader complains about a post that will not update.

### For the projection writer

InvalidatePost and InvalidateThread are what an edit and a thread rename need.
Both are safe on entries that were never cached, which matters because the
writer will invalidate on every record it sees, including posts nobody has read.

### Verified by mutation

Three invariants were checked by breaking them: dropping the per-entry overhead
makes the small-post flood test fail; making InvalidatePost a no-op fails the
edit tests; removing the renderer version from the key fails the version test.

## Summary

render.Cache: byte-bounded LRU keyed on thread, ordinal, revision and renderer version, default 256 MB, zero disables. Per-entry overhead is charged so a flood of tiny posts cannot escape the bound; a post larger than the bound is refused rather than evicting everything. InvalidatePost and InvalidateThread reclaim superseded revisions. Stats separates hits, misses, evictions, rejections and drops; TKT-01M29CEE7S publishes them.
