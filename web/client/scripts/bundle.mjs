// The thread view's size budget (docs/07 "Preact client", TKT-01M29CGBYH):
// the JavaScript and CSS a reader loads, gzipped, must stay within the number
// in ../budget.json. Fonts do not count (docs/07 "Fonts").
//
// It reads the build Vite left in internal/web/spa/dist. The roots are the
// /assets scripts and stylesheets the built shell names. From each script it
// follows the manifest's static imports and every chunk's CSS, and it follows
// dynamic imports too, except the admin screens: they are lazy by design and
// never part of reading a thread. A lazy reader chunk added later therefore
// still counts. Each file is gzipped at level 9 and the sizes are summed.
//
// This file measures. budget.mjs is the command that runs it on the real
// build and decides the exit code.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

const here = dirname(fileURLToPath(import.meta.url));

// The lazy admin screens (CEE52), which TestTheAdminBundleIsSeparate keeps
// out of the entry. Only these are left out of the count.
const adminPrefix = 'src/screens/admin/';

export function readBudget(file = join(here, '..', 'budget.json')) {
  const n = JSON.parse(readFileSync(file, 'utf8')).threadViewGzipBytes;
  if (!Number.isInteger(n) || n <= 0) {
    throw new Error(`${file}: threadViewGzipBytes must be a positive integer, got ${n}`);
  }
  return n;
}

// measure returns the files the thread view loads and their gzipped sizes,
// in the order they were reached.
export function measure(distDir) {
  const manifest = JSON.parse(readFileSync(join(distDir, '.vite', 'manifest.json'), 'utf8'));
  const byFile = new Map(Object.entries(manifest).map(([key, chunk]) => [chunk.file, key]));
  const shell = readFileSync(join(distDir, 'index.html'), 'utf8');

  const roots = [...shell.matchAll(/<(?:script|link)\b[^>]*?\b(?:src|href)="\/(assets\/[^"]+\.(?:js|css))"/g)].map(
    (m) => m[1],
  );
  if (roots.length === 0) {
    throw new Error(`${distDir}/index.html names no script or stylesheet under /assets`);
  }

  const files = [];
  const seenFiles = new Set();
  const seenKeys = new Set();
  const add = (file) => {
    if (seenFiles.has(file)) return;
    seenFiles.add(file);
    files.push(file);
  };
  const walk = (key) => {
    if (seenKeys.has(key)) return;
    seenKeys.add(key);
    const chunk = manifest[key];
    if (!chunk) throw new Error(`the manifest has no entry ${key}`);
    add(chunk.file);
    for (const css of chunk.css ?? []) add(css);
    for (const i of chunk.imports ?? []) walk(i);
    for (const i of chunk.dynamicImports ?? []) {
      if (!i.startsWith(adminPrefix)) walk(i);
    }
  };
  for (const root of roots) {
    const key = byFile.get(root);
    if (key) walk(key);
    else add(root);
  }

  // Only chunk files and their CSS are ever added, so a font (a chunk's
  // `assets`) is never reached.
  return files.map((file) => ({ file, gzip: gzipSync(readFileSync(join(distDir, file)), { level: 9 }).length }));
}

// check measures distDir against budget and returns the report lines and
// whether the total is within it.
export function check(distDir, budget) {
  const files = measure(distDir);
  const total = files.reduce((n, f) => n + f.gzip, 0);
  const pct = ((100 * total) / budget).toFixed(1);
  const lines = files.map((f) => `  ${String(f.gzip).padStart(7)} B  ${f.file}`);
  lines.push(`thread-view bundle: ${total} B gzip / ${budget} B budget (${pct}%)`);
  const ok = total <= budget;
  if (!ok) {
    lines.push(`over budget by ${total - budget} B; the budget is web/client/budget.json (docs/07)`);
  }
  return { ok, total, lines };
}
