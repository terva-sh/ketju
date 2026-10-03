import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// Touch targets on the phone layout (TKT-01M29CGBXK, AC1; docs/06 "Targets
// and gestures"): every standalone control is at least 44 px on its shorter
// side. An inline link inside text (an author's name, an ordinal, a tag)
// keeps its visual size and is measured by its hit area, which a .hit class
// pads out. Text inside a post's body is the author's prose, not the
// interface, and is not measured; a quote's header and its source link there
// are the interface (internal/render quote.go), and are.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

test.skip(({ isMobile }) => !isMobile, 'the 44 px rule is the phone layout’s');

// small lists every visible control whose target is under 44 px, with what
// it is and its drawn size. The target is found by pressing where it should
// be: each control is scrolled to the middle of the screen and must be what
// is hit at its centre and 21 px either side of it, across and down. That
// counts a .hit link's padded area, and fails one clipped by an overflow or
// overlapped by a neighbour's. Within an open dialog only the dialog's
// controls are counted; the rest are behind it. within names a popup whose
// controls alone are counted, because it covers what is under it.
function small(page: Page, within?: string) {
  return page.evaluate((within) => {
    const controls = 'a[href], button, [role=button], input:not([type=hidden]), select, textarea, summary, [tabindex="0"]';
    const root = (within ? document.querySelector(within) : document.querySelector('[role=dialog], .composer-sheet')) ?? document;
    const out: string[] = [];
    for (const el of root.querySelectorAll<HTMLElement>(controls)) {
      if (el.closest('.post-body') && !el.matches('details.quote > summary, details.quote .quote-source')) continue;
      if (el.getClientRects().length === 0 || getComputedStyle(el).visibility === 'hidden') continue;
      // A skip link is off screen until it is focused.
      if (el.classList.contains('skip-link')) continue;
      el.scrollIntoView({ block: 'center', inline: 'center' });
      const r = el.getBoundingClientRect();
      const x = r.left + r.width / 2;
      const y = r.top + r.height / 2;
      const hit = ([px, py]: number[]) => {
        const t = document.elementFromPoint(px, py);
        return !!t && (t === el || el.contains(t));
      };
      if (![[x, y], [x - 21, y], [x + 21, y], [x, y - 21], [x, y + 21]].every(hit)) {
        const name = (el.getAttribute('aria-label') || el.textContent || el.tagName).trim().slice(0, 40);
        out.push(`${el.tagName.toLowerCase()}.${[...el.classList].join('.')} "${name}" ${Math.round(r.width)}x${Math.round(r.height)}`);
      }
    }
    return out;
  }, within);
}

async function signedIn(browser: import('@playwright/test').Browser, info: import('@playwright/test').TestInfo) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
  return { ctx, page: await ctx.newPage() };
}

test('the forum index', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  await page.goto('/');
  await expect(page.locator('[data-forum]').first()).toBeVisible();
  expect(await small(page)).toEqual([]);
  await page.locator('[data-fab]').click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await small(page), 'with the forum picker open').toEqual([]);
  await ctx.close();
});

test('the thread list', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  await page.goto('/f/general');
  await expect(page.locator('[data-thread]').first()).toBeVisible();
  expect(await small(page)).toEqual([]);
  await page.goto('/f/chatter');
  await expect(page.locator('[data-older]')).toBeVisible();
  expect(await small(page), 'with paging').toEqual([]);
  await ctx.close();
});

// A cross-forum list's rows name their forum in a link beside the title
// (TKT-01M3V6Y4JP). alice has posts in the main thread, so Participated
// always has rows, whatever she has read.
test('a cross-forum list', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  await page.goto('/participated');
  await expect(page.locator('[data-thread] [data-forum-link]').first()).toBeVisible();
  expect(await small(page)).toEqual([]);
  await ctx.close();
});

test('the thread page, and its composer sheet', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  // Page 1, where post 2 quotes post 1 (cmd/ketju e2e.go).
  await page.goto(`${seed.thread}?page=1`);
  await expect(page.locator('#post-2 details.quote > summary')).toHaveText('Quote from #1');
  await expect(page.locator('#post-2 details.quote .quote-source')).toBeVisible();
  expect(await small(page)).toEqual([]);
  // The header is wider than the source link below it, so pressing at the
  // header's centre does not show the link's target reaching up into it.
  // Press the header's bottom edge right above the link (TKT-01M3BFSBPJ).
  const header = await page.evaluate(() => {
    const s = document.querySelector('#post-2 details.quote > summary')!;
    const a = document.querySelector('#post-2 details.quote .quote-source')!;
    s.scrollIntoView({ block: 'center' });
    const t = document.elementFromPoint(a.getBoundingClientRect().left + 4, s.getBoundingClientRect().bottom - 1);
    return t === s || !!(t && s.contains(t));
  });
  expect(header, 'the quote header’s target is its own above the source link').toBe(true);
  // The reply expander, open (TKT-01M29CGBW6): its toggle and the links in
  // the replies it shows (cmd/ketju e2e.go seedReplies).
  await page.locator('#post-1 button.reply-expander').click();
  await expect(page.locator('#replies-1 .post--recessed').first()).toBeVisible();
  expect(await small(page), 'with #1’s replies open').toEqual([]);
  await page.locator('.replybar-open').click();
  await expect(page.locator('.composer-sheet')).toBeVisible();
  expect(await small(page), 'with the sheet open').toEqual([]);
  await ctx.close();
});

// A profile and its posts (TKT-01M3V3CFMA): the posts link, and in the list
// each post's forum, thread and ordinal, and the paging.
test('a profile and its posts', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  await page.goto('/u/carol');
  await expect(page.locator('[data-profile-posts]')).toBeVisible();
  expect(await small(page)).toEqual([]);
  await page.goto('/u/carol/posts');
  await expect(page.locator('[data-older]')).toBeVisible();
  expect(await small(page), 'the posts list').toEqual([]);
  await ctx.close();
});

// A post's "more" control on the phone, its items, and the flag dialog
// (TKT-01M3QQWA5Z; docs/06 "Mobile"). #3 is carol's, so alice may flag it,
// and she is signed in, so unread from here is there too (TKT-01M3KFFFJR).
// #4 answers it, so it offers its conversation (TKT-01M29CTY71). She may
// start a thread in the forum, so reply as new thread is there too
// (TKT-01M29CYSC0). The open list covers the post below it, so only the
// list is measured.
test('a post’s more control, its items, and the flag dialog', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  await page.goto(`${seed.thread}?page=1`);
  const more = page.locator('#post-3 [data-post-more]');
  await expect(more).toBeVisible();
  await more.click();
  const list = page.locator('#post-3 .post-more-list');
  await expect(list).toBeVisible();
  await expect(list.locator('a, button')).toHaveText(['link', 'conversation', 'reply as new thread', 'flag', 'unread from here', 'history']);
  expect(await small(page, '#post-3 .post-more-list'), 'with #3’s more open').toEqual([]);
  await page.locator('#post-3 [data-post-action=flag]').click();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await small(page), 'with the flag dialog open').toEqual([]);
  await ctx.close();
});
