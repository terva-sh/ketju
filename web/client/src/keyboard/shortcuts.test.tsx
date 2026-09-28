import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Post, ThreadList, ThreadPage, ThreadRow } from '../api/dto';
import golden from '../../../../internal/dto/testdata/golden/thread.member.json';
import threadsGolden from '../../../../internal/dto/testdata/golden/threads.member.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { renderAt, route, settle } from '../testing';
import { helpOpen, hint, ignored, nextUnread, shortcuts, step } from './shortcuts';

// The desktop keyboard shortcuts (TKT-01M29CGBX4; docs/06 "Paged, not
// infinite"). The rules are tested as tables; the keys are dispatched on the
// document against a stubbed thread, as a reader's would be. Where things
// land on a real screen is Playwright's (e2e/shortcuts.spec.ts).

const id = '3yymw47g00001';
const slug = 'why-we-left-slack';
const base = golden as ThreadPage;

function post(ordinal: number): Post {
  return { ...base.posts[0], ordinal, reply_to: null, reply_to_suppressed: false, reply_count: 0, deleted: false, html: `<p>body of ${ordinal}</p>` };
}

// page n of a 15-post thread, five posts a page: this test's thread, or
// another in the forum, whose slug is s-{its id}.
function page(n: number, lastRead = 0, tid = id): ThreadPage {
  const first = (n - 1) * 5 + 1;
  return {
    ...base,
    lens: null,
    thread: { ...base.thread, id: tid, slug: tid === id ? slug : `s-${tid}`, post_count: 15 },
    page: { number: n, size: 5, total_pages: 3 },
    viewer: { ...base.viewer, last_read_ordinal: lastRead },
    posts: [0, 1, 2, 3, 4].map((i) => post(first + i)),
  };
}

const row = (rid: string, unread: number, lastRead = 0): ThreadRow => ({
  ...(threadsGolden as ThreadList).threads[0],
  id: rid,
  slug: `s-${rid}`,
  unread,
  last_read_ordinal: lastRead,
});

// serve answers a thread page by its ?page= or permalink ordinal, and the
// forum's list from lists, a page per cursor, once held says it may; it
// records every URL.
function serve(lastRead = 0, lists: ThreadList[] = [], held: Promise<void> = Promise.resolve()) {
  const urls: string[] = [];
  vi.stubGlobal('fetch', async (url: string) => {
    urls.push(url);
    const u = new URL(url, 'http://x');
    if (u.pathname.startsWith('/api/v1/forums/')) {
      await held;
      const i = Number(u.searchParams.get('cursor') ?? 0);
      return new Response(JSON.stringify(lists[i]), { status: 200 });
    }
    const tid = decodeURIComponent(u.pathname.split('/')[4]);
    const ordinal = Number(u.searchParams.get('ordinal') ?? 0);
    const n = ordinal ? Math.ceil(ordinal / 5) : Number(u.searchParams.get('page') ?? 1);
    return new Response(JSON.stringify(page(n, tid === id ? lastRead : 0, tid)), { status: 200 });
  });
  return urls;
}

const list = (threads: ThreadRow[], next = ''): ThreadList => ({ forum: { slug: 'general', name: 'General' }, threads, next_cursor: next });

// Each post's top is where it would be on a page scrolled to the top: 72
// px, the reading line, for the first, and 200 px apart.
let saved: Me;
let rect: typeof Element.prototype.getBoundingClientRect;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  helpOpen.value = false;
  Element.prototype.scrollIntoView = () => {};
  rect = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function (this: Element) {
    const o = Number(this.getAttribute('data-ordinal') ?? 0);
    const top = o ? ((o - 1) % 5) * 200 + 72 : 0;
    return { top, bottom: top + 180, left: 0, right: 0, width: 0, height: 180, x: 0, y: top, toJSON() {} } as DOMRect;
  };
});
afterEach(() => {
  viewer.value = saved;
  isPhone.value = false;
  Element.prototype.getBoundingClientRect = rect;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function press(key: string, target: Element = document.activeElement ?? document.body, init: KeyboardEventInit = {}) {
  await act(async () => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }));
  });
}
async function type(root: HTMLElement, ...keys: string[]) {
  for (const k of keys) await press(k);
  await settle(root);
}
const focused = () => document.activeElement?.id ?? document.activeElement?.tagName;

