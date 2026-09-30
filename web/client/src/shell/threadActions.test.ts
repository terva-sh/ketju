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
    // After Reply and First unread, before Copy link.
    expect(ids({ locked: false, pinned: null, staff: true })).toEqual(['reply', 'lock', 'pin', 'copy-link']);
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
    expect(open.mock.calls).toEqual([['unlock'], ['pin'], ['unpin']]);
    expect(actions.map((a) => a.label)).toEqual(['Unlock', 'Pin', 'Unpin', 'Copy link']);
  });

  it('counts only moderators and admins by role as staff', () => {
    expect(['moderator', 'admin', 'member', 'agent', '', undefined].map(isStaff)).toEqual([true, true, false, false, false, false]);
  });
});
