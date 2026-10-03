import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { stateFile } from './session';

// Reply as a new thread (TKT-01M29CYSC0), at 390 px and 1440 px, against the
// real server. carol, a seeded member, starts a thread of two posts in events
// through the API, chooses "reply as new thread" under the second, finds the
// composer pre-filled with a plain quote of it, posts, and reads both ends:
// the new thread's "continued from" line and the source post's "continued
// in" note. Who is told what is the server's test (internal/service/threads
// continue_test.go) and the screen's unit tests; this proves the screen and
// the route agree.
//
// The source is the spec's own thread, one per project, as posts-split.spec.ts
// makes its own. carol's events draft is deleted first, so a draft left by
// an earlier run is not what the composer opens with. Her stored session is
// used (session.setup.ts), so nothing here spends /auth/*. Each test runs one
// axe scan at most (TKT-01M3MTQG5A, e2e/a11y.spec.ts).

test.use({ storageState: stateFile('carol') });

const forum = 'events';
type Ref = { id: string; slug: string; title: string };

const sources = new Map<string, Ref>();
const continued = new Map<string, Ref>();

async function source(request: APIRequestContext, project: string): Promise<Ref> {
  const have = sources.get(project);
  if (have) return have;
  const title = `Lantern route, ${project} ${Date.now()}`;
  const created = await request.post(`/api/v1/forums/${forum}/threads`, {
    data: { title, source: 'Which way does the lantern walk go this year?' },
  });
  expect(created.status(), await created.text()).toBe(201);
  const { thread } = (await created.json()) as { thread: { id: string; slug: string } };
  const r = await request.post(`/api/v1/threads/${thread.id}/posts`, {
    data: { source: 'We could start at the harbour and finish on the hill.' },
  });
  expect(r.status(), await r.text()).toBe(201);
  const t = { ...thread, title };
  sources.set(project, t);
  return t;
}

const scan = async (page: Page) => {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)).toEqual([]);
};

test('a member continues a post in a new thread, starting from a quote of it', async ({ page, isMobile }, info) => {
  const del = await page.request.delete(`/api/v1/forums/${forum}/draft`);
  expect([204, 404]).toContain(del.status());
  const t = await source(page.request, info.project.name);
  await page.goto(`/t/${t.id}/${t.slug}`);
  const second = page.locator('#post-2');
  await expect(second).toBeVisible();
  if (isMobile) {
    await second.locator('[data-post-more]').click();
  }
  await second.locator('[data-post-action=continue]').click();

  await expect(page).toHaveURL(new RegExp(`/f/${forum}/new\\?from=${t.id}&ordinal=2$`));
  const notice = page.locator('[data-continues]');
  await expect(notice).toHaveText(`Continuing ${t.title}, post #2.`);
  const body = page.locator('.composer textarea');
  await expect(body).toHaveValue(/^@carol wrote:\n\n> We could start at the harbour and finish on the hill\.\n\n$/);
  await scan(page);

  const title = `The hill route, ${info.project.name} ${Date.now()}`;
  await page.getByRole('textbox', { name: 'Title' }).fill(title);
  await body.fill(`${await body.inputValue()}The hill has the better view.`);
  const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith(`/forums/${forum}/threads`));
  await (isMobile ? page.locator('[data-shell-primary=post-thread]') : page.locator('[data-post-thread]')).click();
  const r = await answer;
  expect(r.status(), await r.text()).toBe(201);
  const { thread } = (await r.json()) as { thread: { id: string; slug: string } };
  continued.set(info.project.name, { ...thread, title });

  await expect(page).toHaveURL(new RegExp(`/t/${thread.id}/`));
  const from = page.locator('[data-continued-from]');
  await expect(from).toHaveText(`Continued from ${t.title}.`);
  await expect(from.getByRole('link')).toHaveAttribute('href', `/t/${t.id}/${t.slug}/2`);
  await expect(page.locator('#post-1 .post-body blockquote')).toContainText('harbour');
});

test('the source post notes the thread that continues it', async ({ page }, info) => {
  const t = await source(page.request, info.project.name);
  const to = continued.get(info.project.name);
  test.skip(!to, 'the continuing test did not run in this project');
  await page.goto(`/t/${t.id}/${t.slug}`);
  const note = page.locator('#post-2 + [data-continued-in]');
  await expect(note).toHaveText(`Continued in ${to!.title}.`);
  await expect(note.getByRole('link')).toHaveAttribute('href', `/t/${to!.id}/${to!.slug}`);
  await expect(page.locator('[data-continued-in]')).toHaveCount(1);
  await scan(page);
});
