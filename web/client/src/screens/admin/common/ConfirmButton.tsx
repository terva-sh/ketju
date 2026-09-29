import { useState } from 'preact/hooks';

// ConfirmButton is an action that asks first: the first press shows what
// will happen with Confirm and Cancel beside it, and only Confirm acts. The
// admin screens use it for anything that cannot be taken back in one step,
// such as suspending a member or deleting a forum. While the action runs
// both buttons are disabled, so a second press cannot send it twice.
export function ConfirmButton({
  label,
  prompt,
  confirmLabel = 'Confirm',
  danger = false,
  onConfirm,
}: {
  label: string;
  // What will happen, as a sentence: "Suspend alice for 7 days?"
  prompt: string;
  confirmLabel?: string;
  danger?: boolean;
  onConfirm: () => Promise<unknown> | void;
}) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const cls = danger ? 'admin-button danger' : 'admin-button';

  if (!asking) {
    return (
      <button type="button" class={cls} onClick={() => setAsking(true)}>
        {label}
      </button>
    );
  }
  const confirm = async () => {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
      setAsking(false);
    }
  };
  return (
    <span class="admin-confirm" role="group" aria-label={label}>
      <span>{prompt}</span>
      <button type="button" class={cls} disabled={busy} onClick={confirm}>
        {confirmLabel}
      </button>
      <button type="button" class="admin-button" disabled={busy} onClick={() => setAsking(false)}>
        Cancel
      </button>
    </span>
  );
}
