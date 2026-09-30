import { computed, signal } from '@preact/signals';
import { apiGet } from '../api/client';
import type { Me } from '../api/dto';
import { readBootstrap } from '../bootstrap';

// viewer is who is reading, seeded from the bootstrap block the shell inlines
// (docs/07 "Embedding") so the first paint needs no request, and replaced
// whenever /me is fetched again.
export const viewer = signal<Me>(readBootstrap());

// notifications is the viewer's unread notification count. It is derived,
// not stored, so it cannot disagree with the viewer it came from.
export const notifications = computed(() => viewer.value.notifications);

// badges are the counts the navigation shows, the same numbers at every
// width (docs/06 "Mobile"). Only counts /me provides are here: it has none
// for New, so New shows no badge until one exists (settled in grooming for
// TKT-01M29CEE4K). queue is the moderator queue's, null for a viewer with no
// queue, who is offered none (TKT-01M29CRJ4E).
export const badges = computed(() => ({
  unread: viewer.value.unread.threads,
  alerts: viewer.value.notifications,
  queue: viewer.value.queue ?? null,
}));

// siteName is the wordmark: the site_name setting, which /me carries for
// every viewer (TKT-01M29CXK2H), so an admin's change shows from the next
// /me. The product's name stands in for an empty one.
export const siteName = computed(() => viewer.value.site_name || 'Ketju');

let refreshes = 0;

// refreshViewer fetches /me and replaces the viewer with it. A failure leaves
// the viewer as it was and is the caller's to report.
//
// Only the newest refresh replaces the viewer, and not if it was aborted:
// refreshes can overlap (a sign-in, then a sign-out) and finish in any order,
// and an older one landing last would put back a viewer that is no longer
// signed in, notification count and all.
export async function refreshViewer(signal?: AbortSignal): Promise<Me> {
  const n = ++refreshes;
  const me = await apiGet<Me>('/api/v1/me', { signal });
  if (n === refreshes && !signal?.aborted) viewer.value = me;
  return me;
}
