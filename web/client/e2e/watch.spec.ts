import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { setReadPosition, stateFile } from './session';

// The watch control (TKT-01M3V6Y87D, docs/04 `watches`), run once per
// project in playwright.config.ts, so it runs at 390 and 1440 px. alice
// mutes "Minutes from the September meeting", where she has unread posts
// (cmd/ketju e2e.go seeds her at #30): from the toolbar at 1440 px and from
// the more menu at 390 px. The forum's list then shows no badge for it.
// She sets it back to Normal the same way and the badge returns. A second
// test does the same for the chatter forum from its list's header. Each
// test makes one axe scan, with the dialog open, and spends no /auth/*: it
// starts from alice's stored session (e2e/session.ts). A level is alice's
// alone, and each test puts hers back whatever happens, since
// thread-list.spec.ts reads her badges.

test.use({ storageState: stateFile('alice') });

const title = 'Minutes from the September meeting';

async function axe(page: Page) {
  const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
  const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
  expect(found, found.join('\n')).toEqual([]);
}

test('a member mutes a thread with unread posts, and its badge goes', async ({ page, isMobile }) => {
  await page.goto('/f/general');
  const row = page.locator('[data-thread]', { hasText: title });
  const href = (await row.locator('.thread-title').getAttribute('href'))!;
  const id = /^\/t\/([^/?#]+)/.exec(href)![1];
  const level = async () => ((await (await page.request.get(`/api/v1/threads/${id}?page=1`)).json()) as { viewer: { watch: string } }).viewer.watch;
  expect(await level()).toBe('');

  // The control is the toolbar's on a desktop and the more menu's on a
  // phone. Either way it is labelled with the level, and focus comes back
  // to what opened the dialog.
  const opener = isMobile ? page.getByRole('button', { name: 'More', exact: true }) : page.locator('[data-shell=toolbar] [data-action=watch]');
  const control = async () => {
    if (isMobile) await opener.click();
    return page.locator(isMobile ? '[data-shell=more-menu] [data-action=watch]' : '[data-shell=toolbar] [data-action=watch]');
  };
  const dialog = page.getByRole('dialog', { name: 'Alerts for this thread' });
  const set = async (choice: string, label: string) => {
    await (await control()).click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole('radio', { name: choice }).check();
    const answer = page.waitForResponse((r) => r.url().endsWith(`/threads/${id}/watch`) && r.request().method() === 'POST');
    await dialog.getByRole('button', { name: 'Save' }).click();
    expect((await answer).status()).toBe(200);
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();
    const shown = await control();
    await expect(shown).toHaveText(label);
    if (isMobile) await page.keyboard.press('Escape');
  };
  // The list again, with alice back at #30: reading the thread moves her on.
  const list = async () => {
    await setReadPosition(page.request, href, 30);
    await page.goto('/f/general');
    await expect(row).toBeVisible();
  };

  try {
    await setReadPosition(page.request, href, 30);
    await page.goto(href);
    await expect(page.locator('.thread-head h1')).toHaveText(title);
    expect(await (await control()).textContent()).toBe('Watch');
    if (isMobile) await page.keyboard.press('Escape');

    // The dialog, open on the thread's own level, is scanned once.
    await (await control()).click();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('radio', { name: /^Normal/ })).toBeChecked();
    await axe(page);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(opener).toBeFocused();

    await set('Mute', 'Muted');
    expect(await level()).toBe('mute');
    await list();
    await expect(row.locator('[data-unread]')).toHaveCount(0);

    await page.goto(href);
    await set('Normal', 'Watch');
    expect(await level()).toBe('');
    await list();
    await expect(row.locator('[data-unread]')).toBeVisible();
  } finally {
    await page.request.post(`/api/v1/threads/${id}/watch`, { data: { level: '' } });
    await setReadPosition(page.request, href, 30);
  }
});

// The forum's control is in its thread list's header at both widths. alice
// has read none of chatter's one-post threads, so each carries a badge
// until she mutes the forum.
test('a member mutes a forum from its header, and its badges go', async ({ page }) => {
  const level = async () => ((await (await page.request.get('/api/v1/forums/chatter/threads')).json()) as { watch: string }).watch;
  expect(await level()).toBe('');
  const control = page.locator('[data-watch-forum]');
  const dialog = page.getByRole('dialog', { name: 'Alerts for Chatter' });
  const badges = page.locator('[data-thread] [data-unread]');
  const set = async (choice: string, label: string) => {
    await control.click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole('radio', { name: choice }).check();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(control).toBeFocused();
    await expect(control).toHaveText(label);
  };
  try {
    await page.goto('/f/chatter');
    await expect(control).toHaveText('Watch');
    await expect(badges.first()).toBeVisible();

    await control.click();
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('not its subforums');
    await axe(page);
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);

    await set('Mute', 'Muted');
    expect(await level()).toBe('mute');
    await expect(badges).toHaveCount(0);
    await page.reload();
    await expect(control).toHaveText('Muted');
    await expect(page.locator('[data-thread]').first()).toBeVisible();
    await expect(badges).toHaveCount(0);

    await set('Normal', 'Watch');
    expect(await level()).toBe('');
    await expect(badges.first()).toBeVisible();
  } finally {
    await page.request.post('/api/v1/forums/chatter/watch', { data: { level: '' } });
  }
});
