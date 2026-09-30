import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { stateFile } from './session';

// Rename and move from the thread screen (TKT-01M3QXV399), at 390 px and
// 1440 px, against the real server: the seeded moderator, mona, renames the
// garden forum's one thread and renames it back, then moves it to the
// reading group and back. Each test leaves the thread as it found it, which
// is how e2e/thread-moderation.spec.ts, the other spec that opens it, expects
// it (cmd/ketju e2e.go seedSideThreads). Who may rename and move, and the
// order of the refusals, are the server's tests; this proves the screen and
// the routes agree.

test.use({ storageState: stateFile('mona') });

const title = 'Drip line pressure is dropping at the far bed';
const slug = 'drip-line-pressure-is-dropping-at-the-far-bed';

// action runs one of the thread's actions: a toolbar button on desktop, an
// item of the top bar's more menu on a phone (docs/06 "Mobile").
async function action(page: Page, isMobile: boolean, id: string) {
  if (isMobile) {
    await page.locator('[data-shell=phone-topbar] .more > button').click();
    await page.locator(`[data-shell=more-menu] [data-action=${id}]`).click();
  } else {
    await page.locator(`[data-shell=toolbar] [data-action=${id}]`).click();
  }
}

// send presses the panel's button and waits for the route's answer.
async function send(page: Page, kind: 'rename' | 'move', button: string) {
  const answer = page.waitForResponse(
    (r) =>
      kind === 'rename'
        ? r.request().method() === 'PATCH' && /\/api\/v1\/threads\/[^/]+$/.test(new URL(r.url()).pathname)
        : r.request().method() === 'POST' && r.url().endsWith('/move'),
  );
  await page.locator(`[data-moderate=${kind}]`).getByRole('button', { name: button, exact: true }).click();
  const r = await answer;
  expect(r.status(), await r.text()).toBe(200);
  await expect(page.locator('[data-moderate]')).toHaveCount(0);
}

// open goes to the garden thread and checks it is as the seed left it.
async function open(page: Page) {
  await page.goto('/f/garden');
  await page.locator('[data-thread] .thread-title', { hasText: title }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(title);
  await expect(page).toHaveURL(new RegExp(`/${slug}(\\?|$)`));
}

async function rename(page: Page, isMobile: boolean, to: string) {
  await action(page, isMobile, 'rename');
  const box = page.getByRole('group', { name: 'Rename this thread' }).getByRole('textbox', { name: 'Title' });
  await expect(box).toBeFocused();
  await box.fill(to);
  await send(page, 'rename', 'Rename');
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(to);
}

async function move(page: Page, isMobile: boolean, forum: string, name: string) {
  await action(page, isMobile, 'move');
  const to = page.getByRole('group', { name: 'Move this thread' }).getByRole('combobox', { name: 'Move to' });
  await expect(to).toBeEnabled();
  await to.selectOption(forum);
  await send(page, 'move', 'Move');
  await expect(page.locator('.thread-head .forum')).toHaveText(name);
}

// Each test is short and runs at most one axe scan, because several steps
// and scans sharing one test's 30 s budget timed out on a loaded CI runner
// (TKT-01M3MTQG5A, e2e/a11y.spec.ts).
test('a moderator renames a thread, and the address follows', async ({ page, isMobile }) => {
  await open(page);
  await rename(page, isMobile, 'Drip line pressure is fixed');
  await expect(page).toHaveURL(/\/drip-line-pressure-is-fixed(\?|$)/);
  await rename(page, isMobile, title);
  await expect(page).toHaveURL(new RegExp(`/${slug}(\\?|$)`));
});

test('a moderator moves a thread and moves it back', async ({ page, isMobile }) => {
  await open(page);
  await move(page, isMobile, 'reading', 'Reading group');
  await move(page, isMobile, 'garden', 'The garden build');
});

// The move panel, whose select is the one new control, fits the width and
// passes axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans) in each theme.
// Nothing is sent, so the thread is left as it was.
for (const colorScheme of ['light', 'dark'] as const) {
  test(`the move panel passes the scan in the ${colorScheme} theme`, async ({ page, isMobile }) => {
    await page.emulateMedia({ colorScheme });
    await open(page);
    await action(page, isMobile, 'move');
    const to = page.getByRole('group', { name: 'Move this thread' }).getByRole('combobox', { name: 'Move to' });
    await expect(to).toBeEnabled();
    // The thread's own forum is not a destination.
    await expect(to.locator('option[value=garden]')).toHaveCount(0);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
    const { violations } = await new AxeBuilder({ page })
      .include('[data-moderate]')
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();
    const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(found, colorScheme).toEqual([]);
  });
}
