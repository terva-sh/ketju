import { useEffect, useLayoutEffect, useRef } from 'preact/hooks';

// useScrollMemory keeps a list's scroll position in its history entry, so
// going back to it lands where the reader was (TKT-01M29CEE6D). preact-iso
// scrolls to the top only on a push that changes the path, and restores
// nothing on back, so a list does both itself.
//
// The position is written into history.state with replaceState: after the
// reader stops scrolling, and at once on any click, in the capture phase,
// which runs before the router's own click handler pushes the next entry. A
// write per scroll event would be simplest, but Safari throttles
// replaceState, and a debounced write alone could still be pending when a
// link is followed, and would then land on the new entry.
//
// When the entry is shown and ready is true (its data is on screen), the
// saved position is restored once per key; an entry with none, a page just
// pushed, starts at the top.
export function useScrollMemory(key: string, ready: boolean) {
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const later = () => {
      clearTimeout(timer);
      timer = setTimeout(rememberScroll, 100);
    };
    const now = () => {
      clearTimeout(timer);
      rememberScroll();
    };
    addEventListener('scroll', later, { passive: true });
    addEventListener('click', now, true);
    return () => {
      clearTimeout(timer);
      removeEventListener('scroll', later);
      removeEventListener('click', now, true);
    };
  }, [key]);

  const restored = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (!ready || restored.current === key) return;
    restored.current = key;
    scrollTo(0, savedScroll() ?? 0);
  }, [key, ready]);
}

export function rememberScroll() {
  const state = history.state && typeof history.state === 'object' ? history.state : {};
  history.replaceState({ ...state, scrollY: Math.round(scrollY) }, '');
}

export function savedScroll(): number | null {
  const y = (history.state as { scrollY?: unknown } | null)?.scrollY;
  return typeof y === 'number' && Number.isFinite(y) && y >= 0 ? y : null;
}
