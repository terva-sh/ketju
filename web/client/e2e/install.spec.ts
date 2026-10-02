import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// The installable web app (TKT-01M29CGBYY), run once per project in
// playwright.config.ts: the manifest the server writes is one Chromium
// reads without error and would install from, and nothing registers a
// service worker, because v1 has none (docs/06 "Installable", docs/10).
//
// Whether a real iOS or Android device installs it and opens it standalone
// is the ticket's last criterion, which a person checks: no emulator here
// installs anything.

test.use({ storageState: stateFile('alice') });

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
}

interface AppManifest {
  url: string;
  errors: { message: string; critical: boolean }[];
  data?: string;
}

// The one installability error this test accepts: an older Chromium still
// asks for a service worker, and v1 has none on purpose. The Chromium the
// run was written against reports nothing at all.
const notThePages = new Set(['no-matching-service-worker']);

test('the manifest Chromium reads has no errors', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#app')).not.toBeEmpty();
  const cdp = await page.context().newCDPSession(page);

  // The manifest is fetched once the page has loaded, so poll until
  // Chromium has one.
  let manifest: AppManifest = { url: '', errors: [] };
  await expect
    .poll(async () => {
      manifest = (await cdp.send('Page.getAppManifest')) as AppManifest;
      return manifest.data ?? '';
    })
    .not.toBe('');
  expect(new URL(manifest.url).pathname).toBe('/manifest.webmanifest');
  expect(manifest.errors.map((e) => e.message)).toEqual([]);

  const data = JSON.parse(manifest.data!) as {
    name: string;
    short_name: string;
    display: string;
    start_url: string;
    icons: { src: string; sizes: string; purpose?: string }[];
  };
  const me = (await (await page.request.get('/api/v1/me')).json()) as { site_name: string };
  expect(data.name).toBe(me.site_name);
  expect(data.short_name).toBe(me.site_name);
  expect(data.display).toBe('standalone');
  expect(data.start_url).toBe('/');
  expect(data.icons.map((i) => `${i.sizes} ${i.purpose ?? 'any'}`)).toEqual(['192x192 any', '512x512 any', '512x512 maskable']);
  for (const icon of data.icons) {
    const res = await page.request.get(icon.src);
    expect(res.status(), icon.src).toBe(200);
    expect(res.headers()['content-type'], icon.src).toBe('image/png');
  }

  // Installability fetches the icons and checks the rest of what a browser
  // needs to offer the install.
  const { installabilityErrors } = (await cdp.send('Page.getInstallabilityErrors')) as {
    installabilityErrors: { errorId: string }[];
  };
  const ids = installabilityErrors.map((e) => e.errorId);
  test.info().annotations.push({ type: 'installability', description: ids.join(', ') || 'none' });
  expect(ids.filter((id) => !notThePages.has(id))).toEqual([]);

  await scan(page);
});

test('no service worker is registered, and no style depends on display-mode', async ({ page }) => {
  await page.goto(seed.thread);
  await expect(page.locator('article.post').first()).toBeVisible();

  expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistrations()).length)).toBe(0);

  // The installed app opens in standalone, and the safe-area padding
  // e2e/thread.spec.ts checks holds there only if no rule changes with the
  // display mode. Every stylesheet the page loaded, searched rule by rule,
  // nested media blocks included.
  const found = await page.evaluate(() => {
    const out: string[] = [];
    const walk = (rules: CSSRuleList) => {
      for (const rule of rules) {
        if (rule instanceof CSSMediaRule && rule.conditionText.includes('display-mode')) out.push(rule.conditionText);
        if ('cssRules' in rule) walk((rule as CSSGroupingRule).cssRules);
      }
    };
    for (const sheet of document.styleSheets) walk(sheet.cssRules);
    return { sheets: document.styleSheets.length, out };
  });
  expect(found.sheets).toBeGreaterThan(0);
  expect(found.out).toEqual([]);

  await scan(page);
});
