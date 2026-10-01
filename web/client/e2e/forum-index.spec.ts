import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
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
  // The heading is the site's name, the text the wordmark shows (docs/06
  // "Thread list views (forum index)").
  const wordmark = (await page.locator('[data-shell] .wordmark:visible').first().textContent())!.trim();
  expect(wordmark).not.toBe('');
  await expect(page.locator('.forum-index-head h1')).toHaveText(wordmark);

  // Before anything is focused or opened, at the page's full height: a
  // taller viewport rather than a full-page capture, so the tab bar and the
  // floating button sit where a reader sees them, at the foot. The last
  // posts' times are masked, since the seed's times are relative to when it
  // ran (TKT-01M39WBN4Q, as e2e/thread.spec.ts does).
  if (process.env.KETJU_SCREENSHOTS === '1') {
    const dir = join(dirname(fileURLToPath(import.meta.url)), 'screenshots');
    mkdirSync(dir, { recursive: true });
    await page.evaluate(() => document.fonts.ready.then(() => undefined));
    const vp = page.viewportSize()!;
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    await page.setViewportSize({ width: vp.width, height: Math.max(vp.height, height) });
    for (const colorScheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme });
      await page.screenshot({
        path: join(dir, `forum-index-${info.project.name}-${colorScheme}.png`),
        animations: 'disabled',
        caret: 'hide',
        mask: [page.locator('[data-screen="forum-index"] time')],
        maskColor: '#8B95A8',
      });
    }
    await page.emulateMedia({ colorScheme: null });
    await page.setViewportSize(vp);
  }

  // At the tablet breakpoint the name column keeps most of the row (review
  // on PR #86): the fixed columns do not crush it.
  if (!isMobile) {
    const vp = page.viewportSize()!;
    await page.setViewportSize({ width: 720, height: vp.height });
    const name = (await page.locator('[data-forum="general"] .forum-main').boundingBox())!;
    expect(name.width).toBeGreaterThanOrEqual(200);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(720);
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

  // Escape pressed straight after opening, with nothing between, closes it
  // too, and focus goes back (TKT-01M3TS0ZV9).
  const opener = isMobile ? fab : page.locator('.new-thread');
  await opener.click();
  await page.keyboard.press('Escape');
  await expect(picker).toHaveCount(0);
  await expect(opener).toBeFocused();

  await ctx.close();
});

// The footer (docs/06 "Thread list views (forum index)", TKT-01M3S3ZRYD):
// the member count is a member's alone, the post count sums only the
// forums the reader is shown, so the restricted Moderators forum alice
// cannot read adds nothing, and the shortcut hint is the desktop's.
const sumOfRows = (page: Page) =>
  page.locator('[data-forum] [data-posts]').evaluateAll((els) => els.reduce((n, el) => n + Number(el.getAttribute('data-posts')), 0));

test('an anonymous reader’s footer counts the posts they can read and no members', async ({ page }) => {
  await page.goto('/');
  const foot = page.locator('.forum-index-foot');
  await expect(foot).toBeVisible();
  await expect(foot.locator('[data-members]')).toHaveCount(0);
  await expect(foot).not.toContainText('member');
  const total = Number(await foot.locator('[data-posts-total]').getAttribute('data-posts-total'));
  expect(total).toBe(await sumOfRows(page));
  expect(total).toBeGreaterThan(0);
});

test('a member’s footer names the members, and the desktop offers the shortcut list', async ({ browser, isMobile }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  const page = await ctx.newPage();
  await page.goto('/');
  const foot = page.locator('.forum-index-foot');
  await expect(foot).toBeVisible();
  await expect(foot).toContainText(/^[\d\s.,  ]+ members · [\d\s.,  ]+ posts/);
  expect(Number(await foot.locator('[data-members]').getAttribute('data-members'))).toBeGreaterThan(0);
  await expect(page.locator('[data-forum="moderators"]')).toHaveCount(0);
  expect(Number(await foot.locator('[data-posts-total]').getAttribute('data-posts-total'))).toBe(await sumOfRows(page));

  const hint = foot.getByRole('button', { name: 'Press ? for keyboard shortcuts' });
  if (isMobile) {
    await expect(hint).toHaveCount(0);
    await page.keyboard.press('?');
    await expect(page.getByRole('dialog')).toHaveCount(0);
    // The footer keeps clear of the floating button above the tab bar.
    await foot.scrollIntoViewIfNeeded();
    const f = (await foot.boundingBox())!;
    const fab = (await page.locator('[data-fab]').boundingBox())!;
    const text = (await foot.locator('span').first().boundingBox())!;
    expect(text.x + text.width <= fab.x || f.y + f.height <= fab.y || f.y >= fab.y + fab.height).toBe(true);
    const { violations } = await new AxeBuilder({ page }).include('.forum-index-foot').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
    await ctx.close();
    return;
  }
  await hint.click();
  const list = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(list).toContainText('These keys work on a thread page.');
  await page.keyboard.press('Escape');
  await expect(list).toHaveCount(0);
  await expect(hint).toBeFocused();
  // ? opens it here too, as the hint says.
  await page.keyboard.press('?');
  await expect(list).toBeVisible();
  // The one scan, with the list open over the footer.
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
  await ctx.close();
});
