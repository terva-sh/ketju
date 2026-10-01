import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { stateFile } from './session';

// Attachments (TKT-01M3V73J8K), run once per project in playwright.config.ts:
// alice attaches an image and a text file to a reply from the composer,
// carol sees both cards, and a members-only file is not served to a reader
// who is not signed in. The seed grants members upload everywhere
// (cmd/ketju e2e.go), since no migration does. Every test starts from a
// stored session (e2e/session.ts) and spends no /auth/* request.
//
// The replies go in the seed's short side threads, not the main one, whose
// post count other specs read.

test.describe.configure({ mode: 'serial' });

const fixtures = join(dirname(fileURLToPath(import.meta.url)), 'fixtures');
const png = join(fixtures, 'pixel.png');
const notes = join(fixtures, 'notes.txt');

// threadIn is the newest thread in forum, as its list answers it.
async function threadIn(request: APIRequestContext, forum: string): Promise<{ id: string; path: string }> {
  const r = await request.get(`/api/v1/forums/${forum}/threads`);
  expect(r.status(), await r.text()).toBe(200);
  const t = ((await r.json()) as { threads: { id: string; slug: string }[] }).threads[0];
  return { id: t.id, path: `/t/${t.id}/${t.slug}` };
}

const scan = async (page: Page) => {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
};

// text is what alice's reply says in this project's run, which carol's
// test looks for.
let text = '';

test.describe('as alice', () => {
  test.use({ storageState: stateFile('alice') });

  test('a member attaches an image and a file to a reply', async ({ page, isMobile }, info) => {
    const thread = await threadIn(page.request, 'introductions');
    text = `Files from the ${info.project.name} run, ${Date.now()}.`;
    await page.goto(`${thread.path}?page=1`);
    await expect(page.locator('article.post').first()).toBeVisible();

    // The phone's composer is the sheet the reply bar opens.
    if (isMobile) await page.locator('[data-shell=replybar] .replybar-open').click();
    const composer = page.locator(isMobile ? '.composer-sheet' : '.composer');
    await composer.locator('textarea').fill(text);
    await expect(composer.locator('[data-attach]')).toBeEnabled();
    await composer.locator('[data-attach-input]').setInputFiles([png, notes]);
    const chips = composer.locator('.file-chip[data-file]');
    await expect(chips).toHaveCount(2);
    await expect(chips.locator('.file-name')).toHaveText(['pixel.png', 'notes.txt']);
    await expect(composer.locator('[data-upload]')).toHaveCount(0);
    // The composer with its chips passes the scan.
    await scan(page);

    await page.locator(isMobile ? '.sheet-head [data-post-reply]' : '.composer-foot [data-post-reply]').click();
    await expect(page).toHaveURL(/\/\d+$/);
    const post = page.locator('article.post', { hasText: text });
    await expect(post).toBeVisible();
    const cards = post.locator('a.attachment');
    await expect(cards).toHaveCount(2);
    await expect(cards.locator('.attachment-name')).toHaveText(['pixel.png', 'notes.txt']);
    // The image has a thumbnail that loaded; the text file downloads under
    // its name.
    const thumb = cards.first().locator('img');
    await expect.poll(() => thumb.evaluate((i: HTMLImageElement) => (i.complete ? i.naturalWidth : 0))).toBeGreaterThan(0);
    await expect(cards.nth(1)).toHaveAttribute('download', 'notes.txt');
    await expect(cards.nth(1).locator('img')).toHaveCount(0);
    // A desktop shows the image's dimensions, a phone the size only.
    await expect(cards.first().locator('.attachment-meta')).toHaveText(isMobile ? /^\d+ B$/ : /^\d+ B · 24×16$/);
    // Posted, the composer's files are gone with its text.
    await expect(page.locator('.composer .file-chip')).toHaveCount(0);
  });
});

test.describe('as carol', () => {
  test.use({ storageState: stateFile('carol') });

  test('another member sees the cards', async ({ page }) => {
    expect(text, 'alice posted first').not.toBe('');
    const thread = await threadIn(page.request, 'introductions');
    const r = await page.request.get(`/api/v1/threads/${thread.id}?page=1`);
    const pages = ((await r.json()) as { page: { total_pages: number } }).page.total_pages;
    await page.goto(`${thread.path}?page=${pages}`);
    const post = page.locator('article.post', { hasText: text });
    await expect(post).toBeVisible();
    const cards = post.locator('a.attachment');
    await expect(cards.locator('.attachment-name')).toHaveText(['pixel.png', 'notes.txt']);
    const href = await cards.nth(1).getAttribute('href');
    const file = await page.request.get(href!);
    expect(file.status()).toBe(200);
    expect(await file.text()).toBe(readFileSync(notes, 'utf8'));
    await scan(page);
  });
});

test.describe('as alice, in a members-only forum', () => {
  test.use({ storageState: stateFile('alice') });

  test('a stranger cannot fetch the blob', async ({ page, playwright, baseURL }, info) => {
    const thread = await threadIn(page.request, 'garden');
    // Bytes of this test's own, so no public post names the same blob: the
    // fixture with a tail after its IEND, which a PNG decoder ignores.
    const buffer = Buffer.concat([readFileSync(png), Buffer.from(`${info.project.name} ${Date.now()}`)]);
    const up = await page.request.post(`/api/v1/blobs?thread=${thread.id}`, {
      multipart: { file: { name: 'far-bed.png', mimeType: 'image/png', buffer } },
    });
    expect(up.status(), await up.text()).toBe(201);
    const { hash } = (await up.json()) as { hash: string };
    // Not served before a post names it, even to the uploader.
    expect((await page.request.get(`/blobs/${hash}`)).status()).toBe(404);
    const posted = await page.request.post(`/api/v1/threads/${thread.id}/posts`, {
      data: { source: 'The far bed, photographed.', attachments: [hash] },
    });
    expect(posted.status(), await posted.text()).toBe(201);
    expect((await page.request.get(`/blobs/${hash}`)).status()).toBe(200);
    expect((await page.request.get(`/blobs/${hash}/thumb`)).status()).toBe(200);

    // An empty storage state, since a new context otherwise takes alice's
    // from test.use.
    const stranger = await playwright.request.newContext({ baseURL, ignoreHTTPSErrors: true, storageState: { cookies: [], origins: [] } });
    try {
      const me = (await (await stranger.get('/api/v1/me')).json()) as { user: unknown };
      expect(me.user, 'the stranger is signed out').toBeNull();
      expect((await stranger.get(`/api/v1/threads/${thread.id}?page=1`)).status()).toBe(401);
      expect((await stranger.get(`/blobs/${hash}`)).status()).toBe(404);
      expect((await stranger.get(`/blobs/${hash}/thumb`)).status()).toBe(404);
    } finally {
      await stranger.dispose();
    }
  });
});
