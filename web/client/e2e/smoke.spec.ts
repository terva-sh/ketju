import { expect, test } from '@playwright/test';
import { setReadPosition, signIn, stateFile } from './session';
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
      // Signed in once by the setup project: the sign-in flow is alice's
      // step above, and /auth/* allows only a burst of ten (docs/08).
      storageState: stateFile('carol'),
    });
    // carol has read the seed, as seeded. Opening the thread moves her
    // position (TKT-01M29CGBWN), so it is put back first rather than
    // trusted to be where the other width's run left it.
    await setReadPosition(ctx.request, seed.thread, seed.posts);
    const carol = await ctx.newPage();
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
