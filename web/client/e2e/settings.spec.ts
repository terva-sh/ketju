import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { paceAuth, signIn, stateFile } from './session';

// The settings screens (TKT-01M29CXK4E), at 390 px and 1440 px, one axe
// scan per test.
//
// The preferences test changes carol's stored settings through her stored
// session and puts them back after, because other specs read her thread
// pages at 25 to a page. The session and identity tests sign out and
// remove sign-ins, so they use a member of their own per project
// (seedSettings in cmd/ketju/e2e.go), whom no other spec signs in as. They
// spend /auth/*, so each waits for the limiter first (paceAuth; the budget
// is in e2e/session.ts).

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

// member is this project's settings member, and link the second subject
// the identities test links to them.
const member = (project: string) => `sid-${project}`;
const linkSubject = (project: string) => `sid-${project}-link`;

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations).toEqual([]);
}

async function me(page: Page) {
  const r = await page.request.get('/api/v1/me');
  expect(r.status()).toBe(200);
  return (await r.json()) as { user: { handle: string } | null; settings: { page_size: number; theme: string } };
}

const patched = (page: Page) =>
  page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/settings' && r.request().method() === 'PATCH');

test.describe('preferences', () => {
  test.use({ storageState: stateFile('carol') });

  test.afterEach(async ({ page }) => {
    const r = await page.request.patch('/api/v1/settings', { data: { page_size: 25, theme: 'system' } });
    expect(r.status(), await r.text()).toBe(200);
  });

  test('preferences apply at once and follow the member to a second browser', async ({ page, browser }, info) => {
    await test.step('the Account block leads to Settings', async () => {
      await page.goto('/u/carol');
      await page.locator('[data-account]').getByRole('link', { name: 'Settings' }).click();
      await page.waitForURL((u) => u.pathname === '/settings');
      await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
    });

    await test.step('page size 10 and the dark theme, each saved as it changes', async () => {
      const size = page.getByLabel('Posts on a thread page');
      await expect(size).toHaveValue('25');
      let answer = patched(page);
      await size.fill('10');
      await size.blur();
      expect((await answer).status()).toBe(200);
      answer = patched(page);
      await page.getByRole('radio', { name: 'Dark' }).check();
      expect((await answer).status()).toBe(200);
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      await expect(page.getByRole('status')).toHaveText('Saved.');
      const now = await me(page);
      expect(now.settings.page_size).toBe(10);
      expect(now.settings.theme).toBe('dark');
      await scan(page);
    });

    await test.step('a thread page holds 10 posts', async () => {
      await page.goto(`${seed.thread}?page=1`);
      await expect(page.locator('article.post').first()).toBeVisible();
      await expect(page.locator('article.post')).toHaveCount(10);
    });

    await test.step('a second browser shows the dark theme on its first load', async () => {
      const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
      try {
        const other = await ctx.newPage();
        await other.goto('/');
        await expect(other.locator('[data-screen=forum-index]')).toBeVisible();
        await expect(other.locator('html')).toHaveAttribute('data-theme', 'dark');
        expect(await other.evaluate(() => localStorage.getItem('ketju.theme'))).toBe('dark');
      } finally {
        await ctx.close();
      }
    });
  });
});

// The profile settings (TKT-01M3V6XVVG): carol sets a display name and a
// bio with a link and a mention, through her stored session. Other specs
// read her as "Carol Ruiz" with no bio, so both are put back after.
test.describe('profile', () => {
  test.use({ storageState: stateFile('carol') });

  test.afterEach(async ({ page }) => {
    const r = await page.request.patch('/api/v1/settings/profile', { data: { display_name: 'Carol Ruiz', bio: '' } });
    expect(r.status(), await r.text()).toBe(200);
  });

  test('a member edits their display name and bio, and the profile and thread page show them', async ({ page }) => {
    const name = 'Carol R. the Reader';
    await test.step('Edit profile leads to the form', async () => {
      await page.goto('/u/carol');
      await page.locator('[data-account]').getByRole('link', { name: 'Edit profile' }).click();
      await page.waitForURL((u) => u.pathname === '/settings/profile');
      await expect(page.getByRole('heading', { level: 1, name: 'Profile' })).toBeVisible();
      await expect(page.getByLabel('Display name')).toHaveValue('Carol Ruiz');
    });

    await test.step('Save sends the name and the bio', async () => {
      await page.getByLabel('Display name').fill(name);
      await page.getByLabel('Bio').fill('Reading with @alice. See the [book list](https://example.org/books).');
      const answer = page.waitForResponse(
        (r) => new URL(r.url()).pathname === '/api/v1/settings/profile' && r.request().method() === 'PATCH',
      );
      await page.getByRole('button', { name: 'Save' }).click();
      expect((await answer).status()).toBe(200);
      await expect(page.getByRole('status')).toContainText('Saved.');
      await scan(page);
    });

    await test.step('the profile shows both', async () => {
      await page.getByRole('link', { name: 'View your profile' }).click();
      await page.waitForURL((u) => u.pathname === '/u/carol');
      await expect(page.locator('main[data-screen=profile] h1')).toHaveText(name);
      const bio = page.locator('.profile-bio');
      await expect(bio.getByRole('link', { name: 'book list' })).toHaveAttribute('href', 'https://example.org/books');
      await expect(bio.getByRole('link', { name: '@alice' })).toHaveAttribute('href', '/u/alice');
    });

    await test.step('a thread page she posted in carries the new name', async () => {
      await page.goto(`${seed.thread}?page=1`);
      await expect(page.locator('#post-1 .post-header a.author')).toHaveText(name);
    });
  });
});

