import { describe, expect, it } from 'vitest';
import type { FormAdmin, FormSchema } from '../../../api/dto';
import { kinds } from '../../../forms/fields';
import golden from '../../../../../../internal/dto/testdata/golden/form-admin.admin.json';
import {
  addField,
  attach,
  fieldTypes,
  fromSchema,
  moveField,
  propsFor,
  removeField,
  toSchema,
  typeLabel,
  updateField,
} from './draft';

// The editor's model (TKT-01M29CP2P1): what each change to the draft sends,
// and where a refusal's paths land.

const schema = () => structuredClone((golden as unknown as FormAdmin).schema) as FormSchema;
const keys = (s: FormSchema) => s.fields.map((f) => f.key);
// display is the one type that takes no answer, found through the map as
// the editor finds it.
const display = Object.keys(kinds).find((t) => !kinds[t].answers)!;

describe('the draft', () => {
  it('sends a schema nobody changed as it was loaded', () => {
    expect(toSchema(fromSchema(schema()))).toEqual(schema());
  });

  it('adds a field of the chosen type at the end, with a key no field has', () => {
    const d = fromSchema(schema());
    const [next, id] = addField(d, 'text');
    expect(next.fields.at(-1)!.id).toBe(id);
    expect(toSchema(next).fields.at(-1)).toEqual({ key: 'field_7', type: 'text' });
    const [again] = addField(updateField(next, id, { key: 'field_8' }), 'checkbox');
    expect(keys(toSchema(again)).slice(-2)).toEqual(['field_8', 'field_9']);
  });

  it('gives a new select an empty options box, sent as no options', () => {
    const [next] = addField(fromSchema(schema()), 'select');
    expect(toSchema(next).fields.at(-1)).toEqual({ key: 'field_7', type: 'select', options: [] });
  });

  it('removes a field, and moves one up or down, but not past either end', () => {
    const d = fromSchema(schema());
    const who = d.fields[2].id;
    expect(keys(toSchema(removeField(d, who)))).toEqual(['name', 'email', 'rules', 'interests', 'welcome']);
    expect(keys(toSchema(moveField(d, who, -1)))).toEqual(['name', 'who', 'email', 'rules', 'interests', 'welcome']);
    expect(keys(toSchema(moveField(d, who, 1)))).toEqual(['name', 'email', 'rules', 'who', 'interests', 'welcome']);
    expect(moveField(d, d.fields[0].id, -1)).toBe(d);
    expect(moveField(d, d.fields[5].id, 1)).toBe(d);
  });

  it('leaves out a property set back to its default, as the stored form does', () => {
    const d = fromSchema(schema());
    const name = d.fields[0].id;
    const changed = updateField(d, name, { required: false, from_claim: '', max: undefined, label: 'Your name' });
    expect(toSchema(changed).fields[0]).toEqual({ key: 'name', type: 'text', label: 'Your name' });
    expect(toSchema({ ...d, intro: '' })).not.toHaveProperty('intro_markdown');
  });

  it('keeps the options box as typed, and sends one option to a line', () => {
    const d = fromSchema(schema());
    const interests = d.fields[4];
    expect(interests.options).toBe('Games\nBooks\nHiking');
    const typed = updateField(d, interests.id, { optionsText: 'Games\n\nBooks\n' });
    expect(typed.fields[4].options).toBe('Games\n\nBooks\n');
    expect(toSchema(typed).fields[4].options).toEqual(['Games', 'Books']);
  });

  it('carries a property it draws no control for', () => {
    const s = schema();
    (s.fields[0] as unknown as Record<string, unknown>).placeholder = 'Ada';
    expect(toSchema(fromSchema(s)).fields[0]).toHaveProperty('placeholder', 'Ada');
  });
});

describe('the controls a type gets', () => {
  it.each([
    ['text', ['required', 'max', 'from_claim', 'locked']],
    ['email', ['required', 'max', 'from_claim', 'locked']],
    ['textarea', ['required', 'max']],
    ['checkbox', ['required']],
    ['select', ['required', 'options', 'multiple']],
    ['nonesuch', []],
  ])('%s', (type, props) => {
    expect(propsFor(type)).toEqual(props);
  });

  it('gives the type that takes no answer its text alone, and calls it a notice', () => {
    expect(propsFor(display)).toEqual(['markdown']);
    expect(typeLabel(display)).toBe('Notice');
  });

  it('offers every type the renderer draws', () => {
    expect(fieldTypes).toEqual(Object.keys(kinds));
    expect(fieldTypes.map(typeLabel)).not.toContain(undefined);
  });
});

describe('attach', () => {
  it('places each path on the control it names', () => {
    const d = fromSchema(schema());
    const a = attach(
      {
        title: 'required',
        intro_markdown: 'too_long',
        'fields.2.key': 'invalid_key',
        'fields.4.options.1': 'duplicate_option',
        'fields.5.markdown': 'required',
      },
      d,
    );
    expect(a.placed).toEqual({
      title: [{ code: 'required' }],
      intro: [{ code: 'too_long' }],
      [`${d.fields[2].id}.key`]: [{ code: 'invalid_key' }],
      [`${d.fields[4].id}.options`]: [{ code: 'duplicate_option', option: 2 }],
      [`${d.fields[5].id}.markdown`]: [{ code: 'required' }],
    });
    expect(a.unplaced).toEqual([]);
  });

  it('keeps an error on its field when the field moves after the refusal', () => {
    // With the first field gone, no id is its position plus one.
    const loaded = fromSchema(schema());
    const d = removeField(loaded, loaded.fields[0].id);
    const a = attach({ 'fields.2.label': 'required' }, d);
    expect(Object.keys(a.placed)).toEqual([`${d.fields[2].id}.label`]);
    const moved = moveField(d, d.fields[2].id, -1);
    expect(Number(Object.keys(a.placed)[0].split('.')[0])).toBe(moved.fields[1].id);
    expect(moved.fields[1].f.key).toBe('rules');
  });

  it('returns what has no control, and where it points', () => {
    const d = fromSchema(schema());
    const a = attach(
      {
        '': 'unknown_property',
        fields: 'too_many',
        'fields.1': 'unknown_property',
        'fields.3.type': 'unknown_type',
        'fields.3.max': 'not_allowed',
        'fields.9.key': 'required',
      },
      d,
    );
    expect(a.placed).toEqual({});
    expect(a.unplaced).toEqual([
      { path: '', code: 'unknown_property' },
      { path: 'fields', code: 'too_many' },
      { path: 'fields.1', code: 'unknown_property', field: { at: 1, key: 'email' } },
      { path: 'fields.3.type', code: 'unknown_type', field: { at: 3, key: 'rules' } },
      { path: 'fields.3.max', code: 'not_allowed', field: { at: 3, key: 'rules' } },
      { path: 'fields.9.key', code: 'required' },
    ]);
  });
});
