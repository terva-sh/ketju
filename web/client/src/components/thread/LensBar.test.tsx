import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../../api/dto';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import { isPhone } from '../../state/layout';
import { resetReadTracker } from '../../state/readTracker';
import { viewer } from '../../state/viewer';
import { lensHome as home, renderAt, serveLenses as serve, settle, unmount, watchObservers as watch } from '../../testing';

// The lens bar and its switcher (TKT-01M29CTY71, docs/06 "the lens bar").

const saved = viewer.value;
const tz = process.env.TZ;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  resetReadTracker();
  watch();
  Element.prototype.scrollIntoView = () => {};
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  if (tz === undefined) delete process.env.TZ;
  else process.env.TZ = tz;
  vi.unstubAllGlobals();
  resetReadTracker();
});

const lensButton = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-lens-switch]')!;
const menu = (root: HTMLElement) => root.querySelector<HTMLElement>('#lens-menu');
const items = (root: HTMLElement) =>
  [...root.querySelectorAll<HTMLAnchorElement>('#lens-menu a')].map((a) => [a.textContent, a.getAttribute('href')]);
const open = async (root: HTMLElement) => act(async () => lensButton(root).click());

describe('the lens bar', () => {
  // AC2: Only <handle>, one press from the thread head, at both widths.
  it('offers Only <thread author> as the author lens address, under the thread head', async () => {
    for (const phone of [false, true]) {
      isPhone.value = phone;
      serve();
      const root = await renderAt(`${home}?page=1`);
      const only = root.querySelector<HTMLAnchorElement>('.thread-head [data-lens-only]')!;
      expect(only.textContent).toBe('Only carol');
      expect(only.getAttribute('href')).toBe(`${home}?lens=author:carol`);
      expect(only.hasAttribute('aria-current')).toBe(false);
    }
  });

  it('marks Only current while the author lens is on', async () => {
    serve();
    const root = await renderAt(`${home}?lens=author:Carol`);
    expect(root.querySelector('[data-lens-only]')!.getAttribute('aria-current')).toBe('true');
  });

  it('offers every lens to a signed-in member, and removed posts only to a moderator', async () => {
    serve();
    let root = await renderAt(`${home}?page=1`);
    await open(root);
    expect(lensButton(root).getAttribute('aria-expanded')).toBe('true');
    expect(items(root)).toEqual([
      ['Full thread', home],
      ['Unread', `${home}?lens=unread`],
      ['By carol', `${home}?lens=author:carol`],
      ['By alice', `${home}?lens=author:alice`],
      ['Mentioning me', `${home}?lens=mentions`],
    ]);
    expect(menu(root)!.querySelector('input[type=date]')).not.toBeNull();

    serve({ moderator: true });
    root = await renderAt(`${home}?page=1`);
    await open(root);
    expect(items(root).at(-1)).toEqual(['Removed posts', `${home}?lens=mod`]);
  });

  it('offers a visitor no unread and no mentions', async () => {
    viewer.value = { ...structuredClone(member), user: null } as Me;
    serve();
    const root = await renderAt(`${home}?page=1`);
    await open(root);
    expect(items(root).map(([t]) => t)).toEqual(['Full thread', 'By carol', 'By alice']);
  });

  it('marks the lens on screen current, a held unread included', async () => {
    serve({ lastRead: 20 });
    const root = await renderAt(`${home}?lens=unread:20`);
    await open(root);
    const current = [...root.querySelectorAll('#lens-menu a[aria-current]')].map((a) => a.textContent);
    expect(current).toEqual(['Unread']);
  });

  it('closes on Escape and on a press elsewhere, focus going back to Lens', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await open(root);
    root.querySelector<HTMLAnchorElement>('#lens-menu a')!.focus();
    await act(async () => void document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    expect(menu(root)).toBeNull();
    expect(lensButton(root).getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(lensButton(root));

    await open(root);
    root.querySelector<HTMLAnchorElement>('#lens-menu a')!.focus();
    await act(async () => void document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })));
    expect(menu(root)).toBeNull();
    expect(document.activeElement).toBe(lensButton(root));
  });

  it('closes when an item is chosen', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await open(root);
    await act(async () => root.querySelector<HTMLAnchorElement>('#lens-menu a[data-lens-item="author:alice"]')!.click());
    await settle(root);
    expect(location.pathname + location.search).toBe(`${home}?lens=author:alice`);
    expect(menu(root)).toBeNull();
  });
});

