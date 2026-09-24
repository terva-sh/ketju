import { signal } from '@preact/signals';
import { useEffect } from 'preact/hooks';

// ShellAction is one action on what the screen shows: the thread toolbar's
// buttons on desktop, the phone top bar's "more" menu (docs/06 "Mobile").
// Both render the same list, so an action on one is on the other by
// construction.
export interface ShellAction {
  id: string;
  label: string;
  run: () => void;
}

// ShellContext is what the phone top bar needs from the screen under it: a
// title, where back goes, and the screen's actions. A screen that sets none
// gets the wordmark and no menu.
export interface ShellContext {
  title: string;
  back?: { href: string; label: string };
  actions: ShellAction[];
}

export const shellContext = signal<ShellContext | null>(null);

// useShellContext publishes a screen's context while it is mounted, and
// takes it down with the screen, unless a newer screen has already put up
// its own.
export function useShellContext(ctx: ShellContext | null, deps: unknown[]) {
  useEffect(() => {
    shellContext.value = ctx;
    return () => {
      if (shellContext.value === ctx) shellContext.value = null;
    };
  }, deps);
}
