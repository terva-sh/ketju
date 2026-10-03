import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Browser, type Locator, type Page, type TestInfo } from '@playwright/test';
import { stateFile } from './session';

// Phase 4's exit (TKT-01M3YMG0NX, docs/09 and docs/15): the work behind the
// v1.0 tag. A moderator splits posts into a new thread and merges posts into
// another, and both ends say so; a member who cannot read where posts went
// is told they moved and nothing more; a member replies to a post as a new
// thread; tags are set, edited and filtered by; an admin archives a thread
// and nobody may write to it afterwards; and a post shows who quoted it, at
// 390 px and 1440 px. Each test makes one axe scan (WCAG 2.1 A and AA, as
// e2e/a11y.spec.ts scans).
//
// The selectors are the feature specs' (posts-split, reply-as-new-thread,
// new-thread, thread-moderation, thread-list and quoted-by), as their
// tickets landed them. alice is the member, carol the second member, mona
// the moderator and ada the admin, each from a stored session
// (session.setup.ts), so nothing here spends /auth/*.
//
// Writes go only into threads each test starts in site-feedback, through
// the API, titled with the project and the time, and into the threads a
// split makes from them. The reveal-rule test splits into the seeded
// restricted moderators forum, which alice cannot read and mona can; no
// other spec reads that forum's row or list as mona or ada, so the seed is
// unchanged.

test.use({ storageState: stateFile('alice') });

const forum = 'site-feedback';

async function scan(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, 'horizontal overflow').toBeLessThanOrEqual(0);
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, found.join('\n')).toEqual([]);
}

interface Thread {
  id: string;
  slug: string;
}

const path = (t: Thread) => `/t/${t.id}/${t.slug}`;

// as runs fn with who's stored session, in a context at this project's width.
async function as<T>(who: string, browser: Browser, info: TestInfo, fn: (request: APIRequestContext) => Promise<T>): Promise<T> {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile(who) });
  try {
    return await fn(ctx.request);
  } finally {
    await ctx.close();
  }
}

// browse runs fn on a page signed in as who, at this project's width.
async function browse<T>(who: string, browser: Browser, info: TestInfo, fn: (page: Page) => Promise<T>): Promise<T> {
  const ctx = await browser.newContext({ ...info.project.use, storageState: stateFile(who) });
  try {
    return await fn(await ctx.newPage());
  } finally {
    await ctx.close();
  }
}

async function start(request: APIRequestContext, title: string, source: string): Promise<Thread> {
  const r = await request.post(`/api/v1/forums/${forum}/threads`, { data: { title, source } });
  expect(r.status(), await r.text()).toBe(201);
  return ((await r.json()) as { thread: Thread }).thread;
}

async function reply(request: APIRequestContext, t: Thread, source: string) {
  const r = await request.post(`/api/v1/threads/${t.id}/posts`, { data: { source } });
  expect(r.status(), await r.text()).toBe(201);
}

const title = (info: TestInfo, what: string) => `${what} ${info.project.name} ${Date.now()}`;

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

// offered is every thread action this width offers: the toolbar's on
// desktop, the more menu's on a phone, which is closed again after.
async function offered(page: Page, isMobile: boolean): Promise<string[]> {
  if (!isMobile) return page.locator('[data-shell=toolbar] [data-action]').evaluateAll((els) => els.map((e) => e.getAttribute('data-action')!));
  const more = page.locator('[data-shell=phone-topbar] .more > button');
  await more.click();
  const menu = page.locator('[data-shell=more-menu]');
  await expect(menu).toBeVisible();
  const ids = await menu.locator('[data-action]').evaluateAll((els) => els.map((e) => e.getAttribute('data-action')!));
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  return ids;
}

// selectPosts opens t as a moderator and selects the posts at ordinals.
async function selectPosts(page: Page, isMobile: boolean, t: Thread, ordinals: number[]) {
  await page.goto(path(t));
  await expect(page.locator(`#post-${Math.max(...ordinals)}`)).toBeVisible();
  await action(page, isMobile, 'select-posts');
  const bar = page.getByRole('region', { name: 'Selected posts' });
  await expect(bar).toBeVisible();
  for (const o of ordinals) await page.getByRole('checkbox', { name: `Select post #${o}` }).check();
  await expect(bar.locator('[data-selection-count]')).toHaveText(`${ordinals.length} posts selected`);
  return bar;
}

