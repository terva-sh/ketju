import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from './routes';
import { renderAt, screenOf } from './testing';

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

  it.each(['/nope', '/t/01k4z3j0w8x2c', '/admin/nope', '/f', '/u/alice/posts/extra', '/auth/github/start'])(
    '%s is not found',
    async (url) => {
      expect(screenOf(await renderAt(url))).toBe('not-found');
    },
  );

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
