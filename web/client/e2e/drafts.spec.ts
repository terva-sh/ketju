import { expect, test, type APIRequestContext, type Browser } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// Reply drafts autosave (TKT-01M3NS1931), run once per project in
// playwright.config.ts: a reply and its reply target typed in one browser
// context are restored in a second one signed in as the same member, in the
// composer at 1440 px and in the reply bar at 390 px, and posting from there
// deletes the draft.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));
const thread = /^\/t\/([^/?#]+)/.exec(seed.thread)![1];
const draftUrl = `/api/v1/drafts/${thread}`;
const at = `${seed.thread}?page=1`;

// smoke.spec.ts posts from the same composer as alice, so the draft is
// deleted before and after rather than trusted to be absent.
async function deleteDraft(request: APIRequestContext) {
  const r = await request.delete(draftUrl);
  expect(r.status(), await r.text()).toBe(204);
}

async function asAlice(browser: Browser, isMobile: boolean, baseURL: string | undefined) {
  return browser.newContext({
    baseURL,
    viewport: isMobile ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    isMobile,
    hasTouch: isMobile,
    ignoreHTTPSErrors: true,
    // Signed in once by the setup project, since /auth/* allows only a
    // burst of ten (docs/08).
    storageState: stateFile('alice'),
  });
}

test('a reply and its target follow alice to another browser, and posting it deletes the draft', async ({ browser, isMobile, baseURL }, info) => {
  const first = await asAlice(browser, isMobile, baseURL);
  const second = await asAlice(browser, isMobile, baseURL);
  try {
    await deleteDraft(first.request);
    const text = `A draft from the ${info.project.name} run, ${Date.now()}.`;

    // Post 10, which no other spec's expander rule names (replies.spec.ts).
    await test.step('alice writes a reply to post 10 and it is saved', async () => {
      const page = await first.newPage();
      await page.goto(at);
      await page.locator('#post-10 [data-post-action=reply]').click();
      if (isMobile) {
        await page.locator('.composer-sheet textarea').fill(text);
      } else {
        await page.locator('.composer textarea').fill(text);
        await expect(page.locator('.composer-foot [role=status]')).toHaveText('Draft saved');
      }
      await expect
        .poll(async () => {
          const r = await first.request.get(draftUrl);
          return r.status() === 200 ? await r.json() : null;
        })
        .toMatchObject({ source: text, reply_to: 10 });
    });

    await test.step('another browser finds it, and posts it', async () => {
      const page = await second.newPage();
      await page.goto(at);
      if (isMobile) {
        const bar = page.locator('[data-shell=replybar]');
        await expect(bar.locator('.replybar-text')).toHaveText(text);
        await expect(bar.locator('.reply-chip')).toHaveAttribute('data-reply-chip', '10');
        await bar.locator('.replybar-open').click();
        await expect(page.locator('.composer-sheet textarea')).toHaveValue(text);
        await page.locator('.sheet-head [data-post-reply]').click();
        await expect(page.locator('.composer-sheet')).toHaveCount(0);
      } else {
        await expect(page.locator('.composer textarea')).toHaveValue(text);
        await expect(page.locator('.composer .reply-chip')).toHaveAttribute('data-reply-chip', '10');
        await page.locator('.composer-foot [data-post-reply]').click();
      }
      await expect(page).toHaveURL(/\/\d+$/);
      await expect(page.locator('article.post', { hasText: text })).toBeVisible();
      await expect.poll(async () => (await second.request.get(draftUrl)).status()).toBe(204);

      // Nothing comes back on a reload, from the server or from this
      // browser's own copy.
      const loaded = page.waitForResponse((r) => new URL(r.url()).pathname === draftUrl && r.request().method() === 'GET');
      await page.goto(at);
      expect((await loaded).status()).toBe(204);
      await expect(page.locator('article.post').first()).toBeVisible();
      if (isMobile) await expect(page.locator('[data-shell=replybar] .replybar-text')).toHaveText('Write a reply…');
      else await expect(page.locator('.composer textarea')).toHaveValue('');
      expect((await second.request.get(draftUrl)).status()).toBe(204);
    });
  } finally {
    await deleteDraft(first.request);
    await first.close();
    await second.close();
  }
});
