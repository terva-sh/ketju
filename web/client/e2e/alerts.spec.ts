import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Browser, type Page, type TestInfo } from '@playwright/test';
import { stateFile } from './session';

// The Alerts screen (TKT-01M29CTY86), at 390 px and 1440 px against the real
// server and its notification generator. alice starts a thread in
// site-feedback, as live.spec.ts does, and carol answers her first post
// there, naming her, through the API from her own stored session
// (session.setup.ts), so nothing here spends /auth/*. Each test runs one
// axe scan (WCAG 2.1 A and AA, as e2e/a11y.spec.ts scans).

test.use({ storageState: stateFile('alice') });

const forum = 'site-feedback';

async function scan(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found).toEqual([]);
}

// alertsCount is /me's notification count for whoever page is signed in as.
async function alertsCount(page: Page): Promise<number> {
  const r = await page.request.get('/api/v1/me');
  expect(r.status()).toBe(200);
  return ((await r.json()) as { notifications: number }).notifications;
}

// replyAsCarol has carol answer post 1 of thread id, mentioning alice.
async function replyAsCarol(browser: Browser, info: TestInfo, id: string) {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile('carol') });
  try {
    const r = await ctx.request.post(`/api/v1/threads/${id}/posts`, { data: { source: '@alice, this one is for you.', reply_to: 1 } });
    expect(r.status(), await r.text()).toBe(201);
  } finally {
    await ctx.close();
  }
}

// The bell on a desktop and the Alerts tab on a phone: the same link, in
// the bar each width draws.
const alertsLink = (page: Page, isMobile: boolean) => page.locator(`${isMobile ? '.tabbar' : '.topbar'} [data-nav=alerts]`);

test('an alert leads to its post and clears the badge', async ({ page, browser, isMobile }, info) => {
  // Every alert alice has is read first, by the route mark-all uses, so the
  // count below is this test's alone whatever ran before it.
  const cleared = await page.request.post('/api/v1/notifications/read', { data: { up_to: Number.MAX_SAFE_INTEGER } });
  expect(cleared.status(), await cleared.text()).toBe(200);
  expect(await alertsCount(page)).toBe(0);

  const title = `Alerts ${info.project.name} ${Date.now()}`;
  const created = await page.request.post(`/api/v1/forums/${forum}/threads`, { data: { title, source: 'Who will answer?' } });
  expect(created.status(), await created.text()).toBe(201);
  const t = ((await created.json()) as { thread: { id: string; slug: string } }).thread;

  await replyAsCarol(browser, info, t.id);
  // Generation is asynchronous: the generator follows the event bus.
  await expect.poll(() => alertsCount(page), { timeout: 15_000 }).toBe(1);

  await page.goto('/');
  const link = alertsLink(page, isMobile);
  await expect(link.locator('[data-badge]')).toHaveText(/^1\b/);

  const marked = page.waitForResponse((r) => r.url().endsWith('/api/v1/notifications/read') && r.request().method() === 'POST');
  await link.click();
  await expect(page).toHaveURL(/\/notifications$/);
  const row = page.locator('[data-alert]').filter({ has: page.getByRole('link', { name: title }) });
  await expect(row).toHaveCount(1);
  await expect(row).toHaveAttribute('data-kind', 'mention');
  await expect(row).toHaveClass(/\bunread\b/);
  await expect(row.locator('.alert-what')).toContainText('mentioned you in');
  expect((await marked).status()).toBe(200);
  // The rows shown were marked read, and /me fetched again: the badge is
  // gone, while the row keeps its marking until alice leaves.
  await expect(link.locator('[data-badge]')).toHaveCount(0);
  await expect(row).toHaveClass(/\bunread\b/);
  await scan(page);

  await row.getByRole('link', { name: title }).click();
  await expect(page).toHaveURL(new RegExp(`/t/${t.id}/${t.slug}/2$`));
  await expect(page.locator('#post-2')).toBeInViewport();
  await expect(page.locator('#post-2')).toContainText('this one is for you');
  await expect(alertsLink(page, isMobile).locator('[data-badge]')).toHaveCount(0);
  expect(await alertsCount(page)).toBe(0);
});

// An unread row's sentence is weighted, as its link is, so the link needs a
// cue besides colour: axe's link-in-text-block fails when text follows the
// link, which a thread_you_watch row has and a mention row does not
// (TKT-01M3XNQ772). The list is answered with one such row, and its mark
// with nothing, so it stays unread whatever alice has.
test('an unread watched-thread alert tells its link apart without colour', async ({ page, isMobile }) => {
  const row = {
    id: 9001,
    kind: 'thread_you_watch',
    thread: { id: 'tw', slug: 'a-watched-thread', title: 'A watched thread' },
    forum: { slug: forum, name: 'Site feedback' },
    ordinal: 3,
    actor: { handle: 'carol', display_name: 'Carol Ruiz', avatar: '', role: 'member', kind: 'human' },
    at: Date.now(),
    read: false,
  };
  await page.route('**/api/v1/notifications', (r) =>
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notifications: [row], next_cursor: '' }) }),
  );
  await page.route('**/api/v1/notifications/read', (r) =>
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ marked: 0 }) }),
  );
  await page.goto('/');
  await alertsLink(page, isMobile).click();
  const shown = page.locator('[data-alert="9001"]');
  await expect(shown).toHaveClass(/\bunread\b/);
  await expect(shown.locator('.alert-what')).toContainText('Carol Ruiz posted in A watched thread, which you watch');
  await scan(page);
});

test('an empty list says what would appear there', async ({ page, isMobile }) => {
  // A member with no alerts at all: the list is answered empty, since every
  // seeded reader may have some by the time this runs.
  await page.route('**/api/v1/notifications', (r) =>
    r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ notifications: [], next_cursor: '' }) }),
  );
  await page.goto('/');
  await alertsLink(page, isMobile).click();
  await expect(page.locator('[data-screen=notifications]')).toBeVisible();
  await expect(page.locator('[data-empty-list]')).toHaveText(
    'No alerts yet. You are alerted here when someone mentions you, replies to or quotes one of your posts, or posts in a thread you watch.',
  );
  await expect(page.getByRole('button', { name: 'Mark all read' })).toHaveCount(0);
  await scan(page);
});
