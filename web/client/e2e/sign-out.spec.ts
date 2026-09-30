import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { paceAuth, signIn } from './session';

// Sign out from the client (TKT-01M3QVWKAG), at 390 px and 1440 px: a member
// presses their handle in the top bar, or the Me tab, and on their own
// profile presses Sign out, a plain form post to /auth/logout. The server
// ends the session and sends the browser to /, signed out, and nothing of
// theirs is left: not in this tab, not in another tab of the same browser,
// not behind Back, not in local storage.
//
// Signing out revokes the session on the server, so the spec signs carol in
// afresh in a context of its own; the stored session other specs load from
// carol.state.json survives. The sign-in (/auth/fake/start and the callback)
// and the sign-out are three /auth/* requests, so it first waits for the
// limiter to refill three (paceAuth); see the budget in e2e/session.ts.
//
// A page the back/forward cache restores is not tried here: Playwright runs
// Chromium with that cache off, so state/viewerGuard.test.ts holds it.

const cookieName = '__Host-ketju_session';

// signedOut is a page drawn for nobody: Me is Sign in, and no Account block.
async function signedOut(page: Page) {
  await expect(page.locator('[data-nav=me]')).toHaveAttribute('href', /^\/auth\/signin\?/);
  await expect(page.locator('[data-account]')).toHaveCount(0);
  await expect(page.locator('form[action="/auth/logout"]')).toHaveCount(0);
}

async function me(page: Page) {
  const r = await page.request.get('/api/v1/me');
  expect(r.status()).toBe(200);
  return (await r.json()) as { user: { handle: string } | null; pre_user: unknown };
}

test('a member signs out from their own profile', async ({ browser }, info) => {
  await paceAuth(3);
  const ctx = await browser.newContext({ ...info.project.use, storageState: undefined });
  try {
    const page = await ctx.newPage();

    await test.step('carol signs in, a session of this spec’s own', async () => {
      await signIn(page, 'carol');
    });
    const session = (await ctx.cookies()).find((c) => c.name === cookieName);
    expect(session, 'the session cookie').toBeDefined();

    // Another tab of the same browser, on carol's profile, and a draft copy
    // of hers in local storage, as a page left before its save answered keeps
    // one (state/drafts.ts).
    const other = await ctx.newPage();
    await test.step('a second tab shows carol’s profile', async () => {
      await other.goto('/u/carol');
      await expect(other.locator('[data-account]')).toBeVisible();
      await other.evaluate(() => localStorage.setItem('ketju.draft.carol.reply:x', '{"snapshot":{"source":"hers"}}'));
    });

    await test.step('the handle or the Me tab goes to her profile, in one press', async () => {
      await page.bringToFront();
      await page.goto('/');
      await page.locator('[data-nav=me]').click();
      await page.waitForURL((u) => u.pathname === '/u/carol');
    });

    const button = page.locator('[data-account] form[method=post][action="/auth/logout"]').getByRole('button', { name: 'Sign out' });
    await test.step('Sign out is in the Account block, a full target', async () => {
      await expect(button).toBeVisible();
      const box = await button.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(44);
      const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
      expect(violations).toEqual([]);
    });

    await test.step('Sign out lands on / signed out', async () => {
      const answer = page.waitForResponse((r) => new URL(r.url()).pathname === '/auth/logout');
      await button.click();
      const r = await answer;
      expect(r.request().method()).toBe('POST');
      expect(r.status()).toBe(303);
      await page.waitForURL((u) => u.pathname === '/');
      await expect(page.locator('[data-screen=forum-index]')).toBeVisible();
      await expect(page.locator('[data-shell="signin-hint"]')).toBeVisible();
      await signedOut(page);
      const now = await me(page);
      expect(now.user).toBeNull();
      expect(now.pre_user).toBeNull();
      expect((await ctx.cookies()).find((c) => c.name === cookieName)).toBeUndefined();
    });

    await test.step('the session is ended on the server, not only forgotten here', async () => {
      const r = await page.request.get('/api/v1/me', { headers: { cookie: `${cookieName}=${session!.value}` } });
      expect(r.status()).toBe(200);
      expect(((await r.json()) as { user: unknown }).user).toBeNull();
    });

    await test.step('nothing of carol’s is left in this browser', async () => {
      // The other tab reloads, signed out (state/viewerGuard.ts).
      await expect(other.locator('[data-screen=profile]')).toBeVisible();
      await signedOut(other);
      // The page after sign-out removed her local draft copies.
      expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('ketju.draft.')))).toEqual([]);
    });

    await test.step('Back and a reload do not bring the session back', async () => {
      await page.goBack();
      await page.waitForURL((u) => u.pathname === '/u/carol');
      await expect(page.locator('[data-screen=profile]')).toBeVisible();
      await signedOut(page);
      await page.reload();
      await expect(page.locator('[data-screen=profile]')).toBeVisible();
      await signedOut(page);
      expect((await me(page)).user).toBeNull();
    });
  } finally {
    await ctx.close();
  }
});
