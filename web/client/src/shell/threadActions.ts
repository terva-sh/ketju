import type { Pinned } from '../api/dto';
import { replyToThread } from '../state/composer';
import type { ShellAction } from '../state/shell';

export interface ThreadActionTarget {
  id: string;
  slug: string;
}

// ModerateKind is one of the four thread moderation routes, POST
// /api/v1/admin/threads/{id}/{kind} (docs/07 "Locking and pinning threads").
export type ModerateKind = 'lock' | 'unlock' | 'pin' | 'unpin';

// ModerateState is what decides which moderation actions a thread offers:
// its header's lock and pin, and whether the viewer is a moderator or admin
// by role (staff).
export interface ModerateState {
  locked: boolean;
  pinned: Pinned | null;
  staff: boolean;
}

// moderateKinds is the moderation a viewer who moderates the thread's forum
// is offered, by the thread's state: Lock or Unlock, and Pin, with Unpin
// beside it while a pin is active. A pin replaces the active one, so Pin is
// offered over a pin too.
//
// A global pin is a moderator's or admin's by role (TKT-01M3RQP0TG): the
// server refuses anyone else a pin with scope global, an unpin of a global
// pin, and a pin that would replace one (staffPin in
// internal/service/threads/moderate.go). So while a global pin is active a
// viewer who is not staff is offered neither Pin nor Unpin, and the pin
// panel offers the global scope only to staff. The client offers exactly
// what the server allows.
export function moderateKinds(s: ModerateState): ModerateKind[] {
  const kinds: ModerateKind[] = [s.locked ? 'unlock' : 'lock'];
  if (s.pinned?.scope === 'global' && !s.staff) return kinds;
  kinds.push('pin');
  if (s.pinned) kinds.push('unpin');
  return kinds;
}

const moderateLabels: Record<ModerateKind, string> = { lock: 'Lock', unlock: 'Unlock', pin: 'Pin', unpin: 'Unpin' };

export interface ThreadActionOptions {
  canReply?: boolean;
  firstUnread?: () => void;
  // moderate is set only for a viewer the thread page says moderates the
  // thread (viewer.can_moderate), and open brings up the panel for a kind.
  moderate?: ModerateState & { open: (kind: ModerateKind) => void };
}

// threadActions is every action on a thread, the one list the desktop
// toolbar and the phone's more menu both render (docs/06 "Mobile"), so an
// action added here is on both. Watch (TKT-01M29CA9CE) adds itself here with
// its ticket.
//
// Reply is offered when the reader may reply, which the thread page says
// (viewer.can_reply). It brings up the composer at the foot of the thread as
// a reply to the thread, with no reply_to (docs/06 "Composer").
//
// First unread is offered when the page has a first unread post to go to,
// which the thread page works out and hands over as firstUnread. It is the
// visible control for the u shortcut (keyboard/shortcuts.ts).
//
// Lock or Unlock, Pin and Unpin are offered when the page says the viewer
// moderates the thread (viewer.can_moderate), by moderateKinds. Thread
// moderation follows the forum grant (docs/08 "Decided 2026-09-30"), so a
// member who moderates the forum through a group is offered them too.
export function threadActions(t: ThreadActionTarget, opts: ThreadActionOptions = {}): ShellAction[] {
  const m = opts.moderate;
  return [
    ...(opts.canReply ? [{ id: 'reply', label: 'Reply', run: () => replyToThread(t.id) }] : []),
    ...(opts.firstUnread ? [{ id: 'first-unread', label: 'First unread', run: opts.firstUnread }] : []),
    ...(m ? moderateKinds(m).map((k) => ({ id: k, label: moderateLabels[k], run: () => m.open(k) })) : []),
    {
      id: 'copy-link',
      label: 'Copy link',
      run: () => {
        const url = new URL(`/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}`, location.href).href;
        void navigator.clipboard?.writeText(url).catch(() => {});
      },
    },
  ];
}

// isStaff is a role that is a moderator's or an admin's, as the server's
// Viewer.Staff reads it.
export function isStaff(role: string | undefined): boolean {
  return role === 'moderator' || role === 'admin';
}
