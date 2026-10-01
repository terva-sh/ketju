import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import { isPhone } from '../state/layout';
import { dwellMs, resetReadTracker } from '../state/readTracker';
import { viewer } from '../state/viewer';
import {
  dwellOn,
  lensHome as home,
  renderAt,
  route,
  serveLenses as serve,
  settle,
  trackerObservers as trackers,
  unmount,
  watchObservers as watch,
} from '../testing';

// The thread screen under a lens (TKT-01M29CTY71): the address follows the
// answer's lens, the read beacon runs only where it may, the dividers stand
// at the gaps, Conversation, and First unread under the unread lens.

const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  resetReadTracker();
  watch();
  Element.prototype.scrollIntoView = () => {};
});
afterEach(async () => {
  // Unmounted while fetch is still the stub: leaving the page flushes the
  // tracker and may ask for more.
  vi.useRealTimers();
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
  resetReadTracker();
});

const where = () => location.pathname + location.search + location.hash;
const ordinals = (root: HTMLElement) =>
  [...root.querySelectorAll<HTMLElement>('.posts > [id^="post-"]')].map((e) => Number(e.dataset.ordinal));

describe('the address follows the answer', () => {
  // AC3: a bare unread is held at its boundary.
  it('replaces a bare unread with unread:B, keeping the page and the hash', async () => {
    serve({ lastRead: 25 });
    const root = await renderAt(`${home}?page=2&lens=unread#post-33`);
    await settle(root);
    expect(where()).toBe(`${home}?page=2&lens=unread:25#post-33`);
    expect(ordinals(root)).toEqual([31, 32, 33, 34, 35]);
    // Page links carry the held boundary.
    const next = root.querySelector<HTMLAnchorElement>('.pager a[rel=next]')!;
    expect(new URL(next.href).searchParams.get('lens')).toBe('unread:25');
  });

  it('holds the posts of a held boundary after the reader moves on', async () => {
    const s = serve({ lastRead: 25 });
    let root = await renderAt(`${home}?lens=unread`);
    await settle(root);
    expect(where()).toBe(`${home}?lens=unread:25`);
    // Page 1 read: the position is now 30, and page 2 is still 31 to 35.
    s.lastRead = 30;
    root = await renderAt(`${home}?page=2&lens=unread:25`);
    expect(ordinals(root)).toEqual([31, 32, 33, 34, 35]);
  });

  it('drops a lens the server answered as the full thread', async () => {
    serve();
    const root = await renderAt(`${home}?page=2&lens=nope`);
    await settle(root);
    expect(where()).toBe(`${home}?page=2`);
    expect(root.querySelector('.lens-header')).toBeNull();
  });

  // AC8: every switch sets the lens and drops the page and the ordinal, and
  // a fresh load of the address shows the same posts.
  it('switches drop the page and the ordinal, and a fresh load shows the same posts', async () => {
    serve({ over: { 7: { reply_to: 3 } } });
    let root = await renderAt(`${home}/7?page=2`);
    const only = root.querySelector<HTMLAnchorElement>('[data-lens-only]')!;
    expect(only.getAttribute('href')).toBe(`${home}?lens=author:carol`);
    expect(root.querySelector('#post-7 [data-conversation]')?.getAttribute('href')).toBe(`${home}?lens=chain:7`);
    await act(async () => root.querySelector<HTMLButtonElement>('[data-lens-switch]')!.click());
    for (const a of root.querySelectorAll<HTMLAnchorElement>('#lens-menu a')) {
      const href = a.getAttribute('href')!;
      expect(href.startsWith(`${home}?`) || href === home, href).toBe(true);
      expect(href).not.toMatch(/page=|ordinal=/);
    }
    await act(async () => only.click());
    await settle(root);
    expect(where()).toBe(`${home}?lens=author:carol`);
    const shown = ordinals(root);
    expect(shown).toEqual([1, 3, 5, 7, 9]);
    root = await renderAt(`${home}?lens=author:carol`);
    expect(ordinals(root)).toEqual(shown);
  });
});

