import type { Post as PostDTO } from '../../api/dto';

// How a removed post reads (docs/08 "Tombstones" and "Flags", docs/07
// "Removed posts"). The thread page, the reply expander and the popover all
// draw from here, so a removal reads the same wherever it appears.

// removedText is what stands in a removed post's place, by who removed it.
// A post from a server that predates `removal` says only that it was
// removed.
export function removedText(post: Pick<PostDTO, 'removal'>): string {
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
// moderator, or to its author when a moderator removed it.
export function removedMark(post: Pick<PostDTO, 'removal'>): string {
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

// TombstoneText is a tombstone's words: who removed the post, or, for a post
// split into another thread, a pointer to it (the server fills moved_to in
// TKT-01M29CYSAM).
export function TombstoneText({ post }: { post: Pick<PostDTO, 'removal' | 'moved_to'> }) {
  if (post.moved_to !== null) {
    return (
      <span class="tombstone-text">
        Post moved to <a href={`/t/${encodeURIComponent(post.moved_to)}`}>another thread</a>
      </span>
    );
  }
  return <span class="tombstone-text">{removedText(post)}</span>;
}
