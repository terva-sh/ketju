import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet } from '../api/client';
import type { Forum, Me, ThreadHeader, ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
import { resetLists } from '../state/lists';
import { dwellMs, resetReadTracker } from '../state/readTracker';
import { viewer } from '../state/viewer';
import {
  problemAnswer,
  renderAt,
  route,
  screenOf,
  settle,
  stubServer,
  threadJSON,
  threadPage,
  unmount,
  type Answer,
  type StubServer,
} from '../testing';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import moderator from '../../../../internal/dto/testdata/golden/me.moderator.json';
import threads from '../../../../internal/dto/testdata/golden/threads.member.json';

const problem = (type: string, status: number, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ type, title: 't', status }), {
    status,
    headers: { 'Content-Type': 'application/problem+json', ...headers },
  });

afterEach(() => vi.unstubAllGlobals());

// AC4 on a screen: a failed load says so, and Retry reaches the thread.
describe('the thread screen', () => {
  it('recovers from a failure through Retry', async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async (url: string) => {
      // The reader's draft is asked for once the thread arrives, and is none.
      if (url.startsWith('/api/v1/drafts/')) return new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
      if (calls++ === 0) throw new TypeError('network down');
      return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Arrived' }), { status: 200 });
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.querySelector('[role=alert]')?.textContent).toContain('Could not reach the server');
    const retry = [...root.querySelectorAll('button')].find((b) => b.textContent === 'Retry')!;
    await act(async () => retry.click());
    await settle(root);
    expect(root.textContent).toContain('Arrived');
    expect(root.querySelector('[role=alert]')).toBeNull();
    expect(calls).toBe(2);
  });

  it('offers a sign-in for unauthenticated', async () => {
    vi.stubGlobal('fetch', async () => problem('unauthenticated', 401));
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.querySelector('[data-sign-in]')).not.toBeNull();
  });

  it('does not offer a sign-in for forbidden', async () => {
    vi.stubGlobal('fetch', async () => problem('forbidden', 403));
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.querySelector('[role=alert]')?.textContent).toContain('does not have access');
    expect(root.querySelector('[data-sign-in]')).toBeNull();
  });

  it('shows the wait a rate limit asks for, and leaves the retry to the reader', async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async () => (calls++, problem('rate-limited', 429, { 'Retry-After': '30' })));
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.textContent).toContain('Try again in 30 seconds');
    await act(() => new Promise((r) => setTimeout(r, 50)));
    expect(calls).toBe(1);
  });

  it('says a deleted thread was deleted, rather than calling it missing', async () => {
    vi.stubGlobal('fetch', async () => problem('gone', 410));
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).toContain('This has been deleted');
  });

  // The title shown while another page of the same thread loads is the
  // thread's own, from the cache.
  it("keeps the thread's title while another of its pages loads", async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async () =>
      calls++ === 0
        ? new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Kept' }), { status: 200 })
        : new Promise<Response>(() => {}),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(root.querySelector('h1')?.textContent).toBe('Kept');
  });

  // A page seen before shows at once when the reader returns to it, and is
  // revalidated with its tag.
  it('shows a page it holds at once and revalidates it', async () => {
    const asked: (string | undefined)[] = [];
    let release!: () => void;
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      const tag = (init.headers as Record<string, string>)['If-None-Match'];
      asked.push(tag);
      if (asked.length === 1)
        return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Held' }), {
          status: 200,
          headers: { ETag: '"t1"' },
        });
      if (asked.length === 2)
        return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Page 2' }), { status: 200 });
      await new Promise<void>((r) => (release = r));
      return new Response(null, { status: 304 });
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(root.querySelector('h1')?.textContent).toBe('Page 2');
    await route('/t/01k4z3j0w8x2c/a');
    await settle(root);
    expect(root.querySelector('h1')?.textContent).toBe('Held');
    expect(asked[2]).toBe('"t1"');
    await act(async () => release());
    await settle(root);
    expect(root.querySelector('h1')?.textContent).toBe('Held');
  });

  // Review 411: a thread the viewer loses access to is not shown on retry,
  // though the screen was keeping it beside an earlier failure, nor at once
  // on the next visit.
  it.each([
    ['forbidden', 403],
    ['unauthenticated', 401],
  ] as const)('stops showing a thread once it answers %s', async (type, status) => {
    let calls = 0;
    vi.stubGlobal('fetch', async () => {
      calls++;
      if (calls === 1)
        return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Secret' }), {
          status: 200,
          headers: { ETag: '"s"' },
        });
      if (calls === 3) throw new TypeError('network down'); // revalidating page 1
      if (calls === 4) return problem(type, status); // the retry
      return new Promise<Response>(() => {}); // page 2, and anything later
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await route('/t/01k4z3j0w8x2c/a');
    await settle(root);
    // Back on the held page, whose revalidation failed: kept, beside the error.
    expect(calls).toBe(3);
    expect(root.querySelector('h1')?.textContent).toBe('Secret');
    expect(root.querySelector('[role=alert]')).not.toBeNull();
    const retry = [...root.querySelectorAll('button')].find((x) => x.textContent === 'Retry')!;
    await act(async () => retry.click());
    await settle(root);
    expect(calls).toBe(4);
    expect(root.textContent).not.toContain('Secret');
    await route('/t/01k4z3j0w8x2c/a?page=3');
    await route('/t/01k4z3j0w8x2c/a');
    await settle(root);
    expect(root.textContent).not.toContain('Secret');
  });
});

