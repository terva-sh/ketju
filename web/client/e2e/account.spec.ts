import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { paceAuth, signIn } from './session';

// The account screen (TKT-01M3V6XTYG), at 390 px and 1440 px, against the
// real server: a member changes their handle, the top bar and the Back link
// follow without a reload, the old handle redirects, and the 30 days start;
// then they deactivate, which ends their sessions and loads / signed out.
// The member is one of their own per project (seedAccount in
// cmd/ketju/e2e.go), whom no other spec signs in as. One sign-in spends two
// /auth/* requests, so the test waits for the limiter first (paceAuth; the
// budget is in e2e/session.ts). One axe scan, with the confirmation open.

test.use({ storageState: { cookies: [], origins: [] } });

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found).toEqual([]);
}

async function me(page: Page) {
  const r = await page.request.get('/api/v1/me');
  expect(r.status()).toBe(200);
  return (await r.json()) as { user: { handle: string } | null };
}

test('a member changes their handle, then deactivates', async ({ page }, info) => {
  await paceAuth(2);
  const from = `acct-${info.project.name}`;
  const to = `${from}-2`;
  await signIn(page, from);

  await test.step('Account is last in the settings links', async () => {
    await page.goto('/settings');
    const links = page.getByRole('navigation', { name: 'Settings' }).getByRole('link');
    await expect(links.last()).toHaveText('Account');
    await links.last().click();
    await page.waitForURL((u) => u.pathname === '/settings/account');
    await expect(page.getByRole('heading', { level: 1, name: 'Account' })).toBeVisible();
    await expect(page.locator('[data-current-handle]')).toHaveText(`@${from}`);
  });

  await test.step('the change is confirmed first, and this tab follows it without a reload', async () => {
    await page.evaluate(() => ((window as unknown as { kept: boolean }).kept = true));
    await page.getByLabel('New handle').fill(to);
    await page.getByRole('button', { name: 'Change handle' }).click();
    const dialog = page.getByRole('dialog', { name: `Change your handle to @${to}?` });
    await expect(dialog).toContainText('30 days');
    await expect(dialog).toContainText(`For 90 days your old handle, @${from},`);
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
    await scan(page);    const put = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/settings/handle' && r.request().method() === 'PUT');
    await dialog.getByRole('button', { name: 'Change handle' }).click();
    expect((await put).status()).toBe(200);
    await expect(page.getByRole('status')).toHaveText(`Your handle is now @${to}.`);
    await expect(page.locator('[data-current-handle]')).toHaveText(`@${to}`);
    await expect(page.locator('[data-nav=me]:visible')).toHaveAttribute('href', `/u/${to}`);
    await expect(page.locator('a.account-up')).toHaveAttribute('href', `/u/${to}`);
    expect(await page.evaluate(() => (window as unknown as { kept?: boolean }).kept)).toBe(true);
    expect((await me(page)).user?.handle).toBe(to);
  });

  await test.step('the 30 days have started', async () => {
    await page.reload();
    await expect(page.getByLabel('New handle')).toBeDisabled();
    await expect(page.locator('#account-handle-hint')).toContainText('You can change your handle again from');  });

  await test.step('the old handle redirects to the new one', async () => {
    await page.goto(`/u/${from}`);
    await page.waitForURL((u) => u.pathname === `/u/${to}`);
    await expect(page.locator('[data-screen=profile]')).toBeVisible();
  });

  await test.step('deactivating is confirmed first, then loads / signed out', async () => {
    await page.goto('/settings/account');
    const session = await page.context().cookies();
    await page.getByRole('button', { name: 'Deactivate account' }).click();
    const dialog = page.getByRole('dialog', { name: 'Deactivate your account?' });
    await expect(dialog).toContainText('sign in and press Reactivate');
    await dialog.getByRole('button', { name: 'Deactivate account' }).click();
    await page.waitForURL((u) => u.pathname === '/');
    await expect(page.locator('[data-screen=forum-index]')).toBeVisible();
    await expect(page.locator('[data-nav=me]:visible')).toHaveAttribute('href', /^\/auth\/signin\?/);
    expect((await me(page)).user).toBeNull();
    // The session the member had is over on the server, not only cleared here.
    await page.context().addCookies(session);
    expect((await me(page)).user).toBeNull();
  });
});
