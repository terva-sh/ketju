import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Attachment, Post, ThreadPage } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { isPhone } from '../../state/layout';
import { renderAt } from '../../testing';
import { Attachments } from './Attachments';

// The attachment card under a post (TKT-01M3V73J8K; docs/06 "Post anatomy",
// the mockups' attachment).

const image: Attachment = {
  hash: 'a'.repeat(64),
  size: 245_760,
  mime: 'image/png',
  original_name: 'naming-proposal.png',
  width: 1200,
  height: 640,
};
const text: Attachment = { hash: 'b'.repeat(64), size: 900, mime: 'text/plain', original_name: 'notes.txt', width: null, height: null };

let host: HTMLElement | null = null;
async function show(attachments: Attachment[]) {
  host ??= document.body.appendChild(document.createElement('div'));
  await act(() => render(<Attachments post={{ attachments }} />, host!));
  return host;
}
afterEach(async () => {
  if (host) await act(() => render(null, host!));
  host?.remove();
  host = null;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

describe('an attachment card', () => {
  it('an image card shows the thumbnail, name, size and dimensions', async () => {
    const root = await show([image]);
    const card = root.querySelector<HTMLAnchorElement>('.attachments a.attachment')!;
    expect(card.getAttribute('href')).toBe(`/blobs/${image.hash}`);
    // An image opens inline; only other files download.
    expect(card.hasAttribute('download')).toBe(false);
    const img = card.querySelector('img')!;
    expect(img.getAttribute('src')).toBe(`/blobs/${image.hash}/thumb`);
    expect(img.getAttribute('alt')).toBe('');
    expect([img.getAttribute('width'), img.getAttribute('height'), img.getAttribute('loading')]).toEqual(['48', '36', 'lazy']);
    // The link's text, and so its name, is the file's name and size.
    expect(card.textContent).toBe('naming-proposal.png 240.0 KB · 1200×640');
    // A phone shows the size only, as the phone mockup does.
    await act(() => void (isPhone.value = true));
    expect(card.textContent).toBe('naming-proposal.png 240.0 KB');
  });

  it('a file card downloads under its name', async () => {
    const root = await show([text]);
    const card = root.querySelector<HTMLAnchorElement>('a.attachment')!;
    expect(card.getAttribute('href')).toBe(`/blobs/${text.hash}`);
    expect(card.getAttribute('download')).toBe('notes.txt');
    expect(card.querySelector('img')).toBeNull();
    expect(card.querySelector('.attachment-icon svg')).not.toBeNull();
    expect(card.textContent).toBe('notes.txt 900 B');
  });

  it('a thumbnail that fails to load leaves the card with an icon', async () => {
    const root = await show([image]);
    await act(() => void root.querySelector('img')!.dispatchEvent(new Event('error')));
    expect(root.querySelector('img')).toBeNull();
    expect(root.querySelector('.attachment-icon svg')).not.toBeNull();
    expect(root.querySelector('a.attachment')!.textContent).toBe('naming-proposal.png 240.0 KB · 1200×640');
  });

  it('lists every file in order, and nothing for a post without any', async () => {
    const root = await show([image, text]);
    expect([...root.querySelectorAll('.attachment-name')].map((n) => n.textContent)).toEqual(['naming-proposal.png', 'notes.txt']);
    await show([]);
    expect(root.querySelector('.attachments')).toBeNull();
  });
});

describe('on the thread page', () => {
  it('a tombstone shows no attachments', async () => {
    const base = golden as ThreadPage;
    const post = (ordinal: number, over: Partial<Post>): Post => ({
      ...base.posts[0],
      ordinal,
      reply_to: null,
      reply_to_suppressed: false,
      reply_count: 0,
      ...over,
    });
    const page: ThreadPage = {
      ...base,
      lens: null,
      viewer: { ...base.viewer, last_read_ordinal: 0 },
      posts: [
        post(1, { attachments: [image], deleted: false, removal: null }),
        // The API sends a tombstone none; one that came with some would
        // still draw none.
        post(2, { attachments: [text], deleted: true, removal: 'author', html: '' }),
      ],
    };
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(page), { status: 200 }));
    const root = await renderAt(`/t/${base.thread.id}/${base.thread.slug}?page=1`);
    expect(root.querySelector('#post-1 a.attachment')?.getAttribute('href')).toBe(`/blobs/${image.hash}`);
    expect(root.querySelector('#post-2.tombstone')).not.toBeNull();
    expect(root.querySelector('#post-2 .attachments')).toBeNull();
  });
});
