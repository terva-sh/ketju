import AxeBuilder from '@axe-core/playwright';
import { expect, test, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { stateFile } from './session';

// The form editor (TKT-01M29CP2P1), at 390 px and 1440 px: the seeded
// admin, ada, adds a field to the registration form, sees a key the server
// refuses marked on the field with Save disabled, corrects it, saves a new
// version, and opens version 1 read-only. The editor, the refused draft,
// the saved form and the read-only version are each checked for horizontal
// overflow and scanned with axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts
// does) in the light theme and the dark.
//
// Both projects run against one server, one after the other (beforeAll
// checks that the config still says so), and apply.spec.ts fills in the
// form after them, so each run ends by saving the fields it started with
// back as the newest version. The version numbers are read from the screen,
// not assumed.
//
// Each test runs one scan. Several scans in one test share its 30 s budget,
// and while another CI run loaded the runner that budget ran out in
// e2e/a11y.spec.ts (runs 728, 751 and 950, TKT-01M3MTQG5A). This flow saves
// to the server, so a test cannot replay it up to its state. Instead the
// tests run in order, in serial mode, on one page that beforeAll opens as
// ada and afterAll closes. When a test fails, the rest of the file is
// skipped for that project, as the one test used to end (TKT-01M3RZXGBA).

test.describe.configure({ mode: 'serial' });

const formUrl = '/api/v1/admin/forms/registration';

let context: BrowserContext;
let page: Page;
let start: { active_version: number; schema: { fields: unknown[] } };
let n: number;
let save: Locator;
let added: Locator;

async function overflow(page: Page, what: string) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(over, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
}

async function scan(page: Page, what: string, colorScheme: 'light' | 'dark') {
  await page.emulateMedia({ colorScheme });
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, `${what}, ${colorScheme}`).toEqual([]);
}

// dark scans the state the test before it left, in the dark theme, and sets
// the light theme back for the next state's actions.
function dark(what: string) {
  test(`${what} passes the scan in the dark theme`, async () => {
    await scan(page, what, 'dark');
    await page.emulateMedia({ colorScheme: 'light' });
  });
}

test.beforeAll(async ({ browser }, info) => {
  // Both projects edit the one form on the one server, so their runs must
  // not overlap, or one would save over the other's base version and the
  // restore at the end would not hold (Terva review 1496).
  // playwright.config.ts runs one worker, one test at a time; if that ever
  // changes, this fails here, before anything is saved, rather than as a
  // 409 or a wrong form in apply.spec.ts.
  expect(info.config.workers, 'one worker: the projects share this form').toBe(1);
  expect(info.config.fullyParallel, 'no parallel tests: the projects share this form').toBe(false);
  context = await browser.newContext({ ...info.project.use, storageState: stateFile('ada') });
  page = await context.newPage();
  const before = await page.request.get(formUrl);
  expect(before.status()).toBe(200);
  start = (await before.json()) as typeof start;
  n = start.active_version;
  save = page.getByRole('button', { name: /^Save as version/ });
  added = page.locator('.fe-field').last();
});

test.afterAll(async () => {
  await context?.close();
});

test('the editor passes the scan in the light theme', async () => {
  await page.goto('/admin/forms');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Application forms');
  await expect(page.getByLabel('Title', { exact: true })).toHaveValue('Apply to join');
  await expect(page.locator('[data-preview] [data-field="who"]')).toContainText('Who invited you');
  await expect(save).toHaveText(`Save as version ${n + 1}`);
  await expect(save).toBeDisabled();
  await overflow(page, 'the editor');
  await scan(page, 'the editor', 'light');
});

dark('the editor');

test("the editor's phone frame is 390 px across", async () => {
  // Its border and padding included (Terva review 1494).
  const phone = page.getByRole('button', { name: 'Phone width' });
  await phone.click();
  await expect(phone).toHaveAttribute('aria-pressed', 'true');
  const box = await page.locator('[data-preview]').boundingBox();
  expect(box!.width).toBeLessThanOrEqual(390);
  await phone.click();
});

test('a key the server refuses is marked on its field, and passes the scan in the light theme', async () => {
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
  await overflow(page, 'the refused draft');
  await scan(page, 'the refused draft', 'light');
});

dark('the refused draft');

test('the corrected form saves as a new version, and passes the scan in the light theme', async () => {
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
  await overflow(page, 'the saved form');
  await scan(page, 'the saved form', 'light');
});

dark('the saved form');

test('version 1 opens read-only, and passes the scan in the light theme', async () => {
  await page.getByRole('button', { name: 'View version 1', exact: true }).click();
  const view = page.locator('[data-viewing="1"]');
  await expect(view.getByRole('heading', { name: 'Version 1, read-only' })).toBeFocused();
  await expect(view.locator('[data-field="who"]')).toContainText('Who invited you');
  await expect(view.locator('[data-field="pronouns"]')).toHaveCount(0);
  await expect(view.getByLabel('Name (required)')).toBeDisabled();
  await overflow(page, 'version 1, read-only');
  await scan(page, 'version 1, read-only', 'light');
});

dark('version 1, read-only');

test('the fields it started with are saved back', async () => {
  await page.locator('[data-viewing="1"]').getByRole('button', { name: 'Close version 1' }).click();
  await added.getByRole('button', { name: 'Remove' }).click();
  await expect(page.locator('[data-preview] [data-field="pronouns"]')).toHaveCount(0);
  await expect(save).toBeEnabled();
  await save.click();
  await expect(page.locator('[data-notice]')).toHaveText(`Saved. Version ${n + 2} is now the form applicants see.`);
  const after = (await (await page.request.get(formUrl)).json()) as { active_version: number; schema: unknown };
  expect(after.active_version).toBe(n + 2);
  expect(after.schema).toEqual(start.schema);
});
