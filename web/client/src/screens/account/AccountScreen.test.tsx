import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HandleState, Me } from '../../api/dto';
import { handleMessage } from '../../handles';
import { maxDelay } from './AccountScreen';
import { nav } from '../../leave';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type Answer } from '../../testing';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';

// /settings/account (TKT-01M3V6XTYG): the member changes their handle, at
// most once every 30 days, and deactivates their account. Each asks first.
// The member golden's handle is alice.

const handleUrl = '/api/v1/settings/handle';
const deactivateUrl = '/api/v1/settings/deactivate';
const day = 24 * 3600 * 1000;

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

const state = (next: number | null = null): HandleState => ({ handle: 'alice', next_change_at: next });

function serve(s: HandleState = state(), routes: Record<string, (body: unknown) => Answer> = {}) {
  return stubServer({ [`GET ${handleUrl}`]: () => ({ status: 200, body: s }), ...routes });
}

const input = (root: HTMLElement) => root.querySelector<HTMLInputElement>('#account-handle')!;
const changeButton = (root: HTMLElement) => root.querySelector<HTMLButtonElement>('[data-handle-card] button[type=submit]')!;
const hint = (root: HTMLElement) => root.querySelector<HTMLElement>('#account-handle-hint')!;
const fieldError = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-field-error=handle]')!;
const dialog = (root: HTMLElement) => root.querySelector<HTMLElement>('[role=dialog]');
const sent = (s: ReturnType<typeof serve>) => s.sent.map((r) => `${r.method} ${r.url}`);

async function press(root: HTMLElement, el: HTMLElement) {
  await act(async () => el.click());
  await settle(root);
}

