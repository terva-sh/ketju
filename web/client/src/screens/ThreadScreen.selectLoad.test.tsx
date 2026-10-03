import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ThreadPage } from '../api/dto';
import { viewer } from '../state/viewer';
import { renderAt, route, stubServer, threadPage, unmount } from '../testing';
import moderator from '../../../../internal/dto/testdata/golden/me.moderator.json';

// Select posts while the MovePosts chunk is still loading (TKT-01M29CYSAM,
// Terva review 2057). Leaving the thread ends the selection, and that holds
// for a selection whose chunk had not arrived yet: when it does arrive, it
// starts nothing, so returning to the thread shows no selection bar. Here
// the chunk is held back until the test lets it through, which
// ThreadScreen.select.test.tsx cannot do, since it loads the chunk first.

const chunk = vi.hoisted(() => {
  let open = () => {};
  const arrived = new Promise<void>((r) => (open = r));
  return { arrived, open: () => open() };
});
vi.mock('../components/thread/MovePosts', async (importOriginal) => {
  await chunk.arrived;
  return importOriginal();
});

const saved = viewer.value;
const id = '01k4z3j0w8x7s';
const other = '01k4z3j0w8x7t';
const page = (): ThreadPage => {
  const p = threadPage({ id, slug: 'a', title: 'Drifting' });
  return { ...p, viewer: { ...p.viewer, can_moderate: true } };
};
beforeEach(() => {
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  stubServer({
    [`GET /api/v1/threads/${id}`]: () => ({ status: 200, body: page() }),
    [`GET /api/v1/threads/${other}`]: () => ({ status: 200, body: threadPage({ id: other, slug: 'b', title: 'Other' }) }),
  });
  viewer.value = structuredClone(moderator) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
const bar = () => document.querySelector<HTMLElement>('[data-selection-bar]');

describe('selecting posts while the chunk loads', () => {
  it('starts nothing once the thread was left before the chunk arrived', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await act(async () => root.querySelector<HTMLButtonElement>('[data-shell=toolbar] [data-action=select-posts]')!.click());
    await wait();
    expect(bar()).toBeNull();
    await route(`/t/${other}/b`);
    await wait();
    chunk.open();
    await wait();
    await route(`/t/${id}/a`);
    await wait();
    expect(bar()).toBeNull();
    expect(root.querySelectorAll('[data-select-post]')).toHaveLength(0);
    // Asked again, on the thread, it starts.
    await act(async () => root.querySelector<HTMLButtonElement>('[data-shell=toolbar] [data-action=select-posts]')!.click());
    await wait();
    expect(bar()).not.toBeNull();
  });
});
