import type { ComponentChildren } from 'preact';
import { useLayoutEffect, useState } from 'preact/hooks';
import type { Lens, LensGap } from '../../api/dto';
import { postSelector } from '../../keyboard/shortcuts';
import { meHref } from '../../shell/nav';
import { viewer } from '../../state/viewer';
import { lensHref, lensTitle } from './lens';
import { permalink, type ThreadRef } from './Post';
import { PostTime } from './PostTime';

// LensHeader is drawn above a lens's posts (docs/06 "Lenses"): what is being
// filtered, how many posts it shows and hides, and Full thread, the one
// press back to the canonical thread at the post the reader is looking at.
//
// Full thread is a link whose address is always the canonical permalink of
// the post at the top of the viewport, kept current as the reader scrolls,
// so a press, a new tab and a copied link all land there (docs/06 "the lens
// bar"). On a lens page with no posts it is the thread's own address, where
// the server's landing rule places the reader, and so is every empty
// state's link.
export function LensHeader({ lens, thread, first }: { lens: Lens; thread: ThreadRef; first: number | undefined }) {
  const top = useTopPost(first);
  const full = top === undefined ? lensHref(thread) : permalink(thread, top);
  const total = lens.shown + lens.hidden;
  const signedIn = !!viewer.value.user;
  let empty: ComponentChildren = null;
  if (first === undefined) {
    const read = (
      <a class="hit" href={lensHref(thread)}>
        Read the full thread
      </a>
    );
    empty =
      !signedIn && (lens.kind === 'unread' || lens.kind === 'mentions') ? (
        <a class="hit" href={meHref(location.pathname + location.search)}>
          Sign in to see what is new here
        </a>
      ) : (
        <>
          {lens.kind === 'unread' ? 'Nothing unread. ' : 'No posts here. '}
          {read}
        </>
      );
  } else if (lens.kind === 'chain' && lens.shown === 1) {
    empty = `#${lens.value} is not a reply and has no replies.`;
  }
  return (
    <section class="lens-header" aria-label="Lens" data-lens={lens.kind}>
      <p class="lens-what">
        <strong>
          {lensTitle(lens)}
          {lens.kind === 'since' && (
            <>
              {' '}
              <PostTime at={Number(lens.value)} />
            </>
          )}
        </strong>{' '}
        <span class="lens-count" data-lens-count>
          Showing {lens.shown} of {total} {total === 1 ? 'post' : 'posts'} · {lens.hidden} hidden
        </span>
      </p>
      <a class="lens-full hit" href={full} data-full-thread>
        Full thread
      </a>
      {empty && (
        <p class="lens-empty" data-lens-empty>
          {empty}
        </p>
      )}
    </section>
  );
}

// useTopPost is the ordinal of the post at the top of the viewport: the
// first post whose bottom is below the phone's sticky top bar. It starts as
// first, the page's first post, and is measured again at most once a frame
// as the reader scrolls.
function useTopPost(first: number | undefined): number | undefined {
  const [top, setTop] = useState(first);
  useLayoutEffect(() => {
    setTop(first);
    if (first === undefined) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const bar = document.querySelector('.topbar.phone')?.getBoundingClientRect().bottom ?? 0;
      for (const el of document.querySelectorAll<HTMLElement>(postSelector)) {
        if (el.getBoundingClientRect().bottom > bar) return setTop(Number(el.dataset.ordinal));
      }
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    addEventListener('scroll', onScroll, { passive: true });
    return () => {
      removeEventListener('scroll', onScroll);
      cancelAnimationFrame(frame);
    };
  }, [first]);
  return first === undefined ? undefined : top;
}

// GapDivider stands where a lens skipped posts (docs/06 "the lens bar":
// every lens draws one). Its count links to the first skipped post in the
// full thread.
export function GapDivider({ gap, thread }: { gap: LensGap; thread: ThreadRef }) {
  return (
    <div class="lens-gap" data-gap-after={gap.after}>
      <a class="hit" href={permalink(thread, gap.after + 1)}>
        … {gap.count} {gap.count === 1 ? 'post' : 'posts'} …
      </a>
    </div>
  );
}
