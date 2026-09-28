import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readPosition, setReadPosition, stateFile } from './session';

// The read-tracking beacon (TKT-01M29CGBWN), run once per project in
// playwright.config.ts, so the phone layout is held to the same assertions
// as the desktop (AC3): a post counts as read after a second on screen, and
// the position goes out when the reader navigates in the app and when the
// page goes away.
//
// carol reads "Minutes from the September meeting", which no other spec
// reads as her, and each test sets her position there itself before it
// starts, so neither the order the tests run in nor the other width's run
// changes what they see.

async function asCarol(browser: Browser, info: TestInfo) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  return { ctx, page: await ctx.newPage() };
}

// minutes is page 1 of the thread, from its row in General's list.
async function minutes(page: Page): Promise<string> {
  await page.goto('/f/general');
  const title = page.locator('[data-thread]', { hasText: 'Minutes from the September meeting' }).locator('.thread-title');
  const href = (await title.getAttribute('href'))!;
  expect(href).toMatch(/\?page=1$/);
  return href;
}

const isPut = (r: { method(): string; url(): string }) =>
  r.method() === 'PUT' && /^\/api\/v1\/threads\/[^/]+\/read$/.test(new URL(r.url()).pathname);

// puts records the ordinal of every read beacon the page sends while it is
// loaded. Playwright does not report one sent as the page unloads, so a test
// of that reads the stored position instead.
function puts(page: Page): number[] {
  const sent: number[] = [];
  page.on('request', (r) => {
    if (isPut(r)) sent.push((JSON.parse(r.postData()!) as { ordinal: number }).ordinal);
  });
  return sent;
}

// onScreen is the posts that count as on screen now, by the tracker's rule
// (state/readTracker.ts): half of the post in the viewport, or half the
// viewport filled by it.
const onScreen = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.posts > [id^="post-"]')]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        const shown = Math.min(r.bottom, innerHeight) - Math.max(r.top, 0);
        return shown > 0 && shown >= 0.5 * Math.min(r.height, innerHeight);
      })
      .map((el) => Number(el.getAttribute('data-ordinal'))),
  );

// Longer than the second a post must stay on screen, with room for the
// observer to report.
const dwell = 1600;

// A navigation in the app, as a link click is: the thread's forum.
const toForum = (page: Page) => page.locator('.thread-head a.forum').dispatchEvent('click');

// AC1, and AC4's navigation.
test('a post scrolled past in under a second is not read, and navigating sends what was', async ({ browser }, info) => {
  const { ctx, page } = await asCarol(browser, info);
  const at = await minutes(page);
  await setReadPosition(ctx.request, at, 0);
  const sent = puts(page);
  await page.goto(at);
  await expect(page.locator('#post-1')).toBeVisible();

  // To the foot of the page and straight back.
  await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight));
  await page.waitForTimeout(250);
  const passed = await onScreen(page);
  await page.evaluate(() => scrollTo(0, 0));
  const top = await onScreen(page);
  expect(Math.max(...top)).toBeLessThan(Math.min(...passed));
  await page.waitForTimeout(dwell);
  expect(sent).toEqual([]);

  const put = page.waitForResponse((r) => isPut(r.request()));
  await toForum(page);
  await expect(page).toHaveURL(/\/f\/general$/);
  expect((await put).status()).toBe(200);
  expect(sent).toHaveLength(1);
  expect(top).toContain(sent[0]);
  expect(sent[0]).toBeLessThan(Math.min(...passed));
  expect(await readPosition(ctx.request, at)).toBe(sent[0]);
  await ctx.close();
});

// AC4's page hide: a full navigation away unloads the page, and the
// beacon is sent with keepalive as it goes.
test('leaving the page sends what was read', async ({ browser }, info) => {
  const { ctx, page } = await asCarol(browser, info);
  const at = await minutes(page);
  await setReadPosition(ctx.request, at, 0);
  const sent = puts(page);
  await page.goto(at);
  await expect(page.locator('#post-1')).toBeVisible();
  const top = await onScreen(page);
  await page.waitForTimeout(dwell);
  expect(sent).toEqual([]);

  await page.goto('/f/general');
  await expect.poll(() => readPosition(ctx.request, at)).toBeGreaterThan(0);
  expect(top).toContain(await readPosition(ctx.request, at));
  await ctx.close();
});

// AC2: read further on, then back, and nothing lower goes out.
test('reading an earlier page after a later one sends nothing behind it', async ({ browser }, info) => {
  const { ctx, page } = await asCarol(browser, info);
  const at = await minutes(page);
  await setReadPosition(ctx.request, at, 0);
  const sent = puts(page);
  await page.goto(at.replace(/\?page=1$/, '?page=2'));
  await expect(page.locator('#post-26')).toBeVisible();
  const later = await onScreen(page);
  await page.waitForTimeout(dwell);

  const put = page.waitForResponse((r) => isPut(r.request()));
  await page.locator('.pager a[rel=prev]').first().dispatchEvent('click');
  await expect(page).toHaveURL(/\?page=1$/);
  expect((await put).status()).toBe(200);
  expect(sent).toHaveLength(1);
  expect(later).toContain(sent[0]);

  await expect(page.locator('#post-1')).toBeVisible();
  await page.waitForTimeout(dwell);
  await toForum(page);
  await expect(page).toHaveURL(/\/f\/general$/);
  // Long enough for a request the navigation sent to have been seen.
  await page.waitForTimeout(500);
  expect(sent).toHaveLength(1);
  expect(await readPosition(ctx.request, at)).toBe(sent[0]);
  await ctx.close();
});
