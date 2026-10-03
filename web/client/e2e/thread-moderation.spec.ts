import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { stateFile } from './session';

// Lock, unlock, pin and unpin from the thread screen (TKT-01M3QGH66W), at
// 390 px and 1440 px, against the real server: the seeded moderator, mona,
// locks the garden forum's one thread with a reason and unlocks it, then
// pins it in its forum and unpins it, and sets slow mode and turns it off
// (TKT-01M3R0PM0K), and edits its tags (TKT-01M3YSQPZX). Each project
// leaves the thread as it found it, unlocked, unpinned, with slow mode off
// and no tags, and no other spec reads it
// (cmd/ketju e2e.go seedSideThreads). The bodies each route takes, and the
// order of its refusals, are the server's tests; this proves the screen and
// the routes agree.

test.use({ storageState: stateFile('mona') });

const title = 'Drip line pressure is dropping at the far bed';

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
async function send(page: Page, kind: string, button: string, method = 'POST') {
  const answer = page.waitForResponse((r) => r.url().endsWith(`/${kind}`) && r.request().method() === method);
  await page.locator(`[data-moderate=${kind}]`).getByRole('button', { name: button, exact: true }).click();
  const r = await answer;
  expect(r.status(), await r.text()).toBe(200);
  await expect(page.locator('[data-moderate]')).toHaveCount(0);
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();
}

// open goes to the garden thread and checks it is as the seed left it,
// unlocked and unpinned.
async function open(page: Page) {
  await page.goto('/f/garden');
  await page.locator('[data-thread] .thread-title', { hasText: title }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(title);
  await expect(page.locator('[data-thread-state]')).toHaveCount(0);
}

// Each test is short and runs at most one axe scan, because several steps
// and scans sharing one test's 30 s budget timed out on a loaded CI runner
// (TKT-01M3MTQG5A, e2e/a11y.spec.ts).
test('a moderator locks and unlocks a thread', async ({ page, isMobile }) => {
  await open(page);
  const locked = page.locator('[data-thread-state=locked]');
  // Escape pressed straight after opening, with nothing between, closes
  // the panel (TKT-01M3TS0ZV9), and focus goes to the thread's title. On a
  // phone the more menu that opened the panel no longer takes the key and
  // puts focus on its button (TKT-01M3TV4HBF).
  await action(page, isMobile, 'lock');
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-moderate]')).toHaveCount(0);
  await expect(page.getByRole('heading', { level: 1 })).toBeFocused();

  await action(page, isMobile, 'lock');
  const lock = page.getByRole('group', { name: 'Lock this thread' });
  await expect(lock.getByRole('textbox', { name: 'Reason (optional)' })).toBeFocused();
  await lock.getByRole('textbox').fill('Moving this to the spring planning thread.');
  const lockBody = page.waitForRequest((r) => r.url().endsWith('/lock') && r.method() === 'POST');
  await send(page, 'lock', 'Lock');
  expect((await lockBody).postDataJSON()).toEqual({ reason: 'Moving this to the spring planning thread.' });
  await expect(locked).toHaveText('This thread is locked.');
  // A moderator may still post in a locked thread (docs/08).
  if (!isMobile) await expect(page.locator('[data-composer]')).toBeVisible();

  await action(page, isMobile, 'unlock');
  await send(page, 'unlock', 'Unlock');
  await expect(locked).toHaveCount(0);
});

test('a moderator pins and unpins a thread', async ({ page, isMobile }) => {
  await open(page);
  const pinned = page.locator('[data-thread-state=pinned]');
  await action(page, isMobile, 'pin');
  const pin = page.getByRole('group', { name: 'Pin this thread' });
  await expect(pin.getByRole('radio', { name: 'In this forum' })).toBeChecked();
  // mona is a moderator by role, so the global scope is hers to choose.
  await expect(pin.getByRole('radio', { name: 'On every forum' })).toBeVisible();
  await send(page, 'pin', 'Pin');
  await expect(pinned).toHaveText('Pinned in this forum.');

  await action(page, isMobile, 'unpin');
  await expect(page.getByRole('group', { name: 'Unpin this thread?' })).toBeVisible();
  await send(page, 'unpin', 'Unpin');
  await expect(pinned).toHaveCount(0);
});

