import { act } from 'preact/test-utils';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Queue } from '../../../api/dto';
import { viewer } from '../../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type StubServer } from '../../../testing';
import { approvedApp, appUrl, asModerator, pendingApp } from '../applications/fixtures';
import { handled } from './QueueScreen';
import golden from '../../../../../../internal/dto/testdata/golden/queue.moderator.json';
import resolution from '../../../../../../internal/dto/testdata/golden/queue-resolution.moderator.json';

// The moderation queue at /admin/queue (TKT-01M29CRJ4E), against a stubbed
// server answering with the server's goldens: a post hidden pending review
// with three flags, a pending application, and a post whose thread cannot be
// read just now.

// The first test to open an admin screen waits on the lazy admin chunk;
// importing it once here moves that cost out of every test (PR #179).
beforeAll(async () => {
  await import('../AdminRoot');
}, 30_000);

const queueUrl = '/api/v1/admin/queue';
const postUrl = '/api/v1/admin/queue/posts/3yymw47g00001/63';
const page = () => structuredClone(golden) as Queue;
const onePage = () => ({ ...page(), next_cursor: '' });

let saved: Me;
let server: StubServer;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asModerator();
  server = stubServer({
    [`GET ${queueUrl}`]: () => ({ status: 200, body: onePage() }),
    'GET /api/v1/me': () => ({ status: 200, body: { ...asModerator(), queue: 2 } }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const items = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('[data-queue-item]')];
const item = (root: HTMLElement, key: string) => root.querySelector<HTMLElement>(`[data-queue-item="${key}"]`);
const button = (root: Element, name: string) =>
  [...root.querySelectorAll('button')].find((b) => b.textContent === name) ?? null;
const hidden = 'flag:3yymw47g00001/63';

async function click(root: HTMLElement, el: HTMLElement | null) {
  expect(el).not.toBeNull();
  await act(async () => el!.click());
  await settle(root);
}

// confirm presses an action and then its confirmation.
async function confirm(root: HTMLElement, it: HTMLElement, label: string, confirmLabel: string) {
  await click(root, button(it, label));
  await click(root, button(it, confirmLabel));
}

describe('the queue', () => {
  it('lists flagged posts and applications in one list, as the server orders them', async () => {
    const root = await renderAt('/admin/queue');
    expect(screenOf(root)).toBe('admin-queue');
    expect(items(root).map((i) => i.dataset.queueItem)).toEqual([
      hidden,
      'application:42',
      'flag:3yymw47g00002/2',
    ]);
    const first = item(root, hidden)!;
    expect(first.querySelector('h2 a')!.getAttribute('href')).toBe('/t/3yymw47g00001/the-dinner-bill/63');
    expect(first.querySelector('[data-post-state]')!.textContent).toBe('Hidden pending review');
    expect(first.querySelector('.post-body')!.innerHTML).toContain('the text a moderator still needs to read');
    const reports = [...first.querySelectorAll('[data-report]')].map((r) => r.textContent);
    expect(reports).toHaveLength(3);
    expect(reports[0]).toContain('Other');
    expect(reports[0]).toContain('Posts our home address');
    expect(reports[0]).toContain('@alice');
    expect(item(root, 'application:42')!.textContent).toContain('carol@example.com');
    expect(item(root, 'flag:3yymw47g00002/2')!.textContent).toContain('cannot be shown just now');
  });

  it('says so when nothing is waiting', async () => {
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: { items: [], next_cursor: '' } });
    const root = await renderAt('/admin/queue');
    expect(root.querySelector('[data-queue-empty]')!.textContent).toBe('Nothing is waiting for a moderator.');
  });

  it('pages with More', async () => {
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: { ...page(), items: page().items.slice(0, 1) } });
    server.routes[`GET ${queueUrl}?cursor=${page().next_cursor}`] = () => ({
      status: 200,
      body: { items: page().items.slice(1), next_cursor: '' },
    });
    const root = await renderAt('/admin/queue');
    expect(items(root)).toHaveLength(1);
    await click(root, button(root, 'More items'));
    expect(items(root)).toHaveLength(3);
    expect(button(root, 'More items')).toBeNull();
  });
});

