import { useLayoutEffect, useRef } from 'preact/hooks';
import type { CreatedPost, Post } from '../api/dto';
import { Icon } from '../components/Icon';
import { isControl, trapTab } from '../components/focus';
import { composerFor, type Slow } from '../state/composer';
import { Composer, PostButton } from './Composer';

// ComposerSheet is the phone's composer (docs/06 "Mobile", TKT-01M29CGBVN):
// the same Composer, full screen, as a modal dialog over the thread, with
// the post being answered above it. It is a sheet rather than the inline
// composer the desktop has because on a phone the keyboard takes half the
// screen, and the sheet can keep the post being answered and the toolbar in
// what is left (docs/09, "Mobile composer fights the on-screen keyboard").
//
// It has no gestures: no swipe down to dismiss. Its close button and Escape
// do that, and a swipe added later may only ever be a second way to do what
// a button already does (docs/06 "Targets and gestures"; src/a11y.test.ts
// fails on a gesture handler that is not listed with its button).
//
// It is sized to the visual viewport, which is the part of the layout
// viewport the on-screen keyboard leaves visible, and the formatting toolbar
// is its last row, so the toolbar sits on the keyboard's top edge rather
// than under it. Where visualViewport is missing the sheet is 100dvh.
//
// Closing keeps the text and the reply chip: the reply bar shows them, and
// opening the sheet again carries on where it was left. Post is in the
// sheet's head, where the footer's Post reply would be under the keyboard;
// a posted reply closes the sheet (postReply).
export function ComposerSheet({
  thread,
  authorOf,
  postOf,
  onPosted,
  slow,
}: {
  thread: string;
  authorOf: (ordinal: number) => string | undefined;
  postOf: (ordinal: number) => Post | undefined;
  onPosted: (res: CreatedPost) => void;
  slow?: Slow;
}) {
  const c = composerFor(thread);
  const root = useRef<HTMLDivElement>(null);
  useVisualViewport(root);
  const close = () => (c.open.value = false);

  // While the sheet is up the page behind it does not scroll, and when it
  // goes, focus goes back to whatever opened it: the reply bar, or a post's
  // Reply or Quote. The opener is read while rendering, because the
  // composer's own layout effect, which runs before this one, has already
  // moved focus to the textarea by the time any effect here runs.
  const openerRef = useRef(document.activeElement as HTMLElement | null);
  useLayoutEffect(() => {
    const opener = openerRef.current;
    const html = document.documentElement;
    const overflow = html.style.overflow;
    html.style.overflow = 'hidden';
    return () => {
      html.style.overflow = overflow;
      // Safari does not focus a button on click, so the opener may be the
      // body, or the heading a route change focused; the reply bar is then
      // the way back.
      if (isControl(opener)) opener.focus();
      else document.querySelector<HTMLElement>('.replybar-open')?.focus();
    };
  }, []);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      close();
    } else if (e.key === 'Tab') trapTab(e, root.current);
  };

  const replyTo = c.replyTo.value;
  const answered = replyTo !== null ? postOf(replyTo) : undefined;
  return (
    <div
      class="composer-sheet"
      role="dialog"
      aria-modal="true"
      aria-labelledby="composer-sheet-title"
      ref={root}
      onKeyDown={onKeyDown}
      data-sheet={thread}
    >
      <header class="sheet-head">
        <button type="button" class="sheet-close" aria-label="Close" onClick={close}>
          <Icon name="back" size={18} />
        </button>
        <h2 id="composer-sheet-title">Reply</h2>
        <PostButton thread={thread} onPosted={onPosted} slow={slow} label="Post" />
      </header>
      {answered && answered.html && (
        <figure class="sheet-context" data-answering={answered.ordinal}>
          <figcaption>
            <Icon name="reply" size={12} />#{answered.ordinal} {answered.author.handle}
          </figcaption>
          <div class="post-body" dangerouslySetInnerHTML={{ __html: answered.html }} />
        </figure>
      )}
      <Composer thread={thread} authorOf={authorOf} onPosted={onPosted} slow={slow} sheet />
    </div>
  );
}


// useVisualViewport keeps the sheet on the visual viewport: it sets
// --vv-top and --vv-height on the element, which composer.css places the
// sheet by, whenever the visual viewport moves or resizes, as it does when
// the on-screen keyboard opens. They are custom properties set through the
// CSSOM rather than a style attribute, which the CSP forbids. Where the
// browser has no visualViewport nothing is set and the sheet is 100dvh.
export function useVisualViewport(ref: { current: HTMLElement | null }) {
  useLayoutEffect(() => {
    const vv = window.visualViewport;
    const el = ref.current;
    if (!vv || !el) return;
    const update = () => {
      el.style.setProperty('--vv-top', `${vv.offsetTop}px`);
      el.style.setProperty('--vv-height', `${vv.height}px`);
    };
    update();
    vv.addEventListener('resize', update);
    vv.addEventListener('scroll', update);
    return () => {
      vv.removeEventListener('resize', update);
      vv.removeEventListener('scroll', update);
    };
  }, []);
}
