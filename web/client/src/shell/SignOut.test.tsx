import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { renderAt, screenOf, unmount } from '../testing';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import preUser from '../../../../internal/dto/testdata/golden/me.pre_user.json';

// Sign out on the member's own profile (TKT-01M3QVWKAG): a plain form post
// to /auth/logout in the Account block, beside "Link a sign-in", and nowhere
// else a member's session is not the reader's. The member golden's handle is
// alice.

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(member) as Me;
  isPhone.value = false;
  // Nothing answers unless a test says what does.
  vi.stubGlobal('fetch', () => new Promise(() => {}));
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  isPhone.value = false;
  vi.unstubAllGlobals();
});

const forms = (root: HTMLElement) => root.querySelectorAll<HTMLFormElement>('form[action="/auth/logout"]');

describe('sign out', () => {
  it.each([false, true])('a member can sign out from their own profile, phone %s', async (phone) => {
    isPhone.value = phone;
    const root = await renderAt('/u/alice');
    expect(screenOf(root)).toBe('profile');
    const all = forms(root);
    expect(all).toHaveLength(1);
    const form = all[0];
    expect(form.getAttribute('method')).toBe('post');
    // In the Account block, after Link a sign-in.
    const account = form.closest('[data-account]');
    expect(account?.querySelector('h2')?.textContent).toBe('Account');
    expect(account?.querySelector('[data-nav=link-sign-in]')?.compareDocumentPosition(form)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    const buttons = form.querySelectorAll('button');
    expect(buttons).toHaveLength(1);
    expect(buttons[0].getAttribute('type')).toBe('submit');
    expect(buttons[0].textContent).toBe('Sign out');
    expect(buttons[0].className).toBe('sign-out');
    // A plain post: nothing but the browser handles it.
    expect(form.onsubmit).toBeNull();
    expect(buttons[0].onclick).toBeNull();
  });

  it('is on the profile whatever case the address has', async () => {
    expect(forms(await renderAt('/u/ALICE'))).toHaveLength(1);
  });

  it('is not on another member’s profile', async () => {
    const root = await renderAt('/u/carol');
    expect(screenOf(root)).toBe('profile');
    expect(forms(root)).toHaveLength(0);
  });

  it('is not offered to a signed-out reader', async () => {
    viewer.value = structuredClone(anon) as Me;
    const root = await renderAt('/u/alice');
    expect(screenOf(root)).toBe('profile');
    expect(forms(root)).toHaveLength(0);
  });

  // A pre-user is drawn by ApplicantShell, whose bar has its own Sign out;
  // the profile's is not drawn.
  it('is the applicant bar’s alone for a pre-user', async () => {
    viewer.value = structuredClone(preUser) as Me;
    const root = await renderAt('/u/alice');
    expect(root.querySelector('[data-account]')).toBeNull();
    const all = forms(root);
    expect(all).toHaveLength(1);
    expect(all[0].closest('[data-shell="applicant-bar"]')).not.toBeNull();
  });

  it('follows the viewer', async () => {
    const root = await renderAt('/u/alice');
    expect(forms(root)).toHaveLength(1);
    await act(() => void (viewer.value = structuredClone(anon) as Me));
    expect(forms(root)).toHaveLength(0);
  });
});
