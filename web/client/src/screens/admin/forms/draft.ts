import type { FormSchema, FormSchemaField } from '../../../api/dto';
import { kinds } from '../../../forms/fields';

// The form editor's model (TKT-01M29CP2P1): a draft of a form version's
// schema, and the pure functions the editor changes it through. Nothing here
// judges a schema. The server is the authority on the rules
// (internal/forms Parse): the editor sends the draft to be previewed, and
// what comes back refused is attached to the controls by attach, below.
//
// A field's type is named only through src/forms/fields.tsx's map, as
// everywhere else in the client. The one type that takes no answer, the
// notice, is found by its kind rather than by its name.

// Prop is a property the editor draws a control for, beside the label and
// the key every field has.
export type Prop = 'required' | 'max' | 'options' | 'multiple' | 'from_claim' | 'locked' | 'markdown';

// propsFor is which property controls a field of type gets. It follows
// internal/forms's types table, but only to decide what to offer: a control
// left off here is one the admin cannot set, and the server still refuses a
// property the type does not allow. A type this client does not know gets
// none, and its properties are carried as they are.
const kindOf = (type: string) => (Object.hasOwn(kinds, type) ? kinds[type] : undefined);

export function propsFor(type: string): Prop[] {
  const k = kindOf(type);
  if (!k) return [];
  if (!k.answers) return ['markdown'];
  switch (type) {
    case 'text':
    case 'email':
      return ['required', 'max', 'from_claim', 'locked'];
    case 'textarea':
      return ['required', 'max'];
    case 'select':
      return ['required', 'options', 'multiple'];
    default:
      return ['required'];
  }
}

// typeLabel is a type as the editor names it to an admin.
const typeNames: Record<string, string> = {
  text: 'Short text',
  email: 'Email address',
  textarea: 'Paragraph',
  checkbox: 'Checkbox',
  select: 'Choice',
};

export function typeLabel(type: string): string {
  if (kindOf(type)?.answers === false) return 'Notice';
  return Object.hasOwn(typeNames, type) ? typeNames[type] : type;
}

// fieldTypes is every type the client can draw, in the map's order: the
// types a new field may take.
export const fieldTypes = Object.keys(kinds);

// The claims a field may be prefilled from (docs/05 "Forms"), offered as a
// choice. The server holds the list and refuses any other.
export const claims = ['name', 'email', 'preferred_username'];

// DraftField is one field being edited. id names it for as long as the
// editor is open, whatever its position, so a control keeps its focus and
// its errors when the field moves. f is the schema field, with any property
// the editor does not draw carried as it came. A select's options are held
// as the text of their box, one to a line, so a line being typed is not
// lost; toSchema splits them.
export interface DraftField {
  id: number;
  f: FormSchemaField;
  options?: string;
}

export interface Draft {
  title: string;
  intro: string;
  fields: DraftField[];
  // next is the id the next field added takes.
  next: number;
}

export function fromSchema(s: FormSchema): Draft {
  const fields = s.fields.map((f, i) => ({
    id: i + 1,
    f: { ...f },
    ...(f.options ? { options: f.options.join('\n') } : {}),
  }));
  return { title: s.title, intro: s.intro_markdown ?? '', fields, next: fields.length + 1 };
}

// optionList is a select's options from its box: one to a line, with blank
// lines left out.
export const optionList = (text: string) => text.split('\n').filter((o) => o.trim() !== '');

// toSchema is the draft as the API takes it. A property at its default is
// left out, as the stored canonical form leaves it out
// (internal/forms Canonical), so a draft nobody changed is sent as it was
// loaded.
export function toSchema(d: Draft): FormSchema {
  const out: FormSchema = { title: d.title, fields: d.fields.map(fieldOut) };
  if (d.intro !== '') out.intro_markdown = d.intro;
  return out;
}

function fieldOut({ f, options }: DraftField): FormSchemaField {
  const o: Record<string, unknown> = { ...f };
  if (options !== undefined) o.options = optionList(options);
  for (const [k, v] of Object.entries(o)) {
    if (k === 'key' || k === 'type') continue;
    if (v === false || v === '' || v === 0 || v === undefined || v === null) delete o[k];
  }
  return o as unknown as FormSchemaField;
}