// The seed leaves the thread with no slow mode of its own and the site
// default off, so the panel opens on Off. Turning it off at the end pins
// the thread at 0, which reads the same, and the next run's 10 s is still a
// change from it.
test('a moderator sets slow mode and turns it off', async ({ page, isMobile }) => {
  await open(page);
  await action(page, isMobile, 'slow-mode');
  const slow = page.getByRole('group', { name: 'Slow mode for this thread' });
  await expect(slow.getByRole('radio', { name: 'Off' })).toBeChecked();
  await slow.getByRole('radio', { name: '10 seconds' }).check();
  const body = page.waitForRequest((r) => r.url().endsWith('/slow-mode') && r.method() === 'PUT');
  await send(page, 'slow-mode', 'Save', 'PUT');
  expect((await body).postDataJSON()).toEqual({ seconds: 10 });
  // The page is asked for again, and the composer says so; a moderator is
  // exempt, so nothing holds Post. The phone's composer is its sheet.
  if (!isMobile) await expect(page.locator('[data-slow-mode]')).toHaveText('Slow mode: one post every 10 seconds');

  await action(page, isMobile, 'slow-mode');
  await expect(slow.getByRole('radio', { name: '10 seconds' })).toBeChecked();
  await slow.getByRole('radio', { name: 'Off' }).check();
  await send(page, 'slow-mode', 'Save', 'PUT');
  if (!isMobile) await expect(page.locator('[data-slow-mode]')).toHaveCount(0);
});

// TKT-01M3YSQPZX: Edit tags, PUT …/tags, and the head's chips. The seed
// gives the thread no tags, and the test takes them away again, so the next
// run, and the other project, find it as the seed left it. One axe scan, of
// the head with its chips.
test('a moderator edits the tags and the head shows them', async ({ page, isMobile }) => {
  await open(page);
  const chips = page.locator('.thread-head .thread-tags a');
  await expect(chips).toHaveCount(0);
  await action(page, isMobile, 'tags');
  const panel = page.getByRole('group', { name: 'Edit tags' });
  const field = panel.getByRole('textbox', { name: 'Tags' });
  await expect(field).toBeFocused();
  await expect(field).toHaveValue('');
  await field.fill('Irrigation, drip-line');
  const body = page.waitForRequest((r) => r.url().endsWith('/tags') && r.method() === 'PUT');
  await send(page, 'tags', 'Save', 'PUT');
  expect((await body).postDataJSON()).toEqual({ tags: ['Irrigation', 'drip-line'] });
  await expect(chips).toHaveText(['drip-line', 'irrigation']);
  await expect(chips.first()).toHaveAttribute('href', '/f/garden?tag=drip-line');
  // On a phone each chip is what a press 21 px above or below its centre
  // finds, as e2e/targets.spec.ts measures every control.
  if (isMobile)
    expect(
      await chips.evaluateAll((els) =>
        els.map((el) => {
          el.scrollIntoView({ block: 'center' });
          const r = el.getBoundingClientRect();
          const x = r.left + r.width / 2;
          const y = r.top + r.height / 2;
          return [y - 21, y, y + 21].every((py) => {
            const t = document.elementFromPoint(x, py);
            return !!t && (t === el || el.contains(t));
          });
        }),
      ),
    ).toEqual([true, true]);
  const { violations } = await new AxeBuilder({ page })
    .include('.thread-head')
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  expect(violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)).toEqual([]);

  await action(page, isMobile, 'tags');
  await expect(field).toHaveValue('drip-line irrigation');
  await field.fill('');
  await send(page, 'tags', 'Save', 'PUT');
  await expect(chips).toHaveCount(0);
});