describe('the rules', () => {
  it.each([
    // tops, current, dir, want
    [[72, 272, 472], -1, 1, 0, 'j from nothing: the post at the reading line'],
    [[-128, 72, 272], -1, 1, 1, 'j from nothing: not one scrolled past'],
    [[-400, -200], -1, 1, 2, 'j from nothing, all above: off the page'],
    [[-128, 72, 272], -1, -1, 0, 'k from nothing: the one above the line'],
    [[72, 272], -1, -1, -1, 'k from nothing, none above: off the page'],
    [[72, 272, 472], 1, 1, 2, 'j from a post: the next'],
    [[72, 272, 472], 1, -1, 0, 'k from a post: the previous'],
    [[72, 272, 472], 2, 1, 3, 'j from the last: off the page'],
    [[72, 272, 472], 0, -1, -1, 'k from the first: off the page'],
  ] as const)('step %j from %i by %i is %i (%s)', (tops, current, dir, want, _why) => {
    expect(step([...tops], current, dir)).toBe(want);
  });

  it.each([
    [[['a', 0], ['b', 0], ['c', 3], ['d', 1]], 'b', 2, 'the first unread after the current thread'],
    [[['a', 2], ['b', 0], ['c', 0]], 'b', 0, 'none after: wraps to the first unread before'],
    [[['a', 0], ['b', 5], ['c', 0]], 'b', -1, 'the current thread itself is not next'],
    [[['a', 0], ['b', 4]], 'z', 1, 'the current thread not in the list: the first unread'],
    [[], 'b', -1, 'an empty forum'],
  ] as const)('nextUnread %j from %s is %i (%s)', (rows, current, want, _why) => {
    expect(nextUnread(rows.map(([rid, unread]) => ({ id: rid, unread })), current)).toBe(want);
  });

  it('ignores a key typed into text, the composer or a dialog, or chorded', () => {
    const el = (html: string) => {
      const d = document.createElement('div');
      d.innerHTML = html;
      document.body.replaceChildren(d);
      return d.querySelector('[data-t]')!;
    };
    const ev = (target: Element, init: KeyboardEventInit = {}) => {
      const e = new KeyboardEvent('keydown', { key: 'j', bubbles: true, cancelable: true, ...init });
      Object.defineProperty(e, 'target', { value: target });
      return e;
    };
    expect(ignored(ev(el('<p data-t>text</p>')))).toBe(false);
    expect(ignored(ev(el('<article tabindex="-1" data-t>post</article>')))).toBe(false);
    expect(ignored(ev(el('<textarea data-t></textarea>')))).toBe(true);
    expect(ignored(ev(el('<input data-t>')))).toBe(true);
    expect(ignored(ev(el('<select data-t></select>')))).toBe(true);
    expect(ignored(ev(el('<div contenteditable="true"><span data-t>x</span></div>')))).toBe(true);
    expect(ignored(ev(el('<section class="composer"><button data-t>Write</button></section>')))).toBe(true);
    expect(ignored(ev(el('<div role="dialog"><a href="/" data-t>go</a></div>')))).toBe(true);
    expect(ignored(ev(el('<p data-t>text</p>'), { ctrlKey: true }))).toBe(true);
    expect(ignored(ev(el('<p data-t>text</p>'), { metaKey: true }))).toBe(true);
    expect(ignored(ev(el('<p data-t>text</p>'), { altKey: true }))).toBe(true);
  });
});

