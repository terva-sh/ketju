import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend, forgetGets, peekGet } from '../../api/client';
import type { ForumTree, MovedPosts, ThreadHeader } from '../../api/dto';
import { asProblem, problemMessage } from '../../api/problem';
import { forumsUrl } from '../../screens/ForumIndex';
import { forgetLists } from '../../state/lists';
import { endSelection, isSelected, maxSelection, selection, startSelection, toggleSelected } from '../../state/selection';
import { storeThreadHeader } from '../../state/threads';
import { useModal } from '../focus';
import { destinations, maxTitle, moderateChanged } from './ModerateDialog';
import './movePosts.css';

// Moving posts out of a thread (docs/06 "Moderator flow for drift",
// TKT-01M29CYSAM): a checkbox in the header of each post shown as itself, a
// bar fixed to the foot of the screen that counts the selection, and the
// split dialog. This module is the MovePosts chunk, loaded the first time a
// moderator chooses Select posts, so a reader who never moderates never
// loads it. The thread page decides when selection is on and which posts
// take a box; what is selected is state/selection.ts.

export { endSelection, startSelection };

// SelectBox is one post's checkbox, at the start of its header.
export function SelectBox({ thread, ordinal }: { thread: string; ordinal: number }) {
  const checked = isSelected(thread, ordinal);
  const full = !checked && (selection.value?.ordinals.length ?? 0) >= maxSelection;
  return (
    <label class="post-select hit">
      <input
        type="checkbox"
        aria-label={`Select post #${ordinal}`}
        checked={checked}
        disabled={full}
        data-select-post={ordinal}
        onChange={() => toggleSelected(thread, ordinal)}
      />
    </label>
  );
}

// countText is what the bar says of n selected posts.
export function countText(n: number): string {
  const posts = n === 1 ? '1 post selected' : `${n} posts selected`;
  return n >= maxSelection ? `${posts}, the most one move takes` : posts;
}

// SelectionBar shows the selection in thread. The thread page starts the
// selection (startSelection) and ends it (endSelection): on Cancel and
// Escape, which the bar hands to onDone, after a split, and on leaving the
// thread. The bar is drawn again while each page of the thread loads, and the
// selection outlives that. When it goes with focus inside it, focus goes to
// returnTo, the thread's title. Split… opens the split dialog over it;
// Merge… is TKT-01M29CYSBA's and is not offered yet.
//
// The count is in a live region, so a screen reader hears each change.
export function SelectionBar({
  thread,
  admin,
  onDone,
  onSplit,
  returnTo,
}: {
  thread: ThreadHeader;
  admin: boolean;
  onDone: () => void;
  // onSplit hears the new thread once a split has been answered.
  onSplit: (to: ThreadHeader) => void;
  returnTo: () => HTMLElement | null;
}) {
  const [splitting, setSplitting] = useState(false);
  const bar = useRef<HTMLDivElement>(null);
  const done = useRef(onDone);
  done.current = onDone;
  useEffect(() => {
    if (selection.value?.thread !== thread.id) startSelection(thread.id);
    return () => {
      const at = document.activeElement;
      if (at === null || at === document.body || bar.current?.contains(at)) returnTo()?.focus({ preventScroll: true });
    };
  }, [thread.id]);
  // Escape ends the selection, unless something nearer took the key: an
  // open dialog takes it first (useModal) and prevents its default.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        done.current();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);
  const s = selection.value;
  const ordinals = s && s.thread === thread.id ? s.ordinals : [];
  const n = ordinals.length;
  return (
    <div ref={bar} class="selection-bar" role="region" aria-label="Selected posts" data-selection-bar>
      <span class="selection-count" aria-live="polite" data-selection-count>
        {countText(n)}
      </span>
      <div class="selection-buttons">
        <button type="button" class="selection-split" disabled={n === 0} onClick={() => setSplitting(true)}>
          Split…
        </button>
        <button type="button" disabled title="Merging posts into another thread is not available yet">
          Merge…
        </button>
        <button type="button" onClick={onDone}>
          Cancel
        </button>
      </div>
      {splitting && (
        <SplitDialog
          source={thread}
          ordinals={ordinals}
          admin={admin}
          onClose={() => setSplitting(false)}
          onSplit={onSplit}
          returnTo={() => bar.current?.querySelector<HTMLElement>('.selection-split') ?? null}
        />
      )}
    </div>
  );
}