// TKT-01M29CGBWN: the thread page tracks what a signed-in reader reads of
// it, and a move away sends it.
describe('read tracking on the thread screen', () => {
  const saved = viewer.value;
  afterEach(() => {
    viewer.value = saved;
    resetReadTracker();
  });

  it('sends the position read when the reader navigates away, and tracks nothing for a visitor', async () => {
    const observers: { observed: Element[]; callback: IntersectionObserverCallback }[] = [];
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observed: Element[] = [];
        constructor(readonly callback: IntersectionObserverCallback) {
          observers.push(this);
        }
        observe(el: Element) {
          this.observed.push(el);
        }
        disconnect() {}
      },
    );
    const puts: { url: string; keepalive?: boolean; body: unknown }[] = [];
    vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
      if (init.method === 'PUT') {
        puts.push({ url, keepalive: init.keepalive, body: JSON.parse(String(init.body)) });
        return new Response('{"last_read_ordinal":62}', { status: 200 });
      }
      if (String(url).startsWith('/api/v1/threads/'))
        return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Read' }), { status: 200 });
      return new Promise<Response>(() => {});
    });

    viewer.value = { ...structuredClone(member), user: null } as Me;
    await renderAt('/t/01k4z3j0w8x2c/a');
    expect(observers).toHaveLength(0);

    viewer.value = structuredClone(member) as Me;
    await renderAt('/t/01k4z3j0w8x2c/a');
    const io = observers.at(-1)!;
    // The golden page holds posts 61 to 64, and the reader has read to 60.
    expect(io.observed.map((el) => el.id)).toEqual(['post-61', 'post-62', 'post-63', 'post-64']);
    vi.useFakeTimers();
    io.observed[1].getBoundingClientRect = () => ({ top: 100, bottom: 300, height: 200 }) as DOMRect;
    io.callback(
      [
        {
          target: io.observed[1],
          isIntersecting: true,
          intersectionRect: { height: 200 },
          boundingClientRect: { height: 200 },
          rootBounds: { height: 800 },
        } as unknown as IntersectionObserverEntry,
      ],
      io as unknown as IntersectionObserver,
    );
    vi.advanceTimersByTime(dwellMs);
    vi.useRealTimers();
    expect(puts).toEqual([]);
    await route('/f/general');
    await act(() => new Promise((r) => setTimeout(r, 5)));
    expect(puts).toEqual([{ url: '/api/v1/threads/01k4z3j0w8x2c/read', keepalive: true, body: { ordinal: 62 } }]);
  });
});

// TKT-01M3QQWA5Z: flag, from the thread page. The member golden's viewer is
// alice, and of its posts only #62, scribe's, is another member's and shown.
describe('flagging from the thread screen', () => {
  const saved = viewer.value;
  const id = '01k4z3j0w8x2c';
  let sent: { method: string; url: string; body: unknown }[];

  beforeEach(() => {
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    sent = [];
    vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
      const method = init.method ?? 'GET';
      sent.push({ method, url, body: init.body ? JSON.parse(String(init.body)) : undefined });
      if (method === 'POST' && url === `/api/v1/threads/${id}/flags`)
        return new Response('{"received":true}', { status: 200, headers: { 'Cache-Control': 'no-store' } });
      if (method === 'GET' && url.startsWith(`/api/v1/threads/${id}`))
        return new Response(threadJSON({ id, slug: 'a', title: 'Flagged' }), { status: 200 });
      return new Promise<Response>(() => {});
    });
    viewer.value = structuredClone(member) as Me;
  });
  afterEach(async () => {
    await unmount();
    viewer.value = saved;
    isPhone.value = false;
  });

  const threadGets = () => sent.filter((r) => r.method === 'GET' && r.url.startsWith(`/api/v1/threads/${id}`)).length;
  const dialog = () => document.querySelector<HTMLElement>('[role=dialog]');
  async function report(d: HTMLElement) {
    await act(async () => d.querySelector<HTMLInputElement>('input[value=spam]')!.click());
    await act(async () => d.querySelector<HTMLButtonElement>('.flag-send')!.click());
    await act(() => new Promise((r) => setTimeout(r, 5)));
  }

  it('flags a post from the page', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect([...root.querySelectorAll('[data-post-action=flag]')].map((b) => b.closest('article')?.id)).toEqual(['post-62']);
    const gets = threadGets();
    const flag = root.querySelector<HTMLButtonElement>('#post-62 [data-post-action=flag]')!;
    flag.focus();
    await act(async () => flag.click());
    expect(dialog()?.querySelector('h2')?.textContent).toBe('Flag post #62');
    await report(dialog()!);
    expect(sent.filter((r) => r.method === 'POST')).toEqual([
      { method: 'POST', url: `/api/v1/threads/${id}/flags`, body: { ordinal: 62, reason: 'spam', note: '' } },
    ]);
    expect(dialog()?.textContent).toContain('Thanks, a moderator will look.');
    await act(() => new Promise((r) => setTimeout(r, 20)));
    expect(threadGets(), 'the thread page is not asked for again').toBe(gets);
    await act(async () => dialog()!.querySelector<HTMLButtonElement>('[data-flag-close]')!.click());
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('#post-62 [data-post-action=flag]'));
  });

  it.each(['Close', 'Escape'])("flags a post from the phone's More, closed with %s", async (how) => {
    isPhone.value = true;
    const root = await renderAt(`/t/${id}/a`);
    const more = root.querySelector<HTMLButtonElement>('#post-62 [data-post-more]')!;
    await act(async () => more.click());
    expect(more.getAttribute('aria-expanded')).toBe('true');
    await act(async () => root.querySelector<HTMLButtonElement>('#post-62 [data-post-action=flag]')!.click());
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(dialog()?.querySelector('h2')?.textContent).toBe('Flag post #62');
    expect(dialog()!.contains(document.activeElement)).toBe(true);
    if (how === 'Close') await act(async () => dialog()!.querySelector<HTMLButtonElement>('[data-flag-close]')!.click());
    else
      await act(async () =>
        void document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })),
      );
    expect(dialog()).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('#post-62 [data-post-more]'));
    expect(document.activeElement).not.toBe(document.body);
    expect(more.getAttribute('aria-expanded')).toBe('false');
  });

  it('a signed-out reader has no flag', async () => {
    viewer.value = { ...structuredClone(member), user: null } as Me;
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('#post-62')).not.toBeNull();
    expect(root.querySelector('[data-post-action=flag]')).toBeNull();
  });
});

