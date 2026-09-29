import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

// The application screens (TKT-01M29CP2NG), at 390 px and 1440 px: an
// approval-mode applicant signs in through the fake provider, lands on the
// status page, opens the seeded registration form, sends it, and is back on
// the status page as pending. Each screen is checked for horizontal overflow
// and scanned with axe (WCAG 2.1 A and AA, as e2e/a11y.spec.ts does) in the
// light theme and the dark.
//
// Each project signs in as its own applicant (cmd/ketju e2e.go
// seedApplicants), because an identity may submit one application a day.
// A sign-in costs two of the ten /auth/* requests allowed in a burst
// (docs/08); see e2e/session.ts.

const applicants: Record<string, string> = { phone: 'dana', desktop: 'erin' };

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

test('an applicant applies and waits on the status page', async ({ page }, info) => {
  const who = applicants[info.project.name];
  expect(who, `an applicant for the ${info.project.name} project`).toBeDefined();

  await test.step('sign in, and land on the form', async () => {
    // A first sign-in goes to the form; one after an application goes to
    // the status page (docs/05 "mode = approval" step 1).
    await page.goto('/auth/fake/start');
    await page.getByRole('link', { name: who, exact: true }).click();
    await page.waitForURL((u) => u.pathname === '/apply');
    await expect(page.locator('[data-shell="applicant-bar"]')).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Apply to join');
  });

  await test.step('a member route replaces itself with the status page', async () => {
    // Routed in the page, as a link would be: the server would answer a
    // full load of it with a 303 itself.
    await page.evaluate(() => {
      history.pushState(null, '', '/unread');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    await page.waitForURL((u) => u.pathname === '/apply/status');
    await expect(page.locator('[data-status="none"]')).toContainText('You have not applied yet.');
    await expect(page.locator('[data-shell="tabbar"], [data-shell="topbar"]')).toHaveCount(0);
    await scan(page, 'the status page, not applied');
  });

  await test.step('open the seeded form', async () => {
    await page.getByRole('link', { name: 'Apply to join' }).click();
    await page.waitForURL((u) => u.pathname === '/apply');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Apply to join');
    // The email is locked to the provider's claim, which the fake provider
    // does not send: shown empty, read-only, and labelled.
    await expect(page.getByLabel('Email')).toHaveAttribute('readonly', '');
    await expect(page.locator('[data-field="email"] [data-lock]')).toHaveText('From your fake account');
    await scan(page, 'the form');
  });

  await test.step('a refusal marks the field', async () => {
    // Sent empty: the fake provider sends no name claim, so Name is the
    // first field refused, and focus goes there.
    await page.getByRole('button', { name: 'Send application' }).click();
    const name = page.getByLabel('Name (required)');
    await expect(name).toHaveAttribute('aria-invalid', 'true');
    await expect(name).toBeFocused();
    await expect(page.getByLabel('Who invited you or how did you find us? (required)')).toHaveAttribute('aria-invalid', 'true');
    await expect(page.locator('[data-field="who"] .form-error')).toHaveText('This is required.');
    await scan(page, 'the form with its errors');
  });

  await test.step('send it, and wait on the status page', async () => {
    await page.getByLabel('Name (required)').fill(`${who} Applicant`);
    await page.getByLabel('Who invited you or how did you find us? (required)').fill('Alice invited me.');
    await page.getByLabel('I have read the guidelines (required)').check();
    await page.getByRole('button', { name: 'Send application' }).click();
    await page.waitForURL((u) => u.pathname === '/apply/status');
    await expect(page.locator('[data-status="pending"]')).toContainText('Your application is being reviewed.');
    await expect(page.getByRole('button', { name: 'Withdraw application' })).toBeVisible();
    await scan(page, 'the status page, pending');
  });
});

// Approval retires the pre-user session, so a decided applicant returns
// signed out, and the signed-out shell says where the decision is
// (TKT-01M3Q2V25E).
test('the signed-out shell points an applicant at signing in', async ({ page }) => {
  await page.goto('/');
  const hint = page.locator('[data-shell="signin-hint"]');
  await expect(hint).toHaveText('Applied? Sign in to see your decision');
  await expect(hint.getByRole('link')).toHaveAttribute('href', '/auth/signin?return_to=%2F');
  await expect(page.locator('[data-screen="forum-index"] [data-forum]').first()).toBeVisible();
  await scan(page, 'the signed-out forum index');
});
