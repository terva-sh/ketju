import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser } from '@playwright/test';
import { stateFile } from './session';

// The new-thread screen (TKT-01M3NS16VH), run once per project in
// playwright.config.ts, so at 390 px and 1440 px. A title and body typed in
// one browser context are restored from the server draft in a second one
// signed in as the same member, and posting there creates the thread, lands
// on it, and deletes the draft.
//
// It starts threads in events: public, with one seeded thread, and read by
// no other spec's list, where thread-list.spec.ts reads general's first page
// by title and pin order, which a new thread would shift.
//
// Both tests use alice's stored session (session.setup.ts): /auth/* allows
// a burst of ten, and a sign-in here would spend it. Each test runs one axe
// scan at most, as e2e/a11y.spec.ts does (TKT-01M3MTQG5A).

const forum = 'events';
const draftUrl = `/api/v1/forums/${forum}/draft`;
const screen = `/f/${forum}/new`;

async function deleteDraft(request: APIRequestContext) {
  const r = await request.delete(draftUrl);
  expect(r.status(), await r.text()).toBe(204);
}

async function asAlice(browser: Browser, isMobile: boolean, baseURL: string | undefined) {
  return browser.newContext({
    baseURL,
    viewport: isMobile ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    isMobile,
    hasTouch: isMobile,
    ignoreHTTPSErrors: true,
    storageState: stateFile('alice'),
  });
}

test('a new thread’s title and body follow alice to another browser, and posting it opens the thread', async ({ browser, isMobile, baseURL }, info) => {
  const first = await asAlice(browser, isMobile, baseURL);
  const second = await asAlice(browser, isMobile, baseURL);
  try {
    await deleteDraft(first.request);
    const stamp = `${info.project.name} ${Date.now()}`;
    const title = `Meetup notes from the ${stamp} run`;
    const body = `Draft of a new thread, ${stamp}.`;

    await test.step('alice opens the screen from the thread list and writes a draft', async () => {
      const page = await first.newPage();
      await page.goto(`/f/${forum}`);
      await page.locator('.thread-list-head .new-thread').click();
      await expect(page).toHaveURL(/\/f\/events\/new$/);
      await expect(page.locator('[data-screen="new-thread"]')).toBeVisible();
      await page.locator('#new-thread-title').fill(title);
      await page.locator('.composer textarea').fill(body);
      // With when the server saved it (docs/06 "Composer").
      await expect(page.locator('.draft-note')).toHaveText(/^Draft saved \S/);
      await expect(page.locator('.draft-note time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);
      await expect
        .poll(async () => {
          const r = await first.request.get(draftUrl);
          return r.status() === 200 ? await r.json() : null;
        })
        .toMatchObject({ title, source: body });
      if (isMobile) await expect(page.locator('[data-shell=tabbar]')).toHaveCount(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
    });

    await test.step('another browser finds both, and posts the thread', async () => {
      const page = await second.newPage();
      await page.goto(screen);
      await expect(page.locator('#new-thread-title')).toHaveValue(title);
      await expect(page.locator('.composer textarea')).toHaveValue(body);
      await page.locator(isMobile ? '[data-shell=phone-topbar] [data-shell-primary=post-thread]' : '.composer-foot [data-post-thread]').click();
      await expect(page).toHaveURL(/^[^?]*\/t\/[^/]+\/[^/?]+\?page=1$/);
      await expect(page.locator('article.post', { hasText: body })).toBeVisible();
      await expect.poll(async () => (await second.request.get(draftUrl)).status()).toBe(204);
    });

    await test.step('the screen is empty the next time it opens', async () => {
      const page = await second.newPage();
      const loaded = page.waitForResponse((r) => new URL(r.url()).pathname === draftUrl && r.request().method() === 'GET');
      await page.goto(screen);
      expect((await loaded).status()).toBe(204);
      await expect(page.locator('#new-thread-title')).toHaveValue('');
      await expect(page.locator('.composer textarea')).toHaveValue('');
    });
  } finally {
    await deleteDraft(first.request);
    await first.close();
    await second.close();
  }
});

test.describe('accessibility', () => {
  test.use({ storageState: stateFile('alice') });

  test('the new-thread screen has no axe violations', async ({ page }) => {
    const loaded = page.waitForResponse((r) => new URL(r.url()).pathname === draftUrl && r.request().method() === 'GET');
    await page.goto(screen);
    await loaded;
    await expect(page.locator('[data-screen="new-thread"] #new-thread-title')).toBeVisible();
    const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)).toEqual([]);
  });
});
