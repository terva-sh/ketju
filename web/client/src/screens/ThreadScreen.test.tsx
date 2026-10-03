import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { peekGet } from '../api/client';
import type { Forum, Me, Post, ThreadHeader, ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
import { resetLists } from '../state/lists';
import { dwellMs, resetReadTracker } from '../state/readTracker';
import { viewer } from '../state/viewer';
import {
  FakeEventSource,
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

  // The thread head (docs/06 "The thread head", TKT-01M3S3ZF6J).
  const headAt = async (id: string, over: Partial<ThreadHeader> = {}) => {
    vi.stubGlobal('fetch', async (url: string) => {
      if (url.startsWith('/api/v1/drafts/')) return new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
      return new Response(JSON.stringify(threadPage({ id, slug: 'a', title: 'Head' }, { thread: over as ThreadHeader })), {
        status: 200,
      });
    });
    const root = await renderAt(`/t/${id}/a`);
    return root.querySelector('.thread-head')!;
  };

  it('the head names the starter, the time and the post count', async () => {
    const created = Date.UTC(2025, 8, 10, 9, 30);
    const head = await headAt('01k4z3j0w8x3a', { created_at: created, post_count: 212 });
    const meta = head.querySelector('.thread-meta')!;
    expect(meta.querySelector('.starter')?.textContent).toBe('alice');
    const time = meta.querySelector('time')!;
    expect(time.getAttribute('datetime')).toBe(new Date(created).toISOString());
    expect(meta.textContent).toBe(`started by alice · ${time.textContent} · 212 posts`);
    // Under the title.
    expect(head.querySelector('h1 + .thread-meta')).toBe(meta);
  });

  it('the breadcrumb links the front page and the forum', async () => {
    const head = await headAt('01k4z3j0w8x3b');
    const crumb = head.querySelector('nav.breadcrumb')!;
    expect(crumb.getAttribute('aria-label')).toBe('Breadcrumb');
    expect(crumb.nextElementSibling?.tagName).toBe('H1');
    const links = [...crumb.querySelectorAll('a')].map((a) => [a.textContent, a.getAttribute('href')]);
    expect(links).toEqual([
      ['Forums', '/'],
      ['General', '/f/general'],
    ]);
  });

  it('one post reads 1 post', async () => {
    const head = await headAt('01k4z3j0w8x3c', { post_count: 1 });
    expect(head.querySelector('.thread-meta')?.textContent).toMatch(/ · 1 post$/);
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

    // The read tracker's observers are those watching posts; the screen's
    // own watches the foot of the list for the new-posts pill.
    const trackers = () => observers.filter((o) => o.observed.some((el) => el.id.startsWith('post-')));
    viewer.value = { ...structuredClone(member), user: null } as Me;
    await renderAt('/t/01k4z3j0w8x2c/a');
    expect(trackers()).toHaveLength(0);

    viewer.value = structuredClone(member) as Me;
    await renderAt('/t/01k4z3j0w8x2c/a');
    const io = trackers().at(-1)!;
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
    routes[`PUT /api/v1/admin/threads/${id}/tags`] = (b) => moderate.tags?.(b) ?? problemAnswer('internal', 500);
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

  // The panel is drawn on the microtask after the press that opens it, and
  // must have focus from then on, before a frame has passed, so an Escape
  // pressed straight away reaches it (TKT-01M3TS0ZV9). The presses are
  // outside act, which would run the effects and hide the race.
  it('an Escape pressed straight after opening closes the panel', async () => {
    const root = await renderAt(`/t/${id}/a`);
    const opener = root.querySelector<HTMLButtonElement>('[data-shell=toolbar] [data-action=lock]')!;
    opener.focus();
    try {
      opener.click();
      await Promise.resolve();
      expect(panel()).not.toBeNull();
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      await Promise.resolve();
      expect(panel()).toBeNull();
      expect(posts()).toEqual([]);
      expect(document.activeElement).toBe(root.querySelector('h1'));
    } finally {
      await act(() => new Promise((r) => setTimeout(r, 50)));
    }
  });

  // The panel is inline, not modal: an Escape with focus elsewhere in the
  // page is not its to take.
  it('an Escape outside the panel leaves it open', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'lock');
    const post = root.querySelector<HTMLElement>('[id^=post-]')!;
    post.focus();
    expect(document.activeElement).toBe(post);
    await act(async () => void post.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    expect(panel()?.getAttribute('data-moderate')).toBe('lock');
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
    can_upload: false,
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
  // The radio whose checked property is set. happy-dom's :checked selector
  // can answer from before a click that no render followed.
  const checked = () => [...panel()!.querySelectorAll<HTMLInputElement>('input[type=radio]')].find((i) => i.checked)?.value;
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

  // TKT-01M3TSZHSW: after a save, the page is asked for again, and a panel
  // opened before that answer lands was prefilled from the page before the
  // save. It follows the thread's value until the moderator chooses.
  async function saveThenReopen(answer: number) {
    let land!: () => void;
    const held = new Promise<ThreadPage>((r) => (land = () => r(at({ viewer: { slow_mode_seconds: answer } }))));
    pages = [at({ viewer: { slow_mode_seconds: 0 } }), held];
    moderate['slow-mode'] = (b) => ({ status: 200, body: b, headers: { 'Cache-Control': 'no-store' } });
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'slow-mode');
    await pick('10');
    await submit();
    expect(panel()).toBeNull();
    await open(root, 'slow-mode');
    expect(checked()).toBe('0');
    return async () => {
      land();
      await settle(root);
    };
  }

  it('the slow-mode panel opened before the page refetch takes the thread value when it lands', async () => {
    let landed = await saveThenReopen(10);
    await landed();
    expect(checked()).toBe('10');

    // A value no interval names lands as Custom's seconds.
    landed = await saveThenReopen(45);
    await landed();
    expect(checked()).toBe('custom');
    expect(panel()!.querySelector<HTMLInputElement>('input[inputmode=numeric]')!.value).toBe('45');
  });

  // Terva review 1795: a click on the radio already chosen is a choice too.
  it('a click on the slow-mode radio already chosen is kept when the page refetch lands', async () => {
    const landed = await saveThenReopen(10);
    await pick('0');
    await landed();
    expect(checked()).toBe('0');
    await submit();
    expect(puts().map((p) => p.body)).toEqual([{ seconds: 10 }, { seconds: 0 }]);
  });

  it("a moderator's slow-mode choice is kept when the page refetch lands", async () => {
    const landed = await saveThenReopen(10);
    await landed();
    expect(checked()).toBe('10');
    // Once the moderator chooses, a later answer leaves the choice alone:
    // here after a conflict, which asks for the page again.
    let land!: () => void;
    pages = [new Promise<ThreadPage>((r) => (land = () => r(at({ viewer: { slow_mode_seconds: 30 } }))))];
    moderate['slow-mode'] = () => problemAnswer('conflict', 409);
    await pick('300');
    await submit();
    land();
    await settle(document.body);
    expect(panel()?.querySelector('[role=alert]')?.textContent).toBe('Slow mode is already set to that, or the thread is archived.');
    expect(checked()).toBe('300');

    await pick('custom');
    await custom('45');
    await submit();
    await settle(document.body);
    expect(checked()).toBe('custom');
    expect(panel()!.querySelector<HTMLInputElement>('input[inputmode=numeric]')!.value).toBe('45');
    expect(puts().map((p) => p.body)).toEqual([{ seconds: 10 }, { seconds: 300 }, { seconds: 45 }]);
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

  // TKT-01M3YSQPZX: the thread's tags on its head, as links to its forum's
  // list filtered to each, and a moderator's Edit tags, PUT …/tags.
  const tagsUrl = `/api/v1/admin/threads/${id}/tags`;
  const chips = (root: HTMLElement) =>
    [...root.querySelectorAll<HTMLAnchorElement>('.thread-head .thread-tags a')].map((a) => [a.textContent, a.getAttribute('href')]);
  async function typeTags(v: string) {
    const f = panel()!.querySelector<HTMLInputElement>('input[type=text]')!;
    f.value = v;
    await act(async () => void f.dispatchEvent(new Event('input', { bubbles: true })));
  }

  it('shows the tags on the head as links to the filtered list', async () => {
    const forum = at().thread.forum.slug;
    pages = [at({ thread: { tags: ['garden', 'long-read'] } })];
    let root = await renderAt(`/t/${id}/a`);
    expect(chips(root)).toEqual([
      ['garden', `/f/${forum}?tag=garden`],
      ['long-read', `/f/${forum}?tag=long-read`],
    ]);
    // After the started-by line.
    const meta = root.querySelector('.thread-meta')!;
    expect(meta.nextElementSibling?.classList.contains('thread-tags')).toBe(true);
    pages = [at({ thread: { tags: [] } })];
    root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('.thread-head .thread-tags')).toBeNull();
  });

  it('a moderator edits the tags and the head shows them', async () => {
    const forum = at().thread.forum.slug;
    pages = [at({ thread: { tags: ['garden'] } }), at({ thread: { tags: ['garden', 'water'], log_length: len + 1 } })];
    moderate.tags = (b) => ({
      status: 200,
      body: header({ tags: (b as { tags: string[] }).tags.map((t) => t.toLowerCase()).sort() }),
      headers: { 'Cache-Control': 'no-store' },
    });
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('[data-shell=toolbar] [data-action=tags]')?.textContent).toBe('Edit tags…');
    await open(root, 'tags');
    expect(panel()!.querySelector('h2')?.textContent).toBe('Edit tags');
    const field = panel()!.querySelector<HTMLInputElement>('input[type=text]')!;
    expect(field.value).toBe('garden');
    expect(document.activeElement).toBe(field);
    // The tags the thread has, however typed, would change nothing.
    expect(sendDisabled()).toBe(true);
    await typeTags('GARDEN, garden');
    expect(sendDisabled()).toBe(true);
    await typeTags('Garden, water');
    expect(sendDisabled()).toBe(false);
    await submit();
    await settle(root);
    expect(puts()).toEqual([{ url: tagsUrl, body: { tags: ['Garden', 'water'] } }]);
    expect(panel()).toBeNull();
    expect(chips(root)).toEqual([
      ['garden', `/f/${forum}?tag=garden`],
      ['water', `/f/${forum}?tag=water`],
    ]);
    expect(document.activeElement).toBe(root.querySelector('h1'));
  });

  // Terva review 2049: Save is off only for the tags the server would fold
  // to the set the thread has. The Kelvin sign lowercases to k under
  // Unicode rules, but the server folds ASCII only and refuses it, so on a
  // thread tagged k it is a change for the server to judge.
  it('offers Save for a tag that folds to one the thread has only by Unicode rules', async () => {
    pages = [at({ thread: { tags: ['k'] } })];
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'tags');
    expect(sendDisabled()).toBe(true);
    await typeTags('K');
    expect(sendDisabled()).toBe(true);
    await typeTags('\u212A');
    expect(sendDisabled()).toBe(false);
  });

  it('names each refused tag under the field and keeps what was typed', async () => {
    let answer: Answer = problemAnswer('validation-failed', 422, {
      errors: [
        { field: 'tags.1', code: 'invalid_tag' },
        { field: 'tags.2', code: 'too_long' },
        { field: 'tags', code: 'too_many' },
      ],
    });
    moderate.tags = () => answer;
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'tags');
    await typeTags('ok a_b abcdefghijklmnopqrstuvwxyz');
    await submit();
    await settle(root);
    const field = panel()!.querySelector<HTMLInputElement>('input[type=text]')!;
    expect(field.value).toBe('ok a_b abcdefghijklmnopqrstuvwxyz');
    expect(field.getAttribute('aria-invalid')).toBe('true');
    const errs = panel()!.querySelector('[data-tag-errors]')!;
    expect(field.getAttribute('aria-describedby')?.split(' ')).toContain(errs.id);
    expect([...errs.querySelectorAll('li')].map((l) => l.textContent)).toEqual([
      '“a_b” is not a tag: use letters, digits and single hyphens between them.',
      '“abcdefghijklmnopqrstuvwxyz” is longer than 24 characters.',
      'A thread can have at most 8 tags.',
    ]);
    expect(panel()!.querySelector('[role=alert]')?.textContent).toBe('A tag was refused. The Tags field says which.');

    // Another moderator set them first: the form stays, to copy from.
    answer = problemAnswer('conflict', 409);
    await submit();
    await settle(root);
    expect(panel()!.querySelector('[role=alert]')?.textContent).toBe('The thread already has these tags, or it is archived.');
    expect(panel()!.querySelector('[data-tag-errors]')).toBeNull();
    expect(panel()!.querySelector<HTMLInputElement>('input[type=text]')!.value).toBe('ok a_b abcdefghijklmnopqrstuvwxyz');
  });

  it('keeps the tags typed, with Save off, once the page says the viewer no longer moderates', async () => {
    pages = [at(), at({ viewer: { can_moderate: false } })];
    moderate.tags = () => problemAnswer('forbidden', 403);
    const root = await renderAt(`/t/${id}/a`);
    await open(root, 'tags');
    await typeTags('garden water');
    await submit();
    await settle(root);
    expect(panel()!.querySelector('[role=alert]')?.textContent).toBe('You can no longer moderate this thread.');
    expect(panel()!.querySelector<HTMLInputElement>('input[type=text]')!.value).toBe('garden water');
    expect(sendDisabled()).toBe(true);
  });

  it('Edit tags is not offered without can_moderate', async () => {
    for (const role of ['member', 'moderator', 'admin'] as const) {
      as(role);
      pages = [at({ viewer: { can_moderate: false }, thread: { tags: ['garden'] } })];
      const root = await renderAt(`/t/${id}/a`);
      expect(root.querySelector('[data-action=tags]'), role).toBeNull();
      // The chips are every reader's.
      expect(chips(root).length, role).toBe(1);
    }
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
      // Post 62's history (TKT-01M29CP2Q3): one revision.
      [`GET ${url}/posts/62/history`]: () => ({
        status: 200,
        body: { ordinal: 62, deleted: false, redacted: false, revisions: [{ revision: 1, actor: page.posts[0].author, at: 1, reason: '', source: 'x', grace: false }] },
      }),
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
    // The history screen is still the one shown, with the post's revision,
    // after the mark lands (TKT-01M29CP2Q3).
    if (to.endsWith('/history')) {
      expect(screenOf(root)).toBe('post-history');
      expect(root.querySelectorAll('.post-history .revision')).toHaveLength(1);
    }
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

// TKT-01M29CTYA0: live updates on the thread page (docs/06 "Live updates").
// The stub server holds a thread of posts by mara, paged by 25, and the
// stream is FakeEventSource; a hint is sent once the thread has changed.
describe('live updates on the thread screen', () => {
  const saved = viewer.value;
  const id = '01k4z3j0w8x2d';
  const url = `/api/v1/threads/${id}`;
  const base = threadPage({ id, slug: 'a', title: 'Live' });
  let posts: Post[];
  let len: number;
  let lastRead: number;
  let onPost: (() => void) | null;
  let holdPost: Promise<void> | null;
  let holdGet: Promise<void> | null;
  let failGets: number;
  let server: StubServer;
  let observers: { observed: Element[]; callback: IntersectionObserverCallback }[];
  let scrolled: string[];

  const post = (ordinal: number, over: Partial<Post> = {}): Post => ({
    ...base.posts[0],
    ordinal,
    author: { ...base.posts[0].author, handle: 'mara' },
    reply_to: null,
    reply_to_suppressed: false,
    reply_count: 0,
    attachments: [],
    edited_at: null,
    edited_by_moderator: false,
    deleted: false,
    removal: null,
    html: `<p>body of ${ordinal}</p>`,
    ...over,
  });
  const alice = { ...base.posts[0].author, handle: 'alice' };
  const pageOf = (n: number): ThreadPage => ({
    ...base,
    lens: null,
    thread: { ...base.thread, post_count: posts.length, log_length: len, pinned: null },
    page: { number: n, size: 25, total_pages: Math.max(1, Math.ceil(posts.length / 25)) },
    viewer: { ...base.viewer, last_read_ordinal: lastRead, can_reply: true, slow_mode_seconds: 0, next_post_at: null },
    posts: posts.slice((n - 1) * 25, n * 25),
  });
  // add appends posts and lengthens the log; hint then says so on the
  // stream.
  const add = (...made: Post[]) => {
    posts = [...posts, ...made];
    len += 100;
  };
  const stream = () => FakeEventSource.open()[0];
  const hint = async (root: HTMLElement) => {
    await act(async () => stream().emit('thread', { thread_id: id, log_length: len }));
    await settle(root);
  };
  // atBottom says the foot of the post list is in view, or not.
  const atBottom = async (on: boolean) => {
    const io = observers.find((o) => o.observed.some((el) => !el.id.startsWith('post-')))!;
    await act(async () => io.callback([{ isIntersecting: on } as IntersectionObserverEntry], io as unknown as IntersectionObserver));
  };
  const pill = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('button.new-posts');
  const drawn = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.posts > [id^=post-]')];
  const ids = (root: HTMLElement) => drawn(root).map((e) => e.id);

  beforeEach(() => {
    observers = [];
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
    scrolled = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled.push(this.id);
    };
    posts = [1, 2, 3, 4, 5].map((o) => post(o));
    len = 1000;
    lastRead = 0;
    onPost = null;
    holdPost = null;
    holdGet = null;
    failGets = 0;
    const pageFor = (u: string) => {
      const q = new URLSearchParams(u.split('?')[1] ?? '');
      const o = Number(q.get('ordinal') ?? 0);
      const body = pageOf(o ? Math.ceil(o / 25) : Number(q.get('page') ?? 1));
      // A lens is answered as one, as the server answers a lens it knows.
      const [kind, value = ''] = (q.get('lens') ?? '').split(':');
      if (kind) body.lens = { kind, value, shown: posts.length, hidden: 0, gaps: [] };
      return { status: 200, body, headers: { ETag: `"${len}"` } };
    };
    server = stubServer({});
    server.routes = new Proxy(
      {},
      {
        get: (_t, k: string) => {
          if (k === `POST ${url}/posts`)
            return async (b: { source: string }) => {
              await holdPost;
              const made = post(posts.length + 1, { author: alice, html: `<p>${b.source}</p>` });
              add(made);
              onPost?.();
              return { status: 201, body: { post: made, ordinal: made.ordinal, revision: 1, log_length: len } };
            };
          if (k.startsWith('GET /api/v1/drafts/')) return () => ({ status: 204, headers: { 'Draft-Revision': '0' } });
          if (k === `PUT ${url}/read`) return () => ({ status: 200, body: { last_read_ordinal: 1 } });
          if (k === 'POST /api/v1/render') return () => ({ status: 200, body: { html: '', mentions: [], quotes: [], reply_to: null } });
          // A held GET answers with the thread as it was when it was asked.
          if (k === `GET ${url}` || k.startsWith(`GET ${url}?`))
            return async () => {
              if (failGets > 0 && failGets--) return problemAnswer('unavailable', 503);
              const answer = pageFor(k.slice(4));
              await holdGet;
              return answer;
            };
          return undefined;
        },
      },
    ) as StubServer['routes'];
    viewer.value = structuredClone(member) as Me;
  });
  afterEach(async () => {
    await unmount();
    viewer.value = saved;
    isPhone.value = false;
    resetReadTracker();
  });

  it('new posts wait behind the pill for a reader above the bottom', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(FakeEventSource.open().map((s) => s.url)).toEqual([`/events?threads=${id}`]);
    const before = drawn(root);
    expect(pill(root)).toBeNull();
    add(post(6));
    await hint(root);
    const after = drawn(root);
    expect(after).toHaveLength(before.length);
    after.forEach((el, i) => expect(el, el.id).toBe(before[i]));
    expect(root.querySelector('#post-6')).toBeNull();
    expect(pill(root)?.textContent).toBe('1 new post — show');
    // Announced politely, a second after the count changed.
    const said = root.querySelector('[aria-live=polite]')!;
    expect(said.textContent).toBe('');
    await act(() => new Promise((r) => setTimeout(r, 1050)));
    expect(said.textContent).toBe('1 new post');
    add(post(7));
    await hint(root);
    expect(pill(root)?.textContent).toBe('2 new posts — show');
    expect(root.querySelector('#post-6, #post-7')).toBeNull();
    // The head counts every post.
    expect(root.querySelector('.thread-meta')?.textContent).toMatch(/ · 7 posts$/);
  });

  it('a reader at the bottom gets posts inline with no pill', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await atBottom(true);
    add(post(6));
    await hint(root);
    expect(ids(root)).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6']);
    expect(pill(root)).toBeNull();
    // Streamed in, not taken: focus is not moved.
    expect(document.activeElement?.id).not.toBe('post-6');
  });

  it('posts held while above the bottom are drawn when the reader reaches it', async () => {
    const root = await renderAt(`/t/${id}/a`);
    add(post(6));
    await hint(root);
    expect(pill(root)).not.toBeNull();
    await atBottom(true);
    expect(ids(root)).toContain('post-6');
    expect(pill(root)).toBeNull();
  });

  it('show reveals the posts and focuses the first', async () => {
    const root = await renderAt(`/t/${id}/a`);
    add(post(6), post(7));
    await hint(root);
    const before = location.pathname + location.search;
    await act(async () => pill(root)!.click());
    expect(ids(root)).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6', 'post-7']);
    expect(document.activeElement).toBe(root.querySelector('#post-6'));
    expect(pill(root)).toBeNull();
    expect(location.pathname + location.search).toBe(before);
    // The count starts again from here.
    add(post(8));
    await hint(root);
    expect(pill(root)?.textContent).toBe('1 new post — show');
  });

  it('the pill on an earlier page counts only posts that arrived after it opened, and goes to the first of them', async () => {
    posts = Array.from({ length: 60 }, (_, i) => post(i + 1));
    const root = await renderAt(`/t/${id}/a?page=1`);
    await atBottom(true);
    expect(ids(root)).toHaveLength(25);
    expect(pill(root)).toBeNull();
    // A hint no longer than the page on screen changes nothing.
    await hint(root);
    expect(pill(root)).toBeNull();
    add(post(61));
    await hint(root);
    expect(pill(root)?.textContent).toBe('1 new post — show');
    expect(ids(root)).toHaveLength(25);
    await act(async () => pill(root)!.click());
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/a/61`);
    expect(ids(root)).toEqual(Array.from({ length: 11 }, (_, i) => `post-${51 + i}`));
    expect(scrolled).toContain('post-61');
    expect(pill(root)).toBeNull();
  });

  it('a full last page sends the pill to the next page', async () => {
    posts = Array.from({ length: 25 }, (_, i) => post(i + 1));
    const root = await renderAt(`/t/${id}/a`);
    await atBottom(true);
    add(post(26));
    await hint(root);
    // At the bottom, but #26 is on page 2: held, not drawn.
    expect(ids(root)).toHaveLength(25);
    expect(pill(root)?.textContent).toBe('1 new post — show');
    await act(async () => pill(root)!.click());
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/a/26`);
    expect(ids(root)).toEqual(['post-26']);
  });

  const reply = async (root: HTMLElement, text: string) => {
    const area = root.querySelector<HTMLTextAreaElement>('.composer textarea')!;
    area.value = text;
    await act(() => void area.dispatchEvent(new Event('input', { bubbles: true })));
    await act(async () => root.querySelector<HTMLButtonElement>('[data-post-reply]')!.click());
  };

  it('a reply from a full page goes to its own page', async () => {
    posts = Array.from({ length: 25 }, (_, i) => post(i + 1));
    const root = await renderAt(`/t/${id}/a?page=1`);
    await reply(root, 'my reply');
    await settle(root);
    await settle(root);
    // Shown at once, with no other post and no hint since.
    expect(location.pathname).toBe(`/t/${id}/a/26`);
    expect(ids(root)).toEqual(['post-26']);
    expect(root.querySelector('#post-26')?.textContent).toContain('my reply');
    expect(scrolled).toContain('post-26');
    expect(pill(root)).toBeNull();
  });

  it("the reader's own reply never counts in the pill", async () => {
    const root = await renderAt(`/t/${id}/a`);
    // The server sends the reply's hint before it answers the POST.
    const seen: (string | null)[] = [];
    onPost = () => stream().emit('thread', { thread_id: id, log_length: len });
    new MutationObserver(() => seen.push(pill(root)?.textContent ?? null)).observe(root, { childList: true, subtree: true });
    await reply(root, 'mine');
    await settle(root);
    await settle(root);
    expect(seen.filter((s) => s !== null)).toEqual([]);
    expect(location.pathname).toBe(`/t/${id}/a/6`);
    expect(ids(root)).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6']);
    expect(pill(root)).toBeNull();
  });

  it('a hint for the reply while it is out shows no pill', async () => {
    const root = await renderAt(`/t/${id}/a`);
    let release!: () => void;
    holdPost = new Promise((r) => (release = r));
    await reply(root, 'mine');
    // The server has made the post, and the stream says so, but the POST
    // has not answered yet.
    add(post(6, { author: alice, html: '<p>mine</p>' }));
    await hint(root);
    expect(pill(root)).toBeNull();
    // The POST then answers: the post is made once.
    posts = posts.slice(0, 5);
    len -= 100;
    release();
    await settle(root);
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/a/6`);
    expect(root.querySelector('#post-6')?.textContent).toContain('mine');
    expect(pill(root)).toBeNull();
  });

  it('posting a reply with posts held takes the reader to the reply', async () => {
    const root = await renderAt(`/t/${id}/a`);
    add(post(6), post(7));
    await hint(root);
    expect(pill(root)?.textContent).toBe('2 new posts — show');
    await reply(root, 'after them');
    await settle(root);
    await settle(root);
    expect(location.pathname).toBe(`/t/${id}/a/8`);
    expect(ids(root)).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6', 'post-7', 'post-8']);
    expect(scrolled.at(-1)).toBe('post-8');
    expect(pill(root)).toBeNull();
  });

  it('an edit and a removal in one change both show', async () => {
    const root = await renderAt(`/t/${id}/a`);
    posts = posts.map((p) =>
      p.ordinal === 3 ? { ...p, html: '<p>edited 3</p>', edited_at: 1 } : p.ordinal === 4 ? { ...p, deleted: true, html: '' } : p,
    );
    len += 100;
    await hint(root);
    expect(root.querySelector('#post-3')?.textContent).toContain('edited 3');
    expect(root.querySelector('#post-4')?.classList.contains('tombstone')).toBe(true);
    expect(pill(root)).toBeNull();
    // The restore that follows shows too.
    posts = posts.map((p) => (p.ordinal === 4 ? post(4) : p));
    len += 100;
    await hint(root);
    expect(root.querySelector('#post-4')?.classList.contains('tombstone')).toBe(false);
    expect(root.querySelector('#post-4')?.textContent).toContain('body of 4');
  });

  it('an edit applies in place without moving the post in view', async () => {
    // #1 and #2 are above the viewport and #3 is the first post in view;
    // #2's edit makes it 60 px taller, which pushes #3 down.
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const o = Number(this.getAttribute('data-ordinal'));
      const grown = document.querySelector('#post-2')?.textContent?.includes('much longer') ? 60 : 0;
      const top = o < 3 ? -300 + o * 100 : 100 + (o - 3) * 100 + grown;
      return { top, bottom: top + 90, height: 90 } as DOMRect;
    });
    const by = vi.fn();
    vi.stubGlobal('scrollBy', by);
    const root = await renderAt(`/t/${id}/a`);
    const third = root.querySelector('#post-3');
    posts = posts.map((p) => (p.ordinal === 2 ? { ...p, html: '<p>much longer now</p>' } : p));
    len += 100;
    await hint(root);
    expect(root.querySelector('#post-2')?.textContent).toContain('much longer');
    expect(root.querySelector('#post-3')).toBe(third);
    expect(by).toHaveBeenCalledWith(0, 60);
    rect.mockRestore();
  });

  it('a post behind the pill is not marked read', async () => {
    const root = await renderAt(`/t/${id}/a`);
    add(post(6));
    await hint(root);
    const tracked = () =>
      observers.filter((o) => o.observed.some((el) => el.id.startsWith('post-'))).flatMap((o) => o.observed.map((el) => el.id));
    expect(tracked()).toContain('post-5');
    expect(tracked()).not.toContain('post-6');
    await act(async () => pill(root)!.click());
    expect(tracked()).toContain('post-6');
  });

  it('a hint asks for the page on screen by its number, never a fragment', async () => {
    posts = Array.from({ length: 30 }, (_, i) => post(i + 1));
    const root = await renderAt(`/t/${id}/a/27`);
    add(post(31));
    await hint(root);
    const gets = server.sent.filter((r) => r.method === 'GET' && r.url.startsWith(url)).map((r) => r.url);
    expect(gets).toEqual([`${url}?ordinal=27`, `${url}?page=2`]);
  });

  it('polls the page on screen when the stream is refused', async () => {
    FakeEventSource.ready = false;
    try {
      const root = await renderAt(`/t/${id}/a`);
      vi.useFakeTimers();
      await act(async () => stream().fail());
      add(post(6));
      await act(async () => void (await vi.advanceTimersByTimeAsync(30_000)));
      vi.useRealTimers();
      await settle(root);
      expect(pill(root)?.textContent).toBe('1 new post — show');
    } finally {
      FakeEventSource.ready = true;
      vi.useRealTimers();
    }
  });

  it('a held copy shown while it is asked for again holds back nothing it brings', async () => {
    const root = await renderAt(`/t/${id}/a?page=1`);
    await route('/f/general');
    await settle(root);
    // The thread grew while the reader was away; the page they return to
    // is shown at once from the held copy, and its answer adds #6.
    add(post(6));
    await route(`/t/${id}/a?page=1`);
    await settle(root);
    expect(ids(root)).toEqual(['post-1', 'post-2', 'post-3', 'post-4', 'post-5', 'post-6']);
    expect(pill(root)).toBeNull();
  });

  it('the divider stays where it was drawn while the reader reads on', async () => {
    lastRead = 3;
    const root = await renderAt(`/t/${id}/a`);
    const divider = () => root.querySelector('#unread')?.nextElementSibling?.id;
    expect(divider()).toBe('post-4');
    expect(root.querySelector('#unread')?.textContent).toContain('2 new posts below');
    // The reader has read on to #5, and #6 arrives at the bottom.
    await atBottom(true);
    lastRead = 5;
    add(post(6));
    await hint(root);
    expect(ids(root)).toContain('post-6');
    expect(divider()).toBe('post-4');
    expect(root.querySelector('#post-5')?.classList.contains('unread')).toBe(true);
  });

  it('a hint while another page of the thread loads is caught up once it arrives', async () => {
    posts = Array.from({ length: 30 }, (_, i) => post(i + 1));
    const root = await renderAt(`/t/${id}/a?page=1`);
    let release!: () => void;
    holdGet = new Promise((r) => (release = r));
    await route(`/t/${id}/a?page=2`);
    // Page 2 is asked for and answered from before #31; the hint for #31
    // comes while it is out.
    add(post(31));
    await act(async () => stream().emit('thread', { thread_id: id, log_length: len }));
    holdGet = null;
    release();
    await settle(root);
    await settle(root);
    expect(ids(root)).toEqual(['post-26', 'post-27', 'post-28', 'post-29', 'post-30']);
    expect(pill(root)?.textContent).toBe('1 new post — show');
  });

  it('a refresh that fails is tried again without another hint', async () => {
    const root = await renderAt(`/t/${id}/a`);
    vi.useFakeTimers();
    try {
      add(post(6));
      failGets = 1;
      await act(async () => stream().emit('thread', { thread_id: id, log_length: len }));
      await act(async () => void (await vi.advanceTimersByTimeAsync(10)));
      expect(pill(root)).toBeNull();
      await act(async () => void (await vi.advanceTimersByTimeAsync(30_000)));
    } finally {
      vi.useRealTimers();
    }
    await settle(root);
    expect(pill(root)?.textContent).toBe('1 new post — show');
    expect(server.sent.filter((r) => r.method === 'GET' && r.url.startsWith(url))).toHaveLength(3);
  });

  it('what the screen offers follows the live page', async () => {
    // A reader who has read everything is offered no First unread; a post
    // that arrives makes one unread.
    lastRead = 5;
    const root = await renderAt(`/t/${id}/a`);
    const offered = () => [...root.querySelectorAll('[data-shell=toolbar] [data-action]')].map((b) => b.getAttribute('data-action'));
    expect(offered()).not.toContain('first-unread');
    add(post(6));
    await hint(root);
    expect(offered()).toContain('first-unread');
    expect(root.querySelector('.composer')).not.toBeNull();
  });

  it('a lens page holds no stream', async () => {
    await renderAt(`/t/${id}/a?lens=author:mara`);
    expect(FakeEventSource.all).toEqual([]);
  });

  it('leaving the thread closes the stream', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(FakeEventSource.open()).toHaveLength(1);
    await route(`/t/${id}/a?page=1`);
    await settle(root);
    // Another page of the same thread keeps it.
    expect(FakeEventSource.open()).toHaveLength(1);
    expect(FakeEventSource.all).toHaveLength(1);
    await route('/f/general');
    await settle(root);
    expect(FakeEventSource.open()).toEqual([]);
  });
});
