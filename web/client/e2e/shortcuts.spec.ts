import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { setReadPosition, stateFile } from './session';

// The desktop keyboard shortcuts (TKT-01M29CGBX4), run once per project in
// playwright.config.ts against the e2e seed (cmd/ketju e2e.go): j, k, g, u,
// n and ? at 1440 px, and none of them at 390 px. Reading a thread moves
// the reader's position (TKT-01M29CGBWN), so the test that needs alice's
// position in "Minutes from the September meeting" at #30, as seeded, puts
// it there first.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

async function asAlice(browser: Browser, info: TestInfo) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  return { ctx, page: await ctx.newPage() };
}

const focusedId = (page: Page) => page.evaluate(() => document.activeElement?.id ?? '');

// minutes is the address of the thread alice has read to #30, from its row
// in General's list.
async function minutes(page: Page): Promise<string> {
  await page.goto('/f/general');
  const title = page.locator('[data-thread]', { hasText: 'Minutes from the September meeting' }).locator('.thread-title');
  return (await title.getAttribute('href'))!;
}

test.describe('at 1440 px', () => {
  test.skip(({ isMobile }) => isMobile, 'the shortcuts are the desktop’s');

  test('j and k move between posts, g goes to one, ? lists them', async ({ browser }, info) => {
    const { ctx, page } = await asAlice(browser, info);
    await page.goto(`${seed.thread}?page=1`);
    await expect(page.locator('#post-1')).toBeVisible();

    await page.keyboard.press('j');
    expect(await focusedId(page)).toBe('post-1');
    await page.keyboard.press('j');
    expect(await focusedId(page)).toBe('post-2');
    await expect(page.locator('#post-2')).toBeInViewport();
    await page.keyboard.press('k');
    expect(await focusedId(page)).toBe('post-1');

    // g 4 0 Enter: post 40 is on page 2, so this is its permalink.
    await page.keyboard.press('g');
    await page.keyboard.press('4');
    await page.keyboard.press('0');
    await expect(page.locator('.shortcut-hint')).toHaveText('Go to #40');
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/\/40$/);
    await expect(page.locator('#post-40')).toBeFocused();
    await expect(page.locator('#post-40')).toBeInViewport();

    // j past the page's last post opens the next page on its first.
    await page.keyboard.press('g');
    await page.keyboard.press('5');
    await page.keyboard.press('0');
    await page.keyboard.press('Enter');
    await expect(page.locator('#post-50')).toBeFocused();
    await page.keyboard.press('j');
    await expect(page).toHaveURL(/\?page=3$/);
    await expect(page.locator('#post-51')).toBeFocused();
    await expect(page.locator('#post-51')).toBeInViewport();

    // AC3: ? and the visible button both open the list.
    await page.keyboard.press('?');
    const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('[data-shortcut]')).toHaveText([/^j/, /^k/, /^g then number then Enter/, /^u/, /^n/, /^\?/]);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await page.getByRole('button', { name: 'Keyboard shortcuts' }).click();
    await expect(dialog).toBeVisible();
    // The list's one scan, while it is open.
    const { violations } = await new AxeBuilder({ page }).include('.help-overlay').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    expect(violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)).toEqual([]);
    await dialog.getByRole('button', { name: 'Close' }).click();
    await expect(dialog).toHaveCount(0);

    // ? then Escape at once, as a quick reader presses them: the list
    // closes before it has taken focus as well as after (TKT-01M3TMRCMJ).
    // A watcher notes that it did open, so the closed list is not just one
    // ? never drew.
    await page.evaluate(() => {
      const w = window as unknown as { helpSeen: boolean };
      w.helpSeen = false;
      new MutationObserver((_, obs) => {
        if (!document.querySelector('.help-overlay')) return;
        w.helpSeen = true;
        obs.disconnect();
      }).observe(document.body, { childList: true, subtree: true });
    });
    await page.keyboard.press('?');
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    expect(await page.evaluate(() => (window as unknown as { helpSeen: boolean }).helpSeen)).toBe(true);
    await ctx.close();
  });

  test('none fires while typing in the composer', async ({ browser }, info) => {
    const { ctx, page } = await asAlice(browser, info);
    await page.goto(`${seed.thread}?page=1`);
    const area = page.locator('.composer textarea');
    await area.click();
    const url = page.url();
    await page.keyboard.type('jkg40u?n');
    await page.keyboard.press('Enter');
    await expect(area).toHaveValue('jkg40u?n\n');
    await expect(area).toBeFocused();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(page.url()).toBe(url);
    await area.fill('');
    await ctx.close();
  });

  test('u goes to the first unread post, and n to the next unread thread', async ({ browser }, info) => {
    const { ctx, page } = await asAlice(browser, info);
    const at = await minutes(page);
    await setReadPosition(ctx.request, at, 30);
    await page.goto(at);
    await expect(page.locator('#post-1')).toBeVisible();
    await page.keyboard.press('u');
    await expect(page).toHaveURL(/\/31$/);
    await expect(page.locator('#post-31')).toBeFocused();
    await expect(page.locator('#post-31')).toBeInViewport();

    // In General's order the next thread alice has not read all of is the
    // rain barrel one, which she has never opened: its own address.
    await page.keyboard.press('n');
    await expect(page).toHaveURL(/\/comparing-rain-barrels$/);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Comparing every rain barrel');
    await ctx.close();
  });
});

test.describe('at 390 px', () => {
  test.skip(({ isMobile }) => !isMobile, 'the phone layout’s');

  // AC4: the keys do nothing, and the list is not offered.
  test('the shortcuts are inert', async ({ browser }, info) => {
    const { ctx, page } = await asAlice(browser, info);
    await page.goto(await minutes(page));
    await expect(page.locator('#post-1')).toBeVisible();
    const url = page.url();
    const scroll = await page.evaluate(() => scrollY);
    const focus = await page.evaluate(() => document.activeElement?.outerHTML.slice(0, 80));
    for (const k of ['j', 'j', 'k', 'u', 'n', '?', 'g', '4', 'Enter']) await page.keyboard.press(k);
    // Long enough for a g to have gone, had it counted.
    await page.waitForTimeout(1700);
    expect(page.url()).toBe(url);
    expect(await page.evaluate(() => scrollY)).toBe(scroll);
    expect(await page.evaluate(() => document.activeElement?.outerHTML.slice(0, 80))).toBe(focus);
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.locator('.shortcut-hint')).toHaveText('');
    await expect(page.locator('.shortcuts-help')).toHaveCount(0);
    // The controls the shortcuts stand in for are all here (AC2).
    await expect(page.locator('.goto-post input')).toBeVisible();
    await expect(page.locator('.next-unread')).toBeVisible();
    await ctx.close();
  });
});
