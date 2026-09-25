import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// The smoke suite (TKT-01M29CGBY1), run once per project in
// playwright.config.ts: sign in, read, reply, and the unread divider, end
// to end against the e2e build of ketju serve.

interface Seed {
  thread: string;
  title: string;
  posts: number;
}
const seed: Seed = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

// signIn goes through the real flow: /auth/fake/start, the fake provider's
// sign-in page, and the callback that sets the session. The client has no
// sign-in screen yet, so the flow starts at the server's own start URL.
async function signIn(page: Page, who: string) {
  await page.goto('/auth/fake/start');
  await page.getByRole('link', { name: who, exact: true }).click();
  await page.waitForURL((u) => u.pathname === '/');
  await expect
    .poll(async () => ((await (await page.request.get('/api/v1/me')).json()) as { user?: { handle: string } }).user?.handle)
    .toBe(who);
}

test('sign in, read a thread, reply, and see the unread divider', async ({ browser, page, isMobile, baseURL }, info) => {
  await test.step('sign in', () => signIn(page, 'alice'));

  await test.step('read the thread', async () => {
    await page.goto(seed.thread);
    await expect(page.getByRole('heading', { level: 1, name: seed.title })).toBeVisible();
    await expect(page.locator('article.post').first()).toBeVisible();
    await expect(page.locator('article.post')).not.toHaveCount(0);
  });

  const text = `A reply from the ${info.project.name} run, ${Date.now()}.`;
  await test.step('reply', async () => {
    if (isMobile) {
      // The phone's reply bar opens the composer sheet (TKT-01M29CGBVN).
      await expect(page.locator('[data-shell=tabbar]')).toHaveCount(0);
      await page.locator('.replybar-open').click();
      await page.locator('.composer-sheet textarea').fill(text);
      await page.locator('.sheet-head [data-post-reply]').click();
      await expect(page.locator('.composer-sheet')).toHaveCount(0);
    } else {
      await page.locator('.composer textarea').fill(text);
      await page.locator('.composer-foot [data-post-reply]').click();
    }
    await expect(page).toHaveURL(/\/\d+$/);
    await expect(page.locator('article.post', { hasText: text })).toBeVisible();
  });

  await test.step('the unread divider, for a reader who had read the seed', async () => {
    const ctx = await browser.newContext({
      baseURL,
      viewport: page.viewportSize(),
      isMobile,
      hasTouch: isMobile,
      ignoreHTTPSErrors: true,
    });
    const carol = await ctx.newPage();
    await signIn(carol, 'carol');
    await carol.goto(seed.thread);
    const divider = carol.locator('#unread');
    await expect(divider).toBeVisible();
    await expect(divider).toHaveAccessibleName(/^\d+ new posts? below$/);
    await expect(divider).toContainText(`last read #${seed.posts}`);
    // Directly before the first post carol has not read, with alice's reply
    // below it.
    expect(await divider.evaluate((d) => d.nextElementSibling?.id)).toBe(`post-${seed.posts + 1}`);
    await expect(carol.locator('#unread ~ article.post', { hasText: text })).toBeVisible();
    await ctx.close();
  });
});
