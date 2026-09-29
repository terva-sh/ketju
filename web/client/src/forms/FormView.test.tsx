import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ApplicationForm, Form, FormField } from '../api/dto';
import { toProblem } from '../api/problem';
import { answersOf, FormView, initialValues, type FormViewProps } from './FormView';
import { fieldMessage } from './messages';
import applyForm from '../../../../internal/dto/testdata/golden/apply-form.pre_user.json';
import fieldErrorsBody from '../../../../internal/problem/testdata/field-errors.json';

// The form renderer (TKT-01M29CP2NG): it draws what the schema says and
// nothing else, through fields.tsx's one map.

const golden = () => structuredClone(applyForm) as ApplicationForm;

const field = (over: Partial<FormField> & Pick<FormField, 'key' | 'type'>): FormField => ({
  label: over.key,
  required: false,
  max: 0,
  options: [],
  multiple: false,
  locked: false,
  from_claim: '',
  html: '',
  ...over,
});

const form = (fields: FormField[], over: Partial<Form> = {}): Form => ({
  key: 'registration',
  version: 3,
  title: 'Apply',
  intro_html: '',
  fields,
  ...over,
});

// Every type in the table, once.
const everyType = form(
  [
    field({ key: 'name', type: 'text', label: 'Name', required: true, max: 200 }),
    field({ key: 'contact', type: 'email', label: 'Contact', max: 254 }),
    field({ key: 'about', type: 'textarea', label: 'About you', max: 500 }),
    field({ key: 'agree', type: 'checkbox', label: 'I agree', required: true }),
    field({ key: 'colour', type: 'select', label: 'Colour', options: ['Red', 'Green'] }),
    field({ key: 'tags', type: 'select', label: 'Tags', options: ['Games', 'Books', 'Hiking'], multiple: true }),
    field({ key: 'note', type: 'markdown_notice', label: '', html: '<p>Be <em>kind</em>.</p>' }),
  ],
  { intro_html: '<p>Hello there.</p>' },
);

afterEach(() => {
  render(null, document.body);
  document.body.replaceChildren();
});

function mount(props: FormViewProps): HTMLElement {
  const root = document.createElement('div');
  document.body.replaceChildren(root);
  act(() => render(<FormView {...props} />, root));
  return root;
}

const fieldEl = (root: HTMLElement, key: string) => root.querySelector<HTMLElement>(`[data-field="${key}"]`)!;
const labelOf = (root: HTMLElement, control: Element) =>
  root.querySelector(`label[for="${control.id}"]`)?.textContent ?? control.closest('fieldset')?.querySelector('legend')?.textContent;

