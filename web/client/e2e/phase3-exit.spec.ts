import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readPosition, setReadPosition, stateFile } from './session';

// Phase 3's exit (TKT-01M3V3CFRE, docs/09 and docs/14): "daily-driver
// quality for an active community, on a phone as much as on a desktop". A
// member reads by the lenses, finds unread threads, is alerted, searches,
// watches a reply arrive live, attaches an image, moves between pages on a
// phone, installs the app, sets their page size and opens a profile, at 390
// px and 1440 px. Each test makes one axe scan (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts scans). The page picker test is the phone's alone,
// because the sheet is (docs/06 "Pages").
//
// The selectors and the seed are the feature specs' (lenses, unread-lists,
// alerts, search, live, attachments, phone-sheets, install, settings and
// profile), as their tickets landed them. Each reader starts from a stored
// session (session.setup.ts), so nothing here spends /auth/*. A second
// member, carol or ada, writes through the API from their own stored
// session.
//
// Writes go into threads each test starts in site-feedback, or into the
// main thread's read position and settings, which each test sets before it
// reads and puts back where other specs read it.

test.use({ storageState: stateFile('alice') });

const seed: { thread: string; restricted_word: string } = JSON.parse(
  readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'),
);
const png = join(dirname(fileURLToPath(import.meta.url)), 'fixtures', 'pixel.png');
const forum = 'site-feedback';

async function scan(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, found.join('\n')).toEqual([]);
}

interface Thread {
  id: string;
  slug: string;
}

const path = (t: Thread) => `/t/${t.id}/${t.slug}`;

// as runs fn with who's stored session, in a context at this project's width.
async function as<T>(who: string, browser: Browser, info: TestInfo, fn: (request: APIRequestContext) => Promise<T>): Promise<T> {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile(who) });
  try {
    return await fn(ctx.request);
  } finally {
    await ctx.close();
  }
}

async function start(request: APIRequestContext, title: string, source: string, where = forum): Promise<Thread> {
  const r = await request.post(`/api/v1/forums/${where}/threads`, { data: { title, source } });
  expect(r.status(), await r.text()).toBe(201);
  return ((await r.json()) as { thread: Thread }).thread;
}

async function reply(request: APIRequestContext, t: Thread, source: string) {
  const r = await request.post(`/api/v1/threads/${t.id}/posts`, { data: { source } });
  expect(r.status(), await r.text()).toBe(201);
}

// unreadIn is t's unread count on the first page of /unread for the reader
// signed in on request, 0 when t is not there.
async function unreadIn(request: APIRequestContext, t: Thread): Promise<number> {
  const r = await request.get('/api/v1/unread');
  expect(r.status(), await r.text()).toBe(200);
  const rows = ((await r.json()) as { threads: { id: string; unread: number }[] }).threads;
  return rows.find((row) => row.id === t.id)?.unread ?? 0;
}

const title = (info: TestInfo, what: string) => `${what} ${info.project.name} ${Date.now()}`;

// ordinals are the posts the page draws, in order.
const ordinals = (page: Page) =>
  page.locator('.posts > [id^="post-"]').evaluateAll((els) => els.map((e) => Number(e.getAttribute('data-ordinal'))));

// firstUnread presses First unread where this width offers it: the toolbar
// on a desktop, the more menu on a phone.
async function firstUnread(page: Page, isMobile: boolean) {
  if (isMobile) {
    await page.locator('[data-shell=phone-topbar] .more > button').click();
    await page.locator('[data-shell=more-menu] [data-action=first-unread]').click();
  } else await page.locator('[data-shell=toolbar] [data-action=first-unread]').click();
}

// The bar each width draws its navigation in.
const bar = (isMobile: boolean) => (isMobile ? '.tabbar' : '.topbar');

