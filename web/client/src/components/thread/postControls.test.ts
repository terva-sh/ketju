import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Me, Post, ThreadViewer } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import { viewer } from '../../state/viewer';
import { mayFlag } from './Post';
import { isOwn, noControls, postControls } from './postControls';

// The offer rule for Edit, Delete and Restore on the viewer's own posts
// (TKT-01M3PTT97X; docs/07 "Edit, delete and restore"). The viewer is the
// member golden's, alice, bound by a 10-minute edit window and a 5-minute
// delete window unless a test says otherwise.

const now = 1_757_600_000_000;
const created = now - 60_000;
const base = (golden as { posts: Post[] }).posts[0];

function post(over: Partial<Post> = {}): Post {
  return {
    ...base,
    author: { ...base.author, handle: 'alice' },
    created_at: created,
    deleted: false,
    removal: null,
    redacted: false,
    moved_to: null,
    ...over,
  };
}

type V = Pick<ThreadViewer, 'can_reply' | 'author_windows'>;
const bound = (over: Partial<V> = {}): V => ({ can_reply: true, author_windows: { edit_seconds: 600, delete_seconds: 300 }, ...over });
const offered = (p: Post, v: V = bound(), at = now) => {
  const c = postControls(p, v, at);
  return { edit: c.edit, delete: c.delete, restore: c.restore };
};

const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
});
afterEach(() => {
  viewer.value = saved;
});

