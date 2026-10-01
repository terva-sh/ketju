import type { AnyComponent } from 'preact';
import { useEffect } from 'preact/hooks';
import { lazy, useLocation } from 'preact-iso';
import { crossListScreen } from './screens/CrossListScreen';
import { ForumIndex } from './screens/ForumIndex';
import { NewThreadScreen } from './screens/NewThreadScreen';
import { NotificationsScreen } from './screens/NotificationsScreen';
import { ThreadListScreen } from './screens/ThreadListScreen';
import { NotFound, Placeholder } from './screens/Placeholder';
// The account screens, a member's profile and posts (TKT-01M3V3CFMA), are
// in the reader's own chunk rather than a lazy one. Measured both ways, the
// split cost 2182 B more in all (69818 B against 67636 B), because the
// budget counts every chunk but admin and the split added a shared chunk of
// what they have in common with the reader (docs/07 "Preact client").
import { ProfilePostsScreen, ProfileScreen } from './screens/account/AccountRoot';
import { SearchScreen } from './screens/SearchScreen';
import { ThreadScreen } from './screens/ThreadScreen';

// The admin screens are their own chunk, fetched the first time an /admin
// path renders, so a reader never downloads them (TKT-01M29CEE52; the thread
// view's bundle budget in TKT-01M29CGBYH depends on it).
const Admin = lazy(() => import('./screens/admin/AdminRoot'));

// The application screens are one chunk too, fetched the first time /apply
// or /apply/status renders (TKT-01M29CP2NG). Unlike the admin chunk they
// count toward the bundle budget.
const ApplyForm = lazy(() => import('./screens/apply/ApplyRoot').then((m) => m.ApplyScreen));
const ApplyStatus = lazy(() => import('./screens/apply/ApplyRoot').then((m) => m.StatusScreen));

export interface RouteEntry {
  // A preact-iso pattern: docs/07's {param} written as :param.
  path: string;
  // The screen's name, rendered as data-screen so a test can tell which one
  // answered.
  screen: string;
  component: AnyComponent<any>;
}

const placeholder = (screen: string, title: string): RouteEntry['component'] =>
  function Screen() {
    return <Placeholder screen={screen} title={title} />;
  };

