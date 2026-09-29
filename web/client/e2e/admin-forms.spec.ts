import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { stateFile } from './session';

// The form editor (TKT-01M29CP2P1), at 390 px and 1440 px: the seeded
// admin, ada, adds a field to the registration form, sees a key the server
// refuses marked on the field with Save disabled, corrects it, saves a new
// version, and opens version 1 read-only. The editor, the refused draft,
// the saved form and the read-only version are each checked for horizontal
// overflow and scanned with axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts
// does) in the light theme and the dark.
//
// Both projects run against one server, and apply.spec.ts fills in the
// form after them, so each run ends by saving the fields it started with
// back as the newest version. The version numbers are read from the screen,
// not assumed.

test.use({ storageState: stateFile('ada') });

const formUrl = '/api/v1/admin/forms/registration';

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

test('an admin edits the registration form and saves a new version', async ({ page }) => {
  const before = await page.request.get(formUrl);
  expect(before.status()).toBe(200);
  const start = (await before.json()) as { active_version: number; schema: { fields: unknown[] } };
  const n = start.active_version;
  const save = page.getByRole('button', { name: /^Save as version/ });

  await test.step('the editor', async () => {
    await page.goto('/admin/forms');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Application forms');
    await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Apply to join');
    await expect(page.locator('[data-preview] [data-field="who"]')).toContainText('Who invited you');
    await expect(save).toHaveText(`Save as version ${n + 1}`);
    await expect(save).toBeDisabled();
    await scan(page, 'the editor');
  });

  const added = page.locator('.fe-field').last();
  await test.step('a key the server refuses is marked on its field', async () => {
    await page.getByLabel('Type of the new field').selectOption({ label: 'Short text' });
    await page.getByRole('button', { name: 'Add field' }).click();
    await expect(added.locator('legend')).toHaveText(`Field ${start.schema.fields.length + 1}: Short text`);
    await expect(added.getByLabel('Label', { exact: true })).toBeFocused();
    await added.getByLabel('Label', { exact: true }).fill('Pronouns');
    await added.getByLabel('Key', { exact: true }).fill('Pronouns');
    const key = added.getByLabel('Key', { exact: true });
    await expect(key).toHaveAttribute('aria-invalid', 'true');
    await expect(key).toHaveAccessibleDescription(/A key starts with a lowercase letter/);
    await expect(page.locator('[data-lead]')).toContainText('The problems are marked on the fields below.');
    await expect(save).toBeDisabled();
    await scan(page, 'the refused draft');
  });

  await test.step('corrected, previewed and saved', async () => {
    await added.getByLabel('Key', { exact: true }).fill('pronouns');
    await expect(page.locator('[data-preview] [data-field="pronouns"]')).toContainText('Pronouns');
    await expect(page.locator('[data-lead]')).toHaveCount(0);
    await expect(save).toBeEnabled();
    await save.click();
    const notice = page.locator('[data-notice]');
    await expect(notice).toHaveText(`Saved. Version ${n + 1} is now the form applicants see.`);
    await expect(notice).toBeFocused();
    const first = page.locator('.fe-versions tbody tr').first();
    await expect(first.locator(`[data-version="${n + 1}"] [data-active]`)).toHaveText('Active');
    await expect(first).toContainText('@ada');
    await expect(page.locator('.fe-versions tbody tr')).toHaveCount(n + 1);
    await scan(page, 'the saved form');
  });

  await test.step('version 1 opens read-only', async () => {
    await page.getByRole('button', { name: 'View version 1', exact: true }).click();
    const view = page.locator('[data-viewing="1"]');
    await expect(view.getByRole('heading', { name: 'Version 1, read-only' })).toBeFocused();
    await expect(view.locator('[data-field="who"]')).toContainText('Who invited you');
    await expect(view.locator('[data-field="pronouns"]')).toHaveCount(0);
    await expect(view.getByLabel('Name (required)')).toBeDisabled();
    await scan(page, 'version 1, read-only');
    await view.getByRole('button', { name: 'Close version 1' }).click();
  });

  await test.step('the fields it started with are saved back', async () => {
    await added.getByRole('button', { name: 'Remove' }).click();
    await expect(page.locator('[data-preview] [data-field="pronouns"]')).toHaveCount(0);
    await expect(save).toBeEnabled();
    await save.click();
    await expect(page.locator('[data-notice]')).toHaveText(`Saved. Version ${n + 2} is now the form applicants see.`);
    const after = (await (await page.request.get(formUrl)).json()) as { active_version: number; schema: unknown };
    expect(after.active_version).toBe(n + 2);
    expect(after.schema).toEqual(start.schema);
  });
});
