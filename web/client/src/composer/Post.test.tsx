import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CreatedPost, Post, ThreadPage } from '../api/dto';
import { ProblemError } from '../api/problem';
import { composerFor, postReply, resetComposers } from '../state/composer';
import { isPhone } from '../state/layout';
import { renderAt, threadPage } from '../testing';
import { postFailure } from './Composer';

const thread = { id: '3yymw47g00001', slug: 'why-we-left-slack', title: 'Why we left Slack' };
const path = `/t/${thread.id}/${thread.slug}?page=1`;

function post(g: ThreadPage, ordinal: number, html: string, handle: string): Post {
  return { ...g.posts[0], ordinal, html, deleted: false, reply_to: null, reply_to_suppressed: false, author: { ...g.posts[0].author, handle } };
}

// before is the page as it was; after holds the reply the server made.
function pages() {
  const g = threadPage(thread);
  const base: ThreadPage = {
    ...g,
    thread: { ...g.thread, post_count: 2, log_length: 1000 },
    page: { number: 1, size: 25, total_pages: 1 },
    viewer: { ...g.viewer, last_read_ordinal: 2, can_reply: true },
    posts: [post(g, 1, '<p>one</p>', 'mara'), post(g, 2, '<p>two</p>', 'drew')],
  };
  const created = post(g, 3, '<p data-posted>my reply</p>', 'carol');
  const after: ThreadPage = { ...base, thread: { ...base.thread, post_count: 3, log_length: 1200 }, posts: [...base.posts, created] };
  return { before: base, after, created };
}

interface Call {
  method: string;
  url: string;
  body: unknown;
}

// serve answers the thread page (the page after the reply once one is
// posted), and POST /posts with answer(), which may hold it open.
function serve(answer: () => Promise<Response>) {
  const { before, after, created } = pages();
  const calls: Call[] = [];
  let posted = false;
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    calls.push({ method, url, body: init.body ? JSON.parse(String(init.body)) : null });
    if (url === '/api/v1/render') return new Response(JSON.stringify({ html: '', mentions: [], quotes: [], reply_to: null }));
    if (method === 'POST' && url.endsWith('/posts')) {
      const r = await answer();
      if (r.ok) posted = true;
      return r;
    }
    return new Response(JSON.stringify(posted ? after : before), { status: 200 });
  });
  const ok = (): CreatedPost => ({ post: created, ordinal: 3, revision: 1, log_length: 1200 });
  return { calls, ok, posts: () => calls.filter((c) => c.method === 'POST' && c.url.endsWith('/posts')) };
}

const problem = (type: string, status: number, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ type, title: 't', status }), {
    status,
    headers: { 'Content-Type': 'application/problem+json', ...headers },
  });

beforeEach(() => {
  resetComposers();
  Element.prototype.scrollIntoView = () => {};
});
afterEach(() => {
  isPhone.value = false;
  vi.unstubAllGlobals();
  document.documentElement.style.overflow = '';
});

const area = (root: HTMLElement) => root.querySelector<HTMLTextAreaElement>('.composer textarea')!;
const postButton = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-post-reply]')!;
const click = (el: Element | null) => act(() => (el as HTMLElement).click());
const settle = () => act(() => new Promise((r) => setTimeout(r, 0)));

async function type(root: HTMLElement, text: string) {
  const a = area(root);
  a.value = text;
  a.setSelectionRange(text.length, text.length);
  await act(() => void a.dispatchEvent(new Event('input', { bubbles: true })));
}

