import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminUserDetail, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, settle } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import userList from '../../../../../internal/dto/testdata/golden/admin-users.admin.json';
import userAdmin from '../../../../../internal/dto/testdata/golden/admin-user.admin.json';
import userModerator from '../../../../../internal/dto/testdata/golden/admin-user.moderator.json';

// The users tab of /admin/users (TKT-01M29CXK1X), against a stubbed server
// that answers with the goldens.

const asModerator = () => structuredClone(moderatorMe) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];

function serve() {
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    if (!handler) return new Response('{}', { status: 404, headers: { 'Content-Type': 'application/problem+json' } });
    const a = await handler(body);
    const problem = a.status >= 400;
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': problem ? 'application/problem+json' : 'application/json' },
    });
  });
}

const conflict = { status: 409, body: { type: 'conflict', status: 409, title: 'conflict' } };

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  sent = [];
  routes = {
    'GET /api/v1/admin/users': () => ({ status: 200, body: userList }),
    'GET /api/v1/admin/users/alice': () => ({ status: 200, body: userAdmin }),
  };
  serve();
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

async function open(root: HTMLElement, handle: string) {
  await act(async () => root.querySelector<HTMLButtonElement>(`[data-user="${handle}"]`)!.click());
  await settle(root);
}

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);

describe('the users tab', () => {
  it('lists accounts with their status, and shows an admin the identities', async () => {
    viewer.value = asAdmin();
    const root = await renderAt('/admin/users');
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => tr.textContent);
    expect(rows[0]).toContain('alice');
    expect(rows[1]).toContain('suspended');
    expect(buttons(root)).toContain('More accounts');

    await open(root, 'alice');
    const detail = root.querySelector('[data-detail="alice"]')!;
    expect(detail.textContent).toContain('alice@example.org');
    expect(detail.querySelector('[data-sessions]')!.textContent).toBe('2');
    expect(detail.querySelector('select')).not.toBeNull();
  });

  it('marks an identity provider membership, and offers no remove for it', async () => {
    viewer.value = asAdmin();
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    const groupRows = [...root.querySelectorAll('[data-detail] tbody tr')].slice(0, 2);
    const [byHand, fromIdP] = groupRows;
    expect(byHand.textContent).toContain('added by bob');
    expect(buttons(byHand)).toEqual(['Remove']);
    expect(fromIdP.querySelector('[data-idp]')).not.toBeNull();
    expect(buttons(fromIdP)).toEqual([]);
  });

  it('changes a role after asking, and shows the account as the server answers', async () => {
    viewer.value = asAdmin();
    const changed = structuredClone(userAdmin) as AdminUserDetail;
    changed.user.user.role = 'moderator';
    routes['PUT /api/v1/admin/users/alice/role'] = () => ({ status: 200, body: changed });
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    const select = root.querySelector<HTMLSelectElement>('[data-detail] select')!;
    await act(async () => {
      select.value = 'moderator';
      select.dispatchEvent(new Event('change'));
    });
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Change role')!.click());
    expect(root.textContent).toContain('Make alice a moderator?');
    expect(sent.some((s) => s.method === 'PUT')).toBe(false);
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Confirm')!.click());
    await settle(root);
    expect(sent.find((s) => s.method === 'PUT')?.body).toEqual({ role: 'moderator' });
    expect(root.querySelector('[data-detail] [data-role]')!.textContent).toBe('moderator');
  });

  // Terva review 1419: the list is asked for again, so the row does not keep
  // the old role, nor stay under a Role filter it no longer matches.
  it('asks for the list again after a role change', async () => {
    viewer.value = asAdmin();
    const changed = structuredClone(userAdmin) as AdminUserDetail;
    changed.user.user.role = 'moderator';
    routes['PUT /api/v1/admin/users/alice/role'] = () => ({ status: 200, body: changed });
    routes['GET /api/v1/admin/users?role=member'] = () => ({
      status: 200,
      body: { users: [userList.users[0]], next_cursor: '' },
    });
    const root = await renderAt('/admin/users');
    const role = [...root.querySelectorAll<HTMLSelectElement>('.admin-filters select')][0];
    await act(async () => {
      role.value = 'member';
      role.dispatchEvent(new Event('change'));
    });
    await settle(root);
    expect(root.querySelectorAll('.admin-split > div tbody tr')).toHaveLength(1);
    await open(root, 'alice');

    // The server now has no member called alice.
    routes['GET /api/v1/admin/users?role=member'] = () => ({ status: 200, body: { users: [], next_cursor: '' } });
    const select = root.querySelector<HTMLSelectElement>('[data-detail] select')!;
    await act(async () => {
      select.value = 'moderator';
      select.dispatchEvent(new Event('change'));
    });
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Change role')!.click());
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Confirm')!.click());
    await settle(root);
    await settle(root); // the list asked again after the PUT answered
    expect(sent.filter((s) => s.url === '/api/v1/admin/users?role=member')).toHaveLength(2);
    expect(root.querySelector('[data-user="alice"]')).toBeNull();
    expect(root.querySelector('[data-detail] [data-role]')!.textContent).toBe('moderator');
  });

  it('keeps the confirmation open and says why when the server refuses the last admin', async () => {
    viewer.value = asAdmin();
    const admin = structuredClone(userAdmin) as AdminUserDetail;
    admin.user.user.role = 'admin';
    routes['GET /api/v1/admin/users/alice'] = () => ({ status: 200, body: admin });
    routes['PUT /api/v1/admin/users/alice/role'] = () => conflict;
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    const select = root.querySelector<HTMLSelectElement>('[data-detail] select')!;
    await act(async () => {
      select.value = 'member';
      select.dispatchEvent(new Event('change'));
    });
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Change role')!.click());
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'Confirm')!.click());
    await settle(root);
    expect(root.querySelector('[data-detail] [role="alert"]')!.textContent).toContain('Someone else changed this');
    expect(buttons(root)).toContain('Confirm');
    expect(root.querySelector('[data-detail] [data-role]')!.textContent).toBe('admin');
  });

  it('shows a moderator the list and the detail read-only, with no identities and no tabs', async () => {
    viewer.value = asModerator();
    routes['GET /api/v1/admin/users/alice'] = () => ({ status: 200, body: userModerator });
    const root = await renderAt('/admin/users?tab=permissions');
    expect(root.querySelector('.admin-tabs')).toBeNull();
    expect(root.querySelector('[data-user="alice"]')).not.toBeNull();
    await open(root, 'alice');
    const detail = root.querySelector('[data-detail="alice"]')!;
    expect(detail.querySelector('select')).toBeNull();
    expect(detail.querySelector('[data-sessions]')).toBeNull();
    expect(detail.textContent).not.toContain('Identities');
    expect(buttons(detail)).toEqual(['Close']);
  });

  // Terva review 1414, finding 3: a page asked for under one filter is not
  // appended to the list another filter drew.
  it('drops a More answer that arrives after the filter changed', async () => {
    viewer.value = asAdmin();
    let answer!: (a: Answer) => void;
    routes[`GET /api/v1/admin/users?cursor=${userList.next_cursor}`] = () => new Promise<Answer>((r) => (answer = r));
    routes['GET /api/v1/admin/users?status=suspended'] = () => ({
      status: 200,
      body: { users: [userList.users[1]], next_cursor: '' },
    });
    const root = await renderAt('/admin/users');
    await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === 'More accounts')!.click());
    const status = [...root.querySelectorAll<HTMLSelectElement>('.admin-filters select')][1];
    await act(async () => {
      status.value = 'suspended';
      status.dispatchEvent(new Event('change'));
    });
    await settle(root);
    const late = structuredClone(userList);
    late.users[0].user.handle = 'zed';
    late.next_cursor = '';
    await act(async () => answer({ status: 200, body: late }));
    await settle(root);
    const rows = [...root.querySelectorAll('tbody tr')].map((tr) => tr.textContent);
    expect(rows).toHaveLength(1);
    expect(rows.join()).not.toContain('zed');
  });

  it('filters by asking the server again', async () => {
    viewer.value = asAdmin();
    routes['GET /api/v1/admin/users?status=suspended'] = () => ({
      status: 200,
      body: { users: [userList.users[1]], next_cursor: '' },
    });
    const root = await renderAt('/admin/users');
    const status = [...root.querySelectorAll<HTMLSelectElement>('.admin-filters select')][1];
    await act(async () => {
      status.value = 'suspended';
      status.dispatchEvent(new Event('change'));
    });
    await settle(root);
    expect(sent.at(-1)?.url).toBe('/api/v1/admin/users?status=suspended');
    expect(root.querySelectorAll('tbody tr').length).toBe(1);
    expect(buttons(root)).not.toContain('More accounts');
  });
});