// TKT-01M3QGH66W: lock, unlock, pin and unpin, from the thread page. They are
// offered on the page's viewer.can_moderate, whatever the role, and a global
// pin only to a moderator or admin by role, so the page offers exactly what
// the server allows (TKT-01M3RQP0TG).
describe('moderating from the thread screen', () => {
  const saved = viewer.value;
  const id = '01k4z3j0w8x2c';
  const base = threadPage({ id, slug: 'a', title: 'Moderated' });
  const url = `/api/v1/threads/${id}`;
  const len = base.thread.log_length;

  type Over = { thread?: Partial<ThreadHeader>; viewer?: Partial<ThreadPage['viewer']> };
  const at = (o: Over = {}): ThreadPage => ({
    ...base,
    thread: { ...base.thread, pinned: null, ...o.thread },
    viewer: { ...base.viewer, can_moderate: true, ...o.viewer },
  });
  const as = (role: 'member' | 'moderator' | 'admin') => {
    const me = structuredClone(role === 'member' ? member : moderator) as Me;
    me.user!.role = role;
    viewer.value = me;
  };

  // pages answers the thread GETs in turn, the last one again for any later
  // request; a Promise holds its answer until it settles.
  let pages: (ThreadPage | Promise<ThreadPage>)[];
  let moderate: Record<string, (body: unknown) => Answer>;
  let forums: Forum[];
  let server: StubServer;
  beforeEach(() => {
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    pages = [at()];
    moderate = {};
    forums = [];
    const page = async () => ({ status: 200, body: await (pages.length > 1 ? pages.shift()! : pages[0]) });
    const routes: StubServer['routes'] = {
      [`GET ${url}`]: page,
      [`GET ${url}?lens=x&ordinal=61`]: page,
      [`PATCH ${url}`]: (b) => moderate.rename?.(b) ?? problemAnswer('internal', 500),
      'GET /api/v1/forums': () => ({ status: 200, body: { forums }, headers: { ETag: '"f1"' } }),
    };
    for (const kind of ['lock', 'unlock', 'pin', 'unpin', 'move'])
      routes[`POST /api/v1/admin/threads/${id}/${kind}`] = (b) => moderate[kind]?.(b) ?? problemAnswer('internal', 500);
    routes[`PUT /api/v1/admin/threads/${id}/slow-mode`] = (b) => moderate['slow-mode']?.(b) ?? problemAnswer('internal', 500);
    server = stubServer(routes);
    as('moderator');
  });
  afterEach(async () => {
    await unmount();
    viewer.value = saved;
    isPhone.value = false;
  });

  const gets = () => server.sent.filter((r) => r.method === 'GET' && r.url === url).length;
  const posts = () => server.sent.filter((r) => r.method === 'POST').map((r) => ({ url: r.url, body: r.body }));
  const offered = (root: HTMLElement) =>
    [...root.querySelectorAll('[data-shell=toolbar] [data-action]')]
      .map((b) => b.getAttribute('data-action'))
      .filter((a) => ['lock', 'unlock', 'pin', 'unpin'].includes(a!));
  const panel = () => document.querySelector<HTMLElement>('[data-moderate]');
  async function open(root: HTMLElement, kind: string) {
    await act(async () => root.querySelector<HTMLButtonElement>(`[data-shell=toolbar] [data-action=${kind}]`)!.click());
    expect(panel()?.getAttribute('data-moderate')).toBe(kind);
  }
  async function submit() {
    await act(async () => panel()!.querySelector<HTMLButtonElement>('.moderate-send')!.click());
    await act(() => new Promise((r) => setTimeout(r, 5)));
  }
  const header = (o: Partial<ThreadHeader>): ThreadHeader => ({ ...at().thread, log_length: len + 1, ...o });
  const pinFrom = (b: unknown) => b as { scope: string; until: number | null };

  it('a moderator sees the moderation actions', async () => {
    pages = [at({ thread: { pinned: { scope: 'forum', until: null } } })];
    let root = await renderAt(`/t/${id}/a`);
    expect(offered(root)).toEqual(['lock', 'pin', 'unpin']);

    // A member who moderates the forum through a group sees them too.
    as('member');
    root = await renderAt(`/t/${id}/a`);
    expect(offered(root)).toEqual(['lock', 'pin', 'unpin']);

    // And on a phone they are in the top bar's more menu.
    isPhone.value = true;
    root = await renderAt(`/t/${id}/a`);
    await act(async () => root.querySelector<HTMLButtonElement>('.more > button')!.click());
    const items = [...root.querySelectorAll('[data-shell=more-menu] [data-action]')].map((b) => b.getAttribute('data-action'));
    expect(items).toEqual(expect.arrayContaining(['lock', 'pin', 'unpin']));
    isPhone.value = false;

    // A member who does not moderate it sees none, nor does a moderator by
    // role whom the page says does not moderate this forum.
    for (const role of ['member', 'moderator'] as const) {
      as(role);
      pages = [at({ viewer: { can_moderate: false }, thread: { pinned: { scope: 'forum', until: null } } })];
      root = await renderAt(`/t/${id}/a`);
      expect(offered(root), role).toEqual([]);
      expect(root.querySelector('[data-shell=toolbar] [data-action=copy-link]')).not.toBeNull();
    }
  });

  it('a group moderator is not offered a global pin', async () => {
    as('member');
    let root = await renderAt(`/t/${id}/a`);
    await open(root, 'pin');
    expect(panel()!.querySelector('input[value=forum]')).not.toBeNull();
    expect(panel()!.querySelector('input[value=global]')).toBeNull();

    // Over an active global pin, neither Pin nor Unpin.
    pages = [at({ thread: { pinned: { scope: 'global', until: null } } })];
    root = await renderAt(`/t/${id}/a`);
    expect(offered(root)).toEqual(['lock']);

    as('moderator');
    root = await renderAt(`/t/${id}/a`);
    expect(offered(root)).toEqual(['lock', 'pin', 'unpin']);
    await open(root, 'pin');
    expect(panel()!.querySelector('input[value=global]')).not.toBeNull();
  });

  it('lock sends the body and shows the lock at once', async () => {
    let release!: (p: ThreadPage) => void;
    pages = [at(), new Promise<ThreadPage>((r) => (release = r))];
    moderate.lock = () => ({ status: 200, body: header({ locked: true }), headers: { 'Cache-Control': 'no-store' } });
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-thread-state=locked]')).toBeNull();
    const before = gets();
    await open(root, 'lock');
    const reason = panel()!.querySelector('textarea')!;
    expect(document.activeElement).toBe(reason);
    expect(reason.maxLength).toBe(500);
    reason.value = 'off topic';
    await act(async () => void reason.dispatchEvent(new Event('input', { bubbles: true })));
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/lock`, body: { reason: 'off topic' } }]);
    // The answer's header shows before the page is answered again.
    expect(gets()).toBe(before + 1);
    expect(root.querySelector('[data-thread-state=locked]')?.textContent).toBe('This thread is locked.');
    expect(offered(root)).toEqual(['unlock', 'pin']);
    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('h1'));
    await act(async () => release(at({ thread: { locked: true, log_length: len + 1 } })));
    await settle(root);
    expect(root.querySelector('[data-thread-state=locked]')).not.toBeNull();
    expect(root.querySelectorAll('article.post').length).toBeGreaterThan(0);
  });

  it('unlock with no reason sends an empty body', async () => {
    pages = [at({ thread: { locked: true } })];
    moderate.unlock = () => ({ status: 200, body: header({ locked: false }) });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'unlock');
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/unlock`, body: {} }]);
    expect(root.querySelector('[data-thread-state=locked]')).toBeNull();
  });

  it('pin sends scope and until', async () => {
    const local = '2030-01-02T03:04';
    const ms = new Date(local).getTime();
    moderate.pin = (b) => ({ status: 200, body: header({ pinned: pinFrom(b) }) });
    let root = await renderAt(`/t/${id}/a`);
    await open(root, 'pin');
    expect(panel()!.querySelector<HTMLInputElement>('input[value=forum]')!.checked).toBe(true);
    await act(async () => panel()!.querySelector<HTMLInputElement>('input[value=global]')!.click());
    const until = panel()!.querySelector<HTMLInputElement>('input[type=datetime-local]')!;
    until.value = local;
    await act(async () => void until.dispatchEvent(new Event('input', { bubbles: true })));
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/pin`, body: { scope: 'global', until: ms } }]);
    const note = root.querySelector('[data-thread-state=pinned]')!;
    expect(note.getAttribute('data-pin-scope')).toBe('global');
    expect(note.textContent).toMatch(/^Pinned on every forum until .+\.$/);
    expect(note.querySelector('time')?.getAttribute('datetime')).toBe(new Date(ms).toISOString());

    server.sent.length = 0;
    root = await renderAt(`/t/${id}/a`);
    await open(root, 'pin');
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/pin`, body: { scope: 'forum', until: null } }]);
    expect(root.querySelector('[data-thread-state=pinned]')?.textContent).toBe('Pinned in this forum.');
  });

  it('pins in the forum for a group moderator', async () => {
    as('member');
    moderate.pin = (b) => ({ status: 200, body: header({ pinned: pinFrom(b) }) });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'pin');
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/pin`, body: { scope: 'forum', until: null } }]);
    expect(root.querySelector('[data-thread-state=pinned]')?.textContent).toBe('Pinned in this forum.');
  });

  it('pins in the forum once the viewer is no longer staff', async () => {
    moderate.pin = (b) => ({ status: 200, body: header({ pinned: pinFrom(b) }) });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'pin');
    await act(async () => panel()!.querySelector<HTMLInputElement>('input[value=global]')!.click());
    // /me is fetched again, and the role has gone while the panel was open.
    await act(async () => as('member'));
    expect(panel()!.querySelector('input[value=global]')).toBeNull();
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/pin`, body: { scope: 'forum', until: null } }]);
  });

  it('leaves the panel behind on another thread', async () => {
    const other = '01k4z3j0w8x2d';
    server.routes[`GET /api/v1/threads/${other}`] = () => ({
      status: 200,
      body: { ...at(), thread: { ...at().thread, id: other, slug: 'b' } },
    });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'lock');
    await route(`/t/${other}/b`);
    await settle(root);
    expect(root.querySelector('h1')?.textContent).toBe('Moderated');
    expect(offered(root)).toEqual(['lock', 'pin']);
    expect(panel()).toBeNull();
    await route(`/t/${id}/a`);
    await settle(root);
    expect(panel()).toBeNull();
  });

  it('unpin sends the active scope', async () => {
    pages = [at({ thread: { pinned: { scope: 'global', until: null } } })];
    moderate.unpin = () => ({ status: 200, body: header({ pinned: null }) });
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-thread-state=pinned]')?.textContent).toBe('Pinned on every forum.');
    await open(root, 'unpin');
    expect(panel()!.querySelector('h2')?.textContent).toBe('Unpin this thread?');
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/unpin`, body: { scope: 'global' } }]);
    expect(root.querySelector('[data-thread-state=pinned]')).toBeNull();
  });

  it('a conflict keeps the panel open', async () => {
    // Another moderator locked it first: the page asked for again says so,
    // and the panel keeps its words and drops its Lock.
    pages = [at(), at({ thread: { locked: true, log_length: len + 1 } })];
    moderate.lock = () => problemAnswer('conflict', 409);
    const root = await renderAt(`/t/${id}/a`);
    const before = gets();
    await open(root, 'lock');
    await submit();
    await settle(root);
    expect(gets()).toBe(before + 1);
    expect(panel()?.querySelector('[role=alert]')?.textContent).toBe('This thread changed while you were working.');
    expect(root.querySelector('[data-thread-state=locked]')).not.toBeNull();
    expect(panel()!.querySelector('.moderate-send')).toBeNull();
    expect(panel()!.querySelector('textarea')).toBeNull();
    expect(document.activeElement).toBe(panel()!.querySelector('[data-moderate-close]'));
    await act(async () => panel()!.querySelector<HTMLButtonElement>('[data-moderate-close]')!.click());
    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('h1'));
  });

  it('keeps the form after a conflict that leaves the action offered', async () => {
    moderate.lock = () => problemAnswer('conflict', 409);
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'lock');
    await submit();
    await settle(root);
    expect(panel()?.querySelector('[role=alert]')?.textContent).toBe('This thread changed while you were working.');
    expect(panel()!.querySelector('.moderate-send')).not.toBeNull();
  });

  it('a refusal says the viewer can no longer moderate, and the actions go', async () => {
    as('member');
    pages = [at(), at({ viewer: { can_moderate: false } })];
    moderate.lock = () => problemAnswer('forbidden', 403);
    const root = await renderAt(`/t/${id}/a`);
    const before = gets();
    await open(root, 'lock');
    await submit();
    await settle(root);
    expect(gets()).toBe(before + 1);
    expect(panel()?.querySelector('[role=alert]')?.textContent).toBe('You can no longer moderate this thread.');
    expect(panel()!.querySelector('.moderate-send')).toBeNull();
    expect(offered(root)).toEqual([]);
  });

  it('Escape and Cancel close the panel and send nothing', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'lock');
    await act(
      async () =>
        void document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })),
    );
    expect(panel()).toBeNull();
    await open(root, 'pin');
    await act(async () => panel()!.querySelector<HTMLButtonElement>('[data-moderate-close]')!.click());
    expect(panel()).toBeNull();
    expect(posts()).toEqual([]);
    expect(document.activeElement).toBe(root.querySelector('h1'));
  });

  it("a locked thread closes a member's composer", async () => {
    as('member');
    pages = [at({ thread: { locked: true }, viewer: { can_moderate: false, can_reply: false } })];
    let root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-thread-state=locked]')?.textContent).toBe('This thread is locked.');
    expect(root.querySelector('[data-composer]')).toBeNull();
    expect(offered(root)).toEqual([]);

    pages = [at({ thread: { locked: true }, viewer: { can_moderate: true, can_reply: true } })];
    root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-composer]')).not.toBeNull();
    expect(offered(root)).toEqual(['unlock', 'pin']);
  });

  it('offers nothing on the post history route', async () => {
    server.routes[`GET ${url}?ordinal=61`] = () => ({ status: 200, body: at() });
    const root = await renderAt(`/t/${id}/a/61/history`);
    expect(screenOf(root)).toBe('post-history');
    expect(offered(root)).toEqual([]);
    expect(root.querySelector('[data-action=slow-mode]')).toBeNull();
    expect(named(root)).toEqual([]);
  });

  // TKT-01M3QXV399: rename and move. Rename is offered on can_moderate and
  // to the author inside rename_until; Move on can_moderate, whatever the
  // role.
  const named = (root: HTMLElement) =>
    [...root.querySelectorAll('[data-shell=toolbar] [data-action]')]
      .map((b) => b.getAttribute('data-action'))
      .filter((a) => a === 'rename' || a === 'move');
  const field = () => panel()!.querySelector<HTMLInputElement>('input[type=text]')!;
  const select = () => panel()!.querySelector<HTMLSelectElement>('select')!;
  const alert = () => panel()?.querySelector('[role=alert]')?.textContent;
  const sendDisabled = () => panel()!.querySelector<HTMLButtonElement>('.moderate-send')!.disabled;
  async function type(value: string) {
    field().value = value;
    await act(async () => void field().dispatchEvent(new Event('input', { bubbles: true })));
  }
  async function choose(slug: string) {
    select().value = slug;
    await act(async () => void select().dispatchEvent(new Event('change', { bubbles: true })));
  }
  const forum = (slug: string, o: Partial<Forum> = {}): Forum => ({
    slug,
    name: slug[0].toUpperCase() + slug.slice(1),
    description: '',
    visibility: 'public',
    archived: false,
    thread_count: 0,
    post_count: 0,
    unread: 0,
    last_post: null,
    children: [],
    ...o,
  });
  const tree = () => [
    forum('general', { children: [forum('introductions'), forum('attic', { archived: true })] }),
    forum('lounge'),
    forum('archive', { archived: true, children: [forum('reading')] }),
  ];
  const listed = () => [...select().options].slice(1).map((o) => o.value);

  it('a moderator is offered rename and move', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(named(root)).toEqual(['rename', 'move']);
  });

  it('a group moderator is offered rename and move', async () => {
    as('member');
    let root = await renderAt(`/t/${id}/a`);
    expect(named(root)).toEqual(['rename', 'move']);
    // A member who does not moderate the forum is offered neither, outside
    // an author's window.
    pages = [at({ viewer: { can_moderate: false, rename_until: null } })];
    root = await renderAt(`/t/${id}/a`);
    expect(named(root)).toEqual([]);
  });

  it('the author is offered rename while the window is open', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      as('member');
      pages = [at({ viewer: { can_moderate: false, rename_until: Date.now() + 60_000 } })];
      let root = await renderAt(`/t/${id}/a`);
      expect(named(root)).toEqual(['rename']);
      await act(async () => void vi.advanceTimersByTime(60_000));
      expect(named(root)).toEqual([]);

      pages = [at({ viewer: { can_moderate: false, rename_until: null } })];
      root = await renderAt(`/t/${id}/a`);
      expect(named(root)).toEqual([]);

      // Forty days ahead is past what setTimeout keeps, which a browser fires
      // at once: no timer is set for longer, and the one set fires early, is
      // set again, and does not withdraw Rename.
      const timers = vi.spyOn(globalThis, 'setTimeout');
      pages = [at({ viewer: { can_moderate: false, rename_until: Date.now() + 40 * 86_400_000 } })];
      root = await renderAt(`/t/${id}/a`);
      await act(async () => void vi.advanceTimersByTime(2_147_483_647));
      expect(named(root)).toEqual(['rename']);
      const delays = timers.mock.calls.map(([, d]) => d ?? 0);
      expect(delays).toContain(2_147_483_647);
      expect(delays.filter((d) => d > 2_147_483_647)).toEqual([]);
      timers.mockRestore();
    } finally {
      vi.useRealTimers();
    }
  });

  it('rename sends the title and follows the new slug', async () => {
    const renamed = { title: 'New title', slug: 'new-title' };
    pages = [at(), at({ thread: { ...renamed, log_length: len + 1 } })];
    moderate.rename = () => ({ status: 200, body: header(renamed), headers: { 'Cache-Control': 'no-store' } });
    const root = await renderAt(`/t/${id}/a/61?lens=x#post-61`);
    await open(root, 'rename');
    expect(document.activeElement).toBe(field());
    expect(field().value).toBe('Moderated');
    // The title it already has, an empty one, and one over 200 characters
    // are not sent.
    expect(sendDisabled()).toBe(true);
    await type('   ');
    expect(sendDisabled()).toBe(true);
    await type('é'.repeat(201));
    expect(sendDisabled()).toBe(true);
    await type('😀'.repeat(200));
    expect(sendDisabled()).toBe(false);
    await type('  New title ');
    await submit();
    expect(server.sent.filter((r) => r.method === 'PATCH')).toEqual([{ method: 'PATCH', url, body: { title: 'New title' } }]);
    expect(root.querySelector('h1')?.textContent).toBe('New title');
    expect(location.pathname + location.search + location.hash).toBe(`/t/${id}/new-title/61?lens=x#post-61`);
    expect(panel()).toBeNull();
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/new-title/61`);
  });

  it('move lists the forums and follows the header', async () => {
    forums = tree();
    const lounge = { slug: 'lounge', name: 'Lounge' };
    moderate.move = () => ({ status: 200, body: header({ forum: lounge }) });
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('.thread-head .forum')?.textContent).toBe('General');
    await open(root, 'move');
    await settle(root);
    // Not the thread's own forum, and no archived forum to a moderator; an
    // archived forum's open child is still listed.
    expect(listed()).toEqual(['introductions', 'lounge', 'reading']);
    expect(select().options[1].textContent).toBe('\u2003Introductions');
    expect(sendDisabled()).toBe(true);
    expect(peekGet('/api/v1/forums')).toBeDefined();
    await choose('lounge');
    await submit();
    expect(posts()).toEqual([{ url: `/api/v1/admin/threads/${id}/move`, body: { forum: 'lounge' } }]);
    expect(root.querySelector('.thread-head .forum')?.textContent).toBe('Lounge');
    expect(panel()).toBeNull();
    // The forum index and thread lists it changed are asked for again.
    expect(peekGet('/api/v1/forums')).toBeUndefined();
  });

  it('an admin may move into an archived forum', async () => {
    forums = tree();
    as('admin');
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'move');
    await settle(root);
    expect(listed()).toEqual(['introductions', 'attic', 'lounge', 'archive', 'reading']);
  });

  it('refusals keep the panel open', async () => {
    forums = tree();
    const renameWith = async (type_: string, status: number, words: string) => {
      moderate.rename = () => problemAnswer(type_, status);
      await type(`Retitled ${type_}`);
      await submit();
      expect(alert(), type_).toBe(words);
      expect(field().value).toBe(`Retitled ${type_}`);
    };
    let root = await renderAt(`/t/${id}/a`);
    await open(root, 'rename');
    await renameWith('conflict', 409, 'This thread changed while you were working, or is already there.');
    await renameWith('validation-failed', 422, 'Enter a title of 1 to 200 characters.');

    await act(async () => panel()!.querySelector<HTMLButtonElement>('[data-moderate-close]')!.click());
    await open(root, 'move');
    await settle(root);
    moderate.move = () => problemAnswer('forbidden', 403);
    await choose('lounge');
    await submit();
    await settle(root);
    expect(alert()).toBe('You cannot move this thread there.');
    expect(select().value).toBe('lounge');
    moderate.move = () => problemAnswer('not-found', 404);
    await submit();
    expect(alert()).toBe('That forum is no longer there.');

    // The author past a window this clock thinks is open: the catalogue's
    // words, and Rename is withdrawn from the actions.
    as('member');
    pages = [at({ viewer: { can_moderate: false, rename_until: Date.now() + 60_000 } })];
    root = await renderAt(`/t/${id}/a`);
    await open(root, 'rename');
    await renameWith('rename-window-closed', 403, 'The time for renaming this thread has passed.');
    expect(named(root)).toEqual([]);
    // The open panel is withdrawn too (Terva review 1589): the title stays,
    // and Send, which would only be refused again, is disabled.
    expect(sendDisabled()).toBe(true);
  });

  // Terva review 1588: a refusal whose fresh page withdraws the author's
  // window (reply taken away, or the thread locked) keeps the typed title,
  // with Send disabled, rather than dropping the form.
  it("a refused author's rename keeps the title when the window is withdrawn", async () => {
    as('member');
    pages = [
      at({ viewer: { can_moderate: false, rename_until: Date.now() + 60_000 } }),
      at({ viewer: { can_moderate: false, rename_until: null } }),
    ];
    moderate.rename = () => problemAnswer('forbidden', 403);
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'rename');
    await type('Mine, retitled');
    await submit();
    await settle(root);
    expect(named(root)).toEqual([]);
    expect(alert()).toBeTruthy();
    expect(field().value).toBe('Mine, retitled');
    expect(sendDisabled()).toBe(true);
    expect(document.activeElement).toBe(panel()!.querySelector('[data-moderate-close]'));
    expect(panel()!.querySelector('[data-moderate-close]')?.textContent).toBe('Close');
  });

  // TKT-01M3R0PM0K: slow mode, PUT …/slow-mode, in the same panel.
  const puts = () => server.sent.filter((r) => r.method === 'PUT').map((r) => ({ url: r.url, body: r.body }));
  const slowUrl = `/api/v1/admin/threads/${id}/slow-mode`;
  const checked = () => panel()!.querySelector<HTMLInputElement>('input[type=radio]:checked')?.value;
  const pick = (v: string) => act(async () => panel()!.querySelector<HTMLInputElement>(`input[value="${v}"]`)!.click());
  async function custom(v: string) {
    const f = panel()!.querySelector<HTMLInputElement>('input[inputmode=numeric]')!;
    f.value = v;
    await act(async () => void f.dispatchEvent(new Event('input', { bubbles: true })));
  }
  const notice = (root: HTMLElement) => root.querySelector('[data-slow-mode]')?.textContent ?? null;

  it('a moderator sets slow mode', async () => {
    pages = [at({ viewer: { slow_mode_seconds: 30 } }), at({ viewer: { slow_mode_seconds: 300 } })];
    moderate['slow-mode'] = (b) => ({ status: 200, body: b, headers: { 'Cache-Control': 'no-store' } });
    const root = await renderAt(`/t/${id}/a`);
    expect(notice(root)).toBe('Slow mode: one post every 30 seconds');
    const item = root.querySelector('[data-shell=toolbar] [data-action=slow-mode]')!;
    expect(item.textContent).toBe('Slow mode…');
    const before = gets();
    await open(root, 'slow-mode');
    expect(checked()).toBe('30');
    expect([...panel()!.querySelectorAll('label')].map((l) => l.textContent)).toEqual([
      'Off',
      '10 seconds',
      '30 seconds',
      '1 minute',
      '5 minutes',
      'Custom',
    ]);
    await pick('300');
    await submit();
    await settle(root);
    expect(puts()).toEqual([{ url: slowUrl, body: { seconds: 300 } }]);
    expect(posts()).toEqual([]);
    expect(gets()).toBe(before + 1);
    expect(panel()).toBeNull();
    expect(notice(root)).toBe('Slow mode: one post every 5 minutes');
    expect(document.activeElement).toBe(root.querySelector('h1'));
  });

  it('a group moderator sets slow mode', async () => {
    as('member');
    pages = [at({ thread: { locked: true, pinned: { scope: 'global', until: null } }, viewer: { slow_mode_seconds: 0 } })];
    moderate['slow-mode'] = (b) => ({ status: 200, body: b });
    const root = await renderAt(`/t/${id}/a`);
    // Offered whatever the thread's state, where lock and pin are not all.
    expect(offered(root)).toEqual(['unlock']);
    await open(root, 'slow-mode');
    expect(checked()).toBe('0');
    await pick('custom');
    expect(panel()!.querySelector<HTMLInputElement>('input[inputmode=numeric]')!.value).toBe('0');
    await custom('45');
    await submit();
    expect(puts()).toEqual([{ url: slowUrl, body: { seconds: 45 } }]);
    expect(panel()).toBeNull();

    // And from a phone's more menu.
    server.sent.length = 0;
    isPhone.value = true;
    const phone = await renderAt(`/t/${id}/a`);
    await act(async () => phone.querySelector<HTMLButtonElement>('.more > button')!.click());
    await act(async () => phone.querySelector<HTMLButtonElement>('[data-shell=more-menu] [data-action=slow-mode]')!.click());
    expect(panel()?.getAttribute('data-moderate')).toBe('slow-mode');
    await pick('10');
    await submit();
    expect(puts()).toEqual([{ url: slowUrl, body: { seconds: 10 } }]);
  });

  it('a member is not offered slow mode', async () => {
    for (const role of ['member', 'moderator', 'admin'] as const) {
      as(role);
      pages = [at({ viewer: { can_moderate: false, slow_mode_seconds: 30 } })];
      const root = await renderAt(`/t/${id}/a`);
      expect(root.querySelector('[data-action=slow-mode]'), role).toBeNull();
      // The notice is every reader's.
      expect(notice(root), role).toBe('Slow mode: one post every 30 seconds');
    }
  });

  it('slow mode refusals stay beside the field', async () => {
    let answer: Answer = problemAnswer('conflict', 409);
    moderate['slow-mode'] = () => answer;
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'slow-mode');
    await pick('300');
    for (const [a, says] of [
      [problemAnswer('conflict', 409), 'Slow mode is already set to that, or the thread is archived.'],
      [problemAnswer('validation-failed', 422), 'Enter a whole number of seconds from 0 to 86400.'],
      [problemAnswer('forbidden', 403), 'You can no longer moderate this thread.'],
    ] as const) {
      answer = a;
      await submit();
      await settle(root);
      expect(panel()?.querySelector('[role=alert]')?.textContent, says).toBe(says);
      expect(checked()).toBe('300');
    }
    expect(puts()).toHaveLength(3);

    // A Custom value the server would refuse is refused here.
    await pick('custom');
    for (const v of ['86401', '-1', '1.5', 'ten', '']) {
      await custom(v);
      await submit();
      expect(panel()?.querySelector('[role=alert]')?.textContent, v).toBe('Enter a whole number of seconds from 0 to 86400.');
    }
    expect(puts()).toHaveLength(3);
  });

  it('prefills Custom with a site default past what a thread takes, and refuses it', async () => {
    pages = [at({ viewer: { slow_mode_seconds: 100_000 } })];
    moderate['slow-mode'] = (b) => ({ status: 200, body: b });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'slow-mode');
    expect(checked()).toBe('custom');
    expect(panel()!.querySelector<HTMLInputElement>('input[inputmode=numeric]')!.value).toBe('100000');
    await submit();
    expect(puts()).toEqual([]);
    expect(panel()?.querySelector('[role=alert]')?.textContent).toBe('Enter a whole number of seconds from 0 to 86400.');
    await custom('86400');
    await submit();
    expect(puts()).toEqual([{ url: slowUrl, body: { seconds: 86400 } }]);
  });
});

// TKT-01M3KFFFJR: Mark unread, for the whole thread from the thread actions
// and from a post with unread from here, then back to the list. The golden
// thread is in General, and the member golden's viewer, alice, has read it
// to #60.
describe('marking unread from the thread screen', () => {
  const saved = viewer.value;
  const id = '01k4z3j0w8x2c';
  const url = `/api/v1/threads/${id}`;
  const read = `${url}/read`;
  const list = '/api/v1/forums/general/threads';
  let page: ThreadPage;
  let unread: (body: unknown) => Answer | Promise<Answer>;
  let lists: (() => Answer | Promise<Answer>)[];
  let server: StubServer;

  beforeEach(() => {
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        disconnect() {}
      },
    );
    page = threadPage({ id, slug: 'a', title: 'Marked' });
    unread = (b) => ({ status: 200, body: { last_read_ordinal: (b as { ordinal?: number } | undefined)?.ordinal ?? 0 } });
    lists = [];
    const listAnswer = () => (lists.length > 1 ? lists.shift()! : (lists[0] ?? (() => ({ status: 200, body: threads, headers: { ETag: '"l"' } }))))();
    server = stubServer({
      [`GET ${url}`]: () => ({ status: 200, body: page }),
      [`GET ${url}?ordinal=62`]: () => ({ status: 200, body: page }),
      [`DELETE ${read}`]: (b) => unread(b),
      'GET /api/v1/me': () => ({ status: 200, body: member }),
      'GET /api/v1/forums': () => ({ status: 200, body: { forums: [] }, headers: { ETag: '"f"' } }),
      [`GET ${list}`]: listAnswer,
      [`GET ${list}?sort=created`]: listAnswer,
    });
    viewer.value = structuredClone(member) as Me;
  });
  afterEach(async () => {
    await unmount();
    viewer.value = saved;
    isPhone.value = false;
    resetReadTracker();
    resetLists();
  });

  const deletes = () => server.sent.filter((r) => r.method === 'DELETE').map((r) => ({ url: r.url, body: r.body }));
  const refused = (root: HTMLElement) => root.querySelector('[data-mark-unread-refused]');
  const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
  async function press(el: Element | null) {
    expect(el).not.toBeNull();
    await act(async () => (el as HTMLElement).click());
    await wait();
  }

  it('Mark unread sends no body, drops the held counts and goes back to the list the reader came from', async () => {
    const root = await renderAt('/');
    await route('/f/general?sort=created');
    await settle(root);
    expect(peekGet('/api/v1/forums')).toBeDefined();
    expect(peekGet(`${list}?sort=created`)).toBeDefined();
    await route(`/t/${id}/a`);
    await settle(root);
    expect(screenOf(root)).toBe('thread');
    // The list, asked for again, is held back so the test can see that
    // nothing was left to show in its place.
    lists = [() => new Promise<Answer>(() => {})];
    const mes = server.sent.filter((r) => r.url === '/api/v1/me').length;
    const entries = history.length;
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    expect(deletes()).toEqual([{ url: read, body: undefined }]);
    expect(location.pathname + location.search).toBe('/f/general?sort=created');
    expect(history.length, 'the thread entry is replaced, so Back does not reopen it').toBe(entries);
    expect(peekGet('/api/v1/forums')).toBeUndefined();
    expect(peekGet(`${list}?sort=created`)).toBeUndefined();
    expect(server.sent.filter((r) => r.url === '/api/v1/me').length).toBe(mes + 1);
  });

  it("goes to the thread's forum when the list on record is another forum's", async () => {
    const root = await renderAt('/f/garden');
    await route(`/t/${id}/a`);
    await settle(root);
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    expect(location.pathname + location.search).toBe('/f/general');
    expect(screenOf(root)).toBe('thread-list');
  });

  it('unread from here sends the ordinal before the post, from the footer', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect([...root.querySelectorAll('[data-post-action=unread]')].map((b) => b.closest('article')?.id)).toEqual([
      'post-61',
      'post-62',
    ]);
    await press(root.querySelector('#post-62 [data-post-action=unread]'));
    expect(deletes()).toEqual([{ url: read, body: { ordinal: 61 } }]);
    expect(location.pathname).toBe('/f/general');
  });

  it("marks unread from the phone's more menu and from a post's More", async () => {
    isPhone.value = true;
    let root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('.post-footer > [data-post-action=unread]')).toBeNull();
    await press(root.querySelector('#post-62 [data-post-more]'));
    const item = root.querySelector<HTMLButtonElement>('#post-62 .post-more-list [data-post-action=unread]');
    await press(item);
    expect(deletes()).toEqual([{ url: read, body: { ordinal: 61 } }]);
    expect(location.pathname).toBe('/f/general');

    root = await renderAt(`/t/${id}/a`);
    await press(root.querySelector('[data-shell=phone-topbar] button[aria-label=More]'));
    await press(root.querySelector('[data-shell=more-menu] [data-action=mark-unread]'));
    expect(deletes().at(-1)).toEqual({ url: read, body: undefined });
    expect(location.pathname).toBe('/f/general');
  });

  it('stays on the page and says so when the mark fails, and a second press marks it', async () => {
    unread = () => problemAnswer('internal', 500);
    const root = await renderAt(`/t/${id}/a`);
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    expect(screenOf(root)).toBe('thread');
    expect(location.pathname).toBe(`/t/${id}/a`);
    expect(refused(root)?.getAttribute('role')).toBe('alert');
    expect(refused(root)?.textContent).toContain('This thread could not be marked unread.');

    await press(root.querySelector('#post-62 [data-post-action=unread]'));
    expect(location.pathname).toBe(`/t/${id}/a`);
    expect(root.querySelector('#post-62 [data-mark-unread-refused]')?.textContent).toContain(
      'could not be marked unread from here',
    );
    expect(root.querySelector<HTMLButtonElement>('#post-62 [data-post-action=unread]')!.disabled).toBe(false);

    unread = () => ({ status: 200, body: { last_read_ordinal: 0 } });
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    expect(deletes()).toHaveLength(3);
    expect(location.pathname).toBe('/f/general');
  });

  it('takes one press at a time while a mark is out', async () => {
    let release!: () => void;
    unread = () => new Promise<Answer>((r) => (release = () => r({ status: 200, body: { last_read_ordinal: 61 } })));
    const root = await renderAt(`/t/${id}/a`);
    const button = root.querySelector<HTMLButtonElement>('#post-62 [data-post-action=unread]')!;
    await press(button);
    expect(button.disabled).toBe(true);
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    // The post's mark is out, and the thread's waits for it (marks of a
    // thread run in call order); its second press was ignored.
    expect(deletes()).toEqual([{ url: read, body: { ordinal: 61 } }]);
    await act(async () => release());
    await wait();
    // Review 1619: the first mark has landed, but the second is still to,
    // so the page stays and no list is asked for with counts about to change.
    expect(location.pathname).toBe(`/t/${id}/a`);
    expect(server.sent.filter((r) => r.url.startsWith(list))).toEqual([]);
    expect(deletes()).toEqual([
      { url: read, body: { ordinal: 61 } },
      { url: read, body: undefined },
    ]);
    await act(async () => release());
    await wait();
    expect(location.pathname).toBe('/f/general');
    expect(server.sent.filter((r) => r.url.startsWith(list))).toHaveLength(1);
  });

  // Review 1623: an earlier mark that succeeded has lowered the position, so
  // the reader returns to the list when the last queued mark settles, even
  // if that one failed.
  it('returns once the last queued mark settles if an earlier one succeeded', async () => {
    const answers: ((a: Answer) => void)[] = [];
    unread = () => new Promise<Answer>((r) => answers.push(r));
    const root = await renderAt(`/t/${id}/a`);
    await press(root.querySelector('#post-62 [data-post-action=unread]'));
    await press(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]'));
    await act(async () => answers.shift()!({ status: 200, body: { last_read_ordinal: 61 } }));
    await wait();
    expect(location.pathname).toBe(`/t/${id}/a`);
    await act(async () => answers.shift()!(problemAnswer('internal', 500)));
    await wait();
    expect(deletes()).toHaveLength(2);
    expect(location.pathname).toBe('/f/general');
  });

  // Review 1622: a reader who leaves the thread's posts while the DELETE is
  // out, for a post's history or another screen, stays where they went.
  it.each([
    ['a post’s history', `/t/${id}/a/62/history`],
    ['another screen', '/f/garden'],
  ])('leaves a reader who went to %s where they are', async (_, to) => {
    let release!: () => void;
    unread = () => new Promise<Answer>((r) => (release = () => r({ status: 200, body: { last_read_ordinal: 61 } })));
    const root = await renderAt(`/t/${id}/a`);
    await press(root.querySelector('#post-62 [data-post-action=unread]'));
    await route(to);
    await settle(root);
    await act(async () => release());
    await wait();
    expect(deletes()).toEqual([{ url: read, body: { ordinal: 61 } }]);
    expect(location.pathname).toBe(to);
  });

  it('is offered to a member who may not reply, and to nobody signed out', async () => {
    page = threadPage({ id, slug: 'a', title: 'Marked' }, { viewer: { ...page.viewer, can_reply: false } });
    let root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-post-action=reply]')).toBeNull();
    expect(root.querySelector('#post-62 [data-post-action=unread]')).not.toBeNull();
    expect(root.querySelector('[data-shell=toolbar] [data-action=mark-unread]')).not.toBeNull();

    viewer.value = { ...structuredClone(member), user: null } as Me;
    root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('#post-62')).not.toBeNull();
    expect(root.querySelector('[data-post-action=unread]')).toBeNull();
    expect(root.querySelector('[data-action=mark-unread]')).toBeNull();
  });
});