test.describe('lenses', () => {
  // alice's place in the main thread is #50, the foot of page 2, so the
  // first unread is #51 whatever earlier specs read.
  test('a member reads a thread through the unread lens and jumps to the first unread', async ({ page, isMobile }) => {
    await setReadPosition(page.request, seed.thread, 50);
    await page.goto(`${seed.thread}?page=1`);
    await expect(page.locator('#post-1')).toBeVisible();

    // The phone's Lens is a sheet, the desktop's a switcher.
    const lens = page.locator(isMobile ? '[data-lens-sheet]' : '[data-lens-switch]');
    await lens.click();
    if (isMobile) await expect(page.getByRole('dialog', { name: 'Lens' })).toBeVisible();
    await page.locator('#lens-menu [data-lens-item=unread]').click();
    await expect(page).toHaveURL(/[?&]lens=unread:50$/);
    await expect(page.locator('.lens-header')).toBeVisible();
    await expect(page.locator('#post-51')).toBeVisible();
    expect((await ordinals(page))[0]).toBe(51);
    await scan(page);

    // The lens page reads on while it is open and flushes as it unloads, so
    // the position is put back once it has gone.
    await page.goto('/f/general');
    await page.waitForTimeout(500);
    await setReadPosition(page.request, seed.thread, 50);
    await page.goto(`${seed.thread}?page=1`);
    await expect(page.locator('#post-1')).toBeVisible();
    await firstUnread(page, isMobile);
    await expect(page).toHaveURL(/\/51$/);
    await expect(page.locator('#post-51')).toBeInViewport();
  });

  test('a member narrows a thread to its starter with only-OP and returns to the full thread at the same ordinal', async ({
    page,
  }) => {
    await page.goto(`${seed.thread}?page=1`);
    // carol started the main thread and wrote its odd posts (cmd/ketju e2e.go).
    const only = page.locator('.thread-head [data-lens-only]');
    await expect(only).toHaveText('Only carol');
    await only.click();
    await expect(page).toHaveURL(/\?lens=author:carol$/);
    await expect(page.locator('.lens-header')).toContainText('Posts by carol');
    expect((await ordinals(page)).slice(0, 3)).toEqual([1, 3, 5]);
    await scan(page);

    // #9 at the top of the viewport, below the header that sticks there,
    // and Full thread goes to it.
    const full = page.locator('[data-full-thread]');
    await page.locator('#post-9').evaluate((el) => {
      // Twice: the first scroll sticks the header, which moves its edge.
      for (let i = 0; i < 2; i++)
        scrollBy(0, el.getBoundingClientRect().top - document.querySelector('.lens-header')!.getBoundingClientRect().bottom);
    });
    await expect(full).toBeInViewport();
    await expect(full).toHaveAttribute('href', /\/9$/);
    await full.click();
    await expect(page).toHaveURL(/\/t\/[^/]+\/[^/?]+\/9$/);
    await expect(page.locator('.lens-header')).toHaveCount(0);
    await expect(page.locator('#post-9')).toBeInViewport();
  });
});

