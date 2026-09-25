import { expect, test } from '@playwright/test';
import { stateFile } from './session';

// Focus and motion (TKT-01M29CGBXK, AC2 and AC3), run once per project in
// playwright.config.ts against the e2e seed (cmd/ketju e2e.go).

const focused = (page: import('@playwright/test').Page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    return a ? `${a.tagName.toLowerCase()}:${(a.textContent ?? '').trim().slice(0, 40)}` : 'none';
  });

test('focus moves to each new screen’s heading', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('[data-forum="general"]')).toBeVisible();
  await page.locator('[data-forum="general"] .forum-name').click();
  await expect(page.locator('[data-screen="thread-list"] [data-thread]').first()).toBeVisible();
  expect(await focused(page)).toBe('h1:General');

  await page.locator('[data-thread]', { hasText: 'Naming the monthly threads' }).locator('.thread-title').click();
  await expect(page.locator('article.post').first()).toBeVisible();
  expect(await focused(page)).toBe('h1:Naming the monthly threads');

  // Back is a route change too.
  // The heading is focused as the screen arrives, and named when its data
  // does.
  await page.goBack();
  await expect(page.locator('[data-screen="thread-list"] [data-thread]').first()).toBeVisible();
  await expect.poll(() => focused(page)).toBe('h1:General');
});

test('the skip link is first and goes to the heading', async ({ page, isMobile }) => {
  test.skip(isMobile, 'Tab is the keyboard’s, and the phone project has none');
  await page.goto('/f/general');
  await expect(page.locator('[data-thread]').first()).toBeVisible();
  await page.keyboard.press('Tab');
  const skip = page.locator('.skip-link');
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press('Enter');
  expect(await focused(page)).toBe('h1:General');
  expect(page.url()).not.toContain('#');
});

test('focus is not lost when the control that had it goes', async ({ page }) => {
  // Paging a list by keyboard: the pager goes while the next page loads,
  // taking the focused "Older threads" with it, and on chatter's last page
  // (60 threads, three pages) it does not come back. Each time focus lands
  // on the heading, from where Tab goes on down the new page.
  await page.goto('/f/chatter');
  for (const first of ['Chatter thread 26', 'Chatter thread 51']) {
    await page.locator('[data-older]').focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-thread]').first()).toContainText(first);
    await expect.poll(() => focused(page)).toBe('h1:Chatter');
  }
  await expect(page.locator('[data-older]')).toHaveCount(0);
});

test('the composer sheet returns focus to the reply bar', async ({ browser, isMobile }, info) => {
  test.skip(!isMobile, 'the sheet is the phone’s');
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  const page = await ctx.newPage();
  await page.goto('/f/general');
  await page.locator('[data-thread]', { hasText: 'House rules' }).locator('.thread-title').click();
  await expect(page.locator('article.post').first()).toBeVisible();
  await page.locator('.replybar-open').click();
  await expect(page.locator('.composer-sheet textarea')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('.composer-sheet')).toHaveCount(0);
  await expect(page.locator('.replybar-open')).toBeFocused();
  await ctx.close();
});

// AC3: nothing moves for a reader who has asked for less motion. Nothing
// animates yet, so this holds the rule in place for what does later.
test('motion stops when reduced motion is asked for', async ({ browser }, info) => {
  const ctx = await browser.newContext({ ...info.project.use, reducedMotion: 'reduce', storageState: stateFile('carol') });
  const page = await ctx.newPage();
  await page.goto('/f/general');
  await expect(page.locator('[data-thread]').first()).toBeVisible();
  const longest = () =>
    page.evaluate(() => {
      let most = 0;
      const secs = (v: string) => Math.max(...v.split(',').map((s) => (s.trim().endsWith('ms') ? parseFloat(s) / 1000 : parseFloat(s))));
      for (const el of document.querySelectorAll('*')) {
        for (const pseudo of [null, '::before', '::after']) {
          const cs = getComputedStyle(el, pseudo);
          most = Math.max(most, secs(cs.transitionDuration), secs(cs.animationDuration));
        }
      }
      return { most, scroll: getComputedStyle(document.documentElement).scrollBehavior };
    });
  // A transition set on purpose, as a later change might, is held to it.
  // (Set through the CSSOM: the CSP refuses an injected stylesheet.)
  await page.evaluate(() => {
    for (const el of document.querySelectorAll<HTMLElement>('.thread-row, .unread-badge')) {
      el.style.transition = 'background 300ms';
      el.style.animation = 'none 2s infinite';
    }
    document.documentElement.style.scrollBehavior = 'smooth';
  });
  expect(await longest()).toEqual({ most: 0.00001, scroll: 'auto' });
  await ctx.close();
});
