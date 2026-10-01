import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { setReadPosition, stateFile } from './session';

// The phone's page sheet and lens sheet (TKT-01M29CGBZD, docs/06 "Mobile",
// "Pages"), run once per project in playwright.config.ts. The sheets are
// the phone's; at 1440 px no trigger for either is drawn. Each test makes
// one axe scan. None calls /auth/*: each starts from a stored session
// (e2e/session.ts).
//
// The main thread (seed.json) runs to page 3 and beyond, 25 posts a page.
// Other specs add posts to it, so no page count is named.

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

// small lists the sheet's controls whose target is under 44 px, pressed
// where the target should be, as e2e/targets.spec.ts does: each control is
// scrolled to the middle of the sheet and must be what is hit at its centre
// and 21 px either side of it, across and down.
function small(page: Page) {
  return page.evaluate(() => {
    const root = document.querySelector('.sheet[role=dialog]')!;
    const out: string[] = [];
    for (const el of root.querySelectorAll<HTMLElement>('a[href], button, input')) {
      if (el.getClientRects().length === 0) continue;
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
        out.push(`${el.tagName.toLowerCase()} "${name}" ${Math.round(r.width)}x${Math.round(r.height)}`);
      }
    }
    return out;
  });
}

// docked is whether the open sheet sits on the foot of the screen and pads
// by the bottom safe-area inset, as the bars do (thread.spec.ts). The inset
// is emulated through Emulation.setSafeAreaInsetsOverride; a Chromium
// without that method has no inset to give, and the padding is read from
// the rule that sets it instead.
async function docked(page: Page) {
  const sheet = page.locator('.sheet[role=dialog]');
  const box = (await sheet.boundingBox())!;
  expect(Math.round(box.y + box.height)).toBe(page.viewportSize()!.height);
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: { bottom: 34 } });
  } catch (err) {
    if (!/wasn'?t found|not found/i.test(String(err))) throw err;
    const padding = await page.evaluate(() => {
      for (const s of document.styleSheets)
        for (const rule of s.cssRules)
          if (rule instanceof CSSStyleRule && rule.selectorText === '.sheet') return rule.style.paddingBottom;
      return '';
    });
    expect(padding).toMatch(/^env\(safe-area-inset-bottom, ?0px\)$/);
    return;
  }
  await expect(sheet).toHaveCSS('padding-bottom', '34px');
  await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets: {} });
  await expect(sheet).toHaveCSS('padding-bottom', '0px');
}

test('the page sheet reaches any page at 390 px', async ({ browser, isMobile }, info) => {
  test.skip(!isMobile, 'the sheet is the phone’s');
  const { ctx, page } = await as('alice', browser, info);
  // First unread is offered to a reader with posts left to read.
  await setReadPosition(ctx.request, seed.thread, 3);
  await page.goto(`${seed.thread}?page=1`);
  const label = page.locator('.pager-narrow .where').first();
  await expect(label).toHaveText(/^Page 1 of \d+$/);
  await label.click();
  const sheet = page.getByRole('dialog', { name: 'Pages' });
  await expect(sheet).toBeVisible();
  await expect(label).toHaveAttribute('aria-expanded', 'true');
  await expect(sheet.locator('.page-grid a[aria-current=page]')).toBeFocused();
  await expect(sheet.locator('[data-sheet-first-unread]')).toBeVisible();
  await expect(sheet.locator('input[type=date]')).toBeVisible();
  await docked(page);
  await axe(page);
  await sheet.getByRole('link', { name: 'Page 2', exact: true }).click();
  await expect(page).toHaveURL(/\?page=2$/);
  await expect(sheet).toHaveCount(0);
  await expect(page.locator('.pager-narrow .where').first()).toHaveText(/^Page 2 of \d+$/);
  await ctx.close();
});

test('the lens sheet reaches every lens at 390 px, Removed posts only for a moderator', async ({ browser, isMobile }, info) => {
  test.skip(!isMobile, 'the sheet is the phone’s');
  for (const who of ['alice', 'mona']) {
    const { ctx, page } = await as(who, browser, info);
    await page.goto(`${seed.thread}?page=1`);
    const lens = page.locator('[data-lens-sheet]');
    await expect(page.locator('[data-lens-switch]')).toBeHidden();
    await lens.click();
    const sheet = page.getByRole('dialog', { name: 'Lens' });
    await expect(sheet).toBeVisible();
    await expect(lens).toHaveAttribute('aria-expanded', 'true');
    const items = await sheet.locator('#lens-menu a').evaluateAll((as) => as.map((a) => a.getAttribute('data-lens-item')));
    expect(items.slice(0, 2)).toEqual(['', 'unread']);
    expect(items).toContain('author:carol');
    expect(items).toContain('mentions');
    expect(items.includes('mod')).toBe(who === 'mona');
    await expect(sheet.locator('#lens-menu a').first()).toBeFocused();
    if (who === 'mona') {
      await docked(page);
      await axe(page);
      await sheet.locator('[data-lens-item=mod]').click();
      await expect(page).toHaveURL(/\?lens=mod$/);
      await expect(sheet).toHaveCount(0);
      await expect(page.locator('.lens-header')).toContainText('Removed posts');
    } else {
      await page.keyboard.press('Escape');
      await expect(sheet).toHaveCount(0);
      await expect(lens).toBeFocused();
      await lens.click();
      await sheet.locator('[data-lens-item="author:carol"]').click();
      await expect(page).toHaveURL(/\?lens=author:carol$/);
      await expect(page.locator('.lens-header')).toContainText('Posts by carol');
    }
    await ctx.close();
  }
});

test('sheet controls are at least 44 px, and the backdrop closes a sheet', async ({ browser, isMobile }, info) => {
  test.skip(!isMobile, 'the 44 px rule is the phone layout’s');
  const { ctx, page } = await as('alice', browser, info);
  await setReadPosition(ctx.request, seed.thread, 3);
  await page.goto(`${seed.thread}?page=2`);
  const label = page.locator('.pager-narrow .where').first();
  await label.click();
  await expect(page.getByRole('dialog', { name: 'Pages' })).toBeVisible();
  expect(await small(page), 'the page sheet').toEqual([]);
  await axe(page);
  // A press on the backdrop, above the sheet, ends it.
  await page.mouse.click(195, 20);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(label).toBeFocused();

  await page.locator('[data-lens-sheet]').click();
  await expect(page.getByRole('dialog', { name: 'Lens' })).toBeVisible();
  expect(await small(page), 'the lens sheet').toEqual([]);
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('[data-lens-sheet]')).toBeFocused();
  await ctx.close();
});

test('the desktop pager and switcher draw no sheet trigger at 1440 px', async ({ browser, isMobile }, info) => {
  test.skip(isMobile, 'the desktop’s');
  const { ctx, page } = await as('alice', browser, info);
  await page.goto(`${seed.thread}?page=1`);
  await expect(page.locator('.pager-wide').first()).toBeVisible();
  await expect(page.locator('.pager-narrow .where').first()).toBeHidden();
  await expect(page.locator('[data-lens-sheet]')).toBeHidden();
  const lens = page.locator('[data-lens-switch]');
  await expect(lens).toBeVisible();
  await lens.click();
  await expect(page.locator('#lens-menu')).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await axe(page);
  await ctx.close();
});
