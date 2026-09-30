import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuditPage, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, screenOf, settle, stubServer, type StubServer, unmount } from '../../testing';
import { dateOf, startOf, stamp } from './AuditScreen';
import anonMe from '../../../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import auditGolden from '../../../../../internal/dto/testdata/golden/audit.admin.json';
import filteredGolden from '../../../../../internal/dto/testdata/golden/audit-filtered.admin.json';

// /admin/audit (TKT-01M29CP2R4), against a stubbed server that answers with
// the goldens.

const asModerator = () => structuredClone(moderatorMe) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};
const first = () => structuredClone(auditGolden) as AuditPage;
const older = () => structuredClone(filteredGolden) as AuditPage;

let server: StubServer;
let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asAdmin();
  server = stubServer({
    'GET /api/v1/admin/audit': () => ({ status: 200, body: first() }),
    'GET /api/v1/admin/audit?cursor=NDA5': () => ({ status: 200, body: older() }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const gets = () => server.sent.filter((s) => s.method === 'GET' && s.url.startsWith('/api/v1/admin/audit')).map((s) => s.url);
const rows = (root: HTMLElement) => [...root.querySelectorAll('tbody tr')];
const field = <T extends HTMLElement>(root: HTMLElement, name: string) => root.querySelector<T>(`[name="${name}"]`)!;

async function type(el: HTMLInputElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event('input'));
  });
}

async function submit(root: HTMLElement) {
  await act(async () => {
    root.querySelector('form.audit-filters')!.dispatchEvent(new Event('submit', { cancelable: true }));
  });
  await settle(root);
}

