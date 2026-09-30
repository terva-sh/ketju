import { expect, test } from '@playwright/test';
import { paceAuth } from './session';

// Sign in from the client (TKT-01M3MFS6KM): a signed-out reader's Sign in,
// the top bar's at 1440 px and the Me tab at 390 px, is a full page load of
// the server's login page, which, with the fake provider the only one
// configured, redirects straight to its start, carrying the page the reader
// left as return_to.
//
// The login page's answer is read here and the browser stops there, rather
// than following it to the provider. /auth/* allows a burst of ten requests
// per address (docs/08), which the setup project, apply.spec.ts and
// phase2-exit.spec.ts also draw on, and a whole sign-in costs three; this
// way the spec costs one. The test first waits for the limiter to refill
// that one (paceAuth), so it holds however little the specs before it left;
// see the budget in e2e/session.ts.
// (page.route cannot stop the browser at the start instead: it never sees
// a request that a redirect made.) The start, the provider and the callback
// are the setup project's sign-in, and internal/web/auth's tests follow
// return_to from the login page through the callback.
test('a signed-out reader reaches the provider from Sign in', async ({ page }) => {
  await paceAuth(1);
  const from = '/f/general?x=1';
  let answer: { status: number; location: string | undefined; navigation: boolean } | undefined;
  await page.route(/\/auth\/signin(\?|$)/, async (route) => {
    const resp = await route.fetch({ maxRedirects: 0 });
    answer = { status: resp.status(), location: resp.headers()['location'], navigation: route.request().isNavigationRequest() };
    await route.fulfill({ status: 200, contentType: 'text/html', body: '<title>signin</title>' });
  });

  await page.goto(from);
  const me = page.locator('[data-nav=me]');
  await expect(me).toHaveAttribute('href', /^\/auth\/signin\?/);
  await me.click();

  // A full page load, not a client route: the browser's address is the
  // server's page.
  await page.waitForURL((u) => u.pathname === '/auth/signin');
  expect(new URL(page.url()).searchParams.get('return_to')).toBe(from);
  expect(answer?.navigation).toBe(true);

  // One provider: straight to its start, return_to carried.
  expect(answer?.status).toBe(302);
  const start = new URL(answer!.location!, page.url());
  expect(start.pathname).toBe('/auth/fake/start');
  expect(start.searchParams.get('return_to')).toBe(from);
});
