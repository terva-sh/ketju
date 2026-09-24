import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { clientScope, nav, routes, serverOwned } from './routes';
import { act } from 'preact/test-utils';
import { renderAt, screenOf, settle } from './testing';

// One concrete URL per line of docs/07's URL scheme, and the screen it must
// reach. /auth/* is the server's and is not a client route.
const urls: [string, string][] = [
  ['/', 'forum-index'],
  ['/f/general', 'thread-list'],
  ['/f/general/new', 'new-thread'],
  ['/t/01k4z3j0w8x2c/a-thread', 'thread'],
  ['/t/01k4z3j0w8x2c/a-thread?page=3&lens=author:alice', 'thread'],
  ['/t/01k4z3j0w8x2c/a-thread/42', 'thread'],
  ['/t/01k4z3j0w8x2c/a-thread/42/history', 'post-history'],
  ['/unread', 'unread'],
  ['/new', 'new'],
  ['/participated', 'participated'],
  ['/watching', 'watching'],
  ['/search?q=rust', 'search'],
  ['/u/alice', 'profile'],
  ['/u/alice/posts', 'profile-posts'],
  ['/settings', 'settings'],
  ['/settings/sessions', 'settings-sessions'],
  ['/settings/identities', 'settings-identities'],
  ['/apply', 'apply'],
  ['/apply/status', 'apply-status'],
  ['/admin', 'admin'],
  ['/admin/queue', 'admin-queue'],
  ['/admin/applications', 'admin-applications'],
  ['/admin/forums', 'admin-forums'],
  ['/admin/users', 'admin-users'],
  ['/admin/forms', 'admin-forms'],
  ['/admin/settings', 'admin-settings'],
];

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the route table', () => {
  // AC1: every path in the URL scheme resolves to its screen, through the
  // real Router, lazy admin chunk included.
  it.each(urls)('%s renders %s', async (url, screen) => {
    // The thread screen asks the API for the thread; nothing answers here.
    vi.stubGlobal('fetch', () => new Promise(() => {}));
    expect(screenOf(await renderAt(url))).toBe(screen);
  });

  it.each(['/nope', '/t/01k4z3j0w8x2c', '/admin/nope', '/f', '/u/alice/posts/extra'])(
    '%s is not found',
    async (url) => {
      expect(screenOf(await renderAt(url))).toBe('not-found');
    },
  );

  // A link to a path the server owns is left to the browser, so it reaches
  // the server; a link to a client route is routed in the page.
  it.each([
    ['/auth/github/start', false],
    ['/auth/logout', false],
    ['/api/v1/me', false],
    ['/blobs/abc', false],
    ['/events', false],
    ['/assets/index.js', false],
    ['/t/01k4z3j0w8x2c/a-thread', true],
    ['/admin/queue', true],
    ['/', true],
  ])('a click on a link to %s is routed in the page: %s', async (href, routed) => {
    vi.stubGlobal('fetch', () => new Promise(() => {}));
    const root = await renderAt('/');
    const a = document.createElement('a');
    a.href = href;
    a.textContent = 'go';
    root.appendChild(a);
    let prevented: boolean | undefined;
    // Registered after the router's, so it sees the router's decision. It
    // then stops the default itself so the test document goes nowhere.
    const seen = (e: Event) => {
      prevented = e.defaultPrevented;
      e.preventDefault();
    };
    addEventListener('click', seen);
    try {
      a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, composed: true, button: 0 }));
    } finally {
      removeEventListener('click', seen);
    }
    expect(prevented).toBe(routed);
    expect(location.pathname === href).toBe(routed);
  });

  // A spelling clientScope cannot see through is resolved by the browser,
  // routed in the page, and then handed to the server by the fallback.
  it.each(['/./auth/github/start', '/t/../auth/logout', '/%61uth/github/start'])(
    'a link to %s still reaches the server',
    async (href) => {
      vi.stubGlobal('fetch', () => new Promise(() => {}));
      const root = await renderAt('/');
      const left: string[] = [];
      const spy = vi.spyOn(nav, 'leave').mockImplementation((u) => void left.push(u));
      const a = document.createElement('a');
      a.href = href;
      root.appendChild(a);
      try {
        await act(async () => {
          a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, composed: true, button: 0 }));
        });
        await settle(root);
      } finally {
        spy.mockRestore();
      }
      const resolved = new URL(href, location.origin).pathname;
      expect(left.map((u) => new URL(u).pathname)).toEqual([resolved]);
      expect(screenOf(root)).toBeNull();
    },
  );

  it.each(['/authors', '/AUTH/github/start', '/%E0%A4%A'])('does not hand %s to the server', async (url) => {
    const spy = vi.spyOn(nav, 'leave').mockImplementation(() => {});
    try {
      expect(screenOf(await renderAt(url))).toBe('not-found');
      expect(spy).not.toHaveBeenCalled();
    } finally {
      spy.mockRestore();
    }
  });

  it.each([
    ['/auth/github/start', true],
    ['/auth', true],
    ['/%61uth/logout', true],
    ['/api/v1/me', true],
    ['/events', true],
    ['/authors', false],
    ['/AUTH/x', false],
    ['/t/01k4z3j0w8x2c/auth', false],
    ['/%E0%A4%A', false],
  ])('serverOwned %s: %s', (path, owned) => {
    expect(serverOwned(path)).toBe(owned);
  });

  it.each([
    ['/auth/github/start', false],
    ['/auth', false],
    ['/API/v1/me', true],
    ['https://ketju.test/auth/github/start', false],
    ['//ketju.test/auth/logout', false],
    ['/events?x=1', false],
    ['/eventsource', true],
    ['/authors', true],
    ['/t/01k4z3j0w8x2c/auth', true],
    ['/u/api', true],
  ])('clientScope %s: %s', (href, inScope) => {
    expect(clientScope.test(href)).toBe(inScope);
  });

  // The table and the document say the same thing, in both directions, so
  // neither can gain a path the other lacks without this failing.
  it('matches docs/07', () => {
    // Vitest runs from web/client, where vite.config.ts is.
    const doc = readFileSync('../../docs/07-api-and-clients.md', 'utf8');
    const block = doc.split('## URL scheme')[1].split('```')[1];
    const documented = new Set<string>();
    for (const line of block.split('\n')) {
      // "path [path ...]    description": the paths are the first column.
      const column = line.trim().split(/\s{3,}/)[0];
      for (const path of column.split(/\s+/).filter((p) => p.startsWith('/'))) {
        if (path.startsWith('/auth/')) continue;
        documented.add(shape(path.split('?')[0]));
      }
    }
    const table = new Set(routes.map((r) => shape(r.path)));
    expect([...table].filter((p) => !documented.has(p))).toEqual([]);
    expect([...documented].filter((p) => !table.has(p))).toEqual([]);
    expect(documented.size).toBeGreaterThan(20);
  });
});

// shape reduces {param} and :param alike to ":", so the document's names
// and the table's may differ while the shapes must not.
const shape = (p: string) => p.replace(/\{[^}]+\}|:[a-z]+/g, ':');