// clientScope is the set of links the client may follow itself, given to
// LocationProvider as its scope. preact-iso intercepts every same-origin link
// click it is in scope for and routes it in the page, so a link to a path the
// server owns — /auth/github/start above all, which must reach the server to
// start sign-in — has to be out of scope, or it renders the client's
// NotFound instead of leaving. The prefixes are the server's groups in
// internal/web/server.go (docs/02).
//
// preact-iso tests the raw href attribute, so an absolute or
// protocol-relative href to the same origin is matched too. A raw href can
// spell a path many ways (/./auth, /x/../auth, /%61uth), and no pattern over
// the spelling catches them all; Fallback below catches the rest, after the
// browser has resolved the path. Case matters, as it does to the server's
// ServeMux: /AUTH is an application route there, and here.
export const clientScope =
  /^(?!(?:[a-z][a-z0-9+.-]*:)?(?:\/\/[^/]*)?\/(?:(?:auth|api|blobs|assets)(?:[/?#]|$)|events(?:[?#]|$)))/;

// serverOwned reports whether the server answers a resolved pathname rather
// than the client, following internal/web/server.go exactly, because a
// guess in either direction is a bug: too narrow and a sign-in link renders
// the client's NotFound, too wide and the server answers with the shell and
// the client hands it back.
//
//   - /auth/, /blobs/ and /assets/ are ServeMux subtrees. ServeMux matches
//     them segment by segment, unescaping each segment, so /%61uth/x is
//     /auth/ but /auth%2Fx is not: an escaped slash is not a boundary. The
//     bare /auth, /blobs and /assets redirect into their subtree.
//   - /events is an exact pattern: /events/x is an application route.
//   - /api is claimed by the fallback handler's isAPIPath, which tests the
//     decoded path, so /api%2Fx is the API's too.
//
// A malformed escape is refused by the server before routing, and names
// nothing here.
export function serverOwned(pathname: string): boolean {
  const segments = pathname.split('/');
  let first: string;
  let whole: string;
  try {
    first = decodeURIComponent(segments[1] ?? '');
    whole = decodeURIComponent(pathname);
  } catch {
    return false;
  }
  if (whole === '/api' || whole.startsWith('/api/')) return true;
  if (first === 'events') return segments.length === 2;
  return first === 'auth' || first === 'blobs' || first === 'assets';
}

// nav.leave hands the address to the server with a full load, in place of
// the entry the router just pushed. An object so a test can intercept it.
//
// nav.loadedAt is the address the server served this document at. Fallback
// never hands that one back: the server has already answered it with the
// shell, and handing it back would reload the page forever if the rules
// above and the server's ever disagree.
export const nav = {
  loadedAt: location.pathname + location.search,
  leave(url: string) {
    location.replace(url);
  },
};

// Fallback is the route for an address nothing in the table matches. If the
// browser resolved it to a server-owned path — a link clientScope could not
// recognise from its spelling — the server gets it; otherwise it is the
// client's NotFound.
export function Fallback() {
  const { url } = useLocation();
  const leaving = serverOwned(location.pathname) && location.pathname + location.search !== nav.loadedAt;
  useEffect(() => {
    if (leaving) nav.leave(location.href);
  }, [url, leaving]);
  return leaving ? null : <NotFound />;
}

// One entry per path in docs/07's URL scheme, which both clients serve, so a
// route here is a URL the HTML client will answer too. /auth/* is not here:
// the server owns it and the client never renders it. routes.test.tsx diffs
// this table against the document in both directions.
//
// Page and lens are query parameters of the thread route, not routes: see
// view.ts.
export const routes: RouteEntry[] = [
  { path: '/', screen: 'forum-index', component: ForumIndex },
  { path: '/f/:forum', screen: 'thread-list', component: ThreadListScreen },
  { path: '/f/:forum/new', screen: 'new-thread', component: NewThreadScreen },
  { path: '/t/:id/:slug', screen: 'thread', component: ThreadScreen },
  { path: '/t/:id/:slug/:ordinal', screen: 'thread', component: ThreadScreen },
  { path: '/t/:id/:slug/:ordinal/history', screen: 'post-history', component: ThreadScreen },
  { path: '/unread', screen: 'unread', component: crossListScreen('unread') },
  { path: '/new', screen: 'new', component: crossListScreen('new') },
  { path: '/participated', screen: 'participated', component: crossListScreen('participated') },
  { path: '/watching', screen: 'watching', component: crossListScreen('watching') },
  { path: '/notifications', screen: 'notifications', component: NotificationsScreen },
  { path: '/search', screen: 'search', component: SearchScreen },
  { path: '/u/:handle', screen: 'profile', component: ProfileScreen },
  { path: '/u/:handle/posts', screen: 'profile-posts', component: ProfilePostsScreen },
  { path: '/settings', screen: 'settings', component: placeholder('settings', 'Settings') },
  { path: '/settings/sessions', screen: 'settings-sessions', component: placeholder('settings-sessions', 'Sessions') },
  { path: '/settings/identities', screen: 'settings-identities', component: placeholder('settings-identities', 'Sign-in identities') },
  { path: '/apply', screen: 'apply', component: ApplyForm },
  { path: '/apply/status', screen: 'apply-status', component: ApplyStatus },
  { path: '/admin', screen: 'admin', component: Admin },
  { path: '/admin/queue', screen: 'admin-queue', component: Admin },
  { path: '/admin/applications', screen: 'admin-applications', component: Admin },
  { path: '/admin/forums', screen: 'admin-forums', component: Admin },
  { path: '/admin/users', screen: 'admin-users', component: Admin },
  { path: '/admin/forms', screen: 'admin-forms', component: Admin },
  { path: '/admin/settings', screen: 'admin-settings', component: Admin },
  { path: '/admin/audit', screen: 'admin-audit', component: Admin },
];
