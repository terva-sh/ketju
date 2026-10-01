import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser, type Page, type TestInfo } from '@playwright/test';
import { stateFile } from './session';

// The search screen (TKT-01M3V70X0F, docs/07 "Search"), at 390 px and
// 1440 px against the real server and its search index. Each test writes
// the posts it searches for, under a word no other post holds, in
// site-feedback, which only live.spec.ts also writes to, or in the
// restricted moderators forum, which alice cannot read. They write through
// the API from stored sessions (session.setup.ts), so nothing here spends
// /auth/*. The indexer runs after a post is written, so each test waits for
// the API to find its word before it searches on screen. Each test runs one
// axe scan (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans).

test.use({ storageState: stateFile('alice') });

// word is a token no seeded or earlier post holds: letters and digits only,
// so the index's tokenizer keeps it whole.
const word = (info: TestInfo, tag: string) => `zq${tag}${info.project.name.slice(0, 2)}${Date.now().toString(36)}`;

interface Thread {
  id: string;
  slug: string;
}

// start creates a thread in forum through request, and replies to it when
// reply is given.
async function start(request: APIRequestContext, forum: string, title: string, source: string, reply?: string): Promise<Thread> {
  const created = await request.post(`/api/v1/forums/${forum}/threads`, { data: { title, source } });
  expect(created.status(), await created.text()).toBe(201);
  const t = ((await created.json()) as { thread: Thread }).thread;
  if (reply !== undefined) {
    const r = await request.post(`/api/v1/threads/${t.id}/posts`, { data: { source: reply } });
    expect(r.status(), await r.text()).toBe(201);
  }
  return t;
}

// indexed waits until a search for q through request finds n results.
async function indexed(request: APIRequestContext, q: string, n = 1) {
  await expect
    .poll(
      async () => {
        const r = await request.get(`/api/v1/search?q=${encodeURIComponent(q)}`);
        return r.ok() ? ((await r.json()) as { results: unknown[] }).results.length : -1;
      },
      { timeout: 30_000, message: `the index finds ${q}` },
    )
    .toBe(n);
}

// asAda runs fn with ada's stored session: the seeded admin, who may post in
// every forum, the restricted one included.
async function asAda<T>(browser: Browser, info: TestInfo, fn: (request: APIRequestContext) => Promise<T>): Promise<T> {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('ada') });
  try {
    return await fn(ctx.request);
  } finally {
    await ctx.close();
  }
}

// search types q into the screen's field and presses Search.
async function search(page: Page, q: string) {
  await page.getByRole('searchbox', { name: 'Search for' }).fill(q);
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page).toHaveURL((u) => u.searchParams.get('q') === q);
}

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found).toEqual([]);
}

test('a member finds a reply by a unique word and follows it', async ({ page }, info) => {
  const w = word(info, 'r');
  const t = await start(page.request, 'site-feedback', `Search ${info.project.name} ${Date.now()}`, 'An opening post.', `The reply that says ${w} once.`);
  await indexed(page.request, w);

  // From the top bar, or the phone's, to the screen, which starts in its field.
  await page.goto('/');
  await page.getByRole('link', { name: 'Search', exact: true }).click();
  await expect(page).toHaveURL(/\/search$/);
  await expect(page.getByRole('searchbox', { name: 'Search for' })).toBeFocused();

  await search(page, w);
  const result = page.locator('.search-result');
  await expect(result).toHaveCount(1);
  await expect(result.locator('mark')).toHaveText(w);
  await expect(result.locator('[data-result-forum]')).toHaveText('Site feedback');
  await expect(page.getByRole('status')).toHaveText('1 result on this page');
  await scan(page);

  await result.locator('.search-title').click();
  await expect(page).toHaveURL(new RegExp(`/t/${t.id}/${t.slug}/2$`));
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(page.locator('#post-2')).toContainText(w);
});

test('a member finds nothing from a forum they cannot read', async ({ page, browser }, info) => {
  const w = word(info, 'm');
  // ada posts it where only the moderators group reads, and the index has it.
  await asAda(browser, info, async (request) => {
    await start(request, 'moderators', `Moderators only ${w}`, `Kept among moderators: ${w}.`);
    await indexed(request, w, 2);
  });

  await page.goto('/search');
  await search(page, w);
  await expect(page.locator('[data-no-results]')).toHaveText(`Nothing matches “${w}”.`);
  await expect(page.locator('.search-result')).toHaveCount(0);
  await expect(page.getByRole('status')).toHaveText('No results');
  // The forum is not offered as a filter either.
  await expect(page.locator('select[name=forum] option[value=moderators]')).toHaveCount(0);
  const answer = await page.request.get(`/api/v1/search?q=${w}`);
  expect(((await answer.json()) as { results: unknown[] }).results).toEqual([]);
  await scan(page);
});

test('the forum filter and a date filter narrow the results', async ({ page, isMobile }, info) => {
  const w = word(info, 'f');
  await start(page.request, 'site-feedback', `Filters ${info.project.name} ${Date.now()}`, `Filter me by ${w}.`);
  await indexed(page.request, w);

  await page.goto(`/search?q=${w}`);
  await expect(page.locator('.search-result')).toHaveCount(1);
  // On the phone the filters fold under a disclosure, closed while none is set.
  const filters = page.locator('.search-filters');
  if (isMobile) {
    await expect(filters.locator('[name=forum]')).toBeHidden();
    await filters.locator('summary').click();
  }

  await filters.getByLabel('Forum').selectOption('general');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]forum=general(&|$)`));
  await expect(page.locator('[data-no-results]')).toBeVisible();
  if (isMobile) await expect(filters.locator('summary')).toHaveText('Filters (1 set)');
  await scan(page);

  await page.locator('[data-no-results] [data-clear-filters]').click();
  await expect(page).toHaveURL(new RegExp(`/search\\?q=${w}$`));
  await expect(page.locator('.search-result')).toHaveCount(1);

  // To, inclusive, yesterday: the post was written today.
  const yesterday = await page.evaluate(() => {
    const d = new Date();
    d.setDate(d.getDate() - 1);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  });
  if (isMobile) await filters.locator('summary').click();
  await filters.getByLabel('To, inclusive').fill(yesterday);
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page).toHaveURL(/[?&]before=\d+/);
  await expect(page.locator('[data-no-results]')).toBeVisible();
  // From yesterday on finds it again.
  await filters.getByLabel('To, inclusive').fill('');
  await filters.getByLabel('From').fill(yesterday);
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page).toHaveURL(/[?&]after=\d+/);
  await expect(page.locator('.search-result')).toHaveCount(1);
});