// split splits the selected posts of the page's thread into a new thread
// titled to in forum where, and answers the new thread.
async function split(page: Page, bar: Locator, to: string, where = forum): Promise<Thread> {
  await bar.getByRole('button', { name: 'Split…' }).click();
  const dialog = page.getByRole('dialog', { name: /^Split \d+ posts into a new thread$/ });
  const field = dialog.getByRole('textbox', { name: 'Title' });
  await expect(field).toBeFocused();
  await field.fill(to);
  if (where !== forum) await dialog.getByRole('combobox', { name: 'Forum' }).selectOption(where);
  await expect(dialog.getByRole('combobox', { name: 'Forum' })).toHaveValue(where);
  const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/split'));
  await dialog.getByRole('button', { name: 'Split', exact: true }).click();
  const r = await answer;
  expect(r.status(), await r.text()).toBe(200);
  return ((await r.json()) as { thread: Thread }).thread;
}

// byline is what a post's header says of who wrote it and when.
const byline = (page: Page, ordinal: number) =>
  page.locator(`#post-${ordinal} .post-header`).evaluate((h) => ({
    author: h.querySelector('a.author')?.textContent,
    at: h.querySelector('time')?.getAttribute('datetime'),
  }));

// deleteDraft clears the signed-in member's site-feedback draft, so the
// new-thread screen opens with what the test puts there.
async function deleteDraft(request: APIRequestContext) {
  const r = await request.delete(`/api/v1/forums/${forum}/draft`);
  expect([204, 404], await r.text()).toContain(r.status());
}

