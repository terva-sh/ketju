import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { dirname, join } from 'node:path';

// signIn goes through the real flow: /auth/fake/start, the fake provider's
// sign-in page, and the callback that sets the session. It starts at the
// provider's start rather than at the client's Sign in and the login page
// (sign-in.spec.ts covers those), because the login page would spend one
// more of the burst /auth/* allows (docs/08).
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
//
// The run's /auth/* budget (TKT-01M3S1TVPD). The e2e server listens on
// loopback, so every browser in the run is one address to the limiter: a
// burst of 10, then one token every 6 s (authRate in
// internal/web/ratelimit.go, docs/08). The limiter never takes the bucket
// below 0, and one worker runs one test at a time, so each spend holds by
// itself, at any speed:
//
//   spend                       cost  what makes it hold
//   setup                          8  runs first, against a fresh server,
//                                     so from a full bucket of 10
//   apply.spec.ts, per project     2  paceAuth(2) first
//   phase2-exit.spec.ts, per       5  waits out each 429 through through()
//     project
//   sign-in.spec.ts, per project   1  paceAuth(1) first
//   link-sign-in.spec.ts, per      1  paceAuth(1) first
//     project
//   sign-out.spec.ts, per project  3  paceAuth(3) first: its own sign-in
//                                     (2) and the sign-out
//   smoke.spec.ts, per project     0  starts from alice's stored session
//
// The rule for a new spec: one that spends /auth/* either calls paceAuth
// with its count first, or waits out a 429 through through(). Add its row
// here.
export function stateFile(who: string): string {
  return join(dirname(process.env.KETJU_E2E_DIR!), `${who}.state.json`);
}

// authEveryMs is how often the /auth/* limiter refills one token: authRate
// in internal/web/ratelimit.go, and docs/08 "Rate limits".
const authEveryMs = 6_000;

// paceAuth waits for the /auth/* limiter to refill n tokens, n x 6 s and
// half a second over, before a spec spends n requests. It runs at the start
// of the test, before any request, whatever came before, so it is pacing:
// it never reads, waits on, or retries after a 429. The test's timeout
// grows by the wait, so the pacing does not eat the time the test has for
// its own steps.
export async function paceAuth(n: number) {
  const info = test.info();
  expect(info.config.workers, 'one worker: the /auth/* budget in e2e/session.ts assumes one spend at a time').toBe(1);
  expect(info.config.fullyParallel, 'no parallel tests: the /auth/* budget in e2e/session.ts assumes one spend at a time').toBe(false);
  const ms = n * authEveryMs + 500;
  info.annotations.push({ type: 'paced', description: `${n} /auth/* requests: waited ${ms} ms` });
  if (info.timeout > 0) info.setTimeout(info.timeout + ms);
  await new Promise((resolve) => setTimeout(resolve, ms));
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
