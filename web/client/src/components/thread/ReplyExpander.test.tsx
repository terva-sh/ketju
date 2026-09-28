import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Post, PostReplies, ThreadPage } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { renderAt, settle } from '../../testing';
import { expanderShown } from './ReplyExpander';

// The inline reply expander (TKT-01M29CGBW6; docs/06 "Post anatomy"). The
// criteria about where things are on screen are Playwright's
// (e2e/replies.spec.ts); these cover what is drawn and when.

const id = '3yymw47g00001';
const base = golden as ThreadPage;

function post(ordinal: number, over: Partial<Post> = {}): Post {
  return {
    ...base.posts[0],
    ordinal,
    reply_to: null,
    reply_to_suppressed: false,
    reply_count: 0,
    deleted: false,
    html: `<p>body of ${ordinal}</p>`,
    ...over,
  };
}

function page(posts: Post[], number = 1): ThreadPage {
  return {
    ...base,
    lens: null,
    thread: { ...base.thread, id, slug: 's', post_count: 80 },
    page: { number, size: 25, total_pages: 4 },
    viewer: { ...base.viewer, last_read_ordinal: 0 },
    posts,
  };
}

// serve answers the thread endpoint with p and a replies request from
// replies, and records every URL asked for.
function serve(p: ThreadPage, replies: Record<number, Post[]> = {}) {
  const urls: string[] = [];
  vi.stubGlobal('fetch', async (url: string) => {
    urls.push(url);
    const m = /\/posts\/(\d+)\/replies$/.exec(url);
    if (m) {
      const posts = replies[Number(m[1])];
      if (!posts) return new Response('{}', { status: 404 });
      const body: PostReplies = { ordinal: Number(m[1]), posts };
      return new Response(JSON.stringify(body), { status: 200 });
    }
    return new Response(JSON.stringify(p), { status: 200 });
  });
  return urls;
}

afterEach(() => vi.unstubAllGlobals());

const toggle = (root: HTMLElement, o: number) => root.querySelector<HTMLButtonElement>(`#post-${o} button.reply-expander`);

async function click(root: HTMLElement, el: HTMLElement) {
  await act(async () => el.click());
  await settle(root);
}

// AC3: the suppression rule, and its page edge (settled into docs/06).
describe('expanderShown', () => {
  const cases: { name: string; count: number; next?: { ordinal: number; reply_to: number | null }; shown: boolean }[] = [
    { name: 'no replies', count: 0, next: { ordinal: 11, reply_to: null }, shown: false },
    { name: 'the sole reply is directly below', count: 1, next: { ordinal: 11, reply_to: 10 }, shown: false },
    { name: 'two replies, one directly below', count: 2, next: { ordinal: 11, reply_to: 10 }, shown: true },
    { name: 'the sole reply is further down', count: 1, next: { ordinal: 11, reply_to: null }, shown: true },
    { name: 'the post below replies to another post', count: 1, next: { ordinal: 11, reply_to: 3 }, shown: true },
    // Under a lens the next post on the page need not be the next ordinal.
    { name: 'the sole reply is next on the page but not next in the thread', count: 1, next: { ordinal: 14, reply_to: 10 }, shown: true },
    // At the foot of a page the reply, if it is #11, opens the next page:
    // not in view, so offered.
    { name: 'last on the page', count: 1, next: undefined, shown: true },
  ];
  it.each(cases)('$name: $shown', ({ count, next, shown }) => {
    expect(expanderShown({ ordinal: 10, reply_count: count }, next)).toBe(shown);
  });
});

