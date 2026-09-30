import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../../api/client';
import type { AdminApplication, Post, Queue, QueueFlag, QueueItem, QueueResolution } from '../../../api/dto';
import { isAbort, isProblem } from '../../../api/problem';
import { useResource } from '../../../api/resource';
import { ErrorPanel } from '../../../components/ErrorPanel';
import { removedMark } from '../../../components/thread/removal';
import { refreshViewer, viewer } from '../../../state/viewer';
import { when } from '../../apply/when';
import { ApplicantCell, applicationHref, hasStatus } from '../applications/ApplicationList';
import { Decision } from '../applications/Decision';
import { ConfirmButton } from '../common/ConfirmButton';
import { Field } from '../common/Field';
import { send } from '../common/send';
import '../applications/applications.css';
import './queue.css';

// QueueScreen is /admin/queue (TKT-01M29CRJ4E, docs/08 "Flags"): open flags,
// one item per flagged post, and pending applications, in one list, newest
// first. A moderator checking in wants one place that is either empty or not.
//
// Each flagged post shows the post as a moderator reads it, its state, and
// every open flag with its reporter, reason and note, and three actions:
// remove the post, keep it (which shows it again if it is hidden pending
// review), or dismiss the flags (which does too). An application shows its
// summary and the applications screen's approve and deny controls. An item
// that is handled leaves the list, and the badge is fetched again.
//
// The server's moderates gate decides who may use it, and scopes the flags
// to the forums the viewer moderates (internal/web/api/admin_queue.go).

const queueUrl = (cursor = '') => `/api/v1/admin/queue${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''}`;

// itemKey names an item across pages and reloads.
export const itemKey = (it: QueueItem) =>
  it.flag ? `flag:${it.flag.thread.id}/${it.flag.ordinal}` : `application:${it.application?.id}`;

// handled is what the screen says when another moderator got there first.
export const handled = 'Another moderator has already handled this.';

export function QueueScreen() {
  const [attempt, setAttempt] = useState(0);
  const url = queueUrl();
  const res = useResource(`${url}#${attempt}`, (signal) => apiGet<Queue>(url, { signal }));
  const [more, setMore] = useState<Queue[]>([]);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [gone, setGone] = useState<Set<string>>(new Set());
  const [notice, setNotice] = useState('');
  // failure is an action that failed. It sits above the list, because the
  // reload that follows replaces the item it came from.
  const [failure, setFailure] = useState('');
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);

  const pages = res.data ? [res.data, ...more] : [];
  const seen = new Set<string>();
  const items = pages
    .flatMap((p) => p.items)
    .filter((it) => {
      const k = itemKey(it);
      if (seen.has(k) || gone.has(k)) return false;
      seen.add(k);
      return true;
    });
  const next = pages.length ? pages[pages.length - 1].next_cursor : '';

  // reload fetches the queue from the top, for the state things are really
  // in after a refusal.
  const reload = () => {
    pending.current?.abort();
    setMore([]);
    setMoreError(null);
    setLoadingMore(false);
    setGone(new Set());
    setAttempt((n) => n + 1);
  };

  const loadMore = async () => {
    const ctl = new AbortController();
    pending.current?.abort();
    pending.current = ctl;
    setMoreError(null);
    setLoadingMore(true);
    try {
      const page = await apiGet<Queue>(queueUrl(next), { signal: ctl.signal });
      if (!ctl.signal.aborted) setMore((m) => [...m, page]);
    } catch (err) {
      if (!ctl.signal.aborted) setMoreError(err);
    } finally {
      if (!ctl.signal.aborted) setLoadingMore(false);
    }
  };

  // done takes an item out of the list and fetches the badge again.
  const done = (it: QueueItem, words: string) => {
    setGone((g) => new Set(g).add(itemKey(it)));
    setFailure('');
    setNotice(words);
    refreshViewer().catch(() => {});
  };

  // conflicted says someone else acted first and shows the queue as it is.
  const conflicted = () => {
    setFailure('');
    setNotice(handled);
    reload();
  };

  // failed shows what went wrong and the queue as that left it.
  const failed = (message: string) => {
    setNotice('');
    setFailure(message);
    reload();
  };

  return (
    <div class="admin-queue">
      <p class="queue-notice" role="status" aria-live="polite" data-queue-notice>
        {notice}
      </p>
      {failure && (
        <p class="admin-error" role="alert" data-queue-failure>
          {failure}
        </p>
      )}
      {res.error && !res.data && <ErrorPanel error={res.error} what="The queue" onRetry={res.retry} />}
      {!res.data && !res.error && (
        <p class="admin-muted" aria-live="polite">
          Loading…
        </p>
      )}
      {res.data &&
        (items.length === 0 && !next ? (
          <p data-queue-empty>Nothing is waiting for a moderator.</p>
        ) : (
          <ol class="queue-items">
            {items.map((it) => (
              <li key={itemKey(it)} class="queue-item" data-queue-item={itemKey(it)}>
                {it.flag ? (
                  <FlagItem item={it} flag={it.flag} onDone={done} onConflict={conflicted} onFailed={failed} />
                ) : it.application ? (
                  <ApplicationItem item={it} onDone={done} />
                ) : null}
              </li>
            ))}
          </ol>
        ))}
      {next && (
        <button type="button" class="admin-button" onClick={loadMore} disabled={loadingMore}>
          More items
        </button>
      )}
      {moreError !== null && <ErrorPanel error={moreError} what="More items" />}
    </div>
  );
}