test.describe('moving posts', () => {
  test('a moderator selects posts and splits them into a new thread, and both threads show their notices', async ({
    browser,
    isMobile,
  }, info) => {
    const from = title(info, 'Split source');
    const t = await as('alice', browser, info, async (request) => {
      const t = await start(request, from, 'Where should the new bike racks go?');
      await as('carol', browser, info, (carol) => reply(carol, t, 'Racks by the library door, said carol.'));
      await reply(request, t, 'And a second rack by the gym, said alice.');
      return t;
    });

    await browse('mona', browser, info, async (page) => {
      const bar = await selectPosts(page, isMobile, t, [2, 3]);
      const before = [await byline(page, 2), await byline(page, 3)];
      expect(before.map((b) => b.author)).toEqual(['Carol Ruiz', expect.any(String)]);
      const to = title(info, 'Bike racks');
      const n = await split(page, bar, to);

      // The new thread: its head says where it came from, and the posts keep
      // their authors and times.
      await expect(page).toHaveURL(new RegExp(`${path(n)}$`));
      await expect(page.getByRole('heading', { level: 1 })).toHaveText(to);
      const note = page.locator('[data-split-from]');
      await expect(note).toHaveText(`Split from ${from}.`);
      await expect(note.getByRole('link')).toHaveAttribute('href', path(t));
      await expect(page.locator('article.post')).toHaveCount(2);
      await expect(page.locator('#post-1 .post-body')).toContainText('library door');
      await expect(page.locator('#post-2 .post-body')).toContainText('by the gym');
      expect([await byline(page, 1), await byline(page, 2)]).toEqual(before);

      // The source: the notice at the first moved post, and tombstones that
      // link to each copy.
      await page.goto(path(t));
      const notice = page.locator('[data-move-notice]');
      await expect(notice).toHaveText(`2 posts were moved to ${to}.`);
      await expect(notice.getByRole('link')).toHaveAttribute('href', `${path(n)}/1`);
      await expect(page.locator('[data-move-notice] + #post-2')).toHaveCount(1);
      for (const [o, copy] of [
        [2, 1],
        [3, 2],
      ]) {
        const tomb = page.locator(`#post-${o}`);
        await expect(tomb).toHaveClass(/\btombstone\b/);
        await expect(tomb.locator('.tombstone-text')).toHaveText(`Post moved to ${to}`);
        await expect(tomb.locator('.tombstone-text a')).toHaveAttribute('href', `${path(n)}/${copy}`);
      }
      await expect(page.locator('#post-1 .post-body')).toContainText('bike racks');
      await scan(page);
    });
  });

  test('a moderator merges posts into an existing thread, and both threads show their notices', async ({
    browser,
    isMobile,
  }, info) => {
    const from = title(info, 'Merge source');
    const into = title(info, 'Merge target');
    const [t, target] = await as('carol', browser, info, async (request) => {
      const t = await start(request, from, 'The noticeboard by the gate is full.');
      await reply(request, t, 'Old posters could come down on Sundays.');
      await reply(request, t, 'Sunday works, and the pins go in the jar.');
      const target = await start(request, into, 'Keeping the noticeboards tidy.');
      await reply(request, target, 'One poster per event, please.');
      return [t, target];
    });

    await browse('mona', browser, info, async (page) => {
      const bar = await selectPosts(page, isMobile, t, [2, 3]);
      await bar.getByRole('button', { name: 'Merge…' }).click();
      const dialog = page.getByRole('dialog', { name: 'Merge 2 posts into another thread' });
      const field = dialog.getByRole('textbox', { name: 'Thread link or id' });
      await expect(field).toBeFocused();
      await field.fill(new URL(path(target), page.url()).href);
      await expect(dialog.locator('[data-merge-target]')).toContainText(into);
      const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/merge'));
      await dialog.getByRole('button', { name: 'Merge', exact: true }).click();
      const r = await answer;
      expect(r.status(), await r.text()).toBe(200);

      // The target: the copies at its end, under the merge's notice.
      await expect(page).toHaveURL(new RegExp(`${path(target)}/3$`));
      const notice = page.locator('[data-merge-notice]');
      await expect(notice).toHaveText(`2 posts were merged from ${from}.`);
      await expect(notice.getByRole('link')).toHaveAttribute('href', `${path(t)}/2`);
      await expect(page.locator('#post-2 ~ [data-merge-notice] + #post-3')).toHaveCount(1);
      await expect(page.locator('article.post')).toHaveCount(4);
      await expect(page.locator('#post-3 .post-body')).toContainText('Sundays');
      await expect(page.locator('#post-4 .post-body')).toContainText('pins go in the jar');
      await scan(page);

      // The source: its moved notice, linked at the first copy.
      await page.goto(path(t));
      const moved = page.locator('[data-move-notice]');
      await expect(moved).toHaveText(`2 posts were moved to ${into}.`);
      await expect(moved.getByRole('link')).toHaveAttribute('href', `${path(target)}/3`);
      await expect(page.locator('#post-2')).toHaveClass(/\btombstone\b/);
      await expect(page.locator('#post-3')).toHaveClass(/\btombstone\b/);
    });
  });

  // docs/15 "Invariants, added": the source's notice names the target only
  // to a viewer who may read it. alice cannot read moderators.
  test('a member who cannot read the target forum sees that posts were moved and nothing more', async ({
    page,
    browser,
    isMobile,
  }, info) => {
    const words = [`zqhushone${Date.now().toString(36)}`, `zqhushtwo${Date.now().toString(36)}`];
    const t = await start(page.request, title(info, 'Reveal source'), 'A question about the spare keys.');
    await as('carol', browser, info, async (request) => {
      await reply(request, t, `The spare keys are with ${words[0]}.`);
      await reply(request, t, `Or ask ${words[1]} at the desk.`);
    });
    const to = title(info, 'Spare keys zqhidden');
    await browse('mona', browser, info, async (mona) => {
      const bar = await selectPosts(mona, isMobile, t, [2, 3]);
      const n = await split(mona, bar, to, 'moderators');
      await expect(mona).toHaveURL(new RegExp(`${path(n)}$`));
      await expect(mona.locator('#post-1 .post-body')).toContainText(words[0]);
    });

    await page.goto(path(t));
    const notice = page.locator('[data-move-notice]');
    await expect(notice).toHaveText('2 posts were moved to another thread.');
    await expect(notice.locator('a')).toHaveCount(0);
    for (const o of [2, 3]) {
      const tomb = page.locator(`#post-${o}`);
      await expect(tomb).toHaveClass(/\btombstone\b/);
      await expect(tomb.locator('.tombstone-text')).toHaveText('Post moved to another thread');
      await expect(tomb.locator('.tombstone-text a')).toHaveCount(0);
    }
    await expect(page.locator('#post-1 .post-body')).toContainText('spare keys');
    // Nothing from the moved posts, and not the target's title, anywhere in
    // the page or in what the thread's route served alice.
    const html = await page.content();
    for (const w of [...words, 'zqhidden']) expect(html, w).not.toContain(w);
    const served = await (await page.request.get(`/api/v1/threads/${t.id}?page=1`)).text();
    for (const w of [...words, 'zqhidden']) expect(served, w).not.toContain(w);
    await scan(page);
  });
});

