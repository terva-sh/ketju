import { expect, type Page } from '@playwright/test';
import { dirname, join } from 'node:path';

// signIn goes through the real flow: /auth/fake/start, the fake provider's
// sign-in page, and the callback that sets the session. The client has no
// sign-in screen yet, so the flow starts at the server's own start URL.
export async function signIn(page: Page, who: string) {
  await page.goto('/auth/fake/start');
  await page.getByRole('link', { name: who, exact: true }).click();
  await page.waitForURL((u) => u.pathname === '/');
  await expect
    .poll(async () => ((await (await page.request.get('/api/v1/me')).json()) as { user?: { handle: string } }).user?.handle)
    .toBe(who);
}

// stateFile is where the setup project (session.setup.ts) keeps who's
// signed-in session, beside the run's data. A spec that needs a signed-in
// reader but is not testing sign-in starts from it, because /auth/* allows
// a burst of ten requests (docs/08), and a sign-in per spec would spend it.
export function stateFile(who: string): string {
  return join(dirname(process.env.KETJU_E2E_DIR!), `${who}.state.json`);
}
