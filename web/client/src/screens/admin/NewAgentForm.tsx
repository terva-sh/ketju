import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import type { CreatedAgent } from '../../api/dto';
import { isProblem } from '../../api/problem';
import { Field } from './common/Field';
import { send } from './common/send';

// NewAgentForm creates an agent account (TKT-01M3QH3GS2, docs/07 "Agents on
// the API"), on the users tab, for an admin.
//
// It holds no token. The creation answer may carry the agent's first one,
// and the form passes the whole answer up through onCreated without keeping
// or drawing any of it: the token is shown by the new agent's detail, so it
// goes when that detail does (Terva review 1488, finding 1).
//
// A taken handle and a refused one get words of their own, because send's
// words for a conflict ("Someone else changed this") would mislead here.
const handleMax = 30; // domain.HandleMaxLen
const displayNameMax = 100; // users.DisplayNameMaxLen

export function NewAgentForm({ onCreated }: { onCreated: (created: CreatedAgent) => void }) {
  const [open, setOpen] = useState(false);
  const [handle, setHandle] = useState('');
  const [name, setName] = useState('');
  const [issue, setIssue] = useState(true);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const first = useRef<HTMLInputElement>(null);
  const opened = useRef(false);
  // In the commit that opens or closes the form: New agent goes as it
  // opens, so a plain effect left a key pressed before the next frame on
  // nothing (TKT-01M3V7CFJ5).
  useLayoutEffect(() => {
    if (open) first.current?.focus();
    else if (opened.current) opener.current?.focus();
    opened.current = opened.current || open;
  }, [open]);

  const reset = () => {
    setHandle('');
    setName('');
    setIssue(true);
    setFailed(null);
  };
  const close = () => {
    reset();
    setOpen(false);
  };
  const submit = async (e: Event) => {
    e.preventDefault();
    setBusy(true);
    try {
      const r = await send<CreatedAgent>('POST', '/api/v1/admin/users', {
        kind: 'agent',
        handle,
        display_name: name,
        issue_credential: issue,
      });
      if (!r.ok) {
        setFailed(
          isProblem(r.error, 'conflict')
            ? 'That handle is taken.'
            : isProblem(r.error, 'validation-failed')
              ? 'That is not a valid handle, or it is reserved.'
              : r.message,
        );
        return;
      }
      reset();
      opened.current = false; // the new agent's detail takes focus, not the opener
      setOpen(false);
      onCreated(r.value);
    } finally {
      setBusy(false);
    }
  };

  if (!open) {
    return (
      <div class="admin-actions" data-new-agent>
        <button type="button" class="admin-button" ref={opener} onClick={() => setOpen(true)}>
          New agent
        </button>
      </div>
    );
  }
  return (
    <form class="admin-new-agent" onSubmit={submit} aria-label="New agent" data-new-agent>
      <h2>New agent</h2>
      <Field label="Handle" hint="3 to 30 characters: a to z, 0 to 9, _ and -.">
        {(c) => (
          <input
            {...c}
            ref={first}
            type="text"
            required
            maxLength={handleMax}
            autocomplete="off"
            autoCapitalize="off"
            spellcheck={false}
            value={handle}
            onInput={(e) => setHandle((e.target as HTMLInputElement).value)}
          />
        )}
      </Field>
      <Field label="Display name" hint="Empty uses the handle.">
        {(c) => (
          <input
            {...c}
            type="text"
            maxLength={displayNameMax}
            autocomplete="off"
            value={name}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
          />
        )}
      </Field>
      <label class="admin-check">
        <input type="checkbox" checked={issue} onChange={(e) => setIssue((e.target as HTMLInputElement).checked)} />
        Issue a token now
      </label>
      <div class="admin-actions">
        <button type="submit" class="admin-button" disabled={busy}>
          Create agent
        </button>
        <button type="button" class="admin-button" disabled={busy} onClick={close}>
          Cancel
        </button>
      </div>
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </form>
  );
}
