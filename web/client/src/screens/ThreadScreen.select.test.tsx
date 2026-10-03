import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, MergedPosts, MovedPosts, ThreadPage } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { preloadMovePosts, renderAt, route, stubServer, threadPage, unmount, type StubServer } from '../testing';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import moderator from '../../../../internal/dto/testdata/golden/me.moderator.json';

// Select posts on the thread page (TKT-01M29CYSAM; docs/06 "Moderator flow
// for drift"): offered to a viewer who moderates the thread, it loads the
// MovePosts chunk, puts a checkbox on each post shown as itself, and the
// selection bar at the foot of the screen. The selection holds across the
// thread's pages and ends on Escape, on Cancel, and on leaving the thread.

const saved = viewer.value;
const id = '01k4z3j0w8x7s';
const other = '01k4z3j0w8x7t';
const page = (n: number, o: Partial<ThreadPage['viewer']> = {}): ThreadPage => {
  const p = threadPage({ id, slug: 'a', title: 'Drifting' });
  return { ...p, page: { ...p.page, number: n }, viewer: { ...p.viewer, can_moderate: true, ...o } };
};
let server: StubServer;
let canModerate: boolean;
beforeEach(async () => {
  await preloadMovePosts();
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  canModerate = true;
  const moved: MovedPosts = {
    source: { ...page(1).thread, log_length: page(1).thread.log_length + 10 },
    thread: { ...page(1).thread, id: '01k4z3j0w8x7u', slug: 'split-out', title: 'Split out' },
  };
  server = stubServer({
    [`GET /api/v1/threads/${id}`]: () => ({ status: 200, body: page(1, { can_moderate: canModerate }) }),
    [`GET /api/v1/threads/${id}?page=2`]: () => ({ status: 200, body: page(2) }),
    [`GET /api/v1/threads/${other}`]: () => ({ status: 200, body: threadPage({ id: other, slug: 'b', title: 'Other' }) }),
    [`GET /api/v1/threads/01k4z3j0w8x7u`]: () => ({ status: 200, body: threadPage(moved.thread) }),
    'GET /api/v1/forums': () => ({ status: 200, body: { forums: [{ slug: 'general', name: 'General', archived: false, children: [] }] } }),
    [`POST /api/v1/admin/threads/${id}/split`]: () => ({ status: 200, body: moved }),
  });
  viewer.value = structuredClone(moderator) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
});

const action = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-shell=toolbar] [data-action=select-posts]');
const boxes = (root: HTMLElement) =>
  [...root.querySelectorAll<HTMLInputElement>('[data-select-post]')].map((b) => Number(b.getAttribute('data-select-post')));
const bar = () => document.querySelector<HTMLElement>('[data-selection-bar]');
const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
async function select(root: HTMLElement) {
  await act(async () => action(root)!.click());
  await wait();
}

