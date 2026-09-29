import type { FunctionComponent } from 'preact';
import { useLocation } from 'preact-iso';
import { useShellContext } from '../../state/shell';
import { viewer } from '../../state/viewer';
import { sections, sectionsFor } from './sections';
import { UsersScreen } from './UsersScreen';
import './admin.css';

// built is each section's screen, by its screen name, as its ticket builds
// it. A section with none says it is not built yet.
const built: Record<string, FunctionComponent> = {
  'admin-users': UsersScreen,
};

// AdminRoot is the admin layout: the list of sections the viewer may use,
// and the section the address names. Everything under screens/admin is
// reached only through the lazy import in routes.tsx, which is what keeps it
// out of the reader's bundle; importing anything here from outside that
// directory would pull it back in, and the manifest test in internal/web/spa
// would fail. Shared pieces for the sections are in ./common.
//
// The list is an offer, not a permission: each section's API is behind the
// server's gate (internal/web/api/admin.go), and a section a viewer reaches
// by typing its address still gets the server's refusal.
export default function AdminRoot() {
  const { path } = useLocation();
  const me = viewer.value;
  const offered = sectionsFor(me);
  const section = sections.find((s) => s.path === path);
  const title = section?.title ?? 'Administration';
  useShellContext({ title, actions: [] }, [title]);

  let body;
  if (offered.length === 0) {
    body = <p>Your account does not have access to administration.</p>;
  } else if (!section) {
    body = <p>Choose a section.</p>;
  } else if (!section.may(me)) {
    body = <p>Your account does not have access to this section.</p>;
  } else {
    const Screen = built[section.screen];
    body = Screen ? <Screen /> : <p>This screen is not built yet.</p>;
  }

  return (
    <main class="admin" data-screen={section?.screen ?? 'admin'}>
      {offered.length > 0 && (
        <nav class="admin-nav" aria-label="Administration sections" data-admin-sections>
          <ul>
            {offered.map((s) => (
              <li key={s.path}>
                <a href={s.path} aria-current={s.path === path ? 'page' : undefined}>
                  {s.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      )}
      <section class="admin-section">
        <h1>{title}</h1>
        {body}
      </section>
    </main>
  );
}
