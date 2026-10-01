import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet, apiSend, forgetGets, peekGet } from '../../api/client';
import type { Forum, ForumTree, Pinned, SlowMode, ThreadHeader } from '../../api/dto';
import { asProblem, isProblem, problemMessage, revokesAccess } from '../../api/problem';
import { forumsUrl } from '../../screens/ForumIndex';
import type { PanelKind } from '../../shell/threadActions';
import { storeThreadHeader } from '../../state/threads';
import { canonicalThreadUrl } from '../../view';

// The longest reason a lock or unlock takes, in characters, as the server
// counts them (docs/07). maxlength counts UTF-16 code units, never fewer
// than the server's characters, so a reason the field allows is never too
// long.
export const maxModerateReason = 500;

// The longest title, in characters, as the server counts them
// (threads.MaxTitle).
export const maxTitle = 200;

// slowModeMax is the longest slow mode a thread takes, in seconds, as
// threads.MaxSlowModeSeconds is on the server.
export const slowModeMax = 86400;

// The slow mode choices, by seconds, and Custom, which takes any.
const slowChoices = [
  ['0', 'Off'],
  ['10', '10 seconds'],
  ['30', '30 seconds'],
  ['60', '1 minute'],
  ['300', '5 minutes'],
  ['custom', 'Custom'],
];

const titles: Record<PanelKind, string> = {
  lock: 'Lock this thread',
  unlock: 'Unlock this thread',
  pin: 'Pin this thread',
  unpin: 'Unpin this thread?',
  rename: 'Rename this thread',
  move: 'Move this thread',
  'slow-mode': 'Slow mode for this thread',
};
const buttons: Record<PanelKind, string> = {
  lock: 'Lock',
  unlock: 'Unlock',
  pin: 'Pin',
  unpin: 'Unpin',
  rename: 'Rename',
  move: 'Move',
  'slow-mode': 'Save',
};

export const moderateChanged = 'This thread changed while you were working.';
export const moderateForbidden = 'You can no longer moderate this thread.';
export const slowModeConflict = 'Slow mode is already set to that, or the thread is archived.';
export const slowModeInvalid = `Enter a whole number of seconds from 0 to ${slowModeMax}.`;

// moderateRefusal is what a refused action says. A conflict is another
// moderator, or an archive, getting there first; forbidden is a grant
// withdrawn or a move since the page loaded. Either way the page is asked
// for again, so the thread head shows the state that caused it.
//
// For a rename or a move, a conflict may also be the title or the forum the
// thread already has. A move's forbidden is usually a destination the viewer
// does not moderate, since the list is every forum they can read
// (destinations), and its not-found a destination gone since the list
// loaded.
//
// Slow mode is conflict for the value the thread already has too, which the
// client cannot foresee: a thread with no value of its own takes any.
export function moderateRefusal(kind: PanelKind, err: unknown): string {
  const p = asProblem(err);
  const named = kind === 'rename' || kind === 'move';
  const slow = kind === 'slow-mode';
  switch (p.type) {
    case 'conflict':
      return named ? 'This thread changed while you were working, or is already there.' : slow ? slowModeConflict : moderateChanged;
    case 'forbidden':
      return kind === 'move' ? 'You cannot move this thread there.' : named ? problemMessage(p).text : moderateForbidden;
    case 'validation-failed':
      return kind === 'pin'
        ? 'Choose an end after now, or leave it empty.'
        : kind === 'rename'
          ? `Enter a title of 1 to ${maxTitle} characters.`
          : slow
            ? slowModeInvalid
            : problemMessage(p).text;
    case 'not-found':
      return kind === 'move' ? 'That forum is no longer there.' : problemMessage(p).text;
    default:
      return problemMessage(p).text;
  }
}

// destinations is where a move may take a thread, from the forum tree the
// viewer is served (GET /api/v1/forums, which lists only the forums they may
// read): every forum but the thread's own, each child after its parent, and
// marked as one. An archived forum is listed only to an admin, since the
// server refuses a move into one to anyone else (docs/08, TKT-01M3N24RHD).
export function destinations(forums: Forum[], current: string, admin: boolean): [Forum, boolean][] {
  const out: [Forum, boolean][] = [];
  const ok = (f: Forum) => f.slug !== current && (admin || !f.archived);
  for (const f of forums) {
    if (ok(f)) out.push([f, false]);
    for (const c of f.children) if (ok(c)) out.push([c, true]);
  }
  return out;
}

