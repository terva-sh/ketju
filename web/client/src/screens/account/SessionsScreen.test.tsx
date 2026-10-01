import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../../api/dto';
import { nav } from '../../leave';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type Answer } from '../../testing';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import sessions from '../../../../../internal/dto/testdata/golden/sessions.member.json';

// /settings/sessions (TKT-01M29CXK4E): the member's sessions, this device
// marked. Another session is signed out in place; this one, or every one,
// is signed out after a dialog, and then / is loaded in full (nav.leave),
// because this browser is signed out too (docs/05 "Sessions and CSRF",
// decided by the run 2026-10-01). The golden's first session is this one.

const here = sessions.sessions[0].id;
const other = sessions.sessions[1].id;

let saved: Me;
let leave: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
  leave = vi.spyOn(nav, 'leave').mockImplementation(() => {});
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const url = '/api/v1/settings/sessions';

function serve(routes: Record<string, () => Answer> = {}) {
  return stubServer({ [`GET ${url}`]: () => ({ status: 200, body: structuredClone(sessions) }), ...routes });
}

const rows = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('[data-session]')].map((r) => r.dataset.session);
const signOut = (root: HTMLElement, id: string) => root.querySelector<HTMLButtonElement>(`[data-session="${id}"] button`)!;

async function press(root: HTMLElement, el: HTMLElement) {
  await act(async () => el.click());
  await settle(root);
}

describe('session management', () => {
  it('sessions list marks this device and signs out another row in place', async () => {
    const s = serve({ [`DELETE ${url}/${other}`]: () => ({ status: 204 }) });
    const root = await renderAt('/settings/sessions');
    expect(screenOf(root)).toBe('settings-sessions');
    expect(rows(root)).toEqual([here, other]);
    const mine = root.querySelector(`[data-session="${here}"]`)!;
    expect(mine.textContent).toContain('This device');
    expect(mine.textContent).toContain('Firefox on Linux');
    expect(mine.textContent).toContain('192.0.2.10');
    expect(mine.textContent).toContain('github');
    expect(mine.querySelectorAll('time')).toHaveLength(2);
    expect(root.querySelector(`[data-session="${other}"]`)!.textContent).not.toContain('This device');

    await press(root, signOut(root, other));
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toEqual([`GET ${url}`, `DELETE ${url}/${other}`]);
    expect(rows(root)).toEqual([here]);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(leave).not.toHaveBeenCalled();
  });

  it('refreshes the list when another row was signed out elsewhere meanwhile', async () => {
    const s = serve({ [`DELETE ${url}/${other}`]: () => problemAnswer('not-found', 404) });
    const root = await renderAt('/settings/sessions');
    await press(root, signOut(root, other));
    expect(s.sent.filter((r) => r.method === 'GET')).toHaveLength(2);
    expect(root.querySelector('[role=alert]')).toBeNull();
  });

  it('says why another row could not be signed out, and keeps it', async () => {
    serve({ [`DELETE ${url}/${other}`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/settings/sessions');
    await press(root, signOut(root, other));
    expect(rows(root)).toEqual([here, other]);
    expect(root.querySelector(`[data-session="${other}"] [role=alert]`)?.textContent).toMatch(/briefly unavailable/);
  });

  it('signing out this device loads /', async () => {
    const s = serve({ [`DELETE ${url}/${here}`]: () => ({ status: 204 }) });
    const root = await renderAt('/settings/sessions');
    await press(root, signOut(root, here));
    // It asks first.
    expect(s.sent.map((r) => r.method)).toEqual(['GET']);
    const dialog = root.querySelector('[role=dialog]')!;
    expect(dialog.textContent).toContain('This browser is signed out');
    await press(root, dialog.querySelector<HTMLElement>('[data-confirm-go]')!);
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toContain(`DELETE ${url}/${here}`);
    expect(leave).toHaveBeenCalledWith('/');
  });

  it('signing out everywhere loads /', async () => {
    const s = serve({ [`DELETE ${url}`]: () => ({ status: 204 }) });
    const root = await renderAt('/settings/sessions');
    await press(root, root.querySelector<HTMLElement>('[data-everywhere]')!);
    await press(root, root.querySelector<HTMLElement>('[role=dialog] [data-confirm-go]')!);
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toEqual([`GET ${url}`, `DELETE ${url}`]);
    expect(leave).toHaveBeenCalledWith('/');
  });

  it('loads nothing without a 204, and says why', async () => {
    serve({ [`DELETE ${url}`]: () => ({ status: 200, body: {} }) });
    const root = await renderAt('/settings/sessions');
    await press(root, root.querySelector<HTMLElement>('[data-everywhere]')!);
    await press(root, root.querySelector<HTMLElement>('[role=dialog] [data-confirm-go]')!);
    expect(leave).not.toHaveBeenCalled();
    expect(root.querySelector('[role=dialog] [role=alert]')?.textContent).toBe('Something went wrong on the server.');
    expect(root.querySelector<HTMLButtonElement>('[data-confirm-go]')!.disabled).toBe(false);
  });

  it("everywhere's dialog says this browser signs out too", async () => {
    serve();
    const root = await renderAt('/settings/sessions');
    const opener = root.querySelector<HTMLButtonElement>('[data-everywhere]')!;
    opener.focus();
    await press(root, opener);
    const dialog = root.querySelector<HTMLElement>('[role=dialog]')!;
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(dialog.querySelector('#confirm-title')?.textContent).toBe('Sign out everywhere?');
    expect(dialog.textContent).toContain('this browser too');
    const [cancel, go] = dialog.querySelectorAll<HTMLButtonElement>('button');
    // Focus starts on Cancel, and Tab stays inside.
    expect(document.activeElement).toBe(cancel);
    go.focus();
    const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
    await act(async () => void go.dispatchEvent(tab));
    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(cancel);
    // Escape closes it, and focus goes back to the opener.
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    await settle(root);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(document.activeElement).toBe(opener);
    expect(leave).not.toHaveBeenCalled();
  });
});