// AC4, as Terva reviews 1730 and 1741 carried it: the beacon runs on the
// canonical view and under unread from a boundary at or below the reader's
// position, and under no other lens.
describe('the read beacon under a lens', () => {
  it('does not run under author, chain, mentions, moderator or since', async () => {
    for (const lens of ['author:carol', 'chain:30', 'mentions', 'mod', 'since:2026-09-01']) {
      serve({ lastRead: 0, moderator: true });
      watch();
      const root = await renderAt(`${home}?lens=${lens}`);
      expect(root.querySelector('.lens-header'), lens).not.toBeNull();
      expect(trackers(), lens).toEqual([]);
    }
  });

  it('runs under unread:B with B at or below the position', async () => {
    for (const b of [25, 10]) {
      serve({ lastRead: 25 });
      watch();
      await renderAt(`${home}?lens=unread:${b}`);
      expect(trackers(), String(b)).toHaveLength(1);
    }
  });

  it('sends what is read under unread:B at the position', async () => {
    const s = serve({ lastRead: 25 });
    const root = await renderAt(`${home}?lens=unread:25`);
    vi.useFakeTimers();
    dwellOn(28, dwellMs);
    vi.useRealTimers();
    await route('/f/general');
    await act(() => new Promise((r) => setTimeout(r, 5)));
    expect(s.puts).toEqual([28]);
    void root;
  });

  it('sends nothing under unread:B above the position', async () => {
    const s = serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:99`);
    expect(root.querySelector('.lens-header')).not.toBeNull();
    expect(trackers()).toEqual([]);
    await route('/f/general');
    await act(() => new Promise((r) => setTimeout(r, 5)));
    expect(s.puts).toEqual([]);
  });

  it('runs on the canonical view', async () => {
    serve({ lastRead: 0 });
    await renderAt(`${home}?page=1`);
    expect(trackers()).toHaveLength(1);
  });
});

describe('a lens page', () => {
  // AC1: a divider at every gap the server reports.
  it('draws a divider at every gap', async () => {
    serve();
    const root = await renderAt(`${home}?lens=author:carol`);
    const list = root.querySelector('.posts')!;
    const seq = [...list.children].map((e) => (e.classList.contains('lens-gap') ? `gap:${e.textContent}` : e.id));
    expect(seq).toEqual([
      'post-1',
      'gap:… 1 post …',
      'post-3',
      'gap:… 1 post …',
      'post-5',
      'gap:… 1 post …',
      'post-7',
      'gap:… 1 post …',
      'post-9',
      'gap:… 1 post …',
    ]);
    expect(list.querySelector('.lens-gap a')?.getAttribute('href')).toBe(`${home}/2`);
    expect(root.querySelector('[data-lens-count]')?.textContent).toBe('Showing 30 of 60 posts · 30 hidden');
  });

  it('draws the chain with its gaps', async () => {
    serve();
    const root = await renderAt(`${home}?lens=chain:30`);
    const list = root.querySelector('.posts')!;
    expect([...list.querySelectorAll('.lens-gap')].map((e) => e.textContent)).toEqual([
      '… 28 posts …',
      '… 24 posts …',
      '… 5 posts …',
    ]);
    expect(ordinals(root)).toEqual([1, 30, 55]);
  });

  it('offers Conversation on a reply and a replied-to post, and no other', async () => {
    serve({ over: { 3: { reply_count: 1 }, 4: { reply_to: 3, reply_to_suppressed: true } } });
    const root = await renderAt(`${home}?page=1`);
    const offered = [...root.querySelectorAll('.posts > .post')]
      .filter((p) => p.querySelector('[data-conversation]'))
      .map((p) => p.id);
    expect(offered).toEqual(['post-3', 'post-4']);
  });

  it('keeps no unread divider under a filtering lens', async () => {
    serve({ lastRead: 4 });
    const root = await renderAt(`${home}?lens=author:carol`);
    expect(root.querySelector('#unread')).toBeNull();
  });
});

// AC3 and Terva review 1730: First unread under the unread lens targets the
// reader's current position plus one, never B + 1.
describe('First unread under the unread lens', () => {
  const press = async (root: HTMLElement) =>
    act(async () => root.querySelector<HTMLButtonElement>('[data-action=first-unread]')!.click());

  it('goes to the post after the position as read since, on this lens page or the one holding it', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:10`);
    expect(ordinals(root)).toEqual([11, 12, 13, 14, 15]);
    vi.useFakeTimers();
    dwellOn(13, dwellMs);
    vi.useRealTimers();
    await press(root);
    expect(document.activeElement?.id).toBe('post-14');

    vi.useFakeTimers();
    dwellOn(15, dwellMs);
    vi.useRealTimers();
    await press(root);
    expect(where()).toBe(`${home}/16?lens=unread:10`);
    await settle(root);
    expect(ordinals(root)).toEqual([16, 17, 18, 19, 20]);
    expect(document.activeElement?.id).toBe('post-16');
  });

  it('and u does the same', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:10`);
    vi.useFakeTimers();
    dwellOn(15, dwellMs);
    vi.useRealTimers();
    await act(async () => {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'u', bubbles: true, cancelable: true }));
    });
    expect(where()).toBe(`${home}/16?lens=unread:10`);
    await settle(root);
    expect(document.activeElement?.id).toBe('post-16');
  });

  // Terva review 1792: a lens that starts above the reader's place leaves
  // their first unread post out, so it is reached in the full thread.
  it('leaves an unread lens whose boundary is above the position', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:20`);
    expect(ordinals(root)).toEqual([21, 22, 23, 24, 25]);
    await press(root);
    expect(where()).toBe(`${home}/11`);
    await settle(root);
    expect(root.querySelector('.lens-header')).toBeNull();
    expect(document.activeElement?.id).toBe('post-11');
    await act(async () => {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'u', bubbles: true, cancelable: true }));
    });
    expect(document.activeElement?.id).toBe('post-11');
  });

  it('goes to the canonical permalink from the full thread', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?page=1`);
    vi.useFakeTimers();
    dwellOn(5, dwellMs);
    vi.useRealTimers();
    await press(root);
    expect(where()).toBe(`${home}/11`);
  });
});
