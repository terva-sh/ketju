import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApplicationForm, Me } from '../../api/dto';
import { fieldMessage } from '../../forms/messages';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type StubServer } from '../../testing';
import { notAccepting } from './ApplyScreen';
import preUser from '../../../../../internal/dto/testdata/golden/me.pre_user.json';
import applyForm from '../../../../../internal/dto/testdata/golden/apply-form.pre_user.json';
import pending from '../../../../../internal/dto/testdata/golden/apply-status-pending.pre_user.json';
import none from '../../../../../internal/dto/testdata/golden/apply-status-none.pre_user.json';
import fieldErrorsBody from '../../../../../internal/problem/testdata/field-errors.json';

// /apply for an approval-mode applicant (TKT-01M29CP2NG), the whole app
// rendered against a stubbed server that answers with the goldens.

let saved: Me;
let server: StubServer;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = structuredClone(preUser) as Me;
  server = stubServer({
    'GET /api/v1/apply/form': () => ({ status: 200, body: applyForm }),
    'GET /api/v1/apply/status': () => ({ status: 200, body: pending }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
});

const field = (root: HTMLElement, key: string) => root.querySelector<HTMLElement>(`[data-field="${key}"]`)!;

function type(root: HTMLElement, key: string, text: string) {
  const el = field(root, key).querySelector<HTMLInputElement | HTMLTextAreaElement>('input, textarea')!;
  el.value = text;
  act(() => {
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

// until waits up to a second for ready to hold.
async function until(ready: () => boolean) {
  for (let i = 0; i < 200 && !ready(); i++) await act(() => new Promise((r) => setTimeout(r, 5)));
}

// send presses Send and waits for what follows to settle: the answer, and
// the status page and its own request when it leads there.
async function send(root: HTMLElement) {
  await act(async () => root.querySelector<HTMLButtonElement>('form.form-view button[type="submit"]')!.click());
  await until(
    () =>
      !root.querySelector('[aria-busy], .apply-loading') &&
      (location.pathname !== '/apply/status' || screenOf(root) === 'apply-status'),
  );
  await settle(root);
}

async function filledIn(): Promise<HTMLElement> {
  const root = await renderAt('/apply');
  type(root, 'who', 'Alice invited me');
  act(() => field(root, 'rules').querySelector<HTMLInputElement>('input')!.click());
  return root;
}

const posts = () => server.sent.filter((r) => r.method === 'POST');

describe('the apply screen', () => {
  it('draws the form under the applicant bar', async () => {
    const root = await renderAt('/apply');
    expect(screenOf(root)).toBe('apply');
    expect(root.querySelector('[data-shell="applicant-bar"]')).not.toBeNull();
    expect(root.querySelector('h1')!.textContent).toBe('Apply to join');
    expect(root.querySelector('.form-intro')!.textContent).toContain('Tell us a bit about yourself');
    expect(field(root, 'email').querySelector('[data-lock]')!.textContent).toBe('From your github account');
  });

  it('sends the version and the answers, without the locked field, and lands on the status page', async () => {
    server.routes['POST /api/v1/apply'] = () => ({ status: 201, body: pending });
    const root = await filledIn();
    await send(root);
    expect(posts()).toEqual([
      {
        method: 'POST',
        url: '/api/v1/apply',
        body: { version: 1, answers: { name: 'Carol Applicant', who: 'Alice invited me', rules: true } },
      },
    ]);
    expect(location.pathname).toBe('/apply/status');
    expect(screenOf(root)).toBe('apply-status');
    expect(root.textContent).toContain('Your application is being reviewed.');
  });

  it('goes to the status page when an application is already pending', async () => {
    server.routes['POST /api/v1/apply'] = () => problemAnswer('conflict', 409);
    const root = await filledIn();
    await send(root);
    expect(location.pathname).toBe('/apply/status');
    expect(screenOf(root)).toBe('apply-status');
  });

  it('reloads a form that changed, keeping the answers whose questions remain', async () => {
    server.routes['GET /api/v1/apply/status'] = () => ({ status: 200, body: none });
    server.routes['POST /api/v1/apply'] = () => problemAnswer('conflict', 409);
    const root = await filledIn();
    const v2 = structuredClone(applyForm) as ApplicationForm;
    v2.form.version = 2;
    v2.form.fields = v2.form.fields.filter((f) => f.key !== 'rules');
    v2.form.fields.push({ ...v2.form.fields[0], key: 'city', label: 'City', required: false, from_claim: '' });
    server.routes['GET /api/v1/apply/form'] = () => ({ status: 200, body: v2 });
    await send(root);

    expect(location.pathname).toBe('/apply');
    expect(root.querySelector('.form-message')!.textContent).toContain('The form changed');
    expect(field(root, 'rules')).toBeNull();
    expect(field(root, 'who').querySelector('textarea')!.value).toBe('Alice invited me');
    expect(field(root, 'city').querySelector('input')!.value).toBe('');

    server.routes['POST /api/v1/apply'] = () => ({ status: 201, body: pending });
    await send(root);
    expect(posts().at(-1)!.body).toEqual({ version: 2, answers: { name: 'Carol Applicant', who: 'Alice invited me', city: '' } });
    expect(location.pathname).toBe('/apply/status');
  });

  it('attaches a refusal to its fields and focuses the first', async () => {
    server.routes['POST /api/v1/apply'] = () => ({ status: 422, body: fieldErrorsBody });
    const root = await filledIn();
    await send(root);
    const who = field(root, 'who').querySelector('textarea')!;
    expect(who.getAttribute('aria-invalid')).toBe('true');
    expect(field(root, 'who').querySelector('.form-error')!.textContent).toBe(fieldMessage('required'));
    expect(field(root, 'rules').querySelector('input')!.getAttribute('aria-invalid')).toBe('true');
    expect(document.activeElement).toBe(who);
    expect(location.pathname).toBe('/apply');
  });

  it('says applications are closed on a 403, and gives no reason', async () => {
    server.routes['POST /api/v1/apply'] = () => problemAnswer('forbidden', 403);
    const root = await filledIn();
    await send(root);
    expect(root.querySelector('.form-message')!.textContent).toBe(notAccepting);
  });

  it('says when applying is possible again on a 429', async () => {
    server.routes['POST /api/v1/apply'] = () => problemAnswer('rate-limited', 429, {}, { 'Retry-After': '30' });
    const root = await filledIn();
    await send(root);
    expect(root.querySelector('.form-message')!.textContent).toBe('Your application was not sent. Try again in 30 seconds.');

    server.routes['POST /api/v1/apply'] = () => problemAnswer('rate-limited', 429, {}, { 'Retry-After': '7200' });
    await send(root);
    expect(root.querySelector('.form-message')!.textContent).toMatch(/^Your application was not sent\. You can try again after .+\.$/);
  });

  describe('when the form is refused', () => {
    it('goes to the status page on a 409', async () => {
      server.routes['GET /api/v1/apply/form'] = () => problemAnswer('conflict', 409);
      const root = await renderAt('/apply');
      await settle(root);
      expect(location.pathname).toBe('/apply/status');
      expect(screenOf(root)).toBe('apply-status');
    });

    it('says applications are closed on a 403', async () => {
      server.routes['GET /api/v1/apply/form'] = () => problemAnswer('forbidden', 403);
      const root = await renderAt('/apply');
      expect(root.textContent).toContain(notAccepting);
      expect(root.querySelector('form.form-view')).toBeNull();
    });

    it('says when applying is possible again on a 429', async () => {
      server.routes['GET /api/v1/apply/form'] = () => problemAnswer('rate-limited', 429, {}, { 'Retry-After': '3600' });
      const root = await renderAt('/apply');
      expect(root.textContent).toContain('You applied recently.');
      expect(root.textContent).toContain('You can try again after');
      expect(root.querySelector('a[href="/apply/status"]')).not.toBeNull();
    });
  });
});
