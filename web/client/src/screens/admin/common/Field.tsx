import type { ComponentChildren } from 'preact';
import { useId } from 'preact/hooks';

// Field is one labelled control. The label, the hint and the error are tied
// to the control by id, so a screen reader announces all three when the
// control takes focus, and a screen cannot forget to. children receives the
// attributes to spread on the control.
export interface ControlProps {
  id: string;
  'aria-describedby'?: string;
  'aria-invalid'?: boolean;
}

export function Field({
  label,
  hint,
  error,
  children,
}: {
  label: string;
  hint?: string;
  // Set when the last attempt refused this field.
  error?: string;
  children: (control: ControlProps) => ComponentChildren;
}) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const described = [hint ? hintId : '', error ? errorId : ''].filter(Boolean).join(' ');
  return (
    <div class="admin-field">
      <label for={id}>{label}</label>
      {hint && (
        <p id={hintId} class="admin-hint">
          {hint}
        </p>
      )}
      {children({ id, 'aria-describedby': described || undefined, 'aria-invalid': error ? true : undefined })}
      {error && (
        <p id={errorId} class="admin-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
