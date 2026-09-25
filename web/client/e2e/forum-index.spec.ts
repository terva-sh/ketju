import { expect, test } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { stateFile } from './session';

// The forum index (TKT-01M29CEE5Y), run once per project in
// playwright.config.ts against the e2e seed's forum tree (cmd/ketju
// e2e.go).
//
// With KETJU_SCREENSHOTS=1 it also writes the screenshots committed beside
// it, e2e/screenshots/forum-index-{phone,desktop}-{light,dark}.png, which the
// pull request compares by eye against docs/mockups/Main.dc.html,
// FrontDark.dc.html and MobileFront{Light,Dark}.dc.html. CI runs the
// assertions and writes nothing.

test('an anonymous reader sees public forums and no unread counts', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('[data-forum="general"]')).toBeVisible();
  await expect(page.locator('[data-forum="introductions"]')).toBeVisible();
  // Members only and restricted: absent, not locked.
  await expect(page.locator('[data-forum="garden"]')).toHaveCount(0);
  await expect(page.locator('[data-forum="moderators"]')).toHaveCount(0);
  await expect(page.locator('.forum-unread')).toHaveCount(0);
  await expect(page.locator('[data-fab]')).toHaveCount(0);
});

test('a member sees counts, empty forums, and a new-thread button clear of the tab bar', async ({ browser, isMobile }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  const page = await ctx.newPage();
  await page.goto('/');
  await expect(page.locator('[data-forum="general"] [data-unread]')).toBeVisible();
  await expect(page.locator('[data-forum="garden"]')).toBeVisible();
  await expect(page.locator('[data-forum="moderators"]')).toHaveCount(0);
  await expect(page.locator('[data-forum="reading"] [data-no-threads] a')).toHaveAttribute('href', '/f/reading/new');

  // Before anything is focused or opened, at the page's full height: a
  // taller viewport rather than a full-page capture, so the tab bar and the
  // floating button sit where a reader sees them, at the foot.
  if (process.env.KETJU_SCREENSHOTS === '1') {
    const dir = join(dirname(fileURLToPath(import.meta.url)), 'screenshots');
    mkdirSync(dir, { recursive: true });
    const vp = page.viewportSize()!;
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    await page.setViewportSize({ width: vp.width, height: Math.max(vp.height, height) });
    for (const colorScheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme });
      await page.screenshot({ path: join(dir, `forum-index-${info.project.name}-${colorScheme}.png`) });
    }
    await page.emulateMedia({ colorScheme: null });
    await page.setViewportSize(vp);
  }

  const fab = page.locator('[data-fab]');
  if (isMobile) {
    // AC3: above the tab bar, and inside the viewport.
    await expect(fab).toBeVisible();
    const f = (await fab.boundingBox())!;
    const bar = (await page.locator('[data-shell="tabbar"]').boundingBox())!;
    const vp = page.viewportSize()!;
    expect(f.y + f.height).toBeLessThanOrEqual(bar.y);
    expect(f.y).toBeGreaterThanOrEqual(0);
    expect(f.x + f.width).toBeLessThanOrEqual(vp.width);
    await fab.click();
  } else {
    await expect(fab).toBeHidden();
    await page.locator('.new-thread').click();
  }
  const picker = page.getByRole('dialog', { name: 'Start a thread in…' });
  await expect(picker.getByRole('link', { name: 'General', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(picker).toHaveCount(0);

  await ctx.close();
});
