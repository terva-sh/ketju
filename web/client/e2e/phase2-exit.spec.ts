import AxeBuilder from '@axe-core/playwright';
import { expect, test, type APIRequestContext, type Page, type Response } from '@playwright/test';
import { stateFile } from './session';

// Phase 2's exit (TKT-01M3NTSCMN, docs/09 and docs/13), at 390 px and
// 1440 px: "a club can open registration to strangers safely". A stranger
// signs in through the fake provider, applies, and waits on the status
// page; the seeded moderator, mona, approves them from /admin/queue; and
// the stranger signs in again as the member the approval made, with the
// decision waiting in their notifications. The form, the status page and
// the queue are each checked for horizontal overflow and scanned with axe
// (WCAG 2.1 A and AA, as e2e/a11y.spec.ts does) in the light theme and the
// dark.
//
// Approval retires the stranger's pre-user session, so their status page,
// reloaded, is signed out and points at signing in, rather than saying
// approved (maintainer decision 2026-09-29, TKT-01M3Q2V25E).
//
// Each project has its own stranger (cmd/ketju e2e.go seedStrangers),
// because an approval cannot be taken twice. The fake provider sends them
// no preferred_username or email, so no handle is suggested, and mona
// writes one.
//
// The stranger signs in twice, which costs five /auth/* requests, and the
// setup project has already spent eight of the ten /auth/* allows one
// address in a burst (docs/08; see e2e/session.ts). So each navigation
// through /auth/* here is patient: a 429 is waited out for as long as its
// Retry-After says, as a person told "Try again shortly" would, and the
// page loaded again. Each wait is recorded as an annotation on the test.

const strangers: Record<string, { subject: string; name: string; handle: string }> = {
  phone: { subject: 'stranger-phone', name: 'Stranger Phone', handle: 'newcomer-phone' },
  desktop: { subject: 'stranger-desktop', name: 'Stranger Desktop', handle: 'newcomer-desktop' },
};

async function scan(page: Page, what: string) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `${what}: horizontal overflow`).toBeLessThanOrEqual(0);
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
    const found = violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(found, `${what}, ${colorScheme}`).toEqual([]);
  }
  await page.emulateMedia({ colorScheme: 'light' });
}

// through makes a navigation happen with act and answers where it landed,
// past any redirect. A landing the per-address limiter refused (429) is
// waited out as Retry-After says and loaded again; the limiter answers
// before any handler runs, so loading again repeats the same request.
async function through(page: Page, act: () => Promise<unknown>): Promise<Response> {
  const landing = page.waitForResponse(
    (r) => r.request().isNavigationRequest() && r.frame() === page.mainFrame() && (r.status() < 300 || r.status() >= 400),
  );
  await act();
  let resp: Response | null = await landing;
  for (let tries = 0; resp?.status() === 429 && tries < 5; tries++) {
    const wait = Number(resp.headers()['retry-after'] ?? '6');
    test.info().annotations.push({ type: 'rate-limited', description: `${new URL(resp.url()).pathname}: waited ${wait} s` });
    await page.waitForTimeout(wait * 1000);
    resp = await page.reload();
  }
  expect(resp, 'a landing').not.toBeNull();
  expect(resp!.status(), `${resp!.url()} answered`).toBeLessThan(400);
  return resp!;
}

// applicationOf is the id of the pending application answered with name,
// as the moderator signed in on request finds it: the queue item carries
// no name, and this stranger's provider sends no email to tell it by.
async function applicationOf(request: APIRequestContext, name: string): Promise<number> {
  const list = await request.get('/api/v1/admin/applications?status=pending');
  expect(list.status(), await list.text()).toBe(200);
  const ids: number[] = [];
  for (const row of ((await list.json()) as { applications: { id: number }[] }).applications) {
    const detail = await request.get(`/api/v1/admin/applications/${row.id}`);
    expect(detail.status()).toBe(200);
    if (((await detail.json()) as { answers: { name?: string } }).answers.name === name) ids.push(row.id);
  }
  expect(ids, `pending applications answered as ${name}`).toHaveLength(1);
  return ids[0];
}

