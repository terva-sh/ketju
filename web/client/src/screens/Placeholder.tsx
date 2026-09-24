import { useShellContext } from '../state/shell';
import { viewer } from '../state/viewer';

// Placeholder stands in for a screen phase 1 has not built yet, so every URL
// in docs/07 already answers with the right screen's name rather than
// NotFound. Each screen's ticket replaces its entry in routes.tsx.
export function Placeholder({ screen, title }: { screen: string; title: string }) {
  useShellContext({ title, actions: [] }, [title]);
  return (
    <main class="scaffold" data-screen={screen}>
      <h1>{title}</h1>
      <p>This screen is not built yet.</p>
    </main>
  );
}

export function ForumIndex() {
  const me = viewer.value;
  return (
    <main class="scaffold" data-screen="forum-index">
      <h1>ketju</h1>
      <p>{me.user ? `Signed in as ${me.user.display_name}.` : 'Not signed in.'}</p>
    </main>
  );
}

export function NotFound() {
  return (
    <main class="scaffold" data-screen="not-found">
      <h1>Not found</h1>
      <p>
        There is nothing at this address. <a href="/">Go to the forum index.</a>
      </p>
    </main>
  );
}
