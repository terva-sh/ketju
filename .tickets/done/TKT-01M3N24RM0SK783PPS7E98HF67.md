---
schema: 3
id: TKT-01M3N24RM0SK783PPS7E98HF67
title: Render remote markdown images as links rather than broken images
type: task
status: done
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
updated_at: 2026-09-30T02:37:02Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-n24rm0
  name: ""
extensions: {}
---

## Description

The CSP's `img-src 'self' data:` (docs/07, `internal/web/security.go`) already stops the browser loading a remote image, so a markdown image with an http or https source renders as a broken image. Decided 2026-09-28 in TKT-01M2K1JKS7 (Decide whether remote images are proxied, refused, or left alone): keep the CSP, and have the renderer turn such an image into a plain link to its URL, with the alt text as the link text, or the URL when there is no alt text.

Attachment images, which are same-origin, and `data:` images the sanitizer already allows are unchanged. The rendered output changes, so the renderer version (docs/03 "Renderer version") must be bumped, and cached renders are invalidated.

## Acceptance criteria

- [x] A markdown image whose source is http, https (in any case) or protocol-relative, including /\host, renders as <a href> to that URL with the alt text, or the URL when there is none, as its text, and never as an img element (TestRemoteImageIsNeverAnImg, images-remote golden)
- [x] An image whose source is exactly /blobs/<64 lowercase hex> still renders as an img, and near misses render as links (TestAttachmentImageStaysAnImg, images-attachment golden)
- [x] An image inside a link renders as text inside that link, and a data: or javascript: image renders as its alt text, with no broken img (images-nested and images-unsafe goldens)
- [x] The sanitizer keeps an img src only when it matches the attachment pattern (TestPolicyDropsNonAttachmentImgSrc)
- [x] render.Version is 5, pipeline.sum is regenerated at version 5, and docs/07 records the rules beside the 2026-09-28 decision

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming). Revised at claim on 2026-09-29 against main at c158b5f.