test('a member finds a thread with new posts on the Unread surface, opens it at the first unread, and the badge clears once they have read it', async ({
  page,
  browser,
  isMobile,
}, info) => {
  // carol starts a thread; alice has read its first post; carol adds two.
  const name = title(info, 'Unread surface');
  const t = await as('carol', browser, info, (request) => start(request, name, 'The first post, which alice has read.'));
  await setReadPosition(page.request, path(t), 1);
  await as('carol', browser, info, async (request) => {
    await reply(request, t, 'The second post.');
    await reply(request, t, 'The third post.');
  });
  // A reply's 201 means the log holds it. /unread reads the database
  // projection, which follows the event bus (docs/03 "Write path", step 5),
  // so under load the list can still count one reply, or none, for a
  // moment. The screen fetches the list once, so wait for the server to
  // count both before opening it (TKT-01M3ZE2211).
  await expect
    .poll(() => unreadIn(page.request, t), { timeout: 15_000, message: 'the projection counts both replies' })
    .toBe(2);

  await page.goto('/');
  await page.locator(`${bar(isMobile)} [data-nav=unread]`).click();
  await expect(page).toHaveURL(/\/unread$/);
  const row = page.locator('[data-thread]', { hasText: name });
  await expect(row.locator('[data-unread]')).toHaveText('2 new');
  await scan(page);

  await row.locator('[data-unread]').click();
  await expect(page).toHaveURL(new RegExp(`${path(t)}/2$`));
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(page.locator('#post-3')).toBeInViewport();
  // Both on screen for the read-tracking second; going back to the list
  // sends the position. A phone's thread page draws no tab bar, so Back is
  // the way out at either width.
  await page.waitForTimeout(1600);
  await page.goBack();
  await expect(page).toHaveURL(/\/unread$/);
  await expect.poll(() => readPosition(page.request, path(t))).toBe(3);
  await expect(page.locator('[data-thread]', { hasText: name })).toHaveCount(0);
});

test('a member is notified of a mention and of a reply in a watched thread, and opening each lands on its post and clears the Alerts badge', async ({
  page,
  browser,
  isMobile,
}, info) => {
  // Every alert alice has is read first, so the count is this test's alone.
  const cleared = await page.request.post('/api/v1/notifications/read', { data: { up_to: Number.MAX_SAFE_INTEGER } });
  expect(cleared.status(), await cleared.text()).toBe(200);
  const count = async () => ((await (await page.request.get('/api/v1/me')).json()) as { notifications: number }).notifications;
  expect(await count()).toBe(0);

  const mentionTitle = title(info, 'Mentioned');
  const watchTitle = title(info, 'Watched');
  const [mentioned, watched] = await as('carol', browser, info, async (request) => [
    await start(request, mentionTitle, 'Something to discuss.'),
    await start(request, watchTitle, 'News will follow.'),
  ]);
  const watching = await page.request.post(`/api/v1/threads/${watched.id}/watch`, { data: { level: 'watch' } });
  expect(watching.status(), await watching.text()).toBe(200);
  await as('carol', browser, info, async (request) => {
    await reply(request, mentioned, '@alice, have a look at this.');
    await reply(request, watched, 'The news, as promised.');
  });
  // Generation follows the event bus.
  await expect.poll(count, { timeout: 15_000 }).toBe(2);

  await page.goto('/');
  const alerts = page.locator(`${bar(isMobile)} [data-nav=alerts]`);
  await expect(alerts.locator('[data-badge]')).toHaveText(/^2\b/);
  await alerts.click();
  await expect(page).toHaveURL(/\/notifications$/);
  const mention = page.locator('[data-alert]').filter({ has: page.getByRole('link', { name: mentionTitle }) });
  const watch = page.locator('[data-alert]').filter({ has: page.getByRole('link', { name: watchTitle }) });
  await expect(mention).toHaveAttribute('data-kind', 'mention');
  await expect(watch).toHaveAttribute('data-kind', 'thread_you_watch');
  // The rows shown are marked read: the badge goes.
  await expect(alerts.locator('[data-badge]')).toHaveCount(0);
  await scan(page);

  await mention.getByRole('link', { name: mentionTitle }).click();
  await expect(page).toHaveURL(new RegExp(`${path(mentioned)}/2$`));
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(page.locator('#post-2')).toContainText('have a look at this');

  await page.goBack();
  await expect(page).toHaveURL(/\/notifications$/);
  await watch.getByRole('link', { name: watchTitle }).click();
  await expect(page).toHaveURL(new RegExp(`${path(watched)}/2$`));
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(page.locator('#post-2')).toContainText('The news, as promised.');
  expect(await count()).toBe(0);
});

