import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// The applications review screen (TKT-01M3NRX2NN), at 390 px and 1440 px:
// the seeded moderator, mona, opens the list, opens a seeded pending
// application, reads its answers, and approves it with the suggested
// handle. The list, the detail and the decided application are each checked
// for horizontal overflow and scanned with axe (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts does) in the light theme and the dark.
//
// Each project approves its own application (cmd/ketju e2e.go seedPending),
// since an approval cannot be taken twice. The desktop project approves
// from the keyboard alone.

const seed: { applications: Record<string, number> } = JSON.parse(
  readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'),
);
const applicants: Record<string, { handle: string; name: string }> = {
  phone: { handle: 'pat', name: 'Pat Lee' },
  desktop: { handle: 'quinn', name: 'Quinn Okafor' },
};

test.use({ storageState: stateFile('mona') });

async function scan(page: Page, what: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(found, `${what}, ${colorScheme}`).toEqual([]);
  }
  await page.emulateMedia({ colorScheme: 'light' });
}

test('a moderator approves a pending application', async ({ page }, info) => {
  const id = seed.applications[info.project.name];
  const who = applicants[info.project.name];
  expect(id, `a seeded application for the ${info.project.name} project`).toBeGreaterThan(0);

  await test.step('the pending list', async () => {
    await page.goto('/admin/applications');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Applications');
    await expect(page.locator('[data-no-email]')).toContainText('notifications are in-app only');
    const link = page.locator(`[data-application-link="${id}"]`);
    await expect(link).toBeVisible();
    await expect(page.locator('tbody tr', { has: link })).toContainText(`${who.handle}@example.test`);
    await scan(page, 'the list');
  });

  await test.step('open the application and read its answers', async () => {
    await page.locator(`[data-application-link="${id}"]`).click();
    await page.waitForURL((u) => u.search === `?id=${id}`);
    await expect(page.getByRole('heading', { level: 2 })).toContainText(`Application #${id}`);
    await expect(page.locator('.form-read [data-field="name"] dd')).toHaveText(who.name);
    await expect(page.locator('.form-read [data-field="rules"] dd')).toHaveText('Yes');
    await expect(page.getByLabel('Handle')).toHaveValue(who.handle);
    await expect(page.getByLabel('Note (required)')).toHaveAccessibleDescription(
      /Moderators only\. The applicant never sees this note\./,
    );
    await scan(page, 'the detail');
  });

  await test.step('approve with the suggested handle', async () => {
    const approve = page.locator('[data-decision] section').filter({ has: page.getByRole('heading', { name: 'Approve' }) });
    if (info.project.name === 'desktop') {
      // From the keyboard: the handle, its note, the Approve button; then
      // the confirmation, which opens on Cancel, and its Approve before it.
      await page.getByLabel('Handle').focus();
      await page.keyboard.press('Tab');
      await expect(page.getByLabel('Note (optional)')).toBeFocused();
      await page.keyboard.press('Tab');
      await expect(approve.getByRole('button', { name: 'Approve' })).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(approve.getByRole('button', { name: 'Cancel' })).toBeFocused();
      await page.keyboard.press('Shift+Tab');
      await expect(approve.getByRole('button', { name: 'Approve' })).toBeFocused();
      await page.keyboard.press('Enter');
    } else {
      await approve.getByRole('button', { name: 'Approve' }).click();
      await approve.getByRole('button', { name: 'Approve' }).click();
    }
    const notice = page.locator('[data-notice]');
    await expect(notice).toHaveText(`Approved as @${who.handle}. They can sign in now.`);
    await expect(notice).toBeFocused();
    await expect(page.locator('[data-decided]')).toContainText('Approved by @mona');
    await expect(page.locator('[data-decision]')).toHaveCount(0);
    await scan(page, 'the approved application');
  });
});
