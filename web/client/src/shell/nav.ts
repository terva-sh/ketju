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
  // what is what the badge counts, in words for a screen reader (Badge).
  what?: string;
}

// meHref is where the Me tab and the top bar's user go from url, the
// address the reader is at: their profile, or, signed out, the server's
// login page, which comes back to url once they have signed in
// (TKT-01M3MFS6KM). /auth/* is outside clientScope (routes.tsx), so the
// link is a full page load and reaches the server.
export function meHref(url: string): string {
  const u = viewer.value.user;
  return u ? profileHref(u.handle) : `/auth/signin?return_to=${encodeURIComponent(url)}`;
}

// profileHref is a member's profile (docs/07 "URL scheme"), where an
// author's name in a post header links (TKT-01M3V3CFMA).
export const profileHref = (handle: string) => `/u/${encodeURIComponent(handle)}`;

// linkSignInHref is the server's "link a sign-in" page (TKT-01M29CKRES,
// docs/05 "Login flow" step 3), which comes back to url, the path and
// query the reader is at, once the member has linked, or chosen not to. It
// is outside clientScope too, so the link is a full page load. The entry
// is on the member's own profile until the settings screens
// (TKT-01M29CXK4) build /settings/identities, whose "add" is this link.
export function linkSignInHref(url: string): string {
  return `/auth/link?return_to=${encodeURIComponent(url)}`;
}

// sameHandle says whether two spellings name one handle. Handles are stored
// with ASCII letters lowercased and compared without case (NormalizeHandle
// in internal/service/identity/handle.go), so only A-Z fold here:
// toLowerCase would fold more than the server does, the Kelvin sign to a
// "k" among others.
export function sameHandle(a: string, b: string): boolean {
  const fold = (s: string) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
  return fold(a) === fold(b);
}

// tabs are the phone's five (docs/06 "Mobile", mockup mtabbar()), from
// url, the address the reader is at. Alerts routes to /notifications
// (decided at claim for TKT-01M29CEE4K).
export const tabs = (url: string): NavItem[] => [
  { id: 'forums', label: 'Forums', href: '/', icon: 'home' },
  { id: 'unread', label: 'Unread', href: '/unread', icon: 'list', badge: badges.value.unread, what: 'unread threads' },
  { id: 'new', label: 'New', href: '/new', icon: 'clock', badge: badges.value.new, what: 'new threads' },
  { id: 'alerts', label: 'Alerts', href: '/notifications', icon: 'bell', badge: badges.value.alerts, what: 'notifications' },
  { id: 'me', label: 'Me', href: meHref(url), icon: 'user' },
];

// links are the desktop top bar's (mockup nav()).
export const links = computed<Omit<NavItem, 'icon'>[]>(() => [
  { id: 'forums', label: 'Forums', href: '/' },
  { id: 'unread', label: 'Unread', href: '/unread', badge: badges.value.unread, what: 'unread threads' },
  { id: 'new', label: 'New', href: '/new', badge: badges.value.new, what: 'new threads' },
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
