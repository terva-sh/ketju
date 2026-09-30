import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ThreadHeader, ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
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
    const routes: StubServer['routes'] = {
      [`GET ${url}`]: async () => ({ status: 200, body: await (pages.length > 1 ? pages.shift()! : pages[0]) }),
    };
    for (const kind of ['lock', 'unlock', 'pin', 'unpin'])
      routes[`POST /api/v1/admin/threads/${id}/${kind}`] = (b) => moderate[kind]?.(b) ?? problemAnswer('internal', 500);
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
  });
});