test('a member replies to a post as a new thread, edits the pre-filled quote, and the source post shows the continued-in note', async ({
  page,
  browser,
  isMobile,
}, info) => {
  const from = title(info, 'Continue source');
  const t = await as('carol', browser, info, async (request) => {
    const t = await start(request, from, 'Should the reading room stay open late?');
    await reply(request, t, 'Late on Thursdays would suit the night shift. Fridays are quiet anyway.');
    return t;
  });
  await deleteDraft(page.request);
  try {
    await page.goto(path(t));
    const second = page.locator('#post-2');
    await expect(second).toBeVisible();
    if (isMobile) await second.locator('[data-post-more]').click();
    await second.locator('[data-post-action=continue]').click();

    await expect(page).toHaveURL(new RegExp(`/f/${forum}/new\\?from=${t.id}&ordinal=2$`));
    await expect(page.locator('[data-continues]')).toHaveText(`Continuing ${from}, post #2.`);
    const body = page.locator('.composer textarea');
    await expect(body).toHaveValue(
      /^@carol wrote:\n\n> Late on Thursdays would suit the night shift\. Fridays are quiet anyway\.\n\n$/,
    );
    // alice keeps the part she answers, and drops the rest of the quote.
    await body.fill(
      (await body.inputValue()).replace(' Fridays are quiet anyway.', '') + 'Thursdays it is, until nine.',
    );
    const to = title(info, 'Late Thursdays');
    await page.getByRole('textbox', { name: 'Title' }).fill(to);
    const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith(`/forums/${forum}/threads`));
    await (isMobile ? page.locator('[data-shell-primary=post-thread]') : page.locator('[data-post-thread]')).click();
    const r = await answer;
    expect(r.status(), await r.text()).toBe(201);
    const n = ((await r.json()) as { thread: Thread }).thread;

    await expect(page).toHaveURL(new RegExp(`${path(n)}`));
    const note = page.locator('[data-continued-from]');
    await expect(note).toHaveText(`Continued from ${from}.`);
    await expect(note.getByRole('link')).toHaveAttribute('href', `${path(t)}/2`);
    const quote = page.locator('#post-1 .post-body blockquote');
    await expect(quote).toContainText('Late on Thursdays');
    await expect(quote).not.toContainText('Fridays');
    await expect(page.locator('#post-1 .post-body')).toContainText('until nine');

    await page.goto(path(t));
    const continued = page.locator('#post-2 + [data-continued-in]');
    await expect(continued).toHaveText(`Continued in ${to}.`);
    await expect(continued.getByRole('link')).toHaveAttribute('href', path(n));
    await expect(page.locator('[data-continued-in]')).toHaveCount(1);
    await scan(page);
  } finally {
    await deleteDraft(page.request);
  }
});