// splitRefusal is what a refused split says. Conflict is a post or the
// thread changing since the page loaded: removed, moved, or archived.
export function splitRefusal(err: unknown): string {
  const p = asProblem(err);
  switch (p.type) {
    case 'conflict':
      return `${moderateChanged} A post may have been removed or moved, or the thread archived.`;
    case 'forbidden':
      return 'You cannot move posts into that forum.';
    case 'not-found':
      return 'That forum, or one of these posts, is no longer there.';
    case 'validation-failed':
      return `Enter a title of 1 to ${maxTitle} characters, and choose 1 to ${maxSelection} posts.`;
    case 'unavailable':
      return 'The split could not finish just now. If it was recorded, the posts arrive in the new thread within a minute.';
    default:
      return problemMessage(p).text;
  }
}

// SplitDialog moves the selected posts into a new thread
// (POST /api/v1/admin/threads/{id}/split): a title, and a forum, the source's
// own to begin with, from the forums the moderator reads (destinations, which
// lists an archived forum only to an admin). It is modal (useModal), as the
// flag dialog is.
//
// The answer is both threads' headers, which replace the cached ones
// (storeThreadHeader), so the source's held pages, whose posts moved, are
// dropped; the forum index and the lists, whose counts the split changed,
// are dropped too, and onSplit takes the moderator to the new thread. A
// refusal is shown in the dialog, which keeps what was typed and chosen.
export function SplitDialog({
  source,
  ordinals,
  admin,
  onClose,
  onSplit,
  returnTo,
}: {
  source: ThreadHeader;
  ordinals: number[];
  admin: boolean;
  onClose: () => void;
  onSplit: (to: ThreadHeader) => void;
  returnTo: () => HTMLElement | null;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [title, setTitle] = useState('');
  const [forum, setForum] = useState(source.forum.slug);
  const [forums, setForums] = useState(() => peekGet<ForumTree>(forumsUrl)?.forums);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [sending, setSending] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  useEffect(
    () => () => {
      mounted.current = false;
    },
    [],
  );
  const load = () => {
    setLoadError(null);
    apiGet<ForumTree>(forumsUrl).then((t) => mounted.current && setForums(t.forums), (e) => mounted.current && setLoadError(e));
  };
  useEffect(load, []);
  useModal(root, onClose, (el) => {
    el?.querySelector<HTMLElement>('input')?.focus();
    return () => returnTo()?.focus({ preventScroll: true });
  });

  const options = forums ? destinations(forums, '', admin) : [];
  const trimmed = title.trim();
  const length = [...trimmed].length;
  const ready = length > 0 && length <= maxTitle && options.some(([f]) => f.slug === forum) && ordinals.length > 0;

  const send = async (e: Event) => {
    e.preventDefault();
    if (!ready || inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    try {
      const r = await apiSend<MovedPosts>(
        'POST',
        `/api/v1/admin/threads/${encodeURIComponent(source.id)}/split`,
        { ordinals, title: trimmed, forum },
        { status: 200 },
      );
      storeThreadHeader(r.source.id, r.source);
      storeThreadHeader(r.thread.id, r.thread);
      forgetGets((u) => u.startsWith(forumsUrl));
      forgetLists();
      onSplit(r.thread);
    } catch (err) {
      if (!mounted.current) return;
      setRefused(splitRefusal(err));
    } finally {
      inFlight.current = false;
      if (mounted.current) setSending(false);
    }
  };

  const n = ordinals.length;
  return (
    <div class="flag-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={root} class="flag-dialog split-dialog" role="dialog" aria-modal="true" aria-labelledby="split-title" data-split-dialog>
        <h2 id="split-title">{n === 1 ? 'Split 1 post into a new thread' : `Split ${n} posts into a new thread`}</h2>
        <form onSubmit={send}>
          <label class="moderate-field">
            Title
            <input
              type="text"
              value={title}
              aria-describedby="split-hint"
              aria-invalid={length > maxTitle}
              onInput={(e) => setTitle(e.currentTarget.value)}
            />
          </label>
          <label class="moderate-field">
            Forum
            <select value={forum} disabled={!forums} onChange={(e) => setForum(e.currentTarget.value)}>
              {!forums && <option value={forum}>Loading…</option>}
              {options.map(([f, child]) => (
                <option key={f.slug} value={f.slug}>
                  {child ? ` ${f.name}` : f.name}
                </option>
              ))}
            </select>
          </label>
          <p id="split-hint" class="flag-hint">
            The posts move to the new thread. To undo, merge them back.
          </p>
          {loadError !== null && (
            <p class="moderate-refused" role="alert">
              {problemMessage(loadError).text}{' '}
              <button type="button" onClick={load}>
                Retry
              </button>
            </p>
          )}
          {refused && (
            <p class="moderate-refused" role="alert">
              {refused}
            </p>
          )}
          <div class="flag-buttons">
            <button type="button" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" class="flag-send" disabled={!ready || sending}>
              Split
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