// The pin panel, which has the most controls, and the slow mode panel fit
// the width and pass axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans) in
// each theme. Nothing is sent, so the thread is left as it was.
const panels = [
  ['pin', 'Pin this thread'],
  ['slow-mode', 'Slow mode for this thread'],
] as const;
for (const [kind, name] of panels) for (const colorScheme of ['light', 'dark'] as const) {
  test(`the ${kind} panel passes the scan in the ${colorScheme} theme`, async ({ page, isMobile }) => {
    await page.emulateMedia({ colorScheme });
    await open(page);
    await action(page, isMobile, kind);
    await expect(page.getByRole('group', { name })).toBeVisible();
    if (kind === 'slow-mode') await page.getByRole('radio', { name: 'Custom' }).check();
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

// Archiving (TKT-01M3YSQQ33). It cannot be undone, so each test archives a
// thread of its own: carol starts it in site-feedback, where live.spec.ts
// starts its threads too and no spec reads the list by position. The admin
// ada archives it from the page, and carol reads it afterwards.
async function startAsCarol(browser: Browser, info: TestInfo): Promise<{ id: string; slug: string; title: string }> {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  try {
    const title = `Archive ${info.project.name} ${info.title.slice(0, 16)} ${Date.now()}`;
    const r = await ctx.request.post('/api/v1/forums/site-feedback/threads', {
      data: { title, source: 'A thread that ends up closed history.' },
    });
    expect(r.status(), await r.text()).toBe(201);
    const t = ((await r.json()) as { thread: { id: string; slug: string } }).thread;
    return { ...t, title };
  } finally {
    await ctx.close();
  }
}

const axeFound = async (page: Page, include?: string) => {
  const builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
  const { violations } = await (include ? builder.include(include) : builder).analyze();
  return violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
};

test.describe('archiving', () => {
  test.use({ storageState: stateFile('ada') });

  // One axe scan, of the open panel.
  test('an admin archives a thread with a reason, and its page and row say so', async ({ page, isMobile, browser }, info) => {
    const t = await startAsCarol(browser, info);
    await page.goto(`/t/${t.id}/${t.slug}?page=1`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(t.title);
    await action(page, isMobile, 'archive');
    const panel = page.getByRole('group', { name: 'Archive this thread?' });
    await expect(panel.locator('[data-archive-warning]')).toHaveText(
      'Archiving makes this thread read-only for everyone, admins included. It cannot be undone.',
    );
    const reason = panel.getByRole('textbox', { name: 'Reason (optional)' });
    await expect(reason).toBeFocused();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
    expect(await axeFound(page, '[data-moderate]')).toEqual([]);
    await reason.fill('Answered and closed.');
    const body = page.waitForRequest((r) => r.url().endsWith('/archive') && r.method() === 'POST');
    await send(page, 'archive', 'Archive');
    expect((await body).postDataJSON()).toEqual({ reason: 'Answered and closed.' });
    await expect(page.locator('[data-thread-state=archived]')).toHaveText('This thread is archived.');
    await expect(page.locator('[data-composer]')).toHaveCount(0);
    if (isMobile) {
      await page.locator('[data-shell=phone-topbar] .more > button').click();
      await expect(page.locator('[data-shell=more-menu] [data-action=archive]')).toHaveCount(0);
      await expect(page.locator('[data-shell=more-menu] [data-action=lock]')).toHaveCount(0);
      await page.keyboard.press('Escape');
    } else {
      await expect(page.locator('[data-shell=toolbar] [data-action=archive]')).toHaveCount(0);
      await expect(page.locator('[data-shell=toolbar] [data-action=lock]')).toHaveCount(0);
    }

    await page.goto('/f/site-feedback');
    const row = page.locator('[data-thread]', { hasText: t.title });
    await expect(row.locator('.thread-flag.archived')).toHaveAccessibleName('Archived');
  });

  // carol reads the thread ada archived: the notice, and nothing that
  // writes. One axe scan, of the whole page.
  test('a member reads an archived thread and is offered nothing that writes', async ({ page, browser, isMobile }, info) => {
    const t = await startAsCarol(browser, info);
    const r = await page.request.post(`/api/v1/admin/threads/${t.id}/archive`, { data: {} });
    expect(r.status(), await r.text()).toBe(200);
    const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
    try {
      const carol = await ctx.newPage();
      await carol.goto(`/t/${t.id}/${t.slug}?page=1`);
      await expect(carol.locator('[data-thread-state=archived]')).toHaveText('This thread is archived.');
      await expect(carol.locator('#post-1')).toBeVisible();
      await expect(carol.locator('[data-composer]')).toHaveCount(0);
      await expect(carol.locator('.replybar-open')).toHaveCount(0);
      if (!isMobile) {
        await expect(carol.locator('#post-1 [data-post-action]')).toHaveCount(1);
        await expect(carol.locator('#post-1 [data-post-action=unread]')).toBeVisible();
      }
      expect(await axeFound(carol)).toEqual([]);
    } finally {
      await ctx.close();
    }
  });
});
