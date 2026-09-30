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
//
// Each test runs one scan. Up to four scans once shared one test's 30 s
// budget. While another CI run loaded the runner they used it up, and the
// timeout fell on whichever scan or click was running at the time (runs 728,
// 751 and 950, TKT-01M3MTQG5A). The page comes from the page fixture, so its
// context is closed when a test fails as well as when it passes.

test.use({ storageState: stateFile('alice') });

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

// open puts a screen in its second state and says what it opened. opened
// names that state in the test's title, which is fixed before the project,
// and so the width, is known.
const screens: {
  name: string;
  path: string;
  ready: string;
  opened?: string;
  open?: (page: Page, phone: boolean) => Promise<string>;
}[] = [
  {
    name: 'forum index',
    path: '/',
    ready: '[data-screen="forum-index"] [data-forum]',
    opened: 'the forum picker open',
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
    opened: '#1’s replies or the composer sheet open',
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

// One test per screen, state and theme: each loads the page in its theme,
// opens the state if the test is for it, and runs one scan.
for (const s of screens) {
  for (const open of s.open ? [false, true] : [false]) {
    for (const colorScheme of ['light', 'dark'] as const) {
      test(`the ${s.name}${open ? ` with ${s.opened}` : ''} passes the scan in the ${colorScheme} theme`, async ({ page, isMobile }) => {
        await page.emulateMedia({ colorScheme });
        await page.goto(s.path);
        await expect(page.locator(s.ready).first()).toBeVisible();
        const what = open ? `${s.name} with ${await s.open!(page, isMobile)}` : s.name;
        expect(await scan(page), `${what}, ${colorScheme}`).toEqual([]);
      });
    }
  }
}