// PostState is how a flagged post stands, as the screen names it.
type PostState = 'visible' | 'review' | 'moderator' | 'author' | 'unknown';

export function postState(p: Post | null): PostState {
  if (!p) return 'unknown';
  if (!p.deleted) return 'visible';
  switch (p.removal) {
    case 'review':
      return 'review';
    case 'moderator':
      return 'moderator';
    default:
      return 'author';
  }
}

const stateWords: Record<PostState, string> = {
  visible: 'Shown',
  review: 'Hidden pending review',
  moderator: 'Hidden by a moderator',
  author: 'Removed',
  unknown: 'Cannot be shown just now',
};

// authorHref is where a flagged post's author links. A moderator or admin by
// role may open the users screen, filtered to the author, for a suspension;
// a member who moderates a forum through a group cannot pass that screen's
// staff gate, so their link goes to the profile (Terva review on PR #140).
export function authorHref(handle: string): string {
  const role = viewer.value.user?.role;
  return role === 'moderator' || role === 'admin'
    ? `/admin/users?prefix=${encodeURIComponent(handle)}`
    : `/u/${encodeURIComponent(handle)}`;
}

const reasonWords: Record<string, string> = {
  spam: 'Spam',
  abuse: 'Abuse',
  'off-topic': 'Off topic',
  other: 'Other',
};

