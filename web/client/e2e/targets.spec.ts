import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// Touch targets on the phone layout (TKT-01M29CGBXK, AC1; docs/06 "Targets
// and gestures"): every standalone control is at least 44 px on its shorter
// side. An inline link inside text (an author's name, an ordinal, a tag)
// keeps its visual size and is measured by its hit area, which a .hit class
// pads out. Text inside a post's body is the author's prose, not the
// interface, and is not measured; a quote's header there is the interface
// (internal/render quote.go), and is.

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

test.skip(({ isMobile }) => !isMobile, 'the 44 px rule is the phone layout’s');

// small lists every visible control whose target is under 44 px, with what
// it is and its drawn size. The target is found by pressing where it should
// be: each control is scrolled to the middle of the screen and must be what
// is hit at its centre and 21 px either side of it, across and down. That
// counts a .hit link's padded area, and fails one clipped by an overflow or
// overlapped by a neighbour's. Within an open dialog only the dialog's
// controls are counted; the rest are behind it.
function small(page: Page) {
  return page.evaluate(() => {
    const controls = 'a[href], button, [role=button], input:not([type=hidden]), select, textarea, summary, [tabindex="0"]';
    const root = document.querySelector('[role=dialog], .composer-sheet') ?? document;
    const out: string[] = [];
    for (const el of root.querySelectorAll<HTMLElement>(controls)) {
      if (el.closest('.post-body') && !el.matches('details.quote > summary, details.quote > summary .quote-source')) continue;
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
  });
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

test('the thread page, and its composer sheet', async ({ browser }, info) => {
  const { ctx, page } = await signedIn(browser, info);
  // Page 1, where post 2 quotes post 1 (cmd/ketju e2e.go).
  await page.goto(`${seed.thread}?page=1`);
  await expect(page.locator('#post-2 details.quote summary .quote-source')).toBeVisible();
  expect(await small(page)).toEqual([]);
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
