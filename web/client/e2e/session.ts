import { expect, type APIRequestContext, type Page } from '@playwright/test';
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

const threadId = (path: string) => {
  const id = /^\/t\/([^/?#]+)/.exec(path)?.[1];
  if (!id) throw new Error(`not a thread address: ${path}`);
  return id;
};

// setReadPosition puts the reader signed in on request at ordinal in the
// thread at path (/t/{id}/...), whatever it was: a DELETE lowers a position
// and a PUT raises one (docs/07 "Read position"), so the two together set it.
// Any thread page a signed-in spec opens moves its reader's position
// (TKT-01M29CGBWN), so a test that depends on one sets it first rather than
// trusting that no earlier test read there.
export async function setReadPosition(request: APIRequestContext, path: string, ordinal: number) {
  const url = `/api/v1/threads/${threadId(path)}/read`;
  const down = await request.delete(url, ordinal > 0 ? { data: { ordinal } } : {});
  expect(down.status(), await down.text()).toBe(200);
  let at = ((await down.json()) as { last_read_ordinal: number }).last_read_ordinal;
  if (at < ordinal) {
    const up = await request.put(url, { data: { ordinal } });
    expect(up.status(), await up.text()).toBe(200);
    at = ((await up.json()) as { last_read_ordinal: number }).last_read_ordinal;
  }
  expect(at).toBe(ordinal);
}

// readPosition is the stored position of the reader signed in on request in
// the thread at path, as the thread page reports it.
export async function readPosition(request: APIRequestContext, path: string): Promise<number> {
  const r = await request.get(`/api/v1/threads/${threadId(path)}?page=1`);
  expect(r.status()).toBe(200);
  return ((await r.json()) as { viewer: { last_read_ordinal: number } }).viewer.last_read_ordinal;
}
