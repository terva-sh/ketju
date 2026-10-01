import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { Notification } from '../api/dto';
import { asProblem, problemMessage, type ProblemError } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { permalink } from '../components/thread/Post';
import { PostTime } from '../components/thread/PostTime';
import { loadNotifications, markAll, markViewed, notificationsUrl } from '../state/notifications';
import { useShellContext } from '../state/shell';
import './notifications.css';

// The words for an empty list: what would appear here, so a member who has
// none yet knows what the screen is for.
export const emptyAlerts =
  'No alerts yet. You are alerted here when someone mentions you, replies to or quotes one of your posts, or posts in a thread you watch.';

// Older is what "Older" has added below the first page: its rows, the
// cursor after them (empty at the end), and the request in flight.
interface Older {
  rows: Notification[];
  next: string;
  loading: boolean;
  error: ProblemError | undefined;
}

// NotificationsScreen is the viewer's alerts (TKT-01M29CTY86), the screen the
// desktop bell and the phone's Alerts tab both open. Each row is one sentence
// naming who did what and where, and its link is the permalink of the post
// that caused it, which serves the page holding the post and anchors it
// (docs/07 "URL scheme"). No row carries any of the post's text, and the
// screen fetches none (docs/04 "notifications").
//
// The rows a page shows are marked read once, as soon as they are shown, and
// /me is fetched again so the badges drop. They keep their unread marking
// until the reader leaves, so the reader can still see which were new.
// "Mark all read" clears everything up to the first row, paged in or not.
// Coming back fetches the list again.
export function NotificationsScreen() {
  useShellContext({ title: 'Alerts', actions: [] }, []);
  const key = notificationsUrl();
  const first = useResource(key, (signal) => loadNotifications(undefined, signal));
  const [older, setOlder] = useState<Older | null>(null);
  const [cleared, setCleared] = useState<'idle' | 'busy' | 'done'>('idle');
  const [clearError, setClearError] = useState<ProblemError | undefined>(undefined);
  const [focusId, setFocusId] = useState<number | null>(null);
  const root = useRef<HTMLElement>(null);
  const posted = useRef(new Set<number>());

  const rows = useMemo(() => (first.data ? [...first.data.notifications, ...(older?.rows ?? [])] : []), [first.data, older?.rows]);
  const next = older ? older.next : (first.data?.next_cursor ?? '');

  // Each row's unread id is posted once, when the row is first shown. A
  // failed mark is not retried here; the row comes back unread on the next
  // visit and is marked then.
  useEffect(() => {
    const ids = rows.filter((n) => !n.read && !posted.current.has(n.id)).map((n) => n.id);
    if (ids.length === 0) return;
    for (const id of ids) posted.current.add(id);
    markViewed(ids).catch(() => {});
  }, [rows]);

  // "Older" moves focus to the first row it added, so a keyboard or screen
  // reader user carries on from where the list grew.
  useEffect(() => {
    if (focusId === null) return;
    root.current?.querySelector<HTMLElement>(`[data-alert="${focusId}"]`)?.focus();
    setFocusId(null);
  }, [focusId]);

  const loadOlder = async () => {
    if (!next || older?.loading) return;
    setOlder((o) => ({ rows: o?.rows ?? [], next, loading: true, error: undefined }));
    try {
      const page = await loadNotifications(next);
      setOlder((o) => ({ rows: [...(o?.rows ?? []), ...page.notifications], next: page.next_cursor, loading: false, error: undefined }));
      if (page.notifications.length > 0) setFocusId(page.notifications[0].id);
    } catch (err) {
      setOlder((o) => ({ rows: o?.rows ?? [], next, loading: false, error: asProblem(err) }));
    }
  };

  const clearAll = async () => {
    if (rows.length === 0 || cleared === 'busy') return;
    setCleared('busy');
    setClearError(undefined);
    try {
      await markAll(rows[0].id);
      setCleared('done');
    } catch (err) {
      setClearError(asProblem(err));
      setCleared('idle');
    }
  };

  return (
    <main class="alerts" data-screen="notifications" ref={root}>
      <header class="alerts-head">
        <h1>Alerts</h1>
        {rows.length > 0 && (
          <button type="button" class="alerts-clear" onClick={clearAll} disabled={cleared === 'busy'} data-mark-all>
            Mark all read
          </button>
        )}
      </header>
      <p class="alerts-status" role="status">
        {cleared === 'done' ? 'All alerts are marked read.' : ''}
      </p>
      {clearError && (
        <p class="alerts-error" role="alert">
          The alerts could not be marked read. {problemMessage(clearError).text}
        </p>
      )}
      {first.error && !first.data && <ErrorPanel error={first.error} what="Your alerts" onRetry={first.retry} />}
      {first.data && rows.length === 0 && (
        <p class="alerts-empty" data-empty-list>
          {emptyAlerts}
        </p>
      )}
      {rows.length > 0 && (
        <ol class="alert-rows">
          {rows.map((n) => (
            <AlertRow key={n.id} n={n} unread={!n.read && cleared !== 'done'} />
          ))}
        </ol>
      )}
      {older?.error && <ErrorPanel error={older.error} what="Older alerts" onRetry={loadOlder} />}
      {first.data && next && !older?.error && (
        <div class="alerts-pages">
          <button type="button" class="alerts-older" onClick={loadOlder} disabled={older?.loading} data-older>
            {older?.loading ? 'Loading older alerts…' : 'Older'}
          </button>
        </div>
      )}
    </main>
  );
}

// AlertRow is one notification. An unread row is marked by a dot and weight
// as well as colour, and says "Unread" to a screen reader.
function AlertRow({ n, unread }: { n: Notification; unread: boolean }) {
  return (
    <li class={'alert-row' + (unread ? ' unread' : '')} data-alert={n.id} data-kind={n.kind} tabIndex={-1}>
      <span class="alert-dot" aria-hidden="true" />
      <div class="alert-main">
        <p class="alert-what">
          {unread && <span class="visually-hidden">Unread: </span>}
          {sentence(n)}
        </p>
        <p class="alert-meta">
          {n.forum && <span class="alert-forum">{n.forum.name}</span>}
          <PostTime at={n.at} />
        </p>
      </div>
    </li>
  );
}

// sentence is a row's words by kind (docs/04 "notifications"). The thread's
// title is the link, to the post that caused the alert. An actor is named as
// every author block names one, an anonymized account by its placeholder.
function sentence(n: Notification): ComponentChildren {
  const who = n.actor ? <span class="alert-actor">{n.actor.display_name || n.actor.handle}</span> : 'Someone';
  const where =
    n.thread && n.ordinal !== null ? (
      <a class="alert-thread hit" href={permalink(n.thread, n.ordinal)}>
        {n.thread.title}
      </a>
    ) : (
      'a thread'
    );
  switch (n.kind) {
    case 'mention':
      return (
        <>
          {who} mentioned you in {where}
        </>
      );
    case 'reply_to_you':
      return (
        <>
          {who} replied to you in {where}
        </>
      );
    case 'quoted':
      return (
        <>
          {who} quoted you in {where}
        </>
      );
    case 'thread_you_watch':
      return (
        <>
          {who} posted in {where}, which you watch
        </>
      );
    case 'application_decided':
      return 'Your application was approved';
    case 'new_application':
      return (
        <a class="alert-thread hit" href="/admin/applications">
          A new application is waiting
        </a>
      );
  }
  // A kind this client does not know yet still links to its post.
  return n.thread ? <>New activity in {where}</> : 'An alert this version cannot show';
}