test.describe('search', () => {
  // indexed waits until a search for q through request finds n results.
  async function indexed(request: APIRequestContext, q: string, n: number) {
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

  async function search(page: Page, q: string) {
    await page.getByRole('searchbox', { name: 'Search for' }).fill(q);
    await page.getByRole('button', { name: 'Search', exact: true }).click();
    await expect(page).toHaveURL((u) => u.searchParams.get('q') === q);
  }

  test('a member finds a post by a word in its body, sees a snippet with the word marked, and opens the post at its permalink', async ({
    page,
    browser,
  }, info) => {
    // A word no other post holds, written by carol in a reply.
    const word = `zqexit${info.project.name.slice(0, 2)}${Date.now().toString(36)}`;
    const t = await as('carol', browser, info, async (request) => {
      const t = await start(request, title(info, 'Search exit'), 'An opening post.');
      await reply(request, t, `The reply that says ${word} once.`);
      return t;
    });
    await indexed(page.request, word, 1);

    await page.goto('/');
    await page.getByRole('link', { name: 'Search', exact: true }).click();
    await expect(page).toHaveURL(/\/search$/);
    await search(page, word);
    const result = page.locator('.search-result');
    await expect(result).toHaveCount(1);
    await expect(result.locator('mark')).toHaveText(word);
    await expect(result).toContainText('The reply that says');
    await scan(page);

    await result.locator('.search-title').click();
    await expect(page).toHaveURL(new RegExp(`${path(t)}/2$`));
    await expect(page.locator('#post-2')).toBeInViewport();
    await expect(page.locator('#post-2')).toContainText(word);
  });

  test('a member’s search finds nothing from a forum they cannot read', async ({ page, browser }, info) => {
    // The seed's word, in mona's post in the restricted moderators forum:
    // ada, an admin, finds it, so the index has it.
    const word = seed.restricted_word;
    await as('ada', browser, info, (request) => indexed(request, word, 1));

    await page.goto('/search');
    await search(page, word);
    await expect(page.locator('[data-no-results]')).toHaveText(`Nothing matches “${word}”.`);
    await expect(page.locator('.search-result')).toHaveCount(0);
    await expect(page.getByRole('status')).toHaveText('No results');
    const answer = await page.request.get(`/api/v1/search?q=${word}`);
    expect(((await answer.json()) as { results: unknown[] }).results).toEqual([]);
    await scan(page);
  });
});

test.describe('live', () => {
  // open goes to the thread's first page and waits for its stream.
  async function open(page: Page, t: Thread) {
    const stream = page.waitForResponse((r) => r.url().includes(`/events?threads=${t.id}`));
    await page.goto(`${path(t)}?page=1`);
    await expect(page.locator('#post-1')).toBeVisible();
    expect((await stream).status()).toBe(200);
  }

  test('a reply posted by another member while the reader is mid-thread raises the new posts pill, and show brings it in without moving the page', async ({
    page,
    browser,
  }, info) => {
    // A tall first post keeps alice well above the foot of the page.
    const source = Array.from({ length: 60 }, (_, i) => `Paragraph ${i + 1} of a long opening post.`).join('\n\n');
    const t = await start(page.request, title(info, 'Live mid-thread'), source);
    await open(page, t);
    const top = (await page.locator('#post-1').boundingBox())!.y;

    await as('carol', browser, info, (request) => reply(request, t, 'Carol was here.'));
    const pill = page.getByRole('button', { name: '1 new post — show' });
    await expect(pill).toBeVisible();
    await expect(page.locator('#post-2')).toHaveCount(0);
    expect((await page.locator('#post-1').boundingBox())!.y, 'the post in view did not move').toBe(top);
    await scan(page);

    // Show reveals it in place, on the page in view (docs/06 "Live
    // updates"): no navigation, and the new post takes focus.
    const url = page.url();
    const loads = await page.evaluate(() => performance.getEntriesByType('navigation').length);
    await pill.click();
    await expect(page.locator('#post-2')).toBeFocused();
    await expect(page.locator('#post-2')).toContainText('Carol was here.');
    await expect(pill).toHaveCount(0);
    expect(page.url()).toBe(url);
    expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(loads);
  });

  test('at the bottom of the thread a new reply streams in directly', async ({ page, browser }, info) => {
    const t = await start(page.request, title(info, 'Live at the foot'), 'A short opening post.');
    await open(page, t);

    await as('carol', browser, info, (request) => reply(request, t, 'Straight in.'));
    await expect(page.locator('#post-2')).toContainText('Straight in.');
    await expect(page.locator('button.new-posts')).toHaveCount(0);
    await scan(page);
  });
});

test('a member attaches an image to a reply, sees its thumbnail in the post, and opens the full image', async ({
  page,
  isMobile,
}, info) => {
  const t = await start(page.request, title(info, 'Attachment'), 'Photos go below.');
  const text = `A photo from the ${info.project.name} run, ${Date.now()}.`;
  await page.goto(`${path(t)}?page=1`);
  await expect(page.locator('#post-1')).toBeVisible();

  // The phone's composer is the sheet the reply bar opens.
  if (isMobile) await page.locator('[data-shell=replybar] .replybar-open').click();
  const composer = page.locator(isMobile ? '.composer-sheet' : '.composer');
  await composer.locator('textarea').fill(text);
  await composer.locator('[data-attach-input]').setInputFiles(png);
  await expect(composer.locator('.file-chip[data-file] .file-name')).toHaveText('pixel.png');
  await expect(composer.locator('[data-upload]')).toHaveCount(0);
  await page.locator(isMobile ? '.sheet-head [data-post-reply]' : '.composer-foot [data-post-reply]').click();

  const post = page.locator('article.post', { hasText: text });
  await expect(post).toBeVisible();
  const card = post.locator('a.attachment');
  await expect(card).toHaveCount(1);
  await expect(card.locator('.attachment-name')).toHaveText('pixel.png');
  const thumb = card.locator('img');
  await expect.poll(() => thumb.evaluate((i: HTMLImageElement) => (i.complete ? i.naturalWidth : 0))).toBeGreaterThan(0);
  expect(await thumb.getAttribute('src')).toMatch(/^\/blobs\/[0-9a-f]{64}\/thumb$/);
  await scan(page);

  // The card opens the image itself.
  const href = (await card.getAttribute('href'))!;
  expect(href).toMatch(/^\/blobs\/[0-9a-f]{64}$/);
  const image = page.waitForResponse((r) => new URL(r.url()).pathname === href);
  await card.click();
  const res = await image;
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('image/png');
  await expect(page).toHaveURL(new RegExp(`${href}$`));
});

test('a member moves between pages with the page picker sheet and jumps to the first unread from it', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the page picker sheet is the phone’s (docs/06 "Pages")');
  // alice has read to #50, the foot of page 2, so moving to page 2 reads
  // nothing new and the first unread stays #51.
  await setReadPosition(page.request, seed.thread, 50);
  await page.goto(`${seed.thread}?page=1`);
  const label = page.locator('.pager-narrow .where').first();
  await expect(label).toHaveText(/^Page 1 of \d+$/);

  await label.click();
  const sheet = page.getByRole('dialog', { name: 'Pages' });
  await expect(sheet).toBeVisible();
  await scan(page);
  await sheet.getByRole('link', { name: 'Page 2', exact: true }).click();
  await expect(page).toHaveURL(/\?page=2$/);
  await expect(sheet).toHaveCount(0);
  await expect(label).toHaveText(/^Page 2 of \d+$/);
  await expect(page.locator('#post-26')).toBeVisible();

  await label.click();
  await expect(sheet).toBeVisible();
  await sheet.locator('[data-sheet-first-unread]').click();
  await expect(page).toHaveURL(/\/51$/);
  await expect(page.locator('#post-51')).toBeInViewport();
});

