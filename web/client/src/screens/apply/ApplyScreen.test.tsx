import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApplicationForm, FormField, Me } from '../../api/dto';
import type { Value } from '../../forms/fields';
import { fieldMessage } from '../../forms/messages';
import { viewer } from '../../state/viewer';
import { problemAnswer, renderAt, screenOf, settle, stubServer, unmount, type StubServer } from '../../testing';
import { carried, notAccepting } from './ApplyScreen';
import preUser from '../../../../../internal/dto/testdata/golden/me.pre_user.json';
import memberMe from '../../../../../internal/dto/testdata/golden/me.member.json';
import applyForm from '../../../../../internal/dto/testdata/golden/apply-form.pre_user.json';
import pending from '../../../../../internal/dto/testdata/golden/apply-status-pending.pre_user.json';
import none from '../../../../../internal/dto/testdata/golden/apply-status-none.pre_user.json';
import approved from '../../../../../internal/dto/testdata/golden/apply-status-approved.pre_user.json';
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

  // Review 1453: pending is read from the status, not from can_withdraw, and
  // any status that leaves the applicant unable to apply is the status
  // page's to explain, never a form change.
  it.each([
    ['pending, with withdrawing not offered', { ...pending, can_withdraw: false }],
    ['approved', approved],
    ['denied within the day', { ...none, status: 'denied', can_apply: false, can_apply_at: 1757686400000 }],
  ])('goes to the status page on a 409 when the status is %s', async (_, status) => {
    server.routes['GET /api/v1/apply/status'] = () => ({ status: 200, body: status });
    server.routes['POST /api/v1/apply'] = () => problemAnswer('conflict', 409);
    const root = await filledIn();
    await send(root);
    expect(location.pathname).toBe('/apply/status');
    expect(server.sent.filter((r) => r.url === '/api/v1/apply/form')).toHaveLength(1);
  });

  it('reloads a form that changed, keeping the answers whose questions remain', async () => {
    server.routes['GET /api/v1/apply/status'] = () => ({ status: 200, body: none });
    server.routes['POST /api/v1/apply'] = () => problemAnswer('conflict', 409);
    const root = await filledIn();
    const v2 = structuredClone(applyForm) as ApplicationForm;
    v2.form.version = 2;
    // rules is a line of text now, not a box: the ticked box is not carried
    // into it.
    const text = { ...v2.form.fields[0], required: false, from_claim: '' };
    v2.form.fields = v2.form.fields.map((f) => (f.key === 'rules' ? { ...text, key: 'rules', label: 'A rule you would add' } : f));
    v2.form.fields.push({ ...text, key: 'city', label: 'City' });
    server.routes['GET /api/v1/apply/form'] = () => ({ status: 200, body: v2 });
    await send(root);

    expect(location.pathname).toBe('/apply');
    expect(root.querySelector('.form-message')!.textContent).toContain('The form changed');
    expect(field(root, 'who').querySelector('textarea')!.value).toBe('Alice invited me');
    expect(field(root, 'rules').querySelector('input')!.value).toBe('');
    expect(field(root, 'city').querySelector('input')!.value).toBe('');

    server.routes['POST /api/v1/apply'] = () => ({ status: 201, body: pending });
    await send(root);
    expect(posts().at(-1)!.body).toEqual({
      version: 2,
      answers: { name: 'Carol Applicant', who: 'Alice invited me', rules: '', city: '' },
    });
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

  // Review 1453: an answer outlives a form change only where it still fits.
  describe('an answer carried into a changed form', () => {
    const f = (over: Partial<FormField> & Pick<FormField, 'type'>): FormField => ({
      key: 'q',
      label: 'Q',
      required: false,
      max: 0,
      options: [],
      multiple: false,
      locked: false,
      from_claim: '',
      html: '',
      ...over,
    });
    const from = (field: FormField, value: Value) => ({
      form: { key: 'registration', version: 1, title: 'T', intro_html: '', fields: [field] },
      values: { q: value },
    });

    it.each([
      ['a line kept as a line', f({ type: 'text' }), 'hi', f({ type: 'text', max: 10 }), 'hi'],
      ['a line into a paragraph', f({ type: 'text' }), 'hi', f({ type: 'textarea' }), undefined],
      ['a line into a box', f({ type: 'text' }), 'hi', f({ type: 'checkbox' }), undefined],
      ['a box into a line', f({ type: 'checkbox' }), true, f({ type: 'text' }), undefined],
      ['a choice still offered', f({ type: 'select', options: ['A', 'B'] }), 'B', f({ type: 'select', options: ['B', 'C'] }), 'B'],
      ['a choice withdrawn', f({ type: 'select', options: ['A', 'B'] }), 'A', f({ type: 'select', options: ['B', 'C'] }), undefined],
      ['no choice yet', f({ type: 'select', options: ['A'] }), '', f({ type: 'select', options: ['B'] }), ''],
      ['one choice become several', f({ type: 'select', options: ['A'] }), 'A', f({ type: 'select', options: ['A'], multiple: true }), undefined],
      ['several still offered', f({ type: 'select', options: ['A', 'B'], multiple: true }), ['A', 'B'], f({ type: 'select', options: ['A', 'B', 'C'], multiple: true }), ['A', 'B']],
      ['several, one withdrawn', f({ type: 'select', options: ['A', 'B'], multiple: true }), ['A', 'B'], f({ type: 'select', options: ['A'], multiple: true }), undefined],
      ['a type this client cannot draw', f({ type: 'future' }), 'x', f({ type: 'future' }), undefined],
    ] as [string, FormField, Value, FormField, Value | undefined][])('%s', (_, was, value, now, want) => {
      expect(carried(from(was, value), now)).toEqual(want);
    });

    it('carries nothing for a key the old form did not answer', () => {
      expect(carried(from(f({ type: 'text' }), 'hi'), f({ type: 'text', key: 'other' }))).toBeUndefined();
    });
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

    it('tells a member there is nothing to apply for', async () => {
      viewer.value = structuredClone(memberMe) as Me;
      server.routes['GET /api/v1/apply/form'] = () => problemAnswer('forbidden', 403);
      const root = await renderAt('/apply');
      expect(root.querySelector('main')!.textContent).toContain('already a member');
      expect(root.textContent).not.toContain(notAccepting);
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