describe('the audit log screen', () => {
  it('lists the entries newest first, with targets and related forums as links', async () => {
    const root = await renderAt('/admin/audit');
    expect(screenOf(root)).toBe('admin-audit');
    const r = rows(root);
    expect(r).toHaveLength(4);

    // thread.moved: the thread, then the forum it left and the one it went to.
    const moved = r[0];
    expect(moved.textContent).toContain('thread.moved');
    expect(moved.querySelector('[data-actor]')!.textContent).toBe('bob');
    const links = [...moved.querySelectorAll('a')].map((a) => [a.textContent, a.getAttribute('href')]);
    expect(links).toEqual([
      ['The dinner bill', '/t/3yymw47g00001/the-dinner-bill'],
      ['General', '/f/general'],
      ['Money', '/f/money'],
    ]);

    // The auto-hide: no account did it, and the post links to itself.
    expect(r[1].querySelector('[data-actor]')!.textContent).toBe('System (thread-log)');
    expect(r[1].querySelector('a[data-target="post"]')!.getAttribute('href')).toBe('/t/3yymw47g00001/the-dinner-bill/63');

    // A deleted blob has no page: its label is text, the hash its title.
    expect(r[2].querySelector('a')).toBeNull();
    const blob = r[2].querySelector('[data-target="blob"]')!;
    expect(blob.textContent).toBe('9f86d081884c');
    expect(blob.getAttribute('title')).toHaveLength(64);

    // The setting row carries its address and its details as key and value.
    expect(r[3].textContent).toContain('203.0.113.7');
    const facts = [...r[3].querySelectorAll('.audit-details dt, .audit-details dd')].map((e) => e.textContent);
    expect(facts).toEqual(['from', 'open', 'to', 'approval']);
    expect(r[3].querySelector('time')!.textContent).toBe(stamp(1757607000000));
    expect(gets()).toEqual(['/api/v1/admin/audit']);
  });

  it('draws details as text, never as markup, and an absent account without a link', async () => {
    const page = older();
    page.entries[0].details = { note: '<b>bold</b>', count: 3 };
    page.entries[1].target.href = 'javascript:alert(1)';
    server.routes['GET /api/v1/admin/audit'] = () => ({ status: 200, body: page });
    const root = await renderAt('/admin/audit');
    const r = rows(root);
    expect(r[0].querySelector('b')).toBeNull();
    const facts = [...r[0].querySelectorAll('.audit-details dd')].map((e) => e.textContent);
    expect(facts).toEqual(['<b>bold</b>', '3']);
    // deleted-user-23 has no page, and an href off the site is never drawn.
    expect(r[1].querySelector('a')).toBeNull();
    expect(r[1].querySelector('[data-target="user"]')!.textContent).toBe('deleted-user-23');
  });

  it("fills the action filter from the answer's actions, with each noun's prefix", async () => {
    const root = await renderAt('/admin/audit');
    const options = [...root.querySelectorAll('[data-actions] option')].map((o) => o.getAttribute('value'));
    expect(options).toEqual([
      'blob.',
      'post.',
      'setting.',
      'thread.',
      'user.',
      ...first().actions,
    ]);
    expect(field<HTMLInputElement>(root, 'action').getAttribute('list')).toBe('audit-actions');
  });

  it('keeps the filters in the address, and sends them to the API as they are', async () => {
    const q = '?actor=bob&action=user.&target_type=user&target_id=17&from=1757000000000&to=1757100000000';
    server.routes['GET /api/v1/admin/audit' + q] = () => ({ status: 200, body: older() });
    const root = await renderAt('/admin/audit' + q);
    expect(gets()).toEqual(['/api/v1/admin/audit' + q]);
    expect(rows(root)).toHaveLength(2);
    expect(field<HTMLInputElement>(root, 'actor').value).toBe('bob');
    expect(field<HTMLInputElement>(root, 'action').value).toBe('user.');
    expect(field<HTMLSelectElement>(root, 'target_type').value).toBe('user');
    expect(field<HTMLInputElement>(root, 'target_id').value).toBe('17');
    expect(field<HTMLInputElement>(root, 'from').value).toBe(dateOf(1757000000000));
    expect(field<HTMLInputElement>(root, 'to').value).toBe(dateOf(1757100000000 - 1));

    // Typing fetches nothing; Apply puts the draft in the address.
    await type(field(root, 'actor'), 'alice');
    await type(field(root, 'to'), '2026-09-30');
    expect(gets()).toHaveLength(1);
    const to = startOf('2026-09-30', 1);
    const next = `?actor=alice&action=user.&target_type=user&target_id=17&from=1757000000000&to=${to}`;
    server.routes['GET /api/v1/admin/audit' + next] = () => ({ status: 200, body: first() });
    await submit(root);
    expect(location.pathname + location.search).toBe('/admin/audit' + next);
    expect(gets()).toEqual(['/api/v1/admin/audit' + q, '/api/v1/admin/audit' + next]);
    expect(rows(root)).toHaveLength(4);
    expect(field<HTMLInputElement>(root, 'actor').value).toBe('alice');
  });

  it('asks for rows no account did with system=1, and not with an actor', async () => {
    server.routes['GET /api/v1/admin/audit?system=1'] = () => ({ status: 200, body: older() });
    const root = await renderAt('/admin/audit?actor=bob');
    const system = field<HTMLInputElement>(root, 'system');
    await act(async () => {
      system.checked = true;
      system.dispatchEvent(new Event('change'));
    });
    expect(field<HTMLInputElement>(root, 'actor').disabled).toBe(true);
    await submit(root);
    expect(location.search).toBe('?system=1');
    expect(gets().at(-1)).toBe('/api/v1/admin/audit?system=1');
  });

  it('appends the older page, and says so when there is none after it', async () => {
    const root = await renderAt('/admin/audit');
    const olderButton = root.querySelector<HTMLButtonElement>('[data-older]')!;
    expect(olderButton.textContent).toBe('Older');
    await act(async () => olderButton.click());
    await settle(root);
    expect(gets()).toEqual(['/api/v1/admin/audit', '/api/v1/admin/audit?cursor=NDA5']);
    expect(rows(root).map((r) => r.querySelector('code')!.textContent)).toEqual([
      'thread.moved',
      'post.auto_hidden',
      'blob.deleted',
      'setting.changed',
      'user.renamed',
      'user.suspended',
    ]);
    expect(root.querySelector('[data-older]')).toBeNull();
  });

  // The fourth criterion's screen half. The API it would call answers 401
  // and 403 to these viewers (TestAuditIsAdminOnly); the screen does not
  // ask it, and says why.
  it.each([
    ['a moderator', asModerator, 'Your account does not have access to this section.'],
    ['a member', () => structuredClone(memberMe) as Me, 'Your account does not have access to administration.'],
    ['an anonymous reader', () => structuredClone(anonMe) as Me, 'Your account does not have access to administration.'],
  ])('is not offered to %s, and asks the server for nothing', async (_who, me, says) => {
    viewer.value = me();
    const root = await renderAt('/admin/audit');
    expect(screenOf(root)).toBe('admin-audit');
    expect(root.textContent).toContain(says);
    expect(root.querySelector('[data-admin-sections] a[href="/admin/audit"]')).toBeNull();
    expect(gets()).toEqual([]);
  });

  it('shows the refusal the server gives', async () => {
    server.routes['GET /api/v1/admin/audit'] = () => ({
      status: 403,
      body: { type: 'forbidden', title: 'forbidden', status: 403 },
    });
    const root = await renderAt('/admin/audit');
    expect(rows(root)).toHaveLength(0);
    expect(root.querySelector('.audit-list .error-panel[role="alert"]')!.textContent).toContain('The audit log');
  });
});
