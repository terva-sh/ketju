---
schema: 3
id: TKT-01M2K1JKS7P0T68EFGH9A11VRS
title: Decide whether remote images are proxied, refused, or left alone
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:render
  - effort:S
assignees: []
milestone: null
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-15T17:24:31Z
updated_at: 2026-09-15T17:24:31Z
created_by:
  id: agent:claude-code/render
  name: ""
updated_by:
  id: agent:claude-code/render
  name: ""
extensions: {}
---

## Description

The markdown pipeline permits images with any http or https source, so a post can embed an image from anywhere. Loading it tells that host the IP address, user agent and referer of everyone who opened the thread, and the host chooses what to send back — which can differ per viewer, and can change after a moderator has looked at it.

This is the ordinary forum trade and every forum makes it one way or the other. It is recorded here rather than decided in the renderer, because the renderer is the wrong place to weigh it: the answer depends on whether attachments exist, and they do not yet.

Three options, none of them free:

- **Leave it.** Simple, expected, and the privacy leak is real but no worse than any other forum a friend group has used.
- **Proxy remote images** through the server, cached in the blob store. Viewers leak nothing, the server leaks its own address, bandwidth and cache size become the server's problem, and a proxy that fetches arbitrary URLs is an SSRF surface that has to be closed deliberately.
- **Refuse remote images**, allowing only attachments. Strongest, and it breaks pasting an image URL, which is how most images get into a post.

`TKT-01M29CXK0` builds the content-addressed blob store and the upload path, which is the point at which the second and third options become possible at all. Decide it there.

If the answer is proxy or refuse, `internal/render` bumps its version constant, because the rendered HTML changes for every post containing a remote image.

## Acceptance criteria

- [ ] A decision is recorded with the reason the other two lost
- [ ] If images stay remote, the trade is documented where an admin will see it
- [ ] If they do not, the renderer version is bumped in the same change