// Whether a browser installs the app is install.spec.ts's, in a profile on
// disk; this is the link and what it serves, which need no profile.
test('the web app manifest is linked from the shell and serves the name and icons', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#app')).not.toBeEmpty();
  const href = await page.locator('link[rel=manifest]').getAttribute('href');
  expect(href).toBe('/manifest.webmanifest');
  const res = await page.request.get(href!);
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('application/manifest+json');
  const data = (await res.json()) as { name: string; display: string; start_url: string; icons: { src: string; sizes: string }[] };
  const me = (await (await page.request.get('/api/v1/me')).json()) as { site_name: string };
  expect(data.name).toBe(me.site_name);
  expect(data.display).toBe('standalone');
  expect(data.start_url).toBe('/');
  expect(data.icons.map((i) => i.sizes)).toEqual(['192x192', '512x512', '512x512']);
  for (const icon of data.icons) {
    const r = await page.request.get(icon.src);
    expect(r.status(), icon.src).toBe(200);
    expect(r.headers()['content-type'], icon.src).toBe('image/png');
  }
  await scan(page);
});

test.describe('settings and profile', () => {
  // ada, whom no other spec reads a thread page as; her size is put back
  // whatever happens.
  test('a member changes their page size in settings and the thread pages by it', async ({ browser }, info) => {
    const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('ada') });
    const page = await ctx.newPage();
    const settings = async () =>
      ((await (await page.request.get('/api/v1/me')).json()) as { settings: { page_size: number } }).settings;
    const before = (await settings()).page_size;
    try {
      await page.goto('/settings');
      const size = page.getByLabel('Posts on a thread page');
      await expect(size).toHaveValue(String(before));
      const saved = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/settings' && r.request().method() === 'PATCH');
      await size.fill('10');
      await size.blur();
      expect((await saved).status()).toBe(200);
      await expect(page.getByRole('status')).toHaveText('Saved.');
      expect((await settings()).page_size).toBe(10);
      await scan(page);

      await page.goto(`${seed.thread}?page=1`);
      await expect(page.locator('article.post').first()).toBeVisible();
      await expect(page.locator('article.post')).toHaveCount(10);
      await page.goto(`${seed.thread}?page=2`);
      // goto returns at the load event, and the page draws its posts only
      // once the thread's answer arrives after it, so the first ordinal is
      // polled rather than read once (TKT-01M3ZGWX60, CI run 2558).
      await expect.poll(async () => (await ordinals(page))[0]).toBe(11);
    } finally {
      const r = await page.request.patch('/api/v1/settings', { data: { page_size: before } });
      expect(r.status(), await r.text()).toBe(200);
      await ctx.close();
    }
  });

  test('a member opens another member’s profile and sees their posts', async ({ page }) => {
    // #1 of the main thread is carol's.
    await page.goto(`${seed.thread}?page=1`);
    const author = page.locator('#post-1 .post-header a.author');
    await expect(author).toHaveText('Carol Ruiz');
    await author.click();
    await expect(page).toHaveURL('/u/carol');
    await expect(page.locator('main[data-screen=profile] h1')).toHaveText('Carol Ruiz');
    await expect(page.locator('[data-post-count]')).toHaveText(/^\d+$/);

    await page.locator('[data-profile-posts]').click();
    await expect(page).toHaveURL('/u/carol/posts');
    await expect(page.locator('main[data-screen=profile-posts] h1')).toHaveText('Posts by Carol Ruiz');
    const posts = page.locator('.history-post');
    await expect(posts.first()).toBeVisible();
    await scan(page);

    const ordinal = posts.first().locator('.ordinal');
    const href = (await ordinal.getAttribute('href'))!;
    await ordinal.click();
    await expect(page).toHaveURL(href);
    await expect(page.locator(`#post-${href.split('/').pop()} .post-header a.author`)).toHaveText('Carol Ruiz');
  });
});