test.describe('a member of their own', () => {
  // A context with no stored session: these sign in afresh.
  test.use({ storageState: { cookies: [], origins: [] } });

  // Session management: two browsers of one member; the first signs the
  // second out, then signs out everywhere. Two sign-ins, four /auth/*
  // requests; the sign-outs are API calls.
  test('signing out another session ends it server-side', async ({ page, browser }, info) => {
    await paceAuth(4);
    const who = member(info.project.name);
    const second = await browser.newContext({ ...info.project.use, storageState: undefined });
    try {
      const b = await second.newPage();
      await test.step('signed in on two browsers', async () => {
        await signIn(page, who);
        await signIn(b, who);
      });

      await test.step('the first signs the second out, in place', async () => {
        await page.goto('/settings/sessions');
        const rows = page.locator('[data-session]');
        await expect(rows).toHaveCount(2);
        const mine = page.locator('[data-session][data-current]');
        await expect(mine).toContainText('This device');
        const other = page.locator('[data-session]:not([data-current])');
        const gone = page.waitForResponse((r) => r.request().method() === 'DELETE');
        await other.getByRole('button', { name: /^Sign out/ }).click();
        expect((await gone).status()).toBe(204);
        await expect(rows).toHaveCount(1);
        expect((await me(b)).user, "the second browser's session is ended on the server").toBeNull();
        expect((await me(page)).user?.handle).toBe(who);
      });

      await test.step('sign out everywhere says this browser goes too, then loads /', async () => {
        await page.getByRole('button', { name: 'Sign out everywhere' }).click();
        const dialog = page.getByRole('dialog', { name: 'Sign out everywhere?' });
        await expect(dialog).toContainText('this browser too');
        await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused();
        await scan(page);
        await dialog.getByRole('button', { name: 'Sign out everywhere' }).click();
        await page.waitForURL((u) => u.pathname === '/');
        await expect(page.locator('[data-screen=forum-index]')).toBeVisible();
        await expect(page.locator('[data-nav=me]')).toHaveAttribute('href', /^\/auth\/signin\?/);
        expect((await me(page)).user).toBeNull();
      });
    } finally {
      await second.close();
    }
  });

  // Identities: the member's only sign-in cannot be removed; once a second
  // is linked, the first can, which signs this browser out, and the second
  // signs the member in. The sign-in (2), the link (the link page, its
  // start and the callback: 3) and the sign-in with the second (2) are
  // seven /auth/* requests.
  test('the only identity cannot be removed, and a linked one can', async ({ page }, info) => {
    await paceAuth(7);
    const who = member(info.project.name);
    const link = linkSubject(info.project.name);
    await signIn(page, who);

    await test.step('the only identity has Remove disabled, and says why', async () => {
      await page.goto('/settings/identities');
      const rows = page.locator('[data-identity]');
      await expect(rows).toHaveCount(1);
      await expect(rows.first()).toContainText('Used for this session');
      await expect(rows.first().getByRole('button', { name: /^Remove/ })).toBeDisabled();
      await expect(rows.first()).toContainText('This is your only way to sign in.');
      await scan(page);
    });

    await test.step('Link a sign-in links a second, and comes back here', async () => {
      await page.getByRole('link', { name: 'Link a sign-in' }).click();
      await page.waitForURL((u) => u.pathname === '/auth/link');
      await page.getByRole('button', { name: 'Link Fake' }).click();
      await page.getByRole('link', { name: link, exact: true }).click();
      await page.waitForURL((u) => u.pathname === '/settings/identities');
      await expect(page.locator('[data-identity]')).toHaveCount(2);
    });

    await test.step('removing the first signs this browser out', async () => {
      const first = page.locator('[data-identity]', { hasText: 'Used for this session' });
      await first.getByRole('button', { name: /^Remove/ }).click();
      const dialog = page.getByRole('dialog', { name: 'Remove this sign-in?' });
      await expect(dialog).toContainText('this browser will be signed out too');
      await dialog.getByRole('button', { name: 'Remove' }).click();
      await page.waitForURL((u) => u.pathname === '/');
      await expect(page.locator('[data-screen=forum-index]')).toBeVisible();
      expect((await me(page)).user).toBeNull();
    });

    await test.step('the linked one signs the member in', async () => {
      await page.goto('/auth/fake/start');
      await page.getByRole('link', { name: link, exact: true }).click();
      await page.waitForURL((u) => u.pathname === '/');
      await expect.poll(async () => (await me(page)).user?.handle).toBe(who);
    });
  });
});
