import { useLocation } from 'preact-iso';

// The admin sections, by path. Everything under screens/admin is reached only
// through the lazy import in routes.tsx, which is what keeps it out of the
// reader's bundle; importing anything here from outside that directory would
// pull it back in, and the manifest test in internal/web/spa would fail.
const sections: Record<string, [screen: string, title: string]> = {
  '/admin': ['admin', 'Administration'],
  '/admin/queue': ['admin-queue', 'Moderation queue'],
  '/admin/applications': ['admin-applications', 'Applications'],
  '/admin/forums': ['admin-forums', 'Forums'],
  '/admin/users': ['admin-users', 'Users'],
  '/admin/forms': ['admin-forms', 'Application forms'],
  '/admin/settings': ['admin-settings', 'Site settings'],
};

export default function AdminRoot() {
  const { path } = useLocation();
  const [screen, title] = sections[path] ?? sections['/admin'];
  return (
    <main class="scaffold" data-screen={screen}>
      <h1>{title}</h1>
      <p>This screen is not built yet.</p>
    </main>
  );
}
