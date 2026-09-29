import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, PermissionGrid } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, settle } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import grid from '../../../../../internal/dto/testdata/golden/permissions.admin.json';
import preview from '../../../../../internal/dto/testdata/golden/permission-preview.admin.json';

// The permissions tab of /admin/users (TKT-01M29CXK1X): staging, the
// preview, and applying the batch, against a stubbed server that answers
// with the goldens. The grid golden has book-club (7) granted reply
// everywhere and read in Books (5), and moderators (3) moderate in General
// (1).

const asAdmin = () => {
  const me = structuredClone(moderatorMe) as Me;
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];
let saved: Me;

beforeEach(() => {
  sent = [];
  routes = { 'GET /api/v1/admin/permissions': () => ({ status: 200, body: grid }) };
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    const a = handler ? await handler(body) : { status: 404, body: { type: 'not-found', status: 404 } };
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': a.status >= 400 ? 'application/problem+json' : 'application/json' },
    });
  });
  saved = viewer.value;
  viewer.value = asAdmin();
});
afterEach(() => {
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const cell = (root: HTMLElement, key: string) => root.querySelector<HTMLTableCellElement>(`[data-cell="${key}"]`)!;
const box = (root: HTMLElement, key: string) => cell(root, key).querySelector('input')!;
const toggle = async (root: HTMLElement, key: string) => {
  await act(async () => box(root, key).click());
};
const press = async (root: HTMLElement, label: string) => {
  await act(async () => [...root.querySelectorAll('button')].find((b) => b.textContent === label)!.click());
  await settle(root);
};
const pick = async (root: HTMLElement, permission: string) => {
  const select = root.querySelector<HTMLSelectElement>('.admin-permissions select')!;
  await act(async () => {
    select.value = permission;
    select.dispatchEvent(new Event('change'));
  });
};

describe('the permissions tab', () => {
  it('draws the grid for one permission at a time, the site-wide column first', async () => {
    const root = await renderAt('/admin/users?tab=permissions');
    const heads = [...root.querySelectorAll('.admin-grid thead th')].map((th) => th.textContent);
    expect(heads).toEqual(['Group', 'Everywhere', 'General', 'Books']);
    expect(box(root, '5:7:read').checked).toBe(true);
    expect(box(root, '0:7:read').checked).toBe(false);
    await pick(root, 'reply');
    expect(box(root, '0:7:reply').checked).toBe(true);
    expect(box(root, '5:7:reply').checked).toBe(false);
  });

  it('stages a change without sending it, marks it, and unstages it when toggled back', async () => {
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    expect(box(root, '5:7:read').checked).toBe(false);
    expect(cell(root, '5:7:read').hasAttribute('data-staged')).toBe(true);
    expect(root.querySelector('[data-staged-count]')!.textContent).toBe('1 change staged, not yet applied.');
    expect(sent.filter((s) => s.method !== 'GET')).toEqual([]);
    await toggle(root, '5:7:read');
    expect(cell(root, '5:7:read').hasAttribute('data-staged')).toBe(false);
    expect(root.querySelector('[data-staged-count]')!.textContent).toBe('No changes staged.');
  });

  it('previews a member with the staged changes, marking what would change', async () => {
    routes['POST /api/v1/admin/permissions/preview'] = () => ({ status: 200, body: preview });
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    const input = root.querySelector<HTMLInputElement>('form[aria-label="Preview for a member"] input')!;
    await act(async () => {
      input.value = 'bob';
      input.dispatchEvent(new Event('input'));
    });
    await press(root, 'Preview');
    expect(sent.find((s) => s.url.endsWith('/preview'))?.body).toEqual({
      handle: 'bob',
      changes: [{ forum_id: 5, group_id: 7, permission: 'read', grant: false }],
    });
    const table = root.querySelector('[data-preview="bob"]')!;
    const rows = [...table.querySelectorAll('tbody tr')];
    expect(rows[0].querySelectorAll('[data-changed]').length).toBe(0);
    expect(rows[1].querySelectorAll('[data-changed]').length).toBe(5);
    expect(rows[1].querySelector('td')!.textContent).toBe('allow → hidden');

    // A further change makes the preview stale, so it goes.
    await toggle(root, '1:3:read');
    expect(root.querySelector('[data-preview]')).toBeNull();
  });

  it('applies the whole batch after asking, and draws the grid the server answers with', async () => {
    const after = structuredClone(grid) as PermissionGrid;
    after.grants = after.grants.filter((g) => !(g.forum_id === 5 && g.group_id === 7));
    after.grants.push({ forum_id: null, group_id: 3, permission: 'read' });
    routes['POST /api/v1/admin/permissions'] = () => ({ status: 200, body: after });
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    await toggle(root, '0:3:read');
    await press(root, 'Apply 2 changes');
    expect(sent.some((s) => s.method === 'POST')).toBe(false);
    await press(root, 'Confirm');
    expect(sent.find((s) => s.method === 'POST')?.body).toEqual({
      changes: [
        { forum_id: 5, group_id: 7, permission: 'read', grant: false },
        { forum_id: null, group_id: 3, permission: 'read', grant: true },
      ],
    });
    expect(root.querySelector('[data-staged]')).toBeNull();
    expect(box(root, '5:7:read').checked).toBe(false);
    expect(box(root, '0:3:read').checked).toBe(true);
  });

  // Terva review 1414, finding 1: nothing can be staged while a batch is
  // with the server, so the answer cannot discard it.
  it('locks the grid while a batch is being applied', async () => {
    let answer!: (a: Answer) => void;
    routes['POST /api/v1/admin/permissions'] = () => new Promise<Answer>((r) => (answer = r));
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    await press(root, 'Apply 1 change');
    await press(root, 'Confirm');
    expect(box(root, '0:3:read').disabled).toBe(true);
    await toggle(root, '0:3:read');
    expect(cell(root, '0:3:read').hasAttribute('data-staged')).toBe(false);

    const after = structuredClone(grid) as PermissionGrid;
    after.grants = after.grants.filter((g) => !(g.forum_id === 5 && g.group_id === 7));
    await act(async () => answer({ status: 200, body: after }));
    await settle(root);
    expect(box(root, '0:3:read').disabled).toBe(false);
    expect(root.querySelector('[data-staged-count]')!.textContent).toBe('No changes staged.');
    expect(sent.filter((s) => s.method === 'POST')).toHaveLength(1);
  });

  // Review 1414, finding 2: a preview answer for a batch that has changed
  // since it was asked for is not shown.
  it('drops a preview answer that arrives after the batch changed', async () => {
    let answer!: (a: Answer) => void;
    routes['POST /api/v1/admin/permissions/preview'] = () => new Promise<Answer>((r) => (answer = r));
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    const input = root.querySelector<HTMLInputElement>('form[aria-label="Preview for a member"] input')!;
    await act(async () => {
      input.value = 'bob';
      input.dispatchEvent(new Event('input'));
    });
    await press(root, 'Preview');
    await toggle(root, '1:3:read');
    await act(async () => answer({ status: 200, body: preview }));
    await settle(root);
    expect(root.querySelector('[data-preview]')).toBeNull();
  });

  // Review 1416, finding 1: no preview is asked for while a batch is with
  // the server, and one asked for before is dropped once the grid changes.
  it('previews nothing across an apply', async () => {
    let applied!: (a: Answer) => void;
    let previewed!: (a: Answer) => void;
    routes['POST /api/v1/admin/permissions'] = () => new Promise<Answer>((r) => (applied = r));
    routes['POST /api/v1/admin/permissions/preview'] = () => new Promise<Answer>((r) => (previewed = r));
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    const input = root.querySelector<HTMLInputElement>('form[aria-label="Preview for a member"] input')!;
    await act(async () => {
      input.value = 'bob';
      input.dispatchEvent(new Event('input'));
    });
    await press(root, 'Preview');
    await press(root, 'Apply 1 change');
    await press(root, 'Confirm');
    const previewButton = [...root.querySelectorAll('button')].find((b) => b.textContent === 'Preview')!;
    expect(previewButton.disabled).toBe(true);

    const after = structuredClone(grid) as PermissionGrid;
    after.grants = after.grants.filter((g) => !(g.forum_id === 5 && g.group_id === 7));
    await act(async () => applied({ status: 200, body: after }));
    await settle(root);
    await act(async () => previewed({ status: 200, body: preview }));
    await settle(root);
    expect(root.querySelector('[data-preview]')).toBeNull();
    expect(previewButton.disabled).toBe(false);
    expect(sent.filter((s) => s.url.endsWith('/preview'))).toHaveLength(1);
  });

  it('keeps the staged batch and says why when the grid moved underneath it', async () => {
    routes['POST /api/v1/admin/permissions'] = () => ({
      status: 409,
      body: { type: 'conflict', status: 409, title: 'conflict' },
    });
    const root = await renderAt('/admin/users?tab=permissions');
    await toggle(root, '5:7:read');
    await press(root, 'Apply 1 change');
    await press(root, 'Confirm');
    expect(root.querySelector('.admin-permissions [role="alert"]')!.textContent).toContain('Someone else changed this');
    expect(cell(root, '5:7:read').hasAttribute('data-staged')).toBe(true);
  });
});
