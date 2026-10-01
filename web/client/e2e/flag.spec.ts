import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// Flag a post (TKT-01M3QQWA5Z), at 390 px and 1440 px, against the real
// server: alice flags carol's #3 on page 1 of the seeded thread (cmd/ketju
// e2e.go: odd posts are carol's) and sees the thanks and nothing else. The
// project that runs second sends a repeat, which the server answers with the
// same receipt. One flag is under the auto-hide threshold, so nothing on the
// page changes for the other specs.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

test.use({ storageState: stateFile('alice') });

test('a member flags another member’s post and is thanked', async ({ page, isMobile }) => {
  await page.goto(`${seed.thread}?page=1`);
  const post = page.locator('#post-3');
  await expect(post).toBeVisible();
  // Not on alice's own post.
  if (isMobile) {
    await page.locator('#post-2 [data-post-more]').click();
    await expect(page.locator('#post-2 .post-more-list')).toBeVisible();
    await expect(page.locator('#post-2 [data-post-action=flag]')).toHaveCount(0);
    await page.keyboard.press('Escape');
  } else {
    await expect(page.locator('#post-2 [data-post-action=flag]')).toHaveCount(0);
  }

  const opener = isMobile ? post.locator('[data-post-more]') : post.locator('[data-post-action=flag]');
  const dialog = page.getByRole('dialog', { name: 'Flag post #3' });
  // Escape pressed straight after opening, with nothing between, closes it
  // (TKT-01M3TS0ZV9).
  if (isMobile) await opener.click();
  await post.locator('[data-post-action=flag]').click();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();

  if (isMobile) await opener.click();
  await post.locator('[data-post-action=flag]').click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('radio', { name: 'Off-topic' }).check();
  const answer = page.waitForResponse((r) => r.url().endsWith('/flags') && r.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Send' }).click();
  expect((await answer).status()).toBe(200);
  await expect(dialog).toContainText('Thanks, a moderator will look.');
  await expect(dialog.getByRole('radio')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();
  await expect(post).toContainText('Seeded post 3.');
});
