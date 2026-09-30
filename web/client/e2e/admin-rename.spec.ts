import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { stateFile } from './session';

// The rename control on the users screen (TKT-01M3P0CYR3), at 390 px and
// 1440 px, against the real server: the seeded admin, ada, renames an
// account, the address and the detail follow the new handle, and the old
// one redirects. A handle someone holds is refused on the field.
//
// Both projects run against one server, and the other specs sign in as the
// seeded readers, so each test renames an agent of its own, made here
// without a token. Each test runs one axe scan (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts does), in the theme it names (TKT-01M3MTQG5A).

test.use({ storageState: stateFile('ada') });

async function scan(page: Page, what: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, what).toEqual([]);
}

// agent creates an agent without a token, whose detail opens under
// /admin/users?user={handle}.
async function agent(page: Page, handle: string) {
  await page.goto('/admin/users');
  await page.getByRole('button', { name: 'New agent' }).click();
  await page.getByLabel('Handle', { exact: true }).fill(handle);
  await page.getByLabel('Display name').fill('E2E renamed');
  await page.getByLabel('Issue a token now').uncheck();
  await page.getByRole('button', { name: 'Create agent' }).click();
  await expect(page.locator(`[data-detail="${handle}"] [data-rename]`)).toBeVisible();
  await expect(page).toHaveURL(`/admin/users?user=${handle}`);
}

const stamp = (project: string) => `${project}-${Date.now().toString(36)}`;

test('an admin renames an account, and the address and the old handle follow', async ({ page }, info) => {
  await page.emulateMedia({ colorScheme: 'light' });
  const from = `was-${stamp(info.project.name)}`;
  const to = `now-${stamp(info.project.name)}`;
  await agent(page, from);

  const rename = page.locator(`[data-detail="${from}"] [data-rename]`);
  await rename.getByLabel('Change handle').fill(to);
  await rename.getByRole('button', { name: 'Rename' }).click();
  await expect(rename).toContainText(`Rename @${from} to @${to}?`);
  await rename.getByRole('button', { name: 'Confirm' }).click();

  const detail = page.locator(`[data-detail="${to}"]`);
  await expect(detail.locator('[data-renamed]')).toHaveText(
    `Renamed from @${from}. The old handle is kept for 90 days and sends visitors here.`,
  );
  await expect(page).toHaveURL(`/admin/users?user=${to}`);
  await expect(detail.getByRole('heading', { level: 2 })).toBeFocused();
  await expect(page.locator(`[data-user="${to}"]`)).toBeVisible();
  await expect(page.locator(`[data-user="${from}"]`)).toHaveCount(0);

  const old = await page.request.get(`/api/v1/users/${from}`, { maxRedirects: 0 });
  expect(old.status()).toBe(302);
  expect(old.headers()['location']).toContain(`/users/${to}`);

  await page.reload();
  await expect(page.locator(`[data-detail="${to}"]`)).toBeVisible();
  await scan(page, 'a renamed account');
});

test('a handle someone holds is refused on the field, in the dark theme', async ({ page }, info) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  const handle = `keep-${stamp(info.project.name)}`;
  await agent(page, handle);

  const rename = page.locator(`[data-detail="${handle}"] [data-rename]`);
  const field = rename.getByLabel('Change handle');
  await field.fill('ada');
  await rename.getByRole('button', { name: 'Rename' }).click();
  await rename.getByRole('button', { name: 'Confirm' }).click();
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(rename).toContainText('That handle is in use, or was given up too recently to reuse. Choose another.');
  await expect(field).toBeFocused();
  await expect(field).toHaveValue('ada');
  await expect(page).toHaveURL(`/admin/users?user=${handle}`);
  await scan(page, 'a refused handle, dark');
});
