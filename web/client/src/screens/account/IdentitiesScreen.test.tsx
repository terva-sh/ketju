import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IdentityList, Me } from '../../api/dto';
import { nav } from '../../leave';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type Answer } from '../../testing';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import identities from '../../../../../internal/dto/testdata/golden/identities.member.json';

// /settings/identities (TKT-01M29CXK4E): the sign-ins linked to the
// account. Remove is disabled and explained where the server says it would
// be refused; a conflict says why and refreshes; removing the identity this
// session came through ends the session, and signed_out loads / in full
// (docs/05 "Sessions and CSRF", decided by the run 2026-10-01). The
// golden's first identity, 3, is this session's.

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

const url = '/api/v1/settings/identities';

function serve(list: IdentityList = structuredClone(identities), routes: Record<string, () => Answer> = {}) {
  return stubServer({ [`GET ${url}`]: () => ({ status: 200, body: list }), ...routes });
}

const row = (root: HTMLElement, id: number) => root.querySelector<HTMLElement>(`[data-identity="${id}"]`)!;
const removeOf = (root: HTMLElement, id: number) => row(root, id).querySelector<HTMLButtonElement>('button')!;

async function press(root: HTMLElement, el: HTMLElement) {
  await act(async () => el.click());
  await settle(root);
}

describe('identities', () => {
  it('lists each sign-in with its provider, email and dates', async () => {
    serve();
    const root = await renderAt('/settings/identities');
    expect(screenOf(root)).toBe('settings-identities');
    expect([...root.querySelectorAll<HTMLElement>('[data-identity]')].map((r) => r.dataset.identity)).toEqual(['3', '8', '9']);
    expect(row(root, 3).textContent).toContain('GitHub');
    expect(row(root, 3).textContent).toContain('alice@example.org');
    expect(row(root, 3).textContent).toContain('Used for this session');
    expect(row(root, 8).textContent).toContain("The Club's sign-in");
    expect(row(root, 8).textContent).toContain('Last sign-in never');
    expect(row(root, 8).textContent).not.toContain('Used for this session');
    expect(row(root, 9).textContent).toContain('No longer configured (oldidp)');
    expect(removeOf(root, 9).disabled).toBe(false);
  });

  it('adds one through Link a sign-in, coming back here', async () => {
    serve();
    const root = await renderAt('/settings/identities');
    expect(root.querySelector('[data-nav=link-sign-in]')?.getAttribute('href')).toBe('/auth/link?return_to=%2Fsettings%2Fidentities');
  });

  it("the only identity's Remove is disabled and explained", async () => {
    const only = structuredClone(identities) as IdentityList;
    only.identities = [{ ...only.identities[0], removable: false }];
    serve(only);
    const root = await renderAt('/settings/identities');
    const remove = removeOf(root, 3);
    expect(remove.disabled).toBe(true);
    const why = row(root, 3).querySelector('[data-not-removable]')!;
    expect(why.textContent).toBe('This is your only way to sign in.');
    expect(remove.getAttribute('aria-describedby')).toBe(why.id);
  });

  it('a conflict answer shows the explanation and refreshes', async () => {
    const s = serve(undefined, { [`DELETE ${url}/8`]: () => problemAnswer('conflict', 409) });
    const root = await renderAt('/settings/identities');
    await press(root, removeOf(root, 8));
    await press(root, root.querySelector<HTMLElement>('[role=dialog] [data-confirm-go]')!);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(root.querySelector('main > [role=alert]')?.textContent).toBe('This is your only way to sign in.');
    expect(s.sent.filter((r) => r.method === 'GET')).toHaveLength(2);
    expect(leave).not.toHaveBeenCalled();
  });

  it('removing another identity refreshes the list and stays', async () => {
    const s = serve(undefined, { [`DELETE ${url}/8`]: () => ({ status: 200, body: { signed_out: false } }) });
    const root = await renderAt('/settings/identities');
    await press(root, removeOf(root, 8));
    const dialog = root.querySelector('[role=dialog]')!;
    expect(dialog.textContent).not.toContain('signed out too');
    await press(root, dialog.querySelector<HTMLElement>('[data-confirm-go]')!);
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toEqual([`GET ${url}`, `DELETE ${url}/8`, `GET ${url}`]);
    expect(root.querySelector('[role=dialog]')).toBeNull();
    expect(leave).not.toHaveBeenCalled();
  });

  it("removing the current session's identity loads / when signed_out is true", async () => {
    const s = serve(undefined, { [`DELETE ${url}/3`]: () => ({ status: 200, body: { signed_out: true } }) });
    const root = await renderAt('/settings/identities');
    await press(root, removeOf(root, 3));
    const dialog = root.querySelector('[role=dialog]')!;
    expect(dialog.textContent).toContain('this browser will be signed out too');
    await press(root, dialog.querySelector<HTMLElement>('[data-confirm-go]')!);
    expect(s.sent.map((r) => `${r.method} ${r.url}`)).toContain(`DELETE ${url}/3`);
    expect(leave).toHaveBeenCalledWith('/');
  });

  it('keeps the dialog open, saying why, when the removal fails otherwise', async () => {
    serve(undefined, { [`DELETE ${url}/8`]: () => problemAnswer('unavailable', 503) });
    const root = await renderAt('/settings/identities');
    await press(root, removeOf(root, 8));
    await press(root, root.querySelector<HTMLElement>('[role=dialog] [data-confirm-go]')!);
    expect(root.querySelector('[role=dialog] [role=alert]')?.textContent).toMatch(/briefly unavailable/);
    expect(leave).not.toHaveBeenCalled();
  });
});
