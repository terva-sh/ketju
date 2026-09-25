import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { stateFile } from './session';

// The thread list (TKT-01M29CEE6D), run once per project in
// playwright.config.ts against the e2e seed (cmd/ketju e2e.go): general
// holds a pinned thread, a thread alice has read to post 30, and one with a
// 150-character title and six tags; chatter holds 60 one-post threads, so it
// pages.
//
// With KETJU_SCREENSHOTS=1 it also writes e2e/screenshots/thread-list-
// {phone,desktop}-{light,dark}.png. There is no thread-list mockup (settled
// in grooming); the pull request compares them with docs/06 "Thread list
// views" and the forum index's rows.

const row = (page: Page, title: string) => page.locator('[data-thread]', { hasText: title });

test('the badge lands on the first unread post and the title on page 1', async ({ browser }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  const page = await ctx.newPage();
  await page.goto('/f/general');
  const minutes = row(page, 'Minutes from the September meeting');
  const badge = minutes.locator('[data-unread]');
  await expect(badge).toBeVisible();
  // Seeded at 30, so the first project's run lands on 31; reading there
  // moves alice on, and the second's lands on whatever is first unread now.
  const first = Number(/\/(\d+)$/.exec((await badge.getAttribute('href'))!)![1]);
  expect(first).toBeGreaterThanOrEqual(31);
  await badge.click();
  const post = page.locator(`#post-${first}`);
  await expect(post).toBeInViewport();
  await expect(page.locator('#post-1')).toHaveCount(0);

  await page.goBack();
  await minutes.locator('.thread-title').click();
  await expect(page).toHaveURL(/\?page=1$/);
  await expect(page.locator('#post-1')).toBeVisible();
  await expect(page.locator(`#post-${first}`)).toHaveCount(0);
  await ctx.close();
});

test('pinned threads come first and look different', async ({ page }) => {
  await page.goto('/f/general');
  const rows = page.locator('[data-thread]');
  await expect(rows.first()).toContainText('House rules');
  await expect(rows.first()).toHaveClass(/row-pinned/);
  await expect(rows.first().locator('.thread-flag.pinned')).toHaveText('Pinned');
  const bg = (i: number) => rows.nth(i).evaluate((r) => getComputedStyle(r).backgroundColor);
  expect(await bg(0)).not.toBe(await bg(1));
});

test('rows stay readable with a long title and six tags', async ({ browser }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  const page = await ctx.newPage();
  await page.goto('/f/general');
  const long = row(page, 'Comparing every rain barrel');
  await expect(long).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
  // Every tag and the counts are on screen, inside the row.
  const box = (await long.boundingBox())!;
  for (const part of [long.locator('.thread-tags li').last(), long.locator('.thread-row-meta')]) {
    await expect(part).toBeVisible();
    const b = (await part.boundingBox())!;
    expect(b.x + b.width).toBeLessThanOrEqual(box.x + box.width);
  }
  await expect(long.locator('.thread-tags li')).toHaveCount(6);
  // Two lines of title at most.
  const title = long.locator('.thread-title');
  const lines = await title.evaluate((t) => t.getBoundingClientRect().height / parseFloat(getComputedStyle(t).lineHeight));
  expect(lines).toBeLessThanOrEqual(2.1);

  if (process.env.KETJU_SCREENSHOTS === '1') {
    const dir = join(dirname(fileURLToPath(import.meta.url)), 'screenshots');
    mkdirSync(dir, { recursive: true });
    const vp = page.viewportSize()!;
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    await page.setViewportSize({ width: vp.width, height: Math.max(vp.height, height) });
    for (const colorScheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme });
      await page.screenshot({ path: join(dir, `thread-list-${info.project.name}-${colorScheme}.png`) });
    }
    await page.emulateMedia({ colorScheme: null });
    await page.setViewportSize(vp);
  }
  await ctx.close();
});

// The links are followed with a dispatched click: Playwright's click
// scrolls its target into view first, and the list would then remember
// where Playwright put it rather than where the reader was.
test('back from a thread returns to the page and the place in the list', async ({ page }) => {
  const scrolled = () => page.evaluate(() => scrollY);
  await page.goto('/f/chatter');
  await expect(page.locator('[data-thread]')).toHaveCount(25);
  await page.mouse.wheel(0, 600);
  await expect.poll(scrolled).toBeGreaterThan(300);
  const onFirst = await scrolled();

  await page.locator('[data-older]').dispatchEvent('click');
  await expect(page).toHaveURL(/[?&]cursor=/);
  await expect(page.locator('[data-thread]').first()).toContainText('Chatter thread 26');
  // A page pushed starts at the top.
  await expect.poll(scrolled).toBe(0);
  await page.mouse.wheel(0, 500);
  await expect.poll(scrolled).toBeGreaterThan(250);
  const onSecond = await scrolled();
  const url = page.url();

  await page.locator('[data-thread] .thread-title').nth(12).dispatchEvent('click');
  await expect(page.locator('article.post').first()).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(url);
  await expect(page.locator('[data-thread]').first()).toContainText('Chatter thread 26');
  await expect.poll(async () => Math.abs((await scrolled()) - onSecond)).toBeLessThanOrEqual(10);

  await page.goBack();
  await expect(page).not.toHaveURL(/cursor=/);
  await expect.poll(async () => Math.abs((await scrolled()) - onFirst)).toBeLessThanOrEqual(10);
});
