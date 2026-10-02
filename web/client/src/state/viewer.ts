import { computed, signal } from '@preact/signals';
import { apiGet } from '../api/client';
import type { Me, Settings } from '../api/dto';
import { readBootstrap } from '../bootstrap';
import { syncThemeFromAccount } from './theme';
import { forgetAllThreadPages } from './threads';

// viewer is who is reading, seeded from the bootstrap block the shell inlines
// (docs/07 "Embedding") so the first paint needs no request, and replaced
// whenever /me is fetched again.
export const viewer = signal<Me>(readBootstrap());

// notifications is the viewer's unread notification count. It is derived,
// not stored, so it cannot disagree with the viewer it came from.
export const notifications = computed(() => viewer.value.notifications);

// badges are the counts the navigation shows, the same numbers at every
// width (docs/06 "Mobile"). Only counts /me provides are here: unread and
// new are the rows of /unread and /new (TKT-01M29CTY7K), and Participated
// and Watching keep no badge, as in the mockup's nav() (TKT-01M3V6Y4JP).
// queue is the moderator queue's, null for a viewer with no queue, who is
// offered none (TKT-01M29CRJ4E).
export const badges = computed(() => ({
  unread: viewer.value.unread.threads,
  new: viewer.value.unread.new,
  alerts: viewer.value.notifications,
  queue: viewer.value.queue ?? null,
}));

// siteName is the wordmark: the site_name setting, which /me carries for
// every viewer (TKT-01M29CXK2H), so an admin's change shows from the next
// /me. The product's name stands in for an empty one.
export const siteName = computed(() => viewer.value.site_name || 'Ketju');

let refreshes = 0;
// settingsTaken counts the settings answers taken from GET or PATCH
// /settings (setViewerSettings).
let settingsTaken = 0;

// setViewerSettings makes s, the account's settings as GET or PATCH
// /settings answered, the viewer's. A /me asked for before it may carry
// the settings from before, and refreshViewer keeps these over them
// (Terva review 1876). It reports whether anything changed.
export function setViewerSettings(s: Settings): boolean {
  settingsTaken++;
  const was = viewer.value.settings;
  if (
    was.timezone === s.timezone &&
    was.locale === s.locale &&
    was.page_size === s.page_size &&
    was.theme === s.theme &&
    was.shortcuts === s.shortcuts
  )
    return false;
  setViewer({ ...viewer.value, settings: s });
  return true;
}

// setViewer replaces the viewer. A page size other than the one the
// thread pages held were cut at drops them all, however the client learned
// of it: pages cut at 25 would otherwise sit beside pages cut at 10 under
// one log length (Terva review 1879).
function setViewer(me: Me) {
  const size = viewer.value.settings.page_size;
  viewer.value = me;
  if (me.settings.page_size !== size) forgetAllThreadPages();
}

// refreshViewer fetches /me and replaces the viewer with it, and takes the
// account's theme (state/theme.ts). A failure leaves the viewer as it was
// and is the caller's to report.
//
// Only the newest refresh replaces the viewer, and not if it was aborted:
// refreshes can overlap (a sign-in, then a sign-out) and finish in any order,
// and an older one landing last would put back a viewer that is no longer
// signed in, notification count and all. Settings taken from /settings
// since the refresh was asked for are newer than its answer's, and are
// kept, for the same viewer.
export async function refreshViewer(signal?: AbortSignal): Promise<Me> {
  const n = ++refreshes;
  const since = settingsTaken;
  const me = await apiGet<Me>('/api/v1/me', { signal });
  if (n === refreshes && !signal?.aborted) {
    const keep = settingsTaken !== since && me.user?.handle === viewer.value.user?.handle;
    setViewer(keep ? { ...me, settings: viewer.value.settings } : me);
    syncThemeFromAccount(viewer.value);
  }
  return me;
}
