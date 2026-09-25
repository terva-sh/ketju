// Focus helpers: for the modal dialogs (the composer sheet, the forum
// picker), and for the page as a whole (TKT-01M29CGBXK; docs/07
// "Accessibility").

// trapTab keeps Tab and Shift+Tab inside the dialog, as aria-modal promises:
// from the last control Tab goes to the first, and from the first Shift+Tab
// goes to the last.
export function trapTab(e: KeyboardEvent, root: HTMLElement | null) {
  if (!root) return;
  const all = [...root.querySelectorAll<HTMLElement>(focusable)].filter((el) => !el.closest('[hidden]'));
  if (all.length === 0) return;
  const first = all[0];
  const last = all[all.length - 1];
  const at = document.activeElement;
  if (e.shiftKey && (at === first || !root.contains(at))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && (at === last || !root.contains(at))) {
    e.preventDefault();
    first.focus();
  }
}

const focusable = 'button:not([disabled]), textarea:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

// isControl says whether el is something a reader focuses to act on, which
// a dialog may return focus to: not the body, and not a heading focus was
// moved to (focusMainHeading), which Safari leaves focused when a button is
// clicked, since it does not focus buttons on click.
export function isControl(el: Element | null): el is HTMLElement {
  return el instanceof HTMLElement && el.isConnected && el.matches(focusable);
}

// focusMainHeading moves focus to the screen's heading, the first h1 in its
// main, or to the main when it has none. It does not scroll: a thread page
// has already put the reader on their post, and a list where they were.
export function focusMainHeading() {
  const to = document.querySelector<HTMLElement>('main h1') ?? document.querySelector<HTMLElement>('main');
  if (!to) return;
  if (!to.hasAttribute('tabindex')) to.setAttribute('tabindex', '-1');
  to.focus({ preventScroll: true });
}

// threadOf is the thread id a path is inside, or null.
function threadOf(path: string): string | null {
  return /^\/t\/([^/?#]+)/.exec(path)?.[1] ?? null;
}

// routeFocuser answers the Router's onRouteChange, which it calls after the
// new route has rendered and only when the path has changed: focus goes to
// the new screen's heading, so a screen reader starts there and Tab starts
// from there. A move within one thread (a page, a permalink, the reply
// just posted) leaves focus alone, because the thread page places the
// reader itself and a composer should keep its focus. It starts from the
// path the app loaded at, since the Router does not call it for that one.
export function routeFocuser(start: string): (url: string) => void {
  let prev = start;
  return (url) => {
    const path = url.split(/[?#]/)[0];
    const was = prev;
    prev = path;
    if (path === was) return;
    const t = threadOf(path);
    if (t !== null && t === threadOf(was)) return;
    focusMainHeading();
  };
}

// guardFocus keeps focus from being lost with an element that is removed
// while it has it: the paging link a last page does not draw, a post a
// tombstone replaces. Chromium does not reliably fire blur for a removed
// element, so the guard remembers what last had focus and, when the page
// changes and focus has fallen to the body with that element gone, focuses
// the nearest of its former ancestors still in the page that takes focus,
// or else the screen's heading. It waits a frame first, so a component that
// returns focus itself (a dialog closing) does so. It answers its teardown.
export function guardFocus(): () => void {
  let last: Element | null = null;
  let line: Element[] = [];
  let pending = false;
  const onFocus = (e: FocusEvent) => {
    last = e.target as Element;
    line = [];
    for (let a = last.parentElement; a; a = a.parentElement) line.push(a);
  };
  const lost = () => !!last && !last.isConnected && (document.activeElement === null || document.activeElement === document.body);
  const recover = () => {
    pending = false;
    if (!lost()) return;
    const to = line.find((a): a is HTMLElement => a.isConnected && a.matches(focusable));
    if (to) to.focus({ preventScroll: true });
    else focusMainHeading();
  };
  const watch = new MutationObserver(() => {
    if (pending || !lost()) return;
    pending = true;
    requestAnimationFrame(() => setTimeout(recover, 0));
  });
  document.addEventListener('focusin', onFocus);
  watch.observe(document.body, { childList: true, subtree: true });
  return () => {
    document.removeEventListener('focusin', onFocus);
    watch.disconnect();
  };
}
