import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
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

test('an admin provisions an agent and manages its token and limits', async ({ page }, info) => {
  const handle = `bot-${info.project.name}-${Date.now().toString(36)}`;
  const detail = page.locator(`[data-detail="${handle}"]`);
  const panel = detail.locator('[data-token-panel]');
  const tokenField = panel.getByLabel(`New token for ${handle}`);
  let first = '';

  await test.step('the new agent form', async () => {
    await page.goto('/admin/users');
    await page.getByRole('button', { name: 'New agent' }).click();
    await expect(page.getByLabel('Handle', { exact: true })).toBeFocused();
    await expect(page.getByLabel('Issue a token now')).toBeChecked();
    await scan(page, 'the new agent form');
  });

  await test.step('created with a token, shown once', async () => {
    await page.getByLabel('Handle', { exact: true }).fill(handle);
    await page.getByLabel('Display name').fill('E2E bot');
    await page.getByRole('button', { name: 'Create agent' }).click();
    await expect(panel).toContainText('This token is shown once. Store it now.');
    await expect(tokenField).toBeFocused();
    first = await tokenField.inputValue();
    await expect(detail.locator('[data-last-used]')).toHaveText('never');
    expect(first).toMatch(/^ketju_agent_/);
    expect(await accepted(page, first)).toBe(200);
    await scan(page, 'the token panel');

    await panel.getByRole('button', { name: 'Done' }).click();
    await expect(panel).toHaveCount(0);
    expect(await shown(page)).not.toContain(first);
    expect(page.url()).not.toContain(first);
    const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }));
    expect(stored).not.toContain(first);
  });

  await test.step('write limits', async () => {
    const burst = detail.getByLabel('Burst', { exact: true });
    await expect(burst).toHaveAttribute('placeholder', /^[0-9]+$/);
    await burst.fill('5');
    await detail.getByRole('button', { name: 'Save limits' }).click();
    await expect(detail.locator('form.admin-limits [role="status"]')).toHaveText('Saved.');
    await expect(burst).toHaveValue('5');
    await scan(page, 'the agent detail');
  });

  await test.step('a new token replaces the old one', async () => {
    await detail.getByRole('button', { name: 'Issue new token' }).click();
    await expect(detail).toContainText('The current token stops working at once.');
    await scan(page, 'the issue confirmation');
    await detail.getByRole('button', { name: 'Confirm' }).click();
    await expect(tokenField).toBeFocused();
    const second = await tokenField.inputValue();
    expect(second).toMatch(/^ketju_agent_/);
    expect(second).not.toBe(first);
    expect(await accepted(page, first)).toBe(401);
    expect(await accepted(page, second)).toBe(200);
    await panel.getByRole('button', { name: 'Done' }).click();
    expect(await shown(page)).not.toContain(second);

    await test.step('and revoking stops it', async () => {
      await detail.getByRole('button', { name: 'Revoke' }).click();
      await detail.getByRole('button', { name: 'Confirm' }).click();
      await expect(detail.locator('[data-no-credential]')).toHaveText('No live token.');
      expect(await accepted(page, second)).toBe(401);
      await scan(page, 'the agent without a token');
    });
  });

  await test.step('closing the detail', async () => {
    await detail.getByRole('button', { name: 'Close' }).click();
    await expect(detail).toHaveCount(0);
  });
});