// AC1.
describe('posting a reply', () => {
  it('sends the source and the chip, clears the composer, and shows the new post where it landed', async () => {
    const s = serve(async () => new Response(JSON.stringify(s.ok()), { status: 201 }));
    const root = await renderAt(path);
    expect(postButton(root).disabled).toBe(true); // nothing to post yet
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    await type(root, 'my reply');
    await click(postButton(root));
    await settle();
    await settle();

    expect(s.posts()).toEqual([{ method: 'POST', url: `/api/v1/threads/${thread.id}/posts`, body: { source: 'my reply', reply_to: 2 } }]);
    expect(location.pathname).toBe(`/t/${thread.id}/${thread.slug}/3`);
    expect(root.querySelector('#post-3 [data-posted]')).not.toBeNull();
    expect(area(root).value).toBe('');
    expect(root.querySelector('.composer .reply-chip')).toBeNull();
    expect(root.querySelector('.post-failure')).toBeNull();
  });

  it('sends no reply_to for a reply to the thread', async () => {
    const s = serve(async () => new Response(JSON.stringify(s.ok()), { status: 201 }));
    const root = await renderAt(path);
    await type(root, '  answer  ');
    await click(postButton(root));
    await settle();
    expect(s.posts()[0].body).toEqual({ source: '  answer  ' });
  });

  it('does not post whitespace, whoever asks', async () => {
    const s = serve(async () => new Response('{}', { status: 201 }));
    composerFor(thread.id).source.value = ' \n\t ';
    expect(await postReply(thread.id)).toBeNull();
    expect(s.posts()).toHaveLength(0);
    expect(composerFor(thread.id).source.value).toBe(' \n\t ');
  });

  it('does not post whitespace from the button', async () => {
    const s = serve(async () => new Response('{}', { status: 201 }));
    const root = await renderAt(path);
    await type(root, ' \n ');
    expect(postButton(root).disabled).toBe(true);
    await click(postButton(root));
    expect(s.posts()).toHaveLength(0);
  });
});

// AC4.
describe('a double click', () => {
  it('posts once, with the button off and the text read-only while it is in flight', async () => {
    let release!: () => void;
    const s = serve(() => new Promise<Response>((r) => (release = () => r(new Response(JSON.stringify(s.ok()), { status: 201 })))));
    const root = await renderAt(path);
    await type(root, 'my reply');
    const b = postButton(root);
    await act(() => {
      b.click();
      b.click();
    });
    expect(b.disabled).toBe(true);
    expect(b.getAttribute('aria-busy')).toBe('true');
    expect(area(root).readOnly).toBe(true);
    await click(b);
    expect(s.posts()).toHaveLength(1);
    await act(() => release());
    await settle();
    expect(s.posts()).toHaveLength(1);
  });
});

// Review on PR #81: nothing chosen while a post is in flight is lost when
// it succeeds, because nothing can be chosen then.
describe('while a post is in flight', () => {
  it('quote, reply and the chip change nothing, so the success clears only what was sent', async () => {
    let release!: () => void;
    const s = serve(() => new Promise<Response>((r) => (release = () => r(new Response(JSON.stringify(s.ok()), { status: 201 })))));
    const root = await renderAt(path);
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    await type(root, 'my reply');
    await click(postButton(root));
    const chipX = root.querySelector<HTMLButtonElement>('.composer .reply-chip button')!;
    expect(chipX.disabled).toBe(true);
    await click(root.querySelector('#post-1 [data-post-action=reply]'));
    await click(root.querySelector('#post-1 [data-post-action=quote]'));
    await click(root.querySelector('[data-shell=toolbar] [data-action=reply]'));
    expect(area(root).value).toBe('my reply');
    expect(root.querySelector('.composer .reply-chip')?.getAttribute('data-reply-chip')).toBe('2');
    expect(document.activeElement).toBe(area(root)); // still brought up
    await act(() => release());
    await settle();
    await settle();
    expect(s.posts()).toHaveLength(1);
    // Once it is posted the composer is live again.
    await click(root.querySelector('#post-1 [data-post-action=reply]'));
    expect(root.querySelector('.composer .reply-chip')?.getAttribute('data-reply-chip')).toBe('1');
  });
});