describe('FormView', () => {
  it('renders every field type', () => {
    const root = mount({ form: everyType, mode: 'fill', values: initialValues(everyType) });
    expect(root.querySelector('.form-intro')!.innerHTML).toBe('<p>Hello there.</p>');
    const name = fieldEl(root, 'name').querySelector('input')!;
    expect(name.type).toBe('text');
    expect(name.getAttribute('aria-required')).toBe('true');
    expect(labelOf(root, name)).toBe('Name (required)');
    expect(fieldEl(root, 'contact').querySelector('input')!.type).toBe('email');
    const about = fieldEl(root, 'about').querySelector('textarea')!;
    expect(about.getAttribute('aria-describedby')).toBe(fieldEl(root, 'about').querySelector('.form-hint')!.id);
    expect(fieldEl(root, 'about').textContent).toContain('Up to 500 characters.');
    expect(fieldEl(root, 'agree').querySelector<HTMLInputElement>('input')!.type).toBe('checkbox');
    const colour = fieldEl(root, 'colour').querySelector('select')!;
    expect([...colour.options].map((o) => o.value)).toEqual(['', 'Red', 'Green']);
    // A multiple choice is a group of boxes, not a multiple select.
    const tags = fieldEl(root, 'tags');
    expect(tags.querySelector('select')).toBeNull();
    expect(tags.querySelector('legend')!.textContent).toBe('Tags');
    expect([...tags.querySelectorAll('input[type="checkbox"]')].map((b) => labelOf(root, b))).toEqual(['Games', 'Books', 'Hiking']);
    expect(fieldEl(root, 'note').querySelector('.form-notice-body')!.innerHTML).toBe('<p>Be <em>kind</em>.</p>');
    expect(fieldEl(root, 'note').querySelector('input, textarea, select')).toBeNull();
    expect(root.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled).toBe(false);
  });

  it('follows the schema: two schemas draw two different forms, and nothing else', () => {
    const one = golden().form;
    const two = form([
      field({ key: 'pet', type: 'select', label: 'Your pet', options: ['Cat', 'Dog'] }),
      field({ key: 'bio', type: 'textarea', label: 'A short bio', max: 300 }),
    ]);
    const drawn = (f: Form) => {
      const root = mount({ form: f, mode: 'fill', values: initialValues(f) });
      const keys = [...root.querySelectorAll('[data-field]')].map((e) => e.getAttribute('data-field'));
      const controls = [...root.querySelectorAll('input, textarea, select')].map((c) => `${c.tagName}:${labelOf(root, c)}`);
      return { keys, controls };
    };
    expect(drawn(one)).toEqual({
      keys: ['name', 'email', 'who', 'rules'],
      controls: [
        'INPUT:Name (required)',
        'INPUT:Email',
        'TEXTAREA:Who invited you or how did you find us? (required)',
        'INPUT:I have read the guidelines (required)',
      ],
    });
    expect(drawn(two)).toEqual({ keys: ['pet', 'bio'], controls: ['SELECT:Your pet', 'TEXTAREA:A short bio'] });
  });

  it('shows a notice for an unknown type and will not submit', () => {
    const f = form([field({ key: 'name', type: 'text', label: 'Name' }), field({ key: 'when', type: 'date', label: 'Birthday' })]);
    const onSubmit = vi.fn();
    const root = mount({ form: f, mode: 'fill', values: initialValues(f), onSubmit });
    const notice = fieldEl(root, 'when');
    expect(notice.getAttribute('role')).toBe('note');
    expect(notice.textContent).toContain('Birthday');
    expect(notice.textContent).toContain('cannot be sent');
    const submit = root.querySelector<HTMLButtonElement>('button[type="submit"]')!;
    expect(submit.disabled).toBe(true);
    act(() => {
      root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }));
    });
    expect(onSubmit).not.toHaveBeenCalled();
    // An unknown field is sent nothing: the client cannot say what it takes.
    expect(Object.keys(answersOf(f, initialValues(f)))).toEqual(['name']);
  });

  it('draws a locked field read-only with its claim and a lock label, and never sends it', () => {
    const g = golden();
    const values = initialValues(g.form, g.prefill);
    const root = mount({ form: g.form, mode: 'fill', prefill: g.prefill, values, provider: 'github' });
    const email = fieldEl(root, 'email').querySelector('input')!;
    expect(email.readOnly).toBe(true);
    expect(email.value).toBe('carol@example.com');
    const lock = fieldEl(root, 'email').querySelector('[data-lock]')!;
    expect(lock.textContent).toBe('From your github account');
    expect(email.getAttribute('aria-describedby')).toBe(lock.id);
    // A locked field is not required of the applicant, and is not marked so.
    expect(email.hasAttribute('aria-required')).toBe(false);
    // Prefill fills an unlocked field too, which the applicant may change.
    const name = fieldEl(root, 'name').querySelector('input')!;
    expect(name.readOnly).toBe(false);
    expect(name.value).toBe('Carol Applicant');

    const answers = answersOf(g.form, { ...values, email: 'someone@else.example' });
    expect(answers).toEqual({ name: 'Carol Applicant', who: '', rules: false });
    expect(answers).not.toHaveProperty('email');
  });

  it('attaches the golden 422 body to its fields and focuses the first', async () => {
    const p = await toProblem(
      new Response(JSON.stringify(fieldErrorsBody), { status: 422, headers: { 'Content-Type': 'application/problem+json' } }),
    );
    const g = golden();
    const values = initialValues(g.form, g.prefill);
    const root = mount({ form: g.form, mode: 'fill', prefill: g.prefill, values });
    expect(root.querySelectorAll('[aria-invalid]')).toHaveLength(0);

    act(() => render(<FormView form={g.form} mode="fill" prefill={g.prefill} values={values} errors={p.fieldErrors} />, root));
    const who = fieldEl(root, 'who').querySelector('textarea')!;
    const rules = fieldEl(root, 'rules').querySelector('input')!;
    for (const [control, key, code] of [
      [who, 'who', 'required'],
      [rules, 'rules', 'wrong_type'],
    ] as const) {
      expect(control.getAttribute('aria-invalid')).toBe('true');
      const error = fieldEl(root, key).querySelector('.form-error')!;
      expect(control.getAttribute('aria-describedby')!.split(' ')).toContain(error.id);
      expect(error.textContent).toBe(fieldMessage(code));
    }
    expect(fieldEl(root, 'name').querySelector('[aria-invalid]')).toBeNull();
    // who comes before rules in the form, so focus is there.
    expect(document.activeElement).toBe(who);
  });

  it('says an error for no field drawn above Submit', () => {
    const g = golden();
    const root = mount({ form: g.form, mode: 'fill', values: initialValues(g.form), errors: { gone: 'required' } });
    expect(root.querySelector('.form-message')!.textContent).toBe(fieldMessage('required'));
  });

  it('reports each change and each submission', () => {
    const onChange = vi.fn();
    const onSubmit = vi.fn();
    const root = mount({ form: everyType, mode: 'fill', values: initialValues(everyType), onChange, onSubmit });
    const name = fieldEl(root, 'name').querySelector('input')!;
    name.value = 'Dana';
    act(() => {
      name.dispatchEvent(new Event('input', { bubbles: true }));
    });
    act(() => fieldEl(root, 'tags').querySelectorAll<HTMLInputElement>('input')[1].click());
    act(() => fieldEl(root, 'agree').querySelector<HTMLInputElement>('input')!.click());
    expect(onChange.mock.calls).toEqual([
      ['name', 'Dana'],
      ['tags', ['Books']],
      ['agree', true],
    ]);
    act(() => {
      root.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }));
    });
    expect(onSubmit).toHaveBeenCalledOnce();
  });

  it('sends every answering field, empty when unanswered, and never a notice', () => {
    expect(answersOf(everyType, initialValues(everyType))).toEqual({
      name: '',
      contact: '',
      about: '',
      agree: false,
      colour: '',
      tags: [],
    });
  });

  it('shows answers in read mode', () => {
    const answers = { name: 'Dana', contact: '', about: 'Line one\nline two', agree: true, colour: 'Red', tags: ['Games', 'Hiking'] };
    const root = mount({ form: everyType, mode: 'read', answers });
    expect(root.querySelector('form, input, textarea, select, button')).toBeNull();
    const shown = Object.fromEntries(
      [...root.querySelectorAll('.form-answer')].map((e) => [e.querySelector('dt')!.textContent, e.querySelector('dd')!.textContent]),
    );
    expect(shown).toEqual({
      Name: 'Dana',
      Contact: '—',
      'About you': 'Line one\nline two',
      'I agree': 'Yes',
      Colour: 'Red',
      Tags: 'Games, Hiking',
    });
  });

  it('draws the controls with no Submit in preview mode', () => {
    const root = mount({ form: everyType, mode: 'preview', values: initialValues(everyType) });
    expect(root.querySelectorAll('input, textarea, select').length).toBeGreaterThan(5);
    expect(root.querySelector('button[type="submit"]')).toBeNull();
  });
});

