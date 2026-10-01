---
schema: 3
id: TKT-01M3V73J8KH0F1Q0H7D615HRA3
title: Attach files in the composer and show them on posts
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area:client
  - area:render
  - effort:M
assignees: []
milestone: phase-3
parent: TKT-01M29BKBM7004ZPAA9T81HC939
origin: null
dependencies:
  - TKT-01M29CXK0KHC2QZQ286AG0KKGD
  - TKT-01M29CYS9W8GAWNQVHFH7HZ7AR
  - TKT-01M3V73DP6TZ1BDQVT7ATX0EC5
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T07:50:47Z
updated_at: 2026-10-01T07:59:27Z
created_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
updated_by:
  id: agent:claude-code/phase3-groom-live
  name: ""
extensions: {}
---

## Description

The server can store, attach and serve blobs (TKT-01M29CXK0K), make thumbnails (TKT-01M29CYS9W) and keep attachments in drafts. No client ticket lets a member attach a file or see one, and docs/09 Phase 3 lists "attachments with thumbnails" as a deliverable.

This ticket covers three things:
- an Attach control in the composer, for replies and new threads, shown where `can_upload` says the viewer may upload;
- the composer's list of attached files, saved with the draft;
- the attachment card under each post, drawn as the mockups draw it (`docs/mockups/generate.py`, `attachment`): thumbnail, file name, size and, for an image, its dimensions.

An edit cannot change a post's attachments, because the log's `Edit` record has no attachments field (docs/03).

Filed by the Phase 3 grooming of live updates and attachments (TKT-01M3V3BVPY).

## Acceptance criteria

- [ ] The composer offers Attach only when can_upload is true and never while editing, uploads each chosen file, shows it as a removable chip, and sends its hash with the reply or new thread (Composer.test.tsx, NewThreadScreen.test.tsx)
- [ ] A refused upload says why on its chip for type_not_allowed, blocked, 413, forbidden and 429, and ten attachments disable Attach (Composer.test.tsx)
- [ ] Attachments survive a draft round trip, for replies and new threads (drafts.test.ts)
- [ ] Each attachment renders as the mockups' card: thumbnail or icon, name, size and an image's dimensions on desktop; a file card downloads under its name; a tombstone shows none (Attachments.test.tsx)
- [ ] e2e attachments.spec.ts passes at 390 and 1440 px with one axe scan per test and no violations, including a stranger's 404 for a members-only blob
- [ ] The dist committed from go generate ./internal/web/spa keeps the reader within the 70000 B gzip budget, and the PR records the change from 60465 B

## Implementation plan

Groomed 2026-10-01 for Phase 3 (TKT-01M3V3BVPY), against main at a599b48, together with the server tickets it needs. Read for this plan:
- docs/06 "Post anatomy" (the `📎 attachments` row) and "Composer";
- docs/07 "Attachments" and "Drafts", with the run decisions of 2026-10-01;
- the mockups' attachment card in `docs/mockups/generate.py`, `attachment`: desktop 320 px wide with a 48 × 36 thumbnail, name, and "240 KB · 1200×640"; the phone version shows the size only;
- in `web/client`: `src/composer/` (`Composer.tsx`, `ComposerBody.tsx`, `ComposerSheet.tsx`), `src/state/composer.ts`, `src/state/drafts.ts`, `src/state/newThread.ts`, `src/screens/NewThreadScreen.tsx`, `src/components/thread/Post.tsx`, `src/api/client.ts`, `src/api/dto.ts` and `dto-contract.ts`, `scripts/bundle.mjs`;
- `cmd/ketju/e2e.go`, the e2e seed.

### Settled in grooming

- **The server contract.**
  - TKT-01M29CXK0K (Build the content-addressed blob store and upload path):
    - `POST /api/v1/blobs?thread=` or `?forum=`, multipart `file`, answering 201 `Attachment {hash, size, mime, original_name, width, height}`;
    - the refusals `type_not_allowed`, `blocked`, 413, 401, 403 and 429;
    - `attachments` on reply and on new thread, at most 10;
    - `viewer.can_upload` on the thread page and `can_upload` on each forum in the forum tree;
    - `GET /blobs/{hash}`, `inline` for images and `attachment` otherwise.
  - TKT-01M29CYS9W (Generate image thumbnails for attachments): `GET /blobs/{hash}/thumb`, 404 when there is none.
  - TKT-01M3V73DP6 (Keep attachments in reply and new-thread drafts): `attachments` on both draft routes.
- **No preview of an unposted file from the server.** An uploader cannot fetch a blob no post names (docs/07 "Attachments", run decision). So the composer shows its attachments by name and size, with no thumbnail. A local preview through `blob:` URLs is not possible either, because the CSP's `img-src 'self' data:` refuses them.
- **An edit cannot change attachments,** since `Edit` has no attachments field (docs/03). The composer in edit mode offers no Attach control.
- **The card** follows the mockups. For an image, the thumbnail is `/blobs/{hash}/thumb`, falling back to a file icon if it fails to load, and the card links to `/blobs/{hash}`. For any other file, the card links to `/blobs/{hash}` with `download={original_name}`, because the server sends no file name in `Content-Disposition`. A tombstone shows no card, and the DTO gives it none.
- **Bundle.**
  - This is reader code: the composer and the post are on the thread page.
  - `scripts/bundle.mjs` counts every dynamic import outside `src/screens/admin/`, so a lazy chunk would save nothing.
  - The estimate is about 2.0 KB gzip: `apiUpload` and the Attach control with its chips about 1.2 KB, and the card with its CSS about 0.8 KB.
  - The reader is 60465 B of 70000 B on main, before the other Phase 3 client work. If the sum would pass the budget, that is a docs/14 class 5 stop.
