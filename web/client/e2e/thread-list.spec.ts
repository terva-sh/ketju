import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setReadPosition, stateFile } from './session';

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
  // Reading there moves alice on (TKT-01M29CGBWN), in this test or any
  // other, so her position goes back to the seeded #30 first and the list
  // is loaded again.
  await setReadPosition(ctx.request, (await minutes.locator('.thread-title').getAttribute('href'))!, 30);
  await page.reload();
  const badge = minutes.locator('[data-unread]');
  await expect(badge).toBeVisible();
  const first = Number(/\/(\d+)$/.exec((await badge.getAttribute('href'))!)![1]);
  expect(first).toBe(31);
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

// TKT-01M3YSQPZX: a chip is a link to the forum's list filtered to its tag
// (docs/06 "Filtering by tag"). Only the six-tag thread carries long-read.
// One axe scan, of the filtered list with its chips and header.
test('a tag chip filters the list, and Show all threads returns', async ({ page }) => {
  await page.goto('/f/general');
  const long = row(page, 'Comparing every rain barrel');
  const chip = long.locator('.thread-tags a', { hasText: 'long-read' });
  await expect(chip).toHaveAttribute('href', '/f/general?tag=long-read');
  await chip.click();
  await expect(page).toHaveURL(/\/f\/general\?tag=long-read$/);
  await expect(page.locator('[data-tagged]')).toHaveText(/^Tagged long-read\s*Show all threads$/);
  await expect(page.locator('[data-thread]')).toHaveCount(1);
  await expect(row(page, 'Comparing every rain barrel')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)).toEqual([]);

  await page.locator('[data-tagged] a', { hasText: 'Show all threads' }).click();
  await expect(page).toHaveURL(/\/f\/general$/);
  await expect(page.locator('[data-tagged]')).toHaveCount(0);
  await expect(page.locator('[data-thread]', { hasText: 'House rules' })).toBeVisible();
});

// On a phone a chip keeps its drawn size, under 44 px tall, and its target
// is 44 px about its centre (docs/06 "Targets and gestures"): pressing 21 px
// above and below it, and across where it is wide enough, still finds it,
// as e2e/targets.spec.ts presses every control.
test('a tag chip has a 44 px target on a phone without growing', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the 44 px rule is the phone layout’s');
  await page.goto('/f/general');
  const chips = row(page, 'Comparing every rain barrel').locator('.thread-tags a');
  await expect(chips).toHaveCount(6);
  const found = await chips.evaluateAll((els) =>
    els.map((el) => {
      el.scrollIntoView({ block: 'center' });
      const r = el.getBoundingClientRect();
      const x = r.left + r.width / 2;
      const y = r.top + r.height / 2;
      const hit = (px: number, py: number) => {
        const t = document.elementFromPoint(px, py);
        return !!t && (t === el || el.contains(t));
      };
      return {
        tag: el.textContent,
        height: r.height,
        reach: [hit(x, y), hit(x, y - 21), hit(x, y + 21), hit(x - Math.min(21, r.width / 2 - 1), y), hit(x + Math.min(21, r.width / 2 - 1), y)],
      };
    }),
  );
  for (const f of found) {
    expect(f.height, f.tag!).toBeLessThan(30);
    expect(f.reach, f.tag!).toEqual([true, true, true, true, true]);
  }
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

  // Back pressed straight after scrolling, inside the save's debounce
  // (review on PR #88): forward again finds the latest position.
  await page.goForward();
  await expect(page).toHaveURL(url);
  await expect.poll(async () => Math.abs((await scrolled()) - onSecond)).toBeLessThanOrEqual(10);
  await page.mouse.wheel(0, 300);
  await expect.poll(scrolled).toBeGreaterThan(onSecond + 150);
  const late = await scrolled();
  await page.goBack();
  await expect(page).not.toHaveURL(/cursor=/);
  await page.goForward();
  await expect(page).toHaveURL(url);
  await expect.poll(async () => Math.abs((await scrolled()) - late)).toBeLessThanOrEqual(10);
});
