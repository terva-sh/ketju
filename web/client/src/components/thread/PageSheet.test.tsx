import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, ThreadPage } from '../../api/dto';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import { isPhone } from '../../state/layout';
import { dwellMs, resetReadTracker } from '../../state/readTracker';
import { viewer } from '../../state/viewer';
import {
  dwellOn,
  lensHome as home,
  renderAt,
  serveLenses as serve,
  settle,
  threadPage,
  unmount,
  watchObservers as watch,
} from '../../testing';
import { sheetPages } from './PageSheet';

// The phone's page sheet (TKT-01M29CGBZD, docs/06 "Mobile", "Pages"). The
// lens thread is sixty posts, five to a page: twelve pages.

const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
  isPhone.value = true;
  resetReadTracker();
  watch();
  Element.prototype.scrollIntoView = () => {};
});
afterEach(async () => {
  vi.useRealTimers();
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
  resetReadTracker();
});

const where = () => location.pathname + location.search + location.hash;
const label = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('.pager-narrow .where')!;
const sheet = () => document.querySelector<HTMLElement>('.sheet[role=dialog]');
const pages = () => [...sheet()!.querySelectorAll<HTMLAnchorElement>('.page-grid a')];
const open = async (root: HTMLElement) => act(async () => label(root).click());
const key = (k: string, shift = false) =>
  act(async () => {
    (document.activeElement ?? document.body).dispatchEvent(
      new KeyboardEvent('keydown', { key: k, shiftKey: shift, bubbles: true, cancelable: true }),
    );
  });

describe('sheetPages', () => {
  it.each([
    [1, 1, [1]],
    [3, 9, [1, 2, 3, 4, 5, 6, 7, 8, 9]],
    [60, 60, Array.from({ length: 60 }, (_, i) => i + 1)],
    [1, 61, [1, ...Array.from({ length: 20 }, (_, i) => i + 2), null, 61]],
    [61, 61, [1, null, ...Array.from({ length: 20 }, (_, i) => i + 41), 61]],
    [22, 100, [1, ...Array.from({ length: 41 }, (_, i) => i + 2), null, 100]],
    [23, 100, [1, null, ...Array.from({ length: 41 }, (_, i) => i + 3), null, 100]],
    [2500, 5000, [1, null, ...Array.from({ length: 41 }, (_, i) => i + 2480), null, 5000]],
  ] as const)('draws page %i of %i as %j', (current, total, want) => {
    expect(sheetPages(current, total)).toEqual(want);
  });

  it('never draws more than 43 links', () => {
    for (const total of [61, 100, 5000])
      for (let current = 1; current <= total; current += 7)
        expect(sheetPages(current, total).filter((n) => n !== null).length).toBeLessThanOrEqual(43);
  });
});

