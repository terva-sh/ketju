import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser, type Page, type TestInfo } from '@playwright/test';
import { paceAuth, setReadPosition, signIn, stateFile } from './session';

// The cross-forum lists and Mark all read (TKT-01M3V6Y4JP, docs/06 "Thread
// list views" and "Read tracking"), run once per project in
// playwright.config.ts, so at 390 px and 1440 px. Each test runs one axe
// scan (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans).
//
// The site-wide mark only moves forward, so nothing can undo it on a
// session another spec reads (Terva review 1743). It is made by nell, a
// member seeded for it (cmd/ketju e2e.go seedMarker), who is signed in here
// and nowhere else; the sign-in is paced (e2e/session.ts). The forum-level
// mark is alice's, in site-feedback, where no other spec reads her place:
// live.spec.ts reads only the threads it starts there.
//
// Each test has carol start a thread in site-feedback first, from her
// stored session, so there is something unread to clear whatever ran
// before, the other project's run included.

const forum = 'site-feedback';

async function startAsCarol(browser: Browser, info: TestInfo, what: string): Promise<string> {
  const title = `${what} ${info.project.name} ${Date.now()}`;
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  try {
    const r = await ctx.request.post(`/api/v1/forums/${forum}/threads`, { data: { title, source: 'Something to read.' } });
    expect(r.status(), await r.text()).toBe(201);
  } finally {
    await ctx.close();
  }
  return title;
}

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found).toEqual([]);
}

// badge is a navigation entry's count where this width draws it: the tab
// bar on a phone, the top bar on a desktop.
const badge = (page: Page, isMobile: boolean, nav: string) =>
  page.locator(`${isMobile ? '.tabbar' : '.topbar'} [data-nav=${nav}] [data-badge]`);

const row = (page: Page, title: string) => page.locator('[data-thread]', { hasText: title });

const fitsWidth = async (page: Page) =>
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);

test('a member’s unread and new threads, and Mark all read for the site', async ({ browser, isMobile }, info) => {
  await paceAuth(2);
  const title = await startAsCarol(browser, info, 'Unread list');
  const ctx = await browser.newContext({ ...info.project.use });
  const page = await ctx.newPage();
  try {
    await signIn(page, 'nell');

    // New: nell has never opened it.
    await page.goto('/new');
    await expect(row(page, title)).toBeVisible();
    await expect(badge(page, isMobile, 'new')).toBeVisible();

    await page.goto('/unread');
    const r = row(page, title);
    await expect(r).toBeVisible();
    await expect(r.locator('[data-forum-link]')).toHaveText('Site feedback');
    await expect(r.locator('[data-forum-link]')).toHaveAttribute('href', `/f/${forum}`);
    await expect(r.locator('[data-unread]')).toHaveText('1 new');
    await expect(badge(page, isMobile, 'unread')).toBeVisible();
    await fitsWidth(page);
    await scan(page);

    // The mark clears the list and both badges without a reload.
    const before = await page.evaluate(() => performance.getEntriesByType('navigation').length);
    await page.locator('[data-mark-read]').click();
    await expect(page.locator('[data-empty-list]')).toHaveText('Nothing unread. Threads with posts you have not read appear here.');
    await expect(page.locator('[data-thread]')).toHaveCount(0);
    await expect(badge(page, isMobile, 'unread')).toHaveCount(0);
    await expect(badge(page, isMobile, 'new')).toHaveCount(0);
    expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(before);
    await expect(page).toHaveURL(/\/unread$/);

    // And the server agrees.
    const me = (await (await page.request.get('/api/v1/me')).json()) as { unread: { threads: number; new: number } };
    expect(me.unread).toMatchObject({ threads: 0, new: 0 });
  } finally {
    await ctx.close();
  }
});

// minutesPath is the seeded thread alice has read to #30 in general.
async function minutesPath(request: APIRequestContext): Promise<string> {
  const r = await request.get('/api/v1/forums/general/threads');
  expect(r.status()).toBe(200);
  const list = (await r.json()) as { threads: { id: string; slug: string; title: string }[] };
  const t = list.threads.find((t) => t.title === 'Minutes from the September meeting')!;
  return `/t/${t.id}/${t.slug}`;
}

test('Mark all read in a forum clears that forum and no other', async ({ browser }, info) => {
  const title = await startAsCarol(browser, info, 'Forum mark');
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  const page = await ctx.newPage();
  try {
    // An unread thread elsewhere: the seed's position, which
    // thread-list.spec.ts also sets before it reads.
    await setReadPosition(ctx.request, await minutesPath(ctx.request), 30);

    await page.goto(`/f/${forum}`);
    await expect(row(page, title).locator('[data-unread]')).toBeVisible();
    await fitsWidth(page);
    await scan(page);
    await page.locator('.thread-list-head [data-mark-read]').click();
    await expect(page.locator('[data-thread] [data-unread]')).toHaveCount(0);
    await expect(row(page, title)).toBeVisible();

    // /unread holds none of this forum's threads. carol's is the newest
    // activity on the site, so it would be on the first page.
    await page.goto('/unread');
    await expect(page.locator('[data-thread]').first()).toBeVisible();
    await expect(page.locator(`[data-forum-link="${forum}"]`)).toHaveCount(0);
    // The other forum's thread is as unread as it was.
    await page.goto('/f/general');
    await expect(row(page, 'Minutes from the September meeting').locator('[data-unread]')).toHaveText('30 new');
  } finally {
    await ctx.close();
  }
});
