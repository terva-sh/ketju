---
schema: 3
id: TKT-01M2K1JKS7P0T68EFGH9A11VRS
title: Decide whether remote images are proxied, refused, or left alone
type: spike
status: done
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
updated_at: 2026-09-28T22:39:08Z
created_by:
  id: agent:claude-code/render
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
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

- [x] A decision is recorded with the reason the other two lost
- [ ] If images stay remote, the trade is documented where an admin will see it
- [ ] If they do not, the renderer version is bumped in the same change

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:29:11Z

Maintainer decision, 2026-09-28: refuse remote images cleanly. Grooming found the CSP (img-src 'self' data:, internal/web/security.go) already stops the browser loading remote images, so today they render broken. The maintainer first chose 'leave them' without knowing that, then chose, once told, to keep the CSP and have the renderer emit a link. Relaxing img-src lost because it loosens the policy everywhere and leaks readers' addresses. A proxy lost for now because it is an SSRF surface. Written into docs/07 'Security headers'. TKT-01M3N24RM0 (Render remote markdown images as links rather than broken images) builds it.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T22:39:08Z

Corrects the closing: criteria 2 and 3 were ticked in error and are now unticked. Criterion 2 does not apply, because remote images do not stay. Criterion 3, the renderer version bump, is not done: no renderer code changes in the grooming PR, and it moves to TKT-01M3N24RM0 (Render remote markdown images as links rather than broken images), whose third criterion carries it. From Terva review 1280 on PR #134.

## Summary

Decided: remote images stay refused by the CSP, and the renderer turns them into plain links. Attachments are how a post shows an image. The decision is in docs/07; TKT-01M3N24RM0 builds it.
