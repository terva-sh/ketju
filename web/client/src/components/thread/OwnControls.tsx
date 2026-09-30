import { signal } from '@preact/signals';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { CreatedPost, PostSource } from '../../api/dto';
import { asProblem, isAbort, isProblem, problemMessage } from '../../api/problem';
import { ComposerBody, type ComposerFields } from '../../composer/ComposerBody';
import { isPhone } from '../../state/layout';
import { viewer } from '../../state/viewer';
import type { Controls } from './postControls';

// Edit, Delete and Restore on the viewer's own posts (TKT-01M3PTT97X; docs/07
// "Edit, delete and restore"), and on every post for a viewer who moderates
// the forum (TKT-01M3QHKD21). postControls decides what is offered, and
// ThreadBody hands each post the viewer may act on an Own; the requests, the
// editor, the inline confirmation and the words for a refusal are here, once,
// for Post and Tombstone alike.

// Own is what the thread page hands a post that is the viewer's own, or any
// post when the viewer moderates the forum.
export interface Own {
  thread: string;
  offer: Controls;
  // A delete here is a moderator's of somebody else's post, which hides it:
  // members see that a moderator removed it, and only a moderator restores
  // it (docs/08 "Restoring a moderator's delete").
  hides: boolean;
  // When the delete window closes, for the confirmation; null when no window
  // binds the viewer.
  deleteUntil: number | null;
  // The page's log length. A change answers with a longer one, and the post
  // offers nothing more until a page that long shows the change.
  logLength: number;
  // Asks for the page again, after a change or a refusal that says the page
  // is out of date.
  onChanged: () => void;
}

export type OwnKind = 'edit' | 'delete' | 'restore';

type Failure = { kind: OwnKind; text: string };

// ownFailure is what a refused edit, delete or restore says, beside the
// control that sent it. The two window codes, and anything else not named
// here, read as the catalogue has them.
export function ownFailure(kind: OwnKind, err: unknown): string {
  switch (asProblem(err).type) {
    case 'conflict':
      return `This post can no longer be ${kind === 'edit' ? 'edited' : kind === 'delete' ? 'deleted' : 'restored'}. It may have changed, or its thread or forum been locked or archived.`;
    case 'forbidden':
      return `You may no longer ${kind} this post.`;
    case 'not-found':
      return 'This post is no longer here.';
    case 'validation-failed':
      return 'The post was not accepted: it may be empty, or quote a post that is not in this thread.';
    case 'payload-too-large':
      return 'The post is longer than the limit allows.';
    default:
      return problemMessage(err).text;
  }
}

// A refusal of these kinds says the page no longer describes the post, so
// the page is asked for again and offers what the server now allows.
const stale = (err: unknown) => ['conflict', 'forbidden', 'not-found', 'gone'].includes(asProblem(err).type);

export type OwnState = ReturnType<typeof useOwn>;