describe('selecting posts on the thread page', () => {
  it('is offered to a viewer who moderates the thread, and to nobody else', async () => {
    let root = await renderAt(`/t/${id}/a`);
    expect(action(root)).not.toBeNull();
    canModerate = false;
    root = await renderAt(`/t/${id}/a`);
    expect(action(root)).toBeNull();
    viewer.value = structuredClone(member) as Me;
    root = await renderAt(`/t/${id}/a`);
    expect(action(root)).toBeNull();
  });

  it('boxes only the posts shown as themselves, and holds the selection across pages', async () => {
    const root = await renderAt(`/t/${id}/a`);
    expect(boxes(root)).toEqual([]);
    await select(root);
    // 63 is a tombstone and 64 was moved: neither moves.
    expect(boxes(root)).toEqual([61, 62]);
    expect(bar()).not.toBeNull();
    // Offered once: it is on.
    expect(action(root)).toBeNull();
    await act(async () => root.querySelector<HTMLInputElement>('[data-select-post="61"]')!.click());
    expect(bar()!.querySelector('[data-selection-count]')!.textContent).toBe('1 post selected');
    await route(`/t/${id}/a?page=2`);
    await wait();
    expect(bar()!.querySelector('[data-selection-count]')!.textContent).toBe('1 post selected');
    expect(root.querySelector<HTMLInputElement>('[data-select-post="61"]')!.checked).toBe(true);
    // Escape ends it.
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    expect(bar()).toBeNull();
    expect(boxes(root)).toEqual([]);
    expect(action(root)).not.toBeNull();
  });

  it('ends on leaving the thread', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await select(root);
    expect(bar()).not.toBeNull();
    await route(`/t/${other}/b`);
    await wait();
    expect(bar()).toBeNull();
    expect(boxes(root)).toEqual([]);
  });

  it('takes the reply bar’s place on a phone', async () => {
    isPhone.value = true;
    const root = await renderAt(`/t/${id}/a`);
    expect(root.querySelector('.replybar')).not.toBeNull();
    await act(async () => root.querySelector<HTMLButtonElement>('.more > button')!.click());
    await act(async () => root.querySelector<HTMLButtonElement>('[data-shell=more-menu] [data-action=select-posts]')!.click());
    await wait();
    expect(bar()).not.toBeNull();
    expect(root.querySelector('.replybar')).toBeNull();
  });

  it('splits the selection and goes to the new thread', async () => {
    const root = await renderAt(`/t/${id}/a`);
    await select(root);
    await act(async () => root.querySelector<HTMLInputElement>('[data-select-post="62"]')!.click());
    await act(async () => bar()!.querySelector<HTMLButtonElement>('.selection-split')!.click());
    await wait();
    const d = document.querySelector<HTMLElement>('[data-split-dialog]')!;
    const t = d.querySelector<HTMLInputElement>('input[type=text]')!;
    t.value = 'Split out';
    await act(async () => void t.dispatchEvent(new Event('input', { bubbles: true })));
    await act(async () => d.querySelector<HTMLButtonElement>('.flag-send')!.click());
    await wait();
    await wait();
    expect(server.sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([
      { ordinals: [62], title: 'Split out', forum: 'general' },
    ]);
    expect(location.pathname).toBe('/t/01k4z3j0w8x7u/split-out');
    expect(bar()).toBeNull();
    expect(document.querySelector('[data-split-dialog]')).toBeNull();
  });

  // TKT-01M29CYSBA.
  it('merges the selection into another thread and goes to the first copy', async () => {
    const target = { ...threadPage({ id: other, slug: 'b', title: 'Other' }).thread, post_count: 12, log_length: 99999 };
    const merged: MergedPosts = { source: { ...page(1).thread, log_length: page(1).thread.log_length + 10 }, target, first_copy: 11 };
    server.routes[`GET /api/v1/threads/${other}?page=1`] = () => ({ status: 200, body: threadPage({ id: other, slug: 'b', title: 'Other' }) });
    server.routes[`POST /api/v1/admin/threads/${id}/merge`] = () => ({ status: 200, body: merged });
    const root = await renderAt(`/t/${id}/a`);
    await select(root);
    await act(async () => root.querySelector<HTMLInputElement>('[data-select-post="61"]')!.click());
    await act(async () => root.querySelector<HTMLInputElement>('[data-select-post="62"]')!.click());
    await act(async () => bar()!.querySelector<HTMLButtonElement>('.selection-merge')!.click());
    await wait();
    const d = document.querySelector<HTMLElement>('[data-merge-dialog]')!;
    const t = d.querySelector<HTMLInputElement>('input[type=text]')!;
    t.value = `${location.origin}/t/${other}/b`;
    await act(async () => void t.dispatchEvent(new Event('input', { bubbles: true })));
    await wait();
    expect(d.querySelector('[data-merge-target]')!.textContent).toBe('Other in General');
    await act(async () => d.querySelector<HTMLButtonElement>('.flag-send')!.click());
    await wait();
    await wait();
    expect(server.sent.filter((r) => r.method === 'POST').map((r) => r.body)).toEqual([{ ordinals: [61, 62], into: other }]);
    expect(location.pathname).toBe(`/t/${other}/b/11`);
    expect(bar()).toBeNull();
    expect(document.querySelector('[data-merge-dialog]')).toBeNull();
  });
});
