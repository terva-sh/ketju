import { useEffect, useRef, useState } from 'preact/hooks';
import { apiSend } from '../../api/client';
import type { Pinned, ThreadHeader } from '../../api/dto';
import { asProblem, isProblem, problemMessage, revokesAccess } from '../../api/problem';
import type { ModerateKind } from '../../shell/threadActions';
import { storeThreadHeader } from '../../state/threads';

// The longest reason a lock or unlock takes, in characters, as the server
// counts them (docs/07). maxlength counts UTF-16 code units, never fewer
// than the server's characters, so a reason the field allows is never too
// long.
export const maxModerateReason = 500;

const titles: Record<ModerateKind, string> = {
  lock: 'Lock this thread',
  unlock: 'Unlock this thread',
  pin: 'Pin this thread',
  unpin: 'Unpin this thread?',
};
const buttons: Record<ModerateKind, string> = { lock: 'Lock', unlock: 'Unlock', pin: 'Pin', unpin: 'Unpin' };

export const moderateChanged = 'This thread changed while you were working.';
export const moderateForbidden = 'You can no longer moderate this thread.';

// moderateRefusal is what a refused action says. A conflict is another
// moderator, or an archive, getting there first; forbidden is a grant
// withdrawn or a move since the page loaded. Either way the page is asked
// for again, so the thread head shows the state that caused it.
export function moderateRefusal(kind: ModerateKind, err: unknown): string {
  const p = asProblem(err);
  switch (p.type) {
    case 'conflict':
      return moderateChanged;
    case 'forbidden':
      return moderateForbidden;
    case 'validation-failed':
      return kind === 'pin' ? 'Choose an end after now, or leave it empty.' : problemMessage(p).text;
    default:
      return problemMessage(p).text;
  }
}

// ModerateDialog is the panel under the thread head for one of the thread
// moderation routes (docs/07 "Locking and pinning threads"): lock and unlock
// take an optional reason, pin a scope and an optional end, and unpin only
// asks, since it sends the active pin's scope. The answer is the thread's
// header as it now reads, which replaces the cached one (storeThreadHeader),
// and the page is then asked for again (onChanged).
//
// It is inline, not modal: it keeps the thread head, where the lock and pin
// notes change, in view, and needs no focus trap. Focus goes to its first
// control when it opens, and to returnTo, the thread's title, when it closes
// with focus inside it, since on a phone the item that opened it is gone.
//
// offered says whether the thread's state still offers this kind
// (moderateKinds). After a conflict or a refusal the page is asked for
// again, and once the kind is no longer offered, the panel keeps its words
// and a Close button and drops the form, whose button would only be
// refused.
export function ModerateDialog({
  thread,
  kind,
  pinned,
  staff,
  offered,
  returnTo,
  onChanged,
  onClose,
}: {
  thread: string;
  kind: ModerateKind;
  pinned: Pinned | null;
  staff: boolean;
  offered: boolean;
  returnTo: () => HTMLElement | null;
  onChanged: () => void;
  onClose: () => void;
}) {
  const root = useRef<HTMLElement>(null);
  const [reason, setReason] = useState('');
  const [scope, setScope] = useState<'forum' | 'global'>('forum');
  const [until, setUntil] = useState('');
  const [sending, setSending] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);
  // Set synchronously, so a second press before the render that disables
  // the button sends nothing.
  const inFlight = useRef(false);

  useEffect(() => {
    root.current?.querySelector<HTMLElement>('textarea, input, button')?.focus();
    const panel = root.current;
    return () => {
      const at = document.activeElement;
      if (at === null || at === document.body || panel?.contains(at)) returnTo()?.focus({ preventScroll: true });
    };
  }, []);
  // The form, and the button that had focus, are replaced by Close.
  useEffect(() => {
    const at = document.activeElement;
    if (!offered && (at === null || at === document.body || root.current?.contains(at)))
      root.current?.querySelector<HTMLElement>('[data-moderate-close]')?.focus();
  }, [offered]);

  const untilMs = until ? new Date(until).getTime() : null;
  const badUntil = untilMs !== null && !Number.isFinite(untilMs);

  const body = (): unknown => {
    switch (kind) {
      case 'lock':
      case 'unlock':
        return reason.trim() ? { reason } : {};
      case 'pin':
        return { scope: staff ? scope : 'forum', until: untilMs };
      case 'unpin':
        return { scope: pinned?.scope };
    }
  };

  const send = async (e: Event) => {
    e.preventDefault();
    if (!offered || badUntil || inFlight.current) return;
    if (kind === 'unpin' && !pinned) return;
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    try {
      const header = await apiSend<ThreadHeader>(
        'POST',
        `/api/v1/admin/threads/${encodeURIComponent(thread)}/${kind}`,
        body(),
        { status: 200 },
      );
      storeThreadHeader(thread, header);
      onChanged();
      onClose();
    } catch (err) {
      setRefused(moderateRefusal(kind, err));
      if (isProblem(err, 'conflict') || revokesAccess(err)) onChanged();
    } finally {
      inFlight.current = false;
      setSending(false);
    }
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      onClose();
    }
  };

  const title = `moderate-title-${thread}`;
  const hint = `moderate-hint-${thread}`;
  const cancel = (
    <button type="button" data-moderate-close onClick={onClose}>
      {offered ? 'Cancel' : 'Close'}
    </button>
  );
  const alert = refused && (
    <p class="moderate-refused" role="alert">
      {refused}
    </p>
  );

  return (
    <section
      ref={root}
      class="moderate-panel"
      role="group"
      aria-labelledby={title}
      data-moderate={kind}
      onKeyDown={onKeyDown}
    >
      <h2 id={title}>{titles[kind]}</h2>
      {offered ? (
        <form onSubmit={send}>
          {(kind === 'lock' || kind === 'unlock') && (
            <label class="moderate-field">
              Reason (optional)
              <textarea
                value={reason}
                rows={2}
                maxLength={maxModerateReason}
                onInput={(e) => setReason(e.currentTarget.value)}
              />
            </label>
          )}
          {kind === 'pin' && (
            <>
              <fieldset>
                <legend>Where</legend>
                <label>
                  <input
                    type="radio"
                    name={`moderate-scope-${thread}`}
                    value="forum"
                    checked={scope === 'forum'}
                    onChange={() => setScope('forum')}
                  />
                  In this forum
                </label>
                {staff && (
                  <label>
                    <input
                      type="radio"
                      name={`moderate-scope-${thread}`}
                      value="global"
                      checked={scope === 'global'}
                      onChange={() => setScope('global')}
                    />
                    On every forum
                  </label>
                )}
              </fieldset>
              <label class="moderate-field">
                Until (optional)
                <input
                  type="datetime-local"
                  value={until}
                  aria-describedby={hint}
                  aria-invalid={badUntil}
                  onInput={(e) => setUntil(e.currentTarget.value)}
                />
              </label>
              <p id={hint} class="moderate-hint">
                Empty keeps it pinned until it is unpinned.
              </p>
            </>
          )}
          {alert}
          <div class="moderate-buttons">
            {cancel}
            <button type="submit" class="moderate-send" disabled={sending || badUntil}>
              {buttons[kind]}
            </button>
          </div>
        </form>
      ) : (
        <>
          {alert ?? <p class="moderate-refused">{moderateChanged}</p>}
          <div class="moderate-buttons">{cancel}</div>
        </>
      )}
    </section>
  );
}