- **Conventions.**
  - e2e tests use stored sessions and one axe scan per test. These specs make no `/auth/*` call, so `e2e/session.ts` needs no `paceAuth` row.
  - There is no new lazy chunk.
  - The built client is committed: run `go generate ./internal/web/spa`.

### Approach

1. **Types.** `src/api/dto.ts` and `dto-contract.ts` gain:
   - `Attachment` with `width` and `height` as `number | null`;
   - `ThreadViewer.can_upload` and `Forum.can_upload`;
   - `attachments` on `Draft` and `ForumDraft`.

   The contract test follows the Go goldens.
2. **`src/api/client.ts`.** `apiUpload<T>(url, file: File, init)` sends `FormData` with one `file` field. It shares `apiSend`'s problem handling and status check, and sets no `Content-Type`, so the browser writes the multipart boundary.
3. **Composer state** (`state/composer.ts`, `state/newThread.ts`, `state/drafts.ts`):
   - `attachments: Signal<Attachment[]>` and `uploading: Signal<{name, error?}[]>`;
   - the draft save sends `attachments` as hashes, and a restored draft fills them;
   - `postReply` and the new-thread submit send them, and clear them on success;
   - at 10, Attach is disabled.
4. **`src/composer/AttachButton.tsx`** (new):
   - It is shown in `ComposerBody`'s toolbar when `canUpload` is true and the composer is not editing.
   - It holds a hidden `<input type="file" multiple accept="image/png,image/jpeg,image/gif,application/pdf,text/plain">` and uploads each chosen file in turn with `?thread=<id>`, or `?forum=<slug>` from the new-thread screen.
   - The chips show the name, `formatSize`, a spinner while uploading, and a 44 px remove button labelled "Remove <name>".
   - A refusal stays on its chip, written as:
     - `type_not_allowed`: "Only PNG, JPEG and GIF images, PDFs and plain text can be attached.";
     - `blocked`: "This file can't be attached.";
     - 413: "This file is larger than this forum accepts.";
     - `forbidden`: "You can't attach files here.";
     - 429: the `Retry-After` wording `postFailure` already uses.
   - `ThreadBody` passes `page.viewer.can_upload`, and `NewThreadScreen` passes the chosen forum's `can_upload` from the tree it already loads.
5. **`src/components/thread/Attachments.tsx`** (new). It renders under the post body in `Post.tsx` when `post.attachments.length > 0`, as a `<ul>` of cards:
   - the thumbnail is `<img alt="" width=48 height=36 loading="lazy">` with `object-fit: cover`;
   - the name is ellipsised;
   - the second line is `formatSize(size)`, plus ` · W×H` above 720 px.

   Each card is a single link whose accessible name is the file name and size.
6. **CSS** in `components/thread/thread.css` and `composer/composer.css`: a 320 px card on desktop and full width on a phone, with 44 px targets.
7. **e2e seed.** `cmd/ketju/e2e.go` grants `upload` to the members group globally, adds a thread in the members-only `garden` forum, and adds `web/client/e2e/fixtures/pixel.png` (a small PNG) and `notes.txt`. Any e2e spec or Go test that counts the seed's grants, such as the admin permissions screen's, is updated in the same PR.
8. **Build.** Run `go generate ./internal/web/spa`, commit the dist, and record the reader size against 60465 B in the PR.

### Tests

- `src/components/thread/Attachments.test.tsx`:
  - 'an image card shows the thumbnail, name, size and dimensions';
  - 'a file card downloads under its name';
  - 'a thumbnail that fails to load leaves the card with an icon';
  - 'a tombstone shows no attachments'.
- `src/composer/Composer.test.tsx`:
  - 'Attach is offered only with can_upload and never while editing';
  - 'an upload adds a chip and the reply sends its hash';
  - 'a refused upload says why on its chip';
  - 'ten attachments disable Attach';
  - 'removing a chip drops the hash from the reply and the draft'.
- `src/state/drafts.test.ts`: 'attachments survive a draft round trip'.
- `src/screens/NewThreadScreen.test.tsx`: 'a new thread sends its attachments with ?forum='.
- `src/api/client.test.ts`: 'apiUpload sends one file part and maps problems'.
- `e2e/attachments.spec.ts`, in both projects (390 and 1440 px), from stored sessions:
  - 'a member attaches an image and a file to a reply': alice uses `setInputFiles` with the PNG and the text file and posts. Both cards appear. The thumbnail's `naturalWidth` is above 0, and the file card's link has `download="notes.txt"`. This test runs the one axe scan.
  - 'another member sees the cards': bob, from his stored session, opens the thread and sees both cards.
  - 'a stranger cannot fetch the blob': alice attaches the PNG to a reply in the `garden` thread, and an anonymous `request.get('/blobs/<hash>')` for it is 404.

### Out of scope

- Every server change: TKT-01M29CXK0K, TKT-01M29CYS9W and TKT-01M3V73DP6.
- Adding or removing attachments when editing a post: impossible without a new `Edit` field, which docs/14 class 2 reserves for the maintainer.
- Drag and drop, and pasting images into the composer. Neither is in docs/06, and both can come later with no ticket now.
- Upload progress bars: `fetch` has no upload progress, and a spinner per chip is enough for a 10 MB cap.
