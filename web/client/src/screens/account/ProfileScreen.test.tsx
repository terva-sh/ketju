import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Profile } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, stubServer, unmount } from '../../testing';
import anonMe from '../../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import anonymized from '../../../../../internal/dto/testdata/golden/profile-anonymized.member.json';
import memberProfile from '../../../../../internal/dto/testdata/golden/profile.member.json';
import moderatorProfile from '../../../../../internal/dto/testdata/golden/profile.moderator.json';

// The profile screen (TKT-01M3V3CFMA) draws GET /users/{handle} as the
// server answers it. Who sees which profile, and the status, are the
// server's (docs/05 "Whose profile is served"); these tests hold the screen
// to drawing what it is sent and nothing more. The viewer is the member
// golden's, alice.

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// as is a profile golden for handle, with over applied.
function as(handle: string, over: Partial<Profile> = {}, golden: unknown = memberProfile): Profile {
  const p = structuredClone(golden) as Profile;
  return { ...p, ...over, user: { ...p.user, handle, ...over.user } };
}

const serve = (handle: string, p: Profile) => stubServer({ [`GET /api/v1/users/${handle}`]: () => ({ status: 200, body: p }) });

describe('the profile', () => {
  it('shows handle, role badge, bio, joined date and post count', async () => {
    serve('carol', as('carol', { user: { ...as('carol').user, display_name: 'Carol Ruiz', role: 'moderator' } }));
    const root = await renderAt('/u/carol');
    expect(screenOf(root)).toBe('profile');
    expect(root.querySelector('h1')?.textContent).toBe('Carol Ruiz');
    expect(root.querySelector('.profile-handle')?.textContent).toContain('@carol');
    expect(root.querySelector('.profile-handle .role-badge')?.textContent).toBe('moderator');
    expect(root.querySelector('.profile-bio')?.innerHTML).toBe('<p>Runs the <em>book club</em>.</p>\n');
    expect(root.querySelector('.profile-bio')?.classList.contains('post-body')).toBe(true);
    const joined = root.querySelector('.profile-facts time')!;
    expect(joined.getAttribute('datetime')).toBe(new Date(1757000000000).toISOString());
    expect(joined.textContent).toMatch(/2025/);
    expect(root.querySelector('[data-post-count]')?.textContent).toBe('41');
    expect(root.querySelector('[data-profile-posts]')?.getAttribute('href')).toBe('/u/carol/posts');
    expect(root.querySelector('.avatar')).not.toBeNull();
  });

  it('draws an anonymized account as the server sends it, with no bio', async () => {
    serve('deleted-user-7', anonymized as Profile);
    const root = await renderAt('/u/deleted-user-7');
    expect(root.querySelector('h1')?.textContent).toBe('deleted-user-7');
    expect(root.querySelector('.profile-bio')).toBeNull();
    expect(root.querySelector('[data-post-count]')?.textContent).toBe('12');
  });
});

// Profile visibility: the status is the server's to send, to moderators and
// admins only. The screen adds no rule of its own in either direction.
describe('shows status only when the API sends it', () => {
  it('draws no status the API left null, even for a suspended account', async () => {
    serve('carol', as('carol', { status: null }));
    const root = await renderAt('/u/carol');
    expect(root.querySelector('[data-post-count]')).not.toBeNull();
    expect(root.querySelector('[data-status]')).toBeNull();
    expect(root.textContent).not.toMatch(/suspended/i);
  });

  it.each([
    ['suspended', 'Suspended'],
    ['deactivated', 'Deactivated'],
    ['pending', 'Pending'],
  ])('draws %s when the API sends it', async (status, words) => {
    serve('carol', as('carol', { status }, moderatorProfile));
    const root = await renderAt('/u/carol');
    expect(root.querySelector(`[data-status="${status}"]`)?.textContent).toBe(words);
  });

  it('draws nothing for an active account', async () => {
    serve('carol', as('carol', {}, moderatorProfile));
    const root = await renderAt('/u/carol');
    expect(root.querySelector('[data-post-count]')?.textContent).toBe('42');
    expect(root.querySelector('[data-status]')).toBeNull();
  });
});

describe('a profile the viewer may not see', () => {
  // An unknown handle and a pending account to a member answer alike.
  it('not-found shows the not-found text', async () => {
    stubServer({ 'GET /api/v1/users/nobody': () => problemAnswer('not-found', 404) });
    const root = await renderAt('/u/nobody');
    expect(screenOf(root)).toBe('not-found');
    expect(root.textContent).toContain('There is nothing at this address.');
  });

  it('unauthenticated offers sign in', async () => {
    viewer.value = structuredClone(anonMe) as Me;
    stubServer({ 'GET /api/v1/users/carol': () => problemAnswer('unauthenticated', 401) });
    const root = await renderAt('/u/carol');
    expect(screenOf(root)).toBe('profile');
    expect(root.querySelector('[role=alert] [data-sign-in]')?.textContent).toBe('Sign in, then try again.');
    expect(root.querySelector('[data-account]')).toBeNull();
  });
});

describe('a renamed handle', () => {
  // The API's 302 is followed by fetch, so the old address answers with the
  // account under its handle now.
  it("replaces a renamed handle's address", async () => {
    stubServer({
      'GET /api/v1/users/carol-old': () => ({ status: 200, body: as('carol') }),
      'GET /api/v1/users/carol': () => ({ status: 200, body: as('carol') }),
    });
    const replace = vi.spyOn(history, 'replaceState');
    const push = vi.spyOn(history, 'pushState');
    const root = await renderAt('/u/carol-old?x=1');
    await vi.waitFor(() => expect(location.pathname + location.search).toBe('/u/carol?x=1'));
    expect(replace.mock.calls.map((c) => String(c[2]))).toContain('/u/carol?x=1');
    expect(push).not.toHaveBeenCalled();
    expect(screenOf(root)).toBe('profile');
  });

  it('leaves an address that differs only in case', async () => {
    const s = serve('CAROL', as('carol'));
    await renderAt('/u/CAROL');
    expect(s.sent.map((r) => r.url)).toEqual(['/api/v1/users/CAROL']);
    expect(location.pathname).toBe('/u/CAROL');
  });
});

describe('the Account block', () => {
  it("own profile's Account block links to Settings and keeps Link a sign-in and Sign out", async () => {
    serve('alice', as('alice'));
    const root = await renderAt('/u/alice');
    expect(root.querySelector('[data-post-count]')).not.toBeNull();
    const block = root.querySelector('[data-account]')!;
    expect(block.querySelector('h2')?.textContent).toBe('Account');
    const links = [...block.querySelectorAll('a')];
    expect(links.map((a) => a.textContent)).toEqual(['Settings', 'Link a sign-in']);
    expect(links[0].getAttribute('href')).toBe('/settings');
    expect(block.querySelector('[data-nav=link-sign-in]')?.textContent).toBe('Link a sign-in');
    expect(block.querySelector('form[action="/auth/logout"] button')?.textContent).toBe('Sign out');
  });

  it("is not on another member's profile", async () => {
    serve('carol', as('carol'));
    const root = await renderAt('/u/carol');
    expect(root.querySelector('[data-post-count]')).not.toBeNull();
    expect(root.querySelector('[data-account]')).toBeNull();
  });
});
