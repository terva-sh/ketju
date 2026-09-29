import { useId } from 'preact/hooks';
import type { FormSchemaField } from '../../../api/dto';
import { fieldMessage } from '../../../forms/messages';
import { Field } from '../common/Field';
import { claims, propsFor, typeLabel, type DraftField, type Placed, type Prop } from './draft';

// FieldEditor is one field of the draft: its label and key, the property
// controls its type offers (draft.ts propsFor), and the buttons that move
// and remove it. It is a fieldset named for its position and type, so the
// controls, which every field repeats, are announced with the field they
// belong to.
//
// data-fe-focus marks the controls the editor moves focus to after an add,
// a move or a remove (FormEditor.tsx).

export type Change = Partial<FormSchemaField> & { optionsText?: string };

// words is what a control's errors say. An option's error names the
// option, since the box holds them all.
export function words(errors: Placed[] | undefined): string | undefined {
  if (!errors?.length) return undefined;
  return errors.map((e) => (e.option ? `Option ${e.option}: ${fieldMessage(e.code)}` : fieldMessage(e.code))).join(' ');
}

export function FieldEditor({
  field,
  at,
  count,
  errors,
  onChange,
  onMove,
  onRemove,
}: {
  field: DraftField;
  at: number;
  count: number;
  // The errors placed on this field, by control.
  errors: Record<string, Placed[] | undefined>;
  onChange: (control: string, change: Change) => void;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}) {
  const { f } = field;
  const props = propsFor(f.type);
  const has = (p: Prop) => props.includes(p);
  const display = has('markdown');
  const err = (control: string) => words(errors[control]);

  return (
    <fieldset class="fe-field" data-fe-id={field.id} data-draft-field={at}>
      <legend>
        Field {at + 1}: {typeLabel(f.type)}
      </legend>
      <Field
        label={display ? 'Heading (optional)' : 'Label'}
        hint={display ? 'Shown above the notice.' : 'The question, as the applicant reads it.'}
        error={err('label')}
      >
        {(c) => (
          <input
            {...c}
            type="text"
            data-fe-focus="label"
            value={f.label ?? ''}
            onInput={(e) => onChange('label', { label: (e.currentTarget as HTMLInputElement).value })}
          />
        )}
      </Field>
      <Field
        label="Key"
        hint="Names the answer in each application. Lowercase letters, digits and underscores, starting with a letter."
        error={err('key')}
      >
        {(c) => (
          <input
            {...c}
            type="text"
            spellcheck={false}
            autocomplete="off"
            value={f.key}
            onInput={(e) => onChange('key', { key: (e.currentTarget as HTMLInputElement).value })}
          />
        )}
      </Field>
      {has('markdown') && (
        <Field label="Text" hint="Markdown, as in a post." error={err('markdown')}>
          {(c) => (
            <textarea
              {...c}
              rows={4}
              value={f.markdown ?? ''}
              onInput={(e) => onChange('markdown', { markdown: (e.currentTarget as HTMLTextAreaElement).value })}
            />
          )}
        </Field>
      )}
      {has('options') && (
        <Field label="Options" hint="One to a line, in the order they are offered." error={err('options')}>
          {(c) => (
            <textarea
              {...c}
              rows={4}
              value={field.options ?? ''}
              onInput={(e) => onChange('options', { optionsText: (e.currentTarget as HTMLTextAreaElement).value })}
            />
          )}
        </Field>
      )}
      {has('multiple') && (
        <Check
          label="Allow more than one choice"
          checked={!!f.multiple}
          error={err('multiple')}
          onChange={(v) => onChange('multiple', { multiple: v })}
        />
      )}
      {has('max') && (
        <Field label="Character limit" hint="Leave empty for the type's own limit." error={err('max')}>
          {(c) => (
            <input
              {...c}
              type="number"
              inputMode="numeric"
              min={1}
              class="fe-number"
              value={f.max ? String(f.max) : ''}
              onInput={(e) => {
                const v = (e.currentTarget as HTMLInputElement).value;
                onChange('max', { max: v === '' ? undefined : Number(v) });
              }}
            />
          )}
        </Field>
      )}
      {has('from_claim') && (
        <Field label="Prefill from the sign-in" hint="The applicant's account at their provider." error={err('from_claim')}>
          {(c) => (
            <select
              {...c}
              value={f.from_claim ?? ''}
              onChange={(e) => onChange('from_claim', { from_claim: (e.currentTarget as HTMLSelectElement).value })}
            >
              <option value="">Nothing</option>
              {claims.map((claim) => (
                <option key={claim} value={claim}>
                  {claim}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}
      {has('locked') && (
        <Check
          label="Locked to the sign-in's value"
          checked={!!f.locked}
          error={err('locked')}
          onChange={(v) => onChange('locked', { locked: v })}
        />
      )}
      {has('required') && (
        <Check
          label="Required"
          checked={!!f.required}
          error={err('required')}
          onChange={(v) => onChange('required', { required: v })}
        />
      )}
      <div class="admin-actions fe-field-actions">
        <button type="button" class="admin-button" data-fe-focus="up" disabled={at === 0} onClick={() => onMove(-1)}>
          Move up
        </button>
        <button
          type="button"
          class="admin-button"
          data-fe-focus="down"
          disabled={at === count - 1}
          onClick={() => onMove(1)}
        >
          Move down
        </button>
        <button type="button" class="admin-button danger" onClick={onRemove}>
          Remove
        </button>
      </div>
    </fieldset>
  );
}

// Check is a box with its label after it, and its error tied to it, as
// Field ties one to a line.
function Check({
  label,
  checked,
  error,
  onChange,
}: {
  label: string;
  checked: boolean;
  error?: string;
  onChange: (v: boolean) => void;
}) {
  const id = useId();
  return (
    <div class="admin-field fe-check">
      <span>
        <input
          id={id}
          type="checkbox"
          checked={checked}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${id}-error` : undefined}
          onChange={(e) => onChange((e.currentTarget as HTMLInputElement).checked)}
        />
        <label for={id}>{label}</label>
      </span>
      {error && (
        <p id={`${id}-error`} class="admin-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
