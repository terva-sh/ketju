import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { stateFile } from './session';

// The post history screen (TKT-01M29CP2Q3), at 390 px and 1440 px, against
// the real server. alice starts a thread in events and edits its first post
// twice through the API, both within the grace window, so the history has
// three revisions, two of them grace edits. Who may read a history, and what
// a removed or redacted post's history holds, are the server's tests and the
// screen's unit tests; this proves the route, the endpoint and the screen
// agree.
//
// events is public, and no other spec's list reads it but new-thread.spec.ts,
// which finds its own threads by title. alice's stored session is used
// (session.setup.ts), so nothing here spends /auth/*. Each test runs one axe
// scan at most (TKT-01M3MTQG5A, e2e/a11y.spec.ts).

test.use({ storageState: stateFile('alice') });

const forum = 'events';
const sources = [
  'Bring a torch and something warm, we start at the old gate at seven.',
  'Bring a torch and something warm, we start at the new gate at seven.',
  'Bring a torch and something warm, we start at the new gate at eight.',
];

// One edited thread per project, made by its first test that needs it. One
// worker runs every test in turn (playwright.config.ts), so the rest of the
// project's tests find it here.
const made = new Map<string, { id: string; slug: string }>();

async function editedThread(request: APIRequestContext, project: string) {
  const have = made.get(project);
  if (have) return have;
  const created = await request.post(`/api/v1/forums/${forum}/threads`, {
    data: { title: `Bonfire route, ${project} ${Date.now()}`, source: sources[0] },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { thread } = (await created.json()) as { thread: { id: string; slug: string } };
  for (const source of sources.slice(1)) {
    const edited = await request.patch(`/api/v1/threads/${thread.id}/posts/1`, { data: { source } });
    expect(edited.status(), await edited.text()).toBe(200);
  }
  made.set(project, thread);
  return thread;
}

async function openHistory(page: Page, project: string, slug?: string) {
  const t = await editedThread(page.request, project);
  await page.goto(`/t/${t.id}/${slug ?? t.slug}/1/history`);
  await expect(page.locator('.post-history h2')).toHaveText('History of post #1');
  await expect(page.locator('.revision')).toHaveCount(3);
  return t;
}

const changes = (page: Page) => page.locator('.diff del, .diff ins');

test('a stale slug lands on the history, which compares the newest revisions and any two chosen', async ({ page }, info) => {
  const t = await openHistory(page, info.project.name, 'an-old-slug');
  await expect(page).toHaveURL(`/t/${t.id}/${t.slug}/1/history`);
  await expect(page.locator('.revision-number')).toHaveText(['Revision 3', 'Revision 2', 'Revision 1 (original)']);
  await expect(page.locator('[data-grace]')).toHaveCount(2);

  await expect(page.locator('.history-diff h3')).toHaveText('Revision 2 to revision 3');
  await expect(changes(page)).toHaveText(['seven.', 'eight.']);

  await page.getByRole('radio', { name: 'Compare from revision 1' }).check();
  await expect(page.locator('.history-diff h3')).toHaveText('Revision 1 to revision 3');
  await expect(changes(page)).toHaveText(['old', 'new', 'seven.', 'eight.']);

  await page.locator('.post-history').getByRole('link', { name: 'Back to post #1' }).click();
  await expect(page.locator('#post-1')).toBeVisible();
});

// The screen fits the width and passes axe (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts scans) in each theme.
for (const colorScheme of ['light', 'dark'] as const) {
  test(`the history passes the scan in the ${colorScheme} theme`, async ({ page }, info) => {
    await page.emulateMedia({ colorScheme });
    await openHistory(page, info.project.name);
    await expect(changes(page)).toHaveCount(2);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
    const { violations } = await new AxeBuilder({ page })
      .include('.post-history')
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();
    const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(found, colorScheme).toEqual([]);
  });
}
