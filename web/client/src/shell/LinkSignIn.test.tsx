import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { renderAt, screenOf, unmount } from '../testing';
import { linkSignInHref, sameHandle } from './nav';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import preUser from '../../../../internal/dto/testdata/golden/me.pre_user.json';

// "Link a sign-in" on the member's own profile (TKT-01M3QS0TMV): a plain
// anchor to the server's /auth/link, coming back to the profile. The member
// golden's handle is alice.

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

const entry = (root: HTMLElement) => root.querySelector<HTMLAnchorElement>('[data-nav=link-sign-in]');

describe('link a sign-in', () => {
  it('builds the link address', () => {
    expect(linkSignInHref('/unread?x=1&y=a%20b')).toBe('/auth/link?return_to=' + encodeURIComponent('/unread?x=1&y=a%20b'));
    expect(linkSignInHref('/u/alice')).toBe('/auth/link?return_to=%2Fu%2Falice');
  });

  // Handles fold as NormalizeHandle folds them: ASCII letters only.
  it.each([
    ['alice', 'alice', true],
    ['Alice', 'alice', true],
    ['ALICE', 'alice', true],
    ['alice', 'alicf', false],
    ['alice', 'alice2', false],
    ['Ken', 'ken', false],
    ['Ken', 'Ken', false],
  ])('sameHandle(%j, %j) is %s', (a, b, same) => {
    expect(sameHandle(a, b)).toBe(same);
    expect(sameHandle(b, a)).toBe(same);
  });

  it.each(['/u/alice', '/u/ALICE', '/u/Alice?tab=x'])('a member sees the entry on their own profile, %s', async (url) => {
    const root = await renderAt(url);
    expect(screenOf(root)).toBe('profile');
    const a = entry(root);
    expect(a?.getAttribute('href')).toBe(linkSignInHref(url));
    expect(a?.textContent).toBe('Link a sign-in');
    expect(a?.closest('[data-account]')?.querySelector('h2')?.textContent).toBe('Account');
    // A plain link out of the client: nothing but the browser handles it.
    expect(a?.onclick).toBeNull();
  });

  it('points back at the profile at both widths', async () => {
    const root = await renderAt('/u/alice');
    const desktop = entry(root)?.getAttribute('href');
    await act(() => void (isPhone.value = true));
    expect(entry(root)?.getAttribute('href')).toBe(desktop);
    expect(desktop).toBe('/auth/link?return_to=%2Fu%2Falice');
  });

  it('is not on another member’s profile', async () => {
    const root = await renderAt('/u/carol');
    expect(screenOf(root)).toBe('profile');
    expect(entry(root)).toBeNull();
    expect(root.querySelector('[data-account]')).toBeNull();
  });

  it('is not offered to a signed-out reader', async () => {
    viewer.value = structuredClone(anon) as Me;
    const root = await renderAt('/u/alice');
    expect(screenOf(root)).toBe('profile');
    expect(entry(root)).toBeNull();
    expect(root.querySelector('[data-account]')).toBeNull();
  });

  // A pre-user (user null, pre_user set) is drawn by ApplicantShell, which
  // sends a member route to the status page; whatever renders, there is no
  // entry.
  it('is not offered to a pre-user', async () => {
    viewer.value = structuredClone(preUser) as Me;
    const root = await renderAt('/u/alice');
    expect(entry(root)).toBeNull();
    expect(root.querySelector('[data-account]')).toBeNull();
    expect(root.querySelector('a[href^="/auth/link"]')).toBeNull();
  });

  // The entry follows the viewer: signing out in another tab, which /me
  // reports, takes it away.
  it('follows the viewer', async () => {
    const root = await renderAt('/u/alice');
    expect(entry(root)).not.toBeNull();
    await act(() => void (viewer.value = structuredClone(anon) as Me));
    expect(entry(root)).toBeNull();
    expect(root.querySelector('[data-account]')).toBeNull();
  });
});
