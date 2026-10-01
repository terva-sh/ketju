import { signal } from '@preact/signals';
import { useEffect, useRef } from 'preact/hooks';
import { forgetGets } from '../api/client';
import { forgetAllThreadPages } from './threads';

// The thread list the reader last had on screen, so a thread page can send
// them back to it: marking a thread unread returns to the list the reader
// came from (docs/06 "Read tracking", TKT-01M3KFFFJR). preact-iso keeps no
// history of its own, and the browser shows a page only its current history
// entry, so the list says where it is as it is shown.
//
// forum is null for a cross-forum list (/unread, /new, /participated,
// /watching; TKT-01M3V6Y4JP), which holds threads from every forum, so any
// thread goes back to it.
let last: { forum: string | null; href: string } | null = null;

// rememberList records href, the address of a page of forum's thread list,
// cursor and sort included, or of a cross-forum list when forum is null.
export function rememberList(forum: string | null, href: string) {
  last = { forum, href };
}

// listFor is where a thread in forum goes back to: the list the reader last
// had on screen when it was this forum's or a cross-forum list, and else the
// forum's first page.
export function listFor(forum: string): string {
  return last && (last.forum === null || last.forum === forum) ? last.href : `/f/${encodeURIComponent(forum)}`;
}

// resetLists forgets the list, for tests.
export function resetLists() {
  last = null;
}

// crossLists are the four cross-forum lists' API names (docs/07,
// TKT-01M29CTY7K): GET /api/v1/{name}.
export const crossLists = ['unread', 'new', 'participated', 'watching'] as const;
export type CrossListName = (typeof crossLists)[number];

// isListUrl is a held answer that carries the viewer's unread counts: the
// forum index, a forum's thread list, or a cross-forum list.
export function isListUrl(url: string): boolean {
  if (url.startsWith('/api/v1/forums')) return true;
  return crossLists.some((l) => url === `/api/v1/${l}` || url.startsWith(`/api/v1/${l}?`));
}

// forgetLists drops every held list, and supersedes any still in flight:
// each began before whatever changed the counts (Terva review 1624 on PR
// #248, after a mark-unread).
export function forgetLists() {
  forgetGets(isListUrl, { inFlight: true });
}

// forgetReadState drops everything held that reports the viewer's place,
// after a mark all read (docs/06 "Read tracking"): the lists, and every
// thread page, whose read boundary, divider and landing page the mark has
// moved. Thread pages are dropped whatever their forum, since the client
// does not know which forum each cached thread is in, and a mark is rare.
export function forgetReadState() {
  forgetLists();
  forgetAllThreadPages();
  marks.value++;
}

// marks counts the marks made in this tab. A list on screen asks for itself
// again when it moves (useAfterMark), whichever screen the mark was made
// from: a reader may leave for another list while the PUT is out, and that
// list may have answered before the mark landed (Terva review 1802).
export const marks = signal(0);

// useAfterMark calls retry each time a mark lands while the caller is
// mounted. It does not call it on mount: a list mounted after a mark asked
// for itself after it.
export function useAfterMark(retry: () => void) {
  const n = marks.value;
  const seen = useRef(n);
  useEffect(() => {
    if (n === seen.current) return;
    seen.current = n;
    retry();
  }, [n]);
}
