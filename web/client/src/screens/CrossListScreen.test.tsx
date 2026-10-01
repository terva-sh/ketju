import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet } from '../api/client';
import type { Me, ThreadList } from '../api/dto';
import { isPhone } from '../state/layout';
import { listFor, resetLists } from '../state/lists';
import { cachedThread, storeThreadPage } from '../state/threads';
import { viewer } from '../state/viewer';
import { problemAnswer, renderAt, route, screenOf, settle, stubServer, threadPage, type StubServer } from '../testing';
import anonMe from '../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import unreadList from '../../../../internal/dto/testdata/golden/unread.member.json';

// The four cross-forum lists (TKT-01M3V6Y4JP, docs/06 "Thread list views"),
// against a stubbed server answering the member golden for each.

const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(memberMe) as Me;
  isPhone.value = false;
  resetLists();
});
afterEach(() => {
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

const list = (over: Partial<ThreadList> = {}) => ({ ...(structuredClone(unreadList) as ThreadList), ...over });
const ok = (body: unknown) => () => ({ status: 200, body });
const lists = ['unread', 'new', 'participated', 'watching'] as const;
const titles = { unread: 'Unread', new: 'New', participated: 'Participated', watching: 'Watching' };

// until waits for what a request chain ends in: settle returns once a
// screen is up, and a mark is a PUT, then a GET.
async function until(cond: () => boolean) {
  for (let i = 0; i < 100 && !cond(); i++) await act(() => new Promise((r) => setTimeout(r, 5)));
  expect(cond()).toBe(true);
}

const rows = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('[data-thread]')].map((r) => r.dataset.thread);
const gets = (s: StubServer) => s.sent.filter((r) => r.method === 'GET').map((r) => r.url);

// AC1: each list from its own route, each row with its forum.
describe('each list', () => {
  for (const name of lists) {
    it(`renders /${name} from GET /api/v1/${name}, each row naming its forum`, async () => {
      const s = stubServer({ [`GET /api/v1/${name}`]: ok(list()) });
      const root = await renderAt(`/${name}`);
      expect(screenOf(root)).toBe(name);
      expect(root.querySelector('main h1')?.textContent).toBe(titles[name]);
      expect(gets(s)).toContain(`/api/v1/${name}`);
      expect(rows(root)).toEqual(['3yymw47g00001', '3yymw47g00002']);
      const forums = [...root.querySelectorAll<HTMLAnchorElement>('[data-forum-link]')];
      expect(forums.map((a) => [a.textContent, a.getAttribute('href')])).toEqual([
        ['General', '/f/general'],
        ['Standups', '/f/standups'],
      ]);
      // The forum is first in the meta line, and a sibling of the title.
      const first = root.querySelector('[data-thread="3yymw47g00001"]')!;
      expect(first.querySelector('.thread-row-meta')?.firstElementChild).toBe(forums[0]);
      expect(first.querySelector('.thread-title')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack?page=1');
      // The unread badge is the forum list's: to the first unread post.
      expect(first.querySelector('[data-unread]')?.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/201');
    });
  }

  it('titles the phone top bar with the list', async () => {
    stubServer({ 'GET /api/v1/watching': ok(list()) });
    isPhone.value = true;
    const root = await renderAt('/watching');
    expect(root.querySelector('[data-shell=title]')?.textContent).toBe('Watching');
  });

  it('draws the heading before the list answers', async () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}));
    const root = await renderAt('/new');
    expect(root.querySelector('main h1')?.textContent).toBe('New');
    expect(root.querySelector('[data-thread]')).toBeNull();
  });

  it('reports a failure with a way to try again', async () => {
    const s = stubServer({ 'GET /api/v1/unread': () => problemAnswer('internal', 500) });
    const root = await renderAt('/unread');
    expect(root.querySelector('.error-panel')?.textContent).toContain('The unread list could not be loaded.');
    s.routes['GET /api/v1/unread'] = ok(list());
    await act(async () => root.querySelector<HTMLButtonElement>('.error-panel button')!.click());
    await settle(root);
    expect(rows(root)).toHaveLength(2);
  });
});

