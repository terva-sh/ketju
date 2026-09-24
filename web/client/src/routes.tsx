import type { AnyComponent } from 'preact';
import { lazy } from 'preact-iso';
import { ForumIndex, Placeholder } from './screens/Placeholder';
import { ThreadScreen } from './screens/ThreadScreen';

// The admin screens are their own chunk, fetched the first time an /admin
// path renders, so a reader never downloads them (TKT-01M29CEE52; the thread
// view's bundle budget in TKT-01M29CGBYH depends on it).
const Admin = lazy(() => import('./screens/admin/AdminRoot'));

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

// One entry per path in docs/07's URL scheme, which both clients serve, so a
// route here is a URL the HTML client will answer too. /auth/* is not here:
// the server owns it and the client never renders it. routes.test.tsx diffs
// this table against the document in both directions.
//
// Page and lens are query parameters of the thread route, not routes: see
// view.ts.
export const routes: RouteEntry[] = [
  { path: '/', screen: 'forum-index', component: ForumIndex },
  { path: '/f/:forum', screen: 'thread-list', component: placeholder('thread-list', 'Threads') },
  { path: '/f/:forum/new', screen: 'new-thread', component: placeholder('new-thread', 'New thread') },
  { path: '/t/:id/:slug', screen: 'thread', component: ThreadScreen },
  { path: '/t/:id/:slug/:ordinal', screen: 'thread', component: ThreadScreen },
  { path: '/t/:id/:slug/:ordinal/history', screen: 'post-history', component: ThreadScreen },
  { path: '/unread', screen: 'unread', component: placeholder('unread', 'Unread') },
  { path: '/new', screen: 'new', component: placeholder('new', 'New') },
  { path: '/participated', screen: 'participated', component: placeholder('participated', 'Participated') },
  { path: '/watching', screen: 'watching', component: placeholder('watching', 'Watching') },
  { path: '/search', screen: 'search', component: placeholder('search', 'Search') },
  { path: '/u/:handle', screen: 'profile', component: placeholder('profile', 'Profile') },
  { path: '/u/:handle/posts', screen: 'profile-posts', component: placeholder('profile-posts', 'Posts') },
  { path: '/settings', screen: 'settings', component: placeholder('settings', 'Settings') },
  { path: '/settings/sessions', screen: 'settings-sessions', component: placeholder('settings-sessions', 'Sessions') },
  { path: '/settings/identities', screen: 'settings-identities', component: placeholder('settings-identities', 'Sign-in identities') },
  { path: '/apply', screen: 'apply', component: placeholder('apply', 'Apply to join') },
  { path: '/apply/status', screen: 'apply-status', component: placeholder('apply-status', 'Application status') },
  { path: '/admin', screen: 'admin', component: Admin },
  { path: '/admin/queue', screen: 'admin-queue', component: Admin },
  { path: '/admin/applications', screen: 'admin-applications', component: Admin },
  { path: '/admin/forums', screen: 'admin-forums', component: Admin },
  { path: '/admin/users', screen: 'admin-users', component: Admin },
  { path: '/admin/forms', screen: 'admin-forms', component: Admin },
  { path: '/admin/settings', screen: 'admin-settings', component: Admin },
];
