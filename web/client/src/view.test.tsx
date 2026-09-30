import { options, render, type VNode } from 'preact';
import { act } from 'preact/test-utils';
import { LocationProvider } from 'preact-iso';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { canonicalThreadUrl, parsePage, useThreadView, type ThreadView } from './view';
import { renderAt, route, screenOf, settle, threadJSON } from './testing';
import { ThreadScreen } from './screens/ThreadScreen';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const at = (url: string) => {
  const u = new URL(url, 'http://ketju.test');
  return { pathname: u.pathname, search: u.search, hash: u.hash };
};

describe('canonicalThreadUrl', () => {
  const thread = { id: '01k4z3j0w8x2c', slug: 'renamed' };
  it.each([
    ['/t/01k4z3j0w8x2c/old', '/t/01k4z3j0w8x2c/renamed'],
    ['/t/01k4z3j0w8x2c/old?page=2&lens=author:alice', '/t/01k4z3j0w8x2c/renamed?page=2&lens=author:alice'],
    ['/t/01k4z3j0w8x2c/old?lens=author%3Aalice&page=2&x=1', '/t/01k4z3j0w8x2c/renamed?lens=author%3Aalice&page=2&x=1'],
    ['/t/01k4z3j0w8x2c/old/42', '/t/01k4z3j0w8x2c/renamed/42'],
    ['/t/01k4z3j0w8x2c/old/42/history', '/t/01k4z3j0w8x2c/renamed/42/history'],
    ['/t/01k4z3j0w8x2c/old/42#post-42', '/t/01k4z3j0w8x2c/renamed/42#post-42'],
  ])('%s → %s', (from, to) => {
    expect(canonicalThreadUrl(at(from), thread)).toBe(to);
  });

  it('leaves a current slug alone', () => {
    expect(canonicalThreadUrl(at('/t/01k4z3j0w8x2c/renamed?page=2'), thread)).toBeNull();
    expect(canonicalThreadUrl(at('/t/01k4z3j0w8x2c/caf%C3%A9'), { ...thread, slug: 'café' })).toBeNull();
  });

  it('leaves another thread, or no thread, alone', () => {
    expect(canonicalThreadUrl(at('/t/01k4z3j0w8x2d/old'), thread)).toBeNull();
    expect(canonicalThreadUrl(at('/f/old'), thread)).toBeNull();
  });

  it('encodes the new slug', () => {
    expect(canonicalThreadUrl(at('/t/01k4z3j0w8x2c/old'), { ...thread, slug: 'café au lait' })).toBe(
      '/t/01k4z3j0w8x2c/caf%C3%A9%20au%20lait',
    );
  });
});

// AC2: the view is the URL. Reading it needs nothing but the address, and
// changing it changes the address and nothing else.
describe('useThreadView', () => {
  let view!: ThreadView;
  function Probe() {
    view = useThreadView();
    return null;
  }
  const mount = async (url: string) => {
    history.replaceState(null, '', url);
    const root = document.createElement('div');
    await act(async () => {
      render(
        <LocationProvider>
          <Probe />
        </LocationProvider>,
        root,
      );
    });
    return root;
  };

  it('reads page and lens from the query', async () => {
    await mount('/t/01k4z3j0w8x2c/a?page=3&lens=author:alice');
    expect([view.page, view.hasPage, view.lens]).toEqual([3, true, 'author:alice']);
    await mount('/t/01k4z3j0w8x2c/a');
    expect([view.page, view.hasPage, view.lens]).toEqual([1, false, '']);
    await mount('/t/01k4z3j0w8x2c/a?page=0');
    expect([view.page, view.hasPage]).toEqual([1, false]);
  });

  it('writes them to the URL, and a fresh load of that URL is the same view', async () => {
    await mount('/t/01k4z3j0w8x2c/a?lens=author:alice');
    await act(() => view.setPage(4));
    expect(location.pathname).toBe('/t/01k4z3j0w8x2c/a');
    expect(new URLSearchParams(location.search).get('page')).toBe('4');
    expect(new URLSearchParams(location.search).get('lens')).toBe('author:alice');
    await act(() => view.setLens(''));
    expect(location.search).toBe('?page=4');

    const shared = location.pathname + location.search;
    await mount(shared);
    expect([view.page, view.lens]).toEqual([4, '']);
  });

  it('keeps the path and the anchor when the query changes', async () => {
    await mount('/t/01k4z3j0w8x2c/a/42?lens=author:alice#post-42');
    await act(() => view.setPage(2));
    expect(location.pathname).toBe('/t/01k4z3j0w8x2c/a/42');
    expect(location.hash).toBe('#post-42');
    await act(() => view.setLens(''));
    expect(location.pathname + location.search + location.hash).toBe('/t/01k4z3j0w8x2c/a/42?page=2#post-42');
  });

  it('treats a malformed page as none', async () => {
    await mount('/t/01k4z3j0w8x2c/a?page=2junk');
    expect([view.page, view.hasPage]).toEqual([1, false]);
  });
});

