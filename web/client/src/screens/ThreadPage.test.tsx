import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Post, ThreadPage } from '../api/dto';
import golden from '../../../../internal/dto/testdata/golden/thread.member.json';
import { pageWindow, pageHref } from '../components/thread/Pager';
import { dividerBefore } from '../components/thread/UnreadDivider';
import { renderAt, route, settle } from '../testing';

const id = '3yymw47g00001';
const base = golden as ThreadPage;

function post(ordinal: number, over: Partial<Post> = {}): Post {
  return {
    ...base.posts[0],
    ordinal,
    reply_to: null,
    reply_to_suppressed: false,
    deleted: false,
    html: `<p>body of ${ordinal}</p>`,
    ...over,
  };
}

// page builds a thread page: the golden's header, with these posts.
function page(posts: Post[], over: { lastRead?: number; postCount?: number; number?: number; totalPages?: number } = {}): ThreadPage {
  return {
    ...base,
    lens: null,
    thread: { ...base.thread, id, slug: 'why-we-left-slack', post_count: over.postCount ?? 212 },
    page: { number: over.number ?? 1, size: 25, total_pages: over.totalPages ?? 9 },
    viewer: { ...base.viewer, last_read_ordinal: over.lastRead ?? 0 },
    posts,
  };
}

// serve answers the thread endpoint with p, a single-post request with the
// post named, and records every URL asked for.
function serve(p: ThreadPage, single: Record<number, Post> = {}) {
  const urls: string[] = [];
  vi.stubGlobal('fetch', async (url: string) => {
    urls.push(url);
    const m = /\/posts\/(\d+)$/.exec(url);
    if (m) {
      const one = single[Number(m[1])];
      return one ? new Response(JSON.stringify(one), { status: 200 }) : new Response('{}', { status: 404 });
    }
    return new Response(JSON.stringify(p), { status: 200 });
  });
  return urls;
}

let scrolled: string[];
beforeEach(() => {
  scrolled = [];
  Element.prototype.scrollIntoView = function (this: Element) {
    scrolled.push(this.id);
  };
});
afterEach(() => vi.unstubAllGlobals());

const at = (root: HTMLElement, sel: string) => root.querySelector(sel);

// AC1: the reply-to indicator.
describe('the reply-to indicator', () => {
  it('shows for a reply to a post further up, and not when the parent is directly above', async () => {
    serve(
      page([
        post(1),
        post(2, { reply_to: 1, reply_to_suppressed: true }),
        post(3, { reply_to: 1 }),
      ]),
    );
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-2 .reply-to')).toBeNull();
    expect(at(root, '#post-3 .reply-to')?.textContent).toBe('↩ #1');
    expect(at(root, '#post-1 .reply-to')).toBeNull();
  });

  it("takes the server's word on suppression rather than working it out", async () => {
    // Directly above, but the server says show it (as it would under a lens
    // that hides the posts between): shown.
    serve(page([post(1), post(2, { reply_to: 1, reply_to_suppressed: false })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-2 .reply-to')).not.toBeNull();
  });
});

// AC2: following it.
describe('following a link to another post', () => {
  it('scrolls to a post on this page and leaves the address alone', async () => {
    serve(page([post(1), post(2), post(3, { reply_to: 1 })]));
    const root = await renderAt(`/t/${id}/why-we-left-slack?page=1`);
    const before = location.href;
    scrolled = [];
    await act(async () => (at(root, '#post-3 .reply-to') as HTMLElement).click());
    expect(scrolled).toEqual(['post-1']);
    expect(location.href).toBe(before);
    expect(at(root, '[role=dialog]')).toBeNull();
  });

  it('opens a popover with the post, and a way to go to it, when it is not on this page', async () => {
    const urls = serve(page([post(26), post(27, { reply_to: 4 })], { number: 2 }), {
      4: post(4, { html: '<p>the earlier post</p>' }),
    });
    const root = await renderAt(`/t/${id}/why-we-left-slack?page=2`);
    const before = location.href;
    await act(async () => (at(root, '#post-27 .reply-to') as HTMLElement).click());
    await settle(root);
    const dialog = at(root, '[role=dialog]')!;
    expect(dialog.textContent).toContain('the earlier post');
    expect(dialog.querySelector('a.go-to')?.getAttribute('href')).toBe(`/t/${id}/why-we-left-slack/4`);
    expect(urls).toContain(`/api/v1/threads/${id}/posts/4`);
    expect(location.href).toBe(before);
    await act(async () => (dialog.querySelector('button.close') as HTMLElement).click());
    expect(at(root, '[role=dialog]')).toBeNull();
  });

  it('treats a quote link in a body the same way', async () => {
    serve(
      page([post(1), post(2, { html: '<details class="quote" open><summary><a href="#post-1" class="quote-source">Post #1</a></summary></details>' })]),
    );
    const root = await renderAt(`/t/${id}/s?page=1`);
    scrolled = [];
    await act(async () => (at(root, '#post-2 a.quote-source') as HTMLElement).click());
    expect(scrolled).toEqual(['post-1']);
  });

  it('closes the popover when the reader moves to another page', async () => {
    const p2 = page([post(26), post(27, { reply_to: 4 })], { number: 2 });
    const p3 = page([post(51), post(52)], { number: 3 });
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.endsWith('/posts/4')) return new Response(JSON.stringify(post(4)), { status: 200 });
      const body = url.includes('page=3') ? p3 : p2;
      return new Response(JSON.stringify(body), { status: 200, headers: { ETag: url.includes('page=3') ? '"3"' : '"2"' } });
    });
    const root = await renderAt(`/t/${id}/why-we-left-slack?page=3`);
    await route(`/t/${id}/why-we-left-slack?page=2`);
    await settle(root);
    await act(async () => (at(root, '#post-27 .reply-to') as HTMLElement).click());
    await settle(root);
    expect(at(root, '[role=dialog]')).not.toBeNull();
    await route(`/t/${id}/why-we-left-slack?page=3`); // held: shown at once
    expect(at(root, '#post-51')).not.toBeNull();
    expect(at(root, '[role=dialog]')).toBeNull();
  });

  it('says so when the post cannot be loaded', async () => {
    serve(page([post(26), post(27, { reply_to: 4 })], { number: 2 }));
    const root = await renderAt(`/t/${id}/s?page=2`);
    await act(async () => (at(root, '#post-27 .reply-to') as HTMLElement).click());
    await settle(root);
    expect(at(root, '[role=dialog] [role=alert]')).not.toBeNull();
  });
});

