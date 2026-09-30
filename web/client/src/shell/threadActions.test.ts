import { describe, expect, it, vi } from 'vitest';
import type { Pinned } from '../api/dto';
import { isStaff, threadActions, type ModerateKind, type ModerateState } from './threadActions';

// TKT-01M3QGH66W: the moderation actions on a thread, by its state and the
// viewer's role.
const t = { id: '01k4z3j0w8x2c', slug: 'a' };
const forumPin: Pinned = { scope: 'forum', until: null };
const globalPin: Pinned = { scope: 'global', until: 1790000000000 };
const moderation: ModerateKind[] = ['lock', 'unlock', 'pin', 'unpin'];

const ids = (m?: ModerateState, open = () => {}) =>
  threadActions(t, { canReply: true, moderate: m && { ...m, open } }).map((a) => a.id);
const kinds = (m: ModerateState) => ids(m).filter((id) => (moderation as string[]).includes(id));

describe('the thread actions', () => {
  it('offers no moderation without moderate', () => {
    expect(ids()).toEqual(['reply', 'copy-link']);
    expect(threadActions(t).map((a) => a.id)).toEqual(['copy-link']);
  });

  it('lock or unlock by state', () => {
    expect(kinds({ locked: false, pinned: null, staff: false })).toEqual(['lock', 'pin']);
    expect(kinds({ locked: true, pinned: null, staff: false })).toEqual(['unlock', 'pin']);
    // After Reply and First unread, with Move and Slow mode after them,
    // before Copy link.
    expect(ids({ locked: false, pinned: null, staff: true })).toEqual(['reply', 'lock', 'pin', 'move', 'slow-mode', 'copy-link']);
  });

  // TKT-01M3R0PM0K: slow mode is on the same gate as lock and pin, so it is
  // offered with them, whatever the thread's state or the viewer's role.
  it('offers slow mode wherever it offers moderation', () => {
    for (const locked of [false, true])
      for (const pinned of [null, forumPin, globalPin])
        for (const staff of [false, true]) expect(ids({ locked, pinned, staff })).toContain('slow-mode');
    expect(ids()).not.toContain('slow-mode');
  });

  it('pin, or unpin and pin, by state', () => {
    expect(kinds({ locked: false, pinned: null, staff: true })).toEqual(['lock', 'pin']);
    expect(kinds({ locked: false, pinned: forumPin, staff: true })).toEqual(['lock', 'pin', 'unpin']);
    // A forum pin is anyone's who moderates the forum.
    expect(kinds({ locked: true, pinned: forumPin, staff: false })).toEqual(['unlock', 'pin', 'unpin']);
  });

  it("a global pin is staff's", () => {
    expect(kinds({ locked: false, pinned: globalPin, staff: false })).toEqual(['lock']);
    expect(kinds({ locked: true, pinned: globalPin, staff: false })).toEqual(['unlock']);
    expect(kinds({ locked: false, pinned: globalPin, staff: true })).toEqual(['lock', 'pin', 'unpin']);
  });

  it('opens the panel for the kind chosen', () => {
    const open = vi.fn();
    const actions = threadActions(t, { moderate: { locked: true, pinned: forumPin, staff: false, open } });
    for (const a of actions.filter((a) => a.id !== 'copy-link')) a.run();
    expect(open.mock.calls).toEqual([['unlock'], ['pin'], ['unpin'], ['move'], ['slow-mode']]);
    expect(actions.map((a) => a.label)).toEqual(['Unlock', 'Pin', 'Unpin', 'Move…', 'Slow mode…', 'Copy link']);
  });

  // TKT-01M3QXV399: Rename comes with the rename option, which the page sets
  // for a moderator and for the author inside their window, and Move with
  // moderate, whatever the role.
  it('offers Rename with rename and Move with moderate', () => {
    const rename = vi.fn();
    expect(threadActions(t, { rename }).map((a) => a.id)).toEqual(['rename', 'copy-link']);
    const actions = threadActions(t, { canReply: true, rename, moderate: { locked: false, pinned: null, staff: false, open() {} } });
    expect(actions.map((a) => a.id)).toEqual(['reply', 'rename', 'lock', 'pin', 'move', 'slow-mode', 'copy-link']);
    expect(actions[1].label).toBe('Rename…');
    actions[1].run();
    expect(rename).toHaveBeenCalledOnce();
    expect(threadActions(t, { canReply: true }).map((a) => a.id)).not.toContain('move');
  });

  it('counts only moderators and admins by role as staff', () => {
    expect(['moderator', 'admin', 'member', 'agent', '', undefined].map(isStaff)).toEqual([true, true, false, false, false, false]);
  });
});
