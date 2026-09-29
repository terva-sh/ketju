import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { GroupDetail, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, settle } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import groupList from '../../../../../internal/dto/testdata/golden/admin-groups.admin.json';
import bookClub from '../../../../../internal/dto/testdata/golden/admin-group.admin.json';

// The groups tab of /admin/users (TKT-01M29CXK1X), against a stubbed server
// that answers with the goldens.

const asAdmin = () => {
  const me = structuredClone(moderatorMe) as Me;
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer>;
let sent: { method: string; url: string; body: unknown }[];

beforeEach(() => {
  sent = [];
  const moderators: GroupDetail = { group: groupList.groups[0], members: [] };
  routes = {
    'GET /api/v1/admin/groups': () => ({ status: 200, body: groupList }),
    'GET /api/v1/admin/groups/7': () => ({ status: 200, body: bookClub }),
    'GET /api/v1/admin/groups/3': () => ({ status: 200, body: moderators }),
  };
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    const a = handler ? handler(body) : { status: 404, body: { type: 'not-found', status: 404 } };
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': a.status >= 400 ? 'application/problem+json' : 'application/json' },
    });
  });
  saved = viewer.value;
  viewer.value = asAdmin();
});
let saved: Me;
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

async function openGroup(root: HTMLElement, slug: string) {
  await act(async () => root.querySelector<HTMLButtonElement>(`[data-group="${slug}"]`)!.click());
  await settle(root);
}

async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);
const press = async (root: HTMLElement, label: string) => {
  await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === label)!.click());
  await settle(root);
};

describe('the groups tab', () => {
  it('lists the groups with their counts', async () => {
    const root = await renderAt('/admin/users?tab=groups');
    const current = root.querySelector('.admin-tabs a[aria-current="page"]')!;
    expect(current.textContent).toBe('Groups');
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => [...tr.querySelectorAll('td')].map((td) => td.textContent));
    expect(rows).toEqual([
      ['Moderators', 'moderators', '1', '1', 'system'],
      ['Book club', 'book-club', '2', '2', ''],
    ]);
  });

  it('marks an identity provider member and offers no remove for them', async () => {
    const root = await renderAt('/admin/users?tab=groups');
    await openGroup(root, 'book-club');
    const detail = root.querySelector('[data-group-detail="book-club"]')!;
    const [alice, scribe] = [...detail.querySelectorAll('tbody tr')];
    expect(buttons(alice)).toEqual(['Remove']);
    expect(scribe.querySelector('[data-idp]')).not.toBeNull();
    expect(buttons(scribe)).toEqual([]);
  });

  it('says what stands between a group and its deletion, and will not delete a system group', async () => {
    const root = await renderAt('/admin/users?tab=groups');
    await openGroup(root, 'book-club');
    expect(root.querySelector('[data-delete-blocked]')!.textContent).toBe(
      'Remove its 2 members and 2 permission rows before deleting it.',
    );
    expect(buttons(root)).not.toContain('Delete group');
    await openGroup(root, 'moderators');
    expect(root.querySelector('[data-delete-blocked]')!.textContent).toBe('System groups cannot be deleted.');
  });

  it('deletes an empty group after asking', async () => {
    const empty = structuredClone(bookClub) as GroupDetail;
    empty.group.members = 0;
    empty.group.grants = 0;
    empty.members = [];
    routes['GET /api/v1/admin/groups/7'] = () => ({ status: 200, body: empty });
    routes['DELETE /api/v1/admin/groups/7'] = () => ({ status: 204 });
    const root = await renderAt('/admin/users?tab=groups');
    await openGroup(root, 'book-club');
    await press(root, 'Delete group');
    expect(sent.some((s) => s.method === 'DELETE')).toBe(false);
    await press(root, 'Confirm');
    expect(sent.some((s) => s.method === 'DELETE' && s.url === '/api/v1/admin/groups/7')).toBe(true);
    expect(root.querySelector('[data-group-detail]')).toBeNull();
  });

  it('adds a member by handle, and shows the group as the server answers', async () => {
    const after = structuredClone(bookClub) as GroupDetail;
    after.members.push({ ...after.members[0], user: { ...after.members[0].user, id: 40, user: { ...after.members[0].user.user, handle: 'dana' } } });
    routes['POST /api/v1/admin/groups/7/members'] = () => ({ status: 200, body: after });
    const root = await renderAt('/admin/users?tab=groups');
    await openGroup(root, 'book-club');
    const input = root.querySelector<HTMLInputElement>('form[aria-label="Add a member"] input')!;
    await type(input, ' dana ');
    await press(root, 'Add');
    expect(sent.find((s) => s.method === 'POST')?.body).toEqual({ handle: 'dana' });
    expect(root.querySelector('[data-group-detail]')!.textContent).toContain('dana');
    expect(input.value).toBe('');
  });

  it('edits the description, sending only the fields that changed', async () => {
    const edited = { ...bookClub.group, description: 'Monthly reads' };
    routes['PATCH /api/v1/admin/groups/7'] = () => ({ status: 200, body: edited });
    const root = await renderAt('/admin/users?tab=groups');
    await openGroup(root, 'book-club');
    const form = root.querySelector<HTMLFormElement>('form[aria-label="Edit the group"]')!;
    const [name, description] = [...form.querySelectorAll<HTMLInputElement>('input')];
    expect(buttons(form)).toEqual([]);
    await type(name, bookClub.group.name);
    expect(buttons(form)).toEqual([]);
    await type(description, 'Monthly reads');
    await press(root, 'Save');
    expect(sent.filter((s) => s.method === 'PATCH').map((s) => s.body)).toEqual([{ description: 'Monthly reads' }]);
    expect(description.value).toBe('Monthly reads');
    expect(buttons(form)).toEqual([]);
    expect(root.querySelector('[data-group-detail]')!.textContent).toContain('Monthly reads');
  });

  it('creates a group and says why when the server refuses one', async () => {
    let n = 0;
    routes['POST /api/v1/admin/groups'] = () =>
      n++ === 0
        ? { status: 409, body: { type: 'conflict', status: 409, title: 'conflict' } }
        : { status: 201, body: bookClub.group };
    const root = await renderAt('/admin/users?tab=groups');
    const form = root.querySelector<HTMLFormElement>('[data-create-group]')!;
    const [slug, name] = [...form.querySelectorAll<HTMLInputElement>('input')];
    await type(slug, 'book-club');
    await type(name, 'Book club');
    await press(root, 'Create group');
    expect(form.querySelector('[role="alert"]')!.textContent).toContain('Someone else changed this');
    expect(slug.value).toBe('book-club');
    await press(root, 'Create group');
    expect(sent.filter((s) => s.method === 'POST').map((s) => s.body)).toEqual([
      { slug: 'book-club', name: 'Book club', description: '' },
      { slug: 'book-club', name: 'Book club', description: '' },
    ]);
    expect(slug.value).toBe('');
    await settle(root); // the new group's detail loads
    expect(root.querySelector('[data-group-detail="book-club"]')).not.toBeNull();
  });
});
