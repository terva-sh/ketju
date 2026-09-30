import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, screenOf } from '../../testing';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import moderator from '../../../../../internal/dto/testdata/golden/me.moderator.json';

// The section list follows the server's gates (internal/web/api/admin.go):
// Queue for whoever moderates somewhere, Applications and Users for staff by
// role, the rest for admins (TKT-01M3NRYCP5).

const asMember = () => structuredClone(member) as Me;
const asModerator = () => structuredClone(moderator) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};
// withQueue is me as /me will serve it once it carries queue
// (TKT-01M29CRJ4E): a number for a viewer who moderates somewhere, null for
// anyone else.
const withQueue = (me: Me, queue: number | null) => ({ ...me, queue }) as Me;

const listed = (root: HTMLElement) =>
  [...root.querySelectorAll<HTMLAnchorElement>('[data-admin-sections] a')].map((a) => a.textContent);

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  vi.stubGlobal('fetch', () => new Promise(() => {}));
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

describe('the admin section list', () => {
  it('shows a moderator Queue, Applications and Users only', async () => {
    viewer.value = asModerator();
    const root = await renderAt('/admin');
    expect(screenOf(root)).toBe('admin');
    expect(listed(root)).toEqual(['Moderation queue', 'Applications', 'Users']);
  });

  it('shows an admin every section', async () => {
    viewer.value = asAdmin();
    const root = await renderAt('/admin');
    expect(listed(root)).toEqual([
      'Moderation queue',
      'Applications',
      'Users',
      'Forums',
      'Application forms',
      'Site settings',
      'Audit log',
    ]);
  });

  // AC5's client half: a member who moderates one forum through a group has
  // a queue on /me, and Queue is all they are offered.
  it('shows a member whose /me has a queue the Queue alone', async () => {
    viewer.value = withQueue(asMember(), 3);
    const root = await renderAt('/admin');
    expect(listed(root)).toEqual(['Moderation queue']);
  });

  it('follows /me queue over the role once /me carries it', async () => {
    viewer.value = withQueue(asModerator(), 0);
    expect(listed(await renderAt('/admin'))).toEqual(['Moderation queue', 'Applications', 'Users']);
    viewer.value = withQueue(asMember(), null);
    expect(listed(await renderAt('/admin'))).toEqual([]);
  });

  it('offers a plain member nothing, and says so', async () => {
    viewer.value = asMember();
    const root = await renderAt('/admin');
    expect(root.querySelector('[data-admin-sections]')).toBeNull();
    expect(root.textContent).toContain('Your account does not have access to administration.');
  });

  it('marks the section the address names, and renders it under its own screen name', async () => {
    viewer.value = asAdmin();
    const root = await renderAt('/admin/users');
    expect(screenOf(root)).toBe('admin-users');
    expect(root.querySelector('h1')?.textContent).toBe('Users');
    const current = [...root.querySelectorAll('[data-admin-sections] a[aria-current="page"]')].map((a) => a.textContent);
    expect(current).toEqual(['Users']);
  });

  it('does not show a section the viewer may not use, even at its address', async () => {
    viewer.value = asModerator();
    const root = await renderAt('/admin/settings');
    expect(screenOf(root)).toBe('admin-settings');
    expect(root.textContent).toContain('Your account does not have access to this section.');
    expect(listed(root)).not.toContain('Site settings');
  });
});
