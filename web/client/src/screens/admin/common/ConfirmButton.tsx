import { useEffect, useRef, useState } from 'preact/hooks';
import { isAbort } from '../../../api/problem';
import { sentence } from './send';

// ConfirmButton is an action that asks first: the first press shows what
// will happen with Confirm and Cancel beside it, and only Confirm acts. The
// admin screens use it for anything that cannot be taken back in one step,
// such as suspending a member or deleting a forum. While the action runs
// both buttons are disabled, so a second press cannot send it twice.
//
// onConfirm reports a failure by rejecting, or by resolving to false when it
// has shown the failure itself (as a caller of send does). Either way the
// confirmation stays open so the admin can try again or cancel, and a
// rejection is shown beside it in send's words. Only success closes it.
//
// Focus follows the question, so a keyboard user is never left on a button
// that has gone: opening it focuses Cancel, the choice that changes
// nothing, and closing it focuses the action's button again. An action that
// moved focus itself, to what it opened, keeps it there: the button is
// focused only when focus went with the question, to the page's body.
export function ConfirmButton({
  label,
  prompt,
  confirmLabel = 'Confirm',
  danger = false,
  disabled = false,
  onConfirm,
}: {
  label: string;
  // What will happen, as a sentence: "Suspend alice for 7 days?"
  prompt: string;
  confirmLabel?: string;
  danger?: boolean;
  // disabled keeps the action from being asked for at all, as when a
  // required note is still empty.
  disabled?: boolean;
  onConfirm: () => Promise<boolean | void> | boolean | void;
}) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const cls = danger ? 'admin-button danger' : 'admin-button';
  const action = useRef<HTMLButtonElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const opened = useRef(false);
  useEffect(() => {
    if (asking) cancelButton.current?.focus();
    else if (opened.current && (document.activeElement === null || document.activeElement === document.body)) {
      action.current?.focus();
    }
    opened.current = opened.current || asking;
  }, [asking]);

  if (!asking) {
    return (
      <button type="button" class={cls} ref={action} disabled={disabled} onClick={() => setAsking(true)}>
        {label}
      </button>
    );
  }
  const confirm = async () => {
    setBusy(true);
    setFailed(null);
    let done = false;
    try {
      done = (await onConfirm()) !== false;
    } catch (err) {
      if (!isAbort(err)) setFailed(sentence(err));
    }
    setBusy(false);
    if (done) setAsking(false);
  };
  const cancel = () => {
    setFailed(null);
    setAsking(false);
  };
  return (
    <span class="admin-confirm" role="group" aria-label={label}>
      <span>{prompt}</span>
      <button type="button" class={cls} disabled={busy} onClick={confirm}>
        {confirmLabel}
      </button>
      <button type="button" class="admin-button" ref={cancelButton} disabled={busy} onClick={cancel}>
        Cancel
      </button>
      {failed && (
        <span class="admin-error" role="alert">
          {failed}
        </span>
      )}
    </span>
  );
}
