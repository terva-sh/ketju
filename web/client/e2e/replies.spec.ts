import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// The inline reply expander (TKT-01M29CGBW6), run once per project in
// playwright.config.ts against the e2e seed (cmd/ketju e2e.go seedReplies):
// on page 1, #1 has replies on pages 2 and 3, #5 two on its own page, #3
// one directly below, and #25 one that opens page 2.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

// tops is the viewport offset of every canonical post, by id, in page order.
const tops = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.posts > [id^=post-]')].map((e) => [e.id, Math.round(e.getBoundingClientRect().top)] as const),
  );
const topOf = async (page: Page, sel: string) => Math.round((await page.locator(sel).boundingBox())!.y);

test('the expander is offered where the rule says', async ({ page }) => {
  await page.goto(`${seed.thread}?page=1`);
  await expect(page.locator('#post-1')).toBeVisible();
  await expect(page.locator('#post-1 button.reply-expander')).toHaveText('2 replies');
  await expect(page.locator('#post-5 button.reply-expander')).toHaveText('2 replies');
  // #4 is #3's only reply, directly below.
  await expect(page.locator('#post-3 button.reply-expander')).toHaveCount(0);
  // #26 is #25's only reply, and it opens page 2.
  await expect(page.locator('#post-25 button.reply-expander')).toHaveText('1 reply');
  await expect(page.locator('#post-2 button.reply-expander')).toHaveCount(0);
});

// AC1 and AC4: a reply that lives on another page opens in place, and the
// canonical page holds still.
for (const ordinal of [1, 5]) {
  test(`expanding and collapsing #${ordinal} moves no canonical post`, async ({ page }) => {
    await page.goto(`${seed.thread}?page=1`);
    const toggle = page.locator(`#post-${ordinal} button.reply-expander`);
    await expect(toggle).toBeVisible();
    // Scrolled so the toggle is mid-screen, with posts above and below it.
    await toggle.evaluate((e) => e.scrollIntoView({ block: 'center' }));
    const before = await tops(page);
    const at = await topOf(page, `#post-${ordinal} button.reply-expander`);
    const index = before.findIndex(([id]) => id === `post-${ordinal}`);

    await toggle.click();
    const region = page.locator(`#replies-${ordinal}`);
    await expect(region.locator('.post--recessed').first()).toBeVisible();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    if (ordinal === 1) {
      // Both of #1's replies are on other pages (2 and 3).
      await expect(region.locator('.post--recessed')).toHaveCount(2);
      expect(await region.locator('.post--recessed').evaluateAll((es) => es.map((e) => e.getAttribute('data-reply')))).toEqual(['30', '55']);
      await expect(region).toContainText('Seeded post 30.');
      await expect(region).toContainText('Seeded post 55.');
      await expect(page.locator('#post-30, #post-55')).toHaveCount(0);
      // A copy keeps its own one-line header at both widths: the phone's
      // stacked header is the canonical post's alone (TKT-01M3S3Z9E1,
      // Terva review 1797).
      const copy = region.locator('[data-reply="55"] .post-header');
      const o = (await copy.locator('.ordinal').boundingBox())!;
      const a = (await copy.locator('.author').boundingBox())!;
      expect(o.y < a.y + a.height && a.y < o.y + o.height, 'ordinal and author share a line').toBe(true);
      expect(o.x).toBeLessThan(a.x);
    } else {
      expect(await region.locator('.post--recessed').evaluateAll((es) => es.map((e) => e.getAttribute('data-reply')))).toEqual(['6', '8']);
    }
    // Read-only: nothing in the expansion but links.
    await expect(region.locator('button')).toHaveCount(0);

    const open = await tops(page);
    expect(open.map(([id]) => id), 'the canonical order').toEqual(before.map(([id]) => id));
    expect(await topOf(page, `#post-${ordinal} button.reply-expander`), 'the toggle').toBe(at);
    expect(open.slice(0, index + 1), 'the post and those above it').toEqual(before.slice(0, index + 1));

    await toggle.click();
    await expect(region).toHaveCount(0);
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(toggle).toBeFocused();
    expect(await topOf(page, `#post-${ordinal} button.reply-expander`), 'the toggle, closed').toBe(at);
    expect(await tops(page), 'every post, closed').toEqual(before);
  });
}

test('a reply in the expansion goes to its own page', async ({ page }) => {
  await page.goto(`${seed.thread}?page=1`);
  await page.locator('#post-1 button.reply-expander').click();
  await page.locator('#replies-1 [data-reply="55"] a.ordinal').click();
  await expect(page.locator('#post-55')).toBeInViewport();
  await expect(page.locator('#post-1')).toHaveCount(0);
});