describe('postControls', () => {
  it("offers edit and delete on one's own post inside the windows", () => {
    expect(offered(post())).toEqual({ edit: true, delete: true, restore: false });
    // Nothing on another member's post, nor to a viewer who is not signed in.
    expect(postControls(post({ author: { ...base.author, handle: 'carol' } }), bound(), now)).toEqual(noControls);
    viewer.value = { ...viewer.value, user: null };
    expect(postControls(post(), bound(), now)).toEqual(noControls);
  });

  it('ownership ignores case', () => {
    const p = post({ author: { ...base.author, handle: 'Alice' } });
    expect(isOwn(p)).toBe(true);
    expect(offered(p)).toEqual({ edit: true, delete: true, restore: false });
    // mayFlag shares the comparison, so an own post is never flaggable.
    expect(mayFlag(p)).toBe(false);
    expect(mayFlag(post({ author: { ...base.author, handle: 'carol' } }))).toBe(true);
  });

  it('edit closes at edit_seconds and delete at delete_seconds', () => {
    const p = post();
    expect(offered(p, bound(), created + 300_000 - 1)).toEqual({ edit: true, delete: true, restore: false });
    expect(offered(p, bound(), created + 300_000)).toEqual({ edit: true, delete: false, restore: false });
    expect(offered(p, bound(), created + 600_000 - 1)).toEqual({ edit: true, delete: false, restore: false });
    expect(offered(p, bound(), created + 600_000)).toEqual({ edit: false, delete: false, restore: false });
    // A window of 0 is never open, not even to a post stamped after now.
    const shut = bound({ author_windows: { edit_seconds: 0, delete_seconds: 0 } });
    expect(offered(p, shut, created)).toEqual({ edit: false, delete: false, restore: false });
    expect(offered(p, shut, created - 60_000)).toEqual({ edit: false, delete: false, restore: false });
    expect(offered(post({ deleted: true, removal: 'author' }), shut, created)).toEqual({ edit: false, delete: false, restore: false });
  });

  it('closesAt is the soonest end of what is offered', () => {
    const p = post();
    expect(postControls(p, bound(), now).closesAt).toBe(created + 300_000);
    expect(postControls(p, bound(), created + 300_000).closesAt).toBe(created + 600_000);
    expect(postControls(p, bound(), created + 600_000).closesAt).toBeNull();
    // Only edit is offered in a locked thread, so only its window counts...
    expect(postControls(p, bound({ can_reply: false }), now).closesAt).toBe(created + 300_000);
    // ...and a restore closes with the delete window.
    expect(postControls(post({ deleted: true, removal: 'author' }), bound(), now).closesAt).toBe(created + 300_000);
    expect(postControls(post({ author: { ...base.author, handle: 'carol' } }), bound(), now).closesAt).toBeNull();
  });

  it('null windows bind nothing', () => {
    const free = bound({ author_windows: null });
    const old = now + 365 * 86_400_000;
    expect(offered(post(), free, old)).toEqual({ edit: true, delete: true, restore: false });
    expect(offered(post({ deleted: true, removal: 'author' }), free, old)).toEqual({ edit: false, delete: false, restore: true });
    expect(postControls(post(), free, old).closesAt).toBeNull();
    // Still only on one's own post.
    expect(postControls(post({ author: { ...base.author, handle: 'carol' } }), free, now)).toEqual(noControls);
  });

  it('no edit without can_reply', () => {
    expect(offered(post(), bound({ can_reply: false }))).toEqual({ edit: false, delete: true, restore: false });
    expect(offered(post(), bound({ can_reply: false, author_windows: null }))).toEqual({ edit: false, delete: true, restore: false });
  });

  it("restore only one's own delete", () => {
    expect(offered(post({ deleted: true, removal: 'author' }))).toEqual({ edit: false, delete: false, restore: true });
    for (const removal of ['moderator', 'review', null]) {
      expect(offered(post({ deleted: true, removal }), bound()), String(removal)).toEqual({ edit: false, delete: false, restore: false });
      expect(offered(post({ deleted: true, removal }), bound({ author_windows: null })), String(removal)).toEqual({
        edit: false,
        delete: false,
        restore: false,
      });
    }
    // Outside the delete window, it is not offered.
    expect(offered(post({ deleted: true, removal: 'author' }), bound(), created + 300_000).restore).toBe(false);
  });

  it('nothing on a redacted or a moved post', () => {
    const redacted = post({ deleted: true, removal: 'author', redacted: true, html: '' });
    expect(postControls(redacted, bound(), now)).toEqual(noControls);
    expect(postControls(redacted, bound({ author_windows: null }), now)).toEqual(noControls);
    const moved = post({ deleted: true, removal: null, moved_to: '3yymw47g00099', html: '' });
    expect(postControls(moved, bound({ author_windows: null }), now)).toEqual(noControls);
    expect(postControls(post({ moved_to: '3yymw47g00099' }), bound({ author_windows: null }), now)).toEqual(noControls);
  });

  // Terva review 1488 on PR #167, finding 2: created_at is Millis, a number.
  it('created_at is Millis', () => {
    expect(offered(post({ created_at: now - 1000 }), bound({ author_windows: { edit_seconds: 900, delete_seconds: 900 } }))).toEqual({
      edit: true,
      delete: true,
      restore: false,
    });
    expect(postControls(post({ created_at: now - 1000 }), bound(), now).closesAt).toBe(now - 1000 + 300_000);
    for (const bad of [NaN, Infinity, '2025-09-10T10:00:00Z' as unknown as number]) {
      expect(postControls(post({ created_at: bad }), bound(), now), String(bad)).toEqual(noControls);
    }
    // With no window to bound it, the time does not matter.
    expect(offered(post({ created_at: NaN }), bound({ author_windows: null }))).toEqual({ edit: true, delete: true, restore: false });
  });

  // The longest window the server sends, TKT-01M3R33SR5's clamp: the end is
  // still an exact number, and 30 days on everything is open.
  it('the clamped window is exact', () => {
    const w = bound({ author_windows: { edit_seconds: 9_223_372_036, delete_seconds: 9_223_372_036 } });
    const c = postControls(post(), w, created + 30 * 86_400_000);
    expect(c).toEqual({ edit: true, delete: true, restore: false, closesAt: created + 9_223_372_036_000 });
    expect(Number.isSafeInteger(c.closesAt)).toBe(true);
  });
});
