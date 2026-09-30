import type { Author, Millis, Post, ThreadViewer } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { isTombstone } from './removal';

// isOwn is whether post is the signed-in viewer's own. Handles are compared
// without case, as the server compares them. A pre-user or an anonymous
// viewer owns nothing. dto.Author carries no user id, and a rename changes
// the handle on both sides, so the handle is the comparison.
export function isOwn(post: { author: Pick<Author, 'handle'> }): boolean {
  const me = viewer.value.user;
  return !!me && me.handle.toLowerCase() === post.author.handle.toLowerCase();
}

// Controls is what the viewer is offered on a post, and when the soonest of
// those offers ends: null when none will end, because none is made or no
// window binds the viewer.
export interface Controls {
  edit: boolean;
  delete: boolean;
  restore: boolean;
  closesAt: Millis | null;
}

export const noControls: Controls = { edit: false, delete: false, restore: false, closesAt: null };

// postControls is the one offer rule for Edit, Delete and Restore on a post
// (docs/07 "Edit, delete and restore"). It offers each only where the server
// would accept it at now, in ms since the epoch (authorize and restoreRecord
// in internal/service/threads/mutate.go):
//
//   - never on a moved or redacted post, which nobody may touch again, nor
//     on the viewer's own post in a forum read-only to them
//     (forum_read_only: archived, and the viewer is not an admin);
//   - to a viewer who may moderate the forum (can_moderate, by role or
//     through a group; TKT-01M3QHKD21), on any post, at any age and without
//     reply: Edit and Delete on a post that is not removed, and Restore on a
//     removed post served with its body, whoever removed it;
//   - to anyone else, only on their own post (isOwn; TKT-01M3PTT97X): Edit
//     on a post that is not deleted, while they may reply here (can_reply,
//     which already folds in the lock and an archived forum) and the edit
//     window is open; Delete on a post that is not deleted, while the delete
//     window is open; Restore on a post they deleted themselves (removal
//     "author"), while the delete window is open. A post a moderator hid, or
//     a review holds, is a moderator's to restore.
//
// A window is open for its first N seconds from the post's creation and
// closed from the Nth on, so 0 never opens. author_windows is null for a
// viewer who may moderate the forum, whom neither binds. created_at is
// Millis, a number; one that is not finite is offered nothing a window
// bounds (Terva review 1488 on PR #167).
export function postControls(
  p: Post,
  v: Pick<ThreadViewer, 'can_reply' | 'can_moderate' | 'author_windows' | 'forum_read_only'>,
  now: number,
): Controls {
  const own = isOwn(p);
  if (p.moved_to !== null || p.redacted || (own && v.forum_read_only)) return noControls;
  if (v.can_moderate) {
    return { edit: !p.deleted, delete: !p.deleted, restore: p.deleted && !isTombstone(p), closesAt: null };
  }
  if (!own) return noControls;
  const w = v.author_windows;
  if (w && !Number.isFinite(p.created_at)) return noControls;
  // end is when a window of s seconds closes: before the post, so at once,
  // for a window of 0.
  const end = (s: number) => (s > 0 ? p.created_at + s * 1000 : -Infinity);
  const editEnd = w ? end(w.edit_seconds) : Infinity;
  const deleteEnd = w ? end(w.delete_seconds) : Infinity;
  const c: Controls = {
    edit: !p.deleted && v.can_reply && now < editEnd,
    delete: !p.deleted && now < deleteEnd,
    restore: p.deleted && p.removal === 'author' && now < deleteEnd,
    closesAt: null,
  };
  const ends = [c.edit ? editEnd : Infinity, c.delete || c.restore ? deleteEnd : Infinity];
  const soonest = Math.min(...ends);
  c.closesAt = Number.isFinite(soonest) ? soonest : null;
  return c;
}