// ask types to into the field and presses Change handle.
async function ask(root: HTMLElement, to: string) {
  const el = input(root);
  await act(async () => {
    el.value = to;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  // happy-dom's click does not focus, as a browser's does.
  changeButton(root).focus();
  await press(root, changeButton(root));
}

const go = (root: HTMLElement) => press(root, dialog(root)!.querySelector<HTMLElement>('[data-confirm-go]')!);

describe('the handle', () => {
  it('shows the cooldown date and disables the field while next_change_at is ahead', async () => {
    const until = Date.now() + 20 * day;
    serve(state(until));
    const root = await renderAt('/settings/account');
    expect(screenOf(root)).toBe('settings-account');
    expect(root.querySelector('[data-current-handle]')?.textContent).toBe('@alice');
    expect(input(root).disabled).toBe(true);
    expect(changeButton(root).disabled).toBe(true);
    expect(hint(root).textContent).toMatch(/^You can change your handle again from .+\.$/);
    expect(hint(root).querySelector('time')?.getAttribute('datetime')).toBe(new Date(until).toISOString());
  });

  // Terva review 2009: a page left open across the date opens by itself.
  it('opens the field when the cooldown ends while the screen is open', async () => {
    serve(state(Date.now() + 300));
    const root = await renderAt('/settings/account');
    expect(input(root).disabled).toBe(true);
    await act(() => new Promise((r) => setTimeout(r, 450)));
    expect(input(root).disabled).toBe(false);
    expect(changeButton(root).disabled).toBe(false);
    expect(hint(root).textContent).toContain('Once every 30 days');
  });

  it('waits out a cooldown longer than one timer allows in steps', async () => {
    const timer = vi.spyOn(globalThis, 'setTimeout');
    serve(state(Date.now() + 40 * day));
    const root = await renderAt('/settings/account');
    expect(input(root).disabled).toBe(true);
    expect(timer.mock.calls.some(([, ms]) => ms === maxDelay)).toBe(true);
    expect(timer.mock.calls.every(([, ms]) => (ms ?? 0) <= maxDelay)).toBe(true);
  });

  it('is open when next_change_at is null or has passed', async () => {
    for (const next of [null, Date.now() - day]) {
      serve(state(next));
      const root = await renderAt('/settings/account');
      expect(input(root).disabled).toBe(false);
      expect(hint(root).textContent).toContain('Once every 30 days');
      expect(hint(root).textContent).toContain('90 days');
      await unmount();
    }
  });

  it('a change is confirmed first, then refreshes the viewer and announces the new handle', async () => {
    const next = Date.now() + 30 * day;
    const renamed = structuredClone(memberMe) as Me;
    renamed.user!.handle = 'alicia2';
    const s = serve(state(), {
      [`PUT ${handleUrl}`]: () => ({ status: 200, body: { handle: 'alicia2', previous: 'alice', next_change_at: next } }),
      'GET /api/v1/me': () => ({ status: 200, body: renamed }),
    });
    const root = await renderAt('/settings/account');
    await ask(root, ' Alicia2 ');
    // It asks first, naming the 30 days and the 90-day reservation.
    expect(sent(s)).toEqual([`GET ${handleUrl}`]);
    const d = dialog(root)!;
    expect(d.querySelector('#confirm-title')?.textContent).toBe('Change your handle to @alicia2?');
    expect(d.textContent).toContain('30 days');
    expect(d.textContent).toContain('For 90 days your old handle, @alice,');

    await go(root);
    expect(s.sent.find((r) => r.method === 'PUT')?.body).toEqual({ handle: 'alicia2' });
    expect(sent(s)).toContain('GET /api/v1/me');
    expect(dialog(root)).toBeNull();
    expect(viewer.value.user?.handle).toBe('alicia2');
    expect(root.querySelector('[role=status]')?.textContent).toBe('Your handle is now @alicia2.');
    expect(document.activeElement).toBe(root.querySelector('[role=status]'));
    expect(root.querySelector('[data-current-handle]')?.textContent).toBe('@alicia2');
    // The top bar and the Back link show the new handle without a reload.
    expect(root.querySelector('[data-nav=me]')?.getAttribute('href')).toBe('/u/alicia2');
    expect(root.querySelector('.account-up')?.getAttribute('href')).toBe('/u/alicia2');
    expect(leave).not.toHaveBeenCalled();
    // The 30 days have started.
    expect(input(root).disabled).toBe(true);
    expect(hint(root).querySelector('time')?.getAttribute('datetime')).toBe(new Date(next).toISOString());
  });

  it.each(['invalid_handle', 'reserved', 'taken', 'same', 'something_new'])(
    'a refused %s shows its words beside the field, focused',
    async (code) => {
      serve(state(), {
        [`PUT ${handleUrl}`]: () => problemAnswer('validation-failed', 422, { errors: [{ field: 'handle', code }] }),
      });
      const root = await renderAt('/settings/account');
      await ask(root, 'bob');
      await go(root);
      expect(dialog(root)).toBeNull();
      expect(fieldError(root).textContent).toBe(handleMessage(code, true));
      expect(input(root).getAttribute('aria-invalid')).toBe('true');
      expect(document.activeElement).toBe(input(root));
      expect(viewer.value.user?.handle).toBe('alice');
    },
  );

  it('says same in a member’s words', () => {
    expect(handleMessage('same', true)).toBe('That is already your handle.');
    expect(handleMessage('same')).toBe("That is already this account's handle.");
  });

  it('a change inside the cooldown shows when the next may be made', async () => {
    const at = Date.now() + 12 * day;
    serve(state(), { [`PUT ${handleUrl}`]: () => problemAnswer('conflict', 409, { available_at: at }) });
    const root = await renderAt('/settings/account');
    await ask(root, 'bob');
    await go(root);
    expect(dialog(root)).toBeNull();
    expect(fieldError(root).textContent).toBe('You can change your handle once every 30 days.');
    expect(document.activeElement).toBe(fieldError(root));
    expect(input(root).disabled).toBe(true);
    expect(hint(root).querySelector('time')?.getAttribute('datetime')).toBe(new Date(at).toISOString());
  });

  it('any other refusal stays in the dialog', async () => {
    serve(state(), { [`PUT ${handleUrl}`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/settings/account');
    await ask(root, 'bob');
    await go(root);
    expect(dialog(root)?.querySelector('[role=alert]')?.textContent).toMatch(/briefly unavailable/);
    expect(fieldError(root).textContent).toBe('');
  });

  it('an empty handle is refused here, without asking', async () => {
    const s = serve();
    const root = await renderAt('/settings/account');
    await ask(root, '   ');
    expect(dialog(root)).toBeNull();
    expect(sent(s)).toEqual([`GET ${handleUrl}`]);
    expect(fieldError(root).textContent).toBe(handleMessage('invalid_handle'));
    expect(document.activeElement).toBe(input(root));
  });

  it('Cancel and Escape send nothing and return focus to the button', async () => {
    const s = serve();
    const root = await renderAt('/settings/account');
    await ask(root, 'bob');
    expect(document.activeElement?.textContent).toBe('Cancel');
    await press(root, dialog(root)!.querySelector<HTMLElement>('button')!);
    expect(dialog(root)).toBeNull();
    expect(document.activeElement).toBe(changeButton(root));

    const opener = root.querySelector<HTMLElement>('[data-deactivate]')!;
    opener.focus();
    await press(root, opener);
    expect(dialog(root)).not.toBeNull();
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })));
    await settle(root);
    expect(dialog(root)).toBeNull();
    expect(document.activeElement).toBe(opener);
    expect(sent(s)).toEqual([`GET ${handleUrl}`]);
    expect(leave).not.toHaveBeenCalled();
  });
});

describe('deactivation', () => {
  it('is confirmed first, says how to come back, then loads / in full', async () => {
    const s = serve(state(), { [`POST ${deactivateUrl}`]: () => ({ status: 204 }) });
    const root = await renderAt('/settings/account');
    expect(root.querySelector('[data-deactivate-card]')?.textContent).toContain('sign in and press Reactivate');
    await press(root, root.querySelector<HTMLElement>('[data-deactivate]')!);
    expect(sent(s)).toEqual([`GET ${handleUrl}`]);
    const d = dialog(root)!;
    expect(d.querySelector('#confirm-title')?.textContent).toBe('Deactivate your account?');
    expect(d.textContent).toContain('this one too');
    expect(d.textContent).toContain('sign in and press Reactivate');
    await go(root);
    expect(sent(s)).toEqual([`GET ${handleUrl}`, `POST ${deactivateUrl}`]);
    expect(leave).toHaveBeenCalledWith('/');
  });

  it('the last active admin’s conflict stays in the dialog', async () => {
    serve(state(), { [`POST ${deactivateUrl}`]: () => problemAnswer('conflict', 409) });
    const root = await renderAt('/settings/account');
    await press(root, root.querySelector<HTMLElement>('[data-deactivate]')!);
    await go(root);
    expect(leave).not.toHaveBeenCalled();
    expect(dialog(root)?.querySelector('[role=alert]')?.textContent).toBe(
      'You are the only active admin. Make someone else an admin first.',
    );
    expect(dialog(root)!.querySelector<HTMLButtonElement>('[data-confirm-go]')!.disabled).toBe(false);
  });
});

describe('the settings links', () => {
  it('end with Account, marked on its own screen', async () => {
    serve();
    const root = await renderAt('/settings/account');
    const links = [...root.querySelectorAll<HTMLAnchorElement>('.settings-nav a')];
    expect(links.map((a) => a.textContent)).toEqual(['Preferences', 'Sessions', 'Sign-in identities', 'Account']);
    expect(links.at(-1)?.getAttribute('aria-current')).toBe('page');
  });
});
