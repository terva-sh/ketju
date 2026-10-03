import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// A quoted post's backlinks, "Quoted by #2 #44" (docs/06 "Post anatomy",
// TKT-01M3YTHAZB), run once per project in playwright.config.ts, at 390 px
// and 1440 px, against the e2e seed: #2 on page 1 and #44 on page 2 quote #1
// (cmd/ketju e2e.go seedQuotesFirst). Read anonymously, so no reader's
// position moves.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

async function scan(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, found.join('\n')).toEqual([]);
}

test('#1 shows who quoted it, and a backlink off the page opens the popover', async ({ page }) => {
  await page.goto(`${seed.thread}?page=1`);
  const line = page.locator('#post-1 [data-quoted-by]');
  await expect(line).toHaveText('Quoted by #2 #44');
  // Between the body and the footer.
  const box = (sel: string) => page.locator(sel).boundingBox().then((b) => b!);
  const [body, by, footer] = await Promise.all([box('#post-1 .post-body'), box('#post-1 [data-quoted-by]'), box('#post-1 .post-footer')]);
  expect(by.y).toBeGreaterThanOrEqual(body.y + body.height);
  expect(footer.y).toBeGreaterThanOrEqual(by.y + by.height);
  // A post nothing quotes has no line.
  await expect(page.locator('#post-3 [data-quoted-by]')).toHaveCount(0);

  // Each backlink is what a press at its centre reaches: on the phone their
  // 44 px targets (.hit) must not cover each other or the footer's.
  for (const a of await line.locator('a').all()) {
    const b = (await a.boundingBox())!;
    const hit = await page.evaluate(
      ([x, y]) => document.elementFromPoint(x, y)?.closest('a')?.getAttribute('href') ?? null,
      [b.x + b.width / 2, b.y + b.height / 2],
    );
    expect(hit).toBe(await a.getAttribute('href'));
  }

  // #44 is on page 2: its backlink opens it in the popover, and the address
  // stays.
  const before = page.url();
  const off = line.locator('a[data-reply-to="44"]');
  await off.click();
  const dialog = page.locator('.post-popover[role=dialog]');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Seeded post 44.');
  expect(page.url()).toBe(before);
  await scan(page);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(off).toBeFocused();

  // #2 is on this page: its backlink scrolls to it, with no popover.
  await line.locator('a[data-reply-to="2"]').click();
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(dialog).toHaveCount(0);
  expect(page.url()).toBe(before);
});
