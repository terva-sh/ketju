import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { stateFile } from './session';

// Live updates on the thread page (TKT-01M29CTYA0, docs/06 "Live updates"),
// at 390 px and 1440 px against the real server and its GET /events. alice
// reads a thread she starts in site-feedback, which no other spec reads, and
// carol replies to it through the API from her own stored session
// (session.setup.ts), so nothing here spends /auth/*. Each test runs one axe
// scan (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans).

test.use({ storageState: stateFile('alice') });

const forum = 'site-feedback';

// start has alice start a thread, tall or short: a tall first post keeps a
// reader at the top of the page well above its foot.
async function start(page: Page, info: TestInfo, tall: boolean) {
  const source = tall
    ? Array.from({ length: 60 }, (_, i) => `Paragraph ${i + 1} of a long opening post.`).join('\n\n')
    : 'A short opening post.';
  const created = await page.request.post(`/api/v1/forums/${forum}/threads`, {
    data: { title: `Live ${info.project.name} ${info.title.slice(0, 20)} ${Date.now()}`, source },
  });
  expect(created.status(), await created.text()).toBe(201);
  return ((await created.json()) as { thread: { id: string; slug: string } }).thread;
}

// replyAsCarol posts a reply to thread id from carol's stored session.
async function replyAsCarol(browser: Browser, info: TestInfo, id: string, source: string) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  try {
    const r = await ctx.request.post(`/api/v1/threads/${id}/posts`, { data: { source } });
    expect(r.status(), await r.text()).toBe(201);
  } finally {
    await ctx.close();
  }
}

// open goes to the thread's first page and waits for the post list and, if
// streaming, for the stream's answer.
async function open(page: Page, t: { id: string; slug: string }, streaming = true) {
  const stream = streaming ? page.waitForResponse((r) => r.url().includes(`/events?threads=${t.id}`)) : null;
  await page.goto(`/t/${t.id}/${t.slug}?page=1`);
  await expect(page.locator('#post-1')).toBeVisible();
  if (stream) expect((await stream).status()).toBe(200);
}

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found).toEqual([]);
}

test('a new post waits behind the pill', async ({ page, browser }, info) => {
  const t = await start(page, info, true);
  await open(page, t);
  const top = (await page.locator('#post-1').boundingBox())!.y;

  await replyAsCarol(browser, info, t.id, 'Carol was here.');
  const pill = page.getByRole('button', { name: '1 new post — show' });
  await expect(pill).toBeVisible();
  await expect(page.locator('#post-2')).toHaveCount(0);
  expect((await page.locator('#post-1').boundingBox())!.y, 'the post in view did not move').toBe(top);
  await expect(page.locator('.thread-meta')).toContainText('2 posts');
  await scan(page);

  await pill.click();
  await expect(page.locator('#post-2')).toBeFocused();
  await expect(page.locator('#post-2')).toContainText('Carol was here.');
  await expect(pill).toHaveCount(0);
});

test('a reader at the bottom sees the post arrive', async ({ page, browser }, info) => {
  const t = await start(page, info, false);
  await open(page, t);

  await replyAsCarol(browser, info, t.id, 'Straight in.');
  await expect(page.locator('#post-2')).toContainText('Straight in.');
  await expect(page.locator('button.new-posts')).toHaveCount(0);
  await scan(page);
});

// On a phone the reply bar covers the foot of the screen, and the end of a
// list still behind it is not in view (Terva review 1778).
test('a list end behind the reply bar is not the bottom', async ({ page, browser, isMobile }, info) => {
  test.skip(!isMobile, 'the reply bar is the phone layout');
  const t = await start(page, info, true);
  await open(page, t);
  await expect(page.locator('.replybar')).toBeVisible();
  // The list's end 10 px below the foot of the screen: within 48 px of the
  // viewport, but 74 px past the top of the 64 px bar, which is where what
  // the reader can see ends.
  await page.evaluate(() => scrollBy(0, document.querySelector('.posts')!.getBoundingClientRect().bottom - (innerHeight + 10)));
  const end = await page.evaluate(() => document.querySelector('.posts')!.getBoundingClientRect().bottom - innerHeight);
  expect(Math.round(end)).toBe(10);

  await replyAsCarol(browser, info, t.id, 'Held under the bar.');
  await expect(page.getByRole('button', { name: '1 new post — show' })).toBeVisible();
  await expect(page.locator('#post-2')).toHaveCount(0);
  await scan(page);

  // Scrolled clear of the bar, the reader is at the bottom and it is drawn.
  await page.evaluate(() => scrollBy(0, 200));
  await expect(page.locator('#post-2')).toContainText('Held under the bar.');
});

test('polling takes over when the stream is refused', async ({ page, browser }, info) => {
  await page.clock.install();
  let refused = 0;
  await page.route('**/events?*', (r) => (refused++, r.abort()));
  const t = await start(page, info, true);
  await open(page, t, false);
  await expect.poll(() => refused).toBeGreaterThan(0);

  await replyAsCarol(browser, info, t.id, 'Found by a poll.');
  await page.clock.runFor(31_000);
  await expect(page.getByRole('button', { name: '1 new post — show' })).toBeVisible();
  await expect(page.locator('#post-2')).toHaveCount(0);
  await scan(page);
});
