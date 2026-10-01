import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { readPosition, setReadPosition, stateFile } from './session';

// The lens header, bar and switcher (TKT-01M29CTY71, docs/06 "Lenses"), run
// once per project in playwright.config.ts, so every test runs at 390 and
// 1440 px. Each test makes one axe scan. None calls /auth/*: each starts
// from a stored session (e2e/session.ts).
//
// The main thread (seed.json) has the reply chains: #30 answers #1
// (cmd/ketju e2e.go seedReplies). carol started it and wrote its odd posts.
// Other specs add posts to it, so no count of it is named. The unread tests
// read "Minutes from the September meeting" as mona, whom no other spec
// reads there, and set her position first.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

async function as(who: string, browser: Browser, info: TestInfo) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile(who) });
  return { ctx, page: await ctx.newPage() };
}

async function axe(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, found.join('\n')).toEqual([]);
}

const ordinals = (page: Page) =>
  page.locator('.posts > [id^="post-"]').evaluateAll((els) => els.map((e) => Number(e.getAttribute('data-ordinal'))));

const search = (page: Page) => new URL(page.url()).search;

// AC2, AC8 and AC11: Only carol is one press from the thread head at both
// widths, gives the author lens address, and a fresh load of it shows the
// same posts; Full thread goes to the post at the top of the viewport.
test('Only <handle> gives the author lens, which a fresh load shows the same', async ({ browser }, info) => {
  const { ctx, page } = await as('alice', browser, info);
  await page.goto(`${seed.thread}?page=2`);
  const only = page.locator('.thread-head [data-lens-only]');
  await expect(only).toHaveText('Only carol');
  await only.click();
  await expect(page).toHaveURL(/\?lens=author:carol$/);
  await expect(page.locator('.lens-header')).toContainText('Posts by carol');
  await expect(page.locator('[data-lens-count]')).toHaveText(/^Showing \d+ of \d+ posts · \d+ hidden$/);
  await expect(only).toHaveAttribute('aria-current', 'true');
  // carol's posts are the odd ones; the deleted #57 is not shown as itself,
  // so it is not in the lens, and gaps stand between hers.
  const shown = await ordinals(page);
  expect(shown.slice(0, 3)).toEqual([1, 3, 5]);
  await expect(page.locator('.posts > .lens-gap').first()).toHaveText('… 1 post …');
  await axe(page);
  await page.reload();
  await expect(page.locator('.lens-header')).toBeVisible();
  expect(await ordinals(page)).toEqual(shown);

  // AC11: Full thread follows the post at the top of the viewport, under the
  // phone's sticky bar, and lands on it in the full thread.
  const full = page.locator('[data-full-thread]');
  await expect(full).toHaveAttribute('href', /\/1$/);
  await page.locator('#post-9').evaluate((el) => {
    const bar = document.querySelector('.topbar.phone')?.getBoundingClientRect().bottom ?? 0;
    scrollBy(0, el.getBoundingClientRect().top - bar);
  });
  await expect(full).toHaveAttribute('href', /\/9$/);
  // The link as it reads now. A pointer click would first scroll the header
  // back into view, which changes the post at the top (filed as a draft:
  // the header is out of reach once the reader has scrolled).
  await full.dispatchEvent('click');
  await expect(page).toHaveURL(/\/t\/[^/]+\/[^/?]+\/9$/);
  await expect(page.locator('.lens-header')).toHaveCount(0);
  await expect(page.locator('#post-9')).toBeInViewport();
  await ctx.close();
});

// AC8: Conversation from a reply gives its chain, with dividers, and Full
// thread goes back to the full thread.
test('Conversation shows the chain, and Full thread returns to the full thread', async ({ browser, isMobile }, info) => {
  const { ctx, page } = await as('alice', browser, info);
  await page.goto(`${seed.thread}/30`);
  await expect(page.locator('#post-30')).toBeVisible();
  if (isMobile) {
    await page.locator('#post-30 [data-post-more]').click();
    await page.locator('#post-30 .post-more-list [data-conversation]').click();
  } else await page.locator('#post-30 [data-conversation]').click();
  await expect(page).toHaveURL(/\/t\/[^/]+\/[^/?]+\?lens=chain:30$/);
  await expect(page.locator('.lens-header')).toContainText('Conversation around #30');
  expect(await ordinals(page)).toEqual([1, 30]);
  await expect(page.locator('.posts > .lens-gap')).toHaveText(['… 28 posts …', /^… \d+ posts …$/]);
  await axe(page);

  // The chain fits on one screen, so #1 is at the top, and Full thread goes
  // there in the full thread.
  const full = page.locator('[data-full-thread]');
  await expect(full).toHaveAttribute('href', /\/1$/);
  await full.click();
  await expect(page).toHaveURL(/\/t\/[^/]+\/[^/?]+\/1$/);
  await expect(page.locator('.lens-header')).toHaveCount(0);
  await expect(page.locator('#post-1')).toBeInViewport();
  await ctx.close();
});