// AC10: the date field writes since:YYYY-MM-DD as chosen, read as midnight
// UTC, whatever the viewer's timezone.
describe('since a date', () => {
  for (const zone of ['America/Los_Angeles', 'Pacific/Auckland']) {
    it(`writes the chosen day unchanged in ${zone}`, async () => {
      process.env.TZ = zone;
      serve();
      const root = await renderAt(`${home}?page=1`);
      await open(root);
      const field = menu(root)!.querySelector<HTMLInputElement>('input[type=date]')!;
      field.value = '2026-09-01';
      await act(async () => void field.dispatchEvent(new Event('input', { bubbles: true })));
      await act(async () => menu(root)!.querySelector<HTMLFormElement>('form')!.requestSubmit());
      await settle(root);
      expect(location.pathname + location.search).toBe(`${home}?lens=since:2026-09-01`);
      expect(menu(root)).toBeNull();
    });

    it(`shows a since lens’s moment as its UTC day in ${zone}`, async () => {
      process.env.TZ = zone;
      // Midnight UTC on 1 September is 31 August in Los Angeles.
      expect(new Date(Date.UTC(2026, 8, 1)).getDate()).toBe(zone === 'America/Los_Angeles' ? 31 : 1);
      serve();
      const root = await renderAt(`${home}?lens=since:${Date.UTC(2026, 8, 1)}`);
      await open(root);
      expect(menu(root)!.querySelector<HTMLInputElement>('input[type=date]')!.value).toBe('2026-09-01');
    });
  }

  it('goes nowhere without a day', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await open(root);
    await act(async () => menu(root)!.querySelector<HTMLFormElement>('form')!.requestSubmit());
    expect(location.pathname + location.search).toBe(`${home}?page=1`);
  });
});

// The phone's lens sheet (TKT-01M29CGBZD, docs/06 "Mobile", "Pages"): Lens
// opens the switcher's own list in a modal sheet.
describe('the lens sheet', () => {
  const sheetButton = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-lens-sheet]')!;
  const sheet = () => document.querySelector<HTMLElement>('.sheet[role=dialog]');
  const openSheet = async (root: HTMLElement) => act(async () => sheetButton(root).click());
  const listed = () =>
    [...sheet()!.querySelectorAll<HTMLAnchorElement>('#lens-menu a')].map((a) => [a.textContent, a.getAttribute('href')]);
  const press = (k: string) =>
    act(async () => void document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })));

  beforeEach(() => {
    isPhone.value = true;
  });

  it('lists exactly the lenses the switcher offers this viewer', async () => {
    for (const moderator of [false, true]) {
      serve({ moderator });
      const root = await renderAt(`${home}?page=1`);
      await open(root);
      const offered = items(root);
      await act(async () => lensButton(root).click());
      expect(sheetButton(root).getAttribute('aria-haspopup')).toBe('dialog');
      expect(sheetButton(root).getAttribute('aria-expanded')).toBe('false');
      await openSheet(root);
      expect(sheetButton(root).getAttribute('aria-expanded')).toBe('true');
      expect(sheet()!.getAttribute('aria-modal')).toBe('true');
      expect(listed()).toEqual(offered);
      expect(listed().some(([t]) => t === 'Removed posts')).toBe(moderator);
      expect(sheet()!.querySelector('input[type=date]')).not.toBeNull();
      // Focus starts on the first lens.
      expect(document.activeElement).toBe(sheet()!.querySelector('#lens-menu a'));
    }
  });

  it('marks the lens on screen current', async () => {
    serve();
    const root = await renderAt(`${home}?lens=author:alice`);
    await openSheet(root);
    expect([...sheet()!.querySelectorAll('#lens-menu a[aria-current]')].map((a) => a.textContent)).toEqual(['By alice']);
  });

  it('goes to a chosen lens and closes', async () => {
    serve();
    const root = await renderAt(`${home}?page=2`);
    await openSheet(root);
    await act(async () => sheet()!.querySelector<HTMLAnchorElement>('[data-lens-item=mentions]')!.click());
    await settle(root);
    expect(location.pathname + location.search).toBe(`${home}?lens=mentions`);
    expect(sheet()).toBeNull();
    expect(root.querySelector('.lens-header')).not.toBeNull();
  });

  it('goes to a date through the since lens', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await openSheet(root);
    const field = sheet()!.querySelector<HTMLInputElement>('input[type=date]')!;
    field.value = '2026-09-01';
    await act(async () => void field.dispatchEvent(new Event('input', { bubbles: true })));
    await act(async () => field.form!.requestSubmit());
    await settle(root);
    expect(location.pathname + location.search).toBe(`${home}?lens=since:2026-09-01`);
    expect(sheet()).toBeNull();
  });

  it('closes on Escape, the close button and the backdrop, focus going back to Lens', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await openSheet(root);
    await press('Escape');
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(sheetButton(root));

    await openSheet(root);
    await act(async () => sheet()!.querySelector<HTMLButtonElement>('.picker-close')!.click());
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(sheetButton(root));

    await openSheet(root);
    await act(async () => void document.querySelector('.sheet-backdrop')!.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(sheet()).toBeNull();
    expect(document.activeElement).toBe(sheetButton(root));
    expect(sheetButton(root).getAttribute('aria-expanded')).toBe('false');
  });

  it('keeps Tab inside', async () => {
    serve();
    const root = await renderAt(`${home}?page=1`);
    await openSheet(root);
    const controls = [...sheet()!.querySelectorAll<HTMLElement>('a[href], button, input')];
    controls.at(-1)!.focus();
    await press('Tab');
    expect(document.activeElement).toBe(controls[0]);
  });
});
