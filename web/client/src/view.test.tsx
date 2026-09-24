import { render } from 'preact';
import { act } from 'preact/test-utils';
import { LocationProvider } from 'preact-iso';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { canonicalThreadUrl, useThreadView, type ThreadView } from './view';
import { renderAt, screenOf } from './testing';

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
});

// AC3: a stale slug is replaced by the canonical one, in place, with the
// ordinal, page and lens kept.
describe('a stale slug', () => {
  it('redirects to the canonical URL, replacing the history entry', async () => {
    const asked: string[] = [];
    vi.stubGlobal('fetch', async (url: string) => {
      asked.push(url);
      return new Response(JSON.stringify({ thread: { id: '01k4z3j0w8x2c', slug: 'renamed', title: 'Renamed' } }), {
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
      new Response(JSON.stringify({ thread: { id: '01k4z3j0w8x2c', slug: 'renamed', title: 'T' } }), { status: 200 }),
    );
    await renderAt('/t/01k4z3j0w8x2c/old-name/42/history');
    expect(location.pathname).toBe('/t/01k4z3j0w8x2c/renamed/42/history');
  });

  it('is not found when the thread is not', async () => {
    vi.stubGlobal('fetch', async () => new Response('{}', { status: 404 }));
    const root = await renderAt('/t/01k4z3j0w8x2c/gone');
    expect(screenOf(root)).toBe('not-found');
  });
});
