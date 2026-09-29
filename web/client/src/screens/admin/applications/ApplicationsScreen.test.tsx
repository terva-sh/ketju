import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminApplicationList, Me } from '../../../api/dto';
import { viewer } from '../../../state/viewer';
import { renderAt, screenOf, settle, stubServer, unmount, type StubServer } from '../../../testing';
import { appUrl, asModerator, listPage, pendingApp } from './fixtures';

// The list at /admin/applications (TKT-01M3NRX2NN): pending by default, a
// status filter in the address, and paging by next_cursor.

const listUrl = (status: string, cursor = '') =>
  `/api/v1/admin/applications?status=${status}${cursor ? `&cursor=${cursor}` : ''}`;

let saved: Me;
let server: StubServer;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asModerator();
  server = stubServer({
    [`GET ${listUrl('pending')}`]: () => ({ status: 200, body: listPage() }),
    [`GET ${appUrl(42)}`]: () => ({ status: 200, body: pendingApp() }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const rows = (root: HTMLElement) => [...root.querySelectorAll('tbody tr')].map((tr) => tr.textContent ?? '');
const gets = () => server.sent.filter((s) => s.method === 'GET').map((s) => s.url);

describe('the applications list', () => {
  it('lists pending applications, newest first, by default', async () => {
    const root = await renderAt('/admin/applications');
    expect(screenOf(root)).toBe('admin-applications');
    expect(gets()).toEqual([listUrl('pending')]);
    const r = rows(root);
    expect(r).toHaveLength(2);
    expect(r[0]).toContain('#42');
    expect(r[0]).toContain('carol@example.com');
    expect(r[0]).toContain('verified');
    expect(r[0]).toContain('via github');
    expect(r[1]).toContain('Sign-in removed');
    expect(root.querySelector<HTMLSelectElement>('[data-filter]')!.value).toBe('pending');
    expect([...root.querySelectorAll('th')].map((th) => th.textContent)).toEqual(['Application', 'Submitted', 'Applicant']);
    expect(root.querySelector<HTMLAnchorElement>('[data-application-link="42"]')!.getAttribute('href')).toBe(
      '/admin/applications?id=42',
    );
  });

  it('states that applicants are not emailed', async () => {
    const root = await renderAt('/admin/applications');
    expect(root.querySelector('[data-no-email]')!.textContent).toContain(
      'Applicants are not emailed. They learn the outcome on their application status page, and notifications are in-app only.',
    );
  });

  it('writes the filter to ?status= and lists that status', async () => {
    const denied: AdminApplicationList = { applications: [], next_cursor: '' };
    server.routes[`GET ${listUrl('denied')}`] = () => ({ status: 200, body: denied });
    const root = await renderAt('/admin/applications');
    const select = root.querySelector<HTMLSelectElement>('[data-filter]')!;
    await act(async () => {
      select.value = 'denied';
      select.dispatchEvent(new Event('change'));
    });
    await settle(root);
    expect(location.pathname + location.search).toBe('/admin/applications?status=denied');
    expect(gets()).toContain(listUrl('denied'));
    expect(root.querySelector('[data-empty]')!.textContent).toBe('No application matches.');
    expect(root.querySelector<HTMLSelectElement>('[data-filter]')!.value).toBe('denied');
  });

  it('reads the filter from the address, and ignores one it does not know', async () => {
    server.routes[`GET ${listUrl('all')}`] = () => ({ status: 200, body: listPage() });
    const all = await renderAt('/admin/applications?status=all');
    expect(gets()).toEqual([listUrl('all')]);
    // All mixes statuses, so it alone has a Status column.
    expect([...all.querySelectorAll('th')].map((th) => th.textContent)).toContain('Status');
    expect(all.querySelector('tbody [data-status="pending"]')).not.toBeNull();
    server.sent.length = 0;
    await renderAt('/admin/applications?status=bogus');
    expect(gets()).toEqual([listUrl('pending')]);
  });

  it('pages with next_cursor until the list ends', async () => {
    const first = listPage();
    const cursor = first.next_cursor;
    const second: AdminApplicationList = {
      applications: [{ ...first.applications[0], id: 38, created_at: first.applications[1].created_at - 1 }],
      next_cursor: '',
    };
    server.routes[`GET ${listUrl('pending', cursor)}`] = () => ({ status: 200, body: second });
    const root = await renderAt('/admin/applications');
    const more = [...root.querySelectorAll('button')].find((b) => b.textContent === 'More applications')!;
    await act(async () => more.click());
    await settle(root);
    expect(gets()).toContain(listUrl('pending', cursor));
    expect([...root.querySelectorAll('[data-application-link]')].map((a) => a.textContent)).toEqual(['#42', '#40', '#38']);
    expect([...root.querySelectorAll('button')].map((b) => b.textContent)).not.toContain('More applications');
  });

  it('opens an application at ?id=', async () => {
    const root = await renderAt('/admin/applications?id=42');
    expect(gets()).toEqual([appUrl(42)]);
    expect(root.querySelector('[data-application="42"]')).not.toBeNull();
    expect(root.querySelector('[data-no-email]')).not.toBeNull();
  });

  it('says so for an id that is not a number, and asks for nothing', async () => {
    const root = await renderAt('/admin/applications?id=abc');
    expect(gets()).toEqual([]);
    expect(root.textContent).toContain('That is not an application number.');
  });
});
