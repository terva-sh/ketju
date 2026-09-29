import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clientScope, nav, routes, serverOwned } from './routes';
import { act } from 'preact/test-utils';
import type { Me } from './api/dto';
import { isPhone } from './state/layout';
import { viewer } from './state/viewer';
import { renderAt, route, screenOf, settle } from './testing';
import preUser from '../../../internal/dto/testdata/golden/me.pre_user.json';
import anonMe from '../../../internal/dto/testdata/golden/me.anon.json';
import memberMe from '../../../internal/dto/testdata/golden/me.member.json';

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
  ['/notifications', 'notifications'],
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

  // A link whose path cannot be decoded names nothing: NotFound, not a
  // crash, and the app still routes the next link.
  it.each(['/f/%', '/t/01k4z3j0w8x2c/%FF', '/u/%E0%A4%A/posts'])(
    'a link to %s is not found, and the app recovers',
    async (href) => {
      vi.stubGlobal('fetch', () => new Promise(() => {}));
      const root = await renderAt('/');
      const click = async (to: string) => {
        const a = document.createElement('a');
        a.href = to;
        root.appendChild(a);
        await act(async () => {
          a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, composed: true, button: 0 }));
        });
        a.remove();
        await settle(root);
      };
      await click(href);
      expect(screenOf(root)).toBe('not-found');
      await click('/unread');
      expect(screenOf(root)).toBe('unread');
    },
  );

  // The server served this document at a server-owned address, so the server
  // treats it as an application route after all. Handing it back would
  // reload the page forever.
  it('never hands back the address the document was loaded at', async () => {
    const spy = vi.spyOn(nav, 'leave').mockImplementation(() => {});
    const was = nav.loadedAt;
    nav.loadedAt = '/auth/github/start?x=1';
    try {
      expect(screenOf(await renderAt('/auth/github/start?x=1'))).toBe('not-found');
      expect(spy).not.toHaveBeenCalled();
      await renderAt('/auth/github/start?x=2');
      expect(spy).toHaveBeenCalledTimes(1);
    } finally {
      nav.loadedAt = was;
      spy.mockRestore();
    }
  });

  it.each(['/authors', '/AUTH/github/start', '/%E0%A4%A', '/auth%2Flogout', '/events/x'])('does not hand %s to the server', async (url) => {
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
    ['/auth%2Flogout', false],
    ['/blobs%2Fabc', false],
    ['/events/', false],
    ['/events/x', false],
    ['/assets', true],
    ['/assets/x.js', true],
    ['/blobs/abc', true],
    ['/api', true],
    ['/api%2Fv1%2Fme', true],
    ['/apix', false],
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
    ['/events/x', true],
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

// An approval-mode applicant (TKT-01M29CKRG9): with pre_user set in /me,
// every address but /apply is the status page, and the shell offers
// sign-out and no way into the forum. The server refuses the rest anyway;
// this is what the reader sees.
describe('an applicant', () => {
  let saved: Me;
  beforeEach(() => {
    saved = viewer.value;
    viewer.value = structuredClone(preUser) as Me;
    vi.stubGlobal('fetch', () => new Promise(() => {}));
  });
  afterEach(() => {
    viewer.value = saved;
  });

  it.each([
    ['/', 'apply-status'],
    ['/f/general', 'apply-status'],
    ['/t/abc/x', 'apply-status'],
    ['/t/01k4z3j0w8x2c/a-thread/42', 'apply-status'],
    ['/admin/applications', 'apply-status'],
    ['/nope', 'apply-status'],
    ['/apply/status', 'apply-status'],
    ['/apply', 'apply'],
  ])('%s renders %s', async (url, screen) => {
    expect(screenOf(await renderAt(url))).toBe(screen);
    // The address is replaced with the page shown, so Back does not return
    // to an address the server would refuse (TKT-01M29CP2NG).
    expect(location.pathname).toBe(screen === 'apply' ? '/apply' : '/apply/status');
  });

  it('replaces the address rather than adding one', async () => {
    await renderAt('/f/general');
    const depth = history.length;
    await route('/t/abc/x');
    await settle(document.body);
    expect(location.pathname).toBe('/apply/status');
    expect(history.length).toBe(depth + 1);
  });

  it('draws no signed-out hint', async () => {
    const root = await renderAt('/apply/status');
    expect(root.querySelector('[data-shell="signin-hint"]')).toBeNull();
  });

  it('can sign out, and has no forum navigation', async () => {
    const root = await renderAt('/f/general');
    const form = root.querySelector<HTMLFormElement>('[data-shell="applicant-bar"] form');
    expect(form?.getAttribute('method')).toBe('post');
    expect(form?.getAttribute('action')).toBe('/auth/logout');
    expect(form?.querySelector('button[type="submit"]')?.textContent).toBe('Sign out');
    expect(root.querySelectorAll('[data-nav], [data-shell="tabbar"], [data-shell="topbar"]')).toHaveLength(0);
    // The only link is the skip link: nothing leads into the forum.
    expect([...root.querySelectorAll('a')].map((a) => a.getAttribute('href'))).toEqual(['#main']);
  });

  it('is the member shell again once pre_user is gone', async () => {
    viewer.value = structuredClone(saved);
    expect(screenOf(await renderAt('/f/general'))).toBe('thread-list');
  });
});

// Approval retires an applicant's pre-user session, so the approved
// applicant comes back signed out (TKT-01M3Q2V25E). The signed-out shell
// says where the decision is, at both widths; a member is not told.
describe('the signed-out hint', () => {
  let saved: Me;
  beforeEach(() => {
    saved = viewer.value;
    vi.stubGlobal('fetch', () => new Promise(() => {}));
  });
  afterEach(() => {
    viewer.value = saved;
    isPhone.value = false;
  });

  it.each([
    ['desktop', false],
    ['phone', true],
  ])('is drawn for a signed-out reader on the %s', async (layout, phone) => {
    isPhone.value = phone;
    viewer.value = structuredClone(anonMe) as Me;
    const root = await renderAt('/f/general?x=1');
    expect(root.querySelector('[data-layout]')!.getAttribute('data-layout')).toBe(layout);
    const hint = root.querySelector('[data-shell="signin-hint"]')!;
    expect(hint.textContent).toBe('Applied? Sign in to see your decision');
    // The same login page Sign in reaches, coming back here.
    expect(hint.querySelector('a')!.getAttribute('href')).toBe('/auth/signin?return_to=%2Ff%2Fgeneral%3Fx%3D1');
  });

  it('is not drawn for a member', async () => {
    viewer.value = structuredClone(memberMe) as Me;
    const root = await renderAt('/f/general');
    expect(root.querySelector('[data-shell="signin-hint"]')).toBeNull();
  });
});
