import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { dwellMs, resetReadTracker } from '../state/readTracker';
import { viewer } from '../state/viewer';
import { renderAt, route, screenOf, settle, threadJSON } from '../testing';
import member from '../../../../internal/dto/testdata/golden/me.member.json';

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
    vi.stubGlobal('fetch', async () => {
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
    // The golden page holds posts 61 to 63, and the reader has read to 60.
    expect(io.observed.map((el) => el.id)).toEqual(['post-61', 'post-62', 'post-63']);
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