describe('acting on a flagged post', () => {
  it('dismisses the flags, takes the item out, and fetches the badge again', async () => {
    server.routes[`POST ${postUrl}`] = () => ({ status: 200, body: resolution });
    const root = await renderAt('/admin/queue');
    const it = item(root, hidden)!;
    await act(async () => {
      const note = it.querySelector<HTMLInputElement>('input')!;
      note.value = 'not spam';
      note.dispatchEvent(new Event('input'));
    });
    await confirm(root, it, 'Dismiss flags', 'Dismiss');
    const posted = server.sent.find((s) => s.method === 'POST')!;
    expect(posted.url).toBe(postUrl);
    expect(posted.body).toEqual({ action: 'dismiss', note: 'not spam' });
    expect(item(root, hidden)).toBeNull();
    expect(root.querySelector('[data-queue-notice]')!.textContent).toBe('Post shown again. The 3 flags are closed.');
    expect(server.sent.some((s) => s.method === 'GET' && s.url === '/api/v1/me')).toBe(true);
    expect(viewer.value.queue).toBe(2);
  });

  it('offers to show a post hidden pending review again, and keeps with resolve', async () => {
    server.routes[`POST ${postUrl}`] = () => ({
      status: 200,
      body: { action: 'resolve', post: 'restored', flags: [7, 8, 9] },
    });
    const root = await renderAt('/admin/queue');
    const it = item(root, hidden)!;
    await click(root, button(it, 'Keep post'));
    expect(it.textContent).toContain('Show this post again and close its flags?');
    await click(root, button(it, 'Keep'));
    expect(server.sent.find((s) => s.method === 'POST')!.body).toEqual({ action: 'resolve', note: '' });
    expect(item(root, hidden)).toBeNull();
  });

  it('says another moderator got there first, and fetches the queue again', async () => {
    server.routes[`POST ${postUrl}`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/queue');
    const gets = () => server.sent.filter((s) => s.method === 'GET' && s.url === queueUrl).length;
    expect(gets()).toBe(1);
    await confirm(root, item(root, hidden)!, 'Remove post', 'Remove');
    expect(root.querySelector('[data-queue-notice]')!.textContent).toBe(
      'Another moderator has already handled this.',
    );
    expect(gets()).toBe(2);
  });

  it('says an action failed, and fetches the queue again for the state it left', async () => {
    server.routes[`POST ${postUrl}`] = () => problemAnswer('internal', 500);
    const root = await renderAt('/admin/queue');
    await confirm(root, item(root, hidden)!, 'Remove post', 'Remove');
    await settle(root);
    expect(root.querySelector('[data-queue-failure]')!.getAttribute('role')).toBe('alert');
    expect(item(root, hidden)).not.toBeNull();
    expect(server.sent.filter((s) => s.method === 'GET' && s.url === queueUrl)).toHaveLength(2);
  });

  it('says another moderator got there first on a post a moderator hid, too', async () => {
    const q = onePage();
    q.items[0].flag!.post!.removal = 'moderator';
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: q });
    server.routes[`POST ${postUrl}`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/queue');
    await confirm(root, item(root, hidden)!, 'Dismiss flags', 'Dismiss');
    expect(root.querySelector('[data-queue-notice]')!.textContent).toBe(handled);
    expect(root.querySelector('[data-queue-failure]')).toBeNull();
  });

  it('shows the files of a post with no words', async () => {
    const q = onePage();
    const p = q.items[0].flag!.post!;
    p.html = '';
    p.attachments = [{ hash: 'abc123', original_name: 'receipt.png', size: 2048, mime: 'image/png', width: 640, height: 480 }];
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: q });
    const root = await renderAt('/admin/queue');
    const it = item(root, hidden)!;
    const file = it.querySelector<HTMLAnchorElement>('.attachments a')!;
    expect(file.getAttribute('href')).toBe('/blobs/abc123');
    expect(file.textContent).toBe('receipt.png');
    expect(it.textContent).not.toContain('This post has no words to show.');
  });

  it('does not offer to keep a post a moderator hid by hand', async () => {
    const q = onePage();
    q.items[0].flag!.post!.removal = 'moderator';
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: q });
    const root = await renderAt('/admin/queue');
    const it = item(root, hidden)!;
    expect(it.querySelector('[data-post-state]')!.textContent).toBe('Hidden by a moderator');
    expect(button(it, 'Keep post')).toBeNull();
    expect(button(it, 'Remove post')).toBeNull();
    expect(it.querySelector('[data-keep-refused]')!.textContent).toContain('restore it from the thread page');
    expect(button(it, 'Dismiss flags')).not.toBeNull();
  });

  // TKT-01M3R1WPNS. The golden's post was hidden pending review; redacted
  // since, it keeps that removal, and the screen must not offer the restore
  // a review's dismiss would be, nor a Remove or Keep of it.
  it('offers only to dismiss the flags on a redacted post, whatever its removal', async () => {
    const q = onePage();
    const p = q.items[0].flag!.post!;
    Object.assign(p, { redacted: true, deleted: true, removal: 'review', html: '', attachments: [] });
    server.routes[`GET ${queueUrl}`] = () => ({ status: 200, body: q });
    server.routes[`POST ${postUrl}`] = () => ({
      status: 200,
      body: { action: 'dismiss', post: 'already hidden', flags: [7, 8, 9] },
    });
    const root = await renderAt('/admin/queue');
    const it = item(root, hidden)!;
    expect(it.querySelector('[data-post-state]')!.textContent).toBe('Removed permanently');
    expect(it.querySelector('.queue-author')!.textContent).toContain('removed permanently');
    expect(button(it, 'Remove post')).toBeNull();
    expect(button(it, 'Keep post')).toBeNull();
    expect(it.querySelector('[data-keep-refused]')).toBeNull();
    expect(it.querySelector('[data-redacted]')!.textContent).toBe(
      'This post was removed permanently and cannot be shown again.',
    );
    expect(it.textContent).not.toContain('This post has no words to show.');
    await click(root, button(it, 'Dismiss flags'));
    expect(it.textContent).toContain('Dismiss the flags as unfounded?');
    expect(it.textContent).not.toContain('show this post again');
    await click(root, button(it, 'Dismiss'));
    expect(server.sent.find((s) => s.method === 'POST')!.body).toEqual({ action: 'dismiss', note: '' });
    expect(item(root, hidden)).toBeNull();
  });
});