// AC1: paging by Older, the forum list's own paging.
describe('paging', () => {
  it('links the older page by the server cursor, and has no way back from the first', async () => {
    stubServer({ 'GET /api/v1/participated': ok(list({ next_cursor: 'c2' })) });
    const root = await renderAt('/participated');
    expect(root.querySelector('[data-older]')?.getAttribute('href')).toBe('/participated?cursor=c2');
    expect([...root.querySelectorAll('.thread-list-pages a')].map((a) => a.textContent)).toEqual(['Older threads']);
  });

  it('asks for the page in the address, and offers the newest', async () => {
    const s = stubServer({ 'GET /api/v1/unread?cursor=a+b%2F': ok(list()) });
    const root = await renderAt('/unread?cursor=a%20b/');
    expect(gets(s)).toContain('/api/v1/unread?cursor=a+b%2F');
    const pages = [...root.querySelectorAll<HTMLAnchorElement>('.thread-list-pages a')];
    expect(pages.map((a) => [a.textContent, a.getAttribute('href')])).toEqual([['Newest', '/unread']]);
  });

  it('follows Older to the next page', async () => {
    const s = stubServer({
      'GET /api/v1/watching': ok(list({ next_cursor: 'c2' })),
      'GET /api/v1/watching?cursor=c2': ok(list({ threads: [list().threads[1]] })),
    });
    const root = await renderAt('/watching');
    await act(async () => root.querySelector<HTMLAnchorElement>('[data-older]')!.click());
    await settle(root);
    expect(location.pathname + location.search).toBe('/watching?cursor=c2');
    expect(gets(s)).toContain('/api/v1/watching?cursor=c2');
    expect(rows(root)).toEqual(['3yymw47g00002']);
  });

  it('says a page past the end has no more', async () => {
    stubServer({ 'GET /api/v1/new?cursor=z': ok(list({ threads: [] })) });
    const root = await renderAt('/new?cursor=z');
    expect(root.querySelector('[data-empty-list]')?.textContent).toBe('No more threads.');
  });
});

// AC1: an empty state each, in the words settled in grooming.
describe('empty states', () => {
  const words = {
    unread: 'Nothing unread. Threads with posts you have not read appear here.',
    new: 'No new threads. Threads you have not opened appear here.',
    participated: 'You have not posted yet. Threads you post in appear here.',
    watching: "You are not watching any threads. Choose Watch in a thread's menu to follow it here.",
  };
  for (const name of lists) {
    it(`/${name}`, async () => {
      stubServer({ [`GET /api/v1/${name}`]: ok(list({ threads: [] })) });
      const root = await renderAt(`/${name}`);
      expect(root.querySelector('[data-empty-list]')?.textContent).toBe(words[name]);
      expect(root.querySelector('.thread-rows')).toBeNull();
      expect(root.querySelector('[data-mark-read]')).toBeNull();
    });
  }
});

