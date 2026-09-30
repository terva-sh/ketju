import type { Me } from '../../api/dto';

// The admin sections, in the order the section list shows them, and who may
// use each (docs/07 "Moderation and admin"). The server's gates decide what a
// viewer may do; this list only decides what to offer, so it follows the
// same rules and never offers a section whose API would refuse the viewer.
//
//   - Queue: anyone who moderates somewhere. /me's queue says so: it is a
//     number for them and null for anyone else (TKT-01M29CRJ4E), which is
//     how a member who moderates one forum through a group sees it.
//   - Applications and Users: moderators and admins, by role (the staff
//     gate).
//   - Everything else: admins.
export interface Section {
  path: string;
  // The screen's name, as routes.tsx gives it, rendered as data-screen.
  screen: string;
  title: string;
  may: (me: Me) => boolean;
}

const role = (me: Me) => me.user?.role ?? '';
const isAdmin = (me: Me) => role(me) === 'admin';
const isStaff = (me: Me) => isAdmin(me) || role(me) === 'moderator';

// moderatesSomewhere is whether /me gave me a queue. A viewer from before
// the server carried it has none, and is offered none.
const moderatesSomewhere = (me: Me) => me.queue !== null && me.queue !== undefined;

export const sections: Section[] = [
  { path: '/admin/queue', screen: 'admin-queue', title: 'Moderation queue', may: moderatesSomewhere },
  { path: '/admin/applications', screen: 'admin-applications', title: 'Applications', may: isStaff },
  { path: '/admin/users', screen: 'admin-users', title: 'Users', may: isStaff },
  { path: '/admin/forums', screen: 'admin-forums', title: 'Forums', may: isAdmin },
  { path: '/admin/forms', screen: 'admin-forms', title: 'Application forms', may: isAdmin },
  { path: '/admin/settings', screen: 'admin-settings', title: 'Site settings', may: isAdmin },
];

// sectionsFor is the sections me may use, in list order.
export function sectionsFor(me: Me): Section[] {
  return sections.filter((s) => s.may(me));
}
