import { useEffect, useRef, useState } from 'preact/hooks';
import { apiSend } from '../api/client';
import type { ForumWatch, ThreadWatch } from '../api/dto';
import { ProblemError } from '../api/problem';
import { forgetLists } from '../state/lists';
import { forgetAllThreadPages } from '../state/threads';
import { refreshViewer } from '../state/viewer';
import { ErrorPanel } from './ErrorPanel';
import { useModal } from './focus';
import './thread/thread.css';

// A level is what POST /threads/{id}/watch and POST /forums/{slug}/watch
// take (docs/07): watch, mute, or '' to remove the row (docs/04 `watches`).
export type WatchLevel = 'watch' | 'mute' | '';

// watchLabel is the watch control's label for an effective level: the
// state the thread mockups draw on the control ("watching ▾").
export function watchLabel(level: string): string {
  return level === 'watch' ? 'Watching' : level === 'mute' ? 'Muted' : 'Watch';
}

const watchLine = 'Alert me to every new post.';
const muteLine = 'No alerts, and no unread count.';

// followChoice is what the third choice, which posts '', says it will do.
// '' removes the thread's own row, and under docs/04's precedence the thread
// then follows its forum: Normal only when the forum has no level, and the
// forum's level, named as such, when it has one (Terva review 1693). There
// is no Normal that overrides a forum, since a row is watch or mute. A
// forum's own '' is Normal, since nothing is above it (forumLevel null).
export function followChoice(forumLevel: string | null): { label: string; line: string } {
  if (forumLevel === 'watch') return { label: 'Follow the forum (Watch)', line: watchLine };
  if (forumLevel === 'mute') return { label: 'Follow the forum (Mute)', line: muteLine };
  return { label: 'Normal', line: 'Alert me when someone mentions, replies to or quotes me.' };
}

// saves numbers the saves started in this tab, and latest is the newest
// save's number per route, so an answer can tell whether a later save to the
// same thread or forum has started since.
let saves = 0;
const latest = new Map<string, number>();

export type WatchTarget = { thread: string; forumLevel: string } | { forum: string; name: string };

// WatchDialog sets the viewer's level on a thread or on a forum's own
// threads (docs/04 `watches`, TKT-01M3V6Y87D). It is the only way a level is
// set: replying or starting a thread does not watch it (docs/06).
//
// It is a modal dialog, as FlagDialog is (useModal), and opens on the
// level the target has of its own. Save posts the choice; the answer goes to
// onSaved, the dialog closes, and /me is asked for again so the badges
// follow, since mute takes a thread out of every unread count. The held
// copies that count unread, or carry the level, are dropped first, so no
// screen shows them again before they are asked for. A refusal is shown in
// the dialog, which stays open as chosen.
//
// Focus goes back to returnTo when it is still in the page and drawn, or
// else to fallback(): on a phone the control was a more menu item, and the
// menu focuses its button before it runs one.
//
// A save outlives a dialog dismissed while it is out, and the reader may
// open the control again and save again before it answers (Terva review
// 1820). Its answer still drops the held copies and asks for /me, since the
// write happened. It is handed to onSaved only if no later save has started
// since on the same target, so answers landing out of order do not put back
// an older level, and
// it closes the dialog only if it is still this one.
export function WatchDialog({
  target,
  level,
  returnTo,
  fallback,
  onSaved,
  onClose,
}: {
  target: WatchTarget;
  level: string;
  returnTo: Element | null;
  fallback: () => HTMLElement | null;
  onSaved: (answer: ThreadWatch | ForumWatch) => void;
  onClose: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [choice, setChoice] = useState<WatchLevel>(level === 'watch' || level === 'mute' ? level : '');
  const [sending, setSending] = useState(false);
  const [refused, setRefused] = useState<unknown>(null);
  // Set synchronously, so a second press before the render that disables
  // Save sends nothing.
  const inFlight = useRef(false);
  const mounted = useRef(true);
  useEffect(
    () => () => {
      mounted.current = false;
    },
    [],
  );

  useModal(root, onClose, (el) => {
    el?.querySelector<HTMLElement>('input:checked')?.focus();
    return () => {
      const to = returnTo instanceof HTMLElement && returnTo.isConnected && returnTo.offsetParent !== null ? returnTo : fallback();
      to?.focus({ preventScroll: true });
    };
  });

  const isThread = 'thread' in target;
  const url = isThread
    ? `/api/v1/threads/${encodeURIComponent(target.thread)}/watch`
    : `/api/v1/forums/${encodeURIComponent(target.forum)}/watch`;
  const third = followChoice(isThread ? target.forumLevel : null);
  const choices: [WatchLevel, string, string][] = [
    ['watch', 'Watch', watchLine],
    ['mute', 'Mute', muteLine],
    ['', third.label, third.line],
  ];

  const save = async (e: Event) => {
    e.preventDefault();
    if (inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    const n = ++saves;
    latest.set(url, n);
    try {
      const r = await apiSend<ThreadWatch | ForumWatch | undefined>('POST', url, { level: choice }, { status: 200 });
      if (typeof r?.watch !== 'string') throw new ProblemError('internal', 200);
      // Every list counts unread, which mute changes, and Watching lists the
      // threads with a thread-level watch (state/lists.ts). Every thread page
      // carries a level in its viewer block, a forum's reaching all of its
      // threads, and a page asked for before this answer is not stored, and
      // is asked for again, however late it lands (forgetAllThreadPages, as
      // a mark all read does; Terva review 1822).
      forgetLists();
      forgetAllThreadPages();
      void refreshViewer().catch(() => undefined);
      if (latest.get(url) === n) onSaved(r);
      if (mounted.current) onClose();
    } catch (err) {
      if (!mounted.current) return;
      setRefused(err);
      inFlight.current = false;
      setSending(false);
    }
  };

  const title = isThread ? 'Alerts for this thread' : `Alerts for ${target.name}`;
  return (
    <div class="flag-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={root} class="flag-dialog watch-dialog" role="dialog" aria-modal="true" aria-labelledby="watch-title" data-watch-dialog>
        <h2 id="watch-title">{title}</h2>
        <form onSubmit={save}>
          <fieldset aria-labelledby="watch-title">
            {choices.map(([value, label, line]) => (
              <label key={value} class="watch-choice">
                <input
                  type="radio"
                  name="watch-level"
                  value={value || 'follow'}
                  checked={choice === value}
                  onChange={() => setChoice(value)}
                />
                <span>
                  <span class="watch-label">{label}</span>
                  <span class="watch-line">{line}</span>
                </span>
              </label>
            ))}
          </fieldset>
          {!isThread && <p class="flag-hint">This covers the threads in {target.name}, not its subforums. A thread's own setting comes first.</p>}
          {refused !== null && <ErrorPanel error={refused} />}
          <div class="flag-buttons">
            <button type="button" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" class="flag-send" disabled={sending}>
              Save
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
