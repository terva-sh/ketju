import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { stateFile } from './session';

// Splitting posts into a new thread (TKT-01M29CYSAM), at 390 px and 1440 px,
// against the real server. mona, the seeded moderator, starts a thread of
// three posts in events through the API, selects two from the thread page,
// splits them into a new thread, and reads both ends: the new thread's
// "split from" line, and the source's notice and tombstones. Who may split,
// and what a viewer who cannot read the other thread is told, are the
// server's tests (internal/service/threads split_test.go) and the screen's
// unit tests; this proves the screen, the chunk and the route agree.
//
// The thread is the spec's own, one per project, as post-history.spec.ts
// makes its own: the seeded side threads are read by other specs, and
// merging posts back is TKT-01M29CYSBA's, so a split of a seeded thread
// could not be undone. events is public, and no other spec's list reads it
// but new-thread.spec.ts and post-history.spec.ts, which find their own
// threads by title. mona's stored session is used (session.setup.ts), so
// nothing here spends /auth/*. Each test runs one axe scan at most
// (TKT-01M3MTQG5A, e2e/a11y.spec.ts).

test.use({ storageState: stateFile('mona') });

const forum = 'events';
type Ref = { id: string; slug: string; title: string };

// One source thread per project, made by the first test that needs it, and
// the thread its posts were split into, once the split test has run. One
// worker runs every test in turn (playwright.config.ts).
const sources = new Map<string, Ref>();
const splits = new Map<string, Ref>();

async function source(request: APIRequestContext, project: string): Promise<Ref> {
  const have = sources.get(project);
  if (have) return have;
  const title = `Bonfire logistics, ${project} ${Date.now()}`;
  const created = await request.post(`/api/v1/forums/${forum}/threads`, {
    data: { title, source: 'Who is bringing the kindling this year?' },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { thread } = (await created.json()) as { thread: { id: string; slug: string } };
  for (const text of ['Has anyone seen the old lantern hooks?', 'The lantern hooks are in the shed, second shelf.']) {
    const r = await request.post(`/api/v1/threads/${thread.id}/posts`, { data: { source: text } });
    expect(r.status(), await r.text()).toBe(201);
  }
  const t = { ...thread, title };
  sources.set(project, t);
  return t;
}

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

// selectTwo opens the source and selects its posts 2 and 3.
async function selectTwo(page: Page, isMobile: boolean, t: Ref) {
  await page.goto(`/t/${t.id}/${t.slug}`);
  await expect(page.locator('#post-3')).toBeVisible();
  await action(page, isMobile, 'select-posts');
  const bar = page.getByRole('region', { name: 'Selected posts' });
  await expect(bar).toBeVisible();
  await expect(bar.locator('[data-selection-count]')).toHaveText('0 posts selected');
  await page.getByRole('checkbox', { name: 'Select post #2' }).check();
  await page.getByRole('checkbox', { name: 'Select post #3' }).check();
  await expect(bar.locator('[data-selection-count]')).toHaveText('2 posts selected');
  return bar;
}

const scan = async (page: Page) => {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
};

test('a moderator selects posts, and the bar counts them until Escape', async ({ page, isMobile }, info) => {
  const t = await source(page.request, info.project.name);
  const bar = await selectTwo(page, isMobile, t);
  // Every post here is shown as itself, so each takes a box.
  await expect(page.locator('[data-select-post]')).toHaveCount(3);
  await scan(page);
  await page.keyboard.press('Escape');
  await expect(bar).toHaveCount(0);
  await expect(page.locator('[data-select-post]')).toHaveCount(0);
});

test('the split dialog moves the selection into a new thread', async ({ page, isMobile }, info) => {
  const t = await source(page.request, info.project.name);
  const bar = await selectTwo(page, isMobile, t);
  await bar.getByRole('button', { name: 'Split…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Split 2 posts into a new thread' });
  const title = dialog.getByRole('textbox', { name: 'Title' });
  await expect(title).toBeFocused();
  await expect(dialog.getByRole('combobox', { name: 'Forum' })).toHaveValue(forum);
  await scan(page);
  const to = `Lantern hooks, ${info.project.name} ${Date.now()}`;
  await title.fill(to);
  const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/split'));
  await dialog.getByRole('button', { name: 'Split', exact: true }).click();
  const r = await answer;
  expect(r.status(), await r.text()).toBe(200);
  const { thread } = (await r.json()) as { thread: { id: string; slug: string } };
  splits.set(info.project.name, { ...thread, title: to });

  await expect(page).toHaveURL(new RegExp(`/t/${thread.id}/${thread.slug}`));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(to);
  const from = page.locator('[data-split-from]');
  await expect(from).toHaveText(`Split from ${t.title}.`);
  await expect(from.getByRole('link')).toHaveAttribute('href', `/t/${t.id}/${t.slug}`);
  await expect(page.locator('article.post')).toHaveCount(2);
  await expect(page.locator('#post-1 .post-body')).toContainText('old lantern hooks');
  await expect(page.getByRole('region', { name: 'Selected posts' })).toHaveCount(0);
});

test('the source keeps its ordinals, with the notice at the first moved post', async ({ page }, info) => {
  const t = await source(page.request, info.project.name);
  const to = splits.get(info.project.name);
  test.skip(!to, 'the split test did not run in this project');
  await page.goto(`/t/${t.id}/${t.slug}`);
  const notice = page.locator('[data-move-notice]');
  await expect(notice).toHaveText(`2 posts were moved to ${to!.title}.`);
  await expect(notice.getByRole('link')).toHaveAttribute('href', `/t/${to!.id}/${to!.slug}/1`);
  for (const [o, copy] of [
    [2, 1],
    [3, 2],
  ]) {
    const tomb = page.locator(`#post-${o}`);
    await expect(tomb).toHaveClass(/\btombstone\b/);
    await expect(tomb.locator('.tombstone-text')).toHaveText(`Post moved to ${to!.title}`);
    await expect(tomb.locator('.tombstone-text a')).toHaveAttribute('href', `/t/${to!.id}/${to!.slug}/${copy}`);
  }
  await expect(page.locator('#post-1 .post-body')).toContainText('kindling');
  await scan(page);
});