function FlagItem({
  item,
  flag,
  onDone,
  onConflict,
  onFailed,
}: {
  item: QueueItem;
  flag: QueueFlag;
  onDone: (it: QueueItem, words: string) => void;
  onConflict: () => void;
  onFailed: (message: string) => void;
}) {
  const [note, setNote] = useState('');
  const state = postState(flag.post);
  const p = flag.post;
  const link = `/t/${flag.thread.id}/${flag.thread.slug}/${flag.ordinal}`;
  const n = flag.flags.length;

  const act = async (action: 'resolve' | 'dismiss' | 'remove'): Promise<boolean> => {
    const r = await send<QueueResolution>('POST', `/api/v1/admin/queue/posts/${flag.thread.id}/${flag.ordinal}`, {
      action,
      note,
    });
    if (r.ok) {
      onDone(item, resolvedWords(r.value));
      return true;
    }
    if (isProblem(r.error, 'conflict') && state !== 'moderator') {
      onConflict();
      return true;
    }
    // The action did not happen, or not all of it: the queue is fetched
    // again, so the item shows the post as that left it.
    onFailed(r.message);
    return false;
  };

  const restores = state === 'review';
  return (
    <article class="queue-flag" aria-labelledby={`q-${flag.thread.id}-${flag.ordinal}`}>
      <header class="queue-head">
        <h2 id={`q-${flag.thread.id}-${flag.ordinal}`}>
          <a href={link}>
            {flag.thread.title} #{flag.ordinal}
          </a>
        </h2>
        <span class="admin-mark" data-post-state={state}>
          {stateWords[state]}
        </span>
        <span class="admin-muted">
          {n} {n === 1 ? 'flag' : 'flags'}, newest {when(item.created_at)}
        </span>
      </header>
      {p ? (
        <div class="queue-post">
          <p class="queue-author">
            <a href={authorHref(p.author.handle)} data-author-link>
              @{p.author.handle}
            </a>{' '}
            <span class="admin-muted">{when(p.created_at)}</span>
            {p.deleted && <span class="admin-muted"> · {removedMark(p)}</span>}
          </p>
          {p.html ? (
            <div class="post-body" dangerouslySetInnerHTML={{ __html: p.html }} />
          ) : (
            <p class="admin-muted">This post has no words to show.</p>
          )}
        </div>
      ) : (
        <p class="admin-muted">This post cannot be shown just now. Open the thread to see it.</p>
      )}
      <ul class="queue-reports" aria-label="Flags">
        {flag.flags.map((f) => (
          <li key={f.id} data-report={f.id}>
            <strong>{reasonWords[f.reason] ?? f.reason}</strong>
            {f.note && <span class="queue-report-note"> “{f.note}”</span>}{' '}
            <span class="admin-muted">
              from @{f.reporter.handle}, {when(f.created_at)}
            </span>
          </li>
        ))}
      </ul>
      <Field label="Note for the record (optional)">
        {(c) => (
          <input
            {...c}
            type="text"
            maxLength={500}
            value={note}
            onInput={(e) => setNote((e.target as HTMLInputElement).value)}
          />
        )}
      </Field>
      <div class="queue-actions" data-queue-actions>
        {state !== 'moderator' && state !== 'author' && (
          <ConfirmButton
            label="Remove post"
            prompt="Hide this post as a moderator's removal and close its flags?"
            confirmLabel="Remove"
            danger
            onConfirm={() => act('remove')}
          />
        )}
        {state === 'moderator' ? (
          <span class="admin-muted" data-keep-refused>
            A moderator hid this post. To show it again, restore it from the thread page.
          </span>
        ) : (
          <ConfirmButton
            label="Keep post"
            prompt={restores ? 'Show this post again and close its flags?' : 'Keep this post and close its flags?'}
            confirmLabel="Keep"
            onConfirm={() => act('resolve')}
          />
        )}
        <ConfirmButton
          label="Dismiss flags"
          prompt={restores ? 'Dismiss the flags and show this post again?' : 'Dismiss the flags as unfounded?'}
          confirmLabel="Dismiss"
          onConfirm={() => act('dismiss')}
        />
      </div>
    </article>
  );
}

// resolvedWords says what an action did.
export function resolvedWords(r: QueueResolution): string {
  const flags = r.flags.length === 1 ? 'The flag is' : `The ${r.flags.length} flags are`;
  const closed = r.action === 'dismiss' ? 'dismissed' : 'resolved';
  switch (r.post) {
    case 'hidden':
      return `Post removed. ${flags} resolved.`;
    case 'restored':
      return `Post shown again. ${flags} closed.`;
    default:
      return `${flags} ${closed}.`;
  }
}

function ApplicationItem({ item, onDone }: { item: QueueItem; onDone: (it: QueueItem, words: string) => void }) {
  const app = item.application!;
  const url = `/api/v1/admin/applications/${app.id}`;
  const [open, setOpen] = useState(false);
  const detail = useResource(open ? url : null, (signal) => apiGet<AdminApplication>(url, { signal }));
  const id = useId();
  const changed = (a: AdminApplication, notice: string) => {
    if (!hasStatus(a, 'pending')) onDone(item, notice);
  };
  return (
    <article class="queue-application" aria-labelledby={id}>
      <header class="queue-head">
        <h2 id={id}>
          <a href={applicationHref(app.id)}>Application #{app.id}</a>
        </h2>
        <span class="admin-mark">Application</span>
        <span class="admin-muted">submitted {when(app.created_at)}</span>
      </header>
      <p>
        <ApplicantCell provider={app.provider} email={app.email} verified={app.email_verified} />
      </p>
      {!open ? (
        <button type="button" class="admin-button" onClick={() => setOpen(true)} data-review>
          Review
        </button>
      ) : detail.error && !detail.data ? (
        isAbort(detail.error) ? null : <ErrorPanel error={detail.error} what="The application" onRetry={detail.retry} />
      ) : !detail.data ? (
        <p class="admin-muted" aria-live="polite">
          Loading…
        </p>
      ) : hasStatus(detail.data, 'pending') ? (
        <Decision app={detail.data} url={url} onChange={changed} />
      ) : (
        <p>
          This application is already {detail.data.status}. <a href={applicationHref(app.id)}>Open it</a>
        </p>
      )}
    </article>
  );
}
