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
  // reply is the thread whose composer the phone's reply bar opens, set by
  // a thread page whose reader may reply there. The reply bar takes the tab
  // bar's place while it is set (docs/06 "Mobile", TKT-01M29CGBVN).
  reply?: string;
  // composing is set by a screen that is itself a composer, the new-thread
  // screen: on a phone the tab bar gives it its room, as the reply bar's
  // sheet takes the whole screen (docs/06 "Mobile").
  composing?: boolean;
  // primary is the one action such a screen exists for, Post, which the
  // phone top bar draws as a button of its own, since the screen's footer
  // would sit under the keyboard. The desktop draws neither: the screen's
  // footer carries it there.
  primary?: ShellAction & { disabled?: boolean };
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