test('a member starts a thread with tags, a moderator edits them, and a tag chip filters the forum’s list', async ({
  page,
  browser,
  isMobile,
}, info) => {
  await deleteDraft(page.request);
  const name = title(info, 'Tagged');
  let t: Thread;
  try {
    const loaded = page.waitForResponse((r) => new URL(r.url()).pathname === `/api/v1/forums/${forum}/draft` && r.request().method() === 'GET');
    await page.goto(`/f/${forum}/new`);
    await loaded;
    await page.locator('#new-thread-title').fill(name);
    await page.locator('.composer textarea').fill('Which beds get the compost first?');
    await page.getByRole('textbox', { name: 'Tags (optional)' }).fill('Garden, long-read');
    const answer = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith(`/forums/${forum}/threads`));
    await page.locator(isMobile ? '[data-shell=phone-topbar] [data-shell-primary=post-thread]' : '.composer-foot [data-post-thread]').click();
    const r = await answer;
    expect(r.status(), await r.text()).toBe(201);
    t = ((await r.json()) as { thread: Thread }).thread;
    await expect(page).toHaveURL(new RegExp(`${path(t)}\\?page=1$`));
    await expect(page.locator('.thread-head .thread-tags a')).toHaveText(['garden', 'long-read']);
  } finally {
    await deleteDraft(page.request);
  }

  await browse('mona', browser, info, async (mona) => {
    await mona.goto(path(t));
    const chips = mona.locator('.thread-head .thread-tags a');
    await expect(chips).toHaveText(['garden', 'long-read']);
    await action(mona, isMobile, 'tags');
    const panel = mona.getByRole('group', { name: 'Edit tags' });
    const field = panel.getByRole('textbox', { name: 'Tags' });
    await expect(field).toBeFocused();
    await expect(field).toHaveValue('garden long-read');
    await field.fill('garden');
    const saved = mona.waitForResponse((r) => r.url().endsWith('/tags') && r.request().method() === 'PUT');
    await panel.getByRole('button', { name: 'Save', exact: true }).click();
    const s = await saved;
    expect(s.status(), await s.text()).toBe(200);
    await expect(mona.locator('[data-moderate]')).toHaveCount(0);
    await expect(chips).toHaveText(['garden']);
  });

  // alice reads the retagged thread, and its chip filters the list.
  await page.goto(path(t));
  const chip = page.locator('.thread-head .thread-tags a');
  await expect(chip).toHaveText(['garden']);
  await expect(chip).toHaveAttribute('href', `/f/${forum}?tag=garden`);
  await chip.click();
  await expect(page).toHaveURL(new RegExp(`/f/${forum}\\?tag=garden$`));
  const tagged = page.locator('[data-tagged]');
  await expect(tagged).toHaveText(/^Tagged garden\s*Show all threads$/);
  const row = page.locator('[data-thread]', { hasText: name });
  await expect(row).toBeVisible();
  await expect(row.locator('.thread-tags a')).toHaveText(['garden']);
  // Every row listed carries the tag.
  for (const tags of await page.locator('[data-thread]').evaluateAll((els) =>
    els.map((e) => [...e.querySelectorAll('.thread-tags a')].map((a) => a.textContent)),
  ))
    expect(tags).toContain('garden');
  const all = tagged.getByRole('link', { name: 'Show all threads' });
  await expect(all).toHaveAttribute('href', `/f/${forum}`);
  await scan(page);
  await all.click();
  await expect(page).toHaveURL(new RegExp(`/f/${forum}$`));
  await expect(tagged).toHaveCount(0);
});

// docs/15: an archived thread is read-only to everyone, admins included,
// and v1 has no unarchive. Reply as a new thread stays offered, since it
// writes only the new thread's log (docs/06 "Moderator flow for drift").
test('an admin archives a thread with a reason, and afterwards nobody is offered a composer or a control, and its row carries the Archived flag', async ({
  page,
  browser,
  isMobile,
}, info) => {
  const name = title(info, 'Archive');
  // alice's thread, with carol's reply, and a reply alice deleted and could
  // restore, so each control the archive withdraws had a post to stand on.
  const t = await start(page.request, name, 'The old suggestion box can go.');
  await as('carol', browser, info, (request) => reply(request, t, 'Agreed, nobody has used it since spring.'));
  await reply(page.request, t, 'A reply alice takes back.');
  const del = await page.request.delete(`/api/v1/threads/${t.id}/posts/3`);
  expect(del.status(), await del.text()).toBe(200);

  // readOnly checks the archived page as one viewer sees it: the notice, no
  // composer and no reply bar, no control on a post but marking unread and
  // replying as a new thread, and no thread action but reading, watching
  // and copying the link.
  const readOnly = async (p: Page, who: string) => {
    await p.goto(`${path(t)}?page=1`);
    await expect(p.locator('[data-thread-state=archived]'), who).toHaveText('This thread is archived.');
    await expect(p.locator('#post-3'), who).toBeVisible();
    await expect(p.locator('[data-composer]'), who).toHaveCount(0);
    await expect(p.locator('[data-shell=replybar]'), who).toHaveCount(0);
    await expect(p.locator('.replybar-open'), who).toHaveCount(0);
    await expect(p.locator('[data-select-post]'), who).toHaveCount(0);
    const posts = await p
      .locator('[data-post-action]')
      .evaluateAll((els) => [...new Set(els.map((e) => e.getAttribute('data-post-action')!))].sort());
    expect(posts, `${who}'s post controls`).toEqual(['continue', 'unread']);
    const actions = await offered(p, isMobile);
    expect(actions, `${who}'s thread actions`).toEqual(expect.arrayContaining(['mark-unread', 'watch']));
    expect(
      actions.filter((a) => !['first-unread', 'mark-unread', 'watch', 'copy-link'].includes(a)),
      `${who}'s thread actions`,
    ).toEqual([]);
  };

  await browse('ada', browser, info, async (ada) => {
    await ada.goto(`${path(t)}?page=1`);
    await expect(ada.getByRole('heading', { level: 1 })).toHaveText(name);
    await action(ada, isMobile, 'archive');
    const panel = ada.getByRole('group', { name: 'Archive this thread?' });
    await expect(panel.locator('[data-archive-warning]')).toHaveText(
      'Archiving makes this thread read-only for everyone, admins included. It cannot be undone.',
    );
    await panel.getByRole('textbox', { name: 'Reason (optional)' }).fill('The box is gone; nothing more to add.');
    const answer = ada.waitForResponse((r) => r.url().endsWith('/archive') && r.request().method() === 'POST');
    await panel.getByRole('button', { name: 'Archive', exact: true }).click();
    const r = await answer;
    expect(r.status(), await r.text()).toBe(200);
    await expect(ada.locator('[data-moderate]')).toHaveCount(0);
    await expect(ada.locator('[data-thread-state=archived]')).toHaveText('This thread is archived.');
    await readOnly(ada, 'ada');
  });
  await browse('mona', browser, info, async (mona) => {
    await readOnly(mona, 'mona');
    await scan(mona);
  });
  await readOnly(page, 'alice');

  await page.goto(`/f/${forum}`);
  const row = page.locator('[data-thread]', { hasText: name });
  await expect(row.locator('.thread-flag.archived')).toHaveAccessibleName('Archived');
});