// fields.tsx is the only place the client names a field type: the renderer
// and the screens that use it reach every type through its map.
describe('the one type map', () => {
  const sources = import.meta.glob(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}'], {
    query: '?raw',
    import: 'default',
    eager: true,
  }) as Record<string, string>;
  const types = ['text', 'email', 'textarea', 'checkbox', 'select', 'markdown_notice'];
  const literal = new RegExp(`['"\`](${types.join('|')})['"\`]`);
  // A file beside this test is keyed ./name, any other ../dir/name.
  const isMap = (p: string) => p === './fields.tsx' || p === '../forms/fields.tsx';

  it('reads the sources', () => {
    expect(Object.keys(sources).some(isMap)).toBe(true);
    expect(sources[Object.keys(sources).find(isMap)!]).toMatch(literal);
  });

  it.each(
    Object.keys(sources).filter((p) => !isMap(p) && (/^\.\.\/(forms|screens\/apply)\//.test(p) || p.startsWith('./'))),
  )('%s names no field type', (p) => {
    expect(sources[p]).not.toMatch(literal);
  });

  // dto.ts describes the wire shape in prose, and draws nothing.
  it.each(Object.keys(sources).filter((p) => !isMap(p) && p !== '../api/dto.ts'))('%s never names markdown_notice', (p) => {
    expect(sources[p]).not.toMatch(/markdown_notice/);
  });
});