// AC2: Mark all read on /unread clears the list and the badges without a
// reload.
describe('mark all read', () => {
  const me = (threads: number, fresh: number): Me => {
    const m = structuredClone(memberMe) as Me;
    m.unread = { ...m.unread, threads, new: fresh };
    return m;
  };

  it('marks the site read, then asks for the list and /me again', async () => {
    let marked = false;
    const s = stubServer({
      'GET /api/v1/unread': () => ({ status: 200, body: marked ? list({ threads: [] }) : list() }),
      'PUT /api/v1/me/read': () => ((marked = true), { status: 200, body: { at: 1757500400000 } }),
      'GET /api/v1/me': () => ({ status: 200, body: me(0, 0) }),
    });
    const root = await renderAt('/unread');
    const badge = (nav: string) => root.querySelector(`[data-nav=${nav}] [data-badge]`)?.firstChild?.textContent ?? null;
    expect(badge('unread')).toBe('12');
    expect(badge('new')).toBe('6');
    const before = location.href;

    await act(async () => root.querySelector<HTMLButtonElement>('[data-mark-read]')!.click());
    await until(() => rows(root).length === 0 && badge('unread') === null);
    expect(s.sent.filter((r) => r.method === 'PUT')).toEqual([{ method: 'PUT', url: '/api/v1/me/read', body: undefined }]);
    expect(gets(s).filter((u) => u === '/api/v1/unread')).toHaveLength(2);
    expect(gets(s)).toContain('/api/v1/me');
    expect(rows(root)).toEqual([]);
    expect(root.querySelector('[data-empty-list]')?.textContent).toContain('Nothing unread.');
    expect(badge('unread')).toBeNull();
    expect(badge('new')).toBeNull();
    expect(location.href).toBe(before);
    // Nothing left to mark.
    expect(root.querySelector('[data-mark-read]')).toBeNull();
  });

  it('drops the held lists and thread pages the mark has made stale', async () => {
    stubServer({
      'GET /api/v1/unread': ok(list()),
      'GET /api/v1/forums': () => ({ status: 200, body: { forums: [], members: null }, headers: { ETag: '"f1"' } }),
      'PUT /api/v1/me/read': ok({ at: 1 }),
      'GET /api/v1/me': ok(me(0, 0)),
    });
    // The forum index, visited first, is held.
    const root = await renderAt('/');
    const thread = { id: '01k4z3j0w8x2c', slug: 'a', title: 'A' };
    storeThreadPage(thread.id, threadPage(thread));
    expect(cachedThread(thread.id)).toBeDefined();
    await route('/unread');
    await settle(root);
    expect(peekGet('/api/v1/forums')).toBeDefined();

    await act(async () => root.querySelector<HTMLButtonElement>('[data-mark-read]')!.click());
    await until(() => peekGet('/api/v1/forums') === undefined);
    expect(cachedThread(thread.id)).toBeUndefined();
  });

  // Terva review 1802: the reader leaves for another list while the PUT is
  // out, and that list answers before the mark lands. The list on screen
  // asks for itself again when it does.
  it('refreshes the list on screen when a mark lands after the reader moved on', async () => {
    let marked = false;
    let land!: () => void;
    const s = stubServer({
      'GET /api/v1/unread': ok(list()),
      'GET /api/v1/new': () => ({ status: 200, body: marked ? list({ threads: [] }) : list() }),
      'PUT /api/v1/me/read': () =>
        new Promise((r) => (land = () => ((marked = true), r({ status: 200, body: { at: 1 } })))),
      'GET /api/v1/me': ok(me(0, 0)),
    });
    const root = await renderAt('/unread');
    await act(async () => root.querySelector<HTMLButtonElement>('[data-mark-read]')!.click());
    await route('/new');
    await until(() => screenOf(root) === 'new' && rows(root).length === 2);
    expect(gets(s).filter((u) => u === '/api/v1/new')).toHaveLength(1);

    await act(async () => land());
    await until(() => rows(root).length === 0);
    expect(gets(s).filter((u) => u === '/api/v1/new')).toHaveLength(2);
    expect(root.querySelector('[data-empty-list]')?.textContent).toContain('No new threads.');
  });

  it('does not ask again for a list mounted after the mark', async () => {
    const s = stubServer({
      'GET /api/v1/unread': ok(list()),
      'GET /api/v1/watching': ok(list()),
      'PUT /api/v1/me/read': ok({ at: 1 }),
      'GET /api/v1/me': ok(me(0, 0)),
    });
    const root = await renderAt('/unread');
    await act(async () => root.querySelector<HTMLButtonElement>('[data-mark-read]')!.click());
    await until(() => gets(s).includes('/api/v1/me'));
    await route('/watching');
    await until(() => rows(root).length === 2);
    await settle(root);
    expect(gets(s).filter((u) => u === '/api/v1/watching')).toHaveLength(1);
  });

  it('keeps the list and says so when the mark fails', async () => {
    const s = stubServer({
      'GET /api/v1/unread': ok(list()),
      'PUT /api/v1/me/read': () => problemAnswer('internal', 500),
    });
    const root = await renderAt('/unread');
    const button = root.querySelector<HTMLButtonElement>('[data-mark-read]')!;
    await act(async () => button.click());
    await settle(root);
    expect(root.querySelector('.mark-read-error[role=alert]')?.textContent).toContain('Could not mark all read.');
    expect(rows(root)).toHaveLength(2);
    expect(button.disabled).toBe(false);
    expect(gets(s).filter((u) => u === '/api/v1/unread')).toHaveLength(1);
  });

  it('is offered on /unread only', async () => {
    for (const name of ['new', 'participated', 'watching'] as const) {
      stubServer({ [`GET /api/v1/${name}`]: ok(list()) });
      const root = await renderAt(`/${name}`);
      expect(rows(root)).toHaveLength(2);
      expect(root.querySelector('[data-mark-read]')).toBeNull();
    }
  });
});

// AC2: offered only to members. The lists are a member's own: a signed-out
// reader is asked to sign in, and nothing is asked of the server.
describe('signed out', () => {
  for (const name of lists) {
    it(`/${name} asks the reader to sign in`, async () => {
      viewer.value = structuredClone(anonMe) as Me;
      const s = stubServer({});
      const root = await renderAt(`/${name}?cursor=x`);
      expect(screenOf(root)).toBe(name);
      const a = root.querySelector<HTMLAnchorElement>('[data-sign-in] a')!;
      expect(a.textContent).toBe('Sign in');
      expect(a.getAttribute('href')).toBe('/auth/signin?return_to=' + encodeURIComponent(`/${name}?cursor=x`));
      expect(gets(s).filter((u) => u.startsWith(`/api/v1/${name}`))).toEqual([]);
      expect(root.querySelector('[data-mark-read]')).toBeNull();
      expect(root.querySelector('[data-thread]')).toBeNull();
    });
  }
});

// A thread marked unread from a cross-forum list goes back to it
// (state/lists.ts).
describe('the list a thread goes back to', () => {
  it('is the cross-forum list the reader was on, whatever the thread’s forum', async () => {
    stubServer({ 'GET /api/v1/unread?cursor=c2': ok(list()) });
    expect(listFor('general')).toBe('/f/general');
    await renderAt('/unread?cursor=c2');
    expect(listFor('general')).toBe('/unread?cursor=c2');
    expect(listFor('standups')).toBe('/unread?cursor=c2');
  });
});
