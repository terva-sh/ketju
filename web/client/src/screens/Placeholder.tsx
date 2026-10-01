import type { ComponentChildren } from 'preact';
import { useShellContext } from '../state/shell';

// Placeholder stands in for a screen phase 1 has not built yet, so every URL
// in docs/07 already answers with the right screen's name rather than
// NotFound. Each screen's ticket replaces its entry in routes.tsx. children
// are anything a placeholder carries before its screen is built.
export function Placeholder({ screen, title, children }: { screen: string; title: string; children?: ComponentChildren }) {
  useShellContext({ title, actions: [] }, [title]);
  return (
    <main class="scaffold" data-screen={screen}>
      <h1>{title}</h1>
      <p>This screen is not built yet.</p>
      {children}
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