test('a stranger applies, a moderator approves them from the queue, and they sign in as a member', async ({
  page,
  browser,
}, info) => {
  const who = strangers[info.project.name];
  expect(who, `a stranger for the ${info.project.name} project`).toBeDefined();
  // Room for the limiter's waits (through).
  test.slow();

  await test.step('the stranger signs in and lands on the form', async () => {
    await through(page, () => page.goto('/auth/fake/start'));
    await through(page, () => page.getByRole('link', { name: who.subject, exact: true }).click());
    await page.waitForURL((u) => u.pathname === '/apply');
    await expect(page.locator('[data-shell="applicant-bar"]')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Apply to join');
    const me = (await (await page.request.get('/api/v1/me')).json()) as { user: unknown; pre_user: { provider: string } | null };
    expect(me.user).toBeNull();
    expect(me.pre_user?.provider).toBe('fake');
    await scan(page, 'the form');
  });

  await test.step('they apply, and wait on the status page', async () => {
    await page.getByLabel('Name (required)').fill(who.name);
    await page.getByLabel('Who invited you or how did you find us? (required)').fill('Mona invited me.');
    await page.getByLabel('I have read the guidelines (required)').check();
    await page.getByRole('button', { name: 'Send application' }).click();
    await page.waitForURL((u) => u.pathname === '/apply/status');
    await expect(page.locator('[data-status="pending"]')).toContainText('Your application is being reviewed.');
    await scan(page, 'the status page, pending');
  });

  // mona, in a browser of her own at the same width.
  const moderation = await browser.newContext({
    baseURL: info.project.use.baseURL,
    viewport: info.project.use.viewport,
    isMobile: info.project.use.isMobile,
    hasTouch: info.project.use.hasTouch,
    storageState: stateFile('mona'),
  });
  try {
    const queue = await moderation.newPage();
    await test.step('the moderator approves them from the queue', async () => {
      const id = await applicationOf(moderation.request, who.name);
      await queue.goto('/admin/queue');
      await expect(queue.getByRole('heading', { level: 1 })).toHaveText('Moderation queue');
      const item = queue.locator(`[data-queue-item="application:${id}"]`);
      await expect(item).toBeVisible();
      await item.getByRole('button', { name: 'Review' }).click();
      const approve = item.locator('[data-decision] section').filter({ has: queue.getByRole('heading', { name: 'Approve' }) });
      await expect(approve).toBeVisible();
      await approve.getByLabel('Handle').fill(who.handle);
      await scan(queue, 'the queue, with the application open');

      await approve.getByRole('button', { name: 'Approve' }).click();
      await approve.getByRole('button', { name: 'Approve' }).click();
      await expect(queue.locator('[data-queue-notice]')).toHaveText(`Approved as @${who.handle}. They can sign in now.`);
      await expect(item).toHaveCount(0);
      await scan(queue, 'the queue, after approving');
    });
  } finally {
    await moderation.close();
  }

  await test.step("the stranger's status page is signed out, and points at signing in", async () => {
    await page.reload();
    const panel = page.locator('[data-status="signed-out"]');
    await expect(panel).toContainText('If you applied to join, sign in to see your decision.');
    const me = (await (await page.request.get('/api/v1/me')).json()) as { user: unknown; pre_user: unknown };
    expect(me.user).toBeNull();
    expect(me.pre_user).toBeNull();
    await expect(panel.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/auth/signin?return_to=%2F');
    await scan(page, 'the status page, signed out after approval');
  });

  await test.step('they sign in as a member, and the decision is waiting', async () => {
    // The login page, with one provider, goes straight to its start.
    await through(page, () => page.locator('[data-status="signed-out"]').getByRole('link', { name: 'Sign in' }).click());
    await through(page, () => page.getByRole('link', { name: who.subject, exact: true }).click());
    await page.waitForURL((u) => u.pathname === '/');
    await expect
      .poll(async () => ((await (await page.request.get('/api/v1/me')).json()) as { user?: { handle: string } }).user?.handle)
      .toBe(who.handle);
    const me = (await (await page.request.get('/api/v1/me')).json()) as {
      user: { role: string };
      pre_user: unknown;
      notifications: number;
    };
    expect(me.user.role).toBe('member');
    expect(me.pre_user).toBeNull();
    // The approval's notification (TKT-01M29CP2N0), on the Alerts badge.
    expect(me.notifications).toBe(1);
    await expect(page.locator('a[data-nav="alerts"]:visible [data-badge]')).toContainText('1');
    await expect(page.locator('[data-screen="forum-index"] [data-forum]').first()).toBeVisible();
  });
});
