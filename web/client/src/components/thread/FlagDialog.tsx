import { useEffect, useRef, useState } from 'preact/hooks';
import { apiSend } from '../../api/client';
import type { FlagReceipt } from '../../api/dto';
import { asProblem, ProblemError, problemMessage } from '../../api/problem';
import { trapTab } from '../focus';

// The longest note a flag takes, in characters (code points), as the server
// counts it (threads.MaxFlagNote, utf8.RuneCountInString).
export const maxFlagNote = 1000;

const reasons = [
  ['spam', 'Spam'],
  ['abuse', 'Abuse'],
  ['off-topic', 'Off-topic'],
  ['other', 'Other'],
] as const;
type Reason = (typeof reasons)[number][0];

export const flagThanks = 'Thanks, a moderator will look.';

// flagRefusal is what a refused flag says. The form stays as filled, so the
// reader can try again or close it. unavailable means nothing was kept (the
// server answers a kept flag with the receipt, even when its auto-hide
// failed), so it is not a thanks.
export function flagRefusal(err: unknown): string {
  switch (asProblem(err).type) {
    case 'unavailable':
      return 'Your report could not be sent. Try again shortly.';
    case 'validation-failed':
      return 'Choose a reason, and say what is wrong when it is Other.';
    case 'conflict':
      return 'This post can no longer be flagged. It may have been removed or moved, or its thread archived.';
    case 'forbidden':
      // The page offers no flag on the reader's own post, so this is a race
      // such as a rename, or access to the thread withdrawn since it loaded.
      return 'You cannot flag this post. It is your own, or you no longer have access to it.';
    case 'not-found':
      return 'This post is no longer here.';
    default:
      return problemMessage(err).text;
  }
}

// FlagDialog reports post ordinal of thread to the moderators (docs/08
// "Flags"; POST /threads/{id}/flags). A 200 says the thanks and nothing
// else: no count, no outcome. Nothing on the page is refetched, since a
// fresh page could show the reporter the post going down, which is the
// outcome (TKT-01M3QQWA5Z).
//
// It is a modal dialog, as HelpOverlay is: focus starts inside, Tab stays
// inside, and Escape, Close or a press on the backdrop end it. Focus then
// goes to returnTo, the control the post handed over, if it is still in the
// page and drawn, or else to the post itself. The control that opened the
// dialog is not remembered at mount, because on a phone it is an item of a
// list that has closed by then (Terva review 1525 on PR #181).
export function FlagDialog({
  thread,
  ordinal,
  returnTo,
  onClose,
}: {
  thread: string;
  ordinal: number;
  returnTo: HTMLElement;
  onClose: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [reason, setReason] = useState<Reason | null>(null);
  const [note, setNote] = useState('');
  const [sending, setSending] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);
  const [received, setReceived] = useState(false);
  // Set synchronously, so a second press before the render that disables
  // Send sends nothing.
  const inFlight = useRef(false);

  useEffect(() => {
    root.current?.querySelector<HTMLElement>('input, button')?.focus();
    return () => {
      if (returnTo.isConnected && returnTo.offsetParent !== null) returnTo.focus({ preventScroll: true });
      else document.getElementById(`post-${ordinal}`)?.focus({ preventScroll: true });
    };
  }, []);
  // The form, and the Send that had focus, are replaced by the thanks.
  useEffect(() => {
    if (received) root.current?.querySelector<HTMLElement>('[data-flag-close]')?.focus();
  }, [received]);

  const chars = [...note].length;
  const long = chars > maxFlagNote;
  const ready = reason !== null && (reason !== 'other' || note.trim() !== '') && !long;

  const send = async (e: Event) => {
    e.preventDefault();
    if (!ready || inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    try {
      const r = await apiSend<FlagReceipt | undefined>('POST', `/api/v1/threads/${encodeURIComponent(thread)}/flags`, {
        ordinal,
        reason,
        note,
      });
      // The thanks is for the receipt and nothing else (Terva review 1538):
      // apiSend resolves on any 2xx, and a 204, or a body that is not
      // {"received": true}, is not the 200 the server answers a kept flag
      // with, so it is the server's fault and the form stays.
      if (r?.received !== true) throw new ProblemError('internal', 0);
      setReceived(true);
    } catch (err) {
      setRefused(flagRefusal(err));
    } finally {
      inFlight.current = false;
      setSending(false);
    }
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      onClose();
    } else if (e.key === 'Tab') trapTab(e, root.current);
  };
  const close = (
    <button type="button" class="flag-close" data-flag-close onClick={onClose}>
      Close
    </button>
  );
  const hint = `flag-note-hint-${ordinal}`;

  return (
    <div class="flag-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={root}
        class="flag-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="flag-title"
        data-flag-dialog={ordinal}
        onKeyDown={onKeyDown}
      >
        <h2 id="flag-title">Flag post #{ordinal}</h2>
        {received ? (
          <>
            <p class="flag-thanks">{flagThanks}</p>
            <div class="flag-buttons">{close}</div>
          </>
        ) : (
          <form onSubmit={send}>
            <fieldset>
              <legend>Why?</legend>
              {reasons.map(([value, label]) => (
                <label key={value}>
                  <input
                    type="radio"
                    name={`flag-reason-${ordinal}`}
                    value={value}
                    checked={reason === value}
                    onChange={() => setReason(value)}
                  />
                  {label}
                </label>
              ))}
            </fieldset>
            <label class="flag-note">
              Note
              <textarea
                value={note}
                rows={3}
                aria-describedby={hint}
                aria-invalid={long}
                onInput={(e) => setNote(e.currentTarget.value)}
              />
            </label>
            <p id={hint} class="flag-hint">
              Required for Other. At most {maxFlagNote} characters. <span class="flag-count">{chars}</span>
              {long && <span class="flag-long"> The note is too long.</span>}
            </p>
            {refused && (
              <p class="flag-refused" role="alert">
                {refused}
              </p>
            )}
            <div class="flag-buttons">
              {close}
              <button type="submit" class="flag-send" disabled={!ready || sending}>
                Send
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
