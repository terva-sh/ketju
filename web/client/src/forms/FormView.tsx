import type { ComponentChildren } from 'preact';
import { useId, useLayoutEffect, useRef } from 'preact/hooks';
import type { Form, FormField } from '../api/dto';
import { Icon } from '../components/Icon';
import { kindOf, type Value } from './fields';
import { fieldMessage } from './messages';
import './forms.css';

// FormView draws a form version from its schema (docs/05 "Forms"): the apply
// screen fills it in, and the form editor's preview (TKT-01M29CP2P1) and the
// review screen (TKT-01M3NRX2NN) will show it from the admin chunk. Every
// field comes from form.fields and is drawn through fields.tsx's map; this
// file adds no field of its own and names no field type.

export type Values = Record<string, Value>;

// fill takes answers and submits them; preview draws the controls with no
// submit; read shows answers already given.
export type FormMode = 'fill' | 'preview' | 'read';

export interface FormViewProps {
  form: Form;
  mode: FormMode;
  // The applicant's claims, keyed by the field they prefill. A locked field
  // shows its entry and is never sent.
  prefill?: Record<string, string>;
  values?: Values;
  // The code for each field the last submission was refused over, less any
  // the reader has since changed.
  errors?: Record<string, string>;
  // refusals counts the submissions refused with field errors. Each time it
  // goes up, focus moves to the first field in errors. It is a count, not
  // the errors themselves, so clearing one error as the reader corrects it
  // does not pull focus away from where they are typing (review 1456).
  refusals?: number;
  // read mode's answers, as they were stored.
  answers?: Record<string, unknown>;
  // The sign-in provider a locked field's value came from, for its label.
  provider?: string;
  onChange?: (key: string, v: Value) => void;
  onSubmit?: () => void;
  busy?: boolean;
  submitLabel?: string;
  // A message about the form as a whole, shown above Submit.
  message?: ComponentChildren;
}

// initialValues is the form before anyone has touched it: each answering
// field empty, or holding its prefill when it takes a line of text.
export function initialValues(form: Form, prefill: Record<string, string> = {}): Values {
  const out: Values = {};
  for (const f of form.fields) {
    const k = kindOf(f);
    if (!k?.answers) continue;
    const empty = k.empty(f);
    const p = Object.hasOwn(prefill, f.key) ? prefill[f.key] : undefined;
    out[f.key] = typeof empty === 'string' && p !== undefined ? p : empty;
  }
  return out;
}

// answersOf is the answers a submission sends: one per answering field,
// empty when unanswered, and none for a locked field, which the server
// fills from the claim whatever is sent, or for one that only shows
// something, which the server refuses any value for.
export function answersOf(form: Form, values: Values): Values {
  const out: Values = {};
  for (const f of form.fields) {
    const k = kindOf(f);
    if (!k?.answers || f.locked) continue;
    out[f.key] = Object.hasOwn(values, f.key) ? values[f.key] : k.empty(f);
  }
  return out;
}

// unknownFields are the fields whose type this client cannot draw.
export function unknownFields(form: Form): FormField[] {
  return form.fields.filter((f) => !kindOf(f));
}

export function FormView(props: FormViewProps) {
  const { form, mode, prefill = {}, values = {}, errors = {}, refusals = 0, provider, onChange, onSubmit, busy, message } =
    props;
  const base = useId();
  const root = useRef<HTMLFormElement>(null);
  const idOf = (i: number) => `${base}-f${i}`;
  const unknown = unknownFields(form);

  // After a refused submission, focus goes to the first field refused, in
  // the order the form draws them, so a reader starts where the problem is.
  // In the commit that draws the refusal: a plain effect left focus on
  // Submit until the next frame, and Enter there submitted again
  // (TKT-01M3V7CFJ5).
  useLayoutEffect(() => {
    if (refusals === 0) return;
    const i = form.fields.findIndex((f) => Object.hasOwn(errors, f.key));
    if (i < 0) return;
    root.current?.querySelector<HTMLElement>(`[id="${idOf(i)}"]`)?.focus();
  }, [refusals]);

  // Errors naming no field drawn here, the whole schema's "" among them,
  // are said above Submit rather than lost.
  const stray = Object.entries(errors).filter(([k]) => !form.fields.some((f) => f.key === k));

  const intro = form.intro_html ? <div class="form-intro" dangerouslySetInnerHTML={{ __html: form.intro_html }} /> : null;

  if (mode === 'read') {
    const answers = props.answers ?? {};
    return (
      <div class="form-view form-read">
        {intro}
        <dl>
          {form.fields.map((f) => {
            const k = kindOf(f);
            if (k && !k.answers) return null;
            return (
              <div class="form-answer" key={f.key} data-field={f.key}>
                <dt>{f.label || f.key}</dt>
                <dd>{k ? k.show(answers[f.key]) : JSON.stringify(answers[f.key] ?? null)}</dd>
              </div>
            );
          })}
        </dl>
      </div>
    );
  }

  return (
    <form
      ref={root}
      class="form-view"
      noValidate
      aria-busy={busy || undefined}
      onSubmit={(e) => {
        e.preventDefault();
        if (mode === 'fill' && !busy && unknown.length === 0) onSubmit?.();
      }}
    >
      {intro}
      {form.fields.map((f, i) => {
        const id = idOf(i);
        const k = kindOf(f);
        if (!k) {
          return (
            <div class="form-field form-unknown" key={f.key} data-field={f.key} role="note">
              <p>
                <strong>{f.label || f.key}</strong> is a kind of field this site cannot show yet ({f.type}). The form
                cannot be sent until it is fixed.
              </p>
            </div>
          );
        }
        const code = Object.hasOwn(errors, f.key) ? errors[f.key] : undefined;
        const errorId = `${id}-error`;
        const lockId = `${id}-lock`;
        const describedBy = [f.locked ? lockId : '', code ? errorId : ''].filter(Boolean).join(' ') || undefined;
        const value = f.locked ? (Object.hasOwn(prefill, f.key) ? prefill[f.key] : '') : values[f.key];
        return (
          <div class={`form-field${code ? ' has-error' : ''}`} key={f.key} data-field={f.key}>
            <k.Control
              field={f}
              value={value}
              readOnly={f.locked}
              id={id}
              describedBy={describedBy}
              invalid={!!code}
              onChange={(v) => onChange?.(f.key, v)}
            />
            {f.locked && (
              <p id={lockId} class="form-lock" data-lock>
                <Icon name="lock" size={14} />
                <span>From your {provider ? `${provider} ` : 'sign-in '}account</span>
              </p>
            )}
            {code && (
              <p id={errorId} class="form-error">
                {fieldMessage(code, f)}
              </p>
            )}
          </div>
        );
      })}
      {(message || stray.length > 0) && (
        <div class="form-message" role="alert">
          {message}
          {stray.map(([k, code]) => (
            <p key={k}>{fieldMessage(code)}</p>
          ))}
        </div>
      )}
      {mode === 'fill' && (
        <div class="form-actions">
          <button type="submit" class="form-submit" disabled={busy || unknown.length > 0}>
            {busy ? 'Sending…' : (props.submitLabel ?? 'Submit')}
          </button>
        </div>
      )}
    </form>
  );
}
