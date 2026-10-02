import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, SearchResults } from '../api/dto';
import { helpOpen, setDeviceShortcuts, shortcuts } from '../keyboard/shortcuts';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { renderAt, screenOf, settle, stubServer, unmount } from '../testing';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import forumsGolden from '../../../../internal/dto/testdata/golden/forums.member.json';
import searchGolden from '../../../../internal/dto/testdata/golden/search.member.json';

// / opens search on every desktop screen with the top bar (TKT-01M3V7GE59,
// docs/06 "Paged, not infinite"): the shell installs it once. Each key is
// dispatched outside act, one microtask after the screen is drawn, as a
// reader's would be; the listener is a layout effect's, so it is already
// there.

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
  isPhone.value = false;
  helpOpen.value = false;
  stubServer({
    'GET /api/v1/forums': () => ({ status: 200, body: forumsGolden }),
    'GET /api/v1/search?q=bill': () => ({ status: 200, body: structuredClone(searchGolden) as SearchResults }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  helpOpen.value = false;
  setDeviceShortcuts(true);
  vi.unstubAllGlobals();
});

// slash presses / on target outside act, one microtask on, and answers
// the event, so a test can see whether it was taken.
async function slash(target: Element = document.activeElement ?? document.body) {
  await Promise.resolve();
  const e = new KeyboardEvent('keydown', { key: '/', bubbles: true, cancelable: true });
  target.dispatchEvent(e);
  return e;
}

const query = () => document.getElementById('search-q') as HTMLInputElement | null;

describe('/ opens search', () => {
  it('/ on the forum index routes to /search and the field has focus', async () => {
    const root = await renderAt('/');
    expect(screenOf(root)).toBe('forum-index');
    const e = await slash(document.body);
    expect(e.defaultPrevented).toBe(true);
    await settle(root);
    expect(location.pathname).toBe('/search');
    expect(document.activeElement).toBe(query());
  });

  it('/ on /search with a query focuses the field and selects its text', async () => {
    const root = await renderAt('/search?q=bill');
    const q = query()!;
    expect(q.value).toBe('bill');
    // Focus somewhere else on the screen, as after reading the results.
    document.body.focus();
    expect(document.activeElement).not.toBe(q);
    const e = await slash(document.body);
    expect(e.defaultPrevented).toBe(true);
    await settle(root);
    expect(location.pathname + location.search).toBe('/search?q=bill');
    expect(document.activeElement).toBe(q);
    expect([q.selectionStart, q.selectionEnd]).toEqual([0, 4]);
  });

  it('/ typed in the composer, in the search field or in a dialog is left alone', async () => {
    const root = await renderAt('/search?q=bill');
    const typed = await slash(query()!);
    expect(typed.defaultPrevented).toBe(false);

    const box = document.createElement('div');
    box.innerHTML = '<div class="composer"><textarea></textarea></div><div role="dialog"><button>x</button></div>';
    document.body.append(box);
    try {
      for (const target of [box.querySelector('textarea')!, box.querySelector('[role=dialog] button')!]) {
        const e = await slash(target);
        expect(e.defaultPrevented).toBe(false);
      }
    } finally {
      box.remove();
    }
    await settle(root);
    expect(location.pathname + location.search).toBe('/search?q=bill');
  });

  it.each([
    [
      'a member',
      () => (viewer.value = { ...viewer.value, settings: { ...viewer.value.settings, shortcuts: false } }),
    ],
    [
      'a signed-out reader',
      () => {
        viewer.value = { ...viewer.value, user: null } as Me;
        setDeviceShortcuts(false);
      },
    ],
  ])('with the shortcuts off, / does nothing, for %s', async (_who, turnOff) => {
    turnOff();
    const root = await renderAt('/');
    const e = await slash(document.body);
    expect(e.defaultPrevented).toBe(false);
    await settle(root);
    expect(location.pathname).toBe('/');
  });

  it('on a phone, / does nothing', async () => {
    isPhone.value = true;
    const root = await renderAt('/');
    const e = await slash(document.body);
    expect(e.defaultPrevented).toBe(false);
    await settle(root);
    expect(location.pathname).toBe('/');
  });

  it('with the shortcut list open, / is the list’s', async () => {
    const root = await renderAt('/');
    await act(async () => void (helpOpen.value = true));
    const e = await slash(document.body);
    expect(e.defaultPrevented).toBe(false);
    await settle(root);
    expect(location.pathname).toBe('/');
  });

  it('the list has a / row naming the top bar’s Search', () => {
    const row = shortcuts.find((s) => s.keys[0] === '/');
    expect(row).toEqual({ keys: ['/'], does: 'Search', control: 'Search, in the top bar' });
  });
});
