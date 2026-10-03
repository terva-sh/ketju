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

  // TKT-01M3KFFFJR: Mark unread only when the page hands it over, after
  // First unread and before the moderation.
  it('offers Mark unread when handed it, and runs it', () => {
    const mark = vi.fn();
    const actions = threadActions(t, {
      canReply: true,
      firstUnread: () => {},
      markUnread: mark,
      moderate: { locked: false, pinned: null, staff: false, open: () => {} },
    });
    expect(actions.map((a) => a.id).slice(0, 4)).toEqual(['reply', 'first-unread', 'mark-unread', 'lock']);
    const action = actions.find((a) => a.id === 'mark-unread')!;
    expect(action.label).toBe('Mark unread');
    action.run();
    expect(mark).toHaveBeenCalledOnce();
    expect(ids()).not.toContain('mark-unread');
  });

  // TKT-01M3V6Y87D: Watch only when the page hands it over, after Mark
  // unread, labelled with the effective level: the thread's own, then its
  // forum's.
  it('offers Watch when handed it, labelled by the effective level', () => {
    const open = vi.fn();
    const watch = (level: string, forumLevel: string) =>
      threadActions(t, { markUnread: () => {}, rename: () => {}, watch: { level, forumLevel, open } });
    expect(watch('', '').map((a) => a.id)).toEqual(['mark-unread', 'watch', 'rename', 'copy-link']);
    const label = (level: string, forumLevel: string) => watch(level, forumLevel).find((a) => a.id === 'watch')!.label;
    expect(label('', '')).toBe('Watch');
    expect(label('watch', '')).toBe('Watching');
    expect(label('mute', 'watch')).toBe('Muted');
    expect(label('watch', 'mute')).toBe('Watching');
    expect(label('', 'mute')).toBe('Muted');
    expect(label('', 'watch')).toBe('Watching');
    watch('', '').find((a) => a.id === 'watch')!.run();
    expect(open).toHaveBeenCalledOnce();
    expect(ids()).not.toContain('watch');
  });

  it('lock or unlock by state', () => {
    expect(kinds({ locked: false, pinned: null, staff: false })).toEqual(['lock', 'pin']);
    expect(kinds({ locked: true, pinned: null, staff: false })).toEqual(['unlock', 'pin']);
    // After Reply and First unread, with Move, Slow mode and Edit tags
    // after them, before Copy link.
    expect(ids({ locked: false, pinned: null, staff: true })).toEqual(['reply', 'lock', 'pin', 'move', 'slow-mode', 'tags', 'copy-link']);
  });

  // TKT-01M3R0PM0K: slow mode is on the same gate as lock and pin, so it is
  // offered with them, whatever the thread's state or the viewer's role.
  it('offers slow mode wherever it offers moderation', () => {
    for (const locked of [false, true])
      for (const pinned of [null, forumPin, globalPin])
        for (const staff of [false, true]) expect(ids({ locked, pinned, staff })).toContain('slow-mode');
    expect(ids()).not.toContain('slow-mode');
  });

  // TKT-01M3YSQPZX: Edit tags is a moderator's (docs/08 "Thread tags"), so
  // it comes with moderate, which the page sets from viewer.can_moderate,
  // whatever the thread's state or the viewer's role, and never without it:
  // not to the author, who is offered Rename.
  it('offers Edit tags with moderate only', () => {
    for (const locked of [false, true])
      for (const pinned of [null, forumPin, globalPin])
        for (const staff of [false, true]) expect(ids({ locked, pinned, staff })).toContain('tags');
    expect(ids()).not.toContain('tags');
    expect(threadActions(t, { canReply: true, rename: () => {} }).map((a) => a.id)).not.toContain('tags');
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
    expect(open.mock.calls).toEqual([['unlock'], ['pin'], ['unpin'], ['move'], ['slow-mode'], ['tags']]);
    expect(actions.map((a) => a.label)).toEqual(['Unlock', 'Pin', 'Unpin', 'Move…', 'Slow mode…', 'Edit tags…', 'Copy link']);
  });

  // TKT-01M3QXV399: Rename comes with the rename option, which the page sets
  // for a moderator and for the author inside their window, and Move with
  // moderate, whatever the role.
  it('offers Rename with rename and Move with moderate', () => {
    const rename = vi.fn();
    expect(threadActions(t, { rename }).map((a) => a.id)).toEqual(['rename', 'copy-link']);
    const actions = threadActions(t, { canReply: true, rename, moderate: { locked: false, pinned: null, staff: false, open() {} } });
    expect(actions.map((a) => a.id)).toEqual(['reply', 'rename', 'lock', 'pin', 'move', 'slow-mode', 'tags', 'copy-link']);
    expect(actions[1].label).toBe('Rename…');
    actions[1].run();
    expect(rename).toHaveBeenCalledOnce();
    expect(threadActions(t, { canReply: true }).map((a) => a.id)).not.toContain('move');
  });

  // TKT-01M3YSQQ33: Archive comes with the archive option, which the page
  // sets for an admin by role on a thread not yet archived, after the
  // moderation and before Copy link. On an archived thread the page hands
  // over watch and Mark unread and nothing that writes, and those stay.
  it('offers Archive with archive, and watch and Mark unread without moderation', () => {
    const archive = vi.fn();
    const actions = threadActions(t, { archive, moderate: { locked: false, pinned: null, staff: true, open() {} } });
    expect(actions.map((a) => a.id).slice(-2)).toEqual(['archive', 'copy-link']);
    const item = actions.find((a) => a.id === 'archive')!;
    expect(item.label).toBe('Archive…');
    item.run();
    expect(archive).toHaveBeenCalledOnce();
    expect(threadActions(t, { moderate: { locked: false, pinned: null, staff: true, open() {} } }).map((a) => a.id)).not.toContain('archive');
    const watch = { level: '', forumLevel: '', open() {} };
    expect(threadActions(t, { markUnread() {}, firstUnread() {}, watch }).map((a) => a.id)).toEqual([
      'first-unread',
      'mark-unread',
      'watch',
      'copy-link',
    ]);
  });

  it('counts only moderators and admins by role as staff', () => {
    expect(['moderator', 'admin', 'member', 'agent', '', undefined].map(isStaff)).toEqual([true, true, false, false, false, false]);
  });
});