### Settled in grooming
- **Remote images are rendered as links.** Decided 2026-09-28 in TKT-01M2K1JKS7, recorded in docs/07 (`:567-575`): "`img-src 'self' data:` ... stays, and the renderer turns a markdown image with a remote source into a plain link to its URL, so nothing renders broken and merely viewing the post sends nothing to the image's host." The link text is the alt text, or the URL when there is no alt text (this ticket's description). The CSP in `internal/web/security.go:114` does not change. A proxy and relaxing `img-src` both lost (docs/07).
- Attachments are how a post shows an image, and attachment images stay images.
- Any change that can change `Render`'s output bumps `render.Version` (docs/03 "Caching"). It is 4 today (`internal/render/render.go:59`), so this ticket makes it 5. That invalidates the render cache, the response cache key (`service/threads/cached.go:47`) and every thread ETag (`view.go:321`). Terva review 1280 on PR #134 moved the bump criterion here.

Settled here as engineering choices, to be written into docs/07 beside the decision:
- **What counts as an attachment image.** A destination that is a same-origin path of the form `/blobs/<64 lowercase hex>`, the attachment URL docs/07 gives (`GET /blobs/{hash}`), matched exactly. Every other destination is not an attachment. Deciding by "has no scheme" lost, because `//host/x.png` and `/\host/x.png` have no scheme and a browser loads both from another host.
- **What a non-attachment image becomes.** When the sanitizer's link rules would keep its URL (http, https, mailto, or relative), it becomes `<a href="URL">alt or URL</a>`, keeping the markdown title as the link's `title`. Otherwise (`data:`, `javascript:` and anything the policy drops) it becomes its alt text as plain text. The description said `data:` images are allowed and unchanged, but `policy.go` never calls `AllowDataURIImages`. So a `data:` image today is an `<img>` with its `src` stripped, which is the broken image the decision exists to remove. Plain text is what "nothing renders broken" asks for.
- **An image inside a link** (`[![alt](src)](href)`) becomes its alt text, or its URL when there is no alt text, as text inside the outer link. HTML cannot nest links, and a parser splits a nested `<a>` apart. Rendering the image's link after the outer one lost because it puts a second link where the author wrote one.
- **The sanitizer is tightened as a second layer.** `img` keeps `src` only when it matches the attachment pattern (`p.AllowAttrs("src").Matching(attachmentSrc).OnElements("img")` in place of `AllowImages()`'s any-URL `src`). A remote image that reached the policy by any path other than the renderer, such as a future extension, then loses its `src` and never loads.


### Approach (revised at claim)
Read at c158b5f: goldmark is v2.1.1 (`renderImage` writes `<img src alt title>` through `ContextLinkURLWriter` and `ContextTextWriter`), bluemonday v1.0.27, and `render.Version` is 4.

1. `internal/render/image.go` (new): `NewImageRenderer()`, an `html.Extension` that registers a node renderer for `ast.KindImage` through `html.WithNodeRenderers`, like `NewQuoteRenderer` and `NewMentionRenderer`, added to `mdRenderer` in `render.go`. It decides in this order:
   - the destination matches `attachmentSrc` (`^/blobs/[0-9a-f]{64}$`, matched against the decoded destination): write the `<img>` exactly as goldmark's `renderImage` does, including inside a link, because an `<img>` inside an `<a>` nests no link;
   - otherwise, when `linkable(dest)` is false (`data:`, `javascript:`, anything goldmark calls dangerous or the policy would drop from `a[href]`): write the alt text as plain text, and nothing when there is none;
   - otherwise, inside an `*ast.Link` ancestor: write the alt text, or the URL when there is no alt text, as plain text;
   - otherwise: `<a href="URL" title="...">alt or URL</a>`, the URL escaped by goldmark's link URL writer, as `renderLink` does.
   The alt text is the image's inline text content (text, code spans, autolink labels and `@handle` as typed), written through goldmark's text writer so it is escaped the way goldmark escapes alt. "No alt text" means nothing but whitespace.
   `linkable` asks the policy itself: it sanitizes `<a href="ESCAPED">x</a>`, built from the same escaped bytes the renderer would write, and reports whether `href` survived. A second copy of the scheme rules would drift from bluemonday; asking the policy cannot.
2. `internal/render/policy.go`: replace `p.AllowImages()` with the rules it set minus the any-URL `src`: `img` with `align` (ImageAlign), `alt` (Paragraph), `height`/`width` (NumberOrPercent), and `src` only matching `attachmentSrc`. Keeping the other three rules unchanged keeps this ticket's change to the one decided. Rewrite the stale comment that says external sources are permitted.
3. `render.go`: `Version = 5`, and a line saying why.
4. Goldens: bump first, then `go test ./internal/render -update`. `images.md` changes. New cases `images-remote`, `images-attachment`, `images-nested`, `images-unsafe`, as the grooming plan lists.
5. docs/07: under the 2026-09-28 decision, the rules above.
6. No client change; `go generate ./internal/web/spa` must leave `internal/web/spa/dist` byte-identical.

### How existing rendered HTML picks up the change
Nothing is re-rendered or migrated, because rendered HTML is never persisted: the thread logs and the database hold sources only (`render/cache.go`). Bumping `render.Version` to 5 retires every cached rendering at once:
- the in-process render cache keys on `Key.Version` (`render/cache.go`);
- the response cache keys on `Renderer: render.Version` (`service/threads/cached.go:50`);
- every thread ETag carries `r<Version>` (`service/threads/view.go:419`), and thread responses are `private, no-cache`, so a browser holding version-4 HTML revalidates, misses the ETag, and gets version-5 HTML.

Profile bios (`threads/userposts.go`) and form help text (`forms/forms.go`) render on every read, so they change on deploy. No cache key or storage change is needed.

### Tests
- The golden cases above, with `pipeline.sum` at version 5.
- `TestRemoteImageIsNeverAnImg` (`image_test.go`): http, https, `HTTPS:`, `//host`, `/\host`, relative, with and without alt; no `<img`, and an `<a href>` to the URL.
- `TestAttachmentImageStaysAnImg`: `/blobs/<hash>` renders `<img src="/blobs/<hash>" alt="...">`; near misses (uppercase, short, suffix, absolute URL, query) render as links.
- `TestNestedAndUnsafeImages`: an image in a link is text in that link with one `<a>`; `data:` and `javascript:` images are alt text and no `<img>`.
- `TestPolicyDropsNonAttachmentImgSrc` (`policy_test.go`): raw `<img src="https://x">` keeps no `src`; `/blobs/<hash>` keeps it.
- `TestLinkableMatchesThePolicy`: for a table of URLs, `linkable` agrees with whether a markdown link `[x](url)` through the full pipeline keeps its `href`.
- Existing `policy_test.go` safety cases still pass `assertSafe`.

### Out of scope
- The attachment upload and attach paths, and the URL an attachment is written with in a post. That is TKT-01M29CXK0 (phase-3). If it chooses a URL form other than `/blobs/<hash>`, it changes `attachmentSrc` and bumps `Version`.
- A server-side image proxy (docs/07: it can be revisited later).
- Any CSP change.

## Notes

**agent:claude-code/phase2-run-n24rm0** at 2026-09-30T02:36:53Z

Built in ed200c4 on run/n24rm0-remote-images-as-links. Choices made while building, beyond the plan:

- `linkable` asks the policy (`sanitize('<a href="...">x</a>')`) on the exact escaped bytes the renderer writes, instead of restating the scheme allowlist. A second copy would drift from bluemonday. TestLinkableMatchesThePolicy compares it with a markdown link to the same URL through the whole pipeline, over 20 URLs including unparseable ones.
- The policy keeps AllowImages' other rules (`align` ImageAlign, `alt` Paragraph, `height`/`width` NumberOrPercent) and replaces only `src`. The grooming approach listed `alt`, `height` and `width` with no pattern; that would have loosened the `alt` and size rules, which this ticket did not decide.
- An attachment image inside a link stays an img (an img in an a nests no link). The nested rule applies to images that would otherwise become links, which is the reason the settled item gives.
- An unlinkable image with no alt text writes nothing. In a paragraph of its own that leaves `<p></p>`, which is harmless and shows nothing broken.
- Link text is the image's inline text: text, code spans, autolink labels and `@handle` as typed. Raw HTML inside the alt is dropped, as goldmark drops it from alt.
- `HTTPS://` comes out as `https://` because bluemonday normalises the scheme, and `/\host` as `/%5Chost` because goldmark URL-escapes the backslash, the same as for a markdown link to either URL. The result is a same-origin path.

`go generate ./internal/web/spa` leaves `internal/web/spa/dist` byte-identical (thread-view bundle 45708 B gzip).

## Summary

A markdown image now renders as an img only when its source is exactly /blobs/<64 lowercase hex>. Any other image becomes a link to its URL with the alt text (or the URL) as text. Inside a link it becomes that text, and with a source no link may carry it becomes its alt text. The sanitizer keeps img src only for the attachment pattern. render.Version is 5, which retires the render cache, the response cache and every thread ETag; no rendered HTML is persisted, so nothing else needed changing. Rules recorded in docs/07 beside the 2026-09-28 decision. No client change; the dist is unchanged.