describe('the reply expander', () => {
  it('is drawn only where the rule offers it', async () => {
    serve(
      page([
        post(10, { reply_count: 1 }),
        post(11, { reply_to: 10, reply_to_suppressed: true }),
        post(12, { reply_count: 2 }),
        post(13, { reply_to: 12, reply_to_suppressed: true }),
        post(14),
        post(15, { reply_count: 1 }),
      ]),
    );
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(toggle(root, 10)).toBeNull();
    expect(toggle(root, 11)).toBeNull();
    expect(toggle(root, 12)?.textContent).toBe('2 replies');
    expect(toggle(root, 14)).toBeNull();
    expect(toggle(root, 15)?.textContent).toBe('1 reply');
  });

  // AC2 and AC4: read-only, recessed, in ordinal order, from any page.
  it('opens the replies read-only and recessed, in ordinal order, wherever they are', async () => {
    const urls = serve(page([post(1, { reply_count: 3 }), post(2), post(3)]), {
      1: [post(30, { reply_to: 1, html: '<p>from page 2</p>' }), post(55, { reply_to: 1 }), post(70, { reply_to: 1 })],
    });
    const root = await renderAt(`/t/${id}/s?page=1`);
    const t = toggle(root, 1)!;
    expect(t.getAttribute('aria-expanded')).toBe('false');
    await click(root, t);

    expect(urls).toContain(`/api/v1/threads/${id}/posts/1/replies`);
    expect(t.getAttribute('aria-expanded')).toBe('true');
    const region = root.querySelector(`#${t.getAttribute('aria-controls')}`)!;
    expect(region.getAttribute('aria-label')).toBe('Replies to #1');
    // Inside the post it answers, after its footer.
    expect(region.closest('#post-1')).not.toBeNull();
    expect(region.previousElementSibling?.classList.contains('post-footer')).toBe(true);

    const shown = [...region.querySelectorAll('.post')];
    expect(shown.map((e) => e.getAttribute('data-reply'))).toEqual(['30', '55', '70']);
    expect(region.textContent).toContain('from page 2');
    for (const e of shown) {
      expect(e.classList.contains('post--recessed')).toBe(true);
      // Copies: no anchor or ordinal a lookup would find, no actions, no
      // reply-to indicator (they all answer #1).
      expect(e.id).toBe('');
      expect(e.hasAttribute('data-ordinal')).toBe(false);
      expect(e.querySelector('button, .post-footer, .reply-to')).toBeNull();
      expect(e.querySelector('a.ordinal')?.getAttribute('href')).toBe(`/t/${id}/s/${e.getAttribute('data-reply')}`);
    }
    // The canonical posts are still the only ones with ids, in their order.
    expect([...root.querySelectorAll('[id^=post-]')].map((e) => e.id)).toEqual(['post-1', 'post-2', 'post-3']);

    await click(root, t);
    expect(t.getAttribute('aria-expanded')).toBe('false');
    expect(root.querySelector('.replies')).toBeNull();
    expect(document.activeElement === t || document.activeElement === document.body).toBe(true);
  });

  it('shows a reply’s attachments, linked to their blobs', async () => {
    const file = { hash: 'abc123', original_name: 'plan.pdf', mime: 'application/pdf', size: 2048 };
    serve(page([post(1, { reply_count: 1 })]), { 1: [post(7, { reply_to: 1, html: '', attachments: [file] })] });
    const root = await renderAt(`/t/${id}/s?page=1`);
    await click(root, toggle(root, 1)!);
    const link = root.querySelector('.replies [data-reply="7"] .attachments a')!;
    expect(link.getAttribute('href')).toBe('/blobs/abc123');
    expect(link.textContent).toBe('plan.pdf');
  });

  it('shows a deleted reply as a tombstone', async () => {
    serve(page([post(1, { reply_count: 2 })]), {
      1: [post(4, { reply_to: 1 }), post(9, { deleted: true, html: '', reply_to: null })],
    });
    const root = await renderAt(`/t/${id}/s?page=1`);
    await click(root, toggle(root, 1)!);
    const gone = root.querySelector('.replies [data-reply="9"]')!;
    expect(gone.classList.contains('tombstone')).toBe(true);
    expect(gone.textContent).toContain('Post removed.');
  });

  it('says so, with a retry, when the replies cannot be loaded', async () => {
    serve(page([post(1, { reply_count: 2 })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    await click(root, toggle(root, 1)!);
    expect(root.querySelector('.replies [role=alert]')?.textContent).toContain('Replies to #1 could not be loaded.');
  });

  it('keeps the toggle where it was on screen across the render', async () => {
    serve(page([post(1, { reply_count: 1 }), post(2)]), { 1: [post(9, { reply_to: 1 })] });
    const root = await renderAt(`/t/${id}/s?page=1`);
    const t = toggle(root, 1)!;
    // happy-dom lays nothing out, so fake a toggle the render pushes 40 px
    // down: the page is scrolled by the same 40 px to hold it.
    let top = 100;
    t.getBoundingClientRect = () => ({ top }) as DOMRect;
    const scrolled: number[] = [];
    vi.stubGlobal('scrollBy', (_x: number, y: number) => scrolled.push(y));
    await act(async () => {
      t.click();
      top = 140;
    });
    expect(scrolled).toEqual([40]);
  });
});
