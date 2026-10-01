import { apiGet, apiSend } from '../api/client';
import type { NotificationList, NotificationsRead } from '../api/dto';
import { refreshViewer } from './viewer';

// The viewer's notifications (docs/07 GET /notifications, TKT-01M29CRJ2T).
// A notification carries no post text, and nothing here fetches any: a row
// names what happened and where, and its link sends the reader to the thread
// page, which decides what they may read there (docs/04 "notifications").

const readUrl = '/api/v1/notifications/read';

// notificationsUrl is one page of the list: the first without a cursor, then
// each next_cursor in turn.
export function notificationsUrl(cursor?: string): string {
  return '/api/v1/notifications' + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : '');
}

export function loadNotifications(cursor: string | undefined, signal?: AbortSignal): Promise<NotificationList> {
  return apiGet<NotificationList>(notificationsUrl(cursor), { signal });
}

// markViewed marks the rows the reader was shown, by id, and fetches /me
// again so the bell and the Alerts tab drop at once. A failed /me leaves the
// badge as it was until the next page load; the rows are marked either way.
export async function markViewed(ids: number[]): Promise<void> {
  if (ids.length === 0) return;
  await apiSend<NotificationsRead>('POST', readUrl, { ids });
  await refreshViewer().catch(() => {});
}

// markAll marks every unread row at or below upTo, the first row of the
// list's first page. The list is in id order, newest first, so that is the
// highest id the reader was shown, and rows not yet paged in are cleared too
// (review 1709). A row written after the page was fetched has a higher id
// and stays unread, so mark-all never clears an alert nobody has seen.
export async function markAll(upTo: number): Promise<NotificationsRead> {
  const r = await apiSend<NotificationsRead>('POST', readUrl, { up_to: upTo });
  await refreshViewer().catch(() => {});
  return r;
}
