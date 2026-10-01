import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// The profile and its posts (TKT-01M3V3CFMA), at 390 px and 1440 px, against
// the real server. Who may see which profile, and its status, are the
// server's tests (internal/web/api users_test.go); this proves the author
// link, the routes, the endpoints and the screens agree, and that the
// screen draws a status only when the server sends one.
//
// Every reader here starts from a stored session (session.setup.ts), so
// nothing spends /auth/*. Each test runs one axe scan (TKT-01M3MTQG5A,
// e2e/a11y.spec.ts).

test.use({ storageState: stateFile('alice') });

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

async function scan(page: Page, what: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, what).toEqual([]);
}

async function as(browser: Browser, info: TestInfo, who: string) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile(who) });
  return { ctx, page: await ctx.newPage() };
}

test('a member opens another member’s profile from a post’s author link, then the posts list, then a permalink', async ({ page }) => {
  // Page 1 of the seeded thread: #1 is carol's (cmd/ketju e2e.go).
  await page.goto(`${seed.thread}?page=1`);
  const author = page.locator('#post-1 .post-header a.author');
  await expect(author).toHaveText('Carol Ruiz');
  await author.click();

  await expect(page).toHaveURL('/u/carol');
  await expect(page.locator('main[data-screen=profile] h1')).toHaveText('Carol Ruiz');
  await expect(page.locator('.profile-handle')).toContainText('@carol');
  await expect(page.locator('[data-post-count]')).toHaveText(/^\d+$/);
  await expect(page.locator('.profile-facts time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);
  // Not her own profile, so no Account block; a member is shown no status.
  await expect(page.locator('[data-account]')).toHaveCount(0);
  await expect(page.locator('[data-status]')).toHaveCount(0);

  await page.locator('[data-profile-posts]').click();
  await expect(page).toHaveURL('/u/carol/posts');
  await expect(page.locator('main[data-screen=profile-posts] h1')).toHaveText('Posts by Carol Ruiz');
  // carol wrote half the seeded thread's sixty posts, so the first page is
  // full and there are older ones.
  const posts = page.locator('.history-post');
  await expect(posts).toHaveCount(25);
  await expect(page.locator('[data-older]')).toBeVisible();
  await scan(page, 'carol’s posts');

  const first = posts.first();
  await expect(first.locator('[data-forum-link]')).toHaveAttribute('href', /^\/f\//);
  const ordinal = first.locator('.ordinal');
  const href = (await ordinal.getAttribute('href'))!;
  expect(href).toMatch(/^\/t\/[^/]+\/[^/]+\/\d+$/);
  const n = href.split('/').pop();
  await ordinal.click();
  await expect(page).toHaveURL(href);
  await expect(page.locator(`#post-${n}`)).toBeVisible();
  await expect(page.locator(`#post-${n} .post-header a.author`)).toHaveText('Carol Ruiz');
});

// Profile visibility (docs/05 "Whose profile is served"): a suspended
// account's profile is served to members, and its status only to moderators
// and admins. Both projects share one server and the other specs sign in as
// the seeded readers, whose sessions a suspension would end, so each project
// suspends an account of its own: an agent ada makes here without a token, as
// admin-suspension.spec.ts does.
test('a moderator sees a suspended member’s status and a member does not', async ({ page, browser }, info) => {
  const handle = `prof-${info.project.name}-${Date.now().toString(36)}`;
  const admin = await as(browser, info, 'ada');
  const made = await admin.page.request.post('/api/v1/admin/users', {
    data: { kind: 'agent', handle, display_name: 'E2E profile', issue_credential: false },
  });
  expect(made.status(), await made.text()).toBe(201);
  const suspended = await admin.page.request.post(`/api/v1/admin/users/${handle}/suspend`, { data: { reason: 'Profile status check.' } });
  expect(suspended.status(), await suspended.text()).toBe(204);
  try {
    const mod = await as(browser, info, 'mona');
    await mod.page.goto(`/u/${handle}`);
    await expect(mod.page.locator('main[data-screen=profile] h1')).toHaveText('E2E profile');
    await expect(mod.page.locator('[data-status=suspended]')).toHaveText('Suspended');
    await scan(mod.page, 'a suspended account, to a moderator');
    await mod.ctx.close();

    await page.goto(`/u/${handle}`);
    await expect(page.locator('main[data-screen=profile] h1')).toHaveText('E2E profile');
    await expect(page.locator('[data-post-count]')).toHaveText('0');
    await expect(page.locator('[data-status]')).toHaveCount(0);
    await expect(page.locator('main')).not.toContainText(/suspended/i);
  } finally {
    const lifted = await admin.page.request.post(`/api/v1/admin/users/${handle}/unsuspend`, { data: {} });
    expect(lifted.status(), await lifted.text()).toBe(204);
    await admin.ctx.close();
  }
});

test('the own profile keeps the Account block', async ({ page }) => {
  await page.goto('/u/alice');
  await expect(page.locator('main[data-screen=profile] h1')).toHaveText('Alice Nakamura');
  await expect(page.locator('[data-post-count]')).toHaveText(/^\d+$/);
  const account = page.locator('[data-account]');
  await expect(account.getByRole('heading', { name: 'Account' })).toBeVisible();
  await expect(account.getByRole('link', { name: 'Link a sign-in' })).toBeVisible();
  await expect(account.getByRole('button', { name: 'Sign out' })).toBeVisible();
  await scan(page, 'alice’s own profile');
});
