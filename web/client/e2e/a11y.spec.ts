import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stateFile } from './session';

// The automated accessibility scan (TKT-01M29CGBXK, AC5), run once per
// project in playwright.config.ts, so at 390 px and 1440 px: the forum
// index, a thread list and a thread page, signed in so every control is
// drawn, in the light theme and the dark; the index again with the forum
// picker open, and the page with its composer sheet (phone) or a post's
// replies (desktop) open. The rules are axe's WCAG 2.1 A and AA sets; docs
// leave the rule set open, and 2.1 AA is what the ticket assumed. A pass is
// zero violations, with no exceptions. (A quote's source link once sat inside
// the <summary> that collapses it, which nested-interactive reported; it is
// now the quotation's first line, TKT-01M3BFSBPJ.)

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

const screens: { name: string; path: string; ready: string; open?: (page: Page, phone: boolean) => Promise<string | null> }[] = [
  {
    name: 'forum index',
    path: '/',
    ready: '[data-screen="forum-index"] [data-forum]',
    open: async (page, phone) => {
      await page.locator(phone ? '[data-fab]' : '.forum-index .new-thread').click();
      await expect(page.getByRole('dialog')).toBeVisible();
      return 'the forum picker';
    },
  },
  { name: 'thread list', path: '/f/general', ready: '[data-screen="thread-list"] [data-thread]' },
  {
    name: 'thread page',
    // Page 1, where post 2 quotes post 1 (cmd/ketju e2e.go).
    path: `${seed.thread}?page=1`,
    ready: '#post-2 details.quote .quote-source',
    open: async (page, phone) => {
      // The desktop scans the reply expander open (TKT-01M29CGBW6); the
      // phone, its composer sheet, which would cover it.
      if (!phone) {
        await page.locator('#post-1 button.reply-expander').click();
        await expect(page.locator('#replies-1 .post--recessed').first()).toBeVisible();
        return '#1’s replies open';
      }
      await page.locator('.replybar-open').click();
      await expect(page.locator('.composer-sheet')).toBeVisible();
      return 'the composer sheet';
    },
  },
];

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  // Say which rule and where, rather than a count.
  return violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
}

for (const s of screens) {
  test(`the ${s.name} passes the scan in both themes`, async ({ browser, isMobile }, info) => {
    const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('alice') });
    const page = await ctx.newPage();
    await page.goto(s.path);
    await expect(page.locator(s.ready).first()).toBeVisible();
    for (const colorScheme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme });
      expect(await scan(page), `${s.name}, ${colorScheme}`).toEqual([]);
    }
    const open = await s.open?.(page, isMobile);
    if (open) {
      for (const colorScheme of ['light', 'dark'] as const) {
        await page.emulateMedia({ colorScheme });
        expect(await scan(page), `${s.name} with ${open}, ${colorScheme}`).toEqual([]);
      }
    }
    await ctx.close();
  });
}
