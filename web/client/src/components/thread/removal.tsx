import { Fragment } from 'preact';
import type { MoveNotice as MoveNoticeDTO, Post as PostDTO, ThreadLink } from '../../api/dto';

// How a removed post reads (docs/08 "Tombstones" and "Flags", docs/07
// "Removed posts"). The thread page, the reply expander, the popover and the
// moderator queue all draw from here, so a removal reads the same wherever
// it appears.

// removedText is what stands in a removed post's place, by who removed it.
// A post from a server that predates `removal` says only that it was
// removed. A hard-redacted post says it is gone for good, whoever removed
// it: that is the one thing its reader needs, since nobody can restore it
// (docs/03 "Hard redaction", TKT-01M3R1WPNS).
export function removedText(post: Pick<PostDTO, 'removal' | 'redacted'>): string {
  if (post.redacted) return 'Post removed permanently';
  switch (post.removal) {
    case 'author':
      return 'Post removed by author';
    case 'moderator':
      return 'Post removed by a moderator';
    case 'review':
      return 'Post hidden pending review';
    default:
      return 'Post removed';
  }
}

// removedMark labels a removed post that is shown with its body: to a
// moderator, or to its author when a moderator removed it. A redacted post
// is served with no body to anyone, but where a mark is drawn beside one,
// as the moderator queue does, it says what its tombstone says.
export function removedMark(post: Pick<PostDTO, 'removal' | 'redacted'>): string {
  if (post.redacted) return 'removed permanently';
  switch (post.removal) {
    case 'author':
      return 'removed by author';
    case 'moderator':
      return 'removed by a moderator';
    case 'review':
      return 'hidden pending review';
    default:
      return 'deleted';
  }
}

// isTombstone is whether a post is drawn as a tombstone: removed or moved,
// and served without its body.
export const isTombstone = (post: Pick<PostDTO, 'deleted' | 'html'>) => post.deleted && !post.html;

// movedHref is where a link to a moved post's other end goes: the copy's
// permalink, with the target's slug, or the target thread while the move is
// unfinished and the copy has no ordinal yet. A source ordinal is never
// used: it is not the copy's (docs/07 "Removed posts").
export function movedHref(thread: ThreadLink, ordinal: number | null): string {
  const base = `/t/${encodeURIComponent(thread.id)}/${encodeURIComponent(thread.slug)}`;
  return ordinal === null ? base : `${base}/${ordinal}`;
}

// TombstoneText is a tombstone's words: who removed the post, or, for a post
// a split or merge moved, where it went. The server names the thread only to
// a viewer who may read it (TKT-01M29CYSAM); anyone else reads that it moved.
export function TombstoneText({ post }: { post: Pick<PostDTO, 'removal' | 'redacted' | 'moved' | 'moved_to'> }) {
  if (post.moved) {
    const to = post.moved_to;
    if (!to) return <span class="tombstone-text">Post moved to another thread</span>;
    return (
      <span class="tombstone-text">
        Post moved to <a href={movedHref(to.thread, to.ordinal)}>{to.thread.title}</a>
        {to.ordinal === null && ' (still moving)'}
      </span>
    );
  }
  return <span class="tombstone-text">{removedText(post)}</span>;
}

// MoveNotice stands above the first post of a move: how many posts were
// moved and, to a viewer who may read it, the thread they went to, linked at
// the move's first copy (docs/06 "Moderator flow for drift").
export function MoveNotice({ notice }: { notice: MoveNoticeDTO }) {
  const what = notice.count === 1 ? '1 post was moved' : `${notice.count} posts were moved`;
  return (
    <p class="move-notice" data-move-notice>
      {what} to{' '}
      {notice.thread ? <a href={movedHref(notice.thread, notice.ordinal)}>{notice.thread.title}</a> : 'another thread'}.
    </p>
  );
}

// ContinuedIn stands under a post that a reply as a new thread continued
// (TKT-01M29CYSC0): "Continued in <link>", one link per thread the server
// lists, which are those the viewer may read. Each goes to the new thread.
export function ContinuedIn({ threads }: { threads: ThreadLink[] }) {
  return (
    <p class="continued-in" data-continued-in>
      Continued in{' '}
      {threads.map((t, i) => (
        <Fragment key={t.id}>
          {i > 0 && ', '}
          <a href={movedHref(t, null)}>{t.title}</a>
        </Fragment>
      ))}
      .
    </p>
  );
}

// MergeNotice stands above the first copy of a merge into this thread
// (TKT-01M29CYSBA): how many posts were merged and, to a viewer who may read
// it, the thread they came from, linked at the first moved post there. The
// copies after it keep their own times, which are older than the posts above
// them: a merge appends and never interleaves (docs/06).
export function MergeNotice({ notice }: { notice: MoveNoticeDTO }) {
  const what = notice.count === 1 ? '1 post was merged' : `${notice.count} posts were merged`;
  return (
    <p class="move-notice" data-merge-notice>
      {what} from{' '}
      {notice.thread ? <a href={movedHref(notice.thread, notice.ordinal)}>{notice.thread.title}</a> : 'another thread'}.
    </p>
  );
}
