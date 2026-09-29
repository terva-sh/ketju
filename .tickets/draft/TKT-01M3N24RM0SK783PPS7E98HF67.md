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
updated_at: 2026-09-29T05:09:50Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

The CSP's `img-src 'self' data:` (docs/07, `internal/web/security.go`) already stops the browser loading a remote image, so a markdown image with an http or https source renders as a broken image. Decided 2026-09-28 in TKT-01M2K1JKS7 (Decide whether remote images are proxied, refused, or left alone): keep the CSP, and have the renderer turn such an image into a plain link to its URL, with the alt text as the link text, or the URL when there is no alt text.

Attachment images, which are same-origin, and `data:` images the sanitizer already allows are unchanged. The rendered output changes, so the renderer version (docs/03 "Renderer version") must be bumped, and cached renders are invalidated.

## Acceptance criteria

- [ ] A markdown image whose source is http, https (in any case) or protocol-relative, including /\host, renders as <a href> to that URL with the alt text, or the URL when there is none, as its text, and never as an img element (TestRemoteImageIsNeverAnImg, images-remote golden)
- [ ] An image whose source is exactly /blobs/<64 lowercase hex> still renders as an img, and near misses render as links (TestAttachmentImageStaysAnImg, images-attachment golden)
- [ ] An image inside a link renders as text inside that link, and a data: or javascript: image renders as its alt text, with no broken img (images-nested and images-unsafe goldens)
- [ ] The sanitizer keeps an img src only when it matches the attachment pattern (TestPolicyDropsNonAttachmentImgSrc)
- [ ] render.Version is 5, pipeline.sum is regenerated at version 5, and docs/07 records the rules beside the 2026-09-28 decision

## Implementation plan

Groomed 2026-09-29 against main at 7af2c5a (TKT-01M3M9RR26, Phase 2 grooming).

### Settled in grooming
- **Remote images are rendered as links.** Decided 2026-09-28 in TKT-01M2K1JKS7, recorded in docs/07 (`:567-575`): "`img-src 'self' data:` ... stays, and the renderer turns a markdown image with a remote source into a plain link to its URL, so nothing renders broken and merely viewing the post sends nothing to the image's host." The link text is the alt text, or the URL when there is no alt text (this ticket's description). The CSP in `internal/web/security.go:114` does not change. A proxy and relaxing `img-src` both lost (docs/07).
- Attachments are how a post shows an image, and attachment images stay images.
- Any change that can change `Render`'s output bumps `render.Version` (docs/03 "Caching"). It is 4 today (`internal/render/render.go:59`), so this ticket makes it 5. That invalidates the render cache, the response cache key (`service/threads/cached.go:47`) and every thread ETag (`view.go:321`). Terva review 1280 on PR #134 moved the bump criterion here.

Settled here as engineering choices, to be written into docs/07 beside the decision:
- **What counts as an attachment image.** A destination that is a same-origin path of the form `/blobs/<64 lowercase hex>`, the attachment URL docs/07 gives (`GET /blobs/{hash}`), matched exactly. Every other destination is not an attachment. Deciding by "has no scheme" lost, because `//host/x.png` and `/\host/x.png` have no scheme and a browser loads both from another host.
- **What a non-attachment image becomes.** When the sanitizer's link rules would keep its URL (http, https, mailto, or relative), it becomes `<a href="URL">alt or URL</a>`, keeping the markdown title as the link's `title`. Otherwise (`data:`, `javascript:` and anything the policy drops) it becomes its alt text as plain text. The description said `data:` images are allowed and unchanged, but `policy.go` never calls `AllowDataURIImages`. So a `data:` image today is an `<img>` with its `src` stripped, which is the broken image the decision exists to remove. Plain text is what "nothing renders broken" asks for.
- **An image inside a link** (`[![alt](src)](href)`) becomes its alt text, or its URL when there is no alt text, as text inside the outer link. HTML cannot nest links, and a parser splits a nested `<a>` apart. Rendering the image's link after the outer one lost because it puts a second link where the author wrote one.
- **The sanitizer is tightened as a second layer.** `img` keeps `src` only when it matches the attachment pattern (`p.AllowAttrs("src").Matching(attachmentSrc).OnElements("img")` in place of `AllowImages()`'s any-URL `src`). A remote image that reached the policy by any path other than the renderer, such as a future extension, then loses its `src` and never loads.

### Approach
1. `internal/render/image.go` (new): an `imageRenderer` registered for `ast.KindImage` through `html.WithNodeRenderers`, in the way `NewQuoteRenderer` and `NewMentionRenderer` are (`quote.go:288-296`, `mention.go:232-242`). Add it to the renderer list in `render.go:206-220`. It:
   - renders an attachment destination (`attachmentSrc`, a `regexp` of `^/blobs/[0-9a-f]{64}$`) exactly as goldmark's `renderImage` does today;
   - renders a non-attachment image as a link or as text, as settled above. Whether a URL is linkable is decided by the same rules the policy applies to `a[href]`, in one function `linkable(dest string) bool`, so the renderer cannot write a link the sanitizer would strip. The alt text is `n.Text(source)`, escaped;
   - inside an `*ast.Link` ancestor, writes text only.
2. `internal/render/policy.go`: replace `p.AllowImages()` with `p.AllowElements("img")`, `p.AllowAttrs("alt", "height", "width").OnElements("img")` and `p.AllowAttrs("src").Matching(attachmentSrc).OnElements("img")`. Also rewrite the stale comment at `:61-65`, which says external sources are permitted.
3. `render.go`: `Version = 5`, and a line in the version history comment if there is one.
4. Goldens under `internal/render/testdata/golden/`:
   - `images.md` changes (all four of its images are remote), so run `go test ./internal/render -update` *after* the bump, as `TestRendererVersionTracksOutput` requires.
   - New cases:
     - `images-remote.md`: http, https, uppercase `HTTPS:`, `//host`, `/\host`, with alt, without alt, with a title;
     - `images-attachment.md`: `/blobs/<hash>`, and near misses such as an uppercase hash, a short hash, `/blobs/<hash>/x` and `https://site/blobs/<hash>`, each of which must become a link;
     - `images-nested.md`;
     - `images-unsafe.md`: `data:image/png`, `javascript:`.
5. docs/07: under the decision, add the settled rules above: the attachment pattern, link or text, the nested case, and the policy layer.
6. No client code changes. The composer preview (`POST /api/v1/render`) uses the same renderer, so the dist does not change.

### Tests
- The golden cases above, with `pipeline.sum` at version 5.
- `TestRemoteImageIsNeverAnImg` (`render_test.go`): over a table of remote sources, the output has no `<img` and has an `<a href="...">`. This is criterion 1.
- `TestAttachmentImageStaysAnImg`: `/blobs/<hash>` renders `<img src="/blobs/<hash>" alt="...">`. This is criterion 2.
- `TestPolicyDropsNonAttachmentImgSrc` (`policy_test.go`): raw `<img src="https://x">` through `Policy().Sanitize` keeps no `src`, and `/blobs/<hash>` keeps it.
- `TestLinkableMatchesThePolicy`: for each URL in a table, `linkable` agrees with whether the policy keeps `a[href]`.
- The existing `policy_test.go` cases (`:93`, `:98`, `:101`) still pass `assertSafe`.

### Out of scope
- The attachment upload and attach paths, and the URL an attachment is written with in a post. That is TKT-01M29CXK0 (phase-3). If it chooses a URL form other than `/blobs/<hash>`, it changes `attachmentSrc` and bumps `Version`.
- A server-side image proxy (docs/07: it can be revisited later).
- Any CSP change.
