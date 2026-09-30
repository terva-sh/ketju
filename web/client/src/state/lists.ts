// The thread list the reader last had on screen, so a thread page can send
// them back to it: marking a thread unread returns to the list the reader
// came from (docs/06 "Read tracking", TKT-01M3KFFFJR). preact-iso keeps no
// history of its own, and the browser shows a page only its current history
// entry, so the list says where it is as it is shown.
let last: { forum: string; href: string } | null = null;

// rememberList records href, the address of a page of forum's thread list,
// cursor and sort included.
export function rememberList(forum: string, href: string) {
  last = { forum, href };
}

// listFor is where a thread in forum goes back to: the list the reader last
// had on screen when it was this forum's, and else the forum's first page.
export function listFor(forum: string): string {
  return last?.forum === forum ? last.href : `/f/${encodeURIComponent(forum)}`;
}

// resetLists forgets the list, for tests.
export function resetLists() {
  last = null;
}
