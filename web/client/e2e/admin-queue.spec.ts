import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import { stateFile } from './session';

// The moderation queue (TKT-01M29CRJ4E), at 390 px and 1440 px: the seeded
// moderator, mona, follows the top bar's queue button to the queue, which
// fits the width and passes axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts
// does) in the light theme and the dark. What it lists is not checked here:
// the seed's pending applications are approved by admin-applications.spec.ts,
// which may run first, and the screen's items are covered by
// QueueScreen.test.tsx against the server's goldens.

test.use({ storageState: stateFile('mona') });

test('a moderator opens the queue from the top bar', async ({ page }) => {
  await page.goto('/');
  const button = page.locator('a[data-nav="queue"]');
  await expect(button).toBeVisible();
  await button.click();
  await page.waitForURL((u) => u.pathname === '/admin/queue');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Moderation queue');
  await expect(page.locator('.admin-queue')).toBeVisible();
  await expect(page.locator('.admin-queue [aria-live]').filter({ hasText: 'Loading' })).toHaveCount(0);

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(found, colorScheme).toEqual([]);
  }
});