describe("the author's link", () => {
  it('goes to the users screen for a moderator by role', async () => {
    const root = await renderAt('/admin/queue');
    expect(item(root, hidden)!.querySelector('[data-author-link]')!.getAttribute('href')).toBe(
      '/admin/users?prefix=bob',
    );
  });

  // AC6: a member who moderates a forum through a group opens the queue and
  // works it; only the author's link differs, since the users screen is
  // staff-only.
  it('goes to the profile for a member who moderates a forum through a group', async () => {
    const me = asModerator();
    me.user!.role = 'member';
    me.queue = 1;
    viewer.value = me;
    server.routes[`POST ${postUrl}`] = () => ({ status: 200, body: resolution });
    const root = await renderAt('/admin/queue');
    expect(screenOf(root)).toBe('admin-queue');
    expect(item(root, hidden)!.querySelector('[data-author-link]')!.getAttribute('href')).toBe('/u/bob');
    await confirm(root, item(root, hidden)!, 'Dismiss flags', 'Dismiss');
    expect(item(root, hidden)).toBeNull();
  });
});

describe('an application item', () => {
  it('opens the approve and deny controls, and leaves the list once decided', async () => {
    server.routes[`GET ${appUrl(42)}`] = () => ({ status: 200, body: pendingApp() });
    server.routes[`POST ${appUrl(42)}/approve`] = () => ({ status: 200, body: { ...approvedApp(), id: 42 } });
    const root = await renderAt('/admin/queue');
    const it = item(root, 'application:42')!;
    expect(it.querySelector('[data-decision]')).toBeNull();
    await click(root, it.querySelector<HTMLElement>('[data-review]'));
    expect(it.querySelector('[data-decision]')).not.toBeNull();
    const approve = [...it.querySelectorAll<HTMLElement>('[data-decision] section')].find(
      (s) => s.querySelector('h3')?.textContent === 'Approve',
    )!;
    await click(root, button(approve, 'Approve'));
    await click(root, button(approve, 'Approve'));
    expect(server.sent.some((s) => s.method === 'POST' && s.url === `${appUrl(42)}/approve`)).toBe(true);
    expect(item(root, 'application:42')).toBeNull();
  });
});