test('a post shows who quoted it, and a backlink opens the quoting post', async ({ page, browser, isMobile }, info) => {
  // A tall first post, so the quoting post starts below the fold.
  const opening = Array.from({ length: 40 }, (_, i) => `Paragraph ${i + 1} of the opening post.`).join('\n\n');
  const t = await start(page.request, title(info, 'Quoted'), opening);
  await as('carol', browser, info, async (request) => {
    await reply(request, t, 'A reply that quotes nothing.');
    await reply(request, t, '> [quote=#1]\n> Paragraph 1 of the opening post.\n\nThe first paragraph says it best.');
  });
  // quoted_by is the projection's, which follows the event bus.
  await expect
    .poll(
      async () => {
        const r = await page.request.get(`/api/v1/threads/${t.id}?page=1`);
        return ((await r.json()) as { posts: { ordinal: number; quoted_by?: number[] }[] }).posts[0]?.quoted_by ?? [];
      },
      { timeout: 15_000, message: 'the projection lists #3 as quoting #1' },
    )
    .toEqual([3]);

  await page.goto(`${path(t)}?page=1`);
  const line = page.locator('#post-1 [data-quoted-by]');
  await expect(line).toHaveText('Quoted by #3');
  await expect(page.locator('#post-2 [data-quoted-by]')).toHaveCount(0);
  await expect(page.locator('#post-3')).not.toBeInViewport();
  const before = page.url();
  await line.locator('a[data-reply-to="3"]').click();
  await expect(page.locator('#post-3')).toBeInViewport();
  await expect(page.locator('.post-popover')).toHaveCount(0);
  expect(page.url()).toBe(before);
  if (!isMobile) {
    await scan(page);
    return;
  }

  // At 390 px, with #3 off the page, the backlink opens it in the popover:
  // alice's own posts, through the author lens, leave #3 out.
  await page.goto(`${path(t)}?lens=author:alice`);
  await expect(page.locator('.lens-header')).toBeVisible();
  await expect(page.locator('#post-1')).toBeVisible();
  await expect(page.locator('#post-3')).toHaveCount(0);
  const off = page.locator('#post-1 [data-quoted-by] a[data-reply-to="3"]');
  const at = page.url();
  await off.click();
  const dialog = page.locator('.post-popover[role=dialog]');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('The first paragraph says it best.');
  expect(page.url()).toBe(at);
  await scan(page);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(off).toBeFocused();
});