// AC3.
describe('a refused post', () => {
  it.each([
    ['conflict', 409, {}, 'locked or archived'],
    ['forbidden', 403, {}, 'may not reply'],
    ['rate-limited', 429, { 'Retry-After': '30' }, 'post again in 30 seconds'],
    ['validation-failed', 422, {}, 'not accepted'],
    ['payload-too-large', 413, {}, 'longer than the limit'],
    ['unavailable', 503, {}, 'briefly unavailable'],
  ])('%s keeps the source and the chip, and says why', async (type_, status, headers, says) => {
    serve(async () => problem(type_, status, headers));
    const root = await renderAt(path);
    await click(root.querySelector('#post-1 [data-post-action=reply]'));
    await type(root, 'my reply');
    await click(postButton(root));
    await settle();
    expect(root.querySelector('[role=alert].post-failure')?.textContent).toContain(says);
    expect(area(root).value).toBe('my reply');
    expect(area(root).readOnly).toBe(false);
    expect(root.querySelector('.composer .reply-chip')?.getAttribute('data-reply-chip')).toBe('1');
    expect(location.pathname).toBe(`/t/${thread.id}/${thread.slug}`);
    expect(postButton(root).disabled).toBe(false);
  });

  it('forgets why on the next attempt', async () => {
    let n = 0;
    const s = serve(async () => (n++ === 0 ? problem('unavailable', 503) : new Response(JSON.stringify(s.ok()), { status: 201 })));
    const root = await renderAt(path);
    await type(root, 'my reply');
    await click(postButton(root));
    await settle();
    expect(root.querySelector('.post-failure')).not.toBeNull();
    await click(postButton(root));
    await settle();
    await settle();
    expect(root.querySelector('.post-failure')).toBeNull();
    expect(root.querySelector('#post-3')).not.toBeNull();
  });

  it('says a one-second wait in the singular, and a slow mode with no wait plainly', () => {
    expect(postFailure(new ProblemError('rate-limited', 429, 1))).toContain('in 1 second.');
    expect(postFailure(new ProblemError('rate-limited', 429, null))).toBe('You are posting too quickly. Wait a moment and try again.');
  });
});

describe('discard', () => {
  it('empties the text and the chip, and is off when there is nothing to discard', async () => {
    serve(async () => new Response('{}', { status: 201 }));
    const root = await renderAt(path);
    const discard = root.querySelector<HTMLButtonElement>('.composer-foot .discard')!;
    expect(discard.disabled).toBe(true);
    await click(root.querySelector('#post-1 [data-post-action=reply]'));
    expect(discard.disabled).toBe(false);
    await type(root, 'draft');
    await click(discard);
    expect(area(root).value).toBe('');
    expect(root.querySelector('.composer .reply-chip')).toBeNull();
    expect(discard.disabled).toBe(true);
  });
});

describe('posting from the phone sheet', () => {
  it('posts from the sheet head, closes the sheet, empties the bar, and shows the post', async () => {
    isPhone.value = true;
    const s = serve(async () => new Response(JSON.stringify(s.ok()), { status: 201 }));
    const root = await renderAt(path);
    await click(root.querySelector('#post-2 [data-post-action=reply]'));
    await type(root, 'my reply');
    await click(root.querySelector('.sheet-head [data-post-reply]'));
    await settle();
    await settle();
    expect(s.posts()).toHaveLength(1);
    expect(root.querySelector('.composer-sheet')).toBeNull();
    expect(root.querySelector('.replybar-text')?.textContent).toBe('Write a reply…');
    expect(root.querySelector('.replybar .reply-chip')).toBeNull();
    expect(root.querySelector('#post-3 [data-posted]')).not.toBeNull();
  });

  it('keeps the sheet open, with the text, when the post is refused', async () => {
    isPhone.value = true;
    serve(async () => problem('conflict', 409));
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    await type(root, 'my reply');
    await click(root.querySelector('.sheet-head [data-post-reply]'));
    await settle();
    expect(root.querySelector('.composer-sheet [role=alert]')?.textContent).toContain('locked');
    expect(area(root).value).toBe('my reply');
  });

  it('still clears the composer when the sheet was closed while the post was in flight', async () => {
    isPhone.value = true;
    let release!: () => void;
    const s = serve(() => new Promise<Response>((r) => (release = () => r(new Response(JSON.stringify(s.ok()), { status: 201 })))));
    const root = await renderAt(path);
    await click(root.querySelector('.replybar-open'));
    await type(root, 'my reply');
    await click(root.querySelector('.sheet-head [data-post-reply]'));
    await click(root.querySelector('.sheet-close'));
    expect(root.querySelector('.replybar-text')?.textContent).toBe('my reply');
    await act(() => release());
    await settle();
    await settle();
    expect(composerFor(thread.id).source.value).toBe('');
    expect(root.querySelector('.replybar-text')?.textContent).toBe('Write a reply…');
  });
});