// sameSchema is whether two schemas say the same thing, whatever order
// their properties are in: a draft whose edits were undone is the version
// it was loaded from, and saving it would write a copy (Terva review 1490).
export function sameSchema(a: FormSchema, b: FormSchema): boolean {
  return sorted(a) === sorted(b);
}

function sorted(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(sorted).join(',')}]`;
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>;
    return `{${Object.keys(o)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${sorted(o[k])}`)
      .join(',')}}`;
  }
  return JSON.stringify(v);
}

// newKey is a key no field of d has yet.
function newKey(d: Draft): string {
  const used = new Set(d.fields.map((x) => x.f.key));
  let n = d.fields.length + 1;
  while (used.has(`field_${n}`)) n++;
  return `field_${n}`;
}

// addField adds an empty field of type at the end. It answers the new
// draft and the new field's id.
export function addField(d: Draft, type: string): [Draft, number] {
  const f: DraftField = { id: d.next, f: { key: newKey(d), type } };
  if (propsFor(type).includes('options')) f.options = '';
  return [{ ...d, fields: [...d.fields, f], next: d.next + 1 }, f.id];
}

export function removeField(d: Draft, id: number): Draft {
  return { ...d, fields: d.fields.filter((x) => x.id !== id) };
}

// moveField moves the field id by one place, up (-1) or down (+1). At
// either end it stays where it is.
export function moveField(d: Draft, id: number, by: -1 | 1): Draft {
  const i = d.fields.findIndex((x) => x.id === id);
  const j = i + by;
  if (i < 0 || j < 0 || j >= d.fields.length) return d;
  const fields = [...d.fields];
  [fields[i], fields[j]] = [fields[j], fields[i]];
  return { ...d, fields };
}

// updateField sets properties of the field id. options is the options'
// box, as it is typed.
export function updateField(d: Draft, id: number, change: Partial<FormSchemaField> & { optionsText?: string }): Draft {
  const { optionsText, ...props } = change;
  return {
    ...d,
    fields: d.fields.map((x) =>
      x.id === id
        ? { ...x, f: { ...x.f, ...props }, ...(optionsText !== undefined ? { options: optionsText } : {}) }
        : x,
    ),
  };
}

// Where is what an error's path names in the editor: the title or the
// intro, one property control of one field, or nowhere drawn.
//
// Placed errors are keyed "title", "intro", or "<id>.<control>", where a
// control is key, label or a Prop. An option's error ("fields.2.options.3")
// is placed on the options box, with the option's number. Everything else
// is unplaced, with where it points, for the editor's lead line to say.
export interface Placed {
  code: string;
  // option is the 1-based number of the option an error names, if it names
  // one.
  option?: number;
}

export interface Unplaced {
  path: string;
  code: string;
  // field is the 0-based position and key of the field the path is in, if
  // it is in one.
  field?: { at: number; key: string };
}

export interface Attached {
  placed: Record<string, Placed[]>;
  unplaced: Unplaced[];
}

// attach places a refusal's errors on the draft they were refused for. The
// paths count fields by position in what was sent, so they are resolved
// against sent, and the ids they land on follow the fields wherever they
// move after.
export function attach(errors: Record<string, string>, sent: Draft): Attached {
  const out: Attached = { placed: {}, unplaced: [] };
  const put = (key: string, p: Placed) => (out.placed[key] ??= []).push(p);
  for (const [path, code] of Object.entries(errors)) {
    if (path === 'title') {
      put('title', { code });
      continue;
    }
    if (path === 'intro_markdown') {
      put('intro', { code });
      continue;
    }
    const m = /^fields\.(\d+)(?:\.([a-z_]+)(?:\.(\d+))?)?$/.exec(path);
    const at = m ? Number(m[1]) : -1;
    const df = m ? sent.fields[at] : undefined;
    if (!df) {
      out.unplaced.push({ path, code });
      continue;
    }
    const control = m![2];
    const drawn = control === 'key' || control === 'label' || propsFor(df.f.type).includes(control as Prop);
    if (control && drawn && (m![3] === undefined || control === 'options')) {
      put(`${df.id}.${control}`, m![3] === undefined ? { code } : { code, option: Number(m![3]) + 1 });
      continue;
    }
    out.unplaced.push({ path, code, field: { at, key: df.f.key } });
  }
  return out;
}
