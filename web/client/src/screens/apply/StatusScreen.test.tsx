import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApplicationStatus, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, stubServer, unmount, type StubServer } from '../../testing';
import { notAccepting } from './ApplyScreen';
import { when } from './when';
import preUser from '../../../../../internal/dto/testdata/golden/me.pre_user.json';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import none from '../../../../../internal/dto/testdata/golden/apply-status-none.pre_user.json';
import pending from '../../../../../internal/dto/testdata/golden/apply-status-pending.pre_user.json';
import approved from '../../../../../internal/dto/testdata/golden/apply-status-approved.pre_user.json';
import withdrawn from '../../../../../internal/dto/testdata/golden/apply-status-withdrawn.pre_user.json';

// /apply/status (TKT-01M29CP2NG), against a stubbed server answering with
// the goldens. There is no denied golden; it is the withdrawn one decided
// the other way.

const denied: ApplicationStatus = { ...(withdrawn as ApplicationStatus), status: 'denied' };

let saved: Me;
let server: StubServer;
let status: unknown;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(preUser) as Me;
  status = pending;
  server = stubServer({ 'GET /api/v1/apply/status': () => ({ status: 200, body: status }) });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

async function until(ready: () => boolean) {
  for (let i = 0; i < 200 && !ready(); i++) await act(() => new Promise((r) => setTimeout(r, 5)));
}

async function open(body: unknown): Promise<HTMLElement> {
  status = body;
  const root = await renderAt('/apply/status');
  await until(() => !root.querySelector('.apply-loading'));
  return root;
}

const panel = (root: HTMLElement) => root.querySelector<HTMLElement>('[data-status]')!;
const links = (el: HTMLElement) => [...el.querySelectorAll('a')].map((a) => [a.textContent, a.getAttribute('href')]);
const button = (root: HTMLElement, text: string) =>
  [...root.querySelectorAll<HTMLButtonElement>('main button')].find((b) => b.textContent === text);

describe('the status screen', () => {
  it('offers the form to someone who has not applied', async () => {
    const root = await open(none);
    expect(screenOf(root)).toBe('apply-status');
    expect(panel(root).dataset.status).toBe('none');
    expect(panel(root).textContent).toContain('You have not applied yet.');
    expect(links(panel(root))).toEqual([['Apply to join', '/apply']]);
  });

  it('says a pending application is being reviewed, and offers to withdraw it', async () => {
    const root = await open(pending);
    expect(panel(root).dataset.status).toBe('pending');
    expect(panel(root).textContent).toContain('Your application is being reviewed.');
    expect(panel(root).textContent).toContain(`Sent ${when(1757600000000)}.`);
    expect(button(root, 'Withdraw application')).toBeDefined();
  });

  it('asks before withdrawing, then posts and shows the answer', async () => {
    server.routes['POST /api/v1/apply/withdraw'] = () => ({ status: 200, body: withdrawn });
    const root = await open(pending);
    await act(async () => button(root, 'Withdraw application')!.click());
    const confirm = root.querySelector<HTMLElement>('[role="group"]')!;
    expect(confirm.getAttribute('aria-label')).toBe('Withdraw application');
    expect(confirm.textContent).toContain('Withdraw your application?');
    expect(server.sent.filter((r) => r.method === 'POST')).toEqual([]);

    // Keep it backs out, and sends nothing.
    await act(async () => button(root, 'Keep it')!.click());
    expect(root.querySelector('[role="group"]')).toBeNull();
    expect(server.sent.filter((r) => r.method === 'POST')).toEqual([]);

    await act(async () => button(root, 'Withdraw application')!.click());
    await act(async () => button(root, 'Withdraw')!.click());
    await until(() => panel(root).dataset.status === 'withdrawn');
    expect(server.sent.filter((r) => r.method === 'POST')).toEqual([{ method: 'POST', url: '/api/v1/apply/withdraw', body: {} }]);
    expect(panel(root).textContent).toContain('You withdrew your application.');
  });

  it('reloads the status when there is nothing left to withdraw', async () => {
    server.routes['POST /api/v1/apply/withdraw'] = () => {
      status = denied;
      return problemAnswer('conflict', 409);
    };
    const root = await open(pending);
    await act(async () => button(root, 'Withdraw application')!.click());
    await act(async () => button(root, 'Withdraw')!.click());
    await until(() => panel(root)?.dataset.status === 'denied');
    expect(panel(root).textContent).toContain('Your application was not approved.');
  });

  it('keeps the confirmation open with the reason when a withdrawal fails', async () => {
    server.routes['POST /api/v1/apply/withdraw'] = () => problemAnswer('rate-limited', 429, {}, { 'Retry-After': '20' });
    const root = await open(pending);
    await act(async () => button(root, 'Withdraw application')!.click());
    await act(async () => button(root, 'Withdraw')!.click());
    await until(() => !!root.querySelector('.apply-failed'));
    expect(root.querySelector('.apply-failed')!.textContent).toBe('Try again in 20 seconds.');
    expect(button(root, 'Withdraw')!.disabled).toBe(false);
  });

  it('draws approved with its sign-in link, though a retired session never reaches it', async () => {
    const root = await open(approved);
    expect(panel(root).dataset.status).toBe('approved');
    expect(links(panel(root))).toEqual([['Sign in', '/auth/github/start?return_to=/']]);
  });

  it.each([
    ['denied', denied, 'Your application was not approved.', 'Decided'],
    ['withdrawn', withdrawn, 'You withdrew your application.', 'Withdrawn'],
  ] as const)('says when a %s applicant may apply again', async (name, body, lead, moment) => {
    const root = await open(body);
    expect(panel(root).dataset.status).toBe(name);
    expect(panel(root).textContent).toContain(lead);
    expect(panel(root).textContent).toContain(`${moment} ${when(1757603600000)}.`);
    expect(panel(root).textContent).toContain(`You can apply again after ${when(1757686400000)}.`);
    expect(links(panel(root))).toEqual([]);
  });

  it('offers to apply again once the server says so', async () => {
    const root = await open({ ...denied, can_apply: true, can_apply_at: null });
    expect(links(panel(root))).toEqual([['Apply again', '/apply']]);
  });

  it('says applications are closed when the server allows none and names no time', async () => {
    const root = await open({ ...denied, can_apply: false, can_apply_at: null });
    expect(panel(root).textContent).toContain(notAccepting);
  });

  it('sends a signed-out reader to sign in, which is where an approved applicant lands', async () => {
    server.routes['GET /api/v1/apply/status'] = () => problemAnswer('unauthenticated', 401);
    const root = await open(null);
    expect(panel(root).dataset.status).toBe('signed-out');
    expect(panel(root).textContent).toContain('sign in to see your decision');
    expect(links(panel(root))).toEqual([['Sign in', '/auth/signin?return_to=%2F']]);
  });

  it('tells a member there is nothing to show', async () => {
    viewer.value = structuredClone(memberMe) as Me;
    server.routes['GET /api/v1/apply/status'] = () => problemAnswer('forbidden', 403);
    const root = await open(null);
    expect(root.querySelector('main')!.textContent).toContain('already a member');
  });
});