describe('the page sheet', () => {
  it('opens from the pager label, a button', async () => {
    serve();
    const root = await renderAt(`${home}?page=3`);
    const b = label(root);
    expect(b.tagName).toBe('BUTTON');
    expect(b.textContent).toBe('Page 3 of 12');
    expect(b.getAttribute('aria-haspopup')).toBe('dialog');
    expect(b.getAttribute('aria-expanded')).toBe('false');
    await open(root);
    expect(b.getAttribute('aria-expanded')).toBe('true');
    expect(sheet()!.getAttribute('aria-modal')).toBe('true');
    expect(document.getElementById(sheet()!.getAttribute('aria-labelledby')!)!.textContent).toBe('Pages');
  });

  it('lists every page, marks the current one and starts on it', async () => {
    serve();
    const root = await renderAt(`${home}?page=3`);
    await open(root);
    expect(pages().map((a) => a.textContent)).toEqual(['1', '2', '3', '4', '5', '6', '7', '8', '9', '10', '11', '12']);
    const current = sheet()!.querySelectorAll('a[aria-current=page]');
    expect([...current].map((a) => a.textContent)).toEqual(['3']);
    expect(document.activeElement).toBe(current[0]);
    // Twelve pages draw no Go to page field.
    expect(sheet()!.querySelector('input[type=number]')).toBeNull();
  });

  it('keeps the lens in every page link, and a chosen page goes there and closes', async () => {
    serve();
    const root = await renderAt(`${home}?page=1&lens=author:alice`);
    await open(root);
    expect(pages()[1].getAttribute('href')).toBe(`${home}?page=2&lens=author:alice`);
    await act(async () => pages()[1].click());
    await settle(root);
    expect(where()).toBe(`${home}?page=2&lens=author:alice`);
    expect(sheet()).toBeNull();
  });

  it('bounds a long thread, and Go to page reaches the rest', async () => {
    const t = { id: '01k4z3j0w8xl9', slug: 'long', title: 'Long' };
    const long = (n: number): ThreadPage =>
      threadPage(t, { lens: null, page: { number: n, size: 25, total_pages: 5000 }, thread: { post_count: 125000 } as ThreadPage['thread'] });
    vi.stubGlobal('fetch', async (url: string) => {
      const u = new URL(url, 'http://x');
      if (u.pathname === `/api/v1/threads/${t.id}`) return new Response(JSON.stringify(long(Number(u.searchParams.get('page')))));
      return new Response(null, { status: 204, headers: { 'Draft-Revision': '0' } });
    });
    const root = await renderAt(`/t/${t.id}/long?page=2500`);
    await open(root);
    const nums = pages().map((a) => Number(a.textContent));
    expect(nums).toEqual([1, ...Array.from({ length: 41 }, (_, i) => i + 2480), 5000]);
    const field = sheet()!.querySelector<HTMLInputElement>('input[type=number]')!;
    expect([field.min, field.max, field.required]).toEqual(['1', '5000', true]);
    const go = async (v: string) => {
      field.value = v;
      await act(async () => void field.dispatchEvent(new Event('input', { bubbles: true })));
      await act(async () => void field.form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
    };
    for (const bad of ['0', '5001', '2.5', '']) {
      await go(bad);
      expect(where()).toBe(`/t/${t.id}/long?page=2500`);
      expect(sheet()).not.toBeNull();
    }
    await go('17');
    await settle(root);
    expect(where()).toBe(`/t/${t.id}/long?page=17`);
    expect(sheet()).toBeNull();
  });

  it('offers First unread only with unread posts', async () => {
    serve({ lastRead: 60 });
    let root = await renderAt(`${home}?page=12`);
    await open(root);
    expect(sheet()!.querySelector('[data-sheet-first-unread]')).toBeNull();

    serve({ lastRead: 10 });
    root = await renderAt(`${home}?page=1`);
    await open(root);
    expect(sheet()!.querySelector('[data-sheet-first-unread]')).not.toBeNull();

    viewer.value = { ...structuredClone(member), user: null } as Me;
    serve();
    root = await renderAt(`${home}?page=1`);
    await open(root);
    expect(sheet()!.querySelector('[data-sheet-first-unread]')).toBeNull();
  });

  // The read-tracking requirements carried onto TKT-01M29CTY71: First unread
  // goes to the position now plus one, within a held unread lens, and out of
  // one whose boundary is above the position.
  it('goes to the post after the position as read since, and keeps focus on it', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:10`);
    vi.useFakeTimers();
    dwellOn(13, dwellMs);
    vi.useRealTimers();
    await open(root);
    await act(async () => sheet()!.querySelector<HTMLButtonElement>('[data-sheet-first-unread]')!.click());
    expect(sheet()).toBeNull();
    expect(document.activeElement?.id).toBe('post-14');

    vi.useFakeTimers();
    dwellOn(15, dwellMs);
    vi.useRealTimers();
    await open(root);
    await act(async () => sheet()!.querySelector<HTMLButtonElement>('[data-sheet-first-unread]')!.click());
    expect(where()).toBe(`${home}/16?lens=unread:10`);
    expect(sheet()).toBeNull();
    await settle(root);
    // The permalink's arrival puts the reader there; a phone moves no focus
    // to a post on another page (useThreadShortcuts).
    expect([...root.querySelectorAll<HTMLElement>('.posts > [id^="post-"]')].map((e) => e.id)[0]).toBe('post-16');
  });

  it('leaves an unread lens whose boundary is above the position', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?lens=unread:20`);
    await open(root);
    await act(async () => sheet()!.querySelector<HTMLButtonElement>('[data-sheet-first-unread]')!.click());
    expect(where()).toBe(`${home}/11`);
  });

  it('jumps to a date through the since lens', async () => {
    serve();
    const root = await renderAt(`${home}?page=2`);
    await open(root);
    const field = sheet()!.querySelector<HTMLInputElement>('input[type=date]')!;
    expect(sheet()!.querySelector(`label[for="${field.id}"]`)!.textContent).toBe('Jump to date');
    field.value = '2026-09-01';
    await act(async () => void field.dispatchEvent(new Event('input', { bubbles: true })));
    await act(async () => field.form!.requestSubmit());
    await settle(root);
    expect(where()).toBe(`${home}?lens=since:2026-09-01`);
    expect(sheet()).toBeNull();
  });

  it('keeps Tab inside, and Escape, the close button and the backdrop return focus to the label', async () => {
    serve({ lastRead: 10 });
    const root = await renderAt(`${home}?page=3`);
    // Escape.
    await open(root);
    await key('Escape');
    expect(sheet()).toBeNull();
    expect(label(root).getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(label(root));
    // Tab from the last control goes to the first, Shift+Tab back.
    await open(root);
    const controls = [...sheet()!.querySelectorAll<HTMLElement>('a[href], button, input')];
    controls.at(-1)!.focus();
    await key('Tab');
    expect(document.activeElement).toBe(controls[0]);
    await key('Tab', true);
    expect(document.activeElement).toBe(controls.at(-1));
    // The close button.
    await act(async () => sheet()!.querySelector<HTMLButtonElement>('.picker-close')!.click());
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(label(root));
    // The backdrop.
    await open(root);
    await act(async () => void document.querySelector('.sheet-backdrop')!.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(label(root));
  });
});
