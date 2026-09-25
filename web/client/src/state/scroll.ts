import { useEffect, useLayoutEffect, useRef } from 'preact/hooks';

// useScrollMemory keeps a list's scroll position per history entry, so
// going back to it lands where the reader was (TKT-01M29CEE6D). preact-iso
// scrolls to the top only on a push that changes the path, and restores
// nothing on back, so a list does both itself.
//
// Each entry is tagged with an id in history.state, and every scroll event
// records the position against the id of the entry the list was rendered
// for, in memory. That is synchronous, so a Back pressed a moment after
// scrolling still finds the latest position: by the time the router sees
// popstate, history.state already names the next entry, and nothing can be
// written into the one being left (review on PR #88). A scroll event in
// that gap, while history.state names the next entry and this one is still
// on screen, is the browser's own restoration and is not recorded; nor is
// anything before this list has restored its own position, while its data
// may still be loading and the page is wherever the browser left it.
//
// The position is also written into history.state itself, so it survives a
// reload: after the reader stops scrolling, only into the entry it was
// recorded for; at once on any click, in the capture phase, which runs
// before the router's own click handler pushes the next entry; and on
// pagehide. A write per scroll event would be simplest, but Safari
// throttles replaceState.
//
// The browser's own restoration is off for every entry a list shows
// (history.scrollRestoration is kept per entry), since it can land after
// this one's and move the reader again; it is back on when the list goes.
//
// Each time a key is shown, and once its data is on screen (ready), the
// saved position is restored; an entry with none, a page just pushed,
// starts at the top. That is each showing, not once per key: a reader who
// goes back and then forward again before the page between has loaded
// comes back to a key that still needs its position (TKT-01M29CGBXK).
export function useScrollMemory(key: string, ready: boolean) {
  // shown is the key on screen; pending, that its position is still to be
  // restored.
  const shown = useRef<string | null>(null);
  const pending = useRef(false);
  useEffect(
    () => () => {
      history.scrollRestoration = 'auto';
    },
    [],
  );
  useEffect(() => {
    history.scrollRestoration = 'manual';
    let timer: ReturnType<typeof setTimeout> | undefined;
    const mine = entryId();
    const live = () => shown.current === key && !pending.current && entryId() === mine;
    const later = () => {
      if (!live()) return;
      positions.set(mine, Math.round(scrollY));
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (live()) rememberScroll();
      }, 100);
    };
    const now = () => {
      clearTimeout(timer);
      if (live()) rememberScroll();
    };
    addEventListener('scroll', later, { passive: true });
    addEventListener('click', now, true);
    addEventListener('pagehide', now);
    return () => {
      clearTimeout(timer);
      removeEventListener('scroll', later);
      removeEventListener('click', now, true);
      removeEventListener('pagehide', now);
    };
  }, [key]);

  useLayoutEffect(() => {
    if (shown.current !== key) {
      shown.current = key;
      pending.current = true;
    }
    if (!ready || !pending.current) return;
    pending.current = false;
    scrollTo(0, savedScroll() ?? 0);
  }, [key, ready]);
}

// positions is each entry's latest position this session, by entry id.
const positions = new Map<string, number>();

type Entry = { entry?: unknown; scrollY?: unknown };

function state(): Record<string, unknown> {
  return history.state && typeof history.state === 'object' ? history.state : {};
}

// entryId is the current history entry's id, given one if it has none.
function entryId(): string {
  const id = (state() as Entry).entry;
  if (typeof id === 'string') return id;
  const made = Math.random().toString(36).slice(2);
  history.replaceState({ ...state(), entry: made }, '');
  return made;
}

export function rememberScroll() {
  const y = Math.round(scrollY);
  const id = entryId();
  positions.set(id, y);
  history.replaceState({ ...state(), scrollY: y }, '');
}

export function savedScroll(): number | null {
  const s = history.state as Entry | null;
  const y = typeof s?.entry === 'string' && positions.has(s.entry) ? positions.get(s.entry) : s?.scrollY;
  return typeof y === 'number' && Number.isFinite(y) && y >= 0 ? y : null;
}
