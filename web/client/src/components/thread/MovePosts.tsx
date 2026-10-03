import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend, forgetGets, peekGet } from '../../api/client';
import type { ForumTree, MergedPosts, MovedPosts, ThreadHeader, ThreadPage } from '../../api/dto';
import { asProblem, problemMessage } from '../../api/problem';
import { forumsUrl } from '../../screens/ForumIndex';
import { forgetLists } from '../../state/lists';
import { endSelection, isSelected, maxSelection, selection, startSelection, toggleSelected } from '../../state/selection';
import { storeThreadHeader, threadUrl } from '../../state/threads';
import { useModal } from '../focus';
import { destinations, maxTitle, moderateChanged } from './ModerateDialog';
import './movePosts.css';

// Moving posts out of a thread (docs/06 "Moderator flow for drift",
// TKT-01M29CYSAM): a checkbox in the header of each post shown as itself, a
// bar fixed to the foot of the screen that counts the selection, the split
// dialog, and the merge dialog (TKT-01M29CYSBA). This module is the
// MovePosts chunk, loaded the first time a
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
// returnTo, the thread's title. Split… opens the split dialog over it, and
// Merge… the merge dialog.
//
// The count is in a live region, so a screen reader hears each change.
export function SelectionBar({
  thread,
  admin,
  onDone,
  onSplit,
  onMerge,
  returnTo,
}: {
  thread: ThreadHeader;
  admin: boolean;
  onDone: () => void;
  // onSplit hears the new thread once a split has been answered.
  onSplit: (to: ThreadHeader) => void;
  // onMerge hears the target and its first copy once a merge has been
  // answered.
  onMerge: (to: ThreadHeader, first: number) => void;
  returnTo: () => HTMLElement | null;
}) {
  const [splitting, setSplitting] = useState(false);
  const [merging, setMerging] = useState(false);
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
        <button type="button" class="selection-merge" disabled={n === 0} onClick={() => setMerging(true)}>
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
      {merging && (
        <MergeDialog
          source={thread}
          ordinals={ordinals}
          onClose={() => setMerging(false)}
          onMerge={onMerge}
          returnTo={() => bar.current?.querySelector<HTMLElement>('.selection-merge') ?? null}
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
// are dropped too, and onSplit takes the moderator to the new thread, but
// only while the dialog is still open. A refusal is shown in the dialog,
// which keeps what was typed and chosen.
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
      // A dialog closed while the split was on its way, or a page left,
      // takes nobody anywhere: the caches above still learn of it.
      if (mounted.current) onSplit(r.thread);
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

// threadIdPattern is a thread's base32 id: 13 characters of Crockford's
// alphabet, the first no more than f, since the id is 64 bits.
const threadIdPattern = /^[0-9a-f][0-9a-hjkmnp-tv-z]{12}$/i;

// threadIdFrom reads a merge's target from what a moderator pasted: a
// thread's id, or a link to it or to one of its posts, from the address bar
// or a post's permalink. It answers the id in lower case, or null when the
// text names no thread. Which site a link names is not checked: the id is
// looked up here, and the dialog shows the thread it found before a merge
// can be sent.
export function threadIdFrom(text: string): string | null {
  const t = text.trim();
  if (threadIdPattern.test(t)) return t.toLowerCase();
  try {
    const m = /^\/t\/([^/]+)/.exec(new URL(t, location.origin).pathname);
    const id = m ? decodeURIComponent(m[1]) : '';
    return threadIdPattern.test(id) ? id.toLowerCase() : null;
  } catch {
    return null;
  }
}

// notFound is what the merge dialog says of a target it cannot show: one
// that does not exist and one the moderator may not read read alike, as the
// server answers them alike (docs/07 "Moving posts").
const notFound = 'No thread you can read has that address.';

// mergeRefusal is what a refused merge says.
export function mergeRefusal(err: unknown): string {
  const p = asProblem(err);
  switch (p.type) {
    case 'conflict':
      return `${moderateChanged} A post may have been removed or moved, or a thread archived.`;
    case 'forbidden':
      return 'You cannot move posts into that thread’s forum.';
    case 'not-found':
      return `${notFound} Or one of these posts is no longer there.`;
    case 'validation-failed':
      return `Choose another thread, and 1 to ${maxSelection} posts.`;
    case 'unavailable':
      return 'The merge could not finish just now. If it was recorded, the posts arrive in that thread within a minute.';
    default:
      return problemMessage(p).text;
  }
}

// MergeDialog moves the selected posts to the end of another thread
// (POST /api/v1/admin/threads/{id}/merge, TKT-01M29CYSBA). It takes the
// target as a link or an id, and once that names a thread other than this
// one it reads the thread's first page and shows its title and forum; Merge
// is offered only then, for the thread shown. It is modal (useModal), as the
// split dialog is.
//
// The answer is both threads' headers, which replace the cached ones; the
// forum index and the lists are dropped, as after a split, and onMerge takes
// the moderator to the target at the first copy, which the answer names,
// but only while the dialog is still open. A refusal is shown in the
// dialog, which keeps what was typed.
export function MergeDialog({
  source,
  ordinals,
  onClose,
  onMerge,
  returnTo,
}: {
  source: ThreadHeader;
  ordinals: number[];
  onClose: () => void;
  onMerge: (to: ThreadHeader, first: number) => void;
  returnTo: () => HTMLElement | null;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [text, setText] = useState('');
  const [target, setTarget] = useState<ThreadHeader | null>(null);
  const [lookupError, setLookupError] = useState<unknown>(null);
  const [attempt, setAttempt] = useState(0);
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
  useModal(root, onClose, (el) => {
    el?.querySelector<HTMLElement>('input')?.focus();
    return () => returnTo()?.focus({ preventScroll: true });
  });

  const id = threadIdFrom(text);
  const self = id === source.id;
  useEffect(() => {
    setTarget(null);
    setLookupError(null);
    if (!id || self) return;
    const ctl = new AbortController();
    apiGet<ThreadPage>(threadUrl(id, { page: 1 }), { signal: ctl.signal }).then(
      (p) => !ctl.signal.aborted && setTarget(p.thread),
      (e) => !ctl.signal.aborted && setLookupError(e),
    );
    return () => ctl.abort();
  }, [id, self, attempt]);

  const shown = target && target.id === id ? target : null;
  const ready = shown !== null && ordinals.length > 0;
  const lookupType = lookupError === null ? null : asProblem(lookupError).type;
  const hidden = lookupType === 'not-found' || lookupType === 'forbidden' || lookupType === 'unauthenticated';

  const send = async (e: Event) => {
    e.preventDefault();
    if (!ready || inFlight.current) return;
    inFlight.current = true;
    setSending(true);
    setRefused(null);
    try {
      const r = await apiSend<MergedPosts>(
        'POST',
        `/api/v1/admin/threads/${encodeURIComponent(source.id)}/merge`,
        { ordinals, into: shown.id },
        { status: 200 },
      );
      storeThreadHeader(r.source.id, r.source);
      storeThreadHeader(r.target.id, r.target);
      forgetGets((u) => u.startsWith(forumsUrl));
      forgetLists();
      // As after a split: a dialog closed, or a page left, while the merge
      // was on its way takes nobody anywhere (Terva review 2057).
      if (mounted.current) onMerge(r.target, r.first_copy);
    } catch (err) {
      if (!mounted.current) return;
      setRefused(mergeRefusal(err));
    } finally {
      inFlight.current = false;
      if (mounted.current) setSending(false);
    }
  };

  const n = ordinals.length;
  let status = null;
  if (self) status = <p class="merge-target">That is this thread. Choose another.</p>;
  else if (text.trim() !== '' && !id) status = <p class="merge-target">Paste a link to a thread, or its 13-character id.</p>;
  else if (shown)
    status = (
      <p class="merge-target" data-merge-target>
        <strong>{shown.title}</strong> in {shown.forum.name}
      </p>
    );
  else if (hidden) status = <p class="merge-target">{notFound}</p>;
  else if (lookupError !== null)
    status = (
      <p class="merge-target">
        {problemMessage(lookupError).text}{' '}
        <button type="button" onClick={() => setAttempt((a) => a + 1)}>
          Retry
        </button>
      </p>
    );
  else if (id) status = <p class="merge-target">Looking for that thread…</p>;
  return (
    <div class="flag-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={root} class="flag-dialog split-dialog" role="dialog" aria-modal="true" aria-labelledby="merge-title" data-merge-dialog>
        <h2 id="merge-title">{n === 1 ? 'Merge 1 post into another thread' : `Merge ${n} posts into another thread`}</h2>
        <form onSubmit={send}>
          <label class="moderate-field">
            Thread link or id
            <input
              type="text"
              value={text}
              autocomplete="off"
              spellcheck={false}
              aria-describedby="merge-status merge-hint"
              aria-invalid={self || (text.trim() !== '' && !id) || hidden}
              onInput={(e) => setText(e.currentTarget.value)}
            />
          </label>
          <div id="merge-status" aria-live="polite">
            {status}
          </div>
          <p id="merge-hint" class="flag-hint">
            The posts move to the end of that thread, keeping their authors and times. To undo, select them there and
            merge them back.
          </p>
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
              Merge
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
