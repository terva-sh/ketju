import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { ShellAction } from '../state/shell';
import { MoreMenu } from './MoreMenu';

// The phone top bar's more menu takes its keys from the press that opened
// it, and lets them go in the commit that closes it (TKT-01M3TV4HBF). A
// plain effect runs a frame later at both ends: an Escape pressed straight
// after opening was lost, and one pressed straight after choosing an item
// was taken by the closed menu, which put focus on More. The presses are
// dispatched outside act, which would run the effects and hide the race.

let root: HTMLElement;
// elsewhere is what an item's action puts focus on, as the lock panel puts
// it in its reason.
let elsewhere: HTMLButtonElement;

beforeEach(() => {
  root = document.createElement('div');
  elsewhere = document.createElement('button');
  elsewhere.textContent = 'elsewhere';
  document.body.replaceChildren(root, elsewhere);
});
afterEach(async () => {
  await act(async () => render(null, root));
});

const actions: ShellAction[] = [
  { id: 'lock', label: 'Lock', run: () => elsewhere.focus() },
  { id: 'pin', label: 'Pin', run: () => {} },
];

const more = () => root.querySelector<HTMLButtonElement>('.more > button')!;
const menu = () => root.querySelector('[data-shell=more-menu]');
const item = (id: string) => root.querySelector<HTMLButtonElement>(`[data-action=${id}]`)!;
const escape = () => document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
// settle lets the raced frame run before the next test.
const settle = () => act(() => new Promise((r) => setTimeout(r, 50)));

describe('the more menu', () => {
  it('Escape one microtask after opening closes the menu and focuses More', async () => {
    await act(async () => render(<MoreMenu actions={actions} />, root));
    more().focus();
    try {
      more().click();
      await Promise.resolve();
      expect(menu()).not.toBeNull();
      expect(document.activeElement).toBe(item('lock'));
      escape();
      await Promise.resolve();
      expect(menu()).toBeNull();
      expect(document.activeElement).toBe(more());
    } finally {
      await settle();
    }
  });

  it('after choosing an item, its listeners are gone: a later Escape leaves focus where the item put it', async () => {
    await act(async () => render(<MoreMenu actions={actions} />, root));
    await act(async () => more().click());
    try {
      item('lock').click();
      await Promise.resolve();
      expect(menu()).toBeNull();
      expect(document.activeElement).toBe(elsewhere);
      escape();
      await Promise.resolve();
      expect(document.activeElement).toBe(elsewhere);
    } finally {
      await settle();
    }
  });

  it('Escape already taken (defaultPrevented) does not move focus', async () => {
    await act(async () => render(<MoreMenu actions={actions} />, root));
    await act(async () => more().click());
    expect(document.activeElement).toBe(item('lock'));
    // A dialog that listens in the capture phase, as useModal does, hears
    // the key first and takes it.
    const take = (e: KeyboardEvent) => e.key === 'Escape' && e.preventDefault();
    document.addEventListener('keydown', take, true);
    try {
      await act(async () => void escape());
      expect(menu()).not.toBeNull();
      expect(document.activeElement).toBe(item('lock'));
    } finally {
      document.removeEventListener('keydown', take, true);
    }
    // One it does take, it marks taken.
    const ev = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    await act(async () => void item('lock').dispatchEvent(ev));
    expect(ev.defaultPrevented).toBe(true);
    expect(menu()).toBeNull();
    expect(document.activeElement).toBe(more());
  });
});
