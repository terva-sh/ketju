import AxeBuilder from '@axe-core/playwright';
import { expect, test, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { stateFile } from './session';

// The agent controls on the users screen (TKT-01M3QH3GS2), at 390 px and
// 1440 px: the seeded admin, ada, creates an agent with a token, sees the
// token once, sets the agent's write limits, issues a new token, which
// stops the first from working, and revokes it. Each state is checked for
// horizontal overflow and scanned with axe (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts does) in the light theme and the dark.
//
// Both projects run against one server, so each creates an agent of its
// own, named for the project and the moment.
//
// Each test runs one scan. Several scans in one test share its 30 s budget,
// and while another CI run loaded the runner that budget ran out in
// e2e/a11y.spec.ts (runs 728, 751 and 950, TKT-01M3MTQG5A). This flow
// creates an agent and issues and revokes its tokens, so a test cannot
// replay it up to its state. Instead the tests run in order, in serial
// mode, on one page that beforeAll opens as ada and afterAll closes. When a
// test fails, the rest of the file is skipped for that project, as the one
// test used to end (TKT-01M3RZXGBA).

test.describe.configure({ mode: 'serial' });

let context: BrowserContext;
let page: Page;
let handle: string;
let detail: Locator;
let panel: Locator;
let tokenField: Locator;
let first = '';
let second = '';

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

// shown is everything the page could give a token back from: its markup and
// every input's value.
const shown = (page: Page) =>
  page.evaluate(() =>
    [document.documentElement.outerHTML, ...[...document.querySelectorAll('input')].map((i) => i.value)].join('\n'),
  );

// accepted is the status a request made with a bearer token is answered
// with: 200 while the token is live, and 401 once it is not. An
// Authorization header decides the request, so ada's cookie plays no part
// (internal/auth/agentcred).
async function accepted(page: Page, token: string): Promise<number> {
  const r = await page.request.get('/api/v1/me', { headers: { Authorization: `Bearer ${token}` } });
  return r.status();
}

test.beforeAll(async ({ browser }, info) => {
  context = await browser.newContext({ ...info.project.use, storageState: stateFile('ada') });
  page = await context.newPage();
  handle = `bot-${info.project.name}-${Date.now().toString(36)}`;
  detail = page.locator(`[data-detail="${handle}"]`);
  panel = detail.locator('[data-token-panel]');
  tokenField = panel.getByLabel(`New token for ${handle}`);
});

test.afterAll(async () => {
  await context?.close();
});

test('the new agent form passes the scan in the light theme', async () => {
  await page.goto('/admin/users');
  await page.getByRole('button', { name: 'New agent' }).click();
  await expect(page.getByLabel('Handle', { exact: true })).toBeFocused();
  await expect(page.getByLabel('Issue a token now')).toBeChecked();
  await overflow(page, 'the new agent form');
  await scan(page, 'the new agent form', 'light');
});

dark('the new agent form');

test("a new agent's token is shown once, and the token panel passes the scan in the light theme", async () => {
  await page.getByLabel('Handle', { exact: true }).fill(handle);
  await page.getByLabel('Display name').fill('E2E bot');
  await page.getByRole('button', { name: 'Create agent' }).click();
  await expect(panel).toContainText('This token is shown once. Store it now.');
  await expect(tokenField).toBeFocused();
  first = await tokenField.inputValue();
  await expect(detail.locator('[data-last-used]')).toHaveText('never');
  expect(first).toMatch(/^ketju_agent_/);
  expect(await accepted(page, first)).toBe(200);
  await overflow(page, 'the token panel');
  await scan(page, 'the token panel', 'light');
});

dark('the token panel');

test('once Done is pressed, the token is nowhere on the page', async () => {
  await panel.getByRole('button', { name: 'Done' }).click();
  await expect(panel).toHaveCount(0);
  expect(await shown(page)).not.toContain(first);
  expect(page.url()).not.toContain(first);
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
  expect(stored).not.toContain(first);
});

test('the write limits save, and the agent detail passes the scan in the light theme', async () => {
  const burst = detail.getByLabel('Burst', { exact: true });
  await expect(burst).toHaveAttribute('placeholder', /^[0-9]+$/);
  await burst.fill('5');
  await detail.getByRole('button', { name: 'Save limits' }).click();
  await expect(detail.locator('form.admin-limits [role="status"]')).toHaveText('Saved.');
  await expect(burst).toHaveValue('5');
  await overflow(page, 'the agent detail');
  await scan(page, 'the agent detail', 'light');
});

dark('the agent detail');

test('the issue confirmation passes the scan in the light theme', async () => {
  await detail.getByRole('button', { name: 'Issue new token' }).click();
  await expect(detail).toContainText('The current token stops working at once.');
  await overflow(page, 'the issue confirmation');
  await scan(page, 'the issue confirmation', 'light');
});

dark('the issue confirmation');

test('a new token replaces the old one', async () => {
  await detail.getByRole('button', { name: 'Confirm' }).click();
  await expect(tokenField).toBeFocused();
  second = await tokenField.inputValue();
  expect(second).toMatch(/^ketju_agent_/);
  expect(second).not.toBe(first);
  expect(await accepted(page, first)).toBe(401);
  expect(await accepted(page, second)).toBe(200);
  await panel.getByRole('button', { name: 'Done' }).click();
  expect(await shown(page)).not.toContain(second);
});

test('revoking stops the token, and the agent without a token passes the scan in the light theme', async () => {
  await detail.getByRole('button', { name: 'Revoke' }).click();
  await detail.getByRole('button', { name: 'Confirm' }).click();
  await expect(detail.locator('[data-no-credential]')).toHaveText('No live token.');
  expect(await accepted(page, second)).toBe(401);
  await overflow(page, 'the agent without a token');
  await scan(page, 'the agent without a token', 'light');
});

dark('the agent without a token');

test('closing the detail', async () => {
  await detail.getByRole('button', { name: 'Close' }).click();
  await expect(detail).toHaveCount(0);
});