// AC3: a bare unread is held at its boundary, page 2 is unchanged once page
// 1 is read, and First unread lands on last_read + 1 from the lens and from
// the canonical view.
test('the unread lens holds while reading, and First unread lands after the position', async ({ browser, isMobile }, info) => {
  const { ctx, page } = await as('mona', browser, info);
  await page.goto('/f/general');
  const href = (await page
    .locator('[data-thread]', { hasText: 'Minutes from the September meeting' })
    .locator('.thread-title')
    .getAttribute('href'))!;
  const thread = href.replace(/\?.*$/, '');
  await setReadPosition(ctx.request, thread, 10);

  await page.goto(`${thread}?lens=unread`);
  await expect(page).toHaveURL(/\?lens=unread:10$/);
  await expect(page.locator('#post-11')).toBeVisible();
  // Read page 1 of the lens: its foot on screen for the second.
  await page.locator('.posts > [id^="post-"]').last().scrollIntoViewIfNeeded();
  await page.waitForTimeout(1600);
  // The pager at the foot, whichever row the width shows.
  await page.locator('.pager a[rel=next]').filter({ visible: true }).last().click();
  await expect(page).toHaveURL(/\?page=2&lens=unread:10$/);
  await expect(page.locator('#post-36')).toBeVisible();
  expect((await ordinals(page))[0]).toBe(36);
  const at = await readPosition(ctx.request, thread);
  expect(at).toBeGreaterThan(10);

  const firstUnread = async () => {
    if (isMobile) {
      await page.locator('[data-shell=phone-topbar] .more > button').click();
      await page.locator('[data-shell=more-menu] [data-action=first-unread]').click();
    } else await page.locator('[data-shell=toolbar] [data-action=first-unread]').click();
  };
  // From the lens: the post after the position, on this lens page or the
  // one holding it.
  await page.evaluate(() => scrollTo(0, 0));
  await firstUnread();
  await expect(page.locator(`#post-${at + 1}`)).toBeInViewport();
  expect(search(page)).toMatch(/lens=unread:10/);
  await axe(page);

  // From the canonical view: its permalink. The lens page reads on while it
  // is open, and flushes as it unloads, so the position is set after it has
  // gone.
  await page.goto('/f/general');
  await page.waitForTimeout(500);
  await setReadPosition(ctx.request, thread, at);
  await page.goto(`${thread}?page=1`);
  await expect(page.locator('#post-1')).toBeVisible();
  await firstUnread();
  await expect(page).toHaveURL(new RegExp(`/${at + 1}$`));
  await expect(page.locator(`#post-${at + 1}`)).toBeInViewport();
  await ctx.close();
});

// AC5: every lens is reachable from the Lens control, with 44 px targets on
// a phone; Removed posts is offered to a moderator and not to a member.
test('the switcher reaches every lens, Removed posts only for a moderator', async ({ browser, isMobile }, info) => {
  for (const who of ['alice', 'mona']) {
    const { ctx, page } = await as(who, browser, info);
    await page.goto(`${seed.thread}?page=1`);
    const lens = page.locator('[data-lens-switch]');
    await lens.click();
    await expect(lens).toHaveAttribute('aria-expanded', 'true');
    const items = page.locator('#lens-menu a');
    const lenses = await items.evaluateAll((as) => as.map((a) => a.getAttribute('data-lens-item')));
    expect(lenses.slice(0, 2)).toEqual(['', 'unread']);
    expect(lenses).toContain('author:carol');
    expect(lenses).toContain('author:alice');
    expect(lenses).toContain('mentions');
    expect(lenses.includes('mod')).toBe(who === 'mona');
    await expect(page.locator('#lens-menu input[type=date]')).toBeVisible();
    if (isMobile) {
      const targets = page.locator('[data-lens-only], [data-lens-switch], #lens-menu a, #lens-menu input, #lens-menu button');
      for (const box of await targets.evaluateAll((els) => els.map((e) => e.getBoundingClientRect().height)))
        expect(box).toBeGreaterThanOrEqual(44);
    }
    if (who === 'mona') {
      await axe(page);
      await page.locator('#lens-menu [data-lens-item=mod]').click();
      await expect(page).toHaveURL(/\?lens=mod$/);
      await expect(page.locator('.lens-header')).toContainText('Removed posts');
      await expect(page.locator('#post-57')).toBeVisible();
    } else {
      await page.keyboard.press('Escape');
      await expect(page.locator('#lens-menu')).toHaveCount(0);
      await expect(lens).toBeFocused();
    }
    await ctx.close();
  }
});
