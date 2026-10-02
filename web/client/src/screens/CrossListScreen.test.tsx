import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet } from '../api/client';
import type { Me, ThreadList } from '../api/dto';
import { isPhone } from '../state/layout';
import { listFor, resetLists } from '../state/lists';
import { dwellMs, resetReadTracker } from '../state/readTracker';
import { cachedThread, storeThreadPage } from '../state/threads';
import { viewer } from '../state/viewer';
import {
  dwellOn,
  lensHome,
  problemAnswer,
  renderAt,
  route,
  screenOf,
  serveLenses,
  settle,
  stubServer,
  threadPage,
  unmount,
  watchObservers,
  type StubServer,
} from '../testing';
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

// TKT-01M3XP6ZRZ, found by the Phase 3 exit check: leaving a thread flushes
// its read beacon, and the list the reader goes back to asks for itself at
// the same moment. When the list's GET reaches the server before the PUT,
// its answer still counts the posts just read. Once the PUT answers with a
// higher position, the held lists are dropped and the list on screen asks
// again (state/lists.ts readLanded).
describe('going back to a list while the read beacon is out', () => {
  const thread = lensHome.split('/')[2];
  // before is the list as the server holds it before the PUT lands: the
  // thread with two new posts. After the PUT it holds nothing.
  const before = () => list({ threads: [{ ...list().threads[0], id: thread, slug: 'a', unread: 2, last_read_ordinal: 0 }] });

  interface Server {
    // stored is whether the server has taken a PUT.
    stored: boolean;
    puts: number[];
    gets: number;
    // land answers the oldest PUT still out with status, storing it on a 200.
    land: (status?: number) => void;
    // hold makes the next /unread GET wait until held[i] is called.
    hold: boolean;
    held: (() => void)[];
  }

  // serve answers the thread from serveLenses, and the beacon and /unread
  // here: every PUT waits for land, and /unread answers with what the server
  // held when the GET arrived.
  function serve(): Server {
    serveLenses({ lastRead: 0 });
    const inner = globalThis.fetch;
    const waiting: ((status: number) => void)[] = [];
    const s: Server = { stored: false, puts: [], gets: 0, land: (status = 200) => waiting.shift()!(status), hold: false, held: [] };
    const json = (body: unknown, status = 200) =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json' },
      });
    vi.stubGlobal('fetch', (url: string, init: RequestInit = {}) => {
      if (init.method === 'PUT' && url === `/api/v1/threads/${thread}/read`) {
        const { ordinal } = JSON.parse(String(init.body)) as { ordinal: number };
        s.puts.push(ordinal);
        return new Promise<Response>((r) =>
          waiting.push((status) => {
            if (status !== 200) return r(json({ type: 'internal', title: 'internal', status }, status));
            s.stored = true;
            r(json({ last_read_ordinal: ordinal }));
          }),
        );
      }
      if (url === '/api/v1/unread') {
        s.gets++;
        // Tagged, as the server tags a list, so the client holds it.
        const stored = s.stored;
        const answer = () => {
          const r = json(stored ? list({ threads: [] }) : before());
          r.headers.set('ETag', stored ? '"after"' : '"before"');
          return r;
        };
        if (!s.hold) return Promise.resolve(answer());
        s.hold = false;
        return new Promise<Response>((r) => s.held.push(() => r(answer())));
      }
      return inner(url, init);
    });
    return s;
  }

  // readAndGoBack reads post 3 of the thread for the dwell, then goes to
  // /unread, which flushes the beacon as the thread page unmounts.
  async function readAndGoBack(): Promise<HTMLElement> {
    const root = await renderAt(`${lensHome}?page=1`);
    vi.useFakeTimers();
    dwellOn(3, dwellMs);
    vi.useRealTimers();
    await route('/unread');
    await settle(root);
    return root;
  }

  const badge = (root: HTMLElement) => root.querySelector(`[data-thread="${thread}"] [data-unread]`)?.textContent ?? null;

  beforeEach(() => {
    resetReadTracker();
    watchObservers();
    Element.prototype.scrollIntoView = () => {};
  });
  afterEach(async () => {
    vi.useRealTimers();
    await unmount();
    resetReadTracker();
  });

  it('asks for the list again once the PUT lands, when the list answered first', async () => {
    const s = serve();
    const root = await readAndGoBack();
    expect(s.puts).toEqual([3]);
    // The GET reached the server first, and counts the posts just read.
    expect(badge(root)).toBe('2 new');
    expect(s.gets).toBe(1);
    await act(async () => s.land());
    await until(() => root.querySelector('[data-empty-list]') !== null);
    expect(s.gets).toBe(2);
    expect(rows(root)).toEqual([]);
  });

  // A late answer must not put back what was true when it was asked for: the
  // GET still out when the PUT lands began before it.
  it('does not show or hold a list GET that answers after the PUT and the GET after it', async () => {
    const s = serve();
    s.hold = true;
    const root = await readAndGoBack();
    expect(s.gets).toBe(1);
    expect(screenOf(root)).toBe('unread');
    await act(async () => s.land());
    await until(() => s.gets === 2 && root.querySelector('[data-empty-list]') !== null);
    // The first GET answers now, with the list from before the PUT.
    await act(async () => s.held[0]());
    await settle(root);
    expect(rows(root)).toEqual([]);
    expect(peekGet<ThreadList>('/api/v1/unread')?.threads).toEqual([]);
  });

  // A refused PUT moved nothing and asks for nothing, and does not stop its
  // retry, from the list the reader went to, refreshing the list once it
  // lands.
  it('asks for nothing after a refused PUT, and for the list once its retry lands', async () => {
    const s = serve();
    const root = await readAndGoBack();
    await act(async () => s.land(503));
    await settle(root);
    expect(s.gets).toBe(1);
    expect(badge(root)).toBe('2 new');
    // The retry goes out on the next flush; a page going away is one.
    await act(async () => void dispatchEvent(new Event('pagehide')));
    expect(s.puts).toEqual([3, 3]);
    await act(async () => s.land());
    await until(() => root.querySelector('[data-empty-list]') !== null);
    expect(s.gets).toBe(2);
  });
});
