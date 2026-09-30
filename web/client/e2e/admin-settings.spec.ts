import AxeBuilder from '@axe-core/playwright';
import { expect, test, type BrowserContext, type Page } from '@playwright/test';
import { stateFile } from './session';

// The site settings screen (TKT-01M29CXK2H), at 390 px and 1440 px: the
// seeded admin, ada, opens it, renames the site, sees the new name in the
// wordmark after a reload, and resets it. The screen, and the screen after
// the change, are each checked for horizontal overflow and scanned with axe
// (WCAG 2.1 A and AA, as e2e/a11y.spec.ts does) in the light theme and the
// dark.
//
// It starts from ada's stored session and spends nothing from the /auth/*
// budget (e2e/session.ts). The site name is the one setting it changes, and
// every other spec reads the wordmark, if at all, as whatever /me says, so
// the change is harmless while it lasts; afterAll puts the default back
// whatever happened, so the next project starts from it. Both projects run
// against one server one after the other, so the tests run in serial mode
// on one page, as admin-forms.spec.ts does.

test.describe.configure({ mode: 'serial' });

const nameUrl = '/api/v1/admin/settings/site_name';
const renamed = 'The Test Club';

let context: BrowserContext;
let page: Page;

async function overflow(what: string) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(over, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
}

async function scan(what: string, colorScheme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme });
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, `${what}, ${colorScheme}`).toEqual([]);
}

function dark(what: string) {
  test(`${what} passes the scan in the dark theme`, async () => {
    await scan(what, 'dark');
    await page.emulateMedia({ colorScheme: 'light' });
  });
}

// wordmark is the site's name as the bar shows it on the index: the top
// bar's on a desktop, the phone bar's title on a phone.
const wordmark = () => page.locator('.wordmark:visible');

test.beforeAll(async ({ browser }, info) => {
  expect(info.config.workers, 'one worker: the projects share the site name').toBe(1);
  expect(info.config.fullyParallel, 'no parallel tests: the projects share the site name').toBe(false);
  context = await browser.newContext({ ...info.project.use, storageState: stateFile('ada') });
  page = await context.newPage();
});

test.afterAll(async () => {
  if (page) {
    const back = await page.request.put(nameUrl, { data: { value: 'Ketju' } });
    expect(back.status()).toBe(200);
  }
  await context?.close();
});

test('the screen passes the scan in the light theme', async () => {
  await page.goto('/admin/settings');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Site settings');
  await expect(page.locator('[data-setting]')).toHaveCount(12);
  // A select, whatever it holds: phase2-exit.spec.ts may have opened
  // registration by the time the second project runs.
  await expect(page.getByLabel('registration.mode')).toHaveJSProperty('tagName', 'SELECT');
  await expect(page.locator('[data-config] [data-section="oidc"]')).toContainText('provider secrets');
  await overflow('the screen');
  await scan('the screen', 'light');
});

dark('the screen');

test('renaming the site shows in the wordmark, and passes the scan in the light theme', async () => {
  const field = page.locator('[data-setting="site_name"]');
  await field.getByLabel('site_name').fill(renamed);
  await field.getByRole('button', { name: 'Save' }).click();
  await expect(field.locator('[data-saved]')).toHaveText('Saved.');
  await expect(field.locator('[data-changed]')).toContainText('Last changed by');
  await expect(field.locator('[data-changed]')).toContainText('@ada');
  await overflow('the changed screen');
  await scan('the changed screen', 'light');
});

dark('the changed screen');

test('the new name is the wordmark and the title on the next page, and resets', async () => {
  await page.goto('/');
  await expect(wordmark()).toHaveText(renamed);
  await expect(page).toHaveTitle(renamed);
  await page.goto('/admin/settings');
  const field = page.locator('[data-setting="site_name"]');
  await field.getByRole('button', { name: 'Reset to default' }).click();
  await expect(field.getByLabel('site_name')).toHaveValue('Ketju');
  await expect(field.locator('[data-saved]')).toHaveText('Saved.');
  await page.goto('/');
  await expect(wordmark()).toHaveText('Ketju');
});
