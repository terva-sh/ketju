import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Form, FormAdmin, Me } from '../../../api/dto';
import { viewer } from '../../../state/viewer';
import { problemAnswer, renderAt, settle, stubServer, unmount, type StubServer } from '../../../testing';
import golden from '../../../../../../internal/dto/testdata/golden/form-admin.admin.json';
import moderatorMe from '../../../../../../internal/dto/testdata/golden/me.moderator.json';
import { previewDelay } from './FormEditor';

// The form editor at /admin/forms (TKT-01M29CP2P1): a draft previewed by
// the server through the apply screen's renderer, saved as a new version on
// base_version, and every earlier version open read-only.

const url = '/api/v1/admin/forms/registration';
const formAdmin = () => structuredClone(golden) as unknown as FormAdmin;
const asModerator = () => structuredClone(moderatorMe) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

// saved is the answer to a save: the draft as version 3, active, with the
// earlier versions as they were.
function saved(title: string): FormAdmin {
  const a = formAdmin();
  a.active_version = 3;
  a.versions = [{ version: 3, created_at: 1757700000000, created_by: a.versions[0].created_by }, ...a.versions];
  a.form = { ...a.form, version: 3, title };
  a.schema = { ...a.schema, title };
  return a;
}

let saved_: Me;
let server: StubServer;
beforeEach(() => {
  saved_ = viewer.value;
  viewer.value = asAdmin();
  server = stubServer({
    [`GET ${url}`]: () => ({ status: 200, body: formAdmin() }),
    [`POST ${url}/preview`]: (body) => {
      const f: Form = { ...formAdmin().form, version: 0, title: (body as { schema: { title: string } }).schema.title };
      return { status: 200, body: f };
    },
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = saved_;
  vi.unstubAllGlobals();
});

const button = (root: Element, name: string | RegExp) =>
  [...root.querySelectorAll('button')].find((b) =>
    typeof name === 'string' ? b.textContent === name : name.test(b.textContent ?? ''),
  ) ?? null;
const saveButton = (root: Element) => button(root, /^Save as version/)!;
const labelled = (root: Element, text: string) => {
  const l = [...root.querySelectorAll('label')].find((x) => x.textContent === text)!;
  return root.querySelector<HTMLInputElement>(`[id="${l.htmlFor}"]`)!;
};
const fieldBox = (root: Element, at: number) => root.querySelector<HTMLFieldSetElement>(`[data-draft-field="${at}"]`)!;
const inBox = (box: Element, text: string) => labelled(box, text);

async function type(root: HTMLElement, el: HTMLInputElement | HTMLTextAreaElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await settle(root);
}
async function click(root: HTMLElement, el: HTMLElement | null) {
  expect(el).not.toBeNull();
  await act(async () => el!.click());
  await settle(root);
}
// previewed waits out the preview's delay and its answer.
async function previewed(root: HTMLElement) {
  await act(() => new Promise((r) => setTimeout(r, previewDelay + 20)));
  await settle(root);
}
const sentTo = (path: string) => server.sent.filter((s) => s.method === 'POST' && s.url === `${url}${path}`);

describe('the form editor', () => {
  it('draws the active version as a draft of fields, and its form as the preview', async () => {
    const root = await renderAt('/admin/forms');
    expect(root.querySelector('[data-screen="admin-forms"]')).not.toBeNull();
    expect(labelled(root, 'Title').value).toBe('Apply to join');
    const legends = [...root.querySelectorAll('.fe-field legend')].map((l) => l.textContent);
    expect(legends).toEqual([
      'Field 1: Short text',
      'Field 2: Email address',
      'Field 3: Paragraph',
      'Field 4: Checkbox',
      'Field 5: Choice',
      'Field 6: Notice',
    ]);
    expect(inBox(fieldBox(root, 4), 'Options').value).toBe('Games\nBooks\nHiking');
    expect(inBox(fieldBox(root, 5), 'Text').value).toBe('Be **kind**.');
    // A notice has no required box, and a checkbox no limit.
    expect(fieldBox(root, 5).textContent).not.toContain('Required');
    expect(fieldBox(root, 3).textContent).not.toContain('Character limit');

    const preview = root.querySelector('[data-preview]')!;
    expect(preview.querySelector('.form-view')).not.toBeNull();
    expect(preview.querySelector('[data-field="welcome"] .form-notice-body')!.innerHTML).toContain('<strong>kind</strong>');
    expect(preview.querySelector('button[type="submit"]')).toBeNull();
    expect(saveButton(root).disabled).toBe(true);
    expect(sentTo('/preview')).toEqual([]);
  });

  it("previews an edit through the server, and draws the server's answer", async () => {
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Join the club');
    expect(root.querySelector('[data-preview-state]')!.textContent).toBe('Updating the preview…');
    expect(saveButton(root).disabled).toBe(true);
    await previewed(root);
    expect(sentTo('/preview')).toHaveLength(1);
    expect((sentTo('/preview')[0].body as { schema: { title: string } }).schema.title).toBe('Join the club');
    expect(root.querySelector('[data-preview] .fe-form-title')!.textContent).toBe('Join the club');
    expect(saveButton(root).disabled).toBe(false);
  });

  it('sends one preview for edits made within the delay', async () => {
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'J');
    await type(root, labelled(root, 'Title'), 'Jo');
    await type(root, labelled(root, 'Title'), 'Join');
    await previewed(root);
    expect(sentTo('/preview').map((s) => (s.body as { schema: { title: string } }).schema.title)).toEqual(['Join']);
  });

  it('save posts base_version and shows the new active version', async () => {
    server.routes[`POST ${url}/versions`] = () => ({ status: 201, body: saved('Join the club') });
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Join the club');
    await previewed(root);
    await click(root, saveButton(root));

    const body = sentTo('/versions')[0].body as { base_version: number; schema: { title: string; fields: unknown[] } };
    expect(body.base_version).toBe(2);
    expect(body.schema.title).toBe('Join the club');
    expect(body.schema.fields).toEqual(formAdmin().schema.fields);

    const notice = root.querySelector<HTMLElement>('[data-notice]')!;
    expect(notice.textContent).toBe('Saved. Version 3 is now the form applicants see.');
    expect(document.activeElement).toBe(notice);
    const rows = [...root.querySelectorAll('.fe-versions tbody tr')];
    expect(rows.map((r) => r.querySelector('[data-version]')!.getAttribute('data-version'))).toEqual(['3', '2', '1']);
    expect(rows[0].querySelector('[data-active]')).not.toBeNull();
    expect(rows[1].querySelector('[data-active]')).toBeNull();
    expect(rows[1].textContent).toContain('@root');
    expect(rows[2].textContent).toContain('Seeded at install');
    expect(saveButton(root).textContent).toBe('Save as version 4');
    expect(saveButton(root).disabled).toBe(true);
  });

  it('a 409 keeps the draft and says a newer version exists', async () => {
    server.routes[`POST ${url}/versions`] = () => problemAnswer('conflict', 409);
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Mine');
    await previewed(root);
    await click(root, saveButton(root));

    const conflict = root.querySelector<HTMLElement>('[data-conflict]')!;
    expect(conflict.textContent).toContain('Someone saved a newer version of this form');
    expect(document.activeElement).toBe(conflict);
    expect(labelled(root, 'Title').value).toBe('Mine');
    expect(saveButton(root).disabled).toBe(true);

    // Loading the newest discards the draft, only once confirmed.
    const newer = saved('Theirs');
    server.routes[`GET ${url}`] = () => ({ status: 200, body: newer });
    await click(root, button(conflict, 'Load the newest version'));
    expect(labelled(root, 'Title').value).toBe('Mine');
    await click(root, button(root, 'Discard and load'));
    expect(labelled(root, 'Title').value).toBe('Theirs');
    expect(root.querySelector('[data-conflict]')).toBeNull();
    expect(root.querySelector('[data-notice]')!.textContent).toBe('Loaded version 3. Your draft was discarded.');
  });

  it('a 422 attaches errors and save stays disabled until a preview passes', async () => {
    let refuse = true;
    server.routes[`POST ${url}/preview`] = () =>
      refuse
        ? problemAnswer('validation-failed', 422, {
            errors: [
              { field: 'fields.2.key', code: 'invalid_key' },
              { field: 'fields.4.options.1', code: 'duplicate_option' },
            ],
          })
        : { status: 200, body: { ...formAdmin().form, version: 0 } };
    const root = await renderAt('/admin/forms');
    await type(root, inBox(fieldBox(root, 2), 'Key'), 'Who');
    await previewed(root);

    const key = inBox(fieldBox(root, 2), 'Key');
    expect(key.getAttribute('aria-invalid')).toBe('true');
    const described = key.getAttribute('aria-describedby')!.split(' ').map((id) => document.getElementById(id)?.textContent);
    expect(described).toContain(
      'A key starts with a lowercase letter and holds only lowercase letters, digits and underscores.',
    );
    expect(fieldBox(root, 4).textContent).toContain('Option 2: Each option can be chosen only once.');
    expect(root.querySelector('[data-lead]')!.textContent).toContain('The problems are marked on the fields below.');
    expect(root.querySelector('[data-preview]')).toBeNull();
    expect(root.querySelector('[data-preview-refused]')).not.toBeNull();
    expect(saveButton(root).disabled).toBe(true);

    // Correcting the key clears what was said about it at once, and Save
    // waits for the preview of the corrected draft.
    refuse = false;
    await type(root, inBox(fieldBox(root, 2), 'Key'), 'who_invited');
    expect(inBox(fieldBox(root, 2), 'Key').hasAttribute('aria-invalid')).toBe(false);
    expect(saveButton(root).disabled).toBe(true);
    await previewed(root);
    expect(root.querySelector('[data-lead]')).toBeNull();
    expect(saveButton(root).disabled).toBe(false);
  });

  // Terva review 1490: an edit undone is no change, and saving it would
  // write a copy of the active version.
  it('does not offer to save a draft whose edits were undone', async () => {
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Something else');
    await previewed(root);
    expect(saveButton(root).disabled).toBe(false);
    await type(root, labelled(root, 'Title'), 'Apply to join');
    await previewed(root);
    expect(saveButton(root).disabled).toBe(true);
    expect(root.querySelector('#fe-save-hint')!.textContent).toBe('Nothing has changed since this version was saved.');

    await click(root, button(root, 'Add field'));
    await click(root, button(fieldBox(root, 6), 'Remove'));
    await previewed(root);
    expect(saveButton(root).disabled).toBe(true);
  });

  // Terva review 1490: a failed request says nothing about the draft. It is
  // not marked refused, and it can be asked for again without an edit.
  it('tells a failed preview request from a refusal, and tries it again on request', async () => {
    let fail = true;
    const ok = server.routes[`POST ${url}/preview`];
    server.routes[`POST ${url}/preview`] = (body) => (fail ? problemAnswer('unavailable', 503) : ok(body));
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Join');
    await previewed(root);

    const failed = root.querySelector('[data-preview-failed]')!;
    expect(failed.textContent).toContain('The preview could not be made.');
    expect(root.querySelector('[data-lead]')).toBeNull();
    expect(root.querySelector('[data-preview-refused]')).toBeNull();
    expect(saveButton(root).disabled).toBe(true);

    fail = false;
    await click(root, button(failed, 'Try the preview again'));
    await act(() => new Promise((r) => setTimeout(r, 20)));
    await settle(root);
    expect(sentTo('/preview')).toHaveLength(2);
    expect(root.querySelector('[data-preview-failed]')).toBeNull();
    expect(root.querySelector('[data-preview] .fe-form-title')!.textContent).toBe('Join');
    expect(saveButton(root).disabled).toBe(false);
  });

  // Terva review 1490: answers tried in the preview go with the draft.
  it('clears the answers tried in the preview when a save reseeds the draft', async () => {
    server.routes[`POST ${url}/versions`] = () => ({ status: 201, body: saved('Join the club') });
    const root = await renderAt('/admin/forms');
    const name = () => root.querySelector<HTMLInputElement>('[data-preview] [data-field="name"] input')!;
    await type(root, name(), 'Sample Name');
    expect(name().value).toBe('Sample Name');
    await type(root, labelled(root, 'Title'), 'Join the club');
    await previewed(root);
    expect(name().value).toBe('Sample Name');
    await click(root, saveButton(root));
    expect(name().value).toBe('');
  });

  it('a 422 on save attaches its errors, focuses the lead line, and disables save', async () => {
    server.routes[`POST ${url}/versions`] = () =>
      problemAnswer('validation-failed', 422, { errors: [{ field: 'title', code: 'too_long' }] });
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Long');
    await previewed(root);
    await click(root, saveButton(root));
    expect(labelled(root, 'Title').getAttribute('aria-invalid')).toBe('true');
    expect(document.activeElement).toBe(root.querySelector('[data-lead]'));
    expect(saveButton(root).disabled).toBe(true);
  });

  it('says in its own lead line an error that has no control', async () => {
    server.routes[`POST ${url}/preview`] = () =>
      problemAnswer('validation-failed', 422, {
        errors: [
          { field: '', code: 'unknown_property' },
          { field: 'fields', code: 'too_many' },
          { field: 'fields.3.type', code: 'unknown_type' },
        ],
      });
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'x');
    await previewed(root);
    const items = [...root.querySelectorAll('[data-lead] li')].map((li) => li.textContent);
    expect(items).toEqual([
      'The form as a whole: This holds a property no form field has.',
      'The list of fields: There are more of these than the form allows.',
      'Field 4 (rules), type: This is not a field type the site knows.',
    ]);
    expect(root.querySelector('[data-lead]')!.textContent).not.toContain('marked on the fields');
  });

  it('adds, moves and removes fields, keeping focus on the control used', async () => {
    const root = await renderAt('/admin/forms');
    const select = root.querySelector<HTMLSelectElement>('[data-fe-add]')!;
    await act(async () => {
      select.value = 'select';
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await click(root, button(root, 'Add field'));
    const added = fieldBox(root, 6);
    expect(added.querySelector('legend')!.textContent).toBe('Field 7: Choice');
    expect(document.activeElement).toBe(inBox(added, 'Label'));

    await click(root, button(fieldBox(root, 6), 'Move up'));
    expect(fieldBox(root, 5).querySelector('legend')!.textContent).toBe('Field 6: Choice');
    expect(document.activeElement).toBe(button(fieldBox(root, 5), 'Move up'));

    await click(root, button(fieldBox(root, 5), 'Remove'));
    expect(root.querySelectorAll('.fe-field')).toHaveLength(6);
    expect(document.activeElement).toBe(inBox(fieldBox(root, 5), 'Heading (optional)'));
    await previewed(root);
    expect((sentTo('/preview').at(-1)!.body as { schema: { fields: unknown[] } }).schema.fields).toEqual(
      formAdmin().schema.fields,
    );
  });

  it('opens an earlier version read-only in the same renderer', async () => {
    const v1 = formAdmin();
    v1.form = { ...v1.form, version: 1, fields: v1.form.fields.slice(0, 4) };
    v1.form.fields[2] = { ...v1.form.fields[2], label: 'The old wording' };
    server.routes[`GET ${url}/versions/1`] = () => ({ status: 200, body: v1 });
    const root = await renderAt('/admin/forms');
    await type(root, labelled(root, 'Title'), 'Kept');
    await click(root, button(root, 'View version 1'));

    const view = root.querySelector('[data-viewing="1"]')!;
    expect(view.querySelector('h3')!.textContent).toBe('Version 1, read-only');
    expect(document.activeElement).toBe(view.querySelector('h3'));
    const frame = view.querySelector<HTMLFieldSetElement>('fieldset.fe-readonly')!;
    expect(frame.disabled).toBe(true);
    expect(frame.querySelector('.form-view')).not.toBeNull();
    expect(frame.textContent).toContain('The old wording');
    expect(frame.querySelector('[data-field="interests"]')).toBeNull();
    // The draft is left as it was.
    expect(labelled(root, 'Title').value).toBe('Kept');

    await click(root, button(view, 'Close version 1'));
    expect(root.querySelector('[data-viewing]')).toBeNull();
    expect(document.activeElement).toBe(button(root, 'View version 1'));
  });

  it('frames the preview at a phone width on request', async () => {
    const root = await renderAt('/admin/forms');
    const toggle = button(root, 'Phone width')!;
    expect(toggle.getAttribute('aria-pressed')).toBe('false');
    await click(root, toggle);
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    expect(root.querySelector('[data-preview]')!.classList.contains('fe-phone')).toBe(true);
  });
});

describe('who sees the form editor', () => {
  it('is hidden from moderators, and refused at its address without asking the server', async () => {
    viewer.value = asModerator();
    const root = await renderAt('/admin/forms');
    expect([...root.querySelectorAll('[data-admin-sections] a')].map((a) => a.textContent)).not.toContain(
      'Application forms',
    );
    expect(root.textContent).toContain('Your account does not have access to this section.');
    expect(root.querySelector('.form-editor')).toBeNull();
    expect(server.sent.filter((s) => s.url.startsWith(url))).toEqual([]);
  });

  it('says what the server said when it refuses the form', async () => {
    server.routes[`GET ${url}`] = () => problemAnswer('forbidden', 403);
    const root = await renderAt('/admin/forms');
    expect(root.querySelector('.error-panel')!.textContent).toContain('The form could not be loaded.');
  });
});
