import { computed } from '@preact/signals';
import type { IconName } from '../components/Icon';
import { badges, viewer } from '../state/viewer';

// A destination in the navigation. Both layouts draw from these, so a badge
// on one is the same number on the other (docs/06 "Mobile").
export interface NavItem {
  id: string;
  label: string;
  href: string;
  icon: IconName;
  badge?: number;
}

// meHref is where the Me tab and the top bar's user go from url, the
// address the reader is at: their profile, or, signed out, the server's
// login page, which comes back to url once they have signed in
// (TKT-01M3MFS6KM). /auth/* is outside clientScope (routes.tsx), so the
// link is a full page load and reaches the server.
export function meHref(url: string): string {
  const u = viewer.value.user;
  return u ? `/u/${encodeURIComponent(u.handle)}` : `/auth/signin?return_to=${encodeURIComponent(url)}`;
}

// tabs are the phone's five (docs/06 "Mobile", mockup mtabbar()), from
// url, the address the reader is at. Alerts routes to /notifications
// (decided at claim for TKT-01M29CEE4K).
export const tabs = (url: string): NavItem[] => [
  { id: 'forums', label: 'Forums', href: '/', icon: 'home' },
  { id: 'unread', label: 'Unread', href: '/unread', icon: 'list', badge: badges.value.unread },
  { id: 'new', label: 'New', href: '/new', icon: 'clock' },
  { id: 'alerts', label: 'Alerts', href: '/notifications', icon: 'bell', badge: badges.value.alerts },
  { id: 'me', label: 'Me', href: meHref(url), icon: 'user' },
];

// links are the desktop top bar's (mockup nav()).
export const links = computed<Omit<NavItem, 'icon'>[]>(() => [
  { id: 'forums', label: 'Forums', href: '/' },
  { id: 'unread', label: 'Unread', href: '/unread', badge: badges.value.unread },
  { id: 'new', label: 'New', href: '/new' },
  { id: 'participated', label: 'Participated', href: '/participated' },
  { id: 'watching', label: 'Watching', href: '/watching' },
]);

export const alerts = computed(() => tabs('/').find((t) => t.id === 'alerts')!);

// current says whether href is the section the reader is in: / only for
// itself, anything else for itself and what is under it.
export function current(href: string, path: string): boolean {
  if (href === '/') return path === '/' || path.startsWith('/f/') || path.startsWith('/t/');
  return path === href || path.startsWith(href + '/');
}

// badgeLabel is a count as the badge shows it, and nothing for none.
export const badgeLabel = (n: number | undefined) => (n && n > 0 ? (n > 99 ? '99+' : String(n)) : null);
