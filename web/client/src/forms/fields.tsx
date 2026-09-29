import type { ComponentChildren, FunctionComponent } from 'preact';
import type { FormField } from '../api/dto';

// The one map from a form field's type to how the client draws it (docs/05
// "Forms", TKT-01M29CP2NG). This is the only file in the client that names
// a field type, as internal/forms's types table is the only place the
// server does: adding a type is an entry there and an entry here. A type
// missing from this map is not skipped; FormView draws a notice in its place
// and will not submit.

// Value is an answer as the form holds it and sends it: a string for a line,
// a paragraph or a single choice, a boolean for a box, a list for a multiple
// choice.
export type Value = string | boolean | string[];

// ControlProps is what FormView hands a field's control. id names the
// control a label points at and focus goes to (the first box of a group);
// describedBy and invalid tie the field's error text to it.
export interface ControlProps {
  field: FormField;
  value: Value | undefined;
  onChange: (v: Value) => void;
  // Locked, or not being filled in: shown, not editable.
  readOnly: boolean;
  id: string;
  describedBy: string | undefined;
  invalid: boolean;
}

export interface FieldKind {
  // answers is false for a type that shows something and takes no answer:
  // nothing is ever sent for it, since the server refuses any value.
  answers: boolean;
  // empty is the value of an unanswered field, sent as it is: the server
  // reads an empty string, an unticked box and an empty list as no answer.
  empty: (f: FormField) => Value;
  Control: FunctionComponent<ControlProps>;
  // show is an answer as read mode prints it.
  show: (v: unknown) => string;
}

// Label is a field's label, marked when an answer is required. A locked
// field is never held to required (docs/05), so it is not marked.
function Label({ field, id, as = 'label' }: { field: FormField; id?: string; as?: 'label' | 'legend' }) {
  const text: ComponentChildren = (
    <>
      {field.label}
      {field.required && !field.locked && <span class="form-required"> (required)</span>}
    </>
  );
  return as === 'legend' ? <legend class="form-label">{text}</legend> : <label class="form-label" for={id}>{text}</label>;
}

// autocomplete is the browser's hint for a field prefilled from a claim, so
// it offers the same thing the provider would have.
const autocomplete: Record<string, string> = { name: 'name', email: 'email', preferred_username: 'username' };

const str = (v: Value | undefined) => (typeof v === 'string' ? v : '');
const showText = (v: unknown) => (typeof v === 'string' && v.trim() !== '' ? v : '—');

function line(inputType: 'text' | 'email'): FieldKind {
  return {
    answers: true,
    empty: () => '',
    show: showText,
    Control: ({ field, value, onChange, readOnly, id, describedBy, invalid }) => (
      <>
        <Label field={field} id={id} />
        <input
          id={id}
          class="form-input"
          type={inputType}
          value={str(value)}
          readOnly={readOnly}
          aria-readonly={readOnly || undefined}
          aria-required={(field.required && !field.locked) || undefined}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          autocomplete={autocomplete[field.from_claim] ?? (inputType === 'email' ? 'email' : 'off')}
          onInput={(e) => onChange((e.currentTarget as HTMLInputElement).value)}
        />
      </>
    ),
  };
}

export const kinds: Record<string, FieldKind> = {
  text: line('text'),
  email: line('email'),

  textarea: {
    answers: true,
    empty: () => '',
    show: showText,
    Control: ({ field, value, onChange, readOnly, id, describedBy, invalid }) => {
      const limit = `${id}-limit`;
      return (
        <>
          <Label field={field} id={id} />
          <textarea
            id={id}
            class="form-input form-textarea"
            rows={5}
            value={str(value)}
            readOnly={readOnly}
            aria-readonly={readOnly || undefined}
            aria-required={field.required || undefined}
            aria-invalid={invalid || undefined}
            aria-describedby={[describedBy, field.max > 0 ? limit : ''].filter(Boolean).join(' ') || undefined}
            onInput={(e) => onChange((e.currentTarget as HTMLTextAreaElement).value)}
          />
          {field.max > 0 && (
            <p id={limit} class="form-hint">
              Up to {field.max} characters.
            </p>
          )}
        </>
      );
    },
  },

  checkbox: {
    answers: true,
    empty: () => false,
    show: (v) => (v === true ? 'Yes' : 'No'),
    Control: ({ field, value, onChange, readOnly, id, describedBy, invalid }) => (
      <div class="form-check">
        <input
          id={id}
          type="checkbox"
          checked={value === true}
          disabled={readOnly}
          aria-required={field.required || undefined}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          onChange={(e) => onChange((e.currentTarget as HTMLInputElement).checked)}
        />
        <Label field={field} id={id} />
      </div>
    ),
  },

  // A single choice is a native select; a multiple one is a group of
  // boxes, because a multiple select is close to unusable on a phone.
  select: {
    answers: true,
    empty: (f) => (f.multiple ? [] : ''),
    show: (v) => (Array.isArray(v) ? (v.length ? v.join(', ') : '—') : showText(v)),
    Control: ({ field, value, onChange, readOnly, id, describedBy, invalid }) => {
      if (!field.multiple) {
        return (
          <>
            <Label field={field} id={id} />
            <select
              id={id}
              class="form-input form-select"
              value={str(value)}
              disabled={readOnly}
              aria-required={field.required || undefined}
              aria-invalid={invalid || undefined}
              aria-describedby={describedBy}
              onChange={(e) => onChange((e.currentTarget as HTMLSelectElement).value)}
            >
              <option value="">Choose…</option>
              {field.options.map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </select>
          </>
        );
      }
      const chosen = Array.isArray(value) ? value : [];
      // The first box takes the field's id, so the error's focus lands there.
      const boxId = (i: number) => (i === 0 ? id : `${id}-${i}`);
      // Kept in the options' order, whatever order they were ticked in.
      const toggle = (o: string, on: boolean) =>
        onChange(field.options.filter((x) => (x === o ? on : chosen.includes(x))));
      return (
        <fieldset class="form-group" aria-describedby={describedBy}>
          <Label field={field} as="legend" />
          {field.options.map((o, i) => (
            <div class="form-check" key={o}>
              <input
                id={boxId(i)}
                type="checkbox"
                checked={chosen.includes(o)}
                disabled={readOnly}
                aria-invalid={invalid || undefined}
                aria-describedby={describedBy}
                onChange={(e) => toggle(o, (e.currentTarget as HTMLInputElement).checked)}
              />
              <label for={boxId(i)}>{o}</label>
            </div>
          ))}
        </fieldset>
      );
    },
  },

  // A notice is display only. Its html is the server's render of its
  // markdown (internal/render), sanitized there as a post is, so it is set
  // as it is, remote images already links.
  markdown_notice: {
    answers: false,
    empty: () => '',
    show: () => '',
    Control: ({ field, id }) => (
      <div class="form-notice" id={id}>
        {field.label && <p class="form-notice-title">{field.label}</p>}
        <div class="form-notice-body" dangerouslySetInnerHTML={{ __html: field.html }} />
      </div>
    ),
  },
};

// kindOf is the kind for a field, or undefined for a type this client does
// not know.
export function kindOf(f: FormField): FieldKind | undefined {
  return Object.hasOwn(kinds, f.type) ? kinds[f.type] : undefined;
}
