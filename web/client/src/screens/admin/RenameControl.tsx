import { useRef, useState } from 'preact/hooks';
import type { HandleChange } from '../../api/dto';
import { isProblem } from '../../api/problem';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { handleMessage, isAnonymizedHandle } from './common/handle';
import { send } from './common/send';

// RenameControl changes an account's handle, on its detail in the users
// screen, for an admin (TKT-01M3P0CYR3, docs/05 "Changing a handle"): no
// cooldown, and the old handle is reserved for 90 days and redirects to the
// new one. UserDetail renders it only for an admin; the route's admin gate
// refuses anyone else whatever a screen offers.
//
// An anonymized account cannot be renamed, so it is offered no field.

export const renamedHint =
  'The old handle is kept for 90 days and sends visitors to the new one. Nobody else can take it until then.';

const conflictWords =
  'This account cannot be renamed. It may have been anonymized, or renamed by someone else. Reload to see it as it is now.';
const notFoundWords = 'No account has this handle now. It may have been renamed. Reload the list.';

export function RenameControl({
  handle,
  url,
  onRenamed,
}: {
  handle: string;
  // The account's address, /api/v1/admin/users/{handle}.
  url: string;
  onRenamed: (change: HandleChange) => void;
}) {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);

  if (isAnonymizedHandle(handle)) {
    return (
      <p class="admin-muted" data-rename-anonymized>
        An anonymized account cannot be renamed.
      </p>
    );
  }

  const next = value.trim();
  // The server decides "same" without case, after trimming.
  const unchanged = next === '' || next.toLowerCase() === handle.toLowerCase();

  const rename = async () => {
    const r = await send<HandleChange>('POST', `${url}/rename`, { handle: next });
    if (r.ok) {
      setError(null);
      setFailed(null);
      onRenamed(r.value);
      return;
    }
    const code = r.error.fieldErrors.handle;
    if (isProblem(r.error, 'validation-failed') && code) {
      // The question named a handle the admin now has to change, so it
      // closes, and focus goes to the field that says why.
      setError(handleMessage(code));
      setFailed(null);
      input.current?.focus();
      return true;
    }
    setError(null);
    if (isProblem(r.error, 'conflict')) setFailed(conflictWords);
    else if (isProblem(r.error, 'not-found')) setFailed(notFoundWords);
    else setFailed(r.message);
    return false;
  };

  return (
    <div class="admin-role" data-rename>
      <Field label="Change handle" hint={renamedHint} error={error ?? undefined}>
        {(c) => (
          <input
            {...c}
            ref={input}
            type="text"
            autocomplete="off"
            autocapitalize="none"
            spellcheck={false}
            value={value}
            onInput={(e) => setValue((e.target as HTMLInputElement).value)}
          />
        )}
      </Field>
      <ConfirmButton label="Rename" prompt={`Rename @${handle} to @${next}?`} disabled={unchanged} onConfirm={rename} />
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </div>
  );
}
