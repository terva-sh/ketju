import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { stateFile } from './session';

// The suspension controls on the users screen (TKT-01M3QMRE47), at 390 px
// and 1440 px, against the real server: the seeded admin, ada, suspends an
// account for 7 days with a reason, sees the status line and the list row
// say so, and lifts it. Each state is checked for horizontal overflow and
// scanned with axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts does) in the
// light theme and the dark.
//
// Both projects run against one server, and the other specs sign in as the
// seeded readers, so each project suspends an agent of its own, made here
// without a token, rather than a reader another spec needs.

test.use({ storageState: stateFile('ada') });

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

test('an admin suspends an account and lifts the suspension', async ({ page }, info) => {
  const handle = `held-${info.project.name}-${Date.now().toString(36)}`;
  const detail = page.locator(`[data-detail="${handle}"]`);
  const panel = detail.locator('[data-suspension-panel]');
  const reason = 'Posting the same link in every thread. <b>Not bold</b>';

  await test.step('an account to suspend', async () => {
    await page.goto('/admin/users');
    await page.getByRole('button', { name: 'New agent' }).click();
    await page.getByLabel('Handle', { exact: true }).fill(handle);
    await page.getByLabel('Display name').fill('E2E held');
    await page.getByLabel('Issue a token now').uncheck();
    await page.getByRole('button', { name: 'Create agent' }).click();
    await expect(panel.getByRole('button', { name: 'Suspend', exact: true })).toBeVisible();
    await expect(panel.locator('[data-suspension]')).toHaveCount(0);
  });

  await test.step('the suspend form refuses an empty reason beside the field', async () => {
    await panel.getByRole('button', { name: 'Suspend', exact: true }).click();
    await expect(panel.getByLabel('Reason')).toBeFocused();
    await panel.getByLabel('7 days', { exact: true }).check();
    await panel.getByRole('button', { name: 'Suspend', exact: true }).click();
    await expect(panel.getByLabel('Reason')).toHaveAttribute('aria-invalid', 'true');
    await expect(panel).toContainText('Give a reason. The member sees it.');
    await expect(panel.getByRole('button', { name: 'Confirm' })).toHaveCount(0);
    await scan(page, 'the suspend form');
  });

  await test.step('suspended for 7 days, after the confirmation', async () => {
    await panel.getByLabel('Reason').fill(reason);
    await panel.getByRole('button', { name: 'Suspend', exact: true }).click();
    await expect(panel).toContainText(`Suspend @${handle} for 7 days? Its token is revoked`);
    await scan(page, 'the confirmation');
    await panel.getByRole('button', { name: 'Confirm' }).click();
    await expect(panel.locator('[data-suspension-message]')).toHaveText(`@${handle} is suspended.`);
    const line = panel.locator('[data-suspension]');
    await expect(line).toContainText(/Suspended until \d{4}-\d\d-\d\d \d\d:\d\d/);
    await expect(line).toContainText(`Reason: ${reason}`);
    await expect(line.locator('b')).toHaveCount(0);
    await expect(detail.locator('dd[data-status]')).toHaveText('suspended');
    await expect(page.locator(`[data-user="${handle}"]`).locator('xpath=ancestor::tr')).toContainText('suspended');
    await expect(panel.getByRole('button', { name: 'Hide their posts' })).toBeVisible();
    await scan(page, 'a suspended account');
  });

  await test.step('lifted', async () => {
    await panel.getByRole('button', { name: 'Unsuspend', exact: true }).click();
    await panel.getByRole('button', { name: 'Confirm' }).click();
    await expect(panel.locator('[data-suspension-message]')).toHaveText(`@${handle} is no longer suspended.`);
    await expect(panel.locator('[data-suspension]')).toHaveCount(0);
    await expect(detail.locator('dd[data-status]')).toHaveText('active');
    await expect(panel.getByRole('button', { name: 'Suspend', exact: true })).toBeVisible();
  });
});