describe('parsePage', () => {
  it.each([
    ['1', 1],
    ['12', 12],
    ['9007199254740991', 9007199254740991],
  ])('accepts %s', (raw, want) => {
    expect(parsePage(raw)).toBe(want);
  });
  it.each([undefined, '', '0', '-1', '01', '2junk', ' 2', '2 ', '1e3', '1.0', '0x10', '9007199254740993'])(
    'refuses %s',
    (raw) => {
      expect(parsePage(raw)).toBeNull();
    },
  );
});

// AC3: a stale slug is replaced by the canonical one, in place, with the
// ordinal, page and lens kept.
describe('a stale slug', () => {
  it('redirects to the canonical URL, replacing the history entry', async () => {
    const asked: string[] = [];
    vi.stubGlobal('fetch', async (url: string) => {
      asked.push(url);
      return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'renamed', title: 'Renamed' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    const push = vi.spyOn(history, 'pushState');
    const replace = vi.spyOn(history, 'replaceState');

    const root = await renderAt('/t/01k4z3j0w8x2c/old-name?page=2&lens=author:alice');
    expect(location.pathname + location.search).toBe('/t/01k4z3j0w8x2c/renamed?page=2&lens=author:alice');
    expect(push).not.toHaveBeenCalled();
    expect(replace).toHaveBeenCalled();
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).toContain('Renamed');
    expect(asked[0]).toBe('/api/v1/threads/01k4z3j0w8x2c?page=2&lens=author%3Aalice');
  });

  it('keeps the permalink ordinal', async () => {
    vi.stubGlobal('fetch', async () =>
      new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'renamed', title: 'T' }), { status: 200 }),
    );
    const asked: string[] = [];
    vi.stubGlobal('fetch', async (url: string) => {
      asked.push(url);
      // The history route's screen asks for the post's history too
      // (TKT-01M29CP2Q3).
      if (url.endsWith('/history')) {
        return new Response(JSON.stringify({ ordinal: 42, deleted: false, redacted: false, revisions: [] }), { status: 200 });
      }
      return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'renamed', title: 'T' }), {
        status: 200,
      });
    });
    await renderAt('/t/01k4z3j0w8x2c/old-name/42/history');
    expect(location.pathname).toBe('/t/01k4z3j0w8x2c/renamed/42/history');
    // The page asked for is the one holding post 42, beside the history.
    expect(asked.filter((u) => !u.endsWith('/history'))).toEqual(['/api/v1/threads/01k4z3j0w8x2c?ordinal=42']);
  });

  // The thread is already on screen and a link to an old slug of it is
  // followed: nothing new is fetched, and the URL is still corrected.
  it('corrects a stale slug of the thread already on screen', async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async () => {
      calls++;
      return new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'renamed', title: 'T' }), {
        status: 200,
      });
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/renamed');
    expect(calls).toBe(1);
    await route('/t/01k4z3j0w8x2c/old-name#post-3');
    await settle(root);
    expect(location.pathname + location.hash).toBe('/t/01k4z3j0w8x2c/renamed#post-3');
    expect(calls).toBe(1);
  });

  it('is not found when the thread is not', async () => {
    vi.stubGlobal('fetch', async () => new Response('{}', { status: 404 }));
    const root = await renderAt('/t/01k4z3j0w8x2c/gone');
    expect(screenOf(root)).toBe('not-found');
  });

  // A 404 belongs to the thread that was missing. Moving to another thread,
  // in the same document, shows that one.
  it('recovers from a missing thread on navigating to one that exists', async () => {
    vi.stubGlobal('fetch', async (url: string) =>
      url.includes('01k4z3j0w8x2c')
        ? new Response('{}', { status: 404 })
        : new Response(threadJSON({ id: '01k4z3j0w8x2d', slug: 'here', title: 'Here' }), {
            status: 200,
          }),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/gone');
    expect(screenOf(root)).toBe('not-found');
    await route('/t/01k4z3j0w8x2d/here');
    await settle(root);
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).toContain('Here');
  });

  // The same thread answering later: a 404 on one request, the page on the
  // next.
  it('recovers when the same thread answers after a 404', async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async () =>
      calls++ === 0
        ? new Response('{}', { status: 404 })
        : new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Back' }), { status: 200 }),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(screenOf(root)).toBe('not-found');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).toContain('Back');
  });

  // Every render of the thread screen, not only the settled one: a stale
  // title or 404 that shows for a single render before an effect clears it
  // is still shown.
  const everyRender = (root: HTMLElement) => {
    const seen: string[] = [];
    const prev = options.diffed;
    options.diffed = (vnode: VNode) => {
      if (vnode.type === ThreadScreen) seen.push(root.textContent ?? '');
      prev?.(vnode);
    };
    return { seen, stop: () => (options.diffed = prev) };
  };

  it('never renders the last thread for the next one, even once', async () => {
    vi.stubGlobal('fetch', async (url: string) =>
      url.includes('01k4z3j0w8x2c')
        ? new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'First' }), { status: 200 })
        : new Promise<Response>(() => {}),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.textContent).toContain('First');
    const r = everyRender(root);
    try {
      await route('/t/01k4z3j0w8x2d/b');
      await settle(root);
    } finally {
      r.stop();
    }
    expect(r.seen.length).toBeGreaterThan(0);
    expect(r.seen.filter((t) => t.includes('First'))).toEqual([]);
  });

  it("never renders the last thread's 404 for the next one, even once", async () => {
    vi.stubGlobal('fetch', async (url: string) =>
      url.includes('01k4z3j0w8x2c') ? new Response('{}', { status: 404 }) : new Promise<Response>(() => {}),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/gone');
    expect(screenOf(root)).toBe('not-found');
    const r = everyRender(root);
    try {
      await route('/t/01k4z3j0w8x2d/b');
      await settle(root);
    } finally {
      r.stop();
    }
    expect(r.seen.length).toBeGreaterThan(0);
    expect(r.seen.filter((t) => t.includes('Not found'))).toEqual([]);
  });

  // A later failure that is not a 404 says so; it does not leave the
  // earlier 404 calling the thread missing.
  it.each(['500', 'network'])('replaces an earlier 404 after a later %s failure', async (kind) => {
    let calls = 0;
    vi.stubGlobal('fetch', async () => {
      if (calls++ === 0) return new Response('{}', { status: 404 });
      if (kind === 'network') throw new TypeError('network down');
      return new Response('{}', { status: 500 });
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(screenOf(root)).toBe('not-found');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).toContain('could not be loaded');
  });

  // Requests can finish out of order. An older one failing after a newer
  // one succeeded is old news, not a failure of what is on screen.
  it('ignores a superseded request that fails late', async () => {
    let failFirst!: () => void;
    let calls = 0;
    vi.stubGlobal('fetch', (url: string) => {
      if (calls++ === 0) return new Promise<Response>((_, reject) => (failFirst = () => reject(new TypeError('late'))));
      return Promise.resolve(
        new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Newer' }), { status: 200 }),
      ).then((r) => (expect(url).toContain('page=2'), r));
    });
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(root.textContent).toContain('Newer');
    await act(async () => failFirst());
    await settle(root);
    expect(root.textContent).toContain('Newer');
    expect(root.textContent).not.toContain('could not be loaded');
  });

  it('keeps the title through a later failure', async () => {
    let calls = 0;
    vi.stubGlobal('fetch', async () =>
      calls++ === 0
        ? new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'Kept' }), { status: 200 })
        : new Response('{}', { status: 503 }),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    await route('/t/01k4z3j0w8x2c/a?page=2');
    await settle(root);
    expect(root.textContent).toContain('Kept');
    expect(root.textContent).toContain('could not be loaded');
  });

  // Nor does one thread's title stay up while the next one loads.
  it('does not show the last thread while the next loads', async () => {
    vi.stubGlobal('fetch', async (url: string) =>
      url.includes('01k4z3j0w8x2c')
        ? new Response(threadJSON({ id: '01k4z3j0w8x2c', slug: 'a', title: 'First' }), { status: 200 })
        : new Promise<Response>(() => {}),
    );
    const root = await renderAt('/t/01k4z3j0w8x2c/a');
    expect(root.textContent).toContain('First');
    await route('/t/01k4z3j0w8x2d/b');
    await settle(root);
    expect(screenOf(root)).toBe('thread');
    expect(root.textContent).not.toContain('First');
  });
});