// AC1: every documented shortcut works.
describe('the shortcuts on a thread page', () => {
  it('j and k move between the posts on the page, and focus each', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=2`);
    await type(root, 'j');
    expect(focused()).toBe('post-6');
    await type(root, 'j', 'j');
    expect(focused()).toBe('post-8');
    await type(root, 'k');
    expect(focused()).toBe('post-7');
  });

  it('skips a reply expander’s copies', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    // A copy inside post 1, as the expander draws it: no id.
    const copy = document.createElement('article');
    copy.className = 'post post--recessed';
    copy.setAttribute('data-reply', '9');
    root.querySelector('#post-1 .post-main')!.append(copy);
    await type(root, 'j', 'j');
    expect(focused()).toBe('post-2');
  });

  it('j past the last post opens the next page on its first post, k before the first the previous on its last', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=2`);
    root.querySelector<HTMLElement>('#post-10')!.focus();
    await type(root, 'j');
    expect(location.search).toBe('?page=3');
    await settle(root);
    expect(focused()).toBe('post-11');
    await type(root, 'k');
    expect(location.search).toBe('?page=2');
    await settle(root);
    expect(focused()).toBe('post-10');
  });

  it('j on the last page’s last post, and k on the first page’s first, stay put', async () => {
    const urls = serve();
    const root = await renderAt(`/t/${id}/${slug}?page=3`);
    root.querySelector<HTMLElement>('#post-15')!.focus();
    const asked = urls.length;
    await type(root, 'j');
    expect(focused()).toBe('post-15');
    expect(urls.length).toBe(asked);
  });

  it('g then a number then Enter goes to a post here, and to the permalink of one elsewhere', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'g', '3');
    expect(hint.value).toBe('Go to #3');
    expect(root.querySelector('.shortcut-hint')?.textContent).toBe('Go to #3');
    await type(root, 'Enter');
    expect(focused()).toBe('post-3');
    expect(hint.value).toBe('');

    await type(root, 'g', '1', '2', 'Enter');
    expect(location.pathname).toBe(`/t/${id}/${slug}/12`);
    await settle(root);
    expect(focused()).toBe('post-12');
  });

  it('g goes after a pause without Enter, and Escape abandons it', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'g', '4');
    await act(async () => void vi.advanceTimersByTime(1600));
    expect(focused()).toBe('post-4');

    await type(root, 'g', '5', 'Escape');
    expect(hint.value).toBe('');
    await act(async () => void vi.advanceTimersByTime(1600));
    expect(focused()).toBe('post-4');
  });

  it('g leaves Tab to move focus, and ends', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'g', '3');
    const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    await act(async () => void document.body.dispatchEvent(tab));
    expect(tab.defaultPrevented).toBe(false);
    expect(hint.value).toBe('');
    await type(root, 'Enter');
    expect(focused()).not.toBe('post-3');
  });

  it('a move whose page fails to load focuses nothing on the page the reader goes to next', async () => {
    const urls = serve();
    const ok = (globalThis.fetch as (u: string) => Promise<Response>).bind(globalThis);
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.includes('ordinal=12')) {
        urls.push(url);
        return new Response('{"type":"about:blank","title":"boom","status":500}', { status: 500 });
      }
      return ok(url);
    });
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'g', '1', '2', 'Enter');
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/${slug}/12`);
    expect(root.querySelector('#post-12')).toBeNull();
    // Page 3 holds #12, but it is not where g went.
    await route(`/t/${id}/${slug}?page=3`);
    await settle(root);
    expect(root.querySelector('#post-12')).not.toBeNull();
    expect(focused()).not.toBe('post-12');
  });

  it('g ends when focus moves, so a pause cannot take a reader from the composer', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const urls = serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'g', '1', '2');
    const area = root.querySelector<HTMLTextAreaElement>('.composer textarea')!;
    await act(async () => area.focus());
    expect(hint.value).toBe('');
    const asked = urls.length;
    await act(async () => void vi.advanceTimersByTime(1600));
    await settle(root);
    expect(document.activeElement).toBe(area);
    expect(urls.length).toBe(asked);
    expect(location.pathname + location.search).toBe(`/t/${id}/${slug}?page=1`);
  });

  it('g to a post the thread does not have says so and asks nothing', async () => {
    const urls = serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const asked = urls.length;
    await type(root, 'g', '9', '9', 'Enter');
    expect(hint.value).toBe('There is no post #99.');
    expect(urls.length).toBe(asked);
    expect(location.pathname).toBe(`/t/${id}/${slug}`);
  });

  it('u goes to the first unread post, here or on its page', async () => {
    serve(2);
    let root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'u');
    expect(focused()).toBe('post-3');

    serve(7);
    root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'u');
    expect(location.pathname).toBe(`/t/${id}/${slug}/8`);
    await settle(root);
    expect(focused()).toBe('post-8');
  });

  it('u with nothing unread says so', async () => {
    serve(15);
    const root = await renderAt(`/t/${id}/${slug}?page=3`);
    await type(root, 'u');
    expect(hint.value).toBe('Nothing unread in this thread.');
    expect(location.pathname).toBe(`/t/${id}/${slug}`);
  });

  it('n opens the next unread thread in the forum, where its badge goes', async () => {
    // The list pages: this thread is on the first page with nothing unread
    // after it, and the next unread one is on the second.
    const urls = serve(0, [
      { forum: { slug: 'general', name: 'General' }, threads: [row('aaa', 3), row(id, 0), row('bbb', 0)], next_cursor: '1' },
      { forum: { slug: 'general', name: 'General' }, threads: [row('ccc', 0), row('ddd', 4, 9)], next_cursor: '' },
    ]);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'n');
    await settle(root);
    expect(urls).toContain('/api/v1/forums/general/threads');
    expect(urls).toContain('/api/v1/forums/general/threads?cursor=1');
    expect(location.pathname).toBe('/t/ddd/s-ddd/10');
    // On the post the badge names, not the first on its page (review 1129).
    await settle(root);
    expect(focused()).toBe('post-10');
  });

  it('n reads the whole list before wrapping to a thread above', async () => {
    // Unread above on the first page, and below only on the seventh.
    const lists = [list([row('aaa', 3), row(id, 0)], '1')];
    for (let i = 1; i < 6; i++) lists.push(list([row(`p${i}`, 0)], String(i + 1)));
    lists.push(list([row('zzz', 2, 4)]));
    serve(0, lists);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'n');
    await settle(root);
    expect(location.pathname).toBe('/t/zzz/s-zzz/5');
  });

  it('n abandons its search when the reader leaves the thread', async () => {
    let release = () => {};
    const held = new Promise<void>((r) => (release = r));
    serve(0, [list([row(id, 0), row('bbb', 2)])], held);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'n');
    await route('/');
    await settle(root);
    await act(async () => release());
    await settle(root);
    expect(location.pathname).toBe('/');
    expect(hint.value).toBe('');
  });

  it('u and n do nothing for a reader who is not signed in, who has no controls for them', async () => {
    viewer.value = { ...viewer.value, user: null } as Me;
    const urls = serve(2, [list([row(id, 0), row('bbb', 2)])]);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const asked = urls.length;
    await type(root, 'u', 'n');
    expect(urls.length).toBe(asked);
    expect(document.activeElement?.id).not.toBe('post-3');
    expect(location.pathname).toBe(`/t/${id}/${slug}`);
    expect(hint.value).toBe('');
  });

  it('n with no other unread thread says so', async () => {
    serve(0, [{ forum: { slug: 'general', name: 'General' }, threads: [row(id, 4)], next_cursor: '' }]);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await type(root, 'n');
    await settle(root);
    expect(hint.value).toBe('No other unread threads in General.');
    expect(location.pathname).toBe(`/t/${id}/${slug}`);
  });

  it('? opens the list of shortcuts, and Escape closes it and gives focus back', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    root.querySelector<HTMLElement>('#post-2')!.focus();
    await type(root, '?');
    const dialog = root.querySelector('[role=dialog]')!;
    expect(dialog.getAttribute('aria-labelledby')).toBe('help-title');
    expect([...dialog.querySelectorAll('[data-shortcut]')].map((r) => r.getAttribute('data-shortcut'))).toEqual(['j', 'k', 'g', 'u', 'n', '?']);
    // Keys typed in the dialog are the dialog's.
    await type(root, 'j');
    expect(focused()).not.toBe('post-3');
    await press('Escape', document.activeElement!);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(focused()).toBe('post-2');
  });
});

// AC1: none fires while focus is in the composer.
describe('typing in the composer', () => {
  it('fires no shortcut', async () => {
    const urls = serve(2);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const area = root.querySelector<HTMLTextAreaElement>('.composer textarea')!;
    expect(area).not.toBeNull();
    area.focus();
    const asked = urls.length;
    for (const k of ['j', 'k', 'g', '3', 'Enter', 'u', 'n', '?']) await press(k, area);
    await settle(root);
    expect(document.activeElement).toBe(area);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(hint.value).toBe('');
    expect(urls.length).toBe(asked);
    expect(location.pathname + location.search).toBe(`/t/${id}/${slug}?page=1`);
  });

  it('nor does typing in the Go to post box', async () => {
    serve(2);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const box = root.querySelector<HTMLInputElement>('.goto-post input')!;
    box.focus();
    for (const k of ['j', 'u', '?']) await press(k, box);
    expect(document.activeElement).toBe(box);
    expect(root.querySelector('[role=dialog]')).toBeNull();
  });
});

// AC2: each shortcut has a visible control that does the same.
describe('the visible controls', () => {
  it.each(shortcuts.map((s) => s.keys[0]))('%s has one', async (key) => {
    serve(2);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const control: Record<string, string> = {
      j: '.pager a[rel=next]',
      k: '.pager a[rel=prev], .pager .step.disabled',
      g: '.goto-post input',
      u: '[data-shell=toolbar] [data-action=first-unread]',
      n: '.thread-foot button.next-unread',
      '?': '.thread-foot button.shortcuts-help',
    };
    expect(root.querySelector(control[key]), key).not.toBeNull();
  });

  it('Go to post goes to the post', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const box = root.querySelector<HTMLInputElement>('.goto-post input')!;
    await act(async () => {
      box.value = '4';
      box.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => root.querySelector<HTMLFormElement>('.goto-post')!.requestSubmit());
    expect(focused()).toBe('post-4');
  });

  it('First unread goes to the first unread post', async () => {
    serve(2);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await act(async () => root.querySelector<HTMLButtonElement>('[data-action=first-unread]')!.click());
    expect(focused()).toBe('post-3');
  });

  it('First unread is not offered when everything is read, or to a reader not signed in', async () => {
    serve(15);
    let root = await renderAt(`/t/${id}/${slug}?page=3`);
    expect(root.querySelector('[data-action=first-unread]')).toBeNull();
    viewer.value = { ...viewer.value, user: null } as Me;
    serve(0);
    root = await renderAt(`/t/${id}/${slug}?page=1`);
    expect(root.querySelector('[data-action=first-unread]')).toBeNull();
    expect(root.querySelector('.next-unread')).toBeNull();
  });

  it('Next unread thread opens it', async () => {
    serve(0, [{ forum: { slug: 'general', name: 'General' }, threads: [row(id, 0), row('bbb', 2)], next_cursor: '' }]);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    await act(async () => root.querySelector<HTMLButtonElement>('.next-unread')!.click());
    await settle(root);
    expect(location.pathname).toBe('/t/bbb/s-bbb');
    // With no read position the server chose the page: the first unread
    // post on it, here the first.
    await settle(root);
    expect(focused()).toBe('post-1');
  });

  it('Keyboard shortcuts opens the list', async () => {
    serve();
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const button = root.querySelector<HTMLButtonElement>('.shortcuts-help')!;
    button.focus();
    await act(async () => button.click());
    expect(root.querySelector('[role=dialog] [data-shortcut="?"]')).not.toBeNull();
    await act(async () => root.querySelector<HTMLButtonElement>('.help-close')!.click());
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(document.activeElement).toBe(button);
  });
});

// AC4: inert on the phone layout.
describe('on the phone layout', () => {
  it('no shortcut does anything, and the list is not offered', async () => {
    isPhone.value = true;
    const urls = serve(2);
    const root = await renderAt(`/t/${id}/${slug}?page=1`);
    const before = document.activeElement;
    const asked = urls.length;
    await type(root, 'j', 'k', 'u', 'n', '?', 'g', '3', 'Enter');
    expect(document.activeElement).toBe(before);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(hint.value).toBe('');
    expect(urls.length).toBe(asked);
    expect(location.pathname + location.search).toBe(`/t/${id}/${slug}?page=1`);
    expect(root.querySelector('.shortcuts-help')).toBeNull();
    // The controls the shortcuts stand in for are all still here.
    expect(root.querySelector('.goto-post')).not.toBeNull();
    expect(root.querySelector('.next-unread')).not.toBeNull();
  });
});
