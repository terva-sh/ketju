// @vitest-environment node
//
// TKT-01M29CGBYH: the bundle budget counts what the thread view loads and
// nothing else, fails over the committed number, and takes that number from
// budget.json alone.

import { spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
import { afterEach, describe, expect, it } from 'vitest';
import { check, measure, readBudget } from './bundle.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const temps = [];
afterEach(() => {
  for (const d of temps.splice(0)) rmSync(d, { recursive: true, force: true });
});

// fixture writes a dist shaped like Vite's: a shell naming an early script,
// the entry and its CSS; the entry importing a shared chunk statically, a
// lazy reader chunk and the lazy admin screen dynamically; and a font.
// Random bytes do not compress, so each file's size is its own.
function fixture(dist = mkdtempSync(join(tmpdir(), 'ketju-budget-'))) {
  if (!temps.includes(dist)) temps.push(dist);
  mkdirSync(join(dist, '.vite'), { recursive: true });
  mkdirSync(join(dist, 'assets'), { recursive: true });
  const files = {
    'assets/early.js': 300,
    'assets/index.js': 4000,
    'assets/index.css': 1000,
    'assets/shared.js': 2000,
    'assets/lazy.js': 1500,
    'assets/lazy.css': 500,
    'assets/AdminRoot.js': 9000,
    'assets/admin.css': 9000,
    'assets/font.woff2': 9000,
    'assets/unreferenced.js': 9000,
  };
  for (const [f, n] of Object.entries(files)) writeFileSync(join(dist, f), randomBytes(n));
  // One file that compresses, so the level the sizes are taken at shows.
  const rules = Array.from({ length: 400 }, (_, i) => `.r${i}{color:#${randomBytes(3).toString('hex')};margin:${i % 7}px}`);
  writeFileSync(join(dist, 'assets/index.css'), rules.join('\n'));
  writeFileSync(
    join(dist, '.vite', 'manifest.json'),
    JSON.stringify({
      '_early.js': { file: 'assets/early.js', src: '_early.js' },
      'index.html': {
        file: 'assets/index.js',
        isEntry: true,
        imports: ['_shared.js'],
        dynamicImports: ['src/screens/Lazy.tsx', 'src/screens/admin/AdminRoot.tsx'],
        css: ['assets/index.css'],
        assets: ['assets/font.woff2'],
      },
      '_shared.js': { file: 'assets/shared.js', imports: ['index.html'] },
      'src/screens/Lazy.tsx': {
        file: 'assets/lazy.js',
        isDynamicEntry: true,
        imports: ['index.html', '_shared.js'],
        css: ['assets/lazy.css'],
      },
      'src/screens/admin/AdminRoot.tsx': {
        file: 'assets/AdminRoot.js',
        isDynamicEntry: true,
        imports: ['index.html'],
        css: ['assets/admin.css'],
      },
      '_unreferenced.js': { file: 'assets/unreferenced.js' },
    }),
  );
  writeFileSync(
    join(dist, 'index.html'),
    `<!DOCTYPE html><html><head>
  <script src="/assets/early.js" nonce="{{.Nonce}}"></script>
  <script type="module" crossorigin src="/assets/index.js" nonce="{{.Nonce}}"></script>
  <link rel="stylesheet" crossorigin href="/assets/index.css" nonce="{{.Nonce}}">
</head><body></body></html>`,
  );
  return dist;
}

const gz = (dist, f) => gzipSync(readFileSync(join(dist, f)), { level: 9 }).length;

describe('measure', () => {
  it('counts the shell, static and lazy reader chunks and their CSS, and nothing else', () => {
    const dist = fixture();
    const got = measure(dist);
    expect(got.map((f) => f.file).sort()).toEqual(
      ['assets/early.js', 'assets/index.css', 'assets/index.js', 'assets/lazy.css', 'assets/lazy.js', 'assets/shared.js'],
    );
    for (const f of got) expect(f.gzip).toBe(gz(dist, f.file));
  });

  it('refuses a shell that names nothing, rather than measuring zero', () => {
    const dist = fixture();
    writeFileSync(join(dist, 'index.html'), '<!DOCTYPE html><html></html>');
    expect(() => measure(dist)).toThrow(/names no script or stylesheet/);
  });
});

describe('check', () => {
  it('passes at the budget and fails one byte over it', () => {
    const dist = fixture();
    const total = measure(dist).reduce((n, f) => n + f.gzip, 0);

    const at = check(dist, total);
    expect(at.ok).toBe(true);
    expect(at.total).toBe(total);
    expect(at.lines.at(-1)).toBe(`thread-view bundle: ${total} B gzip / ${total} B budget (100.0%)`);

    const over = check(dist, total - 1);
    expect(over.ok).toBe(false);
    expect(over.lines.at(-1)).toMatch(/^over budget by 1 B/);
  });
});

describe('budget.json', () => {
  it('holds the design number, 120 KB read as 120000 bytes (docs/07)', () => {
    expect(readBudget()).toBe(120000);
  });
});

describe('npm run budget', () => {
  // A copy of the client's scripts and budget.json beside a fixture dist,
  // laid out as the repository is, so the command runs unmodified.
  function tree(budget) {
    const root = mkdtempSync(join(tmpdir(), 'ketju-budget-tree-'));
    temps.push(root);
    const scripts = join(root, 'web', 'client', 'scripts');
    mkdirSync(scripts, { recursive: true });
    for (const f of ['budget.mjs', 'bundle.mjs']) copyFileSync(join(here, f), join(scripts, f));
    writeFileSync(join(root, 'web', 'client', 'budget.json'), JSON.stringify({ threadViewGzipBytes: budget }));
    const dist = join(root, 'internal', 'web', 'spa', 'dist');
    fixture(dist);
    return { script: join(scripts, 'budget.mjs'), total: measure(dist).reduce((n, f) => n + f.gzip, 0) };
  }
  const run = (script, args = [], env = {}) =>
    spawnSync(process.execPath, [script, ...args], { encoding: 'utf8', env: { ...process.env, ...env } });

  it('exits 0 within budget and prints every file and the total', () => {
    const { script } = tree(1_000_000);
    const r = run(script);
    expect(r.status).toBe(0);
    expect(r.stdout).toMatch(/assets\/index\.js/);
    expect(r.stdout).toMatch(/thread-view bundle: \d+ B gzip \/ 1000000 B budget/);
  });

  it('exits 1 over budget, and no flag or variable talks it out of that', () => {
    const { script, total } = tree(1);
    const r = run(script, ['--budget=1000000', '1000000'], {
      THREAD_VIEW_GZIP_BYTES: '1000000',
      KETJU_BUNDLE_BUDGET: '1000000',
      BUDGET: '1000000',
    });
    expect(r.status).toBe(1);
    expect(r.stdout).toContain(`over budget by ${total - 1} B`);
  });

  it('reads neither argv nor the environment', () => {
    for (const f of ['budget.mjs', 'bundle.mjs']) {
      const src = readFileSync(join(here, f), 'utf8');
      expect(src, f).not.toMatch(/process\.argv|process\.env|\bargv\b|import\.meta\.env/);
    }
  });
});