// useOwn is one post's own controls: which is open (the editor, or the
// delete confirmation), what is offered now, the request in flight, and a
// refusal's words.
//
// A window refusal withdraws what it names from this post for as long as it
// is on screen, whatever this clock says: the server's clock is the one that
// counts, and a button that is only refused again is worse than none. After
// a change, nothing more is offered until the page shows it.
//
// When the editor or the confirmation closes, focus goes back to the control
// that opened it, or on a phone to the post's More button, which holds them;
// and to the post itself when that control is gone.
export function useOwn(own: Own | undefined, ordinal: number) {
  const [mode, setMode] = useState<'edit' | 'delete' | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [closed, setClosed] = useState({ edit: false, delete: false });
  const [waitFor, setWaitFor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [refocus, setRefocus] = useState<OwnKind | null>(null);
  // Set synchronously, so a second press before the render that disables the
  // control sends nothing.
  const inFlight = useRef(false);

  useEffect(() => {
    if (!refocus) return;
    setRefocus(null);
    const post = document.getElementById(`post-${ordinal}`);
    const to = post?.querySelector<HTMLElement>(isPhone.value ? '[data-post-more]' : `.post-footer [data-post-action=${refocus}]`);
    (to ?? post)?.focus({ preventScroll: true });
  }, [refocus]);

  // A post that stops being the viewer's to act on, or a change of who is
  // signed in in this tab, closes what was open on it for good: the editor
  // holds a source only its author or a moderator may read. A moderator's
  // Own rests on the page's can_moderate, which a sign-in as someone else
  // does not change until the page is fetched again, so the handle counts
  // too (TKT-01M3QHKD21).
  const who = own ? (viewer.value.user?.handle ?? '') : null;
  useEffect(
    () => () => {
      setMode(null);
      setFailure(null);
    },
    [who],
  );

  const live = !!own && own.logLength >= waitFor;
  const offer = {
    edit: live && own.offer.edit && !closed.edit,
    delete: live && own.offer.delete && !closed.delete,
    restore: live && own.offer.restore && !closed.delete,
  };

  const open = (kind: 'edit' | 'delete') => {
    setFailure(null);
    setMode(kind);
  };
  const close = (kind: OwnKind) => {
    setMode(null);
    setRefocus(kind);
  };
  const send = async (kind: OwnKind, body?: unknown) => {
    if (!own || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    const url = `/api/v1/threads/${encodeURIComponent(own.thread)}/posts/${ordinal}${kind === 'restore' ? '/restore' : ''}`;
    try {
      const r = await apiSend<CreatedPost>(kind === 'edit' ? 'PATCH' : kind === 'delete' ? 'DELETE' : 'POST', url, body);
      setWaitFor(r.log_length);
      if (kind !== 'restore') close(kind);
      own.onChanged();
    } catch (err) {
      setFailure({ kind, text: ownFailure(kind, err) });
      if (isProblem(err, 'edit-window-closed')) setClosed((c) => ({ ...c, edit: true }));
      if (isProblem(err, 'delete-window-closed')) setClosed((c) => ({ ...c, delete: true }));
      if (stale(err)) own.onChanged();
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };
  // refresh asks for the page again, for a refusal that says it is stale.
  const refresh = () => own?.onChanged();
  return { mode: own ? mode : null, offer, failure, busy, open, close, send, refresh, deleteUntil: own?.deleteUntil ?? null, hides: !!own?.hides };
}

// withdrawn is what an open editor or confirmation says once its control is
// no longer offered and no refusal has said why.
const withdrawn = (kind: OwnKind) => `You can no longer ${kind} this post.`;

// note is the sentence an open editor or confirmation shows: the refusal of
// its own request, else that the offer has been withdrawn under it.
function note(o: OwnState, kind: 'edit' | 'delete'): string | null {
  if (o.failure?.kind === kind) return o.failure.text;
  return o.offer[kind] ? null : withdrawn(kind);
}

// DeleteConfirm asks before a delete, inline in the post, and says until when
// the author can restore it; or, for a moderator's delete of somebody else's
// post, what members will see and who can restore it, in removedText's words
// for it (TKT-01M3QHKD21). Cancel has focus when it opens, so a second
// Enter does not delete; Escape cancels. Neither closes it while the delete
// is in flight, since a refusal is said here and would have nowhere to go
// (Terva review 1610).
export function DeleteConfirm({ o, ordinal }: { o: OwnState; ordinal: number }) {
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => cancel.current?.focus(), []);
  const text = note(o, 'delete');
  return (
    <div
      class="own-confirm"
      role="group"
      aria-label={`Delete post #${ordinal}`}
      data-confirm-delete={ordinal}
      onKeyDown={(e) => {
        if (e.key !== 'Escape' || e.defaultPrevented) return;
        e.preventDefault();
        if (!o.busy) o.close('delete');
      }}
    >
      <p>
        {o.hides ? 'Remove this post? Members will see that a moderator removed it. Only a moderator can restore it.' : 'Delete this post?'}
        {o.deleteUntil !== null && (
          <>
            {' '}You can restore it until <Until at={o.deleteUntil} />.
          </>
        )}
      </p>
      {text && (
        <p class="own-failure" role="alert">
          {text}
        </p>
      )}
      <div class="own-buttons">
        <button type="button" class="own-send" disabled={!o.offer.delete || o.busy} onClick={() => void o.send('delete')}>
          Delete
        </button>
        <button type="button" ref={cancel} disabled={o.busy} onClick={() => o.close('delete')}>
          Cancel
        </button>
      </div>
    </div>
  );
}

function Until({ at }: { at: number }) {
  const { timezone, locale } = viewer.value.settings;
  return (
    <time dateTime={new Date(at).toISOString()}>
      {new Date(at).toLocaleString(locale || undefined, { dateStyle: 'medium', timeStyle: 'short', timeZone: timezone || undefined })}
    </time>
  );
}

// PostEditor edits a post in its place, the viewer's own or, for a
// moderator, anyone's, with the composer
// every writing screen shares, previewed in the thread as the post's reply.
// It loads the post's current source from GET …/source, which answers only
// the author or a moderator and is never held on the client (no ETag), and
// saves with PATCH {source}. It is not a draft: closing it drops the text.
// A save that is refused keeps the text, says why, and leaves Save off while
// the offer is withdrawn. Cancel is off while a save is in flight, so the
// text and the answer's words are still there when it comes back (Terva
// review 1610). A refusal to load the source that says the page is stale
// asks for the page again, as a refused save does.
export function PostEditor({
  o,
  thread,
  ordinal,
  replyTo,
}: {
  o: OwnState;
  thread: string;
  ordinal: number;
  replyTo: number | null;
}) {
  const c: ComposerFields = useMemo(() => ({ source: signal(''), caret: signal(0), summoned: signal(0), posting: signal(false) }), []);
  const [loaded, setLoaded] = useState<{ source: string } | { error: unknown } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const ctl = new AbortController();
    setLoaded(null);
    apiGet<PostSource>(`/api/v1/threads/${encodeURIComponent(thread)}/posts/${ordinal}/source`, { signal: ctl.signal }).then(
      (r) => {
        if (ctl.signal.aborted) return;
        c.source.value = r.source;
        c.caret.value = r.source.length;
        setLoaded({ source: r.source });
      },
      (err) => {
        if (ctl.signal.aborted || isAbort(err)) return;
        setLoaded({ error: err });
        if (stale(err)) o.refresh();
      },
    );
    return () => ctl.abort();
  }, [attempt]);
  // The composer freezes its text while a save is in flight.
  useLayoutEffect(() => {
    c.posting.value = o.busy;
  }, [o.busy]);

  // While the source loads, or when it failed to, focus is on this block's
  // first button: the control that opened the editor may be gone, as a
  // phone's More item is. Once it loads, the composer takes it.
  const status = useRef<HTMLDivElement>(null);
  const waiting = loaded === null || 'error' in loaded;
  useEffect(() => {
    if (waiting) status.current?.querySelector('button')?.focus();
  }, [loaded]);

  const cancel = (
    <button type="button" class="discard" disabled={o.busy} onClick={() => o.close('edit')}>
      Cancel
    </button>
  );
  if (loaded === null || 'error' in loaded) {
    return (
      <div class="own-editor-status" data-editing={ordinal} ref={status}>
        {loaded === null ? (
          <p>Loading the post…</p>
        ) : (
          <p class="own-failure" role="alert">
            {ownFailure('edit', loaded.error)}
          </p>
        )}
        <div class="own-buttons">
          {loaded !== null && (
            <button type="button" onClick={() => setAttempt((n) => n + 1)}>
              Retry
            </button>
          )}
          {cancel}
        </div>
      </div>
    );
  }
  const text = note(o, 'edit');
  const source = c.source.value;
  return (
    <ComposerBody
      state={c}
      thread={thread}
      replyTo={replyTo}
      editing={ordinal}
      name={`Edit post #${ordinal}`}
      label="Post text"
      placeholder="Markdown works."
      focus
      head={<h2>Edit #{ordinal}</h2>}
      notice={
        text && (
          <p class="post-failure" role="alert">
            {text}
          </p>
        )
      }
      foot={
        <footer class="composer-foot">
          <span>Markdown supported</span>
          {cancel}
          <button
            type="button"
            class="post-reply"
            data-edit-save
            disabled={!o.offer.edit || o.busy || !source.trim() || source === loaded.source}
            aria-busy={o.busy}
            onClick={() => void o.send('edit', { source })}
          >
            {o.busy ? 'Saving…' : 'Save'}
          </button>
        </footer>
      }
    />
  );
}

// Restore is the control that takes back a delete: an author's own, in a
// tombstone's footer, or any a moderator may restore, in the footer of the
// removed post the page serves them with its body. A refusal is said beside
// it.
export function Restore({ o }: { o: OwnState }) {
  return (
    <>
      {o.offer.restore && (
        <button type="button" class="hit" data-post-action="restore" disabled={o.busy} onClick={() => void o.send('restore')}>
          restore
        </button>
      )}
      {o.failure?.kind === 'restore' && (
        <span class="own-failure" role="alert">
          {o.failure.text}
        </span>
      )}
    </>
  );
}