// ModerateDialog is the panel under the thread head for one of the thread
// moderation routes (docs/07 "Locking and pinning threads"): lock and unlock
// take an optional reason, pin a scope and an optional end, and unpin only
// asks, since it sends the active pin's scope. The answer is the thread's
// header as it now reads, which replaces the cached one (storeThreadHeader),
// and the page is then asked for again (onChanged).
//
// Rename takes a title and sends PATCH /api/v1/threads/{id}; move takes a
// forum from destinations and sends POST /api/v1/admin/threads/{id}/move.
// Their answer is stored the same way, and the address is replaced at once
// with the header's canonical one (canonicalThreadUrl), which keeps the
// ordinal, /history, the page, the lens and the hash. A move also drops the
// held forum index and thread lists, whose counts it changed. A refused
// rename or move keeps what was typed or chosen, and onWindowClosed hears a
// rename-window-closed.
//
// Slow mode is a choice of intervals, or Custom's seconds, prefilled from
// the page's slow_mode_seconds (slowSeconds). It is a PUT that answers the
// value rather than a header and appends no record, so the page is asked
// for again, and its viewer block, whose change changes the ETag, shows
// the new value.
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
// refused. A refused rename or move keeps its form instead, with its button
// disabled, so the title typed or the forum chosen is still there to copy
// (Terva review 1588).
export function ModerateDialog({
  thread,
  kind,
  pinned,
  staff,
  slowSeconds = 0,
  offered,
  returnTo,
  onChanged,
  onClose,
  current = { title: '', forum: '' },
  admin = false,
  onWindowClosed,
}: {
  thread: string;
  kind: PanelKind;
  pinned: Pinned | null;
  staff: boolean;
  slowSeconds?: number;
  offered: boolean;
  returnTo: () => HTMLElement | null;
  onChanged: () => void;
  onClose: () => void;
  // current is the thread's title and forum slug, for rename and move.
  current?: { title: string; forum: string };
  admin?: boolean;
  onWindowClosed?: () => void;
}) {
  const root = useRef<HTMLElement>(null);
  const { route } = useLocation();
  const [reason, setReason] = useState('');
  const [title, setTitle] = useState(current.title);
  const [dest, setDest] = useState('');
  const [forums, setForums] = useState(() => peekGet<ForumTree>(forumsUrl)?.forums);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [scope, setScope] = useState<'forum' | 'global'>('forum');
  const [until, setUntil] = useState('');
  const [choice, setChoice] = useState(slowChoices.some(([v]) => v === String(slowSeconds)) ? String(slowSeconds) : 'custom');
  const [custom, setCustom] = useState(String(slowSeconds));
  const [sending, setSending] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);
  // Set synchronously, so a second press before the render that disables
  // the button sends nothing.
  const inFlight = useRef(false);

  const load = () => {
    setLoadError(null);
    apiGet<ForumTree>(forumsUrl).then((t) => setForums(t.forums), setLoadError);
  };
  useEffect(() => {
    if (kind === 'move') load();
  }, []);
  // Focus goes in during the press that opened the panel, not a frame later,
  // so an Escape pressed straight away reaches the panel rather than the
  // control that opened it (TKT-01M3TS0ZV9).
  useLayoutEffect(() => {
    root.current?.querySelector<HTMLElement>('textarea, input, select, button')?.focus();
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
  // A title's length in code points, as the server counts runes.
  const trimmed = title.trim();
  const length = [...trimmed].length;
  const unready =
    (kind === 'rename' && (length === 0 || length > maxTitle || trimmed === current.title)) || (kind === 'move' && !dest);
  // The seconds chosen: NaN for a Custom value that is not a whole number.
  const typed = custom.trim();
  const seconds = choice === 'custom' ? (/^\d+$/.test(typed) ? Number(typed) : NaN) : Number(choice);

  const body = (): unknown => {
    switch (kind) {
      case 'lock':
      case 'unlock':
        return reason.trim() ? { reason } : {};
      case 'pin':
        return { scope: staff ? scope : 'forum', until: untilMs };
      case 'unpin':
        return { scope: pinned?.scope };
      case 'rename':
        return { title: trimmed };
      case 'move':
        return { forum: dest };
      case 'slow-mode':
        return { seconds };
    }
  };

  const send = async (e: Event) => {
    e.preventDefault();
    if (!offered || badUntil || unready || inFlight.current) return;
    if (kind === 'unpin' && !pinned) return;
    // A value the server would refuse is refused here, in its words.
    if (kind === 'slow-mode' && !(seconds <= slowModeMax)) return setRefused(slowModeInvalid);
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    try {
      const t = encodeURIComponent(thread);
      if (kind === 'slow-mode') {
        await apiSend<SlowMode>('PUT', `/api/v1/admin/threads/${t}/slow-mode`, body(), { status: 200 });
        onChanged();
        onClose();
        return;
      }
      const header = await apiSend<ThreadHeader>(
        kind === 'rename' ? 'PATCH' : 'POST',
        kind === 'rename' ? `/api/v1/threads/${t}` : `/api/v1/admin/threads/${t}/${kind}`,
        body(),
        { status: 200 },
      );
      storeThreadHeader(thread, header);
      if (kind === 'move') forgetGets((u) => u.startsWith(forumsUrl));
      const to = canonicalThreadUrl(location, header);
      if (to) route(to, true);
      onChanged();
      onClose();
    } catch (err) {
      setRefused(moderateRefusal(kind, err));
      if (isProblem(err, 'rename-window-closed')) onWindowClosed?.();
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

  const heading = `moderate-title-${thread}`;
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
      aria-labelledby={heading}
      data-moderate={kind}
      onKeyDown={onKeyDown}
    >
      <h2 id={heading}>{titles[kind]}</h2>
      {offered || ((kind === 'rename' || kind === 'move') && refused) ? (
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
          {kind === 'rename' && (
            <>
              <label class="moderate-field">
                Title
                <input
                  type="text"
                  value={title}
                  aria-describedby={hint}
                  aria-invalid={length > maxTitle}
                  onInput={(e) => setTitle(e.currentTarget.value)}
                />
              </label>
              <p id={hint} class="moderate-hint">
                Up to {maxTitle} characters.
              </p>
            </>
          )}
          {kind === 'move' && (
            <label class="moderate-field">
              Move to
              <select value={dest} disabled={!forums} onChange={(e) => setDest(e.currentTarget.value)}>
                <option value="">{forums ? 'Choose a forum' : 'Loading…'}</option>
                {forums &&
                  destinations(forums, current.forum, admin).map(([f, child]) => (
                    <option key={f.slug} value={f.slug}>
                      {child ? `\u2003${f.name}` : f.name}
                    </option>
                  ))}
              </select>
            </label>
          )}
          {kind === 'slow-mode' && (
            <>
              <fieldset>
                <legend>Time between posts</legend>
                {slowChoices.map(([v, label]) => (
                  <label key={v}>
                    <input
                      type="radio"
                      name={`moderate-slow-${thread}`}
                      value={v}
                      checked={choice === v}
                      onChange={() => setChoice(v)}
                    />
                    {label}
                  </label>
                ))}
              </fieldset>
              {choice === 'custom' && (
                <label class="moderate-field">
                  Seconds between posts
                  <input inputMode="numeric" value={custom} onInput={(e) => setCustom(e.currentTarget.value)} />
                </label>
              )}
            </>
          )}
          {kind === 'move' && loadError !== null && (
            <p class="moderate-refused" role="alert">
              {problemMessage(loadError).text}{' '}
              <button type="button" onClick={load}>
                Retry
              </button>
            </p>
          )}
          {alert}
          <div class="moderate-buttons">
            {cancel}
            <button type="submit" class="moderate-send" disabled={!offered || sending || badUntil || unready}>
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