// AC3: tombstones.
describe('a deleted post', () => {
  it('is a tombstone in place, keeping its ordinal and anchor', async () => {
    serve(page([post(1), post(2, { deleted: true, html: '' }), post(3)]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    const t = at(root, '#post-2')!;
    expect(t.classList.contains('tombstone')).toBe(true);
    expect(t.textContent).toContain('#2');
    expect(t.textContent).toContain('Post removed');
    const order = [...root.querySelectorAll('[data-ordinal]')].map((e) => e.getAttribute('data-ordinal'));
    expect(order).toEqual(['1', '2', '3']);
  });

  it("shows its content, marked, to a moderator, whose answer carries it", async () => {
    serve(page([post(2, { deleted: true, html: '<p>what was said</p>' })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-2')?.textContent).toContain('what was said');
    expect(at(root, '#post-2 .deleted-mark')).not.toBeNull();
  });
});

// AC5: the divider, and landing on return.
describe('the unread divider', () => {
  it('sits between the last read post and the next, from the golden page', async () => {
    serve({ ...base, thread: { ...base.thread, id } });
    const root = await renderAt(`/t/${id}/s?page=3`);
    const d = at(root, '.unread-divider')!;
    expect(d.nextElementSibling?.id).toBe('post-61');
    expect(d.textContent).toContain('152 new posts below');
    expect(d.textContent).toContain('last read #60');
  });

  it('sits mid-page between read and unread, marking the unread', async () => {
    const posts = Array.from({ length: 5 }, (_, i) => post(i + 1));
    serve(page(posts, { lastRead: 3, postCount: 5, totalPages: 1 }));
    const root = await renderAt(`/t/${id}/s?page=1`);
    const d = at(root, '.unread-divider')!;
    expect(d.previousElementSibling?.id).toBe('post-3');
    expect(d.nextElementSibling?.id).toBe('post-4');
    expect(d.textContent).toContain('2 new posts below');
    expect([...root.querySelectorAll('.post.unread')].map((e) => e.id)).toEqual(['post-4', 'post-5']);
  });

  it.each([
    ['nothing is read', [1, 2, 3], 0, 10],
    ['everything is read', [8, 9, 10], 10, 10],
    ['the line is on an earlier page', [26, 27, 28], 10, 60],
    ['the line is on a later page', [1, 2, 3], 40, 60],
  ])('is absent when %s', (_, ordinals, lastRead, count) => {
    expect(dividerBefore(ordinals, lastRead, count)).toBeNull();
  });

  it.each([
    ['at the top of the landing page', [26, 27, 28], 25, 60, 26],
    ['mid-page', [26, 27, 28], 26, 60, 27],
    ['after a tombstone gap, with a read post on the page', [26, 29, 30], 27, 60, 29],
  ])('is drawn %s', (_, ordinals, lastRead, count, want) => {
    expect(dividerBefore(ordinals, lastRead, count)).toBe(want);
  });

  // A reader who has read nothing gets no divider, and no edge on every post
  // either: the whole thread is new, and marking all of it marks nothing.
  it('marks no post unread for a reader who has read none', async () => {
    serve(page([post(1), post(2)], { lastRead: 0, postCount: 2, totalPages: 1 }));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(root.querySelectorAll('.post.unread').length).toBe(0);
    expect(at(root, '.unread-divider')).toBeNull();
  });

  // Returning to a landing shows the held page at once and scrolls; the
  // revalidation that follows, with a new copy of the same page, does not
  // move the reader again.
  it('scrolls once per arrival, not again when the page revalidates', async () => {
    const p = page([post(26), post(27)], { lastRead: 25, postCount: 60, number: 2, totalPages: 3 });
    let calls = 0;
    let release!: () => void;
    vi.stubGlobal('fetch', async () => {
      calls++;
      if (calls === 3) await new Promise<void>((r) => (release = r));
      return new Response(JSON.stringify(p), { status: 200, headers: { ETag: `"${calls}"` } });
    });
    const root = await renderAt(`/t/${id}/why-we-left-slack`);
    expect(scrolled).toEqual(['unread']);
    await route(`/t/${id}/why-we-left-slack?page=2`);
    await settle(root);
    await route(`/t/${id}/why-we-left-slack`);
    await settle(root);
    expect(scrolled).toEqual(['unread', 'unread']); // the held copy, at once
    await act(async () => release());
    await settle(root);
    expect(calls).toBe(3);
    expect(scrolled).toEqual(['unread', 'unread']);
  });

  it('lands with no page asked for, and scrolls to the divider', async () => {
    const urls = serve(page([post(26), post(27), post(28)], { lastRead: 25, postCount: 60, number: 2, totalPages: 3 }));
    const root = await renderAt(`/t/${id}/why-we-left-slack`);
    expect(urls[0]).toBe(`/api/v1/threads/${id}`);
    expect(at(root, '.unread-divider')?.nextElementSibling?.id).toBe('post-26');
    expect(scrolled).toEqual(['unread']);
    expect(location.search).toBe('');
  });

  it('lands on the last read post when the divider is not on the page', async () => {
    serve(page([post(59), post(60)], { lastRead: 60, postCount: 60, number: 3, totalPages: 3 }));
    await renderAt(`/t/${id}/why-we-left-slack`);
    expect(scrolled).toEqual(['post-60']);
  });

  it('scrolls a permalink to its post, not to the divider', async () => {
    serve(page([post(26), post(27), post(28)], { lastRead: 25, postCount: 60, number: 2 }));
    await renderAt(`/t/${id}/why-we-left-slack/28`);
    expect(scrolled).toEqual(['post-28']);
  });

  it('does not scroll a page the reader chose', async () => {
    serve(page([post(26), post(27)], { lastRead: 25, postCount: 60, number: 2 }));
    await renderAt(`/t/${id}/why-we-left-slack?page=2`);
    expect(scrolled).toEqual([]);
  });
});

describe('the pager', () => {
  it.each([
    [1, 1, [1]],
    [1, 5, [1, 2, 3, 4, 5]],
    [5, 9, [1, 2, 3, 4, 5, 6, 7, 8, 9]],
    [1, 9, [1, 2, 3, null, 9]],
    [9, 9, [1, null, 7, 8, 9]],
    [5, 20, [1, 2, 3, 4, 5, 6, 7, null, 20]],
    [10, 20, [1, null, 8, 9, 10, 11, 12, null, 20]],
  ] as const)('numbers page %i of %i as %j', (current, total, want) => {
    expect(pageWindow(current, total)).toEqual(want);
  });

  it('always names the page, the first included, and keeps the lens', () => {
    const t = { id, slug: 's' };
    expect(pageHref(t, 1, '')).toBe(`/t/${id}/s?page=1`);
    expect(pageHref(t, 4, 'author:drew')).toBe(`/t/${id}/s?page=4&lens=author%3Adrew`);
  });

  it('draws both rows, with the current page and the span', async () => {
    serve(page([post(26), post(27)], { number: 2, totalPages: 9 }));
    const root = await renderAt(`/t/${id}/s?page=2`);
    const nav = at(root, '.pager')!;
    expect(nav.querySelector('[aria-current=page]')?.textContent).toBe('2');
    expect(nav.textContent).toContain('posts 26–50 of 212');
    expect(nav.querySelector('.pager-narrow')?.textContent).toContain('Page 2 of 9');
    expect(nav.querySelector('a[rel=prev]')?.getAttribute('href')).toBe(`/t/${id}/why-we-left-slack?page=1`);
  });

  it('ends the span at the last post on the last page', async () => {
    serve(page([post(201), post(212)], { number: 9, totalPages: 9, postCount: 212 }));
    const root = await renderAt(`/t/${id}/s?page=9`);
    expect(at(root, '.pager')?.textContent).toContain('posts 201–212 of 212');
  });

  it('is absent for a one-page thread', async () => {
    serve(page([post(1)], { totalPages: 1, postCount: 1 }));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '.pager')).toBeNull();
  });

  it('follows a page link in place', async () => {
    const urls = serve(page([post(26), post(27)], { number: 2, totalPages: 9 }));
    const root = await renderAt(`/t/${id}/why-we-left-slack?page=2`);
    const next = at(root, '.pager-wide a[rel=next]') as HTMLAnchorElement;
    await act(async () => next.click());
    await settle(root);
    expect(location.search).toBe('?page=3');
    expect(urls.at(-1)).toBe(`/api/v1/threads/${id}?page=3`);
  });
});

describe('a post', () => {
  it('marks an agent and a moderator, and no one else', async () => {
    const who = (handle: string, role: string, kind: string) => ({ ...base.posts[0].author, handle, role, kind });
    serve(
      page([
        post(1, { author: who('a', 'member', 'human') }),
        post(2, { author: who('b', 'moderator', 'human') }),
        post(3, { author: who('c', 'member', 'agent') }),
      ]),
    );
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-1 .role-badge')).toBeNull();
    expect(at(root, '#post-2 .role-badge')?.textContent).toBe('moderator');
    expect(at(root, '#post-3 .role-badge')?.textContent).toBe('agent');
  });

  it('says it was edited, and links to itself and its history', async () => {
    serve(page([post(7, { edited_at: 1757500200000 }), post(8, { edited_at: null })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-7 .edited')).not.toBeNull();
    expect(at(root, '#post-8 .edited')).toBeNull();
    const links = [...root.querySelectorAll('#post-7 .post-footer a')].map((a) => a.getAttribute('href'));
    expect(links).toEqual([`/t/${id}/why-we-left-slack/7`, `/t/${id}/why-we-left-slack/7/history`]);
  });

  it('draws an uploaded avatar, or the initial', async () => {
    const author = base.posts[0].author;
    serve(page([post(1, { author: { ...author, avatar: 'abc' } }), post(2, { author: { ...author, avatar: '', display_name: 'zed' } })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(at(root, '#post-1 img.avatar')?.getAttribute('src')).toBe('/blobs/abc');
    expect(at(root, '#post-2 .avatar-fallback')?.textContent).toBe('Z');
  });

  it('lists its attachments', async () => {
    serve(page([post(1, { attachments: [{ hash: 'h1', size: 2048, mime: 'image/png', original_name: 'shot.png' }] })]));
    const root = await renderAt(`/t/${id}/s?page=1`);
    const a = at(root, '#post-1 .attachments a')!;
    expect(a.getAttribute('href')).toBe('/blobs/h1');
    expect(at(root, '#post-1 .attachments')?.textContent).toContain('2.0 KB');
  });
});

// Review 421: the history route is not the thread page.
describe('the post history route', () => {
  it('says what it is and links back to the post, rather than listing the page', async () => {
    serve(page([post(26), post(27)], { number: 2 }));
    const root = await renderAt(`/t/${id}/why-we-left-slack/27/history`);
    expect(root.querySelector('[data-screen]')?.getAttribute('data-screen')).toBe('post-history');
    expect(at(root, 'h1')?.textContent).toBe(base.thread.title);
    expect(at(root, '.history-pending h2')?.textContent).toBe('History of post #27');
    expect(at(root, '.history-pending a')?.getAttribute('href')).toBe(`/t/${id}/why-we-left-slack/27`);
    expect(at(root, '.posts')).toBeNull();
    expect(at(root, '.pager')).toBeNull();
  });
});

// Review 421: the unread edge runs through a tombstone past the read line.
describe('an unread tombstone', () => {
  it('carries the unread edge like the posts around it', async () => {
    serve(page([post(1), post(2), post(3, { deleted: true, html: '' }), post(4)], { lastRead: 2, postCount: 4, totalPages: 1 }));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect([...root.querySelectorAll('.unread')].map((e) => e.id)).toEqual(['post-3', 'post-4']);
    expect(at(root, '.unread-divider')?.nextElementSibling?.id).toBe('post-3');
  });

  it('does not carry it with nothing read', async () => {
    serve(page([post(1), post(2, { deleted: true, html: '' })], { lastRead: 0, postCount: 2, totalPages: 1 }));
    const root = await renderAt(`/t/${id}/s?page=1`);
    expect(root.querySelectorAll('.unread').length).toBe(0);
  });
});
