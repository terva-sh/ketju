import { expect, test } from '@playwright/test';
import { paceAuth, stateFile } from './session';

// Link a sign-in from the client (TKT-01M3QS0TMV), at 390 px and 1440 px:
// a member presses their handle in the top bar, or the Me tab, and on their
// own profile presses "Link a sign-in", a full page load of the server's
// /auth/link (TKT-01M29CKRES), which comes back to the profile. Nobody else
// is offered it: not another member's profile, not a signed-out reader.
//
// It starts from alice's stored session rather than signing in, and the
// page load of /auth/link is the one /auth/* request it makes, so it first
// waits for the limiter to refill one (paceAuth); see the budget in
// e2e/session.ts. The browser stops on the server's page: pressing Link
// there would start a sign-in at the provider, which internal/web/auth's
// tests cover (TestNoLinkWithoutIntent and the rest).

test.use({ storageState: stateFile('alice') });

test('a member reaches the link page from their own profile', async ({ page }) => {
  await paceAuth(1);

  await test.step('signed in as alice', async () => {
    const me = await page.request.get('/api/v1/me');
    expect(me.status()).toBe(200);
    expect(((await me.json()) as { user?: { handle: string } }).user?.handle, "the stored session is alice's").toBe('alice');
  });

  await test.step('the handle or the Me tab goes to the profile', async () => {
    // The forum index, where the phone shows the tab bar (a thread page
    // shows the reply bar instead).
    await page.goto('/');
    await page.locator('[data-nav=me]').click();
    await page.waitForURL((u) => u.pathname === '/u/alice');
    await expect(page.locator('[data-screen=profile]')).toBeVisible();
  });

  await test.step('Link a sign-in is the server’s page, coming back here', async () => {
    const account = page.locator('[data-account]');
    await expect(account.getByRole('heading', { level: 2, name: 'Account' })).toBeVisible();
    const link = account.getByRole('link', { name: 'Link a sign-in' });
    await expect(link).toHaveAttribute('href', '/auth/link?return_to=%2Fu%2Falice');
    await link.click();
    await page.waitForURL((u) => u.pathname === '/auth/link');
    expect(new URL(page.url()).searchParams.get('return_to')).toBe('/u/alice');
    // The server's page, not the client's: its heading, and one form per
    // provider, here the e2e server's fake one.
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Link a sign-in');
    await expect(page.getByRole('button', { name: 'Link Fake' })).toBeVisible();
    await expect(page.locator('[data-screen]')).toHaveCount(0);
  });
});

// These two make no /auth/* request, so they spend nothing.
test('another member’s profile offers no link', async ({ page }) => {
  await page.goto('/u/carol');
  await expect(page.locator('[data-screen=profile]')).toBeVisible();
  await expect(page.locator('[data-account]')).toHaveCount(0);
  await expect(page.locator('a[href^="/auth/link"]')).toHaveCount(0);
});

test('a signed-out reader is offered no link', async ({ browser }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, storageState: undefined });
  const page = await ctx.newPage();
  await page.goto('/u/alice');
  await expect(page.locator('[data-screen=profile]')).toBeVisible();
  await expect(page.locator('[data-nav=me]')).toHaveAttribute('href', /^\/auth\/signin\?/);
  await expect(page.locator('[data-account]')).toHaveCount(0);
  await expect(page.locator('a[href^="/auth/link"]')).toHaveCount(0);
  await ctx.close();
});
