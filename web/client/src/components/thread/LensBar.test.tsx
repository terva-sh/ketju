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
